package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// storeDir returns the "store" directory next to the executable.
// Only whatsapp.db and messages.db live here (fixed location expected by whatsapp-mcp-server).
func storeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "store"
	}
	return filepath.Join(filepath.Dir(exe), "store")
}

// dataDir returns the runtime data directory for all user state (sessions, allowed chats,
// per-chat dirs, etc.). Reads WHATSAPP_BRIDGE_DATA_DIR env var; defaults to a bridge-data/
// sibling folder next to the bridge directory.
func dataDir() string {
	if d := os.Getenv("WHATSAPP_BRIDGE_DATA_DIR"); d != "" {
		return d
	}
	return filepath.Join(filepath.Dir(bridgeDir()), "bridge-data")
}

// configDir returns the config directory next to the bridge executable (committed to git).
func configDir() string {
	return filepath.Join(bridgeDir(), "config")
}

func sanitizeChatID(chatID string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, chatID)
}

func chatDir(chatID string) string {
	return filepath.Join(dataDir(), "chats", sanitizeChatID(chatID))
}

func ensureChatDir(chatID string) (string, error) {
	dir := chatDir(chatID)
	return dir, os.MkdirAll(dir, 0755)
}

// bridgeDir returns the directory containing the bridge executable.
func bridgeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

// ensureChatClaudeSettings copies the per-session settings template into
// <chatDir>/.claude/settings.local.json if it does not already exist.
// This allows Claude Code to pick up per-chat settings rather than the
// bridge-root .claude/settings.local.json.
func ensureChatClaudeSettings(chatID string) {
	claudeSubdir := filepath.Join(chatDir(chatID), ".claude")
	if err := os.MkdirAll(claudeSubdir, 0755); err != nil {
		log.Printf("ensureChatClaudeSettings: failed to create .claude dir for %s: %v", chatID, err)
		return
	}
	target := filepath.Join(claudeSubdir, "settings.local.json")
	if _, err := os.Stat(target); err == nil {
		return // already exists — don't overwrite custom settings
	}
	tmpl := filepath.Join(bridgeDir(), ".claude", "templates", "settings.local.json")
	data, err := os.ReadFile(tmpl)
	if err != nil {
		log.Printf("ensureChatClaudeSettings: template not found at %s: %v", tmpl, err)
		return
	}
	if err := os.WriteFile(target, data, 0644); err != nil {
		log.Printf("ensureChatClaudeSettings: failed to write %s: %v", target, err)
	}
}

const defaultAllowedChat = "120363409956054412@g.us"
const codexGroupJID = "120363407895179577@g.us"

// ─── Session management ───────────────────────────────────────────────────────

var (
	sessionsMu        sync.Mutex
	sessionsFile      = filepath.Join(dataDir(), "sessions.json")
	codexSessionsFile = filepath.Join(dataDir(), "codex_sessions.json")
)

func loadSessions() map[string]string {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	data, err := os.ReadFile(sessionsFile)
	if err != nil {
		return make(map[string]string)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return make(map[string]string)
	}
	return m
}

func saveSession(jid, sessionID string) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	m := make(map[string]string)
	if data, err := os.ReadFile(sessionsFile); err == nil {
		json.Unmarshal(data, &m)
	}
	m[jid] = sessionID
	data, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(sessionsFile, data, 0644)
}

func loadCodexSessions() map[string]string {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	data, err := os.ReadFile(codexSessionsFile)
	if err != nil {
		return make(map[string]string)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return make(map[string]string)
	}
	return m
}

func saveCodexSession(jid, threadID string) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	m := make(map[string]string)
	if data, err := os.ReadFile(codexSessionsFile); err == nil {
		json.Unmarshal(data, &m)
	}
	m[jid] = threadID
	data, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(codexSessionsFile, data, 0644)
}

func deleteSession(jid string) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	m := make(map[string]string)
	if data, err := os.ReadFile(sessionsFile); err == nil {
		json.Unmarshal(data, &m)
	}
	delete(m, jid)
	data, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(sessionsFile, data, 0644)
}

