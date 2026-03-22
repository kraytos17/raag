package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

const (
	DefaultLogLevel    = "info"
	DefaultSampleRate  = 44100
	DefaultBufferSize  = 4096
	DefaultVolume      = 80
	DefaultMaxPeers    = 20
	DefaultStreamPort  = 7845
	DefaultMetricsPort = 9200
)

var DefaultListenAddrs = []string{
	"/ip4/0.0.0.0/tcp/7844",
	"/ip4/0.0.0.0/udp/7844/quic-v1",
}

const (
	DefaultMDNSServiceTag = "raag-local"
	DefaultStreamCodec    = "opus"
	DefaultStreamBitrate  = "128k"
)

const (
	DefaultCircuitBreakerThreshold = 5
	DefaultCircuitBreakerCooldown  = 30 * time.Second
)

const (
	DefaultSearchLimit    = 20
	DefaultFuzzyThreshold = 3
)

func getDefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	if home == "" {
		slog.Error("cannot determine home directory, using current directory")
		return "./data"
	}
	return filepath.Join(home, ".local", "share", "raag")
}

func setDefaults(v *viper.Viper) {
	dataDir := getDefaultDataDir()

	v.SetDefault("library.paths", []string{})
	v.SetDefault("library.scan_on_start", true)
	v.SetDefault("library.watch", true)

	v.SetDefault("playback.volume", DefaultVolume)
	v.SetDefault("playback.output_device", "default")
	v.SetDefault("playback.buffer_size", DefaultBufferSize)
	v.SetDefault("playback.sample_rate", DefaultSampleRate)

	v.SetDefault("daemon.socket_path", filepath.Join(dataDir, "raag.sock"))
	v.SetDefault("daemon.log_level", DefaultLogLevel)
	v.SetDefault("daemon.data_dir", dataDir)
	v.SetDefault("daemon.pid_file", filepath.Join(dataDir, "raagd.pid"))

	v.SetDefault("p2p.enabled", false)
	v.SetDefault("p2p.listen_addrs", DefaultListenAddrs)
	v.SetDefault("p2p.mdns_service_tag", DefaultMDNSServiceTag)
	v.SetDefault("p2p.max_peers", DefaultMaxPeers)
	v.SetDefault("p2p.stream_port", DefaultStreamPort)
	v.SetDefault("p2p.announce_library", true)
	v.SetDefault("p2p.per_peer_rate_limit", 10)
	v.SetDefault("p2p.upload_bandwidth", 0)

	v.SetDefault("transcoder.ffmpeg_path", "ffmpeg")
	v.SetDefault("transcoder.stream_codec", DefaultStreamCodec)
	v.SetDefault("transcoder.stream_bitrate", DefaultStreamBitrate)

	v.SetDefault("privacy.share_library_manifest", true)
	v.SetDefault("privacy.share_play_history", false)
	v.SetDefault("privacy.announce_on_discovery", true)

	v.SetDefault("metrics.enabled", false)
	v.SetDefault("metrics.port", DefaultMetricsPort)
	v.SetDefault("metrics.path", "/metrics")
}
