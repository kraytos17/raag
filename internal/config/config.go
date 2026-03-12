package config

import (
	"fmt"
	"os"

	"github.com/p-society/raag/internal/constants"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Config holds all configuration values
type Config struct {
	// Network
	Host       string
	Port       int
	Rendezvous string

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
	Network  bool
	LogLevel string
}

func DefaultConfig() Config {
	return Config{
		Host:           "127.0.0.1",
		Port:           constants.DefaultPort,
		Rendezvous:     constants.DefaultRendezvous,
		TrackerURL:     "",
		DHTEnabled:     true,
		MaxPeers:       constants.DefaultMaxPeers,
		BootstrapPeers: []string{},
		MusicDir:       "./music",
		Volume:         constants.DefaultVolume,
		TUI:            false,
		Network:        false,
		LogLevel:       "info",
	}
}

// InitViper initializes Viper with defaults and binds flags
func InitViper(cmd *cobra.Command) (*viper.Viper, error) {
	v := viper.New()

	configDir, err := Dir()
	if err != nil {
		return nil, fmt.Errorf("could not get user config dir: %w", err)
	}

	configPath := configDir
	configFile, err := FilePath()
	if err != nil {
		return nil, fmt.Errorf("could not determine config file: %w", err)
	}
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
	defaults := DefaultConfig()
	v.SetDefault("network.host", defaults.Host)
	v.SetDefault("network.port", defaults.Port)
	v.SetDefault("network.rendezvous", defaults.Rendezvous)
	v.SetDefault("discovery.tracker_url", defaults.TrackerURL)
	v.SetDefault("discovery.dht_enabled", defaults.DHTEnabled)
	v.SetDefault("discovery.max_peers", defaults.MaxPeers)
	v.SetDefault("discovery.bootstrap_peers", defaults.BootstrapPeers)
	v.SetDefault("playback.music_dir", defaults.MusicDir)
	v.SetDefault("playback.volume", defaults.Volume)
	v.SetDefault("ui.tui_enabled", defaults.TUI)
	v.SetDefault("ui.network", defaults.Network)
	v.SetDefault("ui.log_level", defaults.LogLevel)
}

func bindFlags(v *viper.Viper, cmd *cobra.Command) {
	root := cmd.Root()
	// Network flags
	v.BindPFlag("network.host", root.PersistentFlags().Lookup("host"))
	v.BindPFlag("network.port", root.PersistentFlags().Lookup("port"))
	v.BindPFlag("network.rendezvous", root.PersistentFlags().Lookup("rendezvous"))

	// Discovery flags
	v.BindPFlag("discovery.tracker_url", root.PersistentFlags().Lookup("tracker"))
	v.BindPFlag("discovery.dht_enabled", root.PersistentFlags().Lookup("dht"))
	v.BindPFlag("discovery.max_peers", root.PersistentFlags().Lookup("max-peers"))
	v.BindPFlag("discovery.bootstrap_peers", root.PersistentFlags().Lookup("bootstrap"))

	// Playback flags
	v.BindPFlag("playback.music_dir", root.PersistentFlags().Lookup("musicdir"))

	// UI flags
	v.BindPFlag("ui.tui_enabled", root.PersistentFlags().Lookup("tui"))
	v.BindPFlag("ui.network", root.PersistentFlags().Lookup("network"))
}

// LoadConfig loads configuration from Viper instance
func LoadConfig(v *viper.Viper) (*Config, error) {
	cfg := &Config{
		Host:           v.GetString("network.host"),
		Port:           v.GetInt("network.port"),
		Rendezvous:     v.GetString("network.rendezvous"),
		TrackerURL:     getTrackerURL(v),
		DHTEnabled:     v.GetBool("discovery.dht_enabled"),
		MaxPeers:       v.GetInt("discovery.max_peers"),
		BootstrapPeers: v.GetStringSlice("discovery.bootstrap_peers"),
		MusicDir:       v.GetString("playback.music_dir"),
		Volume:         v.GetInt("playback.volume"),
		TUI:            v.GetBool("ui.tui_enabled"),
		Network:        v.GetBool("ui.network"),
		LogLevel:       v.GetString("ui.log_level"),
	}

	if cfg.Port < 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("invalid port number: %d", cfg.Port)
	}
	if cfg.Port == 0 && cfg.Network {
		cfg.Port = constants.DefaultPort
	}

	return cfg, nil
}

// getTrackerURL returns the tracker URL with priority:
// 1. CLI flag (via config file)
// 2. Environment variable
// 3. Default value
func getTrackerURL(v *viper.Viper) string {
	if url := v.GetString("discovery.tracker_url"); url != "" {
		return url
	}
	if url := os.Getenv(constants.EnvTrackerURL); url != "" {
		return url
	}
	return constants.DefaultTrackerURL
}

// SaveConfig saves current configuration to file
func SaveConfig(v *viper.Viper, cfg *Config) error {
	v.Set("network.host", cfg.Host)
	v.Set("network.port", cfg.Port)
	v.Set("network.rendezvous", cfg.Rendezvous)

	v.Set("discovery.tracker_url", cfg.TrackerURL)
	v.Set("discovery.dht_enabled", cfg.DHTEnabled)
	v.Set("discovery.max_peers", cfg.MaxPeers)
	v.Set("discovery.bootstrap_peers", cfg.BootstrapPeers)

	v.Set("playback.music_dir", cfg.MusicDir)
	v.Set("playback.volume", cfg.Volume)

	v.Set("ui.tui_enabled", cfg.TUI)
	v.Set("ui.network", cfg.Network)
	v.Set("ui.log_level", cfg.LogLevel)

	return v.WriteConfig()
}

// UpdateBootstrapPeers updates just the bootstrap_peers in config file
func UpdateBootstrapPeers(v *viper.Viper, peers []string) error {
	v.Set("discovery.bootstrap_peers", peers)
	return v.WriteConfig()
}
