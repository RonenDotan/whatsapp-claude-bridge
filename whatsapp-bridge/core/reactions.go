package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func LookupReactionPrompt(emoji, text string) string {
	path := filepath.Join(ConfigDir(), "reaction_prompts.json")
	data, err := os.ReadFile(path)
	if err == nil {
		var m map[string]string
		if json.Unmarshal(data, &m) == nil {
			if tmpl, ok := m[emoji]; ok {
				return strings.ReplaceAll(tmpl, "{text}", text)
			}
		}
	}
	return fmt.Sprintf("User reacted with %s to your message:\n\n%s", emoji, text)
}
