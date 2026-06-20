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
	AdminEnabled    bool `json:"admin_enabled"`     // enables admin panel on AdminPort
	AdminPort       int  `json:"admin_port"`        // default 8081
}

func SaveSettings(s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(DataDir(), "settings.json"), data, 0644)
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
	if s.AdminPort == 0 {
		s.AdminPort = 8081
	}
	return s
}
