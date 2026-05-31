package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var personalityPrompts = map[string]string{
	"kids":     "You are a super fun and playful assistant talking to an 8-year-old boy. Keep every response very short (2 sentences max). Use simple words only. Be enthusiastic and encouraging. No complex explanations.",
	"pro":      "You are a professional assistant. Be concise and direct. No unnecessary filler words. Get to the point immediately.",
	"creative": "You are a creative and imaginative assistant. Be expressive, use vivid language, and make responses engaging and story-like when appropriate.",
}

// WhisperConfig holds per-chat Whisper transcription overrides.
type WhisperConfig struct {
	Language      string `json:"language"`
	InitialPrompt string `json:"initial_prompt"`
}

// personalityWhisper maps preset names to optional Whisper config overrides.
// Presets not listed here use no override (nil).
var personalityWhisper = map[string]*WhisperConfig{
	"kids": {
		Language:      "he",
		InitialPrompt: "ילד בן 8 מדבר בעברית ובאנגלית. שיחה יומיומית, משחקים, בית ספר.",
	},
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
	if err := saveChatPersonalities(); err != nil {
		return err
	}
	if err := saveWhisperPrompt(chatID, preset); err != nil {
		return err
	}
	return writePersonalityContextFile(chatID, preset)
}

// writePersonalityContextFile writes the personality prompt to CLAUDE.md or AGENTS.md
// in the chat dir, preserving any existing !set-icon line.
func writePersonalityContextFile(chatID, preset string) error {
	dir, err := ensureChatDir(chatID)
	if err != nil {
		return err
	}
	prompt := personalityPrompts[preset]
	filename := "CLAUDE.md"
	if isCodexChat(chatID) || isSignalCodexChat(chatID) {
		filename = "AGENTS.md"
	}
	filePath := filepath.Join(dir, filename)
	existingData, _ := os.ReadFile(filePath)
	iconLine := extractIconLine(string(existingData))
	var content string
	switch {
	case prompt != "" && iconLine != "":
		content = prompt + "\n\n" + iconLine + "\n"
	case prompt != "":
		content = prompt + "\n"
	case iconLine != "":
		content = iconLine + "\n"
	default:
		os.Remove(filePath)
		return nil
	}
	return os.WriteFile(filePath, []byte(content), 0644)
}

func extractIconLine(content string) string {
	const prefix = "Always start every response with the emoji "
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// upsertContextFileLine replaces the first line starting with linePrefix, or appends newLine.
func upsertContextFileLine(filePath, linePrefix, newLine string) error {
	data, _ := os.ReadFile(filePath)
	content := string(data)
	lines := strings.Split(content, "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), linePrefix) {
			lines[i] = newLine
			found = true
			break
		}
	}
	var result string
	if !found {
		trimmed := strings.TrimRight(content, "\n")
		if trimmed == "" {
			result = newLine + "\n"
		} else {
			result = trimmed + "\n" + newLine + "\n"
		}
	} else {
		result = strings.Join(lines, "\n")
		if !strings.HasSuffix(result, "\n") {
			result += "\n"
		}
	}
	return os.WriteFile(filePath, []byte(result), 0644)
}

// setIconForChat writes/updates the icon instruction in both CLAUDE.md and AGENTS.md.
func setIconForChat(chatID, emoji string) error {
	dir, err := ensureChatDir(chatID)
	if err != nil {
		return err
	}
	const prefix = "Always start every response with the emoji "
	line := prefix + emoji
	for _, filename := range []string{"CLAUDE.md", "AGENTS.md"} {
		if err := upsertContextFileLine(filepath.Join(dir, filename), prefix, line); err != nil {
			return err
		}
	}
	return nil
}

// saveWhisperPrompt writes or removes whisper_prompt.json in the chat's dir.
func saveWhisperPrompt(chatID, preset string) error {
	cfg, ok := personalityWhisper[preset]
	dir, err := ensureChatDir(chatID)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "whisper_prompt.json")
	if !ok || cfg == nil {
		os.Remove(path)
		return nil
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// getWhisperConfigForChat reads whisper_prompt.json from the chat's dir.
// Returns nil if not present or unreadable.
func getWhisperConfigForChat(chatID string) *WhisperConfig {
	dir := chatDir(chatID)
	data, err := os.ReadFile(filepath.Join(dir, "whisper_prompt.json"))
	if err != nil {
		return nil
	}
	var cfg WhisperConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return &cfg
}

// getPersonalityPrompt returns the system prompt for the chat's personality preset,
// or "" if the preset is "default" or unset.
func getPersonalityPrompt(chatID string) string {
	return personalityPrompts[getChatPersonality(chatID)]
}
