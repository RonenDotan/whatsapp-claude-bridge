package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var personalityPrompts map[string]string

func init() {
	personalityPrompts = loadPersonalityTemplates()
}

// loadPersonalityTemplates reads personality templates from config/templates/.
func loadPersonalityTemplates() map[string]string {
	dir := filepath.Join(configDir(), "templates", "personalities")
	presets := []string{"kids", "pro", "creative", "default"}
	m := make(map[string]string, len(presets))
	for _, name := range presets {
		data, err := os.ReadFile(filepath.Join(dir, name+".md"))
		if err != nil {
			log.Printf("personalities: could not load template %s: %v", name, err)
			m[name] = ""
			continue
		}
		m[name] = string(data)
	}
	return m
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
	personalitiesFile = filepath.Join(dataDir(), "chat_personalities.json")
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
	prompt := strings.TrimRight(personalityPrompts[preset], "\n")
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
	const newPrefix = "You MUST begin EVERY response with the "
	const oldPrefix = "Always start every response with the emoji "
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, newPrefix) {
			return trimmed
		}
		if strings.HasPrefix(trimmed, oldPrefix) {
			emoji := strings.TrimPrefix(trimmed, oldPrefix)
			return newPrefix + emoji + " emoji. This is mandatory. Never skip it."
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

// setIconForChat writes/updates the icon instruction in the appropriate context file.
func setIconForChat(chatID, emoji string) error {
	dir, err := ensureChatDir(chatID)
	if err != nil {
		return err
	}
	const newPrefix = "You MUST begin EVERY response with the "
	const oldPrefix = "Always start every response with the emoji "
	newLine := newPrefix + emoji + " emoji. This is mandatory. Never skip it."
	filename := "CLAUDE.md"
	if isCodexChat(chatID) || isSignalCodexChat(chatID) {
		filename = "AGENTS.md"
	}
	filePath := filepath.Join(dir, filename)
	// Remove any legacy icon lines before upserting new format.
	if data, readErr := os.ReadFile(filePath); readErr == nil {
		lines := strings.Split(string(data), "\n")
		var filtered []string
		for _, l := range lines {
			if !strings.HasPrefix(strings.TrimSpace(l), oldPrefix) {
				filtered = append(filtered, l)
			}
		}
		_ = os.WriteFile(filePath, []byte(strings.Join(filtered, "\n")), 0644)
	}
	return upsertContextFileLine(filePath, newPrefix, newLine)
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
