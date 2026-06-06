package main

import "fmt"

// CodexLLM implements LLM using the Codex CLI (`codex exec …`).
// Vision / attachment support depends on what the Codex CLI exposes —
// ProcessWithAttachment will return an unsupported error until that is
// researched and confirmed.
type CodexLLM struct{}

// NewCodexLLM constructs a CodexLLM.
func NewCodexLLM() *CodexLLM { return &CodexLLM{} }

// ID returns "codex".
func (l *CodexLLM) ID() string { return "codex" }

// Process sends a plain-text message to Codex and returns the reply.
//
// TODO: implement — migrate logic from handleWithCodex() in shared.go
func (l *CodexLLM) Process(chatID, text string) (string, error) {
	return "", nil
}

// ProcessWithAttachment is not yet implemented for Codex.
// Returns an error so callers can fall back gracefully (e.g. send the
// attachment to Claude instead, or notify the user).
//
// TODO: research Codex CLI image/file flags, then implement.
func (l *CodexLLM) ProcessWithAttachment(chatID, text string, att *Attachment) (string, error) {
	return "", fmt.Errorf("codex: attachment processing not yet supported (mime=%s)", att.MimeType)
}

// compile-time interface check
var _ LLM = (*CodexLLM)(nil)