func deleteCodexSession(jid string) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	m := make(map[string]string)
	if data, err := os.ReadFile(codexSessionsFile); err == nil {
		json.Unmarshal(data, &m)
	}
	delete(m, jid)
	data, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(codexSessionsFile, data, 0644)
}

func clearSessionData(chatID string) {
	deleteSession(chatID)
	deleteCodexSession(chatID)
	inputHistoryMu.Lock()
	delete(inputHistory, chatID)
	inputHistoryMu.Unlock()
}

// ─── Input history & loop detection ──────────────────────────────────────────

var (
	inputHistoryMu sync.Mutex
	inputHistory   = make(map[string][]string)
)

func addToInputHistory(chatJID, msg string) {
	inputHistoryMu.Lock()
	defer inputHistoryMu.Unlock()
	h := inputHistory[chatJID]
	h = append(h, msg)
	if len(h) > 5 {
		h = h[len(h)-5:]
	}
	inputHistory[chatJID] = h
}

func normalizeForSimilarity(s string) []string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return r
		}
		return ' '
	}, s)
	return strings.Fields(s)
}

func isSimilar(a, b string) bool {
	wordsA := normalizeForSimilarity(a)
	wordsB := normalizeForSimilarity(b)
	if len(wordsA) == 0 && len(wordsB) == 0 {
		return true
	}
	setA := make(map[string]bool, len(wordsA))
	for _, w := range wordsA {
		setA[w] = true
	}
	setB := make(map[string]bool, len(wordsB))
	for _, w := range wordsB {
		setB[w] = true
	}
	intersection := 0
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return true
	}
	return float64(intersection)/float64(union) >= 0.80
}

func isLooping(chatJID, newMsg string) bool {
	inputHistoryMu.Lock()
	defer inputHistoryMu.Unlock()
	history := inputHistory[chatJID]
	similar := 0
	for _, prev := range history {
		if isSimilar(prev, newMsg) {
			similar++
		}
	}
	return similar >= 2
}

// ─── Usage stats ──────────────────────────────────────────────────────────────

type ModelUsageEntry struct {
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	CacheReadTokens int     `json:"cache_read_input_tokens"`
	CostUSD         float64 `json:"cost_usd"`
}

type UsageStats struct {
	CacheReadTokens  int                        `json:"cache_read_input_tokens"`
	CacheWriteTokens int                        `json:"cache_creation_input_tokens"`
	InputTokens      int                        `json:"input_tokens"`
	OutputTokens     int                        `json:"output_tokens"`
	TotalCostUSD     float64                    `json:"total_cost_usd"`
	DurationMs       int                        `json:"duration_ms"`
	ModelUsage       map[string]ModelUsageEntry `json:"model_usage,omitempty"`
	LastUpdated      string
}

type CodexStats struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	LastUpdated  string
}

var (
	usageStatsMu  sync.Mutex
	usageStatsMap = make(map[string]UsageStats)
	codexStatsMu  sync.Mutex
	codexStatsMap = make(map[string]CodexStats)
)

// ─── Transcription ────────────────────────────────────────────────────────────

