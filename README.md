# Raag

A terminal-first music player with local library management, playlists, and peer discovery over libp2p.

## Features

- Play local music from a configured directory
- Manage playlists and playback queue
- Bubble Tea TUI interface
- Peer discovery via mDNS (local network), DHT, or optional tracker
- **Automatic NAT traversal** - works through firewalls without manual configuration
- Configuration persists in `~/.config/raag/`

## Quick Start

```bash
# Build
go build -o bin/raag ./cmd/raag

# Local playback with TUI
./bin/raag --tui

# Networked mode (peers can connect automatically)
./bin/raag --offline=false --host 192.168.1.x
```

## Usage Modes

### Local Only

```bash
# Interactive TUI
./bin/raag --tui

# Headless (no TUI)
./bin/raag --offline --host 127.0.0.1
```

### Networked (Recommended for LAN)

```bash
# On each device, use your actual LAN IP
./bin/raag --offline=false --host 192.168.1.x
```

That's it! Raag will:
1. Discover peers via mDNS on your local network
2. Automatically handle NAT/firewall traversal
3. Connect peers without any manual configuration

## Finding Your LAN IP

```bash
# Linux
ip -4 addr show scope global | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -1

# macOS
ipconfig getifaddr en0
```

## Commands

### Playback
```bash
./bin/raag play <song>      # Play a song from library
./bin/raag pause             # Pause playback
./bin/raag resume           # Resume playback
./bin/raag stop             # Stop playback
./bin/raag next             # Next song
./bin/raag previous         # Previous song
./bin/raag volume [0-100]  # Set volume
./bin/raag queue            # Show queue
./bin/raag nowplaying       # Show current track
```

### Library
```bash
./bin/raag library list              # List all songs
./bin/raag library search <query>   # Search songs
./bin/raag library rescan            # Rescan music directory
./bin/raag library add <path>        # Add song
./bin/raag library remove <title>    # Remove song
```

### Playlist
```bash
./bin/raag playlist create <name>
./bin/raag playlist delete <name>
./bin/raag playlist list
./bin/raag playlist add <playlist> <song>
./bin/raag playlist songs <playlist>
./bin/raag playlist play <playlist>
```

### Peers
```bash
./bin/raag peers list              # Show connected peers
./bin/raag peers info              # Detailed peer info
./bin/raag peers connect <addr>    # Connect to peer
./bin/raag peers disconnect <id>    # Disconnect from peer
```

### Daemon
```bash
./bin/raag daemon                  # Run as background daemon
./bin/raag status                  # Check daemon status
```

### Configuration
```bash
./bin/raag config show            # Show current config
./bin/raag config set <key> <val> # Set config value
./bin/raag config reset            # Reset to defaults
```

## Configuration Keys

| Key | Default | Description |
|-----|---------|-------------|
| `musicdir` | `./music` | Music directory |
| `volume` | `50` | Volume level (0-100) |
| `host` | `127.0.0.1` | Bind address |
| `port` | `45678` | Listen port |
| `offline` | `true` | Enable/disable networking |
| `wifi` | `false` | WiFi mode |
| `tui` | `true` | Start TUI |
| `loglevel` | `info` | Log level |
| `rendezvous` | `raag-music-share` | Discovery namespace |

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--offline` | `true` | Set to `false` for networking |
| `--host` | `127.0.0.1` | Your LAN IP (for networked mode) |
| `--port` | `45678` | Listen port |
| `--wifi` | `false` | Enable WiFi mode |
| `--dht` | `true` | Enable DHT discovery |
| `--tracker` | - | Tracker URL |
| `--tui` | `false` | Start TUI |

## Network Discovery Options

### mDNS (Default for LAN)

Automatic discovery on local network - no configuration needed:
```bash
./bin/raag --offline=false --host 192.168.1.x
```

### DHT

Distributed hash table discovery:
```bash
./bin/raag --offline=false --host 192.168.1.x --dht=true
```

### Tracker

Centralized peer tracking:
```bash
# Start tracker
./bin/tracker

# Point raag to tracker
./bin/raag --offline=false --host 192.168.1.x --tracker http://<tracker-ip>:8080
```

## NAT Traversal

Raag automatically handles NAT and firewall traversal:

| Feature | Purpose |
|---------|---------|
| Hole Punching | Direct NAT traversal |
| UPnP | Auto port opening on router |
| Circuit Relay | Fallback via relay peers |
| AutoNAT | Network reachability detection |

**No manual firewall configuration required!**

## Architecture

```
CLI / TUI
   |
   v
bootstrapRuntime
   |
   +-- config (Viper + YAML)
   +-- library scan
   +-- player
   +-- playlist manager
   +-- network manager
            |
            +-- libp2p host
            +-- mDNS discovery
            +-- DHT discovery
            +-- tracker client
            +-- NAT traversal
```

## Files

Runtime files in `~/.config/raag/`:

- `config.yaml` - Application configuration
- `daemon.sock` - Unix socket for daemon
- `playlists.json` - Saved playlists
- `state.json` - Player state

## Requirements

- Go 1.26+
- Audio output support
- Supported formats: MP3, FLAC, WAV, OGG

## Troubleshooting

### No peers discovered

```bash
# Ensure you're in networked mode
./bin/raag --offline=false --host 192.168.1.x

# Check you're using actual LAN IP (not 127.0.0.1)
# Verify same network on all devices
# Try tracker mode as fallback
```

### Connection failed

NAT traversal handles most cases. If issues persist:
```bash
# Open port (optional)
sudo ufw allow 45678/tcp
```

### Logs

Check logs for connection status:
```bash
./bin/raag --offline=false --host 192.168.1.x
```

Look for:
- `mDNS discovered peer` - Peer found
- `Peer connected` - Connection established
- `Network: Online` - Successfully connected

## Development

```bash
go build ./...
go test ./...
```

## License

Apache License 2.0
