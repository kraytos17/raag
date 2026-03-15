# Raag

Raag is a production-ready P2P music streaming application with terminal-first UI, featuring local playback, playlist management, and secure peer-to-peer music sharing over libp2p.

## Features

- **Local Playback** - Play music from a local directory with full playback controls
- **Playlist Management** - Create, manage, and play playlists from CLI or TUI
- **Secure P2P Sharing** - Share music with peers using encrypted libp2p connections
- **Multi-NAT Traversal** - Hole punching, UPnP, and AutoNAT for direct peer connections
- **Peer Discovery** - mDNS (LAN), DHT (decentralized), and optional tracker (cross-network)
- **Daemon Mode** - Persistent peer connections for continuous availability
- **End-to-End Auth** - Token-based authentication with replay protection

## Quick Start

### Build

```bash
make build          # Production binaries
make build-dev      # Debug builds
make build-race     # Race detector build
```

Or manually:
```bash
go build -o bin/raag ./cmd/raag
go build -o bin/tracker ./tracker/cmd/tracker
```

### Run

```bash
# Local playback with TUI
./bin/raag --tui

# Network mode (peer discovery enabled)
./bin/raag --network

# Daemon mode (persistent connections)
./bin/raag daemon --network
```

### Run with Tracker

```bash
# Start tracker (on public server)
./bin/tracker --http-port 8080 --auth-key <your-auth-key>

# Connect client to tracker
./bin/raag daemon --network --tracker https://your-tracker.example.com
```

## Commands

### Playback

```bash
./bin/raag play <song>
./bin/raag pause
./bin/raag resume
./bin/raag stop
./bin/raag next
./bin/raag previous
./bin/raag seek [seconds]
./bin/raag volume [0-100]
./bin/raag queue
./bin/raag nowplaying
```

### Library

```bash
./bin/raag library list
./bin/raag library search [query]
./bin/raag library rescan
./bin/raag library add <path>
./bin/raag library remove <title>
```

### Playlists

```bash
./bin/raag playlist create <name>
./bin/raag playlist delete <name>
./bin/raag playlist list
./bin/raag playlist add <playlist> <song>
./bin/raag playlist remove <playlist> <index>
./bin/raag playlist songs <playlist>
./bin/raag playlist play <playlist>
```

### Networking

```bash
./bin/raag peers list
./bin/raag peers info
./bin/raag peers ping <peerID>
./bin/raag peers connect <multiaddr>
./bin/raag peers disconnect <peerID>
./bin/raag peers tracker <url>
./bin/raag peers bootstrap <multiaddr>
./bin/raag network status
./bin/raag network auth-key
```

### Daemon & Config

```bash
./bin/raag daemon
./bin/raag status
./bin/raag config show
./bin/raag config set <key> <value>
```

## Flags

### Client

| Flag | Default | Description |
|------|---------|-------------|
| `--network` | `false` | Enable peer discovery |
| `--host` | `0.0.0.0` | Bind address |
| `--port` | `45678` | Peer listen port |
| `--tracker` | (none) | Tracker URL |
| `--dht` | `true` | Enable DHT discovery |
| `--bootstrap` | - | Bootstrap peers (comma-separated) |
| `--max-peers` | `100` | Maximum peers |
| `--music-dir` | `./music` | Music directory |
| `--tui` | `false` | Start TUI |
| `--json` | `false` | JSON logging |

### Tracker

| Flag | Default | Description |
|------|---------|-------------|
| `--http-port` | `8080` | HTTP API port |
| `--libp2p-port` | `45678` | Libp2p port |
| `--relay` | `true` | Enable relay |
| `--auth-key` | - | Trusted auth key (repeatable) |

## Security

### Authentication Model

Raag implements production-grade P2P authentication:

1. **Identity Binding** - Auth key derived from libp2p identity (`identity.key`)
2. **Token-based Auth** - Each peer has a signed authentication token
3. **Peer ID Verification** - Tokens bound to specific peer IDs
4. **Session Nonces** - Unique per-transfer nonces prevent replay attacks

### Trust Flow

```
identity.key → Peer ID
identity.key → Derived Auth Key → Signed Token
Token + Peer ID → Tracker Registration → Peer Trust
```

### Get Your Auth Key

```bash
./bin/raag --network network auth-key
```

### Run Authenticated Tracker

```bash
./bin/tracker --http-port 8080 --auth-key <your-key>
```

Multiple keys:
```bash
./bin/tracker --auth-key KEY1 --auth-key KEY2 --auth-key KEY3
```

## Architecture

### Components

```
raag CLI/TUI
    ├── config + storage
    ├── library + playlists + player
    └── network manager
            ├── identity.key (persistent)
            ├── libp2p host (TCP + QUIC)
            ├── mDNS discovery (LAN)
            ├── DHT discovery (decentralized)
            ├── tracker client (registration)
            ├── NAT traversal (hole punch, UPnP, AutoNAT)
            └── file transfer protocol
```

### Discovery Mechanisms

- **mDNS** - Local network peer discovery
- **DHT** - Decentralized peer discovery via Kademlia
- **Tracker** - Centralized peer registration (optional)

### File Transfer

- Framed libp2p stream protocol
- Metadata: title, artist, album, size, SHA-256
- Encrypted via libp2p's Noise protocol
- Idle timeout protection
- Atomic write with temp files

## Deployment

### Local Tracker

```bash
make run-tracker
# or
./bin/tracker --http-port 8080
```

### Docker

```bash
docker build -t raag .
docker run -p 8080:8080 -p 45678:45678 -e AUTH_KEY=<key> raag
```

### Railway

Set environment variables:
- `PORT` - HTTP port (provided by Railway)
- `AUTH_KEY` - Trusted peer keys

### Environment Variables (Tracker)

| Variable | Description |
|----------|-------------|
| `PORT` | HTTP API port (default: 8080) |
| `AUTH_KEY` | Comma-separated trusted keys |

## Makefile Commands

| Command | Description |
|---------|-------------|
| `make build` | Production build |
| `make build-dev` | Debug build |
| `make build-race` | Race detector build |
| `make test` | Run tests |
| `make test-race` | Tests with race detector |
| `make ci` | Full CI pipeline |
| `make run` | Build and run raag |
| `make run-tracker` | Build and run tracker |
| `make lint` | Format + vet + staticcheck |
| `make clean` | Remove artifacts |

## Files & Storage

Config stored in `~/.config/raag/`:

- `config.yaml` - Configuration
- `state.json` - Playback state
- `identity.key` - Libp2p identity (critical!)
- `daemon.sock` - Daemon socket
- `playlists.json` - Playlist data
- `peers.json` - Discovered peers cache

**Important:** `identity.key` determines your peer ID and auth key. Keep it safe!

## Troubleshooting

### No peers discovered

```bash
./bin/raag network status
# Check:
# - --network flag used
# - Tracker URL reachable
# - Auth key trusted by tracker
# - identity.key unchanged
```

### Tracker rejects registration

```bash
# Get your auth key
./bin/raag network auth-key

# Verify it matches tracker's --auth-key
```

### Peer connection issues

- Check NAT/firewall configuration
- Use tracker for cross-network peers
- Try manual ping: `./bin/raag peers ping <peer-id>`

## License

Apache License 2.0
