package constants

import "time"

// Network Configuration
const (
	// DefaultHost is the default listen host address
	DefaultHost = "0.0.0.0"

	// DefaultPort is the default listen port for peer connections
	DefaultPort = 45678

	// EnvAuthSecret is the environment variable name for shared auth secret
	EnvAuthSecret = "AUTH_SECRET"
)

// Peer Discovery
const (
	// DefaultRendezvous is the default DHT rendezvous string
	DefaultRendezvous = "raag-music-share"

	// DefaultMaxPeers is the default maximum number of connected peers
	DefaultMaxPeers = 100
)

// Discovery Intervals
const (
	// MDNSLogInterval is how often to log mDNS peer count
	MDNSLogInterval = 10 * time.Second

	// MDNSRefreshInterval is how often to refresh mDNS discovery (5 minutes)
	MDNSRefreshInterval = 5 * time.Minute

	// MDNSRetryInitialDelay is the starting backoff delay for mDNS retries
	MDNSRetryInitialDelay = 10 * time.Second

	// MDNSRetryMaxDelay is the max backoff delay for mDNS retries
	MDNSRetryMaxDelay = 60 * time.Second

	// MDNSServiceNameDefault is the default mDNS service name
	MDNSServiceNameDefault = "_p2p._udp"

	// DHTBootstrapDelay is the startup delay before first DHT rendezvous
	DHTBootstrapDelay = 30 * time.Second
)

// Timeouts
const (
	// RPCTimeout is the timeout for RPC calls
	RPCTimeout = 10 * time.Second
)

// Protocol
const (
	// ProtocolID is the libp2p protocol identifier for raag
	ProtocolID = "/raag/1.0.0"
)

// Bitswap
const (
	// BitswapProtocolID is the bitswap protocol identifier
	BitswapProtocolID = "/ipfs/bitswap/1.2.0"

	// ProvideInterval is how often to re-announce local CIDs to the DHT
	ProvideInterval = 22 * time.Hour
)

// Playback
const (
	// DefaultVolume is the default playback volume (0-100)
	DefaultVolume = 50

	// BufferSize is the audio buffer size in bytes
	BufferSize = 8192
)

// GossipSub / PubSub
const (
	// LibraryAnnounceTopic is for library change announcements
	LibraryAnnounceTopic = "/raag/library/1.0.0"
)
