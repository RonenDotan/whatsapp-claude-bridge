package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const DefaultAllowedChat = "120363409956054412@g.us"
const CodexGroupJID = "120363407895179577@g.us"

var (
	allowedChatsFile      = filepath.Join(DataDir(), "allowed_chats.json")
	codexAllowedChatsFile = filepath.Join(DataDir(), "codex_allowed_chats.json")
	allowedChats          map[string]struct{}
	allowedChatsMu        sync.RWMutex
	codexAllowedChats     map[string]struct{}
	codexAllowedChatsMu   sync.RWMutex
)

func loadAllowedChats() map[string]struct{} {
	data, err := os.ReadFile(allowedChatsFile)
	if err != nil {
		return map[string]struct{}{DefaultAllowedChat: {}}
	}
	var jids []string
	if err := json.Unmarshal(data, &jids); err != nil || len(jids) == 0 {
		return map[string]struct{}{DefaultAllowedChat: {}}
	}
	m := make(map[string]struct{}, len(jids))
	for _, j := range jids {
		m[j] = struct{}{}
	}
	return m
}

func IsAllowedChat(jid string) bool {
	allowedChatsMu.RLock()
	_, ok := allowedChats[jid]
	allowedChatsMu.RUnlock()
	if ok {
		return true
	}
	codexAllowedChatsMu.RLock()
	_, ok = codexAllowedChats[jid]
	codexAllowedChatsMu.RUnlock()
	return ok
}

func IsCodexChat(jid string) bool {
	codexAllowedChatsMu.RLock()
	defer codexAllowedChatsMu.RUnlock()
	_, ok := codexAllowedChats[jid]
	return ok
}

func AddAllowedChat(jid string) {
	allowedChatsMu.Lock()
	allowedChats[jid] = struct{}{}
	allowedChatsMu.Unlock()
}

func RemoveAllowedChat(jid string) {
	allowedChatsMu.Lock()
	delete(allowedChats, jid)
	allowedChatsMu.Unlock()
}

func AddCodexAllowedChat(jid string) {
	codexAllowedChatsMu.Lock()
	codexAllowedChats[jid] = struct{}{}
	codexAllowedChatsMu.Unlock()
}

func RemoveCodexAllowedChat(jid string) {
	codexAllowedChatsMu.Lock()
	delete(codexAllowedChats, jid)
	codexAllowedChatsMu.Unlock()
}

func InitAllowedChats() {
	dir := DataDir()
	os.MkdirAll(dir, 0755)
	if _, err := os.Stat(allowedChatsFile); os.IsNotExist(err) {
		data, _ := json.MarshalIndent([]string{DefaultAllowedChat}, "", "  ")
		os.WriteFile(allowedChatsFile, data, 0644)
	}
	allowedChatsMu.Lock()
	allowedChats = loadAllowedChats()
	allowedChatsMu.Unlock()
}

