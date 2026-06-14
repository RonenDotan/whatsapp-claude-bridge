package core

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	recentMessageCacheSize = 20
	persistedCacheSize     = 500
)

type cachedMessage struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

var (
	recentMsgMu     sync.Mutex
	recentMsgCache  = map[string][]cachedMessage{}
	fileMsgCache    = map[string][]cachedMessage{}
	fileCacheLoaded = map[string]bool{}
)

func chatCacheFile(chatID string) string {
	dir := filepath.Join(DataDir(), "chats", chatID)
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "message_cache.json")
}

func ensureFileCacheLoaded(chatID string) {
	if fileCacheLoaded[chatID] {
		return
	}
	fileCacheLoaded[chatID] = true
	data, err := os.ReadFile(chatCacheFile(chatID))
	if err != nil {
		fileMsgCache[chatID] = []cachedMessage{}
		return
	}
	var msgs []cachedMessage
	if err := json.Unmarshal(data, &msgs); err != nil {
		log.Printf("[cache] parse error for %s: %v", chatID, err)
		fileMsgCache[chatID] = []cachedMessage{}
		return
	}
	fileMsgCache[chatID] = msgs
	log.Printf("[cache] loaded %d persisted messages for chat %s", len(msgs), chatID)
}

func writeCacheFile(chatID string, msgs []cachedMessage) {
	data, err := json.Marshal(msgs)
	if err != nil {
		log.Printf("[cache] marshal error for %s: %v", chatID, err)
		return
	}
	if err := os.WriteFile(chatCacheFile(chatID), data, 0o644); err != nil {
		log.Printf("[cache] write error for %s: %v", chatID, err)
	}
}

// StoreRecentMessage saves a message to both the hot in-memory cache and the persistent file cache.
func StoreRecentMessage(chatID, msgID, text string) {
	if text == "" || msgID == "" {
		return
	}
	recentMsgMu.Lock()
	defer recentMsgMu.Unlock()
	log.Printf("[cache] store chat=%s msgID=%q text=%q", chatID, msgID, text)

	hot := recentMsgCache[chatID]
	hot = append(hot, cachedMessage{ID: msgID, Text: text})
	if len(hot) > recentMessageCacheSize {
		hot = hot[len(hot)-recentMessageCacheSize:]
	}
	recentMsgCache[chatID] = hot

	ensureFileCacheLoaded(chatID)
	cold := fileMsgCache[chatID]
	cold = append(cold, cachedMessage{ID: msgID, Text: text})
	if len(cold) > persistedCacheSize {
		cold = cold[len(cold)-persistedCacheSize:]
	}
	fileMsgCache[chatID] = cold
	writeCacheFile(chatID, cold)
}

// LookupRecentMessage finds a message by ID. Checks hot cache first, then the persisted file cache.
func LookupRecentMessage(chatID, msgID string) (string, bool) {
	recentMsgMu.Lock()
	defer recentMsgMu.Unlock()

	for _, m := range recentMsgCache[chatID] {
		if m.ID == msgID {
			return m.Text, true
		}
	}

	ensureFileCacheLoaded(chatID)
	for _, m := range fileMsgCache[chatID] {
		if m.ID == msgID {
			return m.Text, true
		}
	}
	return "", false
}
