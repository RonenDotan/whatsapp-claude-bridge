package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"whatsapp-client/channels/signal"
	"whatsapp-client/channels/whatsapp"
	"whatsapp-client/core"
	"whatsapp-client/llms/claude"
	"whatsapp-client/llms/codex"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version" || os.Args[1] == "-v") {
		fmt.Println(Version)
		os.Exit(0)
	}

	core.BridgeVersion = Version
	settings := core.LoadSettings()
	log.Printf("Starting bridge: WhatsApp=%v Signal=%v RestAPI=%v", settings.WhatsAppEnabled, settings.SignalEnabled, settings.RestAPIEnabled)

	core.InitAllowedChats()
	core.InitChatPersonalities()

	inbox := make(chan core.RawMessage, 32)

	if settings.WhatsAppEnabled {
		go whatsapp.Start(inbox, settings.RestAPIEnabled)
	}
	if settings.SignalEnabled {
		signal.InitOwnerNumber()
		go signal.StartListener(inbox)
	}

	dispatch(inbox)
}

// chatMutexes gates LLM subprocess calls per chat — bridge commands bypass this.
var (
	chatMutexesMu sync.Mutex
	chatMutexes   = map[string]*sync.Mutex{}
)

func getChatMutex(chatID string) *sync.Mutex {
	chatMutexesMu.Lock()
	defer chatMutexesMu.Unlock()
	if m, ok := chatMutexes[chatID]; ok {
		return m
	}
	m := &sync.Mutex{}
	chatMutexes[chatID] = m
	return m
}

// dispatch is the main loop: reads RawMessages from all channels and routes them.
func dispatch(inbox <-chan core.RawMessage) {
	for r := range inbox {
		raw := r
		go func() {
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("PANIC in dispatch for chat=%s: %v", raw.ChatID, rec)
				}
			}()
			processMessage(raw)
		}()
	}
}

// processMessage runs the full filter + parse + route pipeline for one message.
func processMessage(r core.RawMessage) {
	// 1. Introduction check — catch !meet-* before allowed-list filter.
	//    Only the owner can introduce a chat (IsFromMe).
	if r.IsFromMe {
		hint := strings.TrimSpace(r.TextHint)
		if strings.HasPrefix(hint, "!meet-claude") {
			introduceChat(r, "claude")
			return
		}
		if strings.HasPrefix(hint, "!meet-codex") {
			introduceChat(r, "codex")
			return
		}
	}

	// 2. Allowed-list check.
	if !core.IsAllowedChat(r.ChatID) {
		return
	}

	// 3. Parse — may do network I/O (audio transcription, media download).
	evt := r.Parse()
	if evt.Type == core.EventNone {
		return
	}

	// 4. Route by event type.
	switch evt.Type {
	case core.EventCommand:
		handleCommand(evt, r.Sender)
	case core.EventReaction:
		handleReaction(evt, r.Sender)
	case core.EventText, core.EventAttachment:
		// Loop detection applies only to LLM-bound messages, not bridge commands.
		if core.IsLooping(r.ChatID, evt.Text) {
			r.Sender.SendText(r.ChatID, "⚠️ You've sent the same message several times. Try rephrasing or send `!clear-session` to start fresh.")
			return
		}
		core.AddToInputHistory(r.ChatID, evt.Text)
		handleLLM(evt, r.Sender)
	}
}

// introduceChat adds a chat to the allowed list and writes its personality file.
func introduceChat(r core.RawMessage, llm string) {
	chatID := r.ChatID
	// Add to allowed list first so IsCodexChat() works in WritePersonalityContextFile.
	core.AddChatToAllowed(chatID, llm)
	if err := core.SaveAllowedChats(); err != nil {
		log.Printf("introduceChat: failed to save allowed chats: %v", err)
	}
	core.WritePersonalityContextFile(chatID, "default")
	log.Printf("Introduced chat %s as %s", chatID, llm)
	r.Sender.SendText(chatID, fmt.Sprintf("✅ Chat added with %s. Say hello!", strings.ToUpper(llm[:1])+llm[1:]))
}

// handleCommand executes a bridge command and applies side effects.
func handleCommand(evt core.Event, sender *core.Sender) {
	props := core.GetChatProperties(evt.ChatID)
	result := core.HandleBridgeCommand(evt.ChatID, evt.Text, evt.IsFromMe, props)

	if result.Reply != "" {
		if id := sender.SendText(evt.ChatID, result.Reply); id != "" {
			core.StoreRecentMessage(evt.ChatID, id, result.Reply)
		}
	}
	if result.ClearSession {
		core.DeleteSession(evt.ChatID)
		core.DeleteCodexSession(evt.ChatID)
		core.ClearInputHistory(evt.ChatID)
	}
	if result.InvalidateProps {
		core.InvalidateChatProperties(evt.ChatID)
	}
	if result.RemoveFromAllowed {
		core.RemoveChatFromAllowed(evt.ChatID)
		if err := core.SaveAllowedChats(); err != nil {
			log.Printf("handleCommand: failed to save allowed chats: %v", err)
		}
	}
}

// handleReaction resolves the target message and forwards a reaction prompt to the LLM.
func handleReaction(evt core.Event, sender *core.Sender) {
	text, found := core.LookupRecentMessage(evt.ChatID, evt.QuotedMsgID)
	if !found {
		sender.SendText(evt.ChatID, "⚠️ Can't react — message not in cache (too old or bridge was restarted).")
		return
	}
	prompt := core.LookupReactionPrompt(evt.Emoji, text)
	llmEvt := core.Event{
		Type:     core.EventText,
		ChatID:   evt.ChatID,
		SenderID: evt.SenderID,
		IsFromMe: evt.IsFromMe,
		Text:     prompt,
	}
	handleLLM(llmEvt, sender)
}

// handleLLM gates LLM subprocess calls per chat and sends the reply.
func handleLLM(evt core.Event, sender *core.Sender) {
	mu := getChatMutex(evt.ChatID)
	mu.Lock()
	defer mu.Unlock()

	props := core.GetChatProperties(evt.ChatID)
	llmID := props.SelectedLLM
	if llmID == "" {
		llmID = core.GetChatLLM(evt.ChatID)
	}

	send := func(reply string) {
		if id := sender.SendText(evt.ChatID, reply); id != "" {
			core.StoreRecentMessage(evt.ChatID, id, reply)
		}
	}

	chatDir, _ := core.EnsureChatDir(evt.ChatID)
	tracker := core.NewSnapshotTracker(chatDir)
	tracker.Before()

	var err error
	switch llmID {
	case "codex":
		if evt.Attachment != nil {
			var reply string
			reply, err = codex.NewCodexLLM().ProcessWithAttachment(evt.ChatID, evt.Text, evt.Attachment)
			if err == nil && reply != "" {
				send(reply)
			}
		} else {
			codex.HandleWithCodex(evt.ChatID, evt.Text, send)
		}
	default: // "claude" or unknown
		if evt.Attachment != nil {
			var reply string
			reply, err = claude.NewClaudeLLM().ProcessWithAttachment(evt.ChatID, evt.Text, evt.Attachment)
			if err == nil && reply != "" {
				send(reply)
			}
		} else {
			claude.HandleWithClaude(evt.ChatID, evt.Text, send)
		}
	}

	if err != nil {
		sender.SendText(evt.ChatID, "⚠️ Could not process: "+err.Error())
	}

	// Send any files the LLM wrote to the chat directory.
	for _, path := range tracker.After() {
		sender.SendMedia(evt.ChatID, path)
	}
}
