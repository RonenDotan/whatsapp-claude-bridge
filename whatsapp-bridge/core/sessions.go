package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

var (
	sessionsMu        sync.Mutex
	sessionsFile      = filepath.Join(DataDir(), "sessions.json")
	codexSessionsFile = filepath.Join(DataDir(), "codex_sessions.json")
)

func LoadSessions() map[string]string {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	data, err := os.ReadFile(sessionsFile)
	if err != nil {
		return make(map[string]string)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return make(map[string]string)
	}
	return m
}

func SaveSession(jid, sessionID string) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	m := make(map[string]string)
	if data, err := os.ReadFile(sessionsFile); err == nil {
		json.Unmarshal(data, &m)
	}
	m[jid] = sessionID
	data, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(sessionsFile, data, 0644)
}

func LoadCodexSessions() map[string]string {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	data, err := os.ReadFile(codexSessionsFile)
	if err != nil {
		return make(map[string]string)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return make(map[string]string)
	}
	return m
}

func SaveCodexSession(jid, threadID string) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	m := make(map[string]string)
	if data, err := os.ReadFile(codexSessionsFile); err == nil {
		json.Unmarshal(data, &m)
	}
	m[jid] = threadID
	data, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(codexSessionsFile, data, 0644)
}

func DeleteSession(jid string) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	m := make(map[string]string)
	if data, err := os.ReadFile(sessionsFile); err == nil {
		json.Unmarshal(data, &m)
	}
	delete(m, jid)
	data, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(sessionsFile, data, 0644)
}

func DeleteCodexSession(jid string) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	m := make(map[string]string)
	if data, err := os.ReadFile(codexSessionsFile); err == nil {
		json.Unmarshal(data, &m)
	}
	delete(m, jid)
	data, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(codexSessionsFile, data, 0644)
}

func ClearSessionData(chatID string) {
	DeleteSession(chatID)
	DeleteCodexSession(chatID)
	ClearInputHistory(chatID)
}
