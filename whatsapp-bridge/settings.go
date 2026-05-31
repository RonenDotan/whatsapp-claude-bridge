package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Settings struct {
	WhatsAppEnabled bool `json:"whatsapp_enabled"`
	SignalEnabled   bool `json:"signal_enabled"`
}

func loadSettings() Settings {
	data, err := os.ReadFile(filepath.Join(storeDir(), "settings.json"))
	if err != nil {
		return Settings{WhatsAppEnabled: true, SignalEnabled: true}
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{WhatsAppEnabled: true, SignalEnabled: true}
	}
	return s
}
