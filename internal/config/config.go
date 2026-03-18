package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	Library    LibraryConfig    `mapstructure:"library"`
	Playback   PlaybackConfig   `mapstructure:"playback"`
	Daemon     DaemonConfig     `mapstructure:"daemon"`
	P2P        P2PConfig        `mapstructure:"p2p"`
	Transcoder TranscoderConfig `mapstructure:"transcoder"`
	Privacy    PrivacyConfig    `mapstructure:"privacy"`
	Metrics    MetricsConfig    `mapstructure:"metrics"`
}

func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("toml")

	configDir := filepath.Join(os.Getenv("HOME"), ".config", "raag")
	v.AddConfigPath(configDir)
	v.AddConfigPath(".")

	setDefaults(v)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	if len(c.Library.Paths) == 0 {
		return fmt.Errorf("at least one library path is required")
	}
	if c.P2P.MaxPeers <= 0 {
		c.P2P.MaxPeers = 20
	}
	if c.Playback.Volume < 0 || c.Playback.Volume > 100 {
		c.Playback.Volume = 80
	}
	return nil
}

type LibraryConfig struct {
	Paths       []string `mapstructure:"paths"`
	ScanOnStart bool     `mapstructure:"scan_on_start"`
	Watch       bool     `mapstructure:"watch"`
}

type PlaybackConfig struct {
	Volume       int    `mapstructure:"volume"`
	OutputDevice string `mapstructure:"output_device"`
	BufferSize   int    `mapstructure:"buffer_size"`
	SampleRate   int    `mapstructure:"sample_rate"`
}

type DaemonConfig struct {
	SocketPath string `mapstructure:"socket_path"`
	LogLevel   string `mapstructure:"log_level"`
	DataDir    string `mapstructure:"data_dir"`
	PidFile    string `mapstructure:"pid_file"`
}

type P2PConfig struct {
	Enabled          bool     `mapstructure:"enabled"`
	ListenAddrs      []string `mapstructure:"listen_addrs"`
	MDNSServiceTag   string   `mapstructure:"mdns_service_tag"`
	MaxPeers         int      `mapstructure:"max_peers"`
	StreamPort       int      `mapstructure:"stream_port"`
	AnnounceLibrary  bool     `mapstructure:"announce_library"`
	PerPeerRateLimit int      `mapstructure:"per_peer_rate_limit"`
	UploadBandwidth  int      `mapstructure:"upload_bandwidth"`
}

type TranscoderConfig struct {
	FFmpegPath    string `mapstructure:"ffmpeg_path"`
	StreamCodec   string `mapstructure:"stream_codec"`
	StreamBitrate string `mapstructure:"stream_bitrate"`
}

type PrivacyConfig struct {
	ShareLibraryManifest bool `mapstructure:"share_library_manifest"`
	SharePlayHistory     bool `mapstructure:"share_play_history"`
	AnnounceOnDiscovery  bool `mapstructure:"announce_on_discovery"`
}

type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    int    `mapstructure:"port"`
	Path    string `mapstructure:"path"`
}
