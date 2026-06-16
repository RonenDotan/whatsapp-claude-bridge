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

// WhatsAppChannel handles media download for the WhatsApp platform.
type WhatsAppChannel struct {
	client       *whatsmeow.Client
	messageStore *MessageStore
}

// NewWhatsAppChannel constructs a WhatsAppChannel.
func NewWhatsAppChannel(client *whatsmeow.Client, store *MessageStore) *WhatsAppChannel {
	return &WhatsAppChannel{client: client, messageStore: store}
}

// downloadFromProto downloads media directly from the WhatsApp proto message
// using whatsmeow's DownloadAny, which handles CDN authentication correctly.
// Returns (nil, nil) for message types without downloadable attachments.
func (c *WhatsAppChannel) downloadFromProto(protoMsg *waProto.Message, chatID, caption string) (*core.Attachment, error) {
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
		mimeType = protoMsg.GetDocumentMessage().GetMimetype()
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
	default:
		return nil, nil
	}

	data, err := c.client.DownloadAny(context.Background(), protoMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", mediaType, err)
	}

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
