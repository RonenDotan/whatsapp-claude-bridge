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

// storeDir returns an absolute path to the "store" directory next to the executable.
func storeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "store"
	}
	return filepath.Join(filepath.Dir(exe), "store")
}

const defaultAllowedChat = "120363409956054412@g.us"
const codexGroupJID = "120363407895179577@g.us"

// ─── Session management ───────────────────────────────────────────────────────

var (
	sessionsMu        sync.Mutex
	sessionsFile      = filepath.Join(storeDir(), "sessions.json")
	codexSessionsFile = filepath.Join(storeDir(), "codex_sessions.json")
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

// ─── WhatsApp whitelist management ───────────────────────────────────────────

var (
	allowedChatsFile      = filepath.Join(storeDir(), "allowed_chats.json")
	codexAllowedChatsFile = filepath.Join(storeDir(), "codex_allowed_chats.json")
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
	dir := storeDir()
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
	dir := storeDir()
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

// ─── Claude dispatch ──────────────────────────────────────────────────────────

// handleWithClaude calls the Claude CLI and delivers the reply via sendFn.
// sendFn receives both the final reply and any error messages.
func handleWithClaude(chatID, messageText string, sendFn func(string)) {
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
		log.Printf("Claude CLI error for %s: %v", chatID, err)
		sendFn("⚠️ Claude unreachable: " + err.Error())
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

	sendFn("🤖🇫🇷\n" + resp.Result)
}

// ─── Codex dispatch ───────────────────────────────────────────────────────────

// handleWithCodex calls the Codex CLI and delivers the reply via sendFn.
func handleWithCodex(chatID, messageText string, sendFn func(string)) {
	sessions := loadCodexSessions()
	sessionID := sessions[chatID]

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("codex_reply_%d.txt", time.Now().UnixNano()))
	defer os.Remove(tmpFile)

	var args []string
	if sessionID != "" {
		args = []string{"exec", "--json", "--output-last-message", tmpFile,
			"resume", sessionID, messageText}
	} else {
		args = []string{"exec",
			"--json",
			"--output-last-message", tmpFile,
			"--skip-git-repo-check",
			"--dangerously-bypass-approvals-and-sandbox",
			"-s", "workspace-write",
			messageText}
	}

	cmd := exec.Command("codex", args...)
	cmd.Dir = filepath.Dir(storeDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
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

	sendFn("🤖⚡\n" + replyText)
}
