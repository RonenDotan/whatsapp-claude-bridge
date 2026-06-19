package claude

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"whatsapp-client/core"
)

// ClaudeLLM implements core.LLM using the Claude CLI.
type ClaudeLLM struct{}

func NewClaudeLLM() *ClaudeLLM { return &ClaudeLLM{} }
func (l *ClaudeLLM) ID() string { return "claude" }

func (l *ClaudeLLM) Process(chatID, text string) (string, error) {
	var result string
	var callErr error
	HandleWithClaude(chatID, text, func(reply string) {
		if strings.HasPrefix(reply, "⚠️") {
			callErr = fmt.Errorf("%s", strings.TrimPrefix(reply, "⚠️ "))
		} else {
			result = reply
		}
	})
	return result, callErr
}

func (l *ClaudeLLM) ProcessWithAttachment(chatID, text string, att *core.Attachment) (string, error) {
	if !strings.HasPrefix(att.MimeType, "image/") {
		return "", fmt.Errorf("claude: attachment type %q not yet supported", att.MimeType)
	}

	imgData, err := os.ReadFile(att.LocalPath)
	if err != nil {
		return "", fmt.Errorf("claude: failed to read image %s: %w", att.LocalPath, err)
	}
	imgB64 := base64.StdEncoding.EncodeToString(imgData)

	sessions := core.LoadSessions()
	sessionID, hasSession := sessions[chatID]
	isNewSession := !hasSession || sessionID == ""

	chatDirPath, dirErr := core.EnsureChatDir(chatID)
	if dirErr != nil {
		log.Printf("ClaudeLLM.ProcessWithAttachment: failed to create chat dir for %s: %v", chatID, dirErr)
		chatDirPath = core.DataDir()
	}
	if abs, err := filepath.Abs(chatDirPath); err == nil {
		chatDirPath = abs
	}

	messageText := text
	if messageText == "" {
		messageText = "What is in this image?"
	}
	if isNewSession {
		if prompt := strings.TrimRight(core.GetPersonalityPrompt(chatID), "\n"); prompt != "" {
			messageText = prompt + "\n\n" + messageText
		}
	}

	type imageSource struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	}
	type imageBlock struct {
		Type   string      `json:"type"`
		Source imageSource `json:"source"`
	}
	type textBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type msgContent struct {
		Role    string        `json:"role"`
		Content []interface{} `json:"content"`
	}
	type streamMsg struct {
		Type    string     `json:"type"`
		Message msgContent `json:"message"`
	}

	payload := streamMsg{
		Type: "user",
		Message: msgContent{
			Role: "user",
			Content: []interface{}{
				imageBlock{
					Type: "image",
					Source: imageSource{
						Type:      "base64",
						MediaType: att.MimeType,
						Data:      imgB64,
					},
				},
				textBlock{Type: "text", Text: messageText},
			},
		},
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("claude: marshal error: %w", err)
	}

	buildArgs := func(resume string) []string {
		args := []string{"-p", "--verbose", "--output-format", "stream-json", "--input-format", "stream-json"}
		if resume != "" {
			args = append(args, "--resume", resume)
		}
		return args
	}

	runClaude := func(args []string) ([]byte, error) {
		c := exec.Command("claude", args...)
		c.Dir = chatDirPath
		c.Stdin = strings.NewReader(string(payloadJSON) + "\n")
		return c.Output()
	}

	out, err := runClaude(buildArgs(sessionID))
	if err != nil && hasSession && sessionID != "" {
		log.Printf("ClaudeLLM.ProcessWithAttachment: resume failed for %s, retrying fresh: %v", chatID, err)
		core.DeleteSession(chatID)
		out, err = runClaude(buildArgs(""))
	}
	if err != nil {
		errMsg := err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			errMsg = strings.TrimSpace(string(exitErr.Stderr))
		}
		log.Printf("ClaudeLLM.ProcessWithAttachment error for %s: %s\nstdout: %s", chatID, errMsg, string(out))
		return "", fmt.Errorf("claude: %s", errMsg)
	}

	return ParseStreamJSONResult(chatID, out)
}

