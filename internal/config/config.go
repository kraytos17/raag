package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/viper"
)

var ErrNoMusicPath = errors.New("no music path configured")

type DuplicateHandling string

const (
	DuplicateSkip DuplicateHandling = "skip"
	DuplicateWarn DuplicateHandling = "warn"
	DuplicateKeep DuplicateHandling = "keep"
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
	return loadWithOptions("")
}

func LoadWithMusicPath(musicPath string) (*Config, error) {
	return loadWithOptions(musicPath)
}

func loadWithOptions(musicPath string) (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("toml")

	configDir := GetConfigDir()
	v.AddConfigPath(configDir)
	v.AddConfigPath(".")

	setDefaults(v)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := errors.AsType[viper.ConfigFileNotFoundError](err); !ok {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	if err := cfg.expandPaths(); err != nil {
		return nil, err
	}
	if musicPath != "" {
		cfg.Library.Paths = []string{ExpandHome(musicPath)}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	if c.P2P.MaxPeers <= 0 {
		return fmt.Errorf("P2P max peers must be positive, got %d", c.P2P.MaxPeers)
	}
	if c.Playback.Volume < 0 {
		return fmt.Errorf("playback volume must be non-negative, got %d", c.Playback.Volume)
	}
	if c.Playback.Volume > 100 {
		return fmt.Errorf("playback volume must be at most 100, got %d", c.Playback.Volume)
	}
	return nil
}

func (c *Config) ValidateForStart() error {
	if err := c.Validate(); err != nil {
		return err
	}
	if len(c.Library.Paths) == 0 {
		return ErrNoMusicPath
	}
	return nil
}

func (c *Config) expandPaths() error {
	c.Daemon.DataDir = ExpandHome(c.Daemon.DataDir)
	c.Daemon.SocketPath = ExpandHome(c.Daemon.SocketPath)
	c.Daemon.PidFile = ExpandHome(c.Daemon.PidFile)
	for i, p := range c.Library.Paths {
		c.Library.Paths[i] = ExpandHome(p)
	}
	return nil
}

func ExpandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir(), path[2:])
	}
	return path
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return os.Getenv("HOME")
}

func GetConfigDir() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".config", "raag")
}

func GetDataDir() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".local", "share", "raag")
}

var commonMusicPaths = []string{
	"Music",
	"music",
	"Music Library",
	"My Music",
}

func detectMusicDirectory() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	for _, name := range commonMusicPaths {
		path := filepath.Join(home, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func promptDirectory(reader *bufio.Reader, defaultPath string) (string, error) {
	for {
		fmt.Printf("Music directory path [%s]: ", defaultPath)
		input, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "" {
			if defaultPath == "" {
				return "", errors.New("no music path provided and no default available")
			}
			return defaultPath, nil
		}

		path := ExpandHome(input)
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("Directory '%s' does not exist.\n", path)
				fmt.Print("Create it? [Y/n]: ")
				answer, _ := reader.ReadString('\n')
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer == "" || answer == "y" || answer == "yes" {
					if err := os.MkdirAll(path, 0o755); err != nil {
						fmt.Printf("Error creating directory: %v\n", err)
						continue
					}
					return path, nil
				}
				fmt.Println("Please enter a valid path.")
				continue
			}
			fmt.Printf("Error accessing path: %v\n", err)
			continue
		}
		if !info.IsDir() {
			fmt.Printf("Error: '%s' is not a directory.\n", path)
			continue
		}
		return path, nil
	}
}

