package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// ─── Types ────────────────────────────────────────────────────────────────────

type Message struct {
	Time      time.Time
	Sender    string
	Content   string
	IsFromMe  bool
	MediaType string
	Filename  string
}

type MessageStore struct {
	db *sql.DB
}

// ─── Message store ────────────────────────────────────────────────────────────

func NewMessageStore() (*MessageStore, error) {
	dir := storeDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create store directory: %v", err)
	}
	db, err := sql.Open("sqlite3", "file:"+filepath.Join(dir, "messages.db")+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open message database: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS chats (
			jid TEXT PRIMARY KEY,
			name TEXT,
			last_message_time TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS messages (
			id TEXT,
			chat_jid TEXT,
			sender TEXT,
			content TEXT,
			timestamp TIMESTAMP,
			is_from_me BOOLEAN,
			media_type TEXT,
			filename TEXT,
			url TEXT,
			media_key BLOB,
			file_sha256 BLOB,
			file_enc_sha256 BLOB,
			file_length INTEGER,
			PRIMARY KEY (id, chat_jid),
			FOREIGN KEY (chat_jid) REFERENCES chats(jid)
		);
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables: %v", err)
	}
	return &MessageStore{db: db}, nil
}

func (store *MessageStore) Close() error {
	return store.db.Close()
}

func (store *MessageStore) StoreChat(jid, name string, lastMessageTime time.Time) error {
	_, err := store.db.Exec(
		"INSERT OR REPLACE INTO chats (jid, name, last_message_time) VALUES (?, ?, ?)",
		jid, name, lastMessageTime,
	)
	return err
}

