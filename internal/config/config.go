package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Config holds all configuration values
type Config struct {
	// Network
	Host       string
	Port       int
	FixedPort  int
	Rendezvous string
	ProtocolID string

	// Discovery
	TrackerURL     string
	DHTEnabled     bool
	MaxPeers       int
	BootstrapPeers []string

	// Playback
	MusicDir string
	Volume   int

	// UI
	TUI      bool
	Wifi     bool
	Offline  bool
	LogLevel string
}

// InitViper initializes Viper with defaults and binds flags
func InitViper(cmd *cobra.Command) (*viper.Viper, error) {
	v := viper.New()

	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("could not get user config dir: %w", err)
	}
	configPath := filepath.Join(configDir, "raag")
	configFile := filepath.Join(configPath, "config.yaml")
	if err := os.MkdirAll(configPath, 0o755); err != nil {
		return nil, fmt.Errorf("could not create config dir: %w", err)
	}

	v.SetConfigType("yaml")
	v.SetConfigFile(configFile)
	setDefaults(v)

	if _, err := os.Stat(configFile); err == nil {
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("could not read config file: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("could not check config file: %w", err)
	} else {
		if err := v.WriteConfigAs(configFile); err != nil {
			return nil, fmt.Errorf("could not write default config: %w", err)
		}
	}

	bindFlags(v, cmd)
	return v, nil
}

func setDefaults(v *viper.Viper) {
	// Network defaults
	v.SetDefault("network.host", "127.0.0.1")
	v.SetDefault("network.port", 0)
	v.SetDefault("network.fixed_port", 0)
	v.SetDefault("network.rendezvous", "raag-music-share")
	v.SetDefault("network.protocol_id", "/raag/1.0.0")

	// Discovery defaults
	v.SetDefault("discovery.tracker_url", "")
	v.SetDefault("discovery.dht_enabled", true)
	v.SetDefault("discovery.max_peers", 100)
	v.SetDefault("discovery.bootstrap_peers", []string{})

	// Playback defaults
	v.SetDefault("playback.music_dir", "./music")
	v.SetDefault("playback.volume", 50)

	// UI defaults
	v.SetDefault("ui.tui_enabled", true)
	v.SetDefault("ui.wifi_mode", false)
	v.SetDefault("ui.offline", true)
	v.SetDefault("ui.log_level", "info")
}

func bindFlags(v *viper.Viper, cmd *cobra.Command) {
	root := cmd.Root()
	// Network flags
	v.BindPFlag("network.host", root.PersistentFlags().Lookup("host"))
	v.BindPFlag("network.port", root.PersistentFlags().Lookup("port"))
	v.BindPFlag("network.fixed_port", root.PersistentFlags().Lookup("fixed-port"))
	v.BindPFlag("network.rendezvous", root.PersistentFlags().Lookup("rendezvous"))
	v.BindPFlag("network.protocol_id", root.PersistentFlags().Lookup("pid"))

	// Discovery flags
	v.BindPFlag("discovery.tracker_url", root.PersistentFlags().Lookup("tracker"))
	v.BindPFlag("discovery.dht_enabled", root.PersistentFlags().Lookup("dht"))
	v.BindPFlag("discovery.max_peers", root.PersistentFlags().Lookup("max-peers"))
	v.BindPFlag("discovery.bootstrap_peers", root.PersistentFlags().Lookup("bootstrap"))

	// Playback flags
	v.BindPFlag("playback.music_dir", root.PersistentFlags().Lookup("musicdir"))

	// UI flags
	v.BindPFlag("ui.tui_enabled", root.PersistentFlags().Lookup("tui"))
	v.BindPFlag("ui.wifi_mode", root.PersistentFlags().Lookup("wifi"))
	v.BindPFlag("ui.offline", root.PersistentFlags().Lookup("offline"))
}

// LoadConfig loads configuration from Viper instance
func LoadConfig(v *viper.Viper) (*Config, error) {
	cfg := &Config{
		Host:       v.GetString("network.host"),
		Port:       v.GetInt("network.port"),
		FixedPort:  v.GetInt("network.fixed_port"),
		Rendezvous: v.GetString("network.rendezvous"),
		ProtocolID: v.GetString("network.protocol_id"),

		TrackerURL:     v.GetString("discovery.tracker_url"),
		DHTEnabled:     v.GetBool("discovery.dht_enabled"),
		MaxPeers:       v.GetInt("discovery.max_peers"),
		BootstrapPeers: v.GetStringSlice("discovery.bootstrap_peers"),

		MusicDir: v.GetString("playback.music_dir"),
		Volume:   v.GetInt("playback.volume"),

		TUI:      v.GetBool("ui.tui_enabled"),
		Wifi:     v.GetBool("ui.wifi_mode"),
		Offline:  v.GetBool("ui.offline"),
		LogLevel: v.GetString("ui.log_level"),
	}

	if cfg.Port < 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("invalid port number: %d", cfg.Port)
	}
	if cfg.FixedPort < 0 || cfg.FixedPort > 65535 {
		return nil, fmt.Errorf("invalid fixed port number: %d", cfg.FixedPort)
	}
	return cfg, nil
}

// SaveConfig saves current configuration to file
func SaveConfig(v *viper.Viper, cfg *Config) error {
	// Update Viper with config values
	v.Set("network.host", cfg.Host)
	v.Set("network.port", cfg.Port)
	v.Set("network.fixed_port", cfg.FixedPort)
	v.Set("network.rendezvous", cfg.Rendezvous)
	v.Set("network.protocol_id", cfg.ProtocolID)

	v.Set("discovery.tracker_url", cfg.TrackerURL)
	v.Set("discovery.dht_enabled", cfg.DHTEnabled)
	v.Set("discovery.max_peers", cfg.MaxPeers)
	v.Set("discovery.bootstrap_peers", cfg.BootstrapPeers)

	v.Set("playback.music_dir", cfg.MusicDir)
	v.Set("playback.volume", cfg.Volume)

	v.Set("ui.tui_enabled", cfg.TUI)
	v.Set("ui.wifi_mode", cfg.Wifi)
	v.Set("ui.offline", cfg.Offline)
	v.Set("ui.log_level", cfg.LogLevel)

	return v.WriteConfig()
}

// UpdateBootstrapPeers updates just the bootstrap_peers in config file
func UpdateBootstrapPeers(v *viper.Viper, peers []string) error {
	v.Set("discovery.bootstrap_peers", peers)
	return v.WriteConfig()
}
