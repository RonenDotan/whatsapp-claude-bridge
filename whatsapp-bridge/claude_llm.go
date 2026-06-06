package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
)

// ClaudeLLM implements LLM using the Claude CLI (`claude -p …`).
// It supports plain-text messages and image attachments via the --image flag.
type ClaudeLLM struct{}

// NewClaudeLLM constructs a ClaudeLLM.
func NewClaudeLLM() *ClaudeLLM { return &ClaudeLLM{} }

// ID returns "claude".
func (l *ClaudeLLM) ID() string { return "claude" }

// Process delegates to handleWithClaude and captures the reply as a return value.
func (l *ClaudeLLM) Process(chatID, text string) (string, error) {
	var result string
	var callErr error
	handleWithClaude(chatID, text, func(reply string) {
		if strings.HasPrefix(reply, "⚠️") {
			callErr = fmt.Errorf("%s", strings.TrimPrefix(reply, "⚠️ "))
		} else {
			result = reply
		}
	})
	return result, callErr
}

// ProcessWithAttachment sends a message together with a file attachment to Claude.
// Images are passed via --image <path>. Non-image MIME types are not yet supported
// and return a graceful error so the caller can fall back.
func (l *ClaudeLLM) ProcessWithAttachment(chatID, text string, att *Attachment) (string, error) {
	if !strings.HasPrefix(att.MimeType, "image/") {
		return "", fmt.Errorf("claude: attachment type %q not yet supported", att.MimeType)
	}

	sessions := loadSessions()
	sessionID, hasSession := sessions[chatID]
	isNewSession := !hasSession || sessionID == ""

	chatDirPath, dirErr := ensureChatDir(chatID)
	if dirErr != nil {
		log.Printf("ClaudeLLM.ProcessWithAttachment: failed to create chat dir for %s: %v", chatID, dirErr)
		chatDirPath = storeDir()
	}
	if abs, err := filepath.Abs(chatDirPath); err == nil {
		chatDirPath = abs
	}

	// Claude CLI requires non-empty stdin even when an image is the primary input.
	messageText := text
	if messageText == "" {
		messageText = " "
	}
	if isNewSession {
		if prompt := strings.TrimRight(getPersonalityPrompt(chatID), "\n"); prompt != "" {
			messageText = prompt + "\n\n" + messageText
		}
	}

	buildArgs := func(resume string) []string {
		args := []string{"-p", "--output-format", "json", "--image", att.LocalPath}
		if resume != "" {
			args = append(args, "--resume", resume)
		}
		return args
	}

	runClaude := func(args []string) ([]byte, error) {
		c := exec.Command("claude", args...)
		c.Dir = chatDirPath
		c.Stdin = strings.NewReader(messageText)
		return c.Output()
	}

	out, err := runClaude(buildArgs(sessionID))
	if err != nil && hasSession && sessionID != "" {
		// Stale session — retry fresh.
		log.Printf("ClaudeLLM.ProcessWithAttachment: resume failed for %s, retrying fresh: %v", chatID, err)
		deleteSession(chatID)
		out, err = runClaude(buildArgs(""))
	}
	if err != nil {
		errMsg := err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			errMsg = strings.TrimSpace(string(exitErr.Stderr))
		}
		return "", fmt.Errorf("claude: %s", errMsg)
	}

	var resp struct {
		Result    string `json:"result"`
		SessionID string `json:"session_id"`
		IsError   bool   `json:"is_error"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", fmt.Errorf("claude: parse error: %w", err)
	}
	if resp.IsError {
		return "", fmt.Errorf("claude: %s", resp.Result)
	}
	if resp.SessionID != "" {
		saveSession(chatID, resp.SessionID)
	}
	return resp.Result, nil
}

// compile-time interface check
var _ LLM = (*ClaudeLLM)(nil)
