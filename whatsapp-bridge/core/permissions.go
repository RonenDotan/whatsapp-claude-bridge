package core

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

type PermLevel string

const (
	PermStandard  PermLevel = "standard"
	PermDeveloper PermLevel = "developer"
	PermGod       PermLevel = "god"
	PermCustom    PermLevel = "custom"
)

type ChatPermission struct {
	Level       PermLevel `json:"level"`
	CustomAllow []string  `json:"custom_allow,omitempty"`
}

var (
	permMu   sync.RWMutex
	permMap  = map[string]ChatPermission{}
	permFile string
)

func InitPermissions() {
	permFile = filepath.Join(DataDir(), "chat_permissions.json")
	data, err := os.ReadFile(permFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[permissions] failed to read %s: %v", permFile, err)
		}
		return
	}
	permMu.Lock()
	defer permMu.Unlock()
	if err := json.Unmarshal(data, &permMap); err != nil {
		log.Printf("[permissions] failed to parse %s: %v", permFile, err)
	}
}

func GetChatPermission(chatID string) ChatPermission {
	permMu.RLock()
	defer permMu.RUnlock()
	if p, ok := permMap[chatID]; ok {
		return p
	}
	return ChatPermission{Level: PermGod}
}

func SetChatPermission(chatID string, p ChatPermission) {
	permMu.Lock()
	permMap[chatID] = p
	snapshot := make(map[string]ChatPermission, len(permMap))
	for k, v := range permMap {
		snapshot[k] = v
	}
	permMu.Unlock()

	go func() {
		data, err := json.MarshalIndent(snapshot, "", "  ")
		if err != nil {
			log.Printf("[permissions] marshal error: %v", err)
			return
		}
		if err := os.WriteFile(permFile, data, 0644); err != nil {
			log.Printf("[permissions] write error: %v", err)
		}
	}()
}

// ApplyPermission writes the appropriate config file for the chat's LLM and permission level.
// Call SetChatPermission and session-clear separately.
func ApplyPermission(chatID, llm string, p ChatPermission) error {
	switch llm {
	case "codex":
		return applyCodexPermission(chatID, p)
	default:
		return applyClaudePermission(chatID, p)
	}
}

func applyClaudePermission(chatID string, p ChatPermission) error {
	claudeDir := filepath.Join(ChatDir(chatID), ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("mkdir .claude: %w", err)
	}
	target := filepath.Join(claudeDir, "settings.local.json")

	var content []byte
	if p.Level == PermCustom {
		content = buildClaudeCustomJSON(p.CustomAllow)
	} else {
		tmpl := filepath.Join(ConfigDir(), "templates", "permissions", "claude-"+string(p.Level)+".json")
		data, err := os.ReadFile(tmpl)
		if err != nil {
			return fmt.Errorf("read template %s: %w", tmpl, err)
		}
		content = data
	}
	return os.WriteFile(target, content, 0644)
}

func buildClaudeCustomJSON(extra []string) []byte {
	standard := []string{"Read", "Glob", "Grep", "WebSearch", "WebFetch"}
	seen := map[string]bool{}
	tools := append([]string{}, standard...)
	for _, t := range tools {
		seen[t] = true
	}
	for _, t := range extra {
		if !seen[t] {
			tools = append(tools, t)
			seen[t] = true
		}
	}
	type perm struct {
		Allow []string `json:"allow"`
	}
	type doc struct {
		Permissions perm `json:"permissions"`
	}
	data, _ := json.MarshalIndent(doc{Permissions: perm{Allow: tools}}, "", "  ")
	return data
}

func applyCodexPermission(chatID string, p ChatPermission) error {
	codexDir := filepath.Join(ChatDir(chatID), ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		return fmt.Errorf("mkdir .codex: %w", err)
	}
	target := filepath.Join(codexDir, "config.toml")

	level := p.Level
	if level == PermCustom {
		level = PermDeveloper // custom → developer sandbox for Codex
	}
	tmpl := filepath.Join(ConfigDir(), "templates", "permissions", "codex-"+string(level)+".toml")
	data, err := os.ReadFile(tmpl)
	if err != nil {
		return fmt.Errorf("read template %s: %w", tmpl, err)
	}
	return os.WriteFile(target, data, 0644)
}