// transcribeAudio runs whisper on the given audio file and returns the transcript.
// Cleans up both the input file and the generated .txt after reading.
func transcribeAudio(filePath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tmpDir := os.TempDir()
	cmd := exec.CommandContext(ctx, "whisper", filePath,
		"--model", "base",
		"--output_format", "txt",
		"--output_dir", tmpDir,
	)
	cmd.Env = append(os.Environ(), "PYTHONUTF8=1")

	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("whisper failed: %w\noutput: %s", err, string(out))
	}

	base := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	txtPath := filepath.Join(tmpDir, base+".txt")
	defer os.Remove(filePath)
	defer os.Remove(txtPath)

	data, err := os.ReadFile(txtPath)
	if err != nil {
		return "", fmt.Errorf("failed to read transcript: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}

// ─── Recent message cache (for reaction lookups) ─────────────────────────────
//
// Two-tier design:
//   Tier 1 (hot)  — in-memory, last 20 messages per chat. Fast O(n) scan.
//   Tier 2 (cold) — file-backed, last 500 messages per chat.
//                   Persists across bridge restarts and survives cache eviction.
//                   File: store/chats/<chatID>/message_cache.json
//                   Lazy-loaded on first access per chat per session.

const (
	recentMessageCacheSize = 20
	persistedCacheSize     = 500
)

type cachedMessage struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

var (
	recentMsgMu     sync.Mutex
	recentMsgCache  = map[string][]cachedMessage{} // chatID -> last 20 (hot)
	fileMsgCache    = map[string][]cachedMessage{} // chatID -> last 500 (cold, persisted)
	fileCacheLoaded = map[string]bool{}             // true once file loaded for chatID
)

// chatCacheFile returns the path to store/chats/<chatID>/message_cache.json,
// creating the directory if needed.
func chatCacheFile(chatID string) string {
	dir := filepath.Join(dataDir(), "chats", chatID)
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "message_cache.json")
}

// ensureFileCacheLoaded loads the persisted cache from disk the first time a
// chatID is accessed. Must be called with recentMsgMu held.
func ensureFileCacheLoaded(chatID string) {
	if fileCacheLoaded[chatID] {
		return
	}
	fileCacheLoaded[chatID] = true
	data, err := os.ReadFile(chatCacheFile(chatID))
	if err != nil {
		fileMsgCache[chatID] = []cachedMessage{}
		return
	}
	var msgs []cachedMessage
	if err := json.Unmarshal(data, &msgs); err != nil {
		log.Printf("[cache] parse error for %s: %v", chatID, err)
		fileMsgCache[chatID] = []cachedMessage{}
		return
	}
	fileMsgCache[chatID] = msgs
	log.Printf("[cache] loaded %d persisted messages for chat %s", len(msgs), chatID)
}

// writeCacheFile persists msgs to disk. Must be called with recentMsgMu held.
func writeCacheFile(chatID string, msgs []cachedMessage) {
	data, err := json.Marshal(msgs)
	if err != nil {
		log.Printf("[cache] marshal error for %s: %v", chatID, err)
		return
	}
	if err := os.WriteFile(chatCacheFile(chatID), data, 0o644); err != nil {
		log.Printf("[cache] write error for %s: %v", chatID, err)
	}
}

// StoreRecentMessage saves a message to both the hot in-memory cache and the
// persistent file cache. Only call for real user/LLM text, not system responses.
func StoreRecentMessage(chatID, msgID, text string) {
	if text == "" || msgID == "" {
		return
	}
	recentMsgMu.Lock()
	defer recentMsgMu.Unlock()
	log.Printf("[cache] store chat=%s msgID=%q text=%q", chatID, msgID, text)

	// Tier 1: hot in-memory cache (last 20)
	hot := recentMsgCache[chatID]
	hot = append(hot, cachedMessage{ID: msgID, Text: text})
	if len(hot) > recentMessageCacheSize {
		hot = hot[len(hot)-recentMessageCacheSize:]
	}
	recentMsgCache[chatID] = hot

	// Tier 2: persisted file cache (last 500)
	ensureFileCacheLoaded(chatID)
	cold := fileMsgCache[chatID]
	cold = append(cold, cachedMessage{ID: msgID, Text: text})
	if len(cold) > persistedCacheSize {
		cold = cold[len(cold)-persistedCacheSize:]
	}
	fileMsgCache[chatID] = cold
	writeCacheFile(chatID, cold)
}

// LookupRecentMessage finds a message by ID. Checks hot cache first, then the
// persisted file cache (loaded lazily on first miss per chat per session).
func LookupRecentMessage(chatID, msgID string) (string, bool) {
	recentMsgMu.Lock()
	defer recentMsgMu.Unlock()

	// Tier 1: hot cache
	for _, m := range recentMsgCache[chatID] {
		if m.ID == msgID {
			return m.Text, true
		}
	}

	// Tier 2: persisted cache
	ensureFileCacheLoaded(chatID)
	for _, m := range fileMsgCache[chatID] {
		if m.ID == msgID {
			return m.Text, true
		}
	}
	return "", false
}

// ─── Running process management (cancel / timeout / busy) ────────────────────

var (
	runningCancelsMu sync.Mutex
	runningCancels   = map[string]context.CancelFunc{}
)

// setRunningCancel registers a cancel func for chatID.
// Returns false if a process is already running for that chat.
func setRunningCancel(chatID string, cancel context.CancelFunc) bool {
	runningCancelsMu.Lock()
	defer runningCancelsMu.Unlock()
	if _, busy := runningCancels[chatID]; busy {
		return false
	}
	runningCancels[chatID] = cancel
	return true
}

// clearRunningCancel removes the cancel entry for chatID (called on completion).
func clearRunningCancel(chatID string) {
	runningCancelsMu.Lock()
	defer runningCancelsMu.Unlock()
	delete(runningCancels, chatID)
}

// CancelRunning kills the running process for chatID.
// Returns true if there was something to cancel.
func CancelRunning(chatID string) bool {
	runningCancelsMu.Lock()
	defer runningCancelsMu.Unlock()
	cancel, ok := runningCancels[chatID]
	if ok {
		cancel()
		delete(runningCancels, chatID)
	}
	return ok
}

// ─── WhatsApp whitelist management ───────────────────────────────────────────

var (
	allowedChatsFile      = filepath.Join(dataDir(), "allowed_chats.json")
	codexAllowedChatsFile = filepath.Join(dataDir(), "codex_allowed_chats.json")
	allowedChats          map[string]struct{}
	allowedChatsMu        sync.RWMutex
	codexAllowedChats     map[string]struct{}
	codexAllowedChatsMu   sync.RWMutex
)

func loadAllowedChats() map[string]struct{} {
	data, err := os.ReadFile(allowedChatsFile)
	if err != nil {
		return map[string]struct{}{defaultAllowedChat: {}}
	}
	var jids []string
	if err := json.Unmarshal(data, &jids); err != nil || len(jids) == 0 {
		return map[string]struct{}{defaultAllowedChat: {}}
	}
	m := make(map[string]struct{}, len(jids))
	for _, j := range jids {
		m[j] = struct{}{}
	}
	return m
}

func isAllowedChat(jid string) bool {
	allowedChatsMu.RLock()
	_, ok := allowedChats[jid]
	allowedChatsMu.RUnlock()
	if ok {
		return true
	}
	codexAllowedChatsMu.RLock()
	_, ok = codexAllowedChats[jid]
	codexAllowedChatsMu.RUnlock()
	return ok
}

func isCodexChat(jid string) bool {
	codexAllowedChatsMu.RLock()
	defer codexAllowedChatsMu.RUnlock()
	_, ok := codexAllowedChats[jid]
	return ok
}

func initAllowedChats() {
	dir := dataDir()
	os.MkdirAll(dir, 0755)
	if _, err := os.Stat(allowedChatsFile); os.IsNotExist(err) {
		data, _ := json.MarshalIndent([]string{defaultAllowedChat}, "", "  ")
		os.WriteFile(allowedChatsFile, data, 0644)
	}
	allowedChatsMu.Lock()
	allowedChats = loadAllowedChats()
	allowedChatsMu.Unlock()
}

func saveAllowedChats() error {
	allowedChatsMu.RLock()
	jids := make([]string, 0, len(allowedChats))
	for jid := range allowedChats {
		jids = append(jids, jid)
	}
	allowedChatsMu.RUnlock()
	data, err := json.MarshalIndent(jids, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(allowedChatsFile, data, 0644)
}

func loadCodexAllowedChats() map[string]struct{} {
	data, err := os.ReadFile(codexAllowedChatsFile)
	if err != nil {
		return map[string]struct{}{codexGroupJID: {}}
	}
	var jids []string
	if err := json.Unmarshal(data, &jids); err != nil || len(jids) == 0 {
		return map[string]struct{}{codexGroupJID: {}}
	}
	m := make(map[string]struct{}, len(jids))
	for _, j := range jids {
		m[j] = struct{}{}
	}
	return m
}

func initCodexAllowedChats() {
	dir := dataDir()
	os.MkdirAll(dir, 0755)
	if _, err := os.Stat(codexAllowedChatsFile); os.IsNotExist(err) {
		data, _ := json.MarshalIndent([]string{codexGroupJID}, "", "  ")
		os.WriteFile(codexAllowedChatsFile, data, 0644)
	}
	codexAllowedChatsMu.Lock()
	codexAllowedChats = loadCodexAllowedChats()
	codexAllowedChatsMu.Unlock()
}

func saveCodexAllowedChats() error {
	codexAllowedChatsMu.RLock()
	jids := make([]string, 0, len(codexAllowedChats))
	for jid := range codexAllowedChats {
		jids = append(jids, jid)
	}
	codexAllowedChatsMu.RUnlock()
	data, err := json.MarshalIndent(jids, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(codexAllowedChatsFile, data, 0644)
}

// ─── Signal whitelist management ─────────────────────────────────────────────

var (
	signalAllowedChatsFile = filepath.Join(dataDir(), "signal_allowed_chats.json")
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
	os.MkdirAll(dataDir(), 0755)
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

var (
	signalCodexAllowedChatsFile = filepath.Join(dataDir(), "signal_codex_allowed_chats.json")
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
	os.MkdirAll(dataDir(), 0755)
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

// ─── Codex output helpers ─────────────────────────────────────────────────────

func extractCodexReply(output string) string {
	marker := "tokens used\n"
	idx := strings.LastIndex(output, marker)
	if idx >= 0 {
		rest := output[idx+len(marker):]
		newline := strings.Index(rest, "\n")
		if newline >= 0 {
			reply := strings.TrimSpace(rest[newline+1:])
			if reply != "" {
				return reply
			}
		}
	}
	return strings.TrimSpace(output)
}

func parseCodexSessionID(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "session id: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "session id: "))
		}
	}
	return ""
}

// parseCodexJSONL scans JSONL event output from codex --json for session ID and token usage.
func parseCodexJSONL(output string) (sessionID string, inputTokens, outputTokens int) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}

		if sid, _ := ev["session_id"].(string); sid != "" {
			sessionID = sid
		}
		if typ, _ := ev["type"].(string); strings.Contains(strings.ToLower(typ), "session") {
			if sid, _ := ev["id"].(string); sid != "" {
				sessionID = sid
			}
		}
		if sess, ok := ev["session"].(map[string]interface{}); ok {
			if sid, _ := sess["id"].(string); sid != "" {
				sessionID = sid
			}
		}

		extractUsage := func(u map[string]interface{}) {
			if v, _ := u["input_tokens"].(float64); v > 0 {
				inputTokens = int(v)
			} else if v, _ := u["prompt_tokens"].(float64); v > 0 {
				inputTokens = int(v)
			}
			if v, _ := u["output_tokens"].(float64); v > 0 {
				outputTokens = int(v)
			} else if v, _ := u["completion_tokens"].(float64); v > 0 {
				outputTokens = int(v)
			}
		}
		if u, ok := ev["usage"].(map[string]interface{}); ok {
			extractUsage(u)
		}
		if resp, ok := ev["response"].(map[string]interface{}); ok {
			if u, ok := resp["usage"].(map[string]interface{}); ok {
				extractUsage(u)
			}
		}
		if data, ok := ev["data"].(map[string]interface{}); ok {
			if u, ok := data["usage"].(map[string]interface{}); ok {
				extractUsage(u)
			}
		}
	}
	return
}

