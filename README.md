# Raag

[![CI Status](https://img.shields.io/github/actions/workflow/status/p-society/raag/.github/workflows/ci.yml?branch=main)](https://github.com/p-society/raag/actions)
[![Go Version](https://img.shields.io/badge/Go-1.26-blue)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue)](https://www.apache.org/licenses/LICENSE-2.0)
[![Coverage](https://img.shields.io/badge/coverage-17.8%25-yellow)](#test-coverage)
[![Security](https://img.shields.io/badge/security-gosec%200%20issues-green)](#security-practices)

A production-ready, terminal-first P2P music streaming application with local playback, playlist management, and secure peer-to-peer music sharing over libp2p.

## Table of Contents

- [Features](#features)
- [Quick Start](#quick-start)
- [Prerequisites](#prerequisites)
- [Getting Started](#getting-started)
- [Architecture](#architecture)
- [Authentication](#authentication)
- [File Transfer](#file-transfer)
- [Security Practices](#security-practices)
- [Configuration](#configuration)
- [Deployment](#deployment)
- [Commands](#commands)
- [Monitoring](#monitoring)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)
- [License](#license)

## Features

| Category | Feature | Description |
|----------|---------|-------------|
| **Playback** | Local Playback | Play music from a local directory with full playback controls |
| | Format Support | MP3, FLAC, WAV, OGG/Vorbis |
| | Controls | Play, pause, stop, seek, volume, shuffle, repeat |
| **Playlists** | CRUD Operations | Create, modify, and delete playlists |
| | Management | Add, remove, and reorder songs via CLI or TUI |
| **Sharing** | P2P Protocol | Encrypted libp2p connections for music sharing |
| | Discovery | mDNS (LAN), DHT (decentralized), tracker (cross-network) |
| | NAT Traversal | Hole punching, UPnP, and AutoNAT for direct connections |
| **Operation** | Daemon Mode | Persistent peer connections for continuous availability |
| | TUI | Terminal user interface for interactive use |
| | RPC API | Unix socket RPC for programmatic access |

## Quick Start

### Installation

```bash
# Clone the repository
git clone https://github.com/p-society/raag.git
cd raag

# Build the binaries
make build

# Verify the build
./bin/raag --version
./bin/tracker --version
```

### First Run

```bash
# Start the daemon with networking
./bin/raag daemon --network

# In another terminal, use the CLI
./bin/raag library add /path/to/your/music
./bin/raag library list
./bin/raag play "Song Title"
```

See [Getting Started](#getting-started) for a complete walkthrough.

## Prerequisites

### System Requirements

| Requirement | Minimum | Recommended |
|-------------|---------|-------------|
| Go | 1.26 | 1.26+ |
| CPU | x86-64 | x86-64 / ARM64 |
| Memory | 256 MB | 512 MB+ |
| Storage | 10 MB | 100 MB+ |
| Network | Broadband | Broadband |

### Dependencies

- **ALSA Development Libraries** (for audio playback on Linux)
- **Libp2p** (included via Go modules)
- **Tag Library** (for metadata extraction, included)

#### Ubuntu/Debian

```bash
sudo apt-get update
sudo apt-get install -y libasound2-dev
```

#### macOS

```bash
# ALSA not required on macOS
brew install coreutils  # for gtimeout if needed
```

#### Windows

Windows is not officially supported for the daemon, but the tracker can run via WSL2.

### Network Requirements

| Port | Protocol | Purpose |
|------|----------|---------|
| 45678 | TCP/QUIC | Libp2p peer connections |
| 8080 | TCP | Tracker HTTP API (tracker only) |

## Getting Started

This section provides a step-by-step guide for new users.

### Step 1: Configure Music Directory

```bash
# Set your music directory (defaults to ~/.config/raag/music)
export MUSIC_DIR=/path/to/your/music
```

Or configure via command line:

```bash
./bin/raag --music-dir /path/to/your/music daemon --network
```

### Step 2: Start the Daemon

```bash
# Start in daemon mode with networking enabled
./bin/raag daemon --network

# Check the status
./bin/raag status
```

The daemon will:
1. Initialize the music library
2. Start peer discovery (mDNS for LAN, DHT for internet)
3. Register with the tracker (if configured)
4. Listen for incoming peer connections

### Step 3: Add Music to Your Library

```bash
# Scan a directory for music
./bin/raag library add /path/to/music

# List all songs
./bin/raag library list

# Search for a specific song
./bin/raag library search "artist name"
```

### Step 4: Play Music

```bash
# List available songs
./bin/raag library list

# Play a specific song
./bin/raag play "Song Title"

# Control playback
./bin/raag pause
./bin/raag resume
./bin/raag stop
./bin/raag next
./bin/raag previous

# Adjust volume
./bin/raag volume 75
```

### Step 5: Connect to Peers

```bash
# List discovered peers
./bin/raag peers list

# Get your multiaddress to share with others
./bin/raag network status

# Connect to a peer manually
./bin/raag peers connect /ip4/1.2.3.4/tcp/45678/p2p/12D3KooW...

# Share a song with a peer
./bin/raag peers share "Song Title" --peer 12D3KooW...
```

## Architecture

### Component Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         Raag Client                             │
├─────────────────────────────────────────────────────────────────┤
│  CLI / TUI                                                      │
│       │                                                         │
│       ▼                                                         │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │                    Daemon (raagd)                        │   │
│  ├─────────────────────────────────────────────────────────┤   │
│  │  ┌─────────────┐ ┌─────────────┐ ┌─────────────────┐    │   │
│  │  │   Player    │ │  Playlist   │ │    Library      │    │   │
│  │  │             │ │  Manager    │ │    Manager      │    │   │
│  │  └─────────────┘ └─────────────┘ └─────────────────┘    │   │
│  │                        │                                   │   │
│  │                        ▼                                   │   │
│  │  ┌─────────────────────────────────────────────────────┐ │   │
│  │  │              Network Manager                         │ │   │
│  │  ├─────────────────────────────────────────────────────┤ │   │
│  │  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌───────────┐  │ │   │
│  │  │  │Identity │ │Libp2p   │ │Discovery│ │  Tracker  │  │ │   │
│  │  │  │   Key   │ │  Host   │ │ Manager │ │  Client   │  │ │   │
│  │  │  └─────────┘ └─────────┘ └─────────┘ └───────────┘  │ │   │
│  │  └─────────────────────────────────────────────────────┘ │   │
│  └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘

                          │
                          │ Optional
                          ▼

┌─────────────────────────────────────────────────────────────────┐
│                    Tracker Server                               │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  HTTP API  │  Libp2p  │  Auth Service  │  Peer Store   │   │
│  └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

### Data Flow

1. **Music Playback**
   - User selects a song via CLI/TUI
   - Player reads file from local disk
   - Audio decoded and played via ALSA/speaker

2. **Peer Discovery**
   - mDNS: Broadcasts on local network
   - DHT: Decentralized Kademlia DHT
   - Tracker: Central registration server

3. **Peer Connection**
   - Connection established via libp2p
   - Noise protocol for encryption
   - Auth token verification

4. **File Transfer**
   - Framed protocol with metadata header
   - SHA-256 hash verification
   - Atomic file writes with temp files

### Directory Structure

```
~/.config/raag/
├── config.yaml          # Main configuration
├── state.json           # Playback state
├── identity.key         # Libp2p identity (KEEP SAFE!)
├── daemon.sock          # RPC socket
├── playlists.json       # Saved playlists
├── peers.json           # Discovered peers cache
└── music/               # Music directory
    ├── artist/
    │   └── album/
    │       └── song.mp3
    └── ...
```

## Authentication

Raag supports two authentication mechanisms for peer-to-peer connections.

### Option 1: Shared Secret (Recommended)

Uses a password to derive cryptographic keys for all peers.

**Tracker Configuration:**

```bash
export AUTH_SECRET=your-secure-password
./bin/tracker --http-port 8080 --relay
```

**Client Configuration:**

```bash
./bin/raag daemon --network --auth-secret your-secure-password
```

### Option 2: Per-Peer Keys (More Secure)

Each peer has a unique identity key.

**Get Your Auth Key:**

```bash
./bin/raag network auth-key
# Output: 23eb477a60833ed094e456110928584093d349b35f6f793f5057cc781aee586d
```

**Tracker Configuration:**

```bash
./bin/tracker --http-port 8080 --relay \
  --auth-key 23eb477a60833ed094e456110928584093d349b35f6f793f5057cc781aee586d \
  --auth-key another-key-here
```

**Client Configuration:**

```bash
# No extra flag needed - uses identity.key automatically
./bin/raag daemon --network
```

### How Authentication Works

```
┌────────────────────┐     ┌────────────────────┐     ┌────────────────────┐
│      Peer A        │     │      Tracker       │     │      Peer B        │
└────────────────────┘     └────────────────────┘     └────────────────────┘
         │                         │                          │
         │  1. Register with       │                          │
         │     derived key         │                          │
         │────────────────────────▶│                          │
         │                         │                          │
         │                         │  2. Store peer info      │
         │                         │                          │
         │  3. Get auth token      │                          │
         │◀────────────────────────│                          │
         │                         │                          │
         │  4. Connect to peer B   │                          │
         │     + auth token        │                          │
         │─────────────────────────┼─────────────────────────▶
         │                         │                          │
         │                         │     5. Verify token      │
         │                         │◀─────────────────────────
         │                         │                          │
         │  6. Connection established (encrypted)              │
         │◀────────────────────────────────────────────────────
```

### Security Properties

| Property | Implementation |
|----------|----------------|
| Password Security | PBKDF2 key derivation (50,000 iterations) |
| Token Expiry | 24 hours with automatic refresh at 1 hour |
| Replay Protection | Nonce tracking with 5-minute window |
| Tampering Prevention | Ed25519 signature verification |
| Rate Limiting | 10 requests per minute per IP |

## File Transfer

### Protocol

Raag uses a custom framed protocol over libp2p streams for file transfers.

**Transfer Sequence:**

```
1. Handshake
   Peer A → Peer B: [version, song metadata, file size, SHA-256]

2. Data Transfer
   Peer A → Peer B: [length prefix (4 bytes)][data chunk]

3. Verification
   Peer B: Verify SHA-256 hash matches
```

### Security Guarantees

| Guarantee | Implementation |
|-----------|----------------|
| Integrity | SHA-256 hash verification |
| Confidentiality | Libp2p Noise protocol encryption |
| Atomicity | Write to temp file, then rename |
| Size Limits | Maximum 256 MB per file |

### Performance

| Metric | Value |
|--------|-------|
| Idle Timeout | 30 seconds |
| Chunk Size | 32 KB |
| Max Metadata Size | 4 KB |

## Security Practices

This project follows security best practices and passes all security scans.

### Security Audits

| Tool | Purpose | Status |
|------|---------|--------|
| gosec | Static Application Security Testing | 0 issues |
| CodeQL | Code analysis | Passing |
| Trivy | Container scanning | Passing |
| Dependency Review | Vulnerability scanning | Passing |

### Code Security Measures

| Vulnerability | Mitigation |
|--------------|------------|
| Path Traversal | `os.Root` for scoped file access |
| Integer Overflow | Explicit bounds checking |
| Input Validation | `filepath.Clean()` + directory confinement |

### Security Best Practices

1. **Identity Key Protection**
   - Stored with 0600 permissions
   - Never transmitted over network
   - Back up your `identity.key` file

2. **Authentication Tokens**
   - Expires after 24 hours
   - Automatically refreshed
   - Nonce prevents replay attacks

3. **Network Security**
   - All connections use Noise protocol
   - Auth tokens verified before data transfer
   - Rate limiting on tracker endpoints

### Reporting Security Issues

For security vulnerabilities, please report them through GitHub's security advisory channel.

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MUSIC_DIR` | `~/.config/raag/music` | Music library directory |
| `CONFIG_DIR` | `~/.config/raag` | Configuration directory |
| `AUTH_SECRET` | (none) | Shared authentication secret |
| `AUTH_KEY` | (none) | Comma-separated trusted auth keys |
| `TRACKER_URL` | (none) | Tracker server URL |
| `LOG_LEVEL` | `info` | Logging level (debug, info, warn, error) |
| `XDG_CONFIG_HOME` | `~/.config` | Base config directory |

### CLI Flags

#### Client Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--network` | `false` | Enable peer discovery |
| `--host` | `0.0.0.0` | Bind address |
| `--port` | `45678` | Peer listen port |
| `--tracker` | (none) | Tracker URL |
| `--auth-secret` | (none) | Shared secret for auth |
| `--dht` | `true` | Enable DHT discovery |
| `--bootstrap` | (none) | Bootstrap peers (comma-separated) |
| `--max-peers` | `100` | Maximum peers |
| `--music-dir` | `~/.config/raag/music` | Music directory |
| `--tui` | `false` | Start TUI |
| `--json` | `false` | JSON logging |
| `--log-level` | `info` | Log level |

#### Tracker Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--http-port` | `8080` | HTTP API port |
| `--libp2p-port` | `45678` | Libp2p port |
| `--relay` | `true` | Enable circuit relay |
| `--auth-key` | (none) | Trusted auth key (repeatable) |
| `--auth-secret` | (none) | Shared secret for auth |
| `--tls` | `false` | Enable HTTPS |
| `--tls-cert` | (none) | TLS certificate file path |
| `--tls-key` | (none) | TLS key file path |

### Configuration File

Example `~/.config/raag/config.yaml`:

```yaml
# Raag Configuration

# Network Settings
host: "0.0.0.0"
port: 45678
network: true
dht: true
max_peers: 100

# Discovery Settings
tracker_url: "https://your-tracker.example.com"
bootstrap_peers:
  - "/ip4/1.2.3.4/tcp/45678/p2p/12D3KooW..."

# Authentication
auth_secret: ""  # Use identity.key if empty

# Playback
volume: 75
music_dir: ~/.config/raag/music

# Logging
log_level: info
json_logs: false
```

## Deployment

### Docker

```bash
# Build the image
docker build -t raag .

# Run the tracker
docker run -d \
  --name raag-tracker \
  -p 8080:8080 \
  -p 45678:45678 \
  -e AUTH_SECRET=your-secret \
  raag ./bin/tracker --http-port 8080 --relay

# Run the client
docker run -d \
  --name raag-client \
  -v /path/to/music:/music \
  -v raag-config:/config \
  raag ./bin/raag daemon --network --music-dir /music
```

### Docker Compose

```yaml
version: '3.8'

services:
  tracker:
    image: raag:latest
    ports:
      - "8080:8080"
      - "45678:45678"
    environment:
      - AUTH_SECRET=your-secret
      - TLS_ENABLED=false
    volumes:
      - tracker-data:/data
    restart: unless-stopped

  client:
    image: raag:latest
    ports:
      - "45678:45678"
    volumes:
      - ./music:/music:ro
      - client-config:/config
    command: daemon --network --music-dir /music
    restart: unless-stopped

volumes:
  tracker-data:
  client-config:
```

### Kubernetes

Example deployment manifests are available in `deployments/kubernetes/`.

### Production Checklist

- [ ] Configure authentication (shared secret or per-peer keys)
- [ ] Set up TLS for tracker (required for production)
- [ ] Configure persistent storage for music and config
- [ ] Set up monitoring and alerting
- [ ] Configure resource limits
- [ ] Set up log aggregation
- [ ] Backup identity.key file
- [ ] Test failover procedures

## Commands

### Playback Commands

```bash
# Playback control
./bin/raag play <song>              # Play a song
./bin/raag play "artist:song"       # Play matching song
./bin/raag pause                    # Pause playback
./bin/raag resume                   # Resume playback
./bin/raag stop                     # Stop playback
./bin/raag next                     # Next track
./bin/raag previous                 # Previous track

# Seeking
./bin/raag seek +30                 # Forward 30 seconds
./bin/raag seek -30                 # Backward 30 seconds
./bin/raag seek 120                 # Seek to 2:00

# Volume
./bin/raag volume                   # Show current volume
./bin/raag volume 75                # Set to 75%
./bin/raag volume +10               # Increase by 10%
./bin/raag volume -10               # Decrease by 10%

# Queue
./bin/raag queue                    # Show queue
./bin/raag queue clear              # Clear queue
./bin/raag queue shuffle            # Shuffle queue

# Now Playing
./bin/raag nowplaying               # Show current track
./bin/raag status                   # Full playback status
```

### Library Commands

```bash
./bin/raag library list             # List all songs
./bin/raag library search <query>   # Search songs
./bin/raag library add <path>       # Add directory to library
./bin/raag library rescan           # Rescan library
./bin/raag library remove <title>   # Remove from library
./bin/raag library by <artist>      # List songs by artist
./bin/raag library in <album>       # List songs in album
```

### Playlist Commands

```bash
./bin/raag playlist create <name>   # Create playlist
./bin/raag playlist delete <name>   # Delete playlist
./bin/raag playlist list            # List all playlists
./bin/raag playlist show <name>     # Show playlist songs
./bin/raag playlist add <name> <song>  # Add song to playlist
./bin/raag playlist remove <name> <idx>  # Remove by index
./bin/raag playlist play <name>     # Play playlist
./bin/raag playlist shuffle <name>  # Shuffle playlist
```

### Peer Commands

```bash
./bin/raag peers list               # List connected peers
./bin/raag peers info <peer>        # Peer details
./bin/raag peers ping <peer>        # Ping peer
./bin/raag peers disconnect <peer>  # Disconnect peer
./bin/raag peers tracker <url>      # Set tracker URL
./bin/raag peers bootstrap <addr>   # Add bootstrap peer
./bin/raag peers discover           # Trigger discovery
```

### Network Commands

```bash
./bin/raag network status           # Network status
./bin/raag network auth-key         # Show auth public key
./bin/raag network multiaddr        # Show your multiaddress
./bin/raag network dht peers        # DHT peer count
./bin/raag network mdns peers       # mDNS peer count
```

### Daemon Commands

```bash
./bin/raag daemon                   # Start daemon
./bin/raag daemon --network         # Start with networking
./bin/raag status                   # Daemon status
./bin/raag stop                     # Stop daemon
./bin/raag config show              # Show config
./bin/raag config set <key> <val>   # Set config value
./bin/raag logs                     # Show daemon logs
```

### TUI

```bash
./bin/raag --tui                    # Start terminal UI
```

## Monitoring

### Logging

Raag supports structured logging with configurable levels.

```bash
# Set log level
./bin/raag --log-level debug daemon

# JSON logs for log aggregation
./bin/raag --json --log-level info daemon
```

### Health Checks

The tracker provides health check endpoints:

```bash
# Basic health
curl http://localhost:8080/health

# Detailed status
curl http://localhost:8080/status

# Peer list
curl http://localhost:8080/peers
```

### Metrics

Available metrics endpoints (future):

```bash
# Prometheus metrics (when enabled)
curl http://localhost:8080/metrics
```

## Troubleshooting

### Common Issues

#### No Peers Discovered

```bash
# Check network status
./bin/raag network status

# Enable debug logging
./bin/raag --log-level debug daemon --network

# Verify firewall allows inbound connections
sudo ufw status
```

#### Tracker Registration Fails

```bash
# Verify auth secret matches
echo $AUTH_SECRET

# Check tracker URL is reachable
curl -v https://your-tracker.example.com/peers

# For per-peer keys, verify your key is trusted
./bin/raag network auth-key
# Compare with tracker's trusted keys
```

#### Playback Issues

```bash
# Check audio device
aplay -l  # Linux
# or
system_profiler SPAudioDataType  # macOS

# Verify file permissions
ls -la /path/to/music/

# Check file format support
./bin/raag library add /path/to/music 2>&1
```

#### Connection Issues

```bash
# Verify port is open
nc -zv localhost 45678

# Check NAT configuration
./bin/raag network status

# Enable relay mode
./bin/raag daemon --network --relay
```

### Exit Codes

| Code | Meaning | Action |
|------|---------|--------|
| 0 | Success | None |
| 1 | General error | Check logs |
| 2 | Invalid arguments | Verify command syntax |
| 3 | Config error | Check configuration |
| 4 | Network error | Verify network connectivity |
| 5 | Authentication failed | Verify auth credentials |

### Getting Help

```bash
# Show help
./bin/raag --help
./bin/raag --help <command>

# Show version
./bin/raag --version

# Verbose output for debugging
./bin/raag --log-level debug <command>
```

## Contributing

### Development Setup

```bash
# Fork and clone the repository
git clone https://github.com/YOUR-USERNAME/raag.git
cd raag

# Create a feature branch
git checkout -b feature/my-feature

# Install development dependencies
make deps

# Run the full development workflow
make ci
```

### Code Style

- Format: `make fmt` (uses gofumpt)
- Lint: `make lint`
- Vet: `make vet`

### Testing

```bash
# Run all tests
make test

# Run with race detector
make test-race

# Run specific package
go test ./internal/auth/...
go test ./tracker/...
go test ./internal/network/...

# Generate coverage report
make coverage
```

### Pull Request Process

1. Ensure all tests pass: `make ci`
2. Update documentation as needed
3. Add tests for new functionality
4. Request review from maintainers

## License

Raag is licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for the full license text.

---

**Version:** 0.1.0  
**Last Updated:** March 2026  
**Maintainers:** [@p-society](https://github.com/p-society)