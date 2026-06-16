package core

// EventType classifies a parsed incoming event.
type EventType int

const (
	EventText       EventType = iota // plain text message
	EventAttachment                  // file / image / document
	EventReaction                    // emoji reaction to a previous message
	EventCommand                     // message starting with "!"
	EventNone                        // Parse handled the event internally; main should drop it
)

// Event is the fully parsed form of a RawMessage, produced by the Parse closure.
type Event struct {
	Type        EventType
	ChatID      string
	SenderID    string
	IsFromMe    bool
	Text        string      // EventText / EventCommand
	Attachment  *Attachment // EventAttachment
	Emoji       string      // EventReaction
	QuotedMsgID string      // EventReaction — ID; main resolves text from recent-message cache
}

// Attachment represents a file received via any channel.
type Attachment struct {
	LocalPath string
	MimeType  string
	Caption   string
}

// Sender abstracts reply delivery back to the originating channel.
// One instance is created per channel at startup and shared across messages.
type Sender struct {
	SendText  func(chatID, text string) string // sends text; returns msgID or ""
	SendMedia func(chatID, path string)
}

// RawMessage is the lightweight struct channels push into the inbox.
// Parsing is deferred — only called after the filter stage passes.
type RawMessage struct {
	ChatID    string
	SenderID  string
	IsFromMe  bool
	MessageID string
	TextHint  string       // raw text for plain-text messages; "" for reactions/attachments
	Sender    *Sender
	Parse     func() Event // channel-specific parsing; called only after filters pass
}

// LLM abstracts a language-model backend.
type LLM interface {
	ID() string
	Process(chatID, text string) (string, error)
	ProcessWithAttachment(chatID, text string, att *Attachment) (string, error)
}