// ─── Reaction prompt lookup ───────────────────────────────────────────────────

// lookupReactionPrompt loads config/reaction_prompts.json and returns the prompt
// for the given emoji with {text} substituted. Falls back to a generic prompt
// if the emoji is not in the map or the file cannot be read.
func lookupReactionPrompt(emoji, text string) string {
	path := filepath.Join(configDir(), "reaction_prompts.json")
	data, err := os.ReadFile(path)
	if err == nil {
		var m map[string]string
		if json.Unmarshal(data, &m) == nil {
			if tmpl, ok := m[emoji]; ok {
				return strings.ReplaceAll(tmpl, "{text}", text)
			}
		}
	}
	return fmt.Sprintf("User reacted with %s to your message:\n\n%s", emoji, text)
}

// ─── Claude dispatch ──────────────────────────────────────────────────────────

// handleWithClaude calls the Claude CLI and delivers the reply via sendFn.
// sendFn receives both the final reply and any error messages.
// sendMediaFn is called for each output file (image, PDF, etc.) created during the turn.
func handleWithClaude(chatID, messageText string, sendFn func(string), sendMediaFn func(string)) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if !setRunningCancel(chatID, cancel) {
		sendFn("⏳ Still working on your previous request. Send `!cancel` to stop it.")
		return
	}
	defer clearRunningCancel(chatID)

	// After 3 minutes, send a "still working" nudge.
	workingTimer := time.AfterFunc(3*time.Minute, func() {
		sendFn("⏳ Still working on this, hang tight...")
	})
	defer workingTimer.Stop()

	sessions := loadSessions()
	sessionID, hasSession := sessions[chatID]
	isNewSession := !hasSession || sessionID == ""

	chatDirPath, dirErr := ensureChatDir(chatID)
	if dirErr != nil {
		log.Printf("handleWithClaude: failed to create chat dir for %s: %v", chatID, dirErr)
		chatDirPath = dataDir()
	}
	if abs, err := filepath.Abs(chatDirPath); err == nil {
		chatDirPath = abs
	}

	args := []string{"-p", "--output-format", "json"}
	if hasSession && sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	if isNewSession {
		if prompt := strings.TrimRight(getPersonalityPrompt(chatID), "\n"); prompt != "" {
			messageText = prompt + "\n\n" + messageText
		}
	}

	tracker := NewSnapshotTracker(chatDirPath)
	tracker.Snapshot() // stage 1: record timestamp before LLM runs

	runClaude := func(a []string) ([]byte, error) {
		c := exec.CommandContext(ctx, "claude", a...)
		c.Dir = chatDirPath
		c.Stdin = strings.NewReader(messageText)
		return c.Output()
	}

	out, err := runClaude(args)
	if err != nil && hasSession && sessionID != "" {
		// Session may be stale (created under a different working dir) — drop and retry fresh.
		// But don't retry if we were cancelled or timed out.
		if ctx.Err() != nil {
			goto handleErr
		}
		log.Printf("Claude CLI resume failed for %s (session %s), retrying fresh: %v", chatID, sessionID, err)
		deleteSession(chatID)
		freshArgs := []string{"-p", "--output-format", "json"}
		claudeMdPath := filepath.Join(chatDirPath, "CLAUDE.md")
		if _, statErr := os.Stat(claudeMdPath); os.IsNotExist(statErr) {
			if prompt := strings.TrimRight(getPersonalityPrompt(chatID), "\n"); prompt != "" {
				_ = os.WriteFile(claudeMdPath, []byte(prompt+"\n"), 0644)
			}
		}
		out, err = runClaude(freshArgs)
	}
