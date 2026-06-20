package core

import (
	"sync"
	"time"
)

// StartTime is set by main at bridge startup — used for uptime calculation.
var StartTime time.Time

// AdminPublicURL holds the current ngrok public URL (empty if ngrok is not running).
var AdminPublicURL string

// ChannelState holds the live connection state for one transport channel.
type ChannelState struct {
	Connected bool
	AccountID string // phone number or identifier
}

var (
	channelStatusMu sync.RWMutex
	channelStatus   = map[string]ChannelState{}
)

// SetChannelStatus is called by each channel when its connection state changes.
func SetChannelStatus(channel string, state ChannelState) {
	channelStatusMu.Lock()
	channelStatus[channel] = state
	channelStatusMu.Unlock()
}

// GetChannelStatus returns the last-known state for a channel.
func GetChannelStatus(channel string) ChannelState {
	channelStatusMu.RLock()
	defer channelStatusMu.RUnlock()
	return channelStatus[channel]
}

// GetAllChannelStatuses returns a snapshot of all channel states.
func GetAllChannelStatuses() map[string]ChannelState {
	channelStatusMu.RLock()
	defer channelStatusMu.RUnlock()
	out := make(map[string]ChannelState, len(channelStatus))
	for k, v := range channelStatus {
		out[k] = v
	}
	return out
}
