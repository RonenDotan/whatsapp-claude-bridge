package main

import (
	"os"
	"path/filepath"
	"strings"
)

// SignalChannel implements Channel for the Signal platform.
// Signal-cli saves attachment files to disk before delivering the JSON
// message, so ReceiveAttachment only needs to resolve the local path —
// no network download required.
type SignalChannel struct{}

// NewSignalChannel constructs a SignalChannel.
func NewSignalChannel() *SignalChannel { return &SignalChannel{} }

// ID returns "signal".
func (c *SignalChannel) ID() string { return "signal" }

// ReceiveAttachment resolves the local path for the first non-audio attachment
// carried in msg.RawData (expected type: []signalAttachment).
// Returns (nil, nil) when there is no attachment or all attachments are audio.
func (c *SignalChannel) ReceiveAttachment(msg IncomingMessage) (*Attachment, error) {
	attachments, ok := msg.RawData.([]signalAttachment)
	if !ok || len(attachments) == 0 {
		return nil, nil
	}
	for _, a := range attachments {
		ct := strings.ToLower(a.ContentType)
		// Audio is transcribed separately via Whisper — skip it here.
		if strings.HasPrefix(ct, "audio/") || a.VoiceNote {
			continue
		}
		path := resolveSignalAttachmentPath(a)
		if path == "" {
			continue
		}
		return &Attachment{
			LocalPath: path,
			MimeType:  ct,
			Caption:   msg.Text,
		}, nil
	}
	return nil, nil
}

// SendMessage sends text back to a Signal chat.
func (c *SignalChannel) SendMessage(chatID, text string) error {
	sendSignalMessage(chatID, text)
	return nil
}

// resolveSignalAttachmentPath returns the absolute local path for a Signal
// attachment. Signal-cli saves files to signalAttachmentsDir before delivering
// the message envelope, so we only need to find the right filename.
func resolveSignalAttachmentPath(a signalAttachment) string {
	switch {
	case filepath.IsAbs(a.Filename):
		if _, err := os.Stat(a.Filename); err == nil {
			return a.Filename
		}
	case a.Filename != "":
		p := filepath.Join(signalAttachmentsDir, a.Filename)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	case a.ID != "":
		base := filepath.Join(signalAttachmentsDir, a.ID)
		if _, err := os.Stat(base); err == nil {
			return base
		}
		// Try common image/document extensions.
		for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".pdf", ".txt"} {
			if candidate := base + ext; func() bool { _, e := os.Stat(candidate); return e == nil }() {
				return candidate
			}
		}
	}
	return ""
}

// compile-time interface check
var _ Channel = (*SignalChannel)(nil)
