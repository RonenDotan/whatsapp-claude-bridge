package core

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Settings struct {
	WhatsAppEnabled bool `json:"whatsapp_enabled"`
	SignalEnabled   bool `json:"signal_enabled"`
	RestAPIEnabled  bool `json:"rest_api_enabled"` // enables /api/send and /api/download on :8080
}

func LoadSettings() Settings {
	data, err := os.ReadFile(filepath.Join(DataDir(), "settings.json"))
	if err != nil {
		return Settings{WhatsAppEnabled: true, SignalEnabled: true}
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{WhatsAppEnabled: true, SignalEnabled: true}
	}
	return s
}
