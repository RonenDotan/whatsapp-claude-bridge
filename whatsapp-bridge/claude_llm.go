package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ClaudeLLM implements LLM using the Claude CLI.
type ClaudeLLM struct{}

func NewClaudeLLM() *ClaudeLLM { return &ClaudeLLM{} }
func (l *ClaudeLLM) ID() string { return "claude" }

func (l *ClaudeLLM) Process(chatID, text string) (string, error) {
	var result string
	var callErr error
	handleWithClaude(chatID, text, func(reply string) {
		if strings.HasPrefix(reply, "⚠️") {
			callErr = fmt.Errorf("%s", strings.TrimPrefix(reply, "⚠️ "))
		} else {
			result = reply
		}
	}, func(_ string) {})
	return result, callErr
}

// ProcessWithAttachment sends a message with an image to Claude via stream-json input.
func (l *ClaudeLLM) ProcessWithAttachment(chatID, text string, att *Attachment) (string, error) {
	if !strings.HasPrefix(att.MimeType, "image/") {
		return "", fmt.Errorf("claude: attachment type %q not yet supported", att.MimeType)
	}

	imgData, err := os.ReadFile(att.LocalPath)
	if err != nil {
		return "", fmt.Errorf("claude: failed to read image %s: %w", att.LocalPath, err)
	}
	imgB64 := base64.StdEncoding.EncodeToString(imgData)

	sessions := loadSessions()
	sessionID, hasSession := sessions[chatID]
	isNewSession := !hasSession || sessionID == ""

	chatDirPath, dirErr := ensureChatDir(chatID)
	if dirErr != nil {
		log.Printf("ClaudeLLM.ProcessWithAttachment: failed to create chat dir for %s: %v", chatID, dirErr)
		chatDirPath = dataDir()
	}
	if abs, err := filepath.Abs(chatDirPath); err == nil {
		chatDirPath = abs
	}

	messageText := text
	if messageText == "" {
		messageText = "What is in this image?"
	}
	if isNewSession {
		if prompt := strings.TrimRight(getPersonalityPrompt(chatID), "\n"); prompt != "" {
			messageText = prompt + "\n\n" + messageText
		}
	}

	// Build stream-json message with image + text content blocks.
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

	// stream-json input requires stream-json output
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
		deleteSession(chatID)
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

	// stream-json output is newline-delimited JSON; find the "result" event
	return parseStreamJSONResult(chatID, out)
}

// parseStreamJSONResult scans newline-delimited JSON from stream-json output,
// finds the "result" event, saves the session, and returns the reply text.
func parseStreamJSONResult(chatID string, data []byte) (string, error) {
	type resultEvent struct {
		Type      string `json:"type"`
		Subtype   string `json:"subtype"`
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
				saveSession(chatID, evt.SessionID)
			}
			if evt.IsError {
				return "", fmt.Errorf("claude: %s", evt.Result)
			}
			return evt.Result, nil
		}
	}
	return "", fmt.Errorf("claude: no result in stream output")
}

// compile-time interface check
var _ LLM = (*ClaudeLLM)(nil)
