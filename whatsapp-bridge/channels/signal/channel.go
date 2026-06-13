package signal

import (
	"os"
	"path/filepath"
	"strings"

	"whatsapp-client/core"
)

// SignalChannel implements core.Channel for the Signal platform.
type SignalChannel struct{}

func NewSignalChannel() *SignalChannel { return &SignalChannel{} }
func (c *SignalChannel) ID() string    { return "signal" }

func (c *SignalChannel) ReceiveAttachment(msg core.IncomingMessage) (*core.Attachment, error) {
	attachments, ok := msg.RawData.([]signalAttachment)
	if !ok || len(attachments) == 0 {
		return nil, nil
	}
	for _, a := range attachments {
		ct := strings.ToLower(a.ContentType)
		if strings.HasPrefix(ct, "audio/") || a.VoiceNote {
			continue
		}
		path := resolveSignalAttachmentPath(a)
		if path == "" {
			continue
		}
		return &core.Attachment{
			LocalPath: path,
			MimeType:  ct,
			Caption:   msg.Text,
		}, nil
	}
	return nil, nil
}

func (c *SignalChannel) SendMessage(chatID, text string) error {
	sendSignalMessage(chatID, text)
	return nil
}

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
		for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".pdf", ".txt"} {
			candidate := base + ext
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return ""
}

var _ core.Channel = (*SignalChannel)(nil)
