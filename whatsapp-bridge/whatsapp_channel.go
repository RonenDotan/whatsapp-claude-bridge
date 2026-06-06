package main

import (
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow"
)

// WhatsAppChannel implements Channel for the WhatsApp platform.
// It holds the whatsmeow client and the message store so it can
// download encrypted media on demand.
type WhatsAppChannel struct {
	client       *whatsmeow.Client
	messageStore *MessageStore
}

// NewWhatsAppChannel constructs a WhatsAppChannel.
func NewWhatsAppChannel(client *whatsmeow.Client, store *MessageStore) *WhatsAppChannel {
	return &WhatsAppChannel{client: client, messageStore: store}
}

// ID returns "whatsapp".
func (c *WhatsAppChannel) ID() string { return "whatsapp" }

// ReceiveAttachment downloads and decrypts the WhatsApp media referenced by
// msg.MessageID / msg.ChatID, then returns a normalised Attachment.
// Returns (nil, nil) when the message carries no attachment or the media type
// is audio (audio is handled separately via Whisper transcription).
func (c *WhatsAppChannel) ReceiveAttachment(msg IncomingMessage) (*Attachment, error) {
	ok, mediaType, _, localPath, err := downloadMedia(c.client, c.messageStore, msg.MessageID, msg.ChatID)
	if err != nil {
		// "not a media message" is not an error — the message just has no attachment.
		if strings.Contains(err.Error(), "not a media message") {
			return nil, nil
		}
		return nil, err
	}
	if !ok || mediaType == "audio" {
		// Audio is transcribed separately; skip it here.
		return nil, nil
	}
	return &Attachment{
		LocalPath: localPath,
		MimeType:  whatsAppMediaTypeToMime(mediaType),
		Caption:   msg.Text,
	}, nil
}

// SendMessage sends text back to a WhatsApp chat.
func (c *WhatsAppChannel) SendMessage(chatID, text string) error {
	ok, result := sendWhatsAppMessage(c.client, chatID, text, "")
	if !ok {
		return fmt.Errorf("failed to send WhatsApp message: %s", result)
	}
	return nil
}

// whatsAppMediaTypeToMime converts the internal mediaType string used by
// extractMediaInfo / downloadMedia into a MIME type string.
func whatsAppMediaTypeToMime(mediaType string) string {
	switch mediaType {
	case "image":
		return "image/jpeg"
	case "video":
		return "video/mp4"
	case "document":
		return "application/octet-stream"
	default:
		return "application/octet-stream"
	}
}

// compile-time interface check
var _ Channel = (*WhatsAppChannel)(nil)
