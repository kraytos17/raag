package config

import (
	"time"

	"github.com/spf13/viper"
)

func setDefaults(v *viper.Viper) {
	v.SetDefault("library.paths", []string{})
	v.SetDefault("library.scan_on_start", true)
	v.SetDefault("library.watch", true)

	v.SetDefault("playback.volume", 80)
	v.SetDefault("playback.output_device", "default")
	v.SetDefault("playback.buffer_size", 4096)
	v.SetDefault("playback.sample_rate", 44100)

	v.SetDefault("daemon.socket_path", "/tmp/raag.sock")
	v.SetDefault("daemon.log_level", "info")
	v.SetDefault("daemon.data_dir", "~/.local/share/raag")
	v.SetDefault("daemon.pid_file", "/tmp/raagd.pid")

	v.SetDefault("p2p.enabled", true)
	v.SetDefault("p2p.listen_addrs", []string{
		"/ip4/0.0.0.0/tcp/7844",
		"/ip4/0.0.0.0/udp/7844/quic-v1",
	})
	v.SetDefault("p2p.mdns_service_tag", "raag-local")
	v.SetDefault("p2p.max_peers", 20)
	v.SetDefault("p2p.stream_port", 7845)
	v.SetDefault("p2p.announce_library", true)
	v.SetDefault("p2p.per_peer_rate_limit", 10)
	v.SetDefault("p2p.upload_bandwidth", 0)

	v.SetDefault("transcoder.ffmpeg_path", "ffmpeg")
	v.SetDefault("transcoder.stream_codec", "opus")
	v.SetDefault("transcoder.stream_bitrate", "128k")

	v.SetDefault("privacy.share_library_manifest", true)
	v.SetDefault("privacy.share_play_history", false)
	v.SetDefault("privacy.announce_on_discovery", true)

	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.port", 9200)
	v.SetDefault("metrics.path", "/metrics")
}

const (
	DefaultSocketPath  = "/tmp/raag.sock"
	DefaultDataDir     = "~/.local/share/raag"
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
	DefaultRetryMaxAttempts  = 3
	DefaultRetryInitialDelay = 100 * time.Millisecond
	DefaultRetryMaxDelay     = 5 * time.Second
	DefaultRetryMultiplier   = 2.0
)

const (
	DefaultCircuitBreakerThreshold = 5
	DefaultCircuitBreakerCooldown  = 30 * time.Second
)

const (
	DefaultSearchLimit    = 20
	DefaultFuzzyThreshold = 3
)
