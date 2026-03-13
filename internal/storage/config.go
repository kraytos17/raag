package storage

import (
	"github.com/p-society/raag/internal/constants"
)

type AppConfig struct {
	MusicDir   string `json:"music_dir"`
	Volume     int    `json:"volume"`
	TUIEnabled bool   `json:"tui_enabled"`
	Network    bool   `json:"network"`
	Rendezvous string `json:"rendezvous"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	LogLevel   string `json:"log_level"`
}

func DefaultConfig() *AppConfig {
	return &AppConfig{
		MusicDir:   "./music",
		Volume:     constants.DefaultVolume,
		TUIEnabled: false,
		Network:    false,
		Rendezvous: constants.DefaultRendezvous,
		Host:       constants.DefaultHost,
		Port:       0,
		LogLevel:   "info",
	}
}
