package main

import (
	"fmt"
	"log"
	"os"
	"strings"

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
	log.Printf("Starting bridge: WhatsApp=%v Signal=%v", settings.WhatsAppEnabled, settings.SignalEnabled)

	core.InitAllowedChats()
	core.InitCodexAllowedChats()
	core.InitSignalAllowedChats()
	core.InitSignalCodexAllowedChats()
	core.InitChatPersonalities()

	inbox := make(chan core.IncomingMessage, 32)

	if settings.WhatsAppEnabled {
		go whatsapp.Start(inbox)
	}
	if settings.SignalEnabled {
		signal.InitOwnerNumber()
		go signal.StartListener(inbox)
	}

	dispatch(inbox)
}

// dispatch is the main loop: reads messages from all channels and routes them to the right LLM.
func dispatch(inbox <-chan core.IncomingMessage) {
	for msg := range inbox {
		m := msg
		go func() {
			if core.IsLooping(m.ChatID, m.Text) {
				m.Reply("⚠️ You've sent the same message several times. Try rephrasing or type 'clear session' to start fresh.")
				return
			}
			core.AddToInputHistory(m.ChatID, m.Text)

			send := func(reply string) {
				if id := m.Reply(reply); id != "" {
					core.StoreRecentMessage(m.ChatID, id, reply)
				}
			}

			if strings.ToLower(strings.TrimSpace(m.Text)) == "!stats" {
				send(statsReply(m))
				return
			}

			if m.Attachment != nil {
				var reply string
				var err error
				if m.IsCodexChat {
					reply, err = codex.NewCodexLLM().ProcessWithAttachment(m.ChatID, m.Text, m.Attachment)
				} else {
					reply, err = claude.NewClaudeLLM().ProcessWithAttachment(m.ChatID, m.Text, m.Attachment)
				}
				if err != nil {
					m.Reply("⚠️ Could not process attachment: " + err.Error())
					return
				}
				send(reply)
				return
			}

			if m.IsCodexChat {
				codex.HandleWithCodex(m.ChatID, m.Text, send, m.ReplyMedia)
			} else {
				claude.HandleWithClaude(m.ChatID, m.Text, send, m.ReplyMedia)
			}
		}()
	}
}

func statsReply(m core.IncomingMessage) string {
	if m.IsCodexChat {
		s, ok := core.GetCodexStats(m.ChatID)
		if !ok {
			return "No stats yet — send a message first."
		}
		return fmt.Sprintf(
			"📊 Codex stats:\n• Input tokens: %d\n• Output tokens: %d\n• Total tokens: %d\n• Last updated: %s",
			s.InputTokens, s.OutputTokens, s.TotalTokens, s.LastUpdated)
	}
	s, ok := core.GetUsageStats(m.ChatID)
	if !ok {
		return "No stats yet — send a message first."
	}
	durationSec := float64(s.DurationMs) / 1000.0
	reply := fmt.Sprintf(
		"📊 Stats for this session:\n• Cache read: %d tokens\n• Cache write: %d tokens\n• Input tokens: %d\n• Output tokens: %d\n• Total cost: $%.4f USD\n• Response time: %.1fs\n• Last updated: %s",
		s.CacheReadTokens, s.CacheWriteTokens,
		s.InputTokens, s.OutputTokens,
		s.TotalCostUSD, durationSec, s.LastUpdated)
	if len(s.ModelUsage) > 1 {
		reply += "\n\nPer-model breakdown:"
		for model, mu := range s.ModelUsage {
			reply += fmt.Sprintf("\n• %s: %d in / %d out, $%.4f", model, mu.InputTokens, mu.OutputTokens, mu.CostUSD)
		}
	}
	return reply
}