func SaveAllowedChats() error {
	allowedChatsMu.RLock()
	jids := make([]string, 0, len(allowedChats))
	for jid := range allowedChats {
		jids = append(jids, jid)
	}
	allowedChatsMu.RUnlock()
	data, err := json.MarshalIndent(jids, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(allowedChatsFile, data, 0644)
}

func loadCodexAllowedChats() map[string]struct{} {
	data, err := os.ReadFile(codexAllowedChatsFile)
	if err != nil {
		return map[string]struct{}{CodexGroupJID: {}}
	}
	var jids []string
	if err := json.Unmarshal(data, &jids); err != nil || len(jids) == 0 {
		return map[string]struct{}{CodexGroupJID: {}}
	}
	m := make(map[string]struct{}, len(jids))
	for _, j := range jids {
		m[j] = struct{}{}
	}
	return m
}

func InitCodexAllowedChats() {
	dir := DataDir()
	os.MkdirAll(dir, 0755)
	if _, err := os.Stat(codexAllowedChatsFile); os.IsNotExist(err) {
		data, _ := json.MarshalIndent([]string{CodexGroupJID}, "", "  ")
		os.WriteFile(codexAllowedChatsFile, data, 0644)
	}
	codexAllowedChatsMu.Lock()
	codexAllowedChats = loadCodexAllowedChats()
	codexAllowedChatsMu.Unlock()
}

func SaveCodexAllowedChats() error {
	codexAllowedChatsMu.RLock()
	jids := make([]string, 0, len(codexAllowedChats))
	for jid := range codexAllowedChats {
		jids = append(jids, jid)
	}
	codexAllowedChatsMu.RUnlock()
	data, err := json.MarshalIndent(jids, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(codexAllowedChatsFile, data, 0644)
}

// ─── Signal whitelist ─────────────────────────────────────────────────────────

var (
	signalAllowedChatsFile = filepath.Join(DataDir(), "signal_allowed_chats.json")
	signalAllowedChats     map[string]struct{}
	signalAllowedChatsMu   sync.RWMutex
)

func loadSignalAllowedChats() map[string]struct{} {
	data, err := os.ReadFile(signalAllowedChatsFile)
	if err != nil {
		return map[string]struct{}{}
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return map[string]struct{}{}
	}
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

func InitSignalAllowedChats() {
	os.MkdirAll(DataDir(), 0755)
	signalAllowedChatsMu.Lock()
	signalAllowedChats = loadSignalAllowedChats()
	signalAllowedChatsMu.Unlock()
}

func SaveSignalAllowedChats() error {
	signalAllowedChatsMu.RLock()
	ids := make([]string, 0, len(signalAllowedChats))
	for id := range signalAllowedChats {
		ids = append(ids, id)
	}
	signalAllowedChatsMu.RUnlock()
	data, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(signalAllowedChatsFile, data, 0644)
}

func IsSignalAllowedChat(id string) bool {
	signalAllowedChatsMu.RLock()
	_, ok := signalAllowedChats[id]
	signalAllowedChatsMu.RUnlock()
	if ok {
		return true
	}
	signalCodexAllowedChatsMu.RLock()
	_, ok = signalCodexAllowedChats[id]
	signalCodexAllowedChatsMu.RUnlock()
	return ok
}

func AddSignalAllowedChat(id string) {
	signalAllowedChatsMu.Lock()
	signalAllowedChats[id] = struct{}{}
	signalAllowedChatsMu.Unlock()
}

func RemoveSignalAllowedChat(id string) {
	signalAllowedChatsMu.Lock()
	delete(signalAllowedChats, id)
	signalAllowedChatsMu.Unlock()
}

var (
	signalCodexAllowedChatsFile = filepath.Join(DataDir(), "signal_codex_allowed_chats.json")
	signalCodexAllowedChats     map[string]struct{}
	signalCodexAllowedChatsMu   sync.RWMutex
)

func loadSignalCodexAllowedChats() map[string]struct{} {
	data, err := os.ReadFile(signalCodexAllowedChatsFile)
	if err != nil {
		return map[string]struct{}{}
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return map[string]struct{}{}
	}
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

func InitSignalCodexAllowedChats() {
	os.MkdirAll(DataDir(), 0755)
	signalCodexAllowedChatsMu.Lock()
	signalCodexAllowedChats = loadSignalCodexAllowedChats()
	signalCodexAllowedChatsMu.Unlock()
}

func SaveSignalCodexAllowedChats() error {
	signalCodexAllowedChatsMu.RLock()
	ids := make([]string, 0, len(signalCodexAllowedChats))
	for id := range signalCodexAllowedChats {
		ids = append(ids, id)
	}
	signalCodexAllowedChatsMu.RUnlock()
	data, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(signalCodexAllowedChatsFile, data, 0644)
}

func IsSignalCodexChat(id string) bool {
	signalCodexAllowedChatsMu.RLock()
	defer signalCodexAllowedChatsMu.RUnlock()
	_, ok := signalCodexAllowedChats[id]
	return ok
}

func AddSignalCodexAllowedChat(id string) {
	signalCodexAllowedChatsMu.Lock()
	signalCodexAllowedChats[id] = struct{}{}
	signalCodexAllowedChatsMu.Unlock()
}

func RemoveSignalCodexAllowedChat(id string) {
	signalCodexAllowedChatsMu.Lock()
	delete(signalCodexAllowedChats, id)
	signalCodexAllowedChatsMu.Unlock()
}
