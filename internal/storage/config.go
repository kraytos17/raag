package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type AppConfig struct {
	MusicDir   string `json:"music_dir"`
	Volume     int    `json:"volume"`
	TUIEnabled bool   `json:"tui_enabled"`
	WifiMode   bool   `json:"wifi_mode"`
	Offline    bool   `json:"offline"`
	Rendezvous string `json:"rendezvous"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	LogLevel   string `json:"log_level"`
}

func DefaultConfig() *AppConfig {
	return &AppConfig{
		MusicDir:   "./music",
		Volume:     50,
		TUIEnabled: false,
		WifiMode:   false,
		Offline:    true,
		Rendezvous: "raag-music-share",
		Host:       "127.0.0.1",
		Port:       0,
		LogLevel:   "info",
	}
}

func (s *Storage) LoadConfig() (*AppConfig, error) {
	filePath := filepath.Join(s.configDir, "config.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			if err := s.SaveConfig(cfg); err != nil {
				return nil, fmt.Errorf("error saving default config: %w", err)
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("error decoding config: %w", err)
	}
	return &cfg, nil
}

func (s *Storage) SaveConfig(cfg *AppConfig) error {
	filePath := filepath.Join(s.configDir, "config.json")
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("error creating config file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("error encoding config: %w", err)
	}
	return nil
}
