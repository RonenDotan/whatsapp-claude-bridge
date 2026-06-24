package codex

import (
	"context"
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

// CodexLLM implements core.LLM using the Codex CLI.
type CodexLLM struct{}

func NewCodexLLM() *CodexLLM { return &CodexLLM{} }
func (l *CodexLLM) ID() string { return "codex" }

func (l *CodexLLM) Process(chatID, text string) (string, error) {
	var result string
	var callErr error
	HandleWithCodex(chatID, text, func(reply string) {
		if strings.HasPrefix(reply, "⚠️") {
			callErr = fmt.Errorf("%s", reply)
		} else {
			result = reply
		}
	})
	return result, callErr
}

func (l *CodexLLM) ProcessWithAttachment(chatID, text string, att *core.Attachment) (string, error) {
	return l.processWithAttachment(chatID, text, att, false)
}

func (l *CodexLLM) processWithAttachment(chatID, text string, att *core.Attachment, retried bool) (string, error) {
	if !strings.HasPrefix(att.MimeType, "image/") {
		return "", fmt.Errorf("codex: attachment type %q not yet supported", att.MimeType)
	}

	sessions := core.LoadCodexSessions()
	sessionID := sessions[chatID]

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("codex_reply_%d.txt", time.Now().UnixNano()))
	defer os.Remove(tmpFile)

	messageText := text
	if messageText == "" {
		messageText = "What is in this image?"
	}

	var args []string
	if sessionID != "" {
		args = []string{
			"exec", "--json", "--output-last-message", tmpFile,
			"--image", att.LocalPath,
			"resume", sessionID,
		}
	} else {
		args = []string{
			"exec", "--json", "--output-last-message", tmpFile,
			"--skip-git-repo-check",
			"--image", att.LocalPath,
		}
	}

	chatDirPath, dirErr := core.EnsureChatDir(chatID)
	if dirErr != nil {
		log.Printf("CodexLLM.ProcessWithAttachment: failed to create chat dir for %s: %v", chatID, dirErr)
		chatDirPath = core.DataDir()
	}
	if abs, err := filepath.Abs(chatDirPath); err == nil {
		chatDirPath = abs
	}

	if sessionID == "" {
		core.EnsureChatCodexConfig(chatID)
		agentsMdPath := filepath.Join(chatDirPath, "AGENTS.md")
		if _, statErr := os.Stat(agentsMdPath); os.IsNotExist(statErr) {
			if prompt := strings.TrimRight(core.GetPersonalityPrompt(chatID), "\n"); prompt != "" {
				_ = os.WriteFile(agentsMdPath, []byte(prompt+"\n"), 0644)
			}
		}
	}

	cmd := exec.Command("codex", args...)
	cmd.Dir = chatDirPath
	cmd.Stdin = strings.NewReader(messageText)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("CodexLLM.ProcessWithAttachment: exec error for %s: %v\nOutput: %s", chatID, err, string(out))
		if sessionID != "" && !retried {
			core.DeleteCodexSession(chatID)
			return l.processWithAttachment(chatID, text, att, true)
		}
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			exitCode := -1
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			return "", fmt.Errorf("codex (exit %d): %s", exitCode, trimmed)
		}
		return "", fmt.Errorf("codex: %w", err)
	}

	rawOutput := string(out)
	newID, inputTokens, outputTokens := ParseCodexJSONL(rawOutput)

	if newID != "" {
		core.SaveCodexSession(chatID, newID)
	} else if textID := ParseCodexSessionID(rawOutput); textID != "" {
		core.SaveCodexSession(chatID, textID)
	}

	if inputTokens > 0 || outputTokens > 0 {
		core.CodexStatsMu.Lock()
		core.CodexStatsMap[chatID] = core.CodexStats{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			TotalTokens:  inputTokens + outputTokens,
			LastUpdated:  time.Now().Format("2006-01-02 15:04:05"),
		}
		core.CodexStatsMu.Unlock()
	}

	var replyText string
	if data, err := os.ReadFile(tmpFile); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		replyText = strings.TrimSpace(string(data))
	} else {
		replyText = ExtractCodexReply(rawOutput)
	}

	return replyText, nil
}

// HandleWithCodex calls the Codex CLI and delivers the reply via sendFn.
// SnapshotTracker and media delivery are owned by the caller (main).
func HandleWithCodex(chatID, messageText string, sendFn func(string)) {
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

	sessions := core.LoadCodexSessions()
	sessionID := sessions[chatID]

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("codex_reply_%d.txt", time.Now().UnixNano()))
	defer os.Remove(tmpFile)

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
			codexMessage}
	}

	chatDirPath, dirErr := core.EnsureChatDir(chatID)
	if dirErr != nil {
		log.Printf("HandleWithCodex: failed to create chat dir for %s: %v", chatID, dirErr)
		chatDirPath = core.StoreDir()
	}
	if abs, err := filepath.Abs(chatDirPath); err == nil {
		chatDirPath = abs
	}

	if sessionID == "" {
		core.EnsureChatCodexConfig(chatID)
		agentsMdPath := filepath.Join(chatDirPath, "AGENTS.md")
		if _, statErr := os.Stat(agentsMdPath); os.IsNotExist(statErr) {
			if prompt := strings.TrimRight(core.GetPersonalityPrompt(chatID), "\n"); prompt != "" {
				_ = os.WriteFile(agentsMdPath, []byte(prompt+"\n"), 0644)
			}
		}
	}

	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Dir = chatDirPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		switch ctx.Err() {
		case context.DeadlineExceeded:
			sendFn("⏱ Timed out after 10 minutes.")
			return
		case context.Canceled:
			return
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
	newID, inputTokens, outputTokens := ParseCodexJSONL(rawOutput)

	if newID != "" {
		core.SaveCodexSession(chatID, newID)
	} else if textID := ParseCodexSessionID(rawOutput); textID != "" {
		core.SaveCodexSession(chatID, textID)
	}

	if inputTokens > 0 || outputTokens > 0 {
		core.CodexStatsMu.Lock()
		core.CodexStatsMap[chatID] = core.CodexStats{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			TotalTokens:  inputTokens + outputTokens,
			LastUpdated:  time.Now().Format("2006-01-02 15:04:05"),
		}
		core.CodexStatsMu.Unlock()
	}

	var replyText string
	if data, err := os.ReadFile(tmpFile); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		replyText = strings.TrimSpace(string(data))
	} else {
		replyText = ExtractCodexReply(rawOutput)
	}

	sendFn(replyText)
}

func ExtractCodexReply(output string) string {
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

func ParseCodexSessionID(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "session id: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "session id: "))
		}
	}
	return ""
}

func ParseCodexJSONL(output string) (sessionID string, inputTokens, outputTokens int) {
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

// compile-time interface check
var _ core.LLM = (*CodexLLM)(nil)