// ParseStreamJSONResult scans newline-delimited JSON from stream-json output,
// finds the "result" event, saves the session, and returns the reply text.
func ParseStreamJSONResult(chatID string, data []byte) (string, error) {
	type resultEvent struct {
		Type      string `json:"type"`
		Result    string `json:"result"`
		SessionID string `json:"session_id"`
		IsError   bool   `json:"is_error"`
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var evt resultEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		if evt.Type == "result" {
			if evt.SessionID != "" {
				core.SaveSession(chatID, evt.SessionID)
			}
			if evt.IsError {
				return "", fmt.Errorf("claude: %s", evt.Result)
			}
			return evt.Result, nil
		}
	}
	return "", fmt.Errorf("claude: no result in stream output")
}

// HandleWithClaude calls the Claude CLI and delivers the reply via sendFn.
// SnapshotTracker and media delivery are owned by the caller (main).
func HandleWithClaude(chatID, messageText string, sendFn func(string)) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if !core.SetRunningCancel(chatID, cancel) {
		sendFn("⏳ Still working on your previous request. Send `!cancel` to stop it.")
		return
	}
	defer core.ClearRunningCancel(chatID)

	workingTimer := time.AfterFunc(3*time.Minute, func() {
		sendFn("⏳ Still working on this, hang tight...")
	})
	defer workingTimer.Stop()

	sessions := core.LoadSessions()
	sessionID, hasSession := sessions[chatID]
	isNewSession := !hasSession || sessionID == ""

	chatDirPath, dirErr := core.EnsureChatDir(chatID)
	if dirErr != nil {
		log.Printf("HandleWithClaude: failed to create chat dir for %s: %v", chatID, dirErr)
		chatDirPath = core.DataDir()
	}
	if abs, err := filepath.Abs(chatDirPath); err == nil {
		chatDirPath = abs
	}

	args := []string{"-p", "--output-format", "json"}
	if hasSession && sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	if isNewSession {
		if prompt := strings.TrimRight(core.GetPersonalityPrompt(chatID), "\n"); prompt != "" {
			messageText = prompt + "\n\n" + messageText
		}
	}

	runClaude := func(a []string) ([]byte, error) {
		c := exec.CommandContext(ctx, "claude", a...)
		c.Dir = chatDirPath
		c.Stdin = strings.NewReader(messageText)
		return c.Output()
	}

	out, err := runClaude(args)
	if err != nil && hasSession && sessionID != "" {
		if ctx.Err() != nil {
			goto handleErr
		}
		log.Printf("Claude CLI resume failed for %s (session %s), retrying fresh: %v", chatID, sessionID, err)
		core.DeleteSession(chatID)
		freshArgs := []string{"-p", "--output-format", "json"}
		claudeMdPath := filepath.Join(chatDirPath, "CLAUDE.md")
		if _, statErr := os.Stat(claudeMdPath); os.IsNotExist(statErr) {
			if prompt := strings.TrimRight(core.GetPersonalityPrompt(chatID), "\n"); prompt != "" {
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
			return
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
		core.SaveSession(chatID, resp.SessionID)
	}

	modelUsage := make(map[string]core.ModelUsageEntry, len(resp.ModelUsage))
	for model, mu := range resp.ModelUsage {
		modelUsage[model] = core.ModelUsageEntry{
			InputTokens:     mu.InputTokens,
			OutputTokens:    mu.OutputTokens,
			CacheReadTokens: mu.CacheReadTokens,
			CostUSD:         mu.CostUSD,
		}
	}
	core.UsageStatsMu.Lock()
	core.UsageStatsMap[chatID] = core.UsageStats{
		CacheReadTokens:  resp.Usage.CacheReadTokens,
		CacheWriteTokens: resp.Usage.CacheWriteTokens,
		InputTokens:      resp.Usage.InputTokens,
		OutputTokens:     resp.Usage.OutputTokens,
		TotalCostUSD:     resp.Usage.TotalCostUSD,
		DurationMs:       resp.DurationMs,
		ModelUsage:       modelUsage,
		LastUpdated:      time.Now().Format("2006-01-02 15:04:05"),
	}
	core.UsageStatsMu.Unlock()

	sendFn(resp.Result)
}

// compile-time interface check
var _ core.LLM = (*ClaudeLLM)(nil)
