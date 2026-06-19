package core

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// ChatProperties holds per-chat configuration.
// Cached in memory; persisted to bridge-data/chats/<chatID>/chat.json.
type ChatProperties struct {
	SelectedLLM string `json:"selected_llm"` // "claude" or "codex"
	Personality string `json:"personality"`  // "default", "kids", "pro", "creative"
	Icon        string `json:"icon"`
}

var (
	chatPropsMu    sync.RWMutex
	chatPropsCache = map[string]ChatProperties{}
)

// GetChatProperties returns cached ChatProperties for chatID.
// On first access it loads from chat.json, falling back to existing stores.
func GetChatProperties(chatID string) ChatProperties {
	chatPropsMu.RLock()
	p, ok := chatPropsCache[chatID]
	chatPropsMu.RUnlock()
	if ok {
		return p
	}
	p = loadChatPropertiesFromDisk(chatID)
	chatPropsMu.Lock()
	chatPropsCache[chatID] = p
	chatPropsMu.Unlock()
	return p
}

// SetChatProperties writes props to disk and updates the in-memory cache.
func SetChatProperties(chatID string, props ChatProperties) error {
	if err := saveChatPropertiesToDisk(chatID, props); err != nil {
		return err
	}
	chatPropsMu.Lock()
	chatPropsCache[chatID] = props
	chatPropsMu.Unlock()
	return nil
}

// InvalidateChatProperties evicts chatID from the in-memory cache.
// The next GetChatProperties call will reload from disk.
func InvalidateChatProperties(chatID string) {
	chatPropsMu.Lock()
	delete(chatPropsCache, chatID)
	chatPropsMu.Unlock()
}

func loadChatPropertiesFromDisk(chatID string) ChatProperties {
	path := filepath.Join(ChatDir(chatID), "chat.json")
	data, err := os.ReadFile(path)
	if err == nil {
		var p ChatProperties
		if json.Unmarshal(data, &p) == nil && p.SelectedLLM != "" {
			if p.Personality == "" {
				p.Personality = "default"
			}
			return p
		}
	}
	// Fallback: assemble from existing stores for chats that predate chat.json.
	llm := "claude"
	if IsCodexChat(chatID) {
		llm = "codex"
	}
	personality := GetChatPersonality(chatID) // from chat_personalities.json
	log.Printf("chatprops: assembled fallback for %s (llm=%s personality=%s)", chatID, llm, personality)
	return ChatProperties{SelectedLLM: llm, Personality: personality}
}

func saveChatPropertiesToDisk(chatID string, props ChatProperties) error {
	dir, err := EnsureChatDir(chatID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(props, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "chat.json"), data, 0644)
}