func (store *MessageStore) StoreMessage(id, chatJID, sender, content string, timestamp time.Time, isFromMe bool,
	mediaType, filename, url string, mediaKey, fileSHA256, fileEncSHA256 []byte, fileLength uint64) error {
	if content == "" && mediaType == "" {
		return nil
	}
	_, err := store.db.Exec(
		`INSERT OR REPLACE INTO messages
		(id, chat_jid, sender, content, timestamp, is_from_me, media_type, filename, url, media_key, file_sha256, file_enc_sha256, file_length)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, chatJID, sender, content, timestamp, isFromMe, mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength,
	)
	return err
}

func (store *MessageStore) GetMessages(chatJID string, limit int) ([]Message, error) {
	rows, err := store.db.Query(
		"SELECT sender, content, timestamp, is_from_me, media_type, filename FROM messages WHERE chat_jid = ? ORDER BY timestamp DESC LIMIT ?",
		chatJID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []Message
	for rows.Next() {
		var msg Message
		var timestamp time.Time
		err := rows.Scan(&msg.Sender, &msg.Content, &timestamp, &msg.IsFromMe, &msg.MediaType, &msg.Filename)
		if err != nil {
			return nil, err
		}
		msg.Time = timestamp
		messages = append(messages, msg)
	}
	return messages, nil
}

func (store *MessageStore) GetChats() (map[string]time.Time, error) {
	rows, err := store.db.Query("SELECT jid, last_message_time FROM chats ORDER BY last_message_time DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	chats := make(map[string]time.Time)
	for rows.Next() {
		var jid string
		var lastMessageTime time.Time
		if err := rows.Scan(&jid, &lastMessageTime); err != nil {
			return nil, err
		}
		chats[jid] = lastMessageTime
	}
	return chats, nil
}

func (store *MessageStore) StoreMediaInfo(id, chatJID, url string, mediaKey, fileSHA256, fileEncSHA256 []byte, fileLength uint64) error {
	_, err := store.db.Exec(
		"UPDATE messages SET url = ?, media_key = ?, file_sha256 = ?, file_enc_sha256 = ?, file_length = ? WHERE id = ? AND chat_jid = ?",
		url, mediaKey, fileSHA256, fileEncSHA256, fileLength, id, chatJID,
	)
	return err
}

func (store *MessageStore) GetMediaInfo(id, chatJID string) (string, string, string, []byte, []byte, []byte, uint64, error) {
	var mediaType, filename, url string
	var mediaKey, fileSHA256, fileEncSHA256 []byte
	var fileLength uint64
	err := store.db.QueryRow(
		"SELECT media_type, filename, url, media_key, file_sha256, file_enc_sha256, file_length FROM messages WHERE id = ? AND chat_jid = ?",
		id, chatJID,
	).Scan(&mediaType, &filename, &url, &mediaKey, &fileSHA256, &fileEncSHA256, &fileLength)
	return mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength, err
}

// ─── Sent-message deduplication ───────────────────────────────────────────────

var (
	sentMessagesMu sync.Mutex
	sentMessageIDs = make(map[string]struct{})
)

func markSentMessage(msgID string) {
	sentMessagesMu.Lock()
	defer sentMessagesMu.Unlock()
	sentMessageIDs[msgID] = struct{}{}
	if len(sentMessageIDs) > 100 {
		for k := range sentMessageIDs {
			delete(sentMessageIDs, k)
			break
		}
	}
}

func isSentByUs(msgID string) bool {
	sentMessagesMu.Lock()
	defer sentMessagesMu.Unlock()
	_, ok := sentMessageIDs[msgID]
	return ok
}

// ─── Message content helpers ──────────────────────────────────────────────────

func extractTextContent(msg *waProto.Message) string {
	if msg == nil {
		return ""
	}
	if text := msg.GetConversation(); text != "" {
		return text
	} else if extendedText := msg.GetExtendedTextMessage(); extendedText != nil {
		return extendedText.GetText()
	}
	return ""
}

func extractMediaInfo(msg *waProto.Message) (mediaType string, filename string, url string, mediaKey []byte, fileSHA256 []byte, fileEncSHA256 []byte, fileLength uint64) {
	if msg == nil {
		return "", "", "", nil, nil, nil, 0
	}
	if img := msg.GetImageMessage(); img != nil {
		return "image", "image_" + time.Now().Format("20060102_150405") + ".jpg",
			img.GetURL(), img.GetMediaKey(), img.GetFileSHA256(), img.GetFileEncSHA256(), img.GetFileLength()
	}
	if vid := msg.GetVideoMessage(); vid != nil {
		return "video", "video_" + time.Now().Format("20060102_150405") + ".mp4",
			vid.GetURL(), vid.GetMediaKey(), vid.GetFileSHA256(), vid.GetFileEncSHA256(), vid.GetFileLength()
	}
	if aud := msg.GetAudioMessage(); aud != nil {
		return "audio", "audio_" + time.Now().Format("20060102_150405") + ".ogg",
			aud.GetURL(), aud.GetMediaKey(), aud.GetFileSHA256(), aud.GetFileEncSHA256(), aud.GetFileLength()
	}
	if doc := msg.GetDocumentMessage(); doc != nil {
		fn := doc.GetFileName()
		if fn == "" {
			fn = "document_" + time.Now().Format("20060102_150405")
		}
		return "document", fn,
			doc.GetURL(), doc.GetMediaKey(), doc.GetFileSHA256(), doc.GetFileEncSHA256(), doc.GetFileLength()
	}
	return "", "", "", nil, nil, nil, 0
}

// ─── Message formatting ───────────────────────────────────────────────────────

func formatWhatsAppMessage(message string) string {
	return convertMarkdownTablesToWhatsApp(message)
}

func convertMarkdownTablesToWhatsApp(message string) string {
	lines := strings.Split(message, "\n")
	var out []string

	for i := 0; i < len(lines); {
		if i+1 < len(lines) && isMarkdownTableHeader(lines[i], lines[i+1]) {
			header := splitMarkdownTableRow(lines[i])
			j := i + 2
			var rows [][]string
			for j < len(lines) {
				cells := splitMarkdownTableRow(lines[j])
				if len(cells) == 0 {
					break
				}
				rows = append(rows, cells)
				j++
			}
			if len(rows) == 0 {
				out = append(out, lines[i])
				i++
				continue
			}
			tableText := renderWhatsAppTable(header, rows)
			if tableText != "" {
				out = append(out, tableText)
			}
			i = j
			continue
		}
		out = append(out, lines[i])
		i++
	}

	return strings.Join(out, "\n")
}

func isMarkdownTableHeader(headerLine, separatorLine string) bool {
	header := splitMarkdownTableRow(headerLine)
	separator := splitMarkdownTableRow(separatorLine)
	if len(header) == 0 || len(header) != len(separator) {
		return false
	}
	for _, cell := range separator {
		if !isMarkdownTableSeparatorCell(cell) {
			return false
		}
	}
	return true
}

func isMarkdownTableSeparatorCell(cell string) bool {
	cell = strings.TrimSpace(cell)
	if len(cell) < 3 {
		return false
	}
	cell = strings.Trim(cell, ":")
	if len(cell) < 3 {
		return false
	}
	for _, r := range cell {
		if r != '-' {
			return false
		}
	}
	return true
}

func splitMarkdownTableRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	if !strings.Contains(trimmed, "|") {
		return nil
	}
	trimmed = strings.Trim(trimmed, "|")
	parts := strings.Split(trimmed, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

func renderWhatsAppTable(header []string, rows [][]string) string {
	var b strings.Builder
	for rowIndex, row := range rows {
		if rowIndex > 0 {
			b.WriteString("\n")
		}
		title := tableCell(row, 0)
		if title == "" {
			title = fmt.Sprintf("Row %d", rowIndex+1)
		}
		b.WriteString("*")
		b.WriteString(title)
		b.WriteString("*")
		for col := 1; col < len(header); col++ {
			value := tableCell(row, col)
			if value == "" {
				continue
			}
			b.WriteString("\n• ")
			b.WriteString(header[col])
			b.WriteString(": ")
			b.WriteString(value)
		}
	}
	return b.String()
}

func tableCell(row []string, index int) string {
	if index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

// ─── Send message ─────────────────────────────────────────────────────────────

func sendWhatsAppMessage(client *whatsmeow.Client, recipient string, message string, mediaPath string) (bool, string) {
	if !client.IsConnected() {
		return false, "Not connected to WhatsApp"
	}
	message = formatWhatsAppMessage(message)

	var recipientJID types.JID
	var err error

	if strings.Contains(recipient, "@") {
		recipientJID, err = types.ParseJID(recipient)
		if err != nil {
			return false, fmt.Sprintf("Error parsing JID: %v", err)
		}
	} else {
		recipientJID = types.JID{
			User:   recipient,
			Server: "s.whatsapp.net",
		}
	}

	msg := &waProto.Message{}

	if mediaPath != "" {
		mediaData, err := os.ReadFile(mediaPath)
		if err != nil {
			return false, fmt.Sprintf("Error reading media file: %v", err)
		}

		fileExt := strings.ToLower(mediaPath[strings.LastIndex(mediaPath, ".")+1:])
		var mediaType whatsmeow.MediaType
		var mimeType string

		switch fileExt {
		case "jpg", "jpeg":
			mediaType = whatsmeow.MediaImage
			mimeType = "image/jpeg"
		case "png":
			mediaType = whatsmeow.MediaImage
			mimeType = "image/png"
		case "gif":
			mediaType = whatsmeow.MediaImage
			mimeType = "image/gif"
		case "webp":
			mediaType = whatsmeow.MediaImage
			mimeType = "image/webp"
		case "ogg":
			mediaType = whatsmeow.MediaAudio
			mimeType = "audio/ogg; codecs=opus"
		case "mp4":
			mediaType = whatsmeow.MediaVideo
			mimeType = "video/mp4"
		case "avi":
			mediaType = whatsmeow.MediaVideo
			mimeType = "video/avi"
		case "mov":
			mediaType = whatsmeow.MediaVideo
			mimeType = "video/quicktime"
		default:
			mediaType = whatsmeow.MediaDocument
			mimeType = "application/octet-stream"
		}

		resp, err := client.Upload(context.Background(), mediaData, mediaType)
		if err != nil {
			return false, fmt.Sprintf("Error uploading media: %v", err)
		}

		fmt.Println("Media uploaded", resp)

		switch mediaType {
		case whatsmeow.MediaImage:
			msg.ImageMessage = &waProto.ImageMessage{
				Caption:       proto.String(message),
				Mimetype:      proto.String(mimeType),
				URL:           &resp.URL,
				DirectPath:    &resp.DirectPath,
				MediaKey:      resp.MediaKey,
				FileEncSHA256: resp.FileEncSHA256,
				FileSHA256:    resp.FileSHA256,
				FileLength:    &resp.FileLength,
			}
		case whatsmeow.MediaAudio:
			var seconds uint32 = 30
			var waveform []byte
			if strings.Contains(mimeType, "ogg") {
				analyzedSeconds, analyzedWaveform, err := analyzeOggOpus(mediaData)
				if err == nil {
					seconds = analyzedSeconds
					waveform = analyzedWaveform
				} else {
					return false, fmt.Sprintf("Failed to analyze Ogg Opus file: %v", err)
				}
			} else {
				fmt.Printf("Not an Ogg Opus file: %s\n", mimeType)
			}
			msg.AudioMessage = &waProto.AudioMessage{
				Mimetype:      proto.String(mimeType),
				URL:           &resp.URL,
				DirectPath:    &resp.DirectPath,
				MediaKey:      resp.MediaKey,
				FileEncSHA256: resp.FileEncSHA256,
				FileSHA256:    resp.FileSHA256,
				FileLength:    &resp.FileLength,
				Seconds:       proto.Uint32(seconds),
				PTT:           proto.Bool(true),
				Waveform:      waveform,
			}
		case whatsmeow.MediaVideo:
			msg.VideoMessage = &waProto.VideoMessage{
				Caption:       proto.String(message),
				Mimetype:      proto.String(mimeType),
				URL:           &resp.URL,
				DirectPath:    &resp.DirectPath,
				MediaKey:      resp.MediaKey,
				FileEncSHA256: resp.FileEncSHA256,
				FileSHA256:    resp.FileSHA256,
				FileLength:    &resp.FileLength,
			}
		case whatsmeow.MediaDocument:
			msg.DocumentMessage = &waProto.DocumentMessage{
				Title:         proto.String(mediaPath[strings.LastIndex(mediaPath, "/")+1:]),
				Caption:       proto.String(message),
				Mimetype:      proto.String(mimeType),
				URL:           &resp.URL,
				DirectPath:    &resp.DirectPath,
				MediaKey:      resp.MediaKey,
				FileEncSHA256: resp.FileEncSHA256,
				FileSHA256:    resp.FileSHA256,
				FileLength:    &resp.FileLength,
			}
		}
	} else {
		msg.Conversation = proto.String(message)
	}

	sendResp, err := client.SendMessage(context.Background(), recipientJID, msg)
	if err != nil {
		return false, fmt.Sprintf("Error sending message: %v", err)
	}
	markSentMessage(sendResp.ID)
	return true, fmt.Sprintf("Message sent to %s", recipient)
}

// ─── Media download ───────────────────────────────────────────────────────────

type MediaDownloader struct {
	URL           string
	DirectPath    string
	MediaKey      []byte
	FileLength    uint64
	FileSHA256    []byte
	FileEncSHA256 []byte
	MediaType     whatsmeow.MediaType
}

func (d *MediaDownloader) GetDirectPath() string        { return d.DirectPath }
func (d *MediaDownloader) GetURL() string               { return d.URL }
func (d *MediaDownloader) GetMediaKey() []byte          { return d.MediaKey }
func (d *MediaDownloader) GetFileLength() uint64        { return d.FileLength }
func (d *MediaDownloader) GetFileSHA256() []byte        { return d.FileSHA256 }
func (d *MediaDownloader) GetFileEncSHA256() []byte     { return d.FileEncSHA256 }
func (d *MediaDownloader) GetMediaType() whatsmeow.MediaType { return d.MediaType }

func downloadMedia(client *whatsmeow.Client, messageStore *MessageStore, messageID, chatJID string) (bool, string, string, string, error) {
	var mediaType, filename, url string
	var mediaKey, fileSHA256, fileEncSHA256 []byte
	var fileLength uint64
	var err error

	chatDir := fmt.Sprintf("store/%s", strings.ReplaceAll(chatJID, ":", "_"))

	mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength, err = messageStore.GetMediaInfo(messageID, chatJID)
	if err != nil {
		err = messageStore.db.QueryRow(
			"SELECT media_type, filename FROM messages WHERE id = ? AND chat_jid = ?",
			messageID, chatJID,
		).Scan(&mediaType, &filename)
		if err != nil {
			return false, "", "", "", fmt.Errorf("failed to find message: %v", err)
		}
	}

	if mediaType == "" {
		return false, "", "", "", fmt.Errorf("not a media message")
	}

	if err := os.MkdirAll(chatDir, 0755); err != nil {
		return false, "", "", "", fmt.Errorf("failed to create chat directory: %v", err)
	}

	localPath := fmt.Sprintf("%s/%s", chatDir, filename)
	absPath, err := filepath.Abs(localPath)
	if err != nil {
		return false, "", "", "", fmt.Errorf("failed to get absolute path: %v", err)
	}

	if _, err := os.Stat(localPath); err == nil {
		return true, mediaType, filename, absPath, nil
	}

	if url == "" || len(mediaKey) == 0 || len(fileSHA256) == 0 || len(fileEncSHA256) == 0 || fileLength == 0 {
		return false, "", "", "", fmt.Errorf("incomplete media information for download")
	}

	fmt.Printf("Attempting to download media for message %s in chat %s...\n", messageID, chatJID)

	directPath := extractDirectPathFromURL(url)

	var waMediaType whatsmeow.MediaType
	switch mediaType {
	case "image":
		waMediaType = whatsmeow.MediaImage
	case "video":
		waMediaType = whatsmeow.MediaVideo
	case "audio":
		waMediaType = whatsmeow.MediaAudio
	case "document":
		waMediaType = whatsmeow.MediaDocument
	default:
		return false, "", "", "", fmt.Errorf("unsupported media type: %s", mediaType)
	}

	downloader := &MediaDownloader{
		URL:           url,
		DirectPath:    directPath,
		MediaKey:      mediaKey,
		FileLength:    fileLength,
		FileSHA256:    fileSHA256,
		FileEncSHA256: fileEncSHA256,
		MediaType:     waMediaType,
	}

	mediaData, err := client.Download(context.Background(), downloader)
	if err != nil {
		return false, "", "", "", fmt.Errorf("failed to download media: %v", err)
	}

	if err := os.WriteFile(localPath, mediaData, 0644); err != nil {
		return false, "", "", "", fmt.Errorf("failed to save media file: %v", err)
	}

	fmt.Printf("Successfully downloaded %s media to %s (%d bytes)\n", mediaType, absPath, len(mediaData))
	return true, mediaType, filename, absPath, nil
}

func extractDirectPathFromURL(url string) string {
	parts := strings.SplitN(url, ".net/", 2)
	if len(parts) < 2 {
		return url
	}
	return "/" + strings.SplitN(parts[1], "?", 2)[0]
}

// ─── Ogg Opus helpers ─────────────────────────────────────────────────────────

func analyzeOggOpus(data []byte) (duration uint32, waveform []byte, err error) {
	if len(data) < 4 || string(data[0:4]) != "OggS" {
		return 0, nil, fmt.Errorf("not a valid Ogg file (missing OggS signature)")
	}

	var lastGranule uint64
	var sampleRate uint32 = 48000
	var preSkip uint16
	var foundOpusHead bool

	for i := 0; i < len(data); {
		if i+27 >= len(data) {
			break
		}
		if string(data[i:i+4]) != "OggS" {
			i++
			continue
		}
		granulePos := binary.LittleEndian.Uint64(data[i+6 : i+14])
		pageSeqNum := binary.LittleEndian.Uint32(data[i+18 : i+22])
		numSegments := int(data[i+26])
		if i+27+numSegments >= len(data) {
			break
		}
		segmentTable := data[i+27 : i+27+numSegments]
		pageSize := 27 + numSegments
		for _, segLen := range segmentTable {
			pageSize += int(segLen)
		}
		if !foundOpusHead && pageSeqNum <= 1 {
			pageData := data[i : i+pageSize]
			headPos := bytes.Index(pageData, []byte("OpusHead"))
			if headPos >= 0 && headPos+12 < len(pageData) {
				headPos += 8
				if headPos+12 <= len(pageData) {
					preSkip = binary.LittleEndian.Uint16(pageData[headPos+10 : headPos+12])
					sampleRate = binary.LittleEndian.Uint32(pageData[headPos+12 : headPos+16])
					foundOpusHead = true
					fmt.Printf("Found OpusHead: sampleRate=%d, preSkip=%d\n", sampleRate, preSkip)
				}
			}
		}
		if granulePos != 0 {
			lastGranule = granulePos
		}
		i += pageSize
	}

	if !foundOpusHead {
		fmt.Println("Warning: OpusHead not found, using default values")
	}

	if lastGranule > 0 {
		durationSeconds := float64(lastGranule-uint64(preSkip)) / float64(sampleRate)
		duration = uint32(math.Ceil(durationSeconds))
		fmt.Printf("Calculated Opus duration from granule: %f seconds (lastGranule=%d)\n", durationSeconds, lastGranule)
	} else {
		fmt.Println("Warning: No valid granule position found, using estimation")
		duration = uint32(float64(len(data)) / 2000.0)
	}

	if duration < 1 {
		duration = 1
	} else if duration > 300 {
		duration = 300
	}

	waveform = placeholderWaveform(duration)
	fmt.Printf("Ogg Opus analysis: size=%d bytes, calculated duration=%d sec, waveform=%d bytes\n",
		len(data), duration, len(waveform))
	return duration, waveform, nil
}

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

func placeholderWaveform(duration uint32) []byte {
	const waveformLength = 64
	waveform := make([]byte, waveformLength)
	rand.Seed(int64(duration))
	baseAmplitude := 35.0
	frequencyFactor := float64(min(int(duration), 120)) / 30.0
	for i := range waveform {
		pos := float64(i) / float64(waveformLength)
		val := baseAmplitude * math.Sin(pos*math.Pi*frequencyFactor*8)
		val += (baseAmplitude / 2) * math.Sin(pos*math.Pi*frequencyFactor*16)
		val += (rand.Float64() - 0.5) * 15
		fadeInOut := math.Sin(pos * math.Pi)
		val = val * (0.7 + 0.3*fadeInOut)
		val = val + 50
		if val < 0 {
			val = 0
		} else if val > 100 {
			val = 100
		}
		waveform[i] = byte(val)
	}
	return waveform
}

// ─── Chat name resolution ─────────────────────────────────────────────────────

func GetChatName(client *whatsmeow.Client, messageStore *MessageStore, jid types.JID, chatJID string, conversation interface{}, sender string, logger waLog.Logger) string {
	var existingName string
	err := messageStore.db.QueryRow("SELECT name FROM chats WHERE jid = ?", chatJID).Scan(&existingName)
	if err == nil && existingName != "" {
		logger.Infof("Using existing chat name for %s: %s", chatJID, existingName)
		return existingName
	}

	var name string
	if jid.Server == "g.us" {
		logger.Infof("Getting name for group: %s", chatJID)
		if conversation != nil {
			var displayName, convName *string
			v := reflect.ValueOf(conversation)
			if v.Kind() == reflect.Ptr && !v.IsNil() {
				v = v.Elem()
				if f := v.FieldByName("DisplayName"); f.IsValid() && f.Kind() == reflect.Ptr && !f.IsNil() {
					dn := f.Elem().String()
					displayName = &dn
				}
				if f := v.FieldByName("Name"); f.IsValid() && f.Kind() == reflect.Ptr && !f.IsNil() {
					n := f.Elem().String()
					convName = &n
				}
			}
			if displayName != nil && *displayName != "" {
				name = *displayName
			} else if convName != nil && *convName != "" {
				name = *convName
			}
		}
		if name == "" {
			groupInfo, err := client.GetGroupInfo(context.Background(), jid)
			if err == nil && groupInfo.Name != "" {
				name = groupInfo.Name
			} else {
				name = fmt.Sprintf("Group %s", jid.User)
			}
		}
		logger.Infof("Using group name: %s", name)
	} else {
		logger.Infof("Getting name for contact: %s", chatJID)
		contact, err := client.Store.Contacts.GetContact(context.Background(), jid)
		if err == nil && contact.FullName != "" {
			name = contact.FullName
		} else if sender != "" {
			name = sender
		} else {
			name = jid.User
		}
		logger.Infof("Using contact name: %s", name)
	}
	return name
}

// ─── Bridge command handler ───────────────────────────────────────────────────

func handleBridgeCommand(client *whatsmeow.Client, chatJID, content string, isFromMe bool) bool {
	cmd := strings.TrimSpace(content)
	isPersonality := strings.HasPrefix(cmd, "!set-personality")
	isIcon := strings.HasPrefix(cmd, "!set-icon")
	switch cmd {
	case "!meet-claude", "!meet-codex", "!remove-claude", "!remove-codex", "!help", "!clear-session":
	default:
		if !isPersonality && !isIcon {
			return false
		}
	}
	if !isFromMe {
		sendWhatsAppMessage(client, chatJID, "⚠️ Only the bridge owner can use bridge commands", "")
		return true
	}
	switch cmd {
	case "!help":
		sendWhatsAppMessage(client, chatJID, "Bridge commands:\n"+
			"!meet-claude — add this chat to Claude whitelist\n"+
			"!remove-claude — remove this chat from Claude whitelist\n"+
			"!meet-codex — add this chat to Codex whitelist\n"+
			"!remove-codex — remove this chat from Codex whitelist\n"+
			"!clear-session — clear Claude/Codex session memory and start fresh\n"+
			"!set-personality <preset> — set personality (default / kids / pro / creative)\n"+
			"!stats — show token usage and cost for this session\n"+
			"!help — show this help screen", "")
	case "!meet-claude":
		allowedChatsMu.Lock()
		allowedChats[chatJID] = struct{}{}
		allowedChatsMu.Unlock()
		if err := saveAllowedChats(); err != nil {
			sendWhatsAppMessage(client, chatJID, "⚠️ Failed to save whitelist: "+err.Error(), "")
			return true
		}
		ensureChatClaudeSettings(chatJID)
		sendWhatsAppMessage(client, chatJID, "👋 Hi! I'm Claude. This chat is now connected to me — send any message to get started.", "")
	case "!meet-codex":
		codexAllowedChatsMu.Lock()
		codexAllowedChats[chatJID] = struct{}{}
		codexAllowedChatsMu.Unlock()
		if err := saveCodexAllowedChats(); err != nil {
			sendWhatsAppMessage(client, chatJID, "⚠️ Failed to save whitelist: "+err.Error(), "")
			return true
		}
		ensureChatClaudeSettings(chatJID)
		sendWhatsAppMessage(client, chatJID, "👋 Hi! I'm Codex. This chat is now connected to me — send any message to get started.", "")
	case "!remove-claude":
		allowedChatsMu.Lock()
		delete(allowedChats, chatJID)
		allowedChatsMu.Unlock()
		if err := saveAllowedChats(); err != nil {
			sendWhatsAppMessage(client, chatJID, "⚠️ Failed to save whitelist: "+err.Error(), "")
			return true
		}
		sendWhatsAppMessage(client, chatJID, "✅ Claude has left this chat.", "")
	case "!remove-codex":
		codexAllowedChatsMu.Lock()
		delete(codexAllowedChats, chatJID)
		codexAllowedChatsMu.Unlock()
		if err := saveCodexAllowedChats(); err != nil {
			sendWhatsAppMessage(client, chatJID, "⚠️ Failed to save whitelist: "+err.Error(), "")
			return true
		}
		sendWhatsAppMessage(client, chatJID, "✅ Codex has left this chat.", "")
	case "!clear-session":
		sessions := loadSessions()
		codexSessions := loadCodexSessions()
		_, hasSession := sessions[chatJID]
		_, hasCodexSession := codexSessions[chatJID]
		if !hasSession && !hasCodexSession {
			sendWhatsAppMessage(client, chatJID, "No active session to clear.", "")
			return true
		}
		handleClearSession(client, chatJID)
	}
	if isPersonality {
		parts := strings.Fields(cmd)
		if len(parts) < 2 {
			current := getChatPersonality(chatJID)
			sendWhatsAppMessage(client, chatJID, fmt.Sprintf("Current personality: %s\nAvailable: default, kids, pro, creative", current), "")
			return true
		}
		preset := parts[1]
		switch preset {
		case "default", "kids", "pro", "creative":
			if err := setChatPersonality(chatJID, preset); err != nil {
				sendWhatsAppMessage(client, chatJID, "⚠️ Failed to save personality: "+err.Error(), "")
				return true
			}
			clearSessionData(chatJID)
			sendWhatsAppMessage(client, chatJID, fmt.Sprintf("✅ Personality set to: %s (session reset — changes take effect now)", preset), "")
		default:
			sendWhatsAppMessage(client, chatJID, "⚠️ Unknown preset. Available: default, kids, pro, creative", "")
		}
	}
	if isIcon {
		parts := strings.Fields(cmd)
		if len(parts) < 2 {
			sendWhatsAppMessage(client, chatJID, "Usage: !set-icon <emoji>", "")
			return true
		}
		emoji := parts[1]
		if err := setIconForChat(chatJID, emoji); err != nil {
			sendWhatsAppMessage(client, chatJID, "⚠️ Failed to set icon: "+err.Error(), "")
			return true
		}
		clearSessionData(chatJID)
		sendWhatsAppMessage(client, chatJID, fmt.Sprintf("✅ Icon set to: %s (session reset — changes take effect now)", emoji), "")
	}
	return true
}

func handleClearSession(client *whatsmeow.Client, chatJID string) {
	deleteSession(chatJID)
	deleteCodexSession(chatJID)
	inputHistoryMu.Lock()
	delete(inputHistory, chatJID)
	inputHistoryMu.Unlock()
	sendWhatsAppMessage(client, chatJID, "✅ Session cleared for this chat. Next message starts fresh.", "")
}

// ─── Message handler ──────────────────────────────────────────────────────────

func handleMessage(client *whatsmeow.Client, messageStore *MessageStore, msg *events.Message, logger waLog.Logger) {
	chatJID := msg.Info.Chat.String()
	sender := msg.Info.Sender.User
	name := GetChatName(client, messageStore, msg.Info.Chat, chatJID, nil, sender, logger)

	if err := messageStore.StoreChat(chatJID, name, msg.Info.Timestamp); err != nil {
		logger.Warnf("Failed to store chat: %v", err)
	}

	content := extractTextContent(msg.Message)

	if !isAllowedChat(chatJID) {
		if len(content) < 2 || content[:2] != "!m" {
			return
		}
	}

	if handleBridgeCommand(client, chatJID, content, msg.Info.IsFromMe) {
		return
	}

	mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength := extractMediaInfo(msg.Message)

	if content == "" && mediaType == "" {
		return
	}

	err := messageStore.StoreMessage(
		msg.Info.ID, chatJID, sender, content, msg.Info.Timestamp, msg.Info.IsFromMe,
		mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength,
	)
	if err != nil {
		logger.Warnf("Failed to store message: %v", err)
	} else {
		timestamp := msg.Info.Timestamp.Format("2006-01-02 15:04:05")
		direction := "←"
		if msg.Info.IsFromMe {
			direction = "→"
		}
		if mediaType != "" {
			fmt.Printf("[%s] %s %s: [%s: %s] %s\n", timestamp, direction, sender, mediaType, filename, content)
		} else if content != "" {
			fmt.Printf("[%s] %s %s: %s\n", timestamp, direction, sender, content)
		}
	}

	if mediaType == "audio" && content == "" && isAllowedChat(chatJID) && !isSentByUs(msg.Info.ID) {
		_, _, _, audioPath, dlErr := downloadMedia(client, messageStore, msg.Info.ID, chatJID)
		if dlErr != nil {
			sendWhatsAppMessage(client, chatJID, "⚠️ Could not download voice message: "+dlErr.Error(), "")
			return
		}
		transcript, txErr := transcribeAudio(audioPath)
		if txErr != nil {
			sendWhatsAppMessage(client, chatJID, "⚠️ Could not transcribe voice message: "+txErr.Error(), "")
			return
		}
		content = "[🎤 Voice]: " + transcript
	}

	// ── Non-audio attachment pipeline ──────────────────────────────────────────
	if mediaType != "" && mediaType != "audio" && isAllowedChat(chatJID) && !isSentByUs(msg.Info.ID) {
		ch := NewWhatsAppChannel(client, messageStore)
		var llm LLM
		if isCodexChat(chatJID) {
			llm = NewCodexLLM()
		} else {
			llm = NewClaudeLLM()
		}
		inMsg := IncomingMessage{
			ChatID:    chatJID,
			SenderID:  sender,
			Text:      content,
			IsFromMe:  msg.Info.IsFromMe,
			MessageID: msg.Info.ID,
		}
		att, attErr := ch.ReceiveAttachment(inMsg)
		if attErr != nil {
			sendWhatsAppMessage(client, chatJID, "⚠️ Could not download attachment: "+attErr.Error(), "")
			return
		}
		if att != nil {
			go func() {
				reply, procErr := llm.ProcessWithAttachment(chatJID, content, att)
				if procErr != nil {
					sendWhatsAppMessage(client, chatJID, "⚠️ Could not process attachment: "+procErr.Error(), "")
					return
				}
				sendWhatsAppMessage(client, chatJID, reply, "")
			}()
			return
		}
		// att == nil: unsupported media type — fall through to text path
	}

	if content != "" && isAllowedChat(chatJID) && !isSentByUs(msg.Info.ID) {
		if isLooping(chatJID, content) {
			sendWhatsAppMessage(client, chatJID, "⚠️ You've sent the same message several times. Try rephrasing or type 'clear session' to start fresh.", "")
			return
		}
		addToInputHistory(chatJID, content)

		if isCodexChat(chatJID) {
			if strings.ToLower(strings.TrimSpace(content)) == "!stats" {
				codexStatsMu.Lock()
				cStats, cOk := codexStatsMap[chatJID]
				codexStatsMu.Unlock()
				var cReply string
				if !cOk {
					cReply = "No stats yet — send a message first."
				} else {
					cReply = fmt.Sprintf(
						"📊 Codex stats:\n• Input tokens: %d\n• Output tokens: %d\n• Total tokens: %d\n• Last updated: %s",
						cStats.InputTokens, cStats.OutputTokens, cStats.TotalTokens, cStats.LastUpdated,
					)
				}
				sendWhatsAppMessage(client, chatJID, cReply, "")
			} else {
				go handleWithCodex(chatJID, content, func(reply string) {
					sendWhatsAppMessage(client, chatJID, reply, "")
				})
			}
		} else {
			if strings.ToLower(strings.TrimSpace(content)) == "!stats" {
				usageStatsMu.Lock()
				stats, ok := usageStatsMap[chatJID]
				usageStatsMu.Unlock()
				var reply string
				if !ok {
					reply = "No stats yet — send a message first."
				} else {
					durationSec := float64(stats.DurationMs) / 1000.0
					reply = fmt.Sprintf(
						"📊 Stats for this session:\n• Cache read: %d tokens\n• Cache write: %d tokens\n• Input tokens: %d\n• Output tokens: %d\n• Total cost: $%.4f USD\n• Response time: %.1fs\n• Last updated: %s",
						stats.CacheReadTokens, stats.CacheWriteTokens,
						stats.InputTokens, stats.OutputTokens,
						stats.TotalCostUSD, durationSec, stats.LastUpdated,
					)
					if len(stats.ModelUsage) > 1 {
						reply += "\n\nPer-model breakdown:"
						for model, mu := range stats.ModelUsage {
							reply += fmt.Sprintf("\n• %s: %d in / %d out, $%.4f",
								model, mu.InputTokens, mu.OutputTokens, mu.CostUSD)
						}
					}
				}
				sendWhatsAppMessage(client, chatJID, reply, "")
			} else {
				go handleWithClaude(chatJID, content, func(reply string) {
					sendWhatsAppMessage(client, chatJID, reply, "")
				})
			}
		}
	}
}

// ─── History sync ─────────────────────────────────────────────────────────────

func handleHistorySync(client *whatsmeow.Client, messageStore *MessageStore, historySync *events.HistorySync, logger waLog.Logger) {
	fmt.Printf("Received history sync event with %d conversations\n", len(historySync.Data.Conversations))

	syncedCount := 0
	for _, conversation := range historySync.Data.Conversations {
		if conversation.ID == nil {
			continue
		}
		chatJID := *conversation.ID
		jid, err := types.ParseJID(chatJID)
		if err != nil {
			logger.Warnf("Failed to parse JID %s: %v", chatJID, err)
			continue
		}

		name := GetChatName(client, messageStore, jid, chatJID, conversation, "", logger)
		messages := conversation.Messages
		if len(messages) == 0 {
			continue
		}

		latestMsg := messages[0]
		if latestMsg == nil || latestMsg.Message == nil {
			continue
		}
		timestamp := time.Time{}
		if ts := latestMsg.Message.GetMessageTimestamp(); ts != 0 {
			timestamp = time.Unix(int64(ts), 0)
		} else {
			continue
		}
		messageStore.StoreChat(chatJID, name, timestamp)

		for _, msg := range messages {
			if msg == nil || msg.Message == nil {
				continue
			}
			var content string
			if msg.Message.Message != nil {
				if conv := msg.Message.Message.GetConversation(); conv != "" {
					content = conv
				} else if ext := msg.Message.Message.GetExtendedTextMessage(); ext != nil {
					content = ext.GetText()
				}
			}

			var mediaType, filename, url string
			var mediaKey, fileSHA256, fileEncSHA256 []byte
			var fileLength uint64
			if msg.Message.Message != nil {
				mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength = extractMediaInfo(msg.Message.Message)
			}

			logger.Infof("Message content: %v, Media Type: %v", content, mediaType)

			if content == "" && mediaType == "" {
				continue
			}

			var msgSender string
			isFromMe := false
			if msg.Message.Key != nil {
				if msg.Message.Key.FromMe != nil {
					isFromMe = *msg.Message.Key.FromMe
				}
				if !isFromMe && msg.Message.Key.Participant != nil && *msg.Message.Key.Participant != "" {
					msgSender = *msg.Message.Key.Participant
				} else if isFromMe {
					msgSender = client.Store.ID.User
				} else {
					msgSender = jid.User
				}
			} else {
				msgSender = jid.User
			}

			msgID := ""
			if msg.Message.Key != nil && msg.Message.Key.ID != nil {
				msgID = *msg.Message.Key.ID
			}

			ts := time.Time{}
			if t := msg.Message.GetMessageTimestamp(); t != 0 {
				ts = time.Unix(int64(t), 0)
			} else {
				continue
			}

			err = messageStore.StoreMessage(
				msgID, chatJID, msgSender, content, ts, isFromMe,
				mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength,
			)
			if err != nil {
				logger.Warnf("Failed to store history message: %v", err)
			} else {
				syncedCount++
				if mediaType != "" {
					logger.Infof("Stored message: [%s] %s -> %s: [%s: %s] %s",
						ts.Format("2006-01-02 15:04:05"), msgSender, chatJID, mediaType, filename, content)
				} else {
					logger.Infof("Stored message: [%s] %s -> %s: %s",
						ts.Format("2006-01-02 15:04:05"), msgSender, chatJID, content)
				}
			}
		}
	}
	fmt.Printf("History sync complete. Stored %d messages.\n", syncedCount)
}

// ─── REST API ─────────────────────────────────────────────────────────────────

type SendMessageResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type SendMessageRequest struct {
	Recipient string `json:"recipient"`
	Message   string `json:"message"`
	MediaPath string `json:"media_path,omitempty"`
}

type DownloadMediaRequest struct {
	MessageID string `json:"message_id"`
	ChatJID   string `json:"chat_jid"`
}

type DownloadMediaResponse struct {
	Success  bool   `json:"success"`
	Message  string `json:"message"`
	Filename string `json:"filename,omitempty"`
	Path     string `json:"path,omitempty"`
}

func startRESTServer(client *whatsmeow.Client, messageStore *MessageStore, port int) {
	http.HandleFunc("/api/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req SendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request format", http.StatusBadRequest)
			return
		}
		if req.Recipient == "" {
			http.Error(w, "Recipient is required", http.StatusBadRequest)
			return
		}
		if req.Message == "" && req.MediaPath == "" {
			http.Error(w, "Message or media path is required", http.StatusBadRequest)
			return
		}
		fmt.Println("Received request to send message", req.Message, req.MediaPath)
		success, message := sendWhatsAppMessage(client, req.Recipient, req.Message, req.MediaPath)
		fmt.Println("Message sent", success, message)
		w.Header().Set("Content-Type", "application/json")
		if !success {
			w.WriteHeader(http.StatusInternalServerError)
		}
		json.NewEncoder(w).Encode(SendMessageResponse{Success: success, Message: message})
	})

	http.HandleFunc("/api/download", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req DownloadMediaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request format", http.StatusBadRequest)
			return
		}
		if req.MessageID == "" || req.ChatJID == "" {
			http.Error(w, "Message ID and Chat JID are required", http.StatusBadRequest)
			return
		}
		success, mediaType, filename, path, err := downloadMedia(client, messageStore, req.MessageID, req.ChatJID)
		w.Header().Set("Content-Type", "application/json")
		if !success || err != nil {
			errMsg := "Unknown error"
			if err != nil {
				errMsg = err.Error()
			}
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(DownloadMediaResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to download media: %s", errMsg),
			})
			return
		}
		json.NewEncoder(w).Encode(DownloadMediaResponse{
			Success:  true,
			Message:  fmt.Sprintf("Successfully downloaded %s media", mediaType),
			Filename: filename,
			Path:     path,
		})
	})

	serverAddr := fmt.Sprintf(":%d", port)
	fmt.Printf("Starting REST API server on %s...\n", serverAddr)
	go func() {
		if err := http.ListenAndServe(serverAddr, nil); err != nil {
			fmt.Printf("REST API server error: %v\n", err)
		}
	}()
}

// ─── WhatsApp entry point ─────────────────────────────────────────────────────

func startWhatsApp() {
	logger := waLog.Stdout("Client", "INFO", true)
	logger.Infof("Starting WhatsApp client...")

	dir := storeDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.Errorf("Failed to create store directory: %v", err)
		return
	}

	dbLog := waLog.Stdout("Database", "INFO", true)
	container, err := sqlstore.New(context.Background(), "sqlite3",
		"file:"+filepath.Join(dir, "whatsapp.db")+"?_foreign_keys=on", dbLog)
	if err != nil {
		logger.Errorf("Failed to connect to database: %v", err)
		return
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		if err == sql.ErrNoRows {
			deviceStore = container.NewDevice()
			logger.Infof("Created new device")
		} else {
			logger.Errorf("Failed to get device: %v", err)
			return
		}
	}

	client := whatsmeow.NewClient(deviceStore, logger)
	if client == nil {
		logger.Errorf("Failed to create WhatsApp client")
		return
	}

	messageStore, err := NewMessageStore()
	if err != nil {
		logger.Errorf("Failed to initialize message store: %v", err)
		return
	}

	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			handleMessage(client, messageStore, v, logger)
		case *events.HistorySync:
			handleHistorySync(client, messageStore, v, logger)
		case *events.Connected:
			logger.Infof("Connected to WhatsApp")
		case *events.LoggedOut:
			logger.Warnf("Device logged out, please scan QR code to log in again")
		}
	})

	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			logger.Errorf("Failed to connect: %v", err)
			return
		}
		connected := make(chan bool, 1)
		for evt := range qrChan {
			if evt.Event == "code" {
				fmt.Println("\nScan this QR code with your WhatsApp app:")
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
			} else if evt.Event == "success" {
				connected <- true
				break
			}
		}
		select {
		case <-connected:
			fmt.Println("\nSuccessfully connected and authenticated!")
		case <-time.After(3 * time.Minute):
			logger.Errorf("Timeout waiting for QR code scan")
			return
		}
	} else {
		err = client.Connect()
		if err != nil {
			logger.Errorf("Failed to connect: %v", err)
			return
		}
	}

	time.Sleep(2 * time.Second)

	if !client.IsConnected() {
		logger.Errorf("Failed to establish stable connection")
		return
	}

	fmt.Println("\n✓ Connected to WhatsApp!")
	startRESTServer(client, messageStore, 8080)
}
