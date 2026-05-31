package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ─── Signal JSON-RPC wire types ───────────────────────────────────────────────

type signalRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      interface{}     `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type signalReceiveParams struct {
	Envelope signalEnvelope `json:"envelope"`
}

type signalEnvelope struct {
	Source       string             `json:"source"`
	SourceName   string             `json:"sourceName"`
	SourceDevice int                `json:"sourceDevice"`
	Timestamp    int64              `json:"timestamp"`
	DataMessage  *signalDataMessage `json:"dataMessage"`
	SyncMessage  json.RawMessage    `json:"syncMessage"`
}

type signalAttachment struct {
	ContentType string `json:"contentType"`
	Filename    string `json:"filename"`
	ID          string `json:"id"`
	Size        int64  `json:"size"`
	VoiceNote   bool   `json:"voiceNote"`
}

type signalDataMessage struct {
	Timestamp   int64              `json:"timestamp"`
	Message     string             `json:"message"`
	GroupInfo   *signalGroupInfo   `json:"groupInfo"`
	Attachments []signalAttachment `json:"attachments"`
}

type signalGroupInfo struct {
	GroupId string `json:"groupId"`
	Type    string `json:"type"`
}

type signalSyncMessage struct {
	SentMessage *signalSyncSentMessage `json:"sentMessage"`
}

type signalSyncSentMessage struct {
	Timestamp   int64              `json:"timestamp"`
	Message     string             `json:"message"`
	Destination string             `json:"destination"`
	GroupInfo   *signalGroupInfo   `json:"groupInfo"`
	Attachments []signalAttachment `json:"attachments"`
}

// signalAttachmentsDir is where signal-cli daemon auto-saves received attachments.
var signalAttachmentsDir = func() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "signal-cli", "attachments")
}()

// audioExtensions is the set of file extensions treated as voice notes.
var audioExtensions = map[string]bool{
	".aac": true, ".mp3": true, ".ogg": true, ".m4a": true, ".opus": true,
}

// transcribeSignalVoice finds the first audio attachment, resolves its local path,
// and returns a Whisper transcript. Returns ("", nil) if none found.
func transcribeSignalVoice(attachments []signalAttachment) (string, error) {
	for _, a := range attachments {
		ct := strings.ToLower(a.ContentType)
		if !strings.HasPrefix(ct, "audio/") && !a.VoiceNote {
			continue
		}
		var path string
		switch {
		case filepath.IsAbs(a.Filename):
			path = a.Filename
		case a.Filename != "":
			path = filepath.Join(signalAttachmentsDir, a.Filename)
		case a.ID != "":
			base := filepath.Join(signalAttachmentsDir, a.ID)
			if _, err := os.Stat(base); err == nil {
				path = base
			} else {
				for ext := range audioExtensions {
					if candidate := base + ext; func() bool { _, e := os.Stat(candidate); return e == nil }() {
						path = candidate
						break
					}
				}
			}
		}
		if path == "" {
			log.Printf("Signal: cannot locate attachment id=%s filename=%q — skipping", a.ID, a.Filename)
			continue
		}
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("attachment file not found at %s: %w", path, err)
		}
		log.Printf("Signal: transcribing voice attachment %s", path)
		return transcribeAudio(path)
	}
	return "", nil
}

// ─── Last-active chat tracker ─────────────────────────────────────────────────

var (
	lastSignalActiveChatMu sync.Mutex
	lastSignalActiveChat   string
	lastSignalActiveChatTs time.Time
)

func setLastSignalActiveChat(chatID string) {
	lastSignalActiveChatMu.Lock()
	lastSignalActiveChat = chatID
	lastSignalActiveChatTs = time.Now()
	lastSignalActiveChatMu.Unlock()
}

func getLastSignalActiveChat() (string, bool) {
	lastSignalActiveChatMu.Lock()
	defer lastSignalActiveChatMu.Unlock()
	if lastSignalActiveChat == "" || time.Since(lastSignalActiveChatTs) > 10*time.Minute {
		return "", false
	}
	return lastSignalActiveChat, true
}

// ─── TCP connection state ─────────────────────────────────────────────────────

var (
	signalConnMu    sync.Mutex
	signalConn      net.Conn
	signalIDCounter int64
)

// ─── Deduplication ────────────────────────────────────────────────────────────

var (
	signalDedupeMu   sync.Mutex
	signalDedupeSeen = make(map[string]struct{})
)

func signalMarkSeen(key string) bool {
	signalDedupeMu.Lock()
	defer signalDedupeMu.Unlock()
	if _, ok := signalDedupeSeen[key]; ok {
		return true
	}
	signalDedupeSeen[key] = struct{}{}
	if len(signalDedupeSeen) > 1000 {
		for k := range signalDedupeSeen {
			delete(signalDedupeSeen, k)
			break
		}
	}
	return false
}

// ─── sendSignalMessage ────────────────────────────────────────────────────────

func sendSignalMessage(chatID, message string) {
	signalConnMu.Lock()
	conn := signalConn
	signalConnMu.Unlock()

	if conn == nil {
		log.Printf("Signal: cannot send to %s — not connected", chatID)
		return
	}

	id := atomic.AddInt64(&signalIDCounter, 1)

	var params map[string]interface{}
	if strings.HasPrefix(chatID, "+") {
		params = map[string]interface{}{
			"recipient": []string{chatID},
			"message":   message,
		}
	} else {
		params = map[string]interface{}{
			"groupId": chatID,
			"message": message,
		}
	}

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "send",
		"params":  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		log.Printf("Signal: marshal send error: %v", err)
		return
	}
	data = append(data, '\n')

	conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	if _, err := conn.Write(data); err != nil {
		log.Printf("Signal: write error sending to %s: %v", chatID, err)
		signalConnMu.Lock()
		if signalConn == conn {
			signalConn = nil
		}
		signalConnMu.Unlock()
		conn.Close()
	}
	conn.SetWriteDeadline(time.Time{})
}

// ─── Owner number auto-detection ─────────────────────────────────────────────

var signalOwnerNumber string

type signalAccountsFile struct {
	Accounts []struct {
		Number string `json:"number"`
	} `json:"accounts"`
}

// detectSignalOwnerNumber reads signal-cli's accounts.json and returns the
// registered phone number, or "" if not found.
func detectSignalOwnerNumber() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidates := []string{
		filepath.Join(home, ".local", "share", "signal-cli", "data", "accounts.json"),
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		candidates = append(candidates, filepath.Join(appdata, "signal-cli", "data", "accounts.json"))
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var af signalAccountsFile
		if err := json.Unmarshal(data, &af); err != nil {
			continue
		}
		for _, a := range af.Accounts {
			if strings.HasPrefix(a.Number, "+") {
				return a.Number
			}
		}
	}
	return ""
}

func initSignalOwnerNumber() {
	n := detectSignalOwnerNumber()
	if n == "" {
		n = os.Getenv("SIGNAL_OWNER_NUMBER")
	}
	if n == "" {
		log.Printf("Signal: owner number unknown — bridge commands and isFromMe unavailable until detected")
		return
	}
	signalOwnerNumber = n
	masked := n
	if len(n) > 6 {
		masked = n[:4] + strings.Repeat("*", len(n)-6) + n[len(n)-2:]
	}
	log.Printf("Signal: owner number detected: %s", masked)
}

// ─── Bridge commands ──────────────────────────────────────────────────────────

func handleSignalBridgeCommand(chatID, content string, isFromMe bool) bool {
	cmd := strings.TrimSpace(content)
	isPersonality := strings.HasPrefix(cmd, "!set-personality")
	switch cmd {
	case "!meet-claude", "!meet-codex", "!remove-claude", "!remove-codex", "!help", "!clear-session":
	default:
		if !isPersonality {
			return false
		}
	}
	if !isFromMe {
		sendSignalMessage(chatID, "⚠️ Only the bridge owner can use bridge commands")
		return true
	}
	switch cmd {
	case "!help":
		sendSignalMessage(chatID, "Bridge commands:\n"+
			"!meet-claude — add this chat to Claude whitelist\n"+
			"!remove-claude — remove this chat from Claude whitelist\n"+
			"!meet-codex — add this chat to Codex whitelist\n"+
			"!remove-codex — remove this chat from Codex whitelist\n"+
			"!clear-session — clear Claude/Codex session memory and start fresh\n"+
			"!set-personality <preset> — set personality (default / kids / pro / creative)\n"+
			"!stats — show token usage and cost for this session\n"+
			"!help — show this help screen")
	case "!meet-claude":
		signalAllowedChatsMu.Lock()
		signalAllowedChats[chatID] = struct{}{}
		signalAllowedChatsMu.Unlock()
		if err := saveSignalAllowedChats(); err != nil {
			sendSignalMessage(chatID, "⚠️ Failed to save whitelist: "+err.Error())
			return true
		}
		sendSignalMessage(chatID, "👋 Hi! I'm Claude. This chat is now connected to me — send any message to get started.")
	case "!meet-codex":
		signalCodexAllowedChatsMu.Lock()
		signalCodexAllowedChats[chatID] = struct{}{}
		signalCodexAllowedChatsMu.Unlock()
		if err := saveSignalCodexAllowedChats(); err != nil {
			sendSignalMessage(chatID, "⚠️ Failed to save whitelist: "+err.Error())
			return true
		}
		sendSignalMessage(chatID, "👋 Hi! I'm Codex. This chat is now connected to me — send any message to get started.")
	case "!remove-claude":
		signalAllowedChatsMu.Lock()
		delete(signalAllowedChats, chatID)
		signalAllowedChatsMu.Unlock()
		if err := saveSignalAllowedChats(); err != nil {
			sendSignalMessage(chatID, "⚠️ Failed to save whitelist: "+err.Error())
			return true
		}
		sendSignalMessage(chatID, "✅ Claude has left this chat.")
	case "!remove-codex":
		signalCodexAllowedChatsMu.Lock()
		delete(signalCodexAllowedChats, chatID)
		signalCodexAllowedChatsMu.Unlock()
		if err := saveSignalCodexAllowedChats(); err != nil {
			sendSignalMessage(chatID, "⚠️ Failed to save whitelist: "+err.Error())
			return true
		}
		sendSignalMessage(chatID, "✅ Codex has left this chat.")
	case "!clear-session":
		sessions := loadSessions()
		codexSessions := loadCodexSessions()
		_, hasSession := sessions[chatID]
		_, hasCodexSession := codexSessions[chatID]
		if !hasSession && !hasCodexSession {
			sendSignalMessage(chatID, "No active session to clear.")
			return true
		}
		deleteSession(chatID)
		deleteCodexSession(chatID)
		inputHistoryMu.Lock()
		delete(inputHistory, chatID)
		inputHistoryMu.Unlock()
		sendSignalMessage(chatID, "✅ Session cleared for this chat. Next message starts fresh.")
	}
	if isPersonality {
		parts := strings.Fields(cmd)
		if len(parts) < 2 {
			current := getChatPersonality(chatID)
			sendSignalMessage(chatID, fmt.Sprintf("Current personality: %s\nAvailable: default, kids, pro, creative", current))
			return true
		}
		preset := parts[1]
		switch preset {
		case "default", "kids", "pro", "creative":
			if err := setChatPersonality(chatID, preset); err != nil {
				sendSignalMessage(chatID, "⚠️ Failed to save personality: "+err.Error())
				return true
			}
			sendSignalMessage(chatID, fmt.Sprintf("✅ Personality set to: %s", preset))
		default:
			sendSignalMessage(chatID, "⚠️ Unknown preset. Available: default, kids, pro, creative")
		}
	}
	return true
}

// ─── Message router ───────────────────────────────────────────────────────────

func dispatchSignalContent(chatID, content string) {
	if content == "" || !isSignalAllowedChat(chatID) {
		return
	}
	setLastSignalActiveChat(chatID)

	if isLooping(chatID, content) {
		sendSignalMessage(chatID, "⚠️ You've sent the same message several times. Try rephrasing or type 'clear session' to start fresh.")
		return
	}
	addToInputHistory(chatID, content)

	if isSignalCodexChat(chatID) {
		if strings.ToLower(strings.TrimSpace(content)) == "!stats" {
			codexStatsMu.Lock()
			cStats, cOk := codexStatsMap[chatID]
			codexStatsMu.Unlock()
			var reply string
			if !cOk {
				reply = "No stats yet — send a message first."
			} else {
				reply = fmt.Sprintf("📊 Codex stats:\n• Input tokens: %d\n• Output tokens: %d\n• Total tokens: %d\n• Last updated: %s",
					cStats.InputTokens, cStats.OutputTokens, cStats.TotalTokens, cStats.LastUpdated)
			}
			sendSignalMessage(chatID, reply)
		} else {
			go handleWithCodex(chatID, content, func(reply string) { sendSignalMessage(chatID, reply) })
		}
	} else {
		if strings.ToLower(strings.TrimSpace(content)) == "!stats" {
			usageStatsMu.Lock()
			stats, ok := usageStatsMap[chatID]
			usageStatsMu.Unlock()
			var reply string
			if !ok {
				reply = "No stats yet — send a message first."
			} else {
				durationSec := float64(stats.DurationMs) / 1000.0
				reply = fmt.Sprintf("📊 Stats for this session:\n• Cache read: %d tokens\n• Cache write: %d tokens\n• Input tokens: %d\n• Output tokens: %d\n• Total cost: $%.4f USD\n• Response time: %.1fs\n• Last updated: %s",
					stats.CacheReadTokens, stats.CacheWriteTokens,
					stats.InputTokens, stats.OutputTokens,
					stats.TotalCostUSD, durationSec, stats.LastUpdated)
			}
			sendSignalMessage(chatID, reply)
		} else {
			go handleWithClaude(chatID, content, func(reply string) { sendSignalMessage(chatID, reply) })
		}
	}
}

func handleSignalMessage(env signalEnvelope) {
	if len(env.SyncMessage) > 0 && string(env.SyncMessage) != "null" {
		// syncMessage is the "sent from another device" mirror — only valid when we are the sender.
		// When someone else sends in a group, signal-cli emits both a dataMessage (correct, has
		// downloaded attachments) and a spurious syncMessage; drop the syncMessage in that case.
		if env.Source != "" && signalOwnerNumber != "" && env.Source != signalOwnerNumber {
			log.Printf("Signal: syncMessage from non-self source %s, skipping (handled by dataMessage)", env.Source)
			return
		}
		var sync signalSyncMessage
		if err := json.Unmarshal(env.SyncMessage, &sync); err != nil {
			log.Printf("Signal: syncMessage unmarshal error: %v", err)
			return
		}
		if sync.SentMessage == nil {
			return
		}
		msg := sync.SentMessage
		var chatID string
		if msg.GroupInfo != nil && msg.GroupInfo.GroupId != "" {
			chatID = msg.GroupInfo.GroupId
		} else {
			chatID = msg.Destination
		}
		if chatID == "" {
			log.Printf("Signal: sentMessage has no chatID — skipping")
			return
		}
		if handleSignalBridgeCommand(chatID, msg.Message, true) {
			return
		}
		dedupeKey := fmt.Sprintf("%s:%d", chatID, msg.Timestamp)
		if signalMarkSeen(dedupeKey) {
			return
		}
		content := msg.Message
		if content == "" && len(msg.Attachments) > 0 {
			transcript, err := transcribeSignalVoice(msg.Attachments)
			if err != nil {
				log.Printf("Signal: voice transcription error: %v", err)
				sendSignalMessage(chatID, "⚠️ Could not transcribe voice message: "+err.Error())
				return
			}
			if transcript != "" {
				content = "[🎤 Voice]: " + transcript
			}
		}
		if content == "" {
			return
		}
		log.Printf("Signal sync← (chat=%s): %s", chatID, content)
		dispatchSignalContent(chatID, content)
		return
	}

	if env.DataMessage == nil {
		return
	}

	content := env.DataMessage.Message

	var chatID string
	if env.DataMessage.GroupInfo != nil && env.DataMessage.GroupInfo.GroupId != "" {
		chatID = env.DataMessage.GroupInfo.GroupId
	} else {
		chatID = env.Source
	}
	if chatID == "" {
		return
	}

	if !isSignalAllowedChat(chatID) {
		if len(content) < 2 || content[:2] != "!m" {
			return
		}
	}

	dedupeKey := fmt.Sprintf("%s:%d", chatID, env.DataMessage.Timestamp)
	if signalMarkSeen(dedupeKey) {
		return
	}

	log.Printf("Signal ← %s (chat=%s): %s", env.Source, chatID, content)

	isFromMe := signalOwnerNumber != "" && env.Source == signalOwnerNumber
	if handleSignalBridgeCommand(chatID, content, isFromMe) {
		return
	}

	if content == "" && len(env.DataMessage.Attachments) > 0 {
		transcript, err := transcribeSignalVoice(env.DataMessage.Attachments)
		if err != nil {
			log.Printf("Signal: voice transcription error: %v", err)
			sendSignalMessage(chatID, "⚠️ Could not transcribe voice message: "+err.Error())
			return
		}
		if transcript != "" {
			content = "[🎤 Voice]: " + transcript
		}
	}

	dispatchSignalContent(chatID, content)
}

// ─── Heartbeat ────────────────────────────────────────────────────────────────

func signalHeartbeat(conn net.Conn, stop <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			ping := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      0,
				"method":  "version",
				"params":  map[string]interface{}{},
			}
			data, _ := json.Marshal(ping)
			data = append(data, '\n')
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := conn.Write(data); err != nil {
				log.Printf("Signal: heartbeat failed: %v — closing connection", err)
				conn.Close()
				return
			}
			conn.SetWriteDeadline(time.Time{})
		}
	}
}

// ─── Listener goroutine ───────────────────────────────────────────────────────

func startSignalListener() {
	backoff := time.Second
	for {
		log.Printf("Signal: connecting to 127.0.0.1:7583...")
		conn, err := net.DialTimeout("tcp", "127.0.0.1:7583", 10*time.Second)
		if err != nil {
			log.Printf("Signal: connect failed: %v, retrying in %v", err, backoff)
			time.Sleep(backoff)
			backoff *= 2
			if backoff > 60*time.Second {
				backoff = 60 * time.Second
			}
			continue
		}

		log.Printf("Signal: connected to signal-cli daemon")
		backoff = time.Second

		signalConnMu.Lock()
		signalConn = conn
		signalConnMu.Unlock()

		heartbeatStop := make(chan struct{})
		go signalHeartbeat(conn, heartbeatStop)

		decoder := json.NewDecoder(conn)
		for {
			conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			var msg signalRPCMessage
			if err := decoder.Decode(&msg); err != nil {
				log.Printf("Signal: read error: %v — reconnecting", err)
				break
			}
			conn.SetReadDeadline(time.Time{})

			if msg.Method == "receive" {
				var params signalReceiveParams
				if err := json.Unmarshal(msg.Params, &params); err != nil {
					log.Printf("Signal: failed to parse receive params: %v", err)
					continue
				}
				go handleSignalMessage(params.Envelope)
			} else if msg.Method == "" && msg.ID != nil && msg.ID != float64(0) {
				if len(msg.Error) > 0 && string(msg.Error) != "null" {
					log.Printf("Signal: RPC error id=%v: %s", msg.ID, string(msg.Error))
				}
			}
		}

		close(heartbeatStop)

		signalConnMu.Lock()
		if signalConn == conn {
			signalConn = nil
		}
		signalConnMu.Unlock()
		conn.Close()

		log.Printf("Signal: disconnected, retrying in %v", backoff)
		time.Sleep(backoff)
		backoff *= 2
		if backoff > 60*time.Second {
			backoff = 60 * time.Second
		}
	}
}
