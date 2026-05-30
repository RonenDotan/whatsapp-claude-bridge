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

type signalDataMessage struct {
	Timestamp int64            `json:"timestamp"`
	Message   string           `json:"message"`
	GroupInfo *signalGroupInfo `json:"groupInfo"`
}

type signalGroupInfo struct {
	GroupId string `json:"groupId"`
	Type    string `json:"type"`
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
	switch cmd {
	case "!meet-claude", "!meet-codex", "!remove-claude", "!remove-codex":
	default:
		return false
	}
	if !isFromMe {
		sendSignalMessage(chatID, "⚠️ Only the bridge owner can use bridge commands")
		return true
	}
	switch cmd {
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
	}
	return true
}

// ─── Claude handler for Signal ────────────────────────────────────────────────

func handleWithClaudeSignal(chatID, messageText string) {
	sessions := loadSessions()
	sessionID, hasSession := sessions[chatID]

	args := []string{"-p", "--output-format", "json"}
	if hasSession && sessionID != "" {
		args = append(args, "--resume", sessionID)
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

func handleSignalMessage(env signalEnvelope) {
	// Skip sync messages — these are echoes of messages we sent from another device.
	// json.RawMessage is nil when the field is absent; []byte("null") when JSON null.
	if len(env.SyncMessage) > 0 && string(env.SyncMessage) != "null" {
		return
	}

	// Only process data messages (actual received texts).
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

	// Deduplication by source+timestamp to handle replays after restart.
	dedupeKey := fmt.Sprintf("%s:%d", env.Source, env.DataMessage.Timestamp)
	if signalMarkSeen(dedupeKey) {
		log.Printf("Signal: duplicate message from %s ts=%d, skipping", env.Source, env.DataMessage.Timestamp)
		return
	}

	log.Printf("Signal ← %s (chat=%s): %s", env.Source, chatID, content)

	// Bridge commands: only the configured owner phone number counts as "from me".
	isFromMe := signalOwnerNumber != "" && env.Source == signalOwnerNumber
	if handleSignalBridgeCommand(chatID, content, isFromMe) {
		return
	}

	if content == "" || !isSignalAllowedChat(chatID) {
		return
	}

	if strings.Contains(strings.ToLower(content), "clear session") {
		go func() {
			deleteSession(chatID)
			deleteCodexSession(chatID)
			inputHistoryMu.Lock()
			delete(inputHistory, chatID)
			inputHistoryMu.Unlock()
			sendSignalMessage(chatID, "✅ Session cleared for this chat. Next message starts fresh.")
		}()
		return
	}

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
			}
			// Other methods (e.g. version response id=0) are silently ignored.
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
