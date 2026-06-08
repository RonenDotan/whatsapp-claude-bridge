package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CodexLLM implements LLM using the Codex CLI (`codex exec …`).
// Session state is stored in codex_sessions.json alongside Claude's sessions.json.
type CodexLLM struct{}

// NewCodexLLM constructs a CodexLLM.
func NewCodexLLM() *CodexLLM { return &CodexLLM{} }

// ID returns "codex".
func (l *CodexLLM) ID() string { return "codex" }

// Process delegates to handleWithCodex and captures the reply as a return value.
func (l *CodexLLM) Process(chatID, text string) (string, error) {
	var result string
	var callErr error
	handleWithCodex(chatID, text, func(reply string) {
		if strings.HasPrefix(reply, "⚠️") {
			callErr = fmt.Errorf("%s", reply)
		} else {
			result = reply
		}
	}, func(_ string) {})
	return result, callErr
}

// ProcessWithAttachment sends a message together with an image file to Codex.
// Codex CLI supports PNG, JPEG, GIF, and WebP via the --image flag.
// Non-image MIME types return a graceful error.
func (l *CodexLLM) ProcessWithAttachment(chatID, text string, att *Attachment) (string, error) {
	return l.processWithAttachment(chatID, text, att, false)
}

func (l *CodexLLM) processWithAttachment(chatID, text string, att *Attachment, retried bool) (string, error) {
	// Codex supports image/* types only.
	if !strings.HasPrefix(att.MimeType, "image/") {
		return "", fmt.Errorf("codex: attachment type %q not yet supported", att.MimeType)
	}

	sessions := loadCodexSessions()
	sessionID := sessions[chatID]

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("codex_reply_%d.txt", time.Now().UnixNano()))
	defer os.Remove(tmpFile)

	messageText := text
	if messageText == "" {
		messageText = "What is in this image?"
	}

	// When --image is used, codex reads the prompt from stdin (not positional arg).
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
			"--dangerously-bypass-approvals-and-sandbox",
			"-s", "workspace-write",
			"--image", att.LocalPath,
		}
	}

	chatDirPath, dirErr := ensureChatDir(chatID)
	if dirErr != nil {
		log.Printf("CodexLLM.ProcessWithAttachment: failed to create chat dir for %s: %v", chatID, dirErr)
		chatDirPath = storeDir()
	}
	if abs, err := filepath.Abs(chatDirPath); err == nil {
		chatDirPath = abs
	}

	// Write personality prompt to AGENTS.md on first session.
	if sessionID == "" {
		agentsMdPath := filepath.Join(chatDirPath, "AGENTS.md")
		if _, statErr := os.Stat(agentsMdPath); os.IsNotExist(statErr) {
			if prompt := strings.TrimRight(getPersonalityPrompt(chatID), "\n"); prompt != "" {
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

		// Stale session — retry fresh (without resume), but only once.
		if sessionID != "" && !retried {
			deleteCodexSession(chatID)
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

	return replyText, nil
}

// compile-time interface check
var _ LLM = (*CodexLLM)(nil)
