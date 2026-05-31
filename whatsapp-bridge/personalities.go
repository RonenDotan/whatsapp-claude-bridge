package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

var personalityPrompts = map[string]string{
	"kids":     "You are a super fun and playful assistant talking to an 8-year-old boy. Keep every response very short (2-3 sentences max). Use simple words only. Be enthusiastic and encouraging. No complex explanations.",
	"pro":      "You are a professional assistant. Be concise and direct. No unnecessary filler words. Get to the point immediately.",
	"creative": "You are a creative and imaginative assistant. Be expressive, use vivid language, and make responses engaging and story-like when appropriate.",
}

var (
	personalitiesMu   sync.RWMutex
	personalitiesFile = filepath.Join(storeDir(), "chat_personalities.json")
	chatPersonalities map[string]string
)

func loadChatPersonalities() map[string]string {
	data, err := os.ReadFile(personalitiesFile)
	if err != nil {
		return make(map[string]string)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return make(map[string]string)
	}
	return m
}

func initChatPersonalities() {
	personalitiesMu.Lock()
	chatPersonalities = loadChatPersonalities()
	personalitiesMu.Unlock()
}

func saveChatPersonalities() error {
	personalitiesMu.RLock()
	data, err := json.MarshalIndent(chatPersonalities, "", "  ")
	personalitiesMu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(personalitiesFile, data, 0644)
}

func getChatPersonality(chatID string) string {
	personalitiesMu.RLock()
	defer personalitiesMu.RUnlock()
	if p, ok := chatPersonalities[chatID]; ok {
		return p
	}
	return "default"
}

func setChatPersonality(chatID, preset string) error {
	personalitiesMu.Lock()
	chatPersonalities[chatID] = preset
	personalitiesMu.Unlock()
	return saveChatPersonalities()
}

// getPersonalityPrompt returns the system prompt for the chat's personality preset,
// or "" if the preset is "default" or unset.
func getPersonalityPrompt(chatID string) string {
	return personalityPrompts[getChatPersonality(chatID)]
}
