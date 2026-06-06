package main

// SignalChannel implements Channel for the Signal platform.
// Signal-cli saves attachment files to disk before delivering the JSON
// message, so ReceiveAttachment only needs to resolve the local path —
// no network download required.
type SignalChannel struct{}

// NewSignalChannel constructs a SignalChannel.
func NewSignalChannel() *SignalChannel { return &SignalChannel{} }

// ID returns "signal".
func (c *SignalChannel) ID() string { return "signal" }

// ReceiveAttachment resolves the local file path for the first non-audio
// attachment in msg, returning a normalised Attachment.
// Returns (nil, nil) when the message carries no attachment.
//
// TODO: implement — migrate logic from resolveSignalImage() (to be written)
// and transcribeSignalVoice() in signal.go
func (c *SignalChannel) ReceiveAttachment(msg IncomingMessage) (*Attachment, error) {
	return nil, nil
}

// SendMessage sends text back to a Signal chat.
//
// TODO: implement — delegate to sendSignalMessage()
func (c *SignalChannel) SendMessage(chatID, text string) error {
	return nil
}

// compile-time interface check
var _ Channel = (*SignalChannel)(nil)
