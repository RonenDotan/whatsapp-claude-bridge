package core

import (
	"fmt"
	"strings"
)

// CommandResult describes what main should do after a bridge command is handled.
// HandleBridgeCommand is a pure function — it never mutates shared state directly.
type CommandResult struct {
	Reply             string // text to send back to the chat; "" means no reply
	InvalidateProps   bool   // evict ChatProperties cache for this chat
	ClearSession      bool   // clear LLM session and input history
	RemoveFromAllowed bool   // remove chat from the allowed list
}

// HandleBridgeCommand processes a bridge command (any message starting with "!").
// The caller (main) acts on the returned CommandResult for all state mutations.
// Returns a zero CommandResult if the text is not a recognised bridge command.
func HandleBridgeCommand(chatID, text string, isFromMe bool, props ChatProperties) CommandResult {
	cmd := strings.TrimSpace(text)

	isPersonality := strings.HasPrefix(cmd, "!set-personality")
	isIcon := strings.HasPrefix(cmd, "!set-icon")

	// Identify known commands before checking isFromMe so we can return the
	// "owner only" error rather than silently ignoring them.
	known := false
	switch cmd {
	case "!help", "!version", "!cancel", "!clear-session", "!stats",
		"!remove-claude", "!remove-codex":
		known = true
	}
	if !known && !isPersonality && !isIcon {
		return CommandResult{} // not a bridge command
	}

	if !isFromMe {
		return CommandResult{Reply: "⚠️ Only the bridge owner can use bridge commands"}
	}

	// ── Stateless commands ────────────────────────────────────────────────────

	switch cmd {
	case "!help":
		return CommandResult{Reply: "Bridge commands:\n" +
			"!meet-claude — connect this chat to Claude\n" +
			"!meet-codex — connect this chat to Codex\n" +
			"!remove-claude / !remove-codex — disconnect this chat\n" +
			"!clear-session — clear session memory and start fresh\n" +
			"!cancel — cancel the currently running request\n" +
			"!set-personality <preset> — set personality (default / kids / pro / creative)\n" +
			"!set-icon <emoji> — prefix every response with an emoji\n" +
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
			"(any other emoji) — send reaction context to the LLM"}

	case "!version":
		return CommandResult{Reply: "Bridge version: " + BridgeVersion}

	case "!cancel":
		if CancelRunning(chatID) {
			return CommandResult{Reply: "🛑 Cancelled."}
		}
		return CommandResult{Reply: "Nothing is currently running."}

	case "!stats":
		return CommandResult{Reply: buildStatsReply(chatID, props)}

	case "!clear-session":
		return CommandResult{
			Reply:        "✅ Session cleared for this chat. Next message starts fresh.",
			ClearSession: true,
		}

	case "!remove-claude", "!remove-codex":
		return CommandResult{
			Reply:             "✅ This chat has been disconnected from the bridge.",
			RemoveFromAllowed: true,
			ClearSession:      true,
		}
	}

	// ── !set-personality ──────────────────────────────────────────────────────

	if isPersonality {
		parts := strings.Fields(cmd)
		if len(parts) < 2 {
			return CommandResult{Reply: fmt.Sprintf(
				"Current personality: %s\nAvailable: default, kids, pro, creative",
				props.Personality)}
		}
		preset := parts[1]
		switch preset {
		case "default", "kids", "pro", "creative":
			if err := SetChatPersonality(chatID, preset); err != nil {
				return CommandResult{Reply: "⚠️ Failed to save personality: " + err.Error()}
			}
			return CommandResult{
				Reply:           fmt.Sprintf("✅ Personality set to: %s (session reset — changes take effect now)", preset),
				InvalidateProps: true,
				ClearSession:    true,
			}
		default:
			return CommandResult{Reply: "⚠️ Unknown preset. Available: default, kids, pro, creative"}
		}
	}

	// ── !set-icon ─────────────────────────────────────────────────────────────

	if isIcon {
		parts := strings.Fields(cmd)
		if len(parts) < 2 {
			return CommandResult{Reply: "Usage: !set-icon <emoji>"}
		}
		emoji := parts[1]
		if err := SetIconForChat(chatID, emoji); err != nil {
			return CommandResult{Reply: "⚠️ Failed to set icon: " + err.Error()}
		}
		return CommandResult{
			Reply:           fmt.Sprintf("✅ Icon set to: %s (session reset — changes take effect now)", emoji),
			InvalidateProps: true,
			ClearSession:    true,
		}
	}

	return CommandResult{}
}

func buildStatsReply(chatID string, props ChatProperties) string {
	if props.SelectedLLM == "codex" {
		s, ok := GetCodexStats(chatID)
		if !ok {
			return "No stats yet — send a message first."
		}
		return fmt.Sprintf(
			"📊 Codex stats:\n• Input tokens: %d\n• Output tokens: %d\n• Total tokens: %d\n• Last updated: %s",
			s.InputTokens, s.OutputTokens, s.TotalTokens, s.LastUpdated)
	}
	s, ok := GetUsageStats(chatID)
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
