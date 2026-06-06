package main

// ClaudeLLM implements LLM using the Claude CLI (`claude -p …`).
// It supports both plain-text messages and image/file attachments via
// the --image flag.
type ClaudeLLM struct{}

// NewClaudeLLM constructs a ClaudeLLM.
func NewClaudeLLM() *ClaudeLLM { return &ClaudeLLM{} }

// ID returns "claude".
func (l *ClaudeLLM) ID() string { return "claude" }

// Process sends a plain-text message to Claude and returns the reply.
//
// TODO: implement — migrate logic from handleWithClaude() in shared.go
func (l *ClaudeLLM) Process(chatID, text string) (string, error) {
	return "", nil
}

// ProcessWithAttachment sends a message together with a local file to Claude.
// Images are passed via --image <path>; other supported types are embedded in
// the prompt text. Unsupported MIME types return a graceful error.
//
// TODO: implement — extend handleWithClaude() with --image flag support
func (l *ClaudeLLM) ProcessWithAttachment(chatID, text string, att *Attachment) (string, error) {
	return "", nil
}

// compile-time interface check
var _ LLM = (*ClaudeLLM)(nil)
