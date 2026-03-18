package constants

import "time"

// Network Configuration
const (
	// DefaultHost is the default listen host address
	DefaultHost = "0.0.0.0"

	// DefaultPort is the default listen port for peer connections
	DefaultPort = 45678

	// DefaultHTTPPort is the default HTTP API port
	DefaultHTTPPort = 8080

	// EnvAuthSecret is the environment variable name for shared auth secret
	EnvAuthSecret = "AUTH_SECRET"
)

// DefaultBootstrapPeers are public IPFS/Libp2p bootstrap nodes for DHT
// These are well-known public nodes maintained by the libp2p community
var DefaultBootstrapPeers = []string{
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmQCU2EcMqAqQPR2i9bChDtGNJchTbq5TbXJJ16u19uLTa",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmbLHAnMoJPWSCR5Zhtx6BHJX9KiKNN6tpvbUcqanj75Nb",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmcZf59bWwK5XFi76CZX8cbJ4BhTzzA3gU1ZjYZcYW3dwt",
	"/ip4/104.131.131.82/tcp/4001/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ",
}

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

	// PeerHealthCheckInterval is how often to check peer connectivity
	PeerHealthCheckInterval = 2 * time.Minute

	// PeerPingTimeout is the timeout for ping requests
	PeerPingTimeout = 10 * time.Second

	// PeerMetricsLogInterval is how often to log connection metrics
	PeerMetricsLogInterval = 30 * time.Second

	// NonceExpiry is how long used nonces are kept (prevents reuse within token validity)
	NonceExpiry = 24 * time.Hour
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
	// HTTPClientTimeout is the timeout for HTTP client requests
	HTTPClientTimeout = 10 * time.Second

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
