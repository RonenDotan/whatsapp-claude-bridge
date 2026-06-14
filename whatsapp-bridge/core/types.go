package core

// IncomingMessage is the normalised representation of a message received from any channel.
// The channel handler fills Reply/ReplyMedia/IsCodexChat before sending to the inbox.
type IncomingMessage struct {
	ChatID      string
	SenderID    string
	Text        string
	IsFromMe    bool
	MessageID   string
	IsCodexChat bool
	Attachment  *Attachment
	// Reply sends a text reply back through the originating channel.
	// Returns a stable ID (msgID or timestamp string) for cache storage, or "" if unavailable.
	Reply      func(text string) string
	ReplyMedia func(path string)
	RawData    interface{}
}

// Attachment represents a file received via any channel.
type Attachment struct {
	LocalPath string
	MimeType  string
	Caption   string
}

// Channel abstracts a messaging platform (WhatsApp, Signal, …).
type Channel interface {
	ID() string
	ReceiveAttachment(msg IncomingMessage) (*Attachment, error)
	SendMessage(chatID, text string) error
}

// LLM abstracts a language-model backend (Claude CLI, Codex CLI, …).
type LLM interface {
	ID() string
	Process(chatID, text string) (string, error)
	ProcessWithAttachment(chatID, text string, att *Attachment) (string, error)
}
