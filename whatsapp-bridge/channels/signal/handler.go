package signal

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"whatsapp-client/core"
)

// ─── Signal JSON-RPC wire types ───────────────────────────────────────────────

type signalRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      interface{}     `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type signalReceiveParams struct {
	Envelope signalEnvelope `json:"envelope"`
}

type signalEnvelope struct {
	Source       string             `json:"source"`
	SourceName   string             `json:"sourceName"`
	SourceDevice int                `json:"sourceDevice"`
	Timestamp    int64              `json:"timestamp"`
	DataMessage  *signalDataMessage `json:"dataMessage"`
	SyncMessage  json.RawMessage    `json:"syncMessage"`
}

type signalAttachment struct {
	ContentType string `json:"contentType"`
	Filename    string `json:"filename"`
	ID          string `json:"id"`
	Size        int64  `json:"size"`
	VoiceNote   bool   `json:"voiceNote"`
}

type signalReaction struct {
	Emoji               string `json:"emoji"`
	TargetAuthor        string `json:"targetAuthor"`
	TargetSentTimestamp int64  `json:"targetSentTimestamp"`
	IsRemove            bool   `json:"remove"`
}

type signalQuote struct {
	ID     int64  `json:"id"`
	Author string `json:"author"`
	Text   string `json:"text"`
}

type signalDataMessage struct {
	Timestamp   int64              `json:"timestamp"`
	Message     string             `json:"message"`
	GroupInfo   *signalGroupInfo   `json:"groupInfo"`
	Attachments []signalAttachment `json:"attachments"`
	Reaction    *signalReaction    `json:"reaction"`
	Quote       *signalQuote       `json:"quote"`
}

type signalGroupInfo struct {
	GroupId string `json:"groupId"`
	Type    string `json:"type"`
}

type signalSyncMessage struct {
	SentMessage *signalSyncSentMessage `json:"sentMessage"`
}

type signalSyncSentMessage struct {
	Timestamp   int64              `json:"timestamp"`
	Message     string             `json:"message"`
	Destination string             `json:"destination"`
	GroupInfo   *signalGroupInfo   `json:"groupInfo"`
	Attachments []signalAttachment `json:"attachments"`
	Reaction    *signalReaction    `json:"reaction"`
	Quote       *signalQuote       `json:"quote"`
}

// signalAttachmentsDir is where signal-cli daemon auto-saves received attachments.
var signalAttachmentsDir string

func init() {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		unixStyle := filepath.Join(home, ".local", "share", "signal-cli", "attachments")
		appDataStyle := filepath.Join(os.Getenv("APPDATA"), "signal-cli", "attachments")
		if _, err := os.Stat(unixStyle); err == nil {
			signalAttachmentsDir = unixStyle
		} else {
			signalAttachmentsDir = appDataStyle
		}
	case "darwin":
		signalAttachmentsDir = filepath.Join(home, "Library", "Application Support", "signal-cli", "attachments")
	default: // linux
		signalAttachmentsDir = filepath.Join(home, ".local", "share", "signal-cli", "attachments")
	}
}

// audioExtensions is the set of file extensions treated as voice notes.
var audioExtensions = map[string]bool{
	".aac": true, ".mp3": true, ".ogg": true, ".m4a": true, ".opus": true,
}

// transcribeSignalVoice finds the first audio attachment, resolves its local path,
// and returns a Whisper transcript. Returns ("", nil) if none found.
func transcribeSignalVoice(attachments []signalAttachment) (string, error) {
	for _, a := range attachments {
		ct := strings.ToLower(a.ContentType)
		if !strings.HasPrefix(ct, "audio/") && !a.VoiceNote {
			continue
		}
		var path string
		switch {
		case filepath.IsAbs(a.Filename):
			path = a.Filename
		case a.Filename != "":
			path = filepath.Join(signalAttachmentsDir, a.Filename)
		case a.ID != "":
			base := filepath.Join(signalAttachmentsDir, a.ID)
			if _, err := os.Stat(base); err == nil {
				path = base
			} else {
				for ext := range audioExtensions {
					if candidate := base + ext; func() bool { _, e := os.Stat(candidate); return e == nil }() {
						path = candidate
						break
					}
				}
			}
		}
		if path == "" {
			log.Printf("Signal: cannot locate attachment id=%s filename=%q — skipping", a.ID, a.Filename)
			continue
		}
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("attachment file not found at %s: %w", path, err)
		}
		log.Printf("Signal: transcribing voice attachment %s", path)
		return core.TranscribeAudio(path)
	}
	return "", nil
}

// ─── Last-active chat tracker ─────────────────────────────────────────────────

var (
	lastSignalActiveChatMu sync.Mutex
	lastSignalActiveChat   string
	lastSignalActiveChatTs time.Time
)

func setLastSignalActiveChat(chatID string) {
	lastSignalActiveChatMu.Lock()
	lastSignalActiveChat = chatID
	lastSignalActiveChatTs = time.Now()
	lastSignalActiveChatMu.Unlock()
}

func getLastSignalActiveChat() (string, bool) {
	lastSignalActiveChatMu.Lock()
	defer lastSignalActiveChatMu.Unlock()
	if lastSignalActiveChat == "" || time.Since(lastSignalActiveChatTs) > 10*time.Minute {
		return "", false
	}
	return lastSignalActiveChat, true
}

// ─── TCP connection state ─────────────────────────────────────────────────────

var (
	signalConnMu    sync.Mutex
	signalConn      net.Conn
	signalIDCounter int64
)

// signalPendingSends maps RPC id (int64) → chan int64 (sent timestamp result).
var signalPendingSends sync.Map

// ─── Deduplication ────────────────────────────────────────────────────────────

var (
	signalDedupeMu   sync.Mutex
	signalDedupeSeen = make(map[string]struct{})
)

func signalMarkSeen(key string) bool {
	signalDedupeMu.Lock()
	defer signalDedupeMu.Unlock()
	if _, ok := signalDedupeSeen[key]; ok {
		return true
	}
	signalDedupeSeen[key] = struct{}{}
	if len(signalDedupeSeen) > 1000 {
		for k := range signalDedupeSeen {
			delete(signalDedupeSeen, k)
			break
		}
	}
	return false
}

// ─── sendSignalFile ───────────────────────────────────────────────────────────

func sendSignalFile(chatID, filePath string) {
	signalConnMu.Lock()
	conn := signalConn
	signalConnMu.Unlock()

	if conn == nil {
		log.Printf("Signal: cannot send file to %s — not connected", chatID)
		return
	}

	id := atomic.AddInt64(&signalIDCounter, 1)

	var params map[string]interface{}
	if strings.HasPrefix(chatID, "+") {
		params = map[string]interface{}{
			"recipient":   []string{chatID},
			"attachments": []string{filePath},
		}
	} else {
		params = map[string]interface{}{
			"groupId":     chatID,
			"attachments": []string{filePath},
		}
	}

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "send",
		"params":  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		log.Printf("Signal: marshal sendFile error: %v", err)
		return
	}
	data = append(data, '\n')

	conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if _, err := conn.Write(data); err != nil {
		log.Printf("Signal: write error sending file to %s: %v", chatID, err)
		signalConnMu.Lock()
		if signalConn == conn {
			signalConn = nil
		}
		signalConnMu.Unlock()
		conn.Close()
	}
	conn.SetWriteDeadline(time.Time{})
	log.Printf("Signal: sent file %s to %s", filePath, chatID)
}

// sendSignalMessage sends a text message and blocks until signal-cli returns the
// sent timestamp (milliseconds). Returns 0 on error or timeout.
func sendSignalMessage(chatID, message string) int64 {
	signalConnMu.Lock()
	conn := signalConn
	signalConnMu.Unlock()

	if conn == nil {
		log.Printf("Signal: cannot send to %s — not connected", chatID)
		return 0
	}

	id := atomic.AddInt64(&signalIDCounter, 1)

	resCh := make(chan int64, 1)
	signalPendingSends.Store(id, resCh)

	var params map[string]interface{}
	if strings.HasPrefix(chatID, "+") {
		params = map[string]interface{}{
			"recipient": []string{chatID},
			"message":   message,
		}
	} else {
		params = map[string]interface{}{
			"groupId": chatID,
			"message": message,
		}
	}

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "send",
		"params":  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		log.Printf("Signal: marshal send error: %v", err)
		signalPendingSends.Delete(id)
		return 0
	}
	data = append(data, '\n')

	conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	if _, err := conn.Write(data); err != nil {
		log.Printf("Signal: write error sending to %s: %v", chatID, err)
		signalPendingSends.Delete(id)
		signalConnMu.Lock()
		if signalConn == conn {
			signalConn = nil
		}
		signalConnMu.Unlock()
		conn.Close()
		return 0
	}
	conn.SetWriteDeadline(time.Time{})

	select {
	case ts := <-resCh:
		return ts
	case <-time.After(15 * time.Second):
		log.Printf("Signal: timeout waiting for send result id=%d chat=%s", id, chatID)
		signalPendingSends.Delete(id)
		return 0
	}
}

// ─── Owner number auto-detection ─────────────────────────────────────────────

var signalOwnerNumber string

type signalAccountsFile struct {
	Accounts []struct {
		Number string `json:"number"`
	} `json:"accounts"`
}

func detectSignalOwnerNumber() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidates := []string{
		filepath.Join(home, ".local", "share", "signal-cli", "data", "accounts.json"),
	}
	candidates = append(candidates, filepath.Join(home, "Library", "Application Support", "signal-cli", "data", "accounts.json"))
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		candidates = append(candidates, filepath.Join(appdata, "signal-cli", "data", "accounts.json"))
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var af signalAccountsFile
		if err := json.Unmarshal(data, &af); err != nil {
			continue
		}
		for _, a := range af.Accounts {
			if strings.HasPrefix(a.Number, "+") {
				return a.Number
			}
		}
	}
	return ""
}

func InitOwnerNumber() {
	n := detectSignalOwnerNumber()
	if n == "" {
		n = os.Getenv("SIGNAL_OWNER_NUMBER")
	}
	if n == "" {
		log.Printf("Signal: owner number unknown — bridge commands and isFromMe unavailable until detected")
		return
	}
	signalOwnerNumber = n
	masked := n
	if len(n) > 6 {
		masked = n[:4] + strings.Repeat("*", len(n)-6) + n[len(n)-2:]
	}
	log.Printf("Signal: owner number detected: %s", masked)
}

// ─── Message router ───────────────────────────────────────────────────────────

// buildSignalSender creates a Sender for a given chatID.
func buildSignalSender(chatID string) *core.Sender {
	return &core.Sender{
		SendText: func(cid, text string) string {
			ts := sendSignalMessage(cid, text)
			if ts != 0 {
				return fmt.Sprintf("%d", ts)
			}
			return ""
		},
		SendMedia: func(cid, path string) {
			sendSignalMessage(cid, "📎 output file: "+path)
			sendSignalFile(cid, path)
		},
	}
}

// handleSignalMessage routes an envelope to the inbox as a RawMessage.
func handleSignalMessage(env signalEnvelope, inbox chan<- core.RawMessage) {
	if len(env.SyncMessage) > 0 && string(env.SyncMessage) != "null" {
		// SyncMessage is the "sent from another device" mirror — only valid when we are the sender.
		if env.Source != "" && signalOwnerNumber != "" && env.Source != signalOwnerNumber {
			log.Printf("Signal: syncMessage from non-self source %s, skipping", env.Source)
			return
		}
		var sync signalSyncMessage
		if err := json.Unmarshal(env.SyncMessage, &sync); err != nil {
			log.Printf("Signal: syncMessage unmarshal error: %v", err)
			return
		}
		if sync.SentMessage == nil {
			return
		}
		emitSyncMessage(sync.SentMessage, inbox)
		return
	}

	if env.DataMessage == nil {
		return
	}
	emitDataMessage(env, inbox)
}

func emitSyncMessage(msg *signalSyncSentMessage, inbox chan<- core.RawMessage) {
	var chatID string
	if msg.GroupInfo != nil && msg.GroupInfo.GroupId != "" {
		chatID = msg.GroupInfo.GroupId
	} else {
		chatID = msg.Destination
	}
	if chatID == "" {
		log.Printf("Signal: sentMessage has no chatID — skipping")
		return
	}

	dedupeKey := fmt.Sprintf("%s:%d", chatID, msg.Timestamp)
	if signalMarkSeen(dedupeKey) {
		return
	}

	senderID := signalOwnerNumber
	if senderID == "" {
		senderID = chatID
	}

	textHint := msg.Message
	if msg.Reaction != nil {
		textHint = msg.Reaction.Emoji
	}

	sender := buildSignalSender(chatID)
	msgCopy := *msg

	inbox <- core.RawMessage{
		ChatID:    chatID,
		SenderID:  senderID,
		IsFromMe:  true,
		MessageID: fmt.Sprintf("%d", msg.Timestamp),
		TextHint:  textHint,
		Sender:    sender,
		Parse: func() core.Event {
			return parseSignalSyncEvent(chatID, senderID, &msgCopy, sender)
		},
	}
}

func parseSignalSyncEvent(chatID, senderID string, msg *signalSyncSentMessage, sender *core.Sender) core.Event {
	base := core.Event{ChatID: chatID, SenderID: senderID, IsFromMe: true}

	// Reaction
	if msg.Reaction != nil {
		r := msg.Reaction
		log.Printf("Signal sync reaction: emoji=%s remove=%v target=%d chat=%s", r.Emoji, r.IsRemove, r.TargetSentTimestamp, chatID)
		if r.IsRemove || r.Emoji == "" {
			return core.Event{Type: core.EventNone}
		}
		return core.Event{
			Type:        core.EventReaction,
			ChatID:      chatID,
			SenderID:    senderID,
			IsFromMe:    true,
			Emoji:       r.Emoji,
			QuotedMsgID: fmt.Sprintf("%d", r.TargetSentTimestamp),
		}
	}

	content := msg.Message

	// Quote context
	if msg.Quote != nil {
		quotedText := msg.Quote.Text
		if quotedText == "" {
			quotedText = "(no text)"
		}
		content = fmt.Sprintf("[Replying to: \"%s\"]\n\n%s", quotedText, content)
	}

	// Command
	if strings.HasPrefix(content, "!") {
		return core.Event{Type: core.EventCommand, ChatID: chatID, SenderID: senderID, IsFromMe: true, Text: content}
	}

	// Audio transcription
	if content == "" && len(msg.Attachments) > 0 {
		transcript, err := transcribeSignalVoice(msg.Attachments)
		if err != nil {
			log.Printf("Signal sync: voice transcription error: %v", err)
			sender.SendText(chatID, "⚠️ Could not transcribe voice message: "+err.Error())
			return core.Event{Type: core.EventNone}
		}
		if transcript != "" {
			content = "[🎤 Voice]: " + transcript
		}
	}

	// Non-audio attachment
	if content == "" && len(msg.Attachments) > 0 {
		path := resolveSignalAttachmentPath(msg.Attachments[0])
		if path == "" {
			log.Printf("Signal sync: attachment file not found for chat=%s — skipping", chatID)
			return core.Event{Type: core.EventNone}
		}
		ct := strings.ToLower(msg.Attachments[0].ContentType)
		if ct == "" {
			ct = "application/octet-stream"
		}
		return core.Event{
			Type:     core.EventAttachment,
			ChatID:   chatID,
			SenderID: senderID,
			IsFromMe: true,
			Text:     base.Text,
			Attachment: &core.Attachment{
				LocalPath: path,
				MimeType:  ct,
			},
		}
	}

	if content == "" {
		return core.Event{Type: core.EventNone}
	}

	log.Printf("Signal sync← (chat=%s): %s", chatID, content)
	core.StoreRecentMessage(chatID, fmt.Sprintf("%d", msg.Timestamp), content)
	setLastSignalActiveChat(chatID)

	return core.Event{Type: core.EventText, ChatID: chatID, SenderID: senderID, IsFromMe: true, Text: content}
}

func emitDataMessage(env signalEnvelope, inbox chan<- core.RawMessage) {
	dm := env.DataMessage

	var chatID string
	if dm.GroupInfo != nil && dm.GroupInfo.GroupId != "" {
		chatID = dm.GroupInfo.GroupId
	} else {
		chatID = env.Source
	}
	if chatID == "" {
		return
	}

	dedupeKey := fmt.Sprintf("%s:%d", chatID, dm.Timestamp)
	if signalMarkSeen(dedupeKey) {
		return
	}

	isFromMe := signalOwnerNumber != "" && env.Source == signalOwnerNumber

	textHint := dm.Message
	if dm.Reaction != nil {
		textHint = dm.Reaction.Emoji
	}

	sender := buildSignalSender(chatID)
	envCopy := env

	inbox <- core.RawMessage{
		ChatID:    chatID,
		SenderID:  env.Source,
		IsFromMe:  isFromMe,
		MessageID: fmt.Sprintf("%d", dm.Timestamp),
		TextHint:  textHint,
		Sender:    sender,
		Parse: func() core.Event {
			return parseSignalDataEvent(chatID, envCopy, isFromMe, sender)
		},
	}
}

func parseSignalDataEvent(chatID string, env signalEnvelope, isFromMe bool, sender *core.Sender) core.Event {
	dm := env.DataMessage

	// Reaction
	if dm.Reaction != nil {
		r := dm.Reaction
		log.Printf("Signal data reaction: emoji=%s remove=%v target=%d chat=%s", r.Emoji, r.IsRemove, r.TargetSentTimestamp, chatID)
		if r.IsRemove || r.Emoji == "" {
			return core.Event{Type: core.EventNone}
		}
		return core.Event{
			Type:        core.EventReaction,
			ChatID:      chatID,
			SenderID:    env.Source,
			IsFromMe:    isFromMe,
			Emoji:       r.Emoji,
			QuotedMsgID: fmt.Sprintf("%d", r.TargetSentTimestamp),
		}
	}

	content := dm.Message

	// Quote context
	if dm.Quote != nil {
		quotedText := dm.Quote.Text
		if quotedText == "" {
			quotedText = "(no text)"
		}
		content = fmt.Sprintf("[Replying to: \"%s\"]\n\n%s", quotedText, content)
	}

	log.Printf("Signal ← %s (chat=%s): %s", env.Source, chatID, content)

	// Command
	if strings.HasPrefix(content, "!") {
		return core.Event{Type: core.EventCommand, ChatID: chatID, SenderID: env.Source, IsFromMe: isFromMe, Text: content}
	}

	// Audio transcription
	if content == "" && len(dm.Attachments) > 0 {
		transcript, err := transcribeSignalVoice(dm.Attachments)
		if err != nil {
			log.Printf("Signal: voice transcription error: %v", err)
			sender.SendText(chatID, "⚠️ Could not transcribe voice message: "+err.Error())
			return core.Event{Type: core.EventNone}
		}
		if transcript != "" {
			content = "[🎤 Voice]: " + transcript
		}
	}

	// Non-audio attachment
	if content == "" && len(dm.Attachments) > 0 {
		path := resolveSignalAttachmentPath(dm.Attachments[0])
		if path == "" {
			log.Printf("Signal: attachment file not found for chat=%s — skipping", chatID)
			return core.Event{Type: core.EventNone}
		}
		ct := strings.ToLower(dm.Attachments[0].ContentType)
		if ct == "" {
			ct = "application/octet-stream"
		}
		return core.Event{
			Type:     core.EventAttachment,
			ChatID:   chatID,
			SenderID: env.Source,
			IsFromMe: isFromMe,
			Attachment: &core.Attachment{
				LocalPath: path,
				MimeType:  ct,
			},
		}
	}

	if content == "" {
		return core.Event{Type: core.EventNone}
	}

	core.StoreRecentMessage(chatID, fmt.Sprintf("%d", dm.Timestamp), content)
	setLastSignalActiveChat(chatID)

	return core.Event{Type: core.EventText, ChatID: chatID, SenderID: env.Source, IsFromMe: isFromMe, Text: content}
}

// ─── Heartbeat ────────────────────────────────────────────────────────────────

func signalHeartbeat(conn net.Conn, stop <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			ping := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      0,
				"method":  "version",
				"params":  map[string]interface{}{},
			}
			data, _ := json.Marshal(ping)
			data = append(data, '\n')
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := conn.Write(data); err != nil {
				log.Printf("Signal: heartbeat failed: %v — closing connection", err)
				conn.Close()
				return
			}
			conn.SetWriteDeadline(time.Time{})
		}
	}
}

// ─── Listener goroutine ───────────────────────────────────────────────────────

func StartListener(inbox chan<- core.RawMessage) {
	backoff := time.Second
	for {
		log.Printf("Signal: connecting to 127.0.0.1:7583...")
		conn, err := net.DialTimeout("tcp", "127.0.0.1:7583", 10*time.Second)
		if err != nil {
			log.Printf("Signal: connect failed: %v, retrying in %v", err, backoff)
			time.Sleep(backoff)
			backoff *= 2
			if backoff > 60*time.Second {
				backoff = 60 * time.Second
			}
			continue
		}

		log.Printf("Signal: connected to signal-cli daemon")
		backoff = time.Second
		core.SetChannelStatus("signal", core.ChannelState{Connected: true, AccountID: signalOwnerNumber})

		signalConnMu.Lock()
		signalConn = conn
		signalConnMu.Unlock()

		heartbeatStop := make(chan struct{})
		go signalHeartbeat(conn, heartbeatStop)

		decoder := json.NewDecoder(conn)
		for {
			conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			var msg signalRPCMessage
			if err := decoder.Decode(&msg); err != nil {
				log.Printf("Signal: read error: %v — reconnecting", err)
				core.SetChannelStatus("signal", core.ChannelState{Connected: false})
				break
			}
			conn.SetReadDeadline(time.Time{})

			if msg.Method == "receive" {
				var params signalReceiveParams
				if err := json.Unmarshal(msg.Params, &params); err != nil {
					log.Printf("Signal: failed to parse receive params: %v", err)
					continue
				}
				go handleSignalMessage(params.Envelope, inbox)
			} else if msg.Method == "" && msg.ID != nil && msg.ID != float64(0) {
				if len(msg.Error) > 0 && string(msg.Error) != "null" {
					log.Printf("Signal: RPC error id=%v: %s", msg.ID, string(msg.Error))
				}
				if len(msg.Result) > 0 && string(msg.Result) != "null" {
					var result struct {
						Timestamp int64 `json:"timestamp"`
					}
					if jerr := json.Unmarshal(msg.Result, &result); jerr == nil && result.Timestamp != 0 {
						if idFloat, ok := msg.ID.(float64); ok {
							rpcID := int64(idFloat)
							if ch, loaded := signalPendingSends.LoadAndDelete(rpcID); loaded {
								ch.(chan int64) <- result.Timestamp
							}
						}
					}
				}
			}
		}

		close(heartbeatStop)

		signalConnMu.Lock()
		if signalConn == conn {
			signalConn = nil
		}
		signalConnMu.Unlock()
		conn.Close()

		log.Printf("Signal: disconnected, retrying in %v", backoff)
		time.Sleep(backoff)
		backoff *= 2
		if backoff > 60*time.Second {
			backoff = 60 * time.Second
		}
	}
}
