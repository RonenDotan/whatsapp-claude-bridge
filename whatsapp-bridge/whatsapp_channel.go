package main

import "go.mau.fi/whatsmeow"

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
// Returns (nil, nil) when the message carries no attachment.
//
// TODO: implement — migrate logic from downloadMedia() in whatsapp.go
func (c *WhatsAppChannel) ReceiveAttachment(msg IncomingMessage) (*Attachment, error) {
	return nil, nil
}

// SendMessage sends text back to a WhatsApp chat.
//
// TODO: implement — delegate to sendWhatsAppMessage()
func (c *WhatsAppChannel) SendMessage(chatID, text string) error {
	return nil
}

// compile-time interface check
var _ Channel = (*WhatsAppChannel)(nil)
