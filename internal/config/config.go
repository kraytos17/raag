package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/p-society/raag/internal/constants"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Config holds persistent configuration values only.
// Runtime state (volume, playback, etc.) is stored in state.json.
type Config struct {
	// Network - persistent network settings
	Host         string `json:"network.host"`
	Port         int    `json:"network.port"`
	Rendezvous   string `json:"network.rendezvous"`
	RelayAddress string `json:"network.relay_address"`

	// Discovery - persistent discovery settings
	TrackerURL      string   `json:"discovery.tracker_url"`
	DHTEnabled      bool     `json:"discovery.dht_enabled"`
	MaxPeers        int      `json:"discovery.max_peers"`
	BootstrapPeers  []string `json:"discovery.bootstrap_peers"`
	MDNSEnabled     bool     `json:"discovery.mdns_enabled"`
	MDNSServiceName string   `json:"discovery.mdns_service_name"`
	AuthSecret      string   `mapstructure:"-" json:"-"`

	// Playback - persistent playback settings
	MusicDir string `json:"playback.music_dir"`

	// Storage - persistent storage settings
	DataDir string `json:"storage.data_dir"`
	// Runtime - these are set at startup
	Network    bool   `mapstructure:"-" json:"-"`
	ForceRelay bool   `mapstructure:"-" json:"-"`
	Volume     int    `mapstructure:"-" json:"-"`
	TUI        bool   `mapstructure:"-" json:"-"`
	LogLevel   string `mapstructure:"-" json:"-"`
}

func DefaultConfig() Config {
	musicDir := "./music"
	if defaultMusicDir, err := MusicDir(); err == nil {
		musicDir = defaultMusicDir
	}
	dataDir := "./data"
	if defaultDataDir, err := DataDir(); err == nil {
		dataDir = defaultDataDir
	}
	return Config{
		Host:            constants.DefaultHost,
		Port:            constants.DefaultPort,
		Rendezvous:      constants.DefaultRendezvous,
		RelayAddress:    "",
		TrackerURL:      "",
		DHTEnabled:      true,
		MaxPeers:        constants.DefaultMaxPeers,
		BootstrapPeers:  []string{},
		MDNSEnabled:     true,
		MDNSServiceName: "",
		MusicDir:        musicDir,
		DataDir:         dataDir,
		Volume:          constants.DefaultVolume,
		Network:         false,
		ForceRelay:      false,
		LogLevel:        "info",
	}
}

// InitViper initializes Viper with defaults and binds flags
func InitViper(cmd *cobra.Command) (*viper.Viper, error) {
	v := viper.New()
	configDir, err := Dir()
	if err != nil {
		return nil, fmt.Errorf("could not get user config dir: %w", err)
	}

	configFile, err := FilePath()
	if err != nil {
		return nil, fmt.Errorf("could not determine config file: %w", err)
	}
	if err := os.MkdirAll(configDir, 0o750); err != nil {
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
	v.SetDefault("network.relay_address", defaults.RelayAddress)
	v.SetDefault("network.force_relay", defaults.ForceRelay)
	v.SetDefault("discovery.tracker_url", defaults.TrackerURL)
	v.SetDefault("discovery.dht_enabled", defaults.DHTEnabled)
	v.SetDefault("discovery.max_peers", defaults.MaxPeers)
	v.SetDefault("discovery.bootstrap_peers", defaults.BootstrapPeers)
	v.SetDefault("discovery.mdns_enabled", defaults.MDNSEnabled)
	v.SetDefault("discovery.mdns_service_name", defaults.MDNSServiceName)
	v.SetDefault("discovery.auth_secret", os.Getenv("AUTH_SECRET"))
	v.SetDefault("playback.music_dir", defaults.MusicDir)
	v.SetDefault("storage.data_dir", defaults.DataDir)
}

func bindFlags(v *viper.Viper, cmd *cobra.Command) {
	root := cmd.Root()
	_ = v.BindPFlag("network.host", root.PersistentFlags().Lookup("host"))
	_ = v.BindPFlag("network.port", root.PersistentFlags().Lookup("port"))
	_ = v.BindPFlag("network.rendezvous", root.PersistentFlags().Lookup("rendezvous"))
	_ = v.BindPFlag("network.relay_address", root.PersistentFlags().Lookup("relay"))
	_ = v.BindPFlag("network.force_relay", root.PersistentFlags().Lookup("force-relay"))
	_ = v.BindPFlag("discovery.tracker_url", root.PersistentFlags().Lookup("tracker"))
	_ = v.BindPFlag("discovery.dht_enabled", root.PersistentFlags().Lookup("dht"))
	_ = v.BindPFlag("discovery.mdns_enabled", root.PersistentFlags().Lookup("mdns"))
	_ = v.BindPFlag("discovery.mdns_service_name", root.PersistentFlags().Lookup("mdns-service-name"))
	_ = v.BindPFlag("discovery.max_peers", root.PersistentFlags().Lookup("max-peers"))
	_ = v.BindPFlag("discovery.bootstrap_peers", root.PersistentFlags().Lookup("bootstrap"))
	_ = v.BindPFlag("discovery.auth_secret", root.PersistentFlags().Lookup("auth-secret"))
	_ = v.BindPFlag("playback.music_dir", root.PersistentFlags().Lookup("music-dir"))
	_ = v.BindPFlag("storage.data_dir", root.PersistentFlags().Lookup("data-dir"))
}

// LoadConfig loads configuration from Viper instance.
func LoadConfig(v *viper.Viper) (*Config, error) {
	cfg := &Config{
		Host:            v.GetString("network.host"),
		Port:            v.GetInt("network.port"),
		Rendezvous:      v.GetString("network.rendezvous"),
		RelayAddress:    v.GetString("network.relay_address"),
		TrackerURL:      v.GetString("discovery.tracker_url"),
		DHTEnabled:      v.GetBool("discovery.dht_enabled"),
		MaxPeers:        v.GetInt("discovery.max_peers"),
		BootstrapPeers:  v.GetStringSlice("discovery.bootstrap_peers"),
		MDNSEnabled:     v.GetBool("discovery.mdns_enabled"),
		MDNSServiceName: v.GetString("discovery.mdns_service_name"),
		AuthSecret:      v.GetString("discovery.auth_secret"),
		MusicDir:        v.GetString("playback.music_dir"),
		DataDir:         v.GetString("storage.data_dir"),
		ForceRelay:      v.GetBool("network.force_relay"),
	}
	if cfg.TrackerURL == "" {
		if url := os.Getenv(constants.EnvTrackerURL); url != "" {
			cfg.TrackerURL = url
		} else {
			cfg.TrackerURL = constants.DefaultTrackerURL
		}
	}
	if cfg.Port < 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("invalid port number: %d", cfg.Port)
	}
	return cfg, nil
}

// SaveConfig saves persistent configuration values to file.
func SaveConfig(v *viper.Viper, cfg *Config) error {
	jsonBytes, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	var configMap map[string]any
	decoder := json.NewDecoder(bytes.NewReader(jsonBytes))
	decoder.UseNumber()
	if err := decoder.Decode(&configMap); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	for key, value := range configMap {
		if value == nil {
			continue
		}
		v.Set(key, value)
	}
	return v.WriteConfig()
}
