package main

// ─── Shared types ─────────────────────────────────────────────────────────────

// IncomingMessage is the normalised representation of a message received from
// any channel. Each Channel implementation is responsible for populating this
// from its own raw message format.
type IncomingMessage struct {
	ChatID    string // channel-specific chat / group identifier
	SenderID  string // channel-specific sender identifier
	Text      string // plain text body (may be empty if attachment-only)
	IsFromMe  bool   // true when the bridge itself sent this message
	MessageID string // channel-specific unique message ID (used for media lookup on WhatsApp)
	// RawData carries platform-specific attachment metadata that cannot be
	// expressed in the normalised fields above.
	// Signal: []signalAttachment  —  WhatsApp: not used (media info is in SQLite)
	RawData interface{}
}

// Attachment represents a file received via any channel. The file is always
// materialised to a local path before being handed to an LLM.
type Attachment struct {
	LocalPath string // absolute path to the downloaded / resolved file
	MimeType  string // e.g. "image/jpeg", "application/pdf"
	Caption   string // text that accompanied the attachment (may be empty)
}

// ─── Channel interface ────────────────────────────────────────────────────────

// Channel abstracts a messaging platform (WhatsApp, Signal, …).
// Each platform provides its own concrete implementation.
type Channel interface {
	// ID returns a short identifier for the channel, e.g. "whatsapp" or "signal".
	ID() string

	// ReceiveAttachment downloads / resolves the attachment contained in msg,
	// if any, and returns a normalised Attachment. Returns (nil, nil) when the
	// message carries no attachment.
	ReceiveAttachment(msg IncomingMessage) (*Attachment, error)

	// SendMessage delivers text back to the chat identified by chatID.
	SendMessage(chatID, text string) error
}

// ─── LLM interface ────────────────────────────────────────────────────────────

// LLM abstracts a language-model backend (Claude CLI, Codex CLI, …).
type LLM interface {
	// ID returns a short identifier for the backend, e.g. "claude" or "codex".
	ID() string

	// Process sends a plain-text message to the model and returns the reply.
	Process(chatID, text string) (string, error)

	// ProcessWithAttachment sends a message together with a file attachment.
	// Implementations that do not support the attachment type should return a
	// graceful error so the caller can fall back to text-only.
	ProcessWithAttachment(chatID, text string, att *Attachment) (string, error)
}
