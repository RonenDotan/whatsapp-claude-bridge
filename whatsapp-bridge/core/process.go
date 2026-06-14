package core

import (
	"context"
	"sync"
)

var (
	runningCancelsMu sync.Mutex
	runningCancels   = map[string]context.CancelFunc{}
)

// SetRunningCancel registers a cancel func for chatID.
// Returns false if a process is already running for that chat.
func SetRunningCancel(chatID string, cancel context.CancelFunc) bool {
	runningCancelsMu.Lock()
	defer runningCancelsMu.Unlock()
	if _, busy := runningCancels[chatID]; busy {
		return false
	}
	runningCancels[chatID] = cancel
	return true
}

// ClearRunningCancel removes the cancel entry for chatID.
func ClearRunningCancel(chatID string) {
	runningCancelsMu.Lock()
	defer runningCancelsMu.Unlock()
	delete(runningCancels, chatID)
}

// CancelRunning kills the running process for chatID. Returns true if there was something to cancel.
func CancelRunning(chatID string) bool {
	runningCancelsMu.Lock()
	defer runningCancelsMu.Unlock()
	cancel, ok := runningCancels[chatID]
	if ok {
		cancel()
		delete(runningCancels, chatID)
	}
	return ok
}
