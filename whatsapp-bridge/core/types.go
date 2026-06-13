package core

// IncomingMessage is the normalised representation of a message received from any channel.
type IncomingMessage struct {
	ChatID    string
	SenderID  string
	Text      string
	IsFromMe  bool
	MessageID string
	RawData   interface{}
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
