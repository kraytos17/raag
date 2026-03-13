package constants

import (
	"time"
)

// Network Configuration
const (
	// DefaultPort is the default listen port for peer connections
	DefaultPort = 45678

	// DefaultHTTPPort is the default HTTP API port
	DefaultHTTPPort = 8080

	// DefaultTrackerURL is the default tracker URL for peer discovery (fallback)
	// Can be overridden via TRACKER_URL environment variable or --tracker flag
	DefaultTrackerURL = "https://raag-production.up.railway.app"

	// EnvTrackerURL is the environment variable name for tracker URL
	EnvTrackerURL = "TRACKER_URL"
)

// Peer Discovery
const (
	// DefaultRendezvous is the default DHT rendezvous string
	DefaultRendezvous = "raag-music-share"

	// DefaultMaxPeers is the default maximum number of connected peers
	DefaultMaxPeers = 100

	// PeerCleanupInterval is how often to clean up stale peers
	PeerCleanupInterval = 5 * time.Minute

	// PeerTimeout is how long before a peer is considered stale
	PeerTimeout = 10 * time.Minute
)

// Discovery Intervals
const (
	// DHTAdvertisementInterval is how often to re-advertise on DHT
	DHTAdvertisementInterval = 30 * time.Minute

	// DHTDiscoveryInterval is how often to run DHT discovery
	DHTDiscoveryInterval = 1 * time.Minute

	// DHTLogInterval is how often to log DHT routing table size
	DHTLogInterval = 10 * time.Second

	// MDNSLogInterval is how often to log mDNS peer count
	MDNSLogInterval = 10 * time.Second

	// TrackerRetryInterval is how long to wait before retrying tracker connection
	TrackerRetryInterval = 5 * time.Minute

	// TrackerHeartbeatInterval is how often peers refresh tracker registration
	TrackerHeartbeatInterval = 2 * time.Minute

	// TrackerRefreshInterval is how often peers fetch tracker peer lists
	TrackerRefreshInterval = 2 * time.Minute

	// TrackerRetryInitialDelay is the starting backoff delay for tracker retries
	TrackerRetryInitialDelay = 1 * time.Second

	// TrackerRetryMaxDelay is the max backoff delay for tracker retries
	TrackerRetryMaxDelay = 30 * time.Second
)

// Tracker Authentication
const (
	// AuthTokenLength is the expected length of auth tokens
	AuthTokenLength = 32

	// TokenExpiration is how long auth tokens are valid
	TokenExpiration = 24 * time.Hour

	// EnvAuthKey is the environment variable name for tracker auth key(s)
	// Supports multiple keys comma-separated
	EnvAuthKey = "AUTH_KEY"
)

// Timeouts
const (
	// DHTQueryTimeout is the timeout for DHT queries
	DHTQueryTimeout = 30 * time.Second

	// DHTWaitForPeersTimeout is how long to wait for peers to connect
	DHTWaitForPeersTimeout = 10 * time.Second

	// DHTRetryDelay is how long to wait before retrying DHT discovery
	DHTRetryDelay = 10 * time.Second

	// HTTPClientTimeout is the timeout for HTTP client requests
	HTTPClientTimeout = 10 * time.Second

	// TransferIdleTimeout is the max idle time for a transfer stream
	TransferIdleTimeout = 30 * time.Second

	// TransferMaxMetadataSize is the max allowed metadata frame size
	TransferMaxMetadataSize = 64 * 1024

	// TransferMaxFileSize is the max allowed inbound file size in bytes
	TransferMaxFileSize = 256 * 1024 * 1024
)

// Protocol
const (
	// ProtocolID is the libp2p protocol identifier for raag
	ProtocolID = "/raag/1.0.0"

	// ShareProtocolID is the framed transfer protocol identifier for Raag
	ShareProtocolID = "/raag/share/2.0.0"

	// PingProtocolID is the ping/presence protocol identifier for Raag
	PingProtocolID = "/raag/ping/1.0.0"

	// ProtocolVersion indicates the protocol version
	ProtocolVersion = "1.0.0"

	// ShareProtocolVersion indicates the framed transfer protocol version
	ShareProtocolVersion = "2.0.0"

	// PingProtocolVersion indicates the ping protocol version
	PingProtocolVersion = "1.0.0"
)

// Playback
const (
	// DefaultVolume is the default playback volume (0-100)
	DefaultVolume = 50

	// BufferSize is the audio buffer size in bytes
	BufferSize = 8192
)

// Configuration Keys
const (
	ConfigKeyMusicDir       = "musicdir"
	ConfigKeyVolume         = "volume"
	ConfigKeyHost           = "host"
	ConfigKeyPort           = "port"
	ConfigKeyTui            = "tui"
	ConfigKeyNetwork        = "network"
	ConfigKeyLogLevel       = "loglevel"
	ConfigKeyRendezvous     = "rendezvous"
	ConfigKeyDHTEnabled     = "discovery.dht_enabled"
	ConfigKeyTrackerURL     = "tracker.url"
	ConfigKeyBootstrapPeers = "discovery.bootstrap_peers"
	ConfigKeyMaxPeers       = "discovery.max_peers"
)