handleErr:
	if err != nil {
		switch ctx.Err() {
		case context.DeadlineExceeded:
			sendFn("⏱ Timed out after 10 minutes.")
			return
		case context.Canceled:
			return // user cancelled; !cancel already sent the reply
		}
		errMsg := err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			stderrStr := strings.TrimSpace(string(exitErr.Stderr))
			log.Printf("Claude CLI stderr for %s: %s", chatID, stderrStr)
			errMsg = stderrStr
		}
		log.Printf("Claude CLI error for %s: %v", chatID, err)
		sendFn("⚠️ Claude unreachable: " + errMsg)
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
		log.Printf("Failed to parse Claude response: %v\nOutput: %s", err, string(out))
		sendFn("⚠️ Bridge parse error: " + err.Error())
		return
	}
	if resp.IsError {
		log.Printf("Claude returned error for %s: %s", chatID, resp.Result)
		sendFn("⚠️ Claude error: " + resp.Result)
		return
	}
	if resp.Result == "" {
		log.Printf("Claude returned empty result for %s", chatID)
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

	sendFn(resp.Result)

	// stage 2: detect files created/modified during the LLM turn and deliver them
	if files, err := tracker.Snapshot(); err == nil {
		for _, path := range files {
			fmt.Printf("Delivering output file to %s: %s\n", chatID, path)
			sendMediaFn(path)
		}
	}
}

