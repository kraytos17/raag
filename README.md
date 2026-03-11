<h1 align="center">
    Raag CLI
</h1>

<div align="center">
    Raag is a decentralized, P2P music streaming CLI application for local WiFi networks.
</div>

<div align="center">
    <h3>P-Society Handles</h3>
    <h3 align="center">
        <a href="https://dev-psoc.netlify.app/">Website</a>
        <span> | </span>
        <a href="https://discord.gg/UhmKJGMnan">Community Discord</a>
        <span> | </span>
        <a href="https://github.com/p-society/gc-server/blob/main/docs/CONTRIBUTING.md">Contribute</a>
    </h3>
</div>

----------------------------------------

## Table of Contents

1. [Features](#features)
2. [Architecture](#architecture)
3. [Installation](#installation)
4. [Quick Start](#quick-start)
5. [CLI Reference](#cli-reference)
6. [Configuration](#configuration)
7. [Network Modes](#network-modes)
8. [Network Setup](#network-setup)
9. [Troubleshooting](#troubleshooting)
10. [Contributing](#contributing)
11. [License](#license)

----------------------------------------

## Features

- **CLI Music Player**: Play, pause, queue, and manage your local music library
- **P2P Music Streaming**: Stream music from peers on your local network
- **Multiple Discovery Methods**:
  - **mDNS**: Automatic peer discovery on local network (requires `--wifi` mode)
  - **DHT**: Distributed Hash Table for peer finding
  - **Tracker**: Optional centralized tracker for reliable discovery
- **TUI Mode**: Interactive terminal user interface with Bubble Tea
- **Playlist Management**: Create, manage, and share playlists
- **Persistent Peers**: Automatically remember and reconnect to known peers

----------------------------------------

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                              Raag                                    │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌──────────┐    ┌──────────────┐    ┌─────────────────────────┐   │
│  │   CLI    │───▶│NetworkManager│───▶│     Discovery           │   │
│  │ Commands │    │              │    │  ┌─────┐ ┌─────┐ ┌────┐ │   │
│  └──────────┘    │  - libp2p   │    │  │ mDNS│ │ DHT │ │Trk │ │   │
│                  │  - Protocol  │    │  └─────┘ └─────┘ └────┘ │   │
│  ┌──────────┐    │  - Streaming│    └──────────┬──────────────┘   │
│  │   TUI    │───▶│              │───────────────▼                   │
│  │ (Bubble  │    └──────────────┘         ┌──────┐                 │
│  │  Tea)    │                            │ Peers│                 │
│  └──────────┘                            └──────┘                 │
│                                                                      │
│  ┌──────────┐    ┌──────────────┐    ┌─────────────────────────┐   │
│  │  Player  │    │   Library    │    │    Storage              │   │
│  │ (AVPlayer)│   │  (Metadata) │    │  (Playlists, Config)   │   │
│  └──────────┘    └──────────────┘    └─────────────────────────┘   │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Components

| Component | Description |
|-----------|-------------|
| **CLI Commands** | Playback control, peer management, library operations |
| **NetworkManager** | libp2p host, connection management, music streaming |
| **Discovery** | mDNS (local), DHT (distributed), Tracker (centralized) |
| **Player** | Audio playback using AVPlayer |
| **Library** | Music file scanning and metadata extraction |
| **Storage** | Playlist persistence, configuration, peer cache |

### Discovery Methods Comparison

| Method | Requires Network | Setup | Best For |
|--------|------------------|-------|----------|
| **mDNS** | Local WiFi | `--wifi` flag | Multi-laptop on same network |
| **DHT** | Internet | Bootstrap peers | Large-scale P2P |
| **Tracker** | Any | Run tracker binary | Reliable discovery |
| **None** | None | Default (offline) | Local playback only |

----------------------------------------

## Installation

### Prerequisites

- **Go 1.21 or later**
- **FFmpeg** (for audio playback)

### Build from Source

```bash
# Clone the repository
git clone https://github.com/p-society/raag.git
cd raag

# Build raag binary
go build -o raag ./cmd/raag

# Build tracker binary (optional, for centralized discovery)
go build -o tracker ./tracker/cmd/tracker
```

### Directory Structure

```
raag/
├── bin/
│   ├── raag           # Main binary
│   └── tracker        # Tracker binary (optional)
├── music/             # Your music files
├── cmd/raag/          # CLI entry point
├── internal/          # Core packages
│   ├── config/        # Configuration management
│   ├── discovery/     # P2P discovery (mDNS, DHT, Tracker)
│   ├── library/       # Music library
│   ├── network/       # libp2p networking
│   ├── player/       # Audio playback
│   ├── playlist/     # Playlist management
│   └── tui/          # Terminal UI
├── tracker/           # Centralized tracker server
└── README.md
```

----------------------------------------

## Quick Start

### Step 1: Prepare Your Music

```bash
# Create music directory and add your music files
mkdir -p music
cp your_songs.mp3 music/
```

### Step 2: Build

```bash
go build -o raag ./cmd/raag
```

### Step 3: Run with TUI (Interactive Mode)

```bash
./raag --tui
```

### Step 4: Or Run Daemon Mode

```bash
# Basic daemon (local playback only)
./raag daemon --no-tui

# With TUI
./raag daemon --tui
```

### Step 5: Networked Mode (P2P Streaming)

**Option A: Multiple Laptops on Same WiFi (mDNS)**

```bash
# Laptop 1
./raag daemon --wifi --host 0.0.0.0 --no-tui

# Laptop 2 (on same WiFi)
./raag daemon --wifi --host 0.0.0.0 --no-tui
```

**Option B: With Tracker**

```bash
# Terminal 1: Start tracker
./tracker

# Terminal 2: Start daemon with tracker
./raag daemon --wifi --host 0.0.0.0 --tracker http://localhost:8080 --no-tui

# Terminal 3: Another daemon
./raag daemon --wifi --host 0.0.0.0 --tracker http://localhost:8080 --no-tui
```

### Step 6: Control Playback

```bash
# Play a song
./raag play "song name"

# Check peers
./raag peers list

# Stream from peer
./raag play "peer:song name"
```

----------------------------------------

## CLI Reference

### Global Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--config` | - | `~/.config/raag/config.yaml` | Config file path |
| `--musicdir` | - | `./music` | Music directory |
| `--offline` | - | `true` | Run in offline mode |
| `--wifi` | - | `false` | Enable WiFi mode (mDNS + full transports) |
| `--tui` | - | `false` | Start in TUI mode |
| `--host` | - | `127.0.0.1` | Host to bind to (`0.0.0.0` for all interfaces) |
| `--port` | - | `0` | Listen port (0 = random) |
| `--fixed-port` | - | `0` | Fixed port (0 = random) |
| `--tracker` | - | `""` | Tracker URL for discovery |
| `--dht` | - | `true` | Enable DHT discovery |
| `--max-peers` | - | `100` | Maximum peers to maintain |
| `--bootstrap` | - | `[]` | Bootstrap peers (multiaddr) |
| `--rendezvous` | - | `raag-music-share` | Rendezvous string for DHT |
| `--pid` | - | `/raag/1.0.0` | Protocol ID |

### Playback Commands

#### play
```bash
# Play a song from library
./raag play "song name"

# Play from a specific peer
./raag play "QmPeerID:song name"
```

#### pause
```bash
./raag pause
```

#### resume
```bash
./raag resume
```

#### stop
```bash
./raag stop
```

#### next
```bash
./raag next
```

#### previous
```bash
./raag previous
```

#### queue
```bash
# View current queue
./raag queue
```

#### volume
```bash
# Set volume (0-100)
./raag volume 75

# Get current volume
./raag volume
```

#### seek
```bash
# Seek to position (seconds)
./raag seek 60
```

#### nowplaying
```bash
./raag nowplaying
```

### Daemon Command

```bash
# Basic daemon
./raag daemon --no-tui

# With TUI
./raag daemon --tui

# Networked mode
./raag daemon --wifi --host 0.0.0.0 --no-tui

# With tracker
./raag daemon --wifi --host 0.0.0.0 --tracker http://localhost:8080 --no-tui
```

### Peers Commands

#### peers list
```bash
# List all connected peers
./raag peers list
```

#### peers info
```bash
# Show self peer info
./raag peers info
```

#### peers connect
```bash
# Connect to a peer via multiaddr
./raag peers connect /ip4/192.168.1.100/tcp/4001/p2p/QmPeerID
```

#### peers disconnect
```bash
# Disconnect from a peer
./raag peers disconnect QmPeerID
```

#### peers tracker
```bash
# Set tracker URL
./raag peers tracker http://localhost:8080
```

#### peers bootstrap
```bash
# Add bootstrap peer
./raag peers bootstrap /ip4/192.168.1.100/tcp/4001/p2p/QmPeerID
```

### Library Commands

#### library list
```bash
# List all songs
./raag library list
```

#### library search
```bash
# Search for songs
./raag library search "query"
```

#### library rescan
```bash
# Rescan music directory
./raag library rescan
```

#### library add
```bash
# Add song to library
./raag library add /path/to/song.mp3
```

#### library remove
```bash
# Remove song from library
./raag library remove "song name"
```

### Playlist Commands

#### playlist create
```bash
./raag playlist create myplaylist
```

#### playlist delete
```bash
./raag playlist delete myplaylist
```

#### playlist list
```bash
# List all playlists
./raag playlist list
```

#### playlist add
```bash
# Add song to playlist
./raag playlist add myplaylist "song name"
```

#### playlist remove
```bash
# Remove song from playlist
./raag playlist remove myplaylist 0
```

#### playlist songs
```bash
# List songs in playlist
./raag playlist songs myplaylist
```

#### playlist play
```bash
# Play playlist
./raag playlist play myplaylist
```

### Config Commands

#### config show
```bash
# Show current config
./raag config show
```

#### config set
```bash
# Set config value
./raag config set discovery.tracker_url http://localhost:8080
./raag config set playback.volume 75
./raag config set network.host 0.0.0.0
```

#### config reset
```bash
# Reset config to defaults
./raag config reset
```

### Utility Commands

#### share
```bash
# Generate shareable peer info
./raag share
```

#### status
```bash
# Show daemon status
./raag status
```

----------------------------------------

## Configuration

### Config File Location

Default: `~/.config/raag/config.yaml`

### Sample Configuration

```yaml
network:
  host: 127.0.0.1
  port: 0
  fixed_port: 0
  rendezvous: raag-music-share
  protocol_id: /raag/1.0.0

discovery:
  tracker_url: ""
  dht_enabled: true
  max_peers: 100
  bootstrap_peers: []

playback:
  music_dir: ./music
  volume: 50

ui:
  tui_enabled: true
  wifi_mode: false
  offline: true
  log_level: info
```

### CLI Flag Precedence

Viper uses this priority order (highest to lowest):

1. **CLI flags** (e.g., `--wifi`, `--host 0.0.0.0`)
2. **Environment variables** (e.g., `RAAG_WIFI=true`)
3. **Config file** (e.g., `config.yaml`)
4. **Default values** (hardcoded in code)

**Example:**
```bash
# CLI flag overrides config file
./raag daemon --wifi --host 0.0.0.0  # Uses wifi=true even if config has offline=true
```

### Persisting CLI Flags

Some flags are automatically saved to config:

| Flag | Persists? | Notes |
|------|-----------|-------|
| `--tracker` | Yes | Saved to `discovery.tracker_url` |
| `--wifi` | No | Runtime override only |
| `--host` | No | Runtime override only |
| `--musicdir` | No | Runtime override only |

----------------------------------------

## Network Modes

### Offline Mode (Default)

```bash
./raag daemon --no-tui
```

- Only local playback
- Limited libp2p transports
- No peer discovery
- Config: `offline: true`

### WiFi Mode

```bash
./raag daemon --wifi --host 0.0.0.0 --no-tui
```

- Full libp2p transports
- mDNS peer discovery (local network)
- DHT enabled
- Config: `wifi_mode: true`

### Tracker Mode

```bash
# Start tracker
./tracker

# Connect daemons to tracker
./raag daemon --wifi --host 0.0.0.0 --tracker http://localhost:8080 --no-tui
```

- Centralized peer registry
- More reliable than pure mDNS
- Works across networks

### Mode Comparison

| Feature | Offline | WiFi | Tracker |
|---------|---------|------|---------|
| Local Playback | ✅ | ✅ | ✅ |
| mDNS Discovery | ❌ | ✅ | ✅ |
| DHT Discovery | ❌ | ✅ | ✅ |
| Tracker Discovery | ❌ | ✅ | ✅ |
| P2P Streaming | ❌ | ✅ | ✅ |

----------------------------------------

## Network Setup

### Setting Up mDNS (Multiple Laptops)

mDNS enables automatic peer discovery on local networks without any central server.

**Requirements:**
- 2+ laptops on the same WiFi network
- Firewall allows mDNS (port 5353)

**Steps:**

1. **Build the binary**
   ```bash
   go build -o raag ./cmd/raag
   ```

2. **Copy to all laptops**
   ```bash
   scp raag laptop2:/path/to/raag
   ```

3. **Run on each laptop**
   ```bash
   # Laptop 1
   ./raag daemon --wifi --host 0.0.0.0 --no-tui

   # Laptop 2
   ./raag daemon --wifi --host 0.0.0.0 --no-tui

   # Laptop 3
   ./raag daemon --wifi --host 0.0.0.0 --no-tui
   ```

4. **Verify**
   ```bash
   # On any laptop
   ./raag peers list
   ```

You should see peers from other laptops within ~10 seconds.

### Setting Up Tracker

The tracker provides centralized peer discovery as a fallback.

**Steps:**

1. **Build tracker**
   ```bash
   go build -o tracker ./tracker/cmd/tracker
   ```

2. **Start tracker** (one machine, keeps running)
   ```bash
   ./tracker
   ```

3. **Connect daemons**
   ```bash
   # Get tracker's IP first
   hostname -I

   # On all machines, use tracker's IP
   ./raag daemon --wifi --host 0.0.0.0 --tracker http://192.168.1.X:8080 --no-tui
   ```

4. **Verify**
   ```bash
   ./raag peers list
   ```

### Setting Up DHT with Bootstrap Peers

For DHT to work, you need at least one bootstrap peer.

**Steps:**

1. **Get a peer's multiaddr**
   ```bash
   ./raag peers info
   # Output: /ip4/192.168.1.100/tcp/4001/p2p/QmPeerID
   ```

2. **Use as bootstrap**
   ```bash
   ./raag daemon --bootstrap /ip4/192.168.1.100/tcp/4001/p2p/QmPeerID --no-tui
   ```

3. **Or add to config**
   ```yaml
   discovery:
     bootstrap_peers:
       - /ip4/192.168.1.100/tcp/4001/p2p/QmPeerID
   ```

----------------------------------------

## Troubleshooting

### No Peers Discovered

**Symptoms:** `./raag peers list` shows 0 peers

**Solutions:**

1. **Check network mode**
   ```bash
   # Should show wifi=true, host=0.0.0.0
   ./raag status
   ```

2. **Restart with correct flags**
   ```bash
   ./raag daemon --wifi --host 0.0.0.0 --no-tui
   ```

3. **Try tracker mode**
   ```bash
   ./tracker  # In one terminal
   ./raag daemon --wifi --host 0.0.0.0 --tracker http://localhost:8080 --no-tui
   ```

### Port Already in Use

**Symptoms:** `Error: port already in use`

**Solutions:**

1. **Use random port**
   ```bash
   ./raag daemon --port 0 --no-tui
   ```

2. **Use fixed port**
   ```bash
   ./raag daemon --fixed-port 4001 --no-tui
   ```

### mDNS Not Working

**Symptoms:** Peers not discovered on local network

**Solutions:**

1. **Verify --wifi flag**
   ```bash
   # Must use --wifi, not just --offline=false
   ./raag daemon --wifi --host 0.0.0.0 --no-tui
   ```

2. **Check firewall**
   ```bash
   # Linux - allow mDNS
   sudo ufw allow 5353/udp
   ```

3. **Same network?** mDNS doesn't work across different networks or VPN

4. **Try tracker** as fallback

### Config Not Saving

**Symptoms:** CLI flags work but don't persist after restart

**Solutions:**

1. **Check config file location**
   ```bash
   cat ~/.config/raag/config.yaml
   ```

2. **Check file permissions**
   ```bash
   ls -la ~/.config/raag/
   ```

3. **Manually set config**
   ```bash
   ./raag config set discovery.tracker_url http://localhost:8080
   ```

### Audio Not Playing

**Symptoms:** Songs show in library but don't play

**Solutions:**

1. **Check FFmpeg installation**
   ```bash
   ffmpeg -version
   ```

2. **Check file format**
   ```bash
   # Supported: MP3, FLAC, WAV, OGG, M4A
   file music/song.mp3
   ```

3. **Check volume**
   ```bash
   ./raag volume 100
   ```

### DHT Routing Table Empty

**Symptoms:** `routing_table_size=0` in logs

**Solutions:**

1. **This is normal initially** - DHT populates over time
2. **Add bootstrap peers** to speed up
3. **Use tracker** for faster initial discovery

### Network Peers Shows Negative

**Symptoms:** `network_peers=-1`

**Solutions:**

1. **This is a display bug**, actual peers are fine
2. **Check with:**
   ```bash
   ./raag peers list
   ```

----------------------------------------

## Contributing

Contributions are welcome! Please see our [Contributing Guide](https://github.com/p-society/gc-server/blob/main/docs/CONTRIBUTING.md).

### Development Setup

```bash
# Clone and setup
git clone https://github.com/p-society/raag.git
cd raag

# Install dependencies
go mod download

# Run tests
go test ./...

# Run with hot reload (optional)
```

### Code Structure

```
internal/
├── config/       # Viper configuration
├── discovery/   # P2P discovery (mDNS, DHT, Tracker)
├── library/      # Music file scanning
├── logger/       # Logging (charmbracelet/log)
├── metadata/     # Audio metadata
├── network/      # libp2p networking
├── player/       # Audio playback
├── playlist/     # Playlist management
├── socket/       # Unix socket IPC
├── storage/      # Data persistence
└── tui/         # Terminal UI
```

----------------------------------------

## License

Raag is licensed under the [Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0).

---

<div align="center">
    <br/>
    <img src='https://github.com/p-society/raag/assets/119437069/43759dc5-7386-4bc9-9598-bb95ad90ad8f' width='70' height='70'>
    <img src='https://github.com/p-society/raag/assets/119437069/108a1ce3-107d-4a43-ad63-d34a007beab3' width='70' height='70' style='border-radius: 10px;'>
    <img src='https://github.com/p-society/raag/assets/119437069/c6e0493e-07b5-4af1-a35c-04c3012247e1' width='70' height='70' style='border-radius: 10px;'>
    <img src='https://github.com/p-society/raag/assets/119437069/b69a92ce-e818-4fac-b44a-5d0f58f909a5' width='70' height='70' style='border-radius: 10px;'>
    <br/>
</div>

### Current Contributors

<a href="https://github.com/p-society/raag/graphs/contributors">
    <img src="https://contributors-img.web.app/image?repo=p-society/raag" />
</a>

Made with [contributors-img](https://contributors-img.web.app).

## Subscribe to Updates

Join our [Discord Server](https://discord.gg/UhmKJGMnan) and subscribe to this repository to get updates about Raag.