func RunSetup() error {
	configDir := GetConfigDir()
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	fmt.Println("Welcome to Raag!")
	fmt.Println("This tool will help you configure Raag for first-time use.")
	fmt.Println()

	detectedPath := detectMusicDirectory()
	defaultPath := detectedPath
	if defaultPath == "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			defaultPath = filepath.Join(home, "Music")
		} else {
			defaultPath = ""
		}
	}

	reader := bufio.NewReader(os.Stdin)
	musicPath, err := promptDirectory(reader, defaultPath)
	if err != nil {
		return err
	}

	dataDir := GetDataDir()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	socketPath := filepath.Join(dataDir, "raag.sock")
	pidFile := filepath.Join(dataDir, "raagd.pid")
	configPath := filepath.Join(configDir, "config.toml")
	newCfg := &Config{
		Library: LibraryConfig{
			Paths:             []string{musicPath},
			ScanOnStart:       true,
			Watch:             false,
			DuplicateHandling: DuplicateWarn,
		},
		Daemon: DaemonConfig{
			SocketPath: socketPath,
			LogLevel:   "info",
			DataDir:    dataDir,
			PidFile:    pidFile,
		},
		Playback: PlaybackConfig{
			Volume:       80,
			OutputDevice: "default",
			BufferSize:   4096,
			SampleRate:   44100,
		},
		P2P: P2PConfig{
			Enabled:          true,
			ListenAddrs:      DefaultListenAddrs,
			AnnounceAddrs:    []string{},
			BootstrapPeers:   []string{},
			MDNSServiceTag:   "raag-local",
			MaxPeers:         20,
			StreamPort:       7845,
			AnnounceLibrary:  true,
			PerPeerRateLimit: 10,
			UploadBandwidth:  0,
			LANOnly:          false,
			MaxKnownPeers:    100,
			ChunkSize:        256 * 1024,
			PeerDataTTL:      24 * time.Hour,
		},
		Transcoder: TranscoderConfig{
			FFmpegPath:    "ffmpeg",
			StreamCodec:   "opus",
			StreamBitrate: "128k",
		},
		Privacy: PrivacyConfig{
			ShareLibraryManifest: true,
			SharePlayHistory:     false,
			AnnounceOnDiscovery:  true,
		},
		Metrics: MetricsConfig{
			Enabled: false,
			Port:    9200,
			Path:    "/metrics",
		},
	}

	tomlBytes, err := toml.Marshal(newCfg)
	if err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}

	content := append([]byte("# Raag Configuration\n# Generated by raagd --setup\n\n"), tomlBytes...)
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Println()
	fmt.Println("Configuration saved to:", configPath)
	fmt.Println()
	fmt.Printf("Music directory: %s\n", musicPath)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  - Run 'raagd' to start the daemon")
	fmt.Println("  - Run 'raag lib scan' to scan your music library")
	fmt.Println("  - Run 'raag play <query>' to play music")
	return nil
}

type LibraryConfig struct {
	Paths             []string          `mapstructure:"paths"`
	ScanOnStart       bool              `mapstructure:"scan_on_start"`
	Watch             bool              `mapstructure:"watch"`
	DuplicateHandling DuplicateHandling `mapstructure:"duplicate_handling"`
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
	Enabled            bool          `mapstructure:"enabled"`
	ListenAddrs        []string      `mapstructure:"listen_addrs"`
	AnnounceAddrs      []string      `mapstructure:"announce_addrs"`
	BootstrapPeers     []string      `mapstructure:"bootstrap_peers"`
	MDNSServiceTag     string        `mapstructure:"mdns_service_tag"`
	MaxPeers           int           `mapstructure:"max_peers"`
	StreamPort         int           `mapstructure:"stream_port"`
	AnnounceLibrary    bool          `mapstructure:"announce_library"`
	PerPeerRateLimit   int           `mapstructure:"per_peer_rate_limit"`
	UploadBandwidth    int           `mapstructure:"upload_bandwidth"`
	CBFailureThreshold int           `mapstructure:"cb_failure_threshold"`
	CBCooldown         time.Duration `mapstructure:"cb_cooldown"`
	ConnMgrLowMark     int           `mapstructure:"conn_mgr_low_mark"`
	ConnMgrHighMark    int           `mapstructure:"conn_mgr_high_mark"`
	ConnMgrGrace       time.Duration `mapstructure:"conn_mgr_grace"`
	LANOnly            bool          `mapstructure:"lan_only"`
	MaxKnownPeers      int           `mapstructure:"max_known_peers"`
	ChunkSize          int           `mapstructure:"chunk_size"`
	PeerDataTTL        time.Duration `mapstructure:"peer_data_ttl"`
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
