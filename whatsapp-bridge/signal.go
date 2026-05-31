package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
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
	// SyncMessage is present for messages sent from our own devices; we use it only to detect and skip echoes.
	SyncMessage json.RawMessage `json:"syncMessage"`
}

type signalAttachment struct {
	ContentType string `json:"contentType"`
	Filename    string `json:"filename"`
	ID          string `json:"id"`
	Size        int64  `json:"size"`
	VoiceNote   bool   `json:"voiceNote"`
}

type signalDataMessage struct {
	Timestamp   int64               `json:"timestamp"`
	Message     string              `json:"message"`
	GroupInfo   *signalGroupInfo    `json:"groupInfo"`
	Attachments []signalAttachment  `json:"attachments"`
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

// transcribeSignalVoice finds the first audio attachment in the list, resolves its
// local path, and returns a Whisper transcript. Returns ("", nil) if none found.
func transcribeSignalVoice(attachments []signalAttachment) (string, error) {
	for _, a := range attachments {
		ct := strings.ToLower(a.ContentType)
		if !strings.HasPrefix(ct, "audio/") && !a.VoiceNote {
			continue
		}

		// Resolve local path: absolute > relative filename > ID-based lookup.
		var path string
		switch {
		case filepath.IsAbs(a.Filename):
			path = a.Filename
		case a.Filename != "":
			path = filepath.Join(signalAttachmentsDir, a.Filename)
		case a.ID != "":
			// signal-cli saves attachments as <id> or <id>.<ext>; try both.
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

// audioExtensions is the set of file extensions treated as voice notes.
var audioExtensions = map[string]bool{
	".aac": true, ".mp3": true, ".ogg": true, ".m4a": true, ".opus": true,
}


// ─── Signal whitelist (Claude) ────────────────────────────────────────────────

var (
	signalAllowedChatsFile = filepath.Join(storeDir(), "signal_allowed_chats.json")
	signalAllowedChats     map[string]struct{}
	signalAllowedChatsMu   sync.RWMutex
)

func loadSignalAllowedChats() map[string]struct{} {
	data, err := os.ReadFile(signalAllowedChatsFile)
	if err != nil {
		return map[string]struct{}{}
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return map[string]struct{}{}
	}
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

func initSignalAllowedChats() {
	os.MkdirAll(storeDir(), 0755)
	signalAllowedChatsMu.Lock()
	signalAllowedChats = loadSignalAllowedChats()
	signalAllowedChatsMu.Unlock()
}

func saveSignalAllowedChats() error {
	signalAllowedChatsMu.RLock()
	ids := make([]string, 0, len(signalAllowedChats))
	for id := range signalAllowedChats {
		ids = append(ids, id)
	}
	signalAllowedChatsMu.RUnlock()
	data, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(signalAllowedChatsFile, data, 0644)
}

func isSignalAllowedChat(id string) bool {
	signalAllowedChatsMu.RLock()
	_, ok := signalAllowedChats[id]
	signalAllowedChatsMu.RUnlock()
	if ok {
		return true
	}
	signalCodexAllowedChatsMu.RLock()
	_, ok = signalCodexAllowedChats[id]
	signalCodexAllowedChatsMu.RUnlock()
	return ok
}

// ─── Signal whitelist (Codex) ─────────────────────────────────────────────────

var (
	signalCodexAllowedChatsFile = filepath.Join(storeDir(), "signal_codex_allowed_chats.json")
	signalCodexAllowedChats     map[string]struct{}
	signalCodexAllowedChatsMu   sync.RWMutex
)

func loadSignalCodexAllowedChats() map[string]struct{} {
	data, err := os.ReadFile(signalCodexAllowedChatsFile)
	if err != nil {
		return map[string]struct{}{}
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return map[string]struct{}{}
	}
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

func initSignalCodexAllowedChats() {
	os.MkdirAll(storeDir(), 0755)
	signalCodexAllowedChatsMu.Lock()
	signalCodexAllowedChats = loadSignalCodexAllowedChats()
	signalCodexAllowedChatsMu.Unlock()
}

func saveSignalCodexAllowedChats() error {
	signalCodexAllowedChatsMu.RLock()
	ids := make([]string, 0, len(signalCodexAllowedChats))
	for id := range signalCodexAllowedChats {
		ids = append(ids, id)
	}
	signalCodexAllowedChatsMu.RUnlock()
	data, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(signalCodexAllowedChatsFile, data, 0644)
}

func isSignalCodexChat(id string) bool {
	signalCodexAllowedChatsMu.RLock()
	defer signalCodexAllowedChatsMu.RUnlock()
	_, ok := signalCodexAllowedChats[id]
	return ok
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

// signalMarkSeen returns true if the key was already seen (duplicate), false otherwise.
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

// sendSignalMessage sends a message via the signal-cli JSON-RPC daemon.
// chatID is either "+E.164number" for 1:1 chats or a Signal group ID.
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

// ─── Bridge commands ──────────────────────────────────────────────────────────

// signalOwnerNumber is the Signal phone number (e.g. "+14155552671") that is
// allowed to run bridge commands (!meet-claude etc.). Set via SIGNAL_OWNER_NUMBER env var.
var signalOwnerNumber = os.Getenv("SIGNAL_OWNER_NUMBER")

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

// ─── Claude handler for Signal ────────────────────────────────────────────────

func handleWithClaudeSignal(chatID, messageText string) {
	sessions := loadSessions()
	sessionID, hasSession := sessions[chatID]

	isNewSession := !hasSession || sessionID == ""

	args := []string{"-p", "--output-format", "json"}
	if hasSession && sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	if isNewSession {
		if prompt := getPersonalityPrompt(chatID); prompt != "" {
			messageText = prompt + "\n\n" + messageText
		}
	}

	cmd := exec.Command("claude", args...)
	cmd.Dir = filepath.Dir(storeDir())
	cmd.Stdin = strings.NewReader(messageText)

	out, err := cmd.Output()
	if err != nil {
		log.Printf("Signal Claude error for %s: %v", chatID, err)
		sendSignalMessage(chatID, "⚠️ Claude unreachable: "+err.Error())
		return
	}

	var resp struct {
		Result     string `json:"result"`
		SessionID  string `json:"session_id"`
		IsError    bool   `json:"is_error"`
		DurationMs int    `json:"duration_ms"`
		Usage      struct {
			CacheReadTokens  int     `json:"cache_read_input_tokens"`
			CacheWriteTokens int     `json:"cache_creation_input_tokens"`
			InputTokens      int     `json:"input_tokens"`
			OutputTokens     int     `json:"output_tokens"`
			TotalCostUSD     float64 `json:"total_cost_usd"`
		} `json:"usage"`
		ModelUsage map[string]struct {
			InputTokens     int     `json:"input_tokens"`
			OutputTokens    int     `json:"output_tokens"`
			CacheReadTokens int     `json:"cache_read_input_tokens"`
			CostUSD         float64 `json:"cost_usd"`
		} `json:"modelUsage"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		log.Printf("Signal: failed to parse Claude response: %v", err)
		sendSignalMessage(chatID, "⚠️ Bridge parse error: "+err.Error())
		return
	}
	if resp.IsError {
		sendSignalMessage(chatID, "⚠️ Claude error: "+resp.Result)
		return
	}
	if resp.Result == "" {
		return
	}

	if resp.SessionID != "" {
		saveSession(chatID, resp.SessionID)
	}

	modelUsage := make(map[string]ModelUsageEntry, len(resp.ModelUsage))
	for model, mu := range resp.ModelUsage {
		modelUsage[model] = ModelUsageEntry{
			InputTokens:     mu.InputTokens,
			OutputTokens:    mu.OutputTokens,
			CacheReadTokens: mu.CacheReadTokens,
			CostUSD:         mu.CostUSD,
		}
	}
	usageStatsMu.Lock()
	usageStatsMap[chatID] = UsageStats{
		CacheReadTokens:  resp.Usage.CacheReadTokens,
		CacheWriteTokens: resp.Usage.CacheWriteTokens,
		InputTokens:      resp.Usage.InputTokens,
		OutputTokens:     resp.Usage.OutputTokens,
		TotalCostUSD:     resp.Usage.TotalCostUSD,
		DurationMs:       resp.DurationMs,
		ModelUsage:       modelUsage,
		LastUpdated:      time.Now().Format("2006-01-02 15:04:05"),
	}
	usageStatsMu.Unlock()

	sendSignalMessage(chatID, "🤖🇫🇷\n"+resp.Result)
}

// ─── Codex handler for Signal ─────────────────────────────────────────────────

func handleWithCodexSignal(chatID, messageText string) {
	sessions := loadCodexSessions()
	sessionID := sessions[chatID]

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("codex_reply_%d.txt", time.Now().UnixNano()))
	defer os.Remove(tmpFile)

	var args []string
	if sessionID != "" {
		args = []string{"exec", "--json", "--output-last-message", tmpFile,
			"resume", sessionID, messageText}
	} else {
		args = []string{"exec", "--json", "--output-last-message", tmpFile,
			"--skip-git-repo-check",
			"--dangerously-bypass-approvals-and-sandbox",
			"-s", "workspace-write",
			messageText}
	}

	cmd := exec.Command("codex", args...)
	cmd.Dir = filepath.Dir(storeDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Signal Codex error for %s: %v\nOutput: %s", chatID, err, string(out))
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			exitCode := -1
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			sendSignalMessage(chatID, fmt.Sprintf("⚠️ Codex error (exit %d): %s", exitCode, trimmed))
		} else {
			sendSignalMessage(chatID, "⚠️ Codex error: "+err.Error())
		}
		return
	}

	rawOutput := string(out)
	newID, inputTokens, outputTokens := parseCodexJSONL(rawOutput)

	if newID != "" {
		saveCodexSession(chatID, newID)
	} else if textID := parseCodexSessionID(rawOutput); textID != "" {
		saveCodexSession(chatID, textID)
	}

	if inputTokens > 0 || outputTokens > 0 {
		codexStatsMu.Lock()
		codexStatsMap[chatID] = CodexStats{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			TotalTokens:  inputTokens + outputTokens,
			LastUpdated:  time.Now().Format("2006-01-02 15:04:05"),
		}
		codexStatsMu.Unlock()
	}

	var replyText string
	if data, err := os.ReadFile(tmpFile); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		replyText = strings.TrimSpace(string(data))
	} else {
		replyText = extractCodexReply(rawOutput)
	}

	sendSignalMessage(chatID, "🤖⚡\n"+replyText)
}

// ─── Message router ───────────────────────────────────────────────────────────

// dispatchSignalContent routes a validated, deduplicated message to Claude or Codex.
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
		lower := strings.ToLower(content)
		if strings.Contains(lower, "tokens") || strings.Contains(lower, "usage") || strings.Contains(lower, "cost") {
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
			go handleWithCodexSignal(chatID, content)
		}
	} else {
		lower := strings.ToLower(content)
		if strings.Contains(lower, "cache") || strings.Contains(lower, "cost") ||
			strings.Contains(lower, "tokens used") || strings.Contains(lower, "usage stats") ||
			strings.Contains(lower, "how much") || strings.Contains(lower, "spending") {
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
			go handleWithClaudeSignal(chatID, content)
		}
	}
}

func handleSignalMessage(env signalEnvelope) {
	// Sync messages are echoes of messages the account owner sent from another linked
	// device (e.g. their phone). Handle both bridge commands and regular messages.
	if len(env.SyncMessage) > 0 && string(env.SyncMessage) != "null" {
		var sync signalSyncMessage
		if err := json.Unmarshal(env.SyncMessage, &sync); err != nil {
			log.Printf("Signal: syncMessage unmarshal error: %v", err)
			return
		}
		if sync.SentMessage == nil {
			// Bare sync (receipt/keepalive): nothing to do.
			return
		}
		{
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
			// Bridge commands are always owner-authorised in sync messages.
			if handleSignalBridgeCommand(chatID, msg.Message, true) {
				return
			}
			// Deduplicate before transcription (which is expensive).
			dedupeKey := fmt.Sprintf("sync:%s:%d", chatID, msg.Timestamp)
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
			// sentMessage with empty text and no usable attachments: nothing to dispatch.
			if content == "" {
				return
			}
			log.Printf("Signal sync← (chat=%s): %s", chatID, content)
			dispatchSignalContent(chatID, content)
		}
		return
	}

	// Only process data messages (actual received texts from other Signal users).
	if env.DataMessage == nil {
		return
	}

	content := env.DataMessage.Message

	// Determine chat ID: group ID for group chats, sender phone for 1:1.
	var chatID string
	if env.DataMessage.GroupInfo != nil && env.DataMessage.GroupInfo.GroupId != "" {
		chatID = env.DataMessage.GroupInfo.GroupId
	} else {
		chatID = env.Source
	}
	if chatID == "" {
		return
	}

	// Early exit: drop messages from unknown chats unless they look like !meet-* commands
	if !isSignalAllowedChat(chatID) {
		if len(content) < 2 || content[:2] != "!m" {
			return
		}
	}

	// Deduplication by source+timestamp to handle replays after restart.
	dedupeKey := fmt.Sprintf("%s:%d", env.Source, env.DataMessage.Timestamp)
	if signalMarkSeen(dedupeKey) {
		return
	}

	log.Printf("Signal ← %s (chat=%s): %s", env.Source, chatID, content)

	// Bridge commands: only the configured owner phone number counts as "from me".
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

// signalHeartbeat sends a periodic version RPC to detect silent disconnects.
// signal-cli can drop a TCP connection without sending EOF; a write failure
// closes the conn and wakes the read loop in startSignalListener.
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

// startSignalListener connects to the signal-cli JSON-RPC daemon on
// 127.0.0.1:7583 and dispatches incoming messages. Reconnects automatically
// with exponential back-off (1s → 60s) on disconnect.
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
		backoff = time.Second // reset on successful connect

		signalConnMu.Lock()
		signalConn = conn
		signalConnMu.Unlock()

		heartbeatStop := make(chan struct{})
		go signalHeartbeat(conn, heartbeatStop)

		// Use json.Decoder (not bufio.Scanner) to avoid the 64 KiB line limit.
		decoder := json.NewDecoder(conn)
		for {
			// 90s read deadline — heartbeat keeps it alive during quiet periods.
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
				// Response to one of our send/version calls — log result or error.
				if len(msg.Error) > 0 && string(msg.Error) != "null" {
					log.Printf("Signal: RPC error id=%v: %s", msg.ID, string(msg.Error))
				}
			}
			// Heartbeat (id=0) version responses are silently ignored.
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
