package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// DefaultAllowedChat and CodexGroupJID are the built-in chats pre-loaded on first run.
const (
	DefaultAllowedChat = "120363409956054412@g.us"
	CodexGroupJID      = "120363407895179577@g.us"
)

var (
	allowedChatsFile = filepath.Join(DataDir(), "chats.json")
	allowedChats     map[string]string // chatID → "claude" | "codex"
	allowedChatsMu   sync.RWMutex
)

func loadAllowedChats() map[string]string {
	data, err := os.ReadFile(allowedChatsFile)
	if err != nil {
		return map[string]string{
			DefaultAllowedChat: "claude",
			CodexGroupJID:      "codex",
		}
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil || len(m) == 0 {
		return map[string]string{
			DefaultAllowedChat: "claude",
			CodexGroupJID:      "codex",
		}
	}
	return m
}

// InitAllowedChats loads the unified allowed-chat list from bridge-data/chats.json.
// Call once at startup; replaces the old Init*AllowedChats calls.
func InitAllowedChats() {
	os.MkdirAll(DataDir(), 0755)
	allowedChatsMu.Lock()
	allowedChats = loadAllowedChats()
	allowedChatsMu.Unlock()
}

// IsAllowedChat reports whether chatID is in the allowed list.
func IsAllowedChat(chatID string) bool {
	allowedChatsMu.RLock()
	_, ok := allowedChats[chatID]
	allowedChatsMu.RUnlock()
	return ok
}

// GetChatLLM returns "claude", "codex", or "" if the chat is not in the allowed list.
func GetChatLLM(chatID string) string {
	allowedChatsMu.RLock()
	llm := allowedChats[chatID]
	allowedChatsMu.RUnlock()
	return llm
}

// IsCodexChat reports whether chatID routes to Codex.
func IsCodexChat(chatID string) bool {
	return GetChatLLM(chatID) == "codex"
}

// AddChatToAllowed adds chatID to the allowed list with the given LLM ("claude" or "codex").
func AddChatToAllowed(chatID, llm string) {
	allowedChatsMu.Lock()
	allowedChats[chatID] = llm
	allowedChatsMu.Unlock()
}

// RemoveChatFromAllowed removes chatID from the allowed list.
func RemoveChatFromAllowed(chatID string) {
	allowedChatsMu.Lock()
	delete(allowedChats, chatID)
	allowedChatsMu.Unlock()
}

// GetAllowedChats returns a snapshot of the current allowed-chat map.
func GetAllowedChats() map[string]string {
	allowedChatsMu.RLock()
	defer allowedChatsMu.RUnlock()
	out := make(map[string]string, len(allowedChats))
	for k, v := range allowedChats {
		out[k] = v
	}
	return out
}

// SaveAllowedChats persists the current allowed list to bridge-data/chats.json.
func SaveAllowedChats() error {
	allowedChatsMu.RLock()
	data, err := json.MarshalIndent(allowedChats, "", "  ")
	allowedChatsMu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(allowedChatsFile, data, 0644)
}
