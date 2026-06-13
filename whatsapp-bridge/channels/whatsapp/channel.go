package whatsapp

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"

	"whatsapp-client/core"
)

// WhatsAppChannel implements Channel for the WhatsApp platform.
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

// ReceiveAttachment downloads and decrypts the WhatsApp media.
// When msg.RawData is a *waProto.Message, it downloads directly from the
// proto (bypassing DB URL lookup which can 403 on CDN auth changes).
// Returns (nil, nil) for audio (handled by Whisper) or unsupported types.
func (c *WhatsAppChannel) ReceiveAttachment(msg core.IncomingMessage) (*core.Attachment, error) {
	// Fast path: download directly from the proto message to avoid 403s
	if protoMsg, ok := msg.RawData.(*waProto.Message); ok && protoMsg != nil {
		return c.downloadFromProto(protoMsg, msg.ChatID, msg.Text)
	}

	// Fallback: DB-based lookup (legacy path)
	ok, mediaType, _, localPath, err := downloadMedia(c.client, c.messageStore, msg.MessageID, msg.ChatID)
	if err != nil {
		if strings.Contains(err.Error(), "not a media message") {
			return nil, nil
		}
		return nil, err
	}
	if !ok || mediaType == "audio" {
		return nil, nil
	}
	return &core.Attachment{
		LocalPath: localPath,
		MimeType:  whatsAppMediaTypeToMime(mediaType),
		Caption:   msg.Text,
	}, nil
}

// downloadFromProto downloads the media directly from the WhatsApp proto
// message using whatsmeow's DownloadAny, which uses proper authenticated CDN.
func (c *WhatsAppChannel) downloadFromProto(protoMsg *waProto.Message, chatID, caption string) (*core.Attachment, error) {
	// Determine media type and MIME
	var mediaType, mimeType string
	switch {
	case protoMsg.GetImageMessage() != nil:
		mediaType = "image"
		mimeType = "image/jpeg"
	case protoMsg.GetVideoMessage() != nil:
		mediaType = "video"
		mimeType = "video/mp4"
	case protoMsg.GetDocumentMessage() != nil:
		mediaType = "document"
		fn := protoMsg.GetDocumentMessage().GetMimetype()
		if fn != "" {
			mimeType = fn
		} else {
			mimeType = "application/octet-stream"
		}
	default:
		return nil, nil // no downloadable attachment
	}

	// Download using whatsmeow's DownloadAny which handles CDN auth properly
	data, err := c.client.DownloadAny(context.Background(), protoMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", mediaType, err)
	}

	// Save to disk
	chatDir := fmt.Sprintf("store/%s", strings.ReplaceAll(chatID, ":", "_"))
	if err := os.MkdirAll(chatDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create chat dir: %w", err)
	}
	ext := mediaTypeToExt(mediaType)
	filename := mediaType + "_" + time.Now().Format("20060102_150405") + ext
	localPath := filepath.Join(chatDir, filename)
	if err := os.WriteFile(localPath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to save media: %w", err)
	}
	absPath, _ := filepath.Abs(localPath)
	log.Printf("Downloaded %s via DownloadAny to %s (%d bytes)", mediaType, absPath, len(data))

	return &core.Attachment{
		LocalPath: absPath,
		MimeType:  mimeType,
		Caption:   caption,
	}, nil
}

func mediaTypeToExt(mediaType string) string {
	switch mediaType {
	case "image":
		return ".jpg"
	case "video":
		return ".mp4"
	default:
		return ""
	}
}

// SendMessage sends text back to a WhatsApp chat.
func (c *WhatsAppChannel) SendMessage(chatID, text string) error {
	ok, result := sendWhatsAppMessage(c.client, chatID, text, "")
	if !ok {
		return fmt.Errorf("failed to send WhatsApp message: %s", result)
	}
	return nil
}

// whatsAppMediaTypeToMime converts the internal mediaType string.
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
var _ core.Channel = (*WhatsAppChannel)(nil)
