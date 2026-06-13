package core

import (
	"fmt"
	"strings"
)

// ChannelHooks wires channel-specific operations into the shared command handler.
type ChannelHooks struct {
	Send               func(string)
	AddAllowed         func(string)
	RemoveAllowed      func(string)
	SaveAllowed        func() error
	AddCodexAllowed    func(string)
	RemoveCodexAllowed func(string)
	SaveCodexAllowed   func() error
}

// HandleBridgeCommand processes a bridge command (any message starting with !)
// using the provided channel hooks for send and whitelist operations.
// Returns true if the message was a bridge command (handled or rejected), false otherwise.
func HandleBridgeCommand(chatID, content string, isFromMe bool, h ChannelHooks) bool {
	cmd := strings.TrimSpace(content)
	isPersonality := strings.HasPrefix(cmd, "!set-personality")
	isIcon := strings.HasPrefix(cmd, "!set-icon")

	switch cmd {
	case "!meet-claude", "!meet-codex", "!remove-claude", "!remove-codex",
		"!help", "!clear-session", "!cancel", "!version":
	default:
		if !isPersonality && !isIcon {
			return false
		}
	}

	if !isFromMe {
		h.Send("⚠️ Only the bridge owner can use bridge commands")
		return true
	}

	switch cmd {
	case "!help":
		h.Send("Bridge commands:\n" +
			"!meet-claude — add this chat to Claude whitelist\n" +
			"!remove-claude — remove this chat from Claude whitelist\n" +
			"!meet-codex — add this chat to Codex whitelist\n" +
			"!remove-codex — remove this chat from Codex whitelist\n" +
			"!clear-session — clear Claude/Codex session memory and start fresh\n" +
			"!cancel — cancel the currently running request\n" +
			"!set-personality <preset> — set personality (default / kids / pro / creative)\n" +
			"!stats — show token usage and cost for this session\n" +
			"!version — show bridge version\n" +
			"!help — show this help screen\n" +
			"\nReactions (react to any message):\n" +
			"🔊🔈📢🔉🗣️📣🎤🎙️🎧 — read aloud and save as mp3\n" +
			"📝 — summarize\n" +
			"🔥 — expand with more detail\n" +
			"❓ — explain in simple terms\n" +
			"🌍 — translate to English\n" +
			"🇮🇱 — translate to Hebrew\n" +
			"✅ — extract action items\n" +
			"(any other emoji) — send reaction context to the LLM")

	case "!version":
		h.Send("Bridge version: " + BridgeVersion)

	case "!cancel":
		if CancelRunning(chatID) {
			h.Send("🛑 Cancelled.")
		} else {
			h.Send("Nothing is currently running.")
		}

	case "!meet-claude":
		h.AddAllowed(chatID)
		if err := h.SaveAllowed(); err != nil {
			h.Send("⚠️ Failed to save whitelist: " + err.Error())
			return true
		}
		EnsureChatClaudeSettings(chatID)
		h.Send("👋 Hi! I'm Claude. This chat is now connected to me — send any message to get started.")

	case "!meet-codex":
		h.AddCodexAllowed(chatID)
		if err := h.SaveCodexAllowed(); err != nil {
			h.Send("⚠️ Failed to save whitelist: " + err.Error())
			return true
		}
		EnsureChatClaudeSettings(chatID)
		h.Send("👋 Hi! I'm Codex. This chat is now connected to me — send any message to get started.")

	case "!remove-claude":
		h.RemoveAllowed(chatID)
		if err := h.SaveAllowed(); err != nil {
			h.Send("⚠️ Failed to save whitelist: " + err.Error())
			return true
		}
		h.Send("✅ Claude has left this chat.")

	case "!remove-codex":
		h.RemoveCodexAllowed(chatID)
		if err := h.SaveCodexAllowed(); err != nil {
			h.Send("⚠️ Failed to save whitelist: " + err.Error())
			return true
		}
		h.Send("✅ Codex has left this chat.")

	case "!clear-session":
		sessions := LoadSessions()
		codexSessions := LoadCodexSessions()
		_, hasSession := sessions[chatID]
		_, hasCodexSession := codexSessions[chatID]
		if !hasSession && !hasCodexSession {
			h.Send("No active session to clear.")
			return true
		}
		ClearSessionData(chatID)
		h.Send("✅ Session cleared for this chat. Next message starts fresh.")
	}

	if isPersonality {
		parts := strings.Fields(cmd)
		if len(parts) < 2 {
			h.Send(fmt.Sprintf("Current personality: %s\nAvailable: default, kids, pro, creative", GetChatPersonality(chatID)))
			return true
		}
		preset := parts[1]
		switch preset {
		case "default", "kids", "pro", "creative":
			if err := SetChatPersonality(chatID, preset); err != nil {
				h.Send("⚠️ Failed to save personality: " + err.Error())
				return true
			}
			ClearSessionData(chatID)
			h.Send(fmt.Sprintf("✅ Personality set to: %s (session reset — changes take effect now)", preset))
		default:
			h.Send("⚠️ Unknown preset. Available: default, kids, pro, creative")
		}
	}

	if isIcon {
		parts := strings.Fields(cmd)
		if len(parts) < 2 {
			h.Send("Usage: !set-icon <emoji>")
			return true
		}
		emoji := parts[1]
		if err := SetIconForChat(chatID, emoji); err != nil {
			h.Send("⚠️ Failed to set icon: " + err.Error())
			return true
		}
		ClearSessionData(chatID)
		h.Send(fmt.Sprintf("✅ Icon set to: %s (session reset — changes take effect now)", emoji))
	}

	return true
}
