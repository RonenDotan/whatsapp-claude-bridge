package core

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// BridgeDir returns the directory containing the bridge executable.
func BridgeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

// StoreDir returns the "store" directory next to the executable.
// Only whatsapp.db and messages.db live here (fixed location expected by whatsapp-mcp-server).
func StoreDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "store"
	}
	return filepath.Join(filepath.Dir(exe), "store")
}

// DataDir returns the runtime data directory for all user state.
// Reads WHATSAPP_BRIDGE_DATA_DIR env var; defaults to bridge-data/ next to the bridge directory.
func DataDir() string {
	if d := os.Getenv("WHATSAPP_BRIDGE_DATA_DIR"); d != "" {
		return d
	}
	return filepath.Join(filepath.Dir(BridgeDir()), "bridge-data")
}

// ConfigDir returns the config directory next to the bridge executable (committed to git).
func ConfigDir() string {
	return filepath.Join(BridgeDir(), "config")
}

// SanitizeChatID returns a filesystem-safe version of chatID.
func SanitizeChatID(chatID string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, chatID)
}

// ChatDir returns the per-chat working directory path.
func ChatDir(chatID string) string {
	return filepath.Join(DataDir(), "chats", SanitizeChatID(chatID))
}

// EnsureChatDir creates the per-chat directory and returns its path.
func EnsureChatDir(chatID string) (string, error) {
	dir := ChatDir(chatID)
	return dir, os.MkdirAll(dir, 0755)
}

// EnsureChatClaudeSettings copies the permission template into
// <chatDir>/.claude/settings.local.json if it does not already exist.
// Uses the chat's stored permission level; defaults to god (bypassPermissions).
func EnsureChatClaudeSettings(chatID string) {
	claudeSubdir := filepath.Join(ChatDir(chatID), ".claude")
	if err := os.MkdirAll(claudeSubdir, 0755); err != nil {
		log.Printf("EnsureChatClaudeSettings: failed to create .claude dir for %s: %v", chatID, err)
		return
	}
	target := filepath.Join(claudeSubdir, "settings.local.json")
	if _, err := os.Stat(target); err == nil {
		return // already written (by ApplyPermission or a previous run)
	}
	p := GetChatPermission(chatID)
	if err := applyClaudePermission(chatID, p); err != nil {
		log.Printf("EnsureChatClaudeSettings: %v", err)
	}
}

// EnsureChatCodexConfig copies the permission template into
// <chatDir>/.codex/config.toml if it does not already exist.
func EnsureChatCodexConfig(chatID string) {
	codexSubdir := filepath.Join(ChatDir(chatID), ".codex")
	if err := os.MkdirAll(codexSubdir, 0755); err != nil {
		log.Printf("EnsureChatCodexConfig: failed to create .codex dir for %s: %v", chatID, err)
		return
	}
	target := filepath.Join(codexSubdir, "config.toml")
	if _, err := os.Stat(target); err == nil {
		return // already written
	}
	p := GetChatPermission(chatID)
	if err := applyCodexPermission(chatID, p); err != nil {
		log.Printf("EnsureChatCodexConfig: %v", err)
	}
}
