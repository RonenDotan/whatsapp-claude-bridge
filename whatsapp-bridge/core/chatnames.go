package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

var (
	chatNamesMu sync.RWMutex
	chatNamesMap = map[string]string{}
)

func InitChatNames() {
	data, err := os.ReadFile(filepath.Join(DataDir(), "chat_names.json"))
	if err != nil {
		return
	}
	chatNamesMu.Lock()
	json.Unmarshal(data, &chatNamesMap)
	chatNamesMu.Unlock()
}

func SetChatName(chatID, name string) {
	if chatID == "" || name == "" {
		return
	}
	chatNamesMu.Lock()
	old := chatNamesMap[chatID]
	chatNamesMap[chatID] = name
	chatNamesMu.Unlock()
	if old != name {
		go func() {
			chatNamesMu.RLock()
			data, _ := json.MarshalIndent(chatNamesMap, "", "  ")
			chatNamesMu.RUnlock()
			os.WriteFile(filepath.Join(DataDir(), "chat_names.json"), data, 0644)
		}()
	}
}

func GetChatName(chatID string) string {
	chatNamesMu.RLock()
	n := chatNamesMap[chatID]
	chatNamesMu.RUnlock()
	return n
}