// ─── Codex dispatch ───────────────────────────────────────────────────────────

// handleWithCodex calls the Codex CLI and delivers the reply via sendFn.
// sendMediaFn is called for each output file created during the turn.
func handleWithCodex(chatID, messageText string, sendFn func(string), sendMediaFn func(string)) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if !setRunningCancel(chatID, cancel) {
		sendFn("⏳ Still working on your previous request. Send `!cancel` to stop it.")
		return
	}
	defer clearRunningCancel(chatID)

	// After 3 minutes, send a "still working" nudge.
	workingTimer := time.AfterFunc(3*time.Minute, func() {
		sendFn("⏳ Still working on this, hang tight...")
	})
	defer workingTimer.Stop()

	sessions := loadCodexSessions()
	sessionID := sessions[chatID]

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("codex_reply_%d.txt", time.Now().UnixNano()))
	defer os.Remove(tmpFile)

	// Codex receives the message as a CLI argument (not stdin like Claude).
	// On Windows, embedded newlines in a Node.js CLI argument get truncated,
	// so collapse them to spaces to ensure the full text reaches Codex.
	codexMessage := strings.ReplaceAll(messageText, "\r\n", " ")
	codexMessage = strings.ReplaceAll(codexMessage, "\n", " ")
	codexMessage = strings.TrimSpace(codexMessage)

	var args []string
	if sessionID != "" {
		args = []string{"exec", "--json", "--output-last-message", tmpFile,
			"resume", sessionID, codexMessage}
	} else {
		args = []string{"exec",
			"--json",
			"--output-last-message", tmpFile,
			"--skip-git-repo-check",
			"--dangerously-bypass-approvals-and-sandbox",
			"-s", "workspace-write",
			codexMessage}
	}

	chatDirPath, dirErr := ensureChatDir(chatID)
	if dirErr != nil {
		log.Printf("handleWithCodex: failed to create chat dir for %s: %v", chatID, dirErr)
		chatDirPath = storeDir()
	}
	if abs, err := filepath.Abs(chatDirPath); err == nil {
		chatDirPath = abs
	}

	if sessionID == "" {
		agentsMdPath := filepath.Join(chatDirPath, "AGENTS.md")
		if _, statErr := os.Stat(agentsMdPath); os.IsNotExist(statErr) {
			if prompt := strings.TrimRight(getPersonalityPrompt(chatID), "\n"); prompt != "" {
				_ = os.WriteFile(agentsMdPath, []byte(prompt+"\n"), 0644)
			}
		}
	}

	tracker := NewSnapshotTracker(chatDirPath)
	tracker.Snapshot() // stage 1: record timestamp before LLM runs

	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Dir = chatDirPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		switch ctx.Err() {
		case context.DeadlineExceeded:
			sendFn("⏱ Timed out after 10 minutes.")
			return
		case context.Canceled:
			return // user cancelled; !cancel already sent the reply
		}
		log.Printf("Codex exec error for %s: %v\nOutput: %s", chatID, err, string(out))
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			exitCode := -1
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			sendFn(fmt.Sprintf("⚠️ Codex error (exit %d): %s", exitCode, trimmed))
		} else {
			sendFn("⚠️ Codex error: " + err.Error())
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

	sendFn(replyText)

	// stage 2: detect files created/modified during the LLM turn and deliver them
	if files, err := tracker.Snapshot(); err == nil {
		for _, path := range files {
			fmt.Printf("Delivering output file to %s: %s\n", chatID, path)
			sendMediaFn(path)
		}
	}
}
