# Raag

Raag is a terminal-first music player with local library management, playlists, and peer discovery over libp2p.
It can run as a local app, or as a long-lived daemon that exposes status and peer information over a Unix socket.

## What It Does

- plays local music from a configured music directory
- manages playlists and a simple queue
- starts a Bubble Tea TUI when requested or when `ui.tui_enabled` is set
- discovers peers with mDNS, DHT, and an optional tracker
- persists config in `~/.config/raag/config.yaml`
- persists discovered peers in `~/.config/raag/peers.json`

## Current Shape

Raag currently has two practical workflows:

1. `raag` or `raag --tui` for a local in-process session
2. `raag daemon` for a background process plus daemon-backed queries like `status`, `peers list`, and `peers info`

The daemon workflow is the most stable path for networking and peer inspection.

## Recommended Usage

Use one of these first:

```bash
# local interactive session
./bin/raag --tui

# offline daemon
./bin/raag daemon --offline --host 127.0.0.1 --no-tui

# LAN daemon
./bin/raag daemon --wifi --host 0.0.0.0 --no-tui
```

Then inspect with:

```bash
./bin/raag status
./bin/raag peers list
./bin/raag peers info
```

## Requirements

- Go `1.26`
- a terminal with audio output support
- local audio files in one of the currently supported formats:
  - `.mp3`
  - `.flac`
  - `.wav`
  - `.ogg`
  - `.ogv`

## Build

```bash
git clone https://github.com/p-society/raag.git
cd raag

go build -o bin/raag ./cmd/raag
go build -o bin/tracker ./tracker/cmd/tracker
```

## Quick Start

### Offline only

Headless daemon:

```bash
./bin/raag daemon --offline --host 127.0.0.1 --no-tui
```

Interactive local run:

```bash
./bin/raag --tui
```

Useful local checks:

```bash
./bin/raag status
./bin/raag config show
```

### Wi-Fi / LAN discovery

On each laptop on the same LAN:

```bash
./bin/raag daemon --wifi --host 0.0.0.0 --no-tui
```

Then inspect peers from any node:

```bash
./bin/raag peers list
./bin/raag peers info
```

### Tracker-backed discovery

Start the tracker on one machine:

```bash
./bin/tracker
```

Then point daemons at it:

```bash
./bin/raag daemon --wifi --host 0.0.0.0 --tracker http://<tracker-ip>:8080 --no-tui
```

## Architecture

```text
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
            +-- peer persistence
```

## Installation Layout

Generated runtime files live under `~/.config/raag/`:

- `config.yaml` - persisted application config
- `daemon.sock` - Unix socket for daemon-backed queries
- `peers.json` - persisted peer addresses
- `playlists.json` - saved playlists
- `state.json` - saved player state metadata

## Commands

### Root

```bash
./bin/raag [flags]
./bin/raag [command]
```

Available commands:

- `config`
- `daemon`
- `library`
- `next`
- `nowplaying`
- `pause`
- `peers`
- `play`
- `playlist`
- `previous`
- `queue`
- `resume`
- `seek`
- `share`
- `status`
- `stop`
- `volume`

### Global flags

These are available on the root command and inherited by subcommands.

| Flag | Default | Meaning |
|---|---:|---|
| `--config` | config under `$HOME/.config/raag/config.yaml` | explicit config file path |
| `--musicdir` | `./music` | music library directory |
| `--offline` | `true` | run without peer discovery |
| `--wifi` | `false` | enable LAN/network mode |
| `--tui` | `false` | request TUI startup |
| `--tracker` | empty | tracker URL |
| `--fixed-port` | `0` | stable libp2p listen port |
| `--dht` | `true` | enable DHT discovery |
| `--max-peers` | `100` | peer cap for discovery state |
| `--bootstrap` | empty | DHT bootstrap peer multiaddrs |
| `--host` | `127.0.0.1` | bind host |
| `--port` | `0` | random port when unset |
| `--rendezvous` | `raag-music-share` | discovery namespace |
| `--pid` | `/raag/1.0.0` | libp2p protocol id |

### Daemon

```bash
./bin/raag daemon [flags]
```

Daemon-specific flags:

- `--tracker`
- `--tui`
- `--no-tui`

Example:

```bash
./bin/raag daemon --wifi --host 0.0.0.0 --fixed-port 4001 --no-tui
```

### Peers

```bash
./bin/raag peers list
./bin/raag peers info
./bin/raag peers connect <multiaddr>
./bin/raag peers disconnect <peer-id>
./bin/raag peers tracker <url>
./bin/raag peers bootstrap <multiaddr>
```

Notes:

- `peers list` shows currently connected peers
- `peers info` shows self info, connected peers, and known peers
- `peers tracker` updates tracker usage in the running app session
- `peers bootstrap` adds one bootstrap peer to discovery

### Config

```bash
./bin/raag config show
./bin/raag config set <key> <value>
./bin/raag config reset
```

Supported `config set` keys:

- `musicdir`
- `volume`
- `tui`
- `wifi`
- `offline`
- `rendezvous`
- `host`
- `port`
- `loglevel`

Examples:

```bash
./bin/raag config set volume 70
./bin/raag config set tui true
./bin/raag config set host 0.0.0.0
./bin/raag config set loglevel debug
```

### Library

```bash
./bin/raag library list
./bin/raag library search <query>
./bin/raag library rescan
./bin/raag library add <path>
./bin/raag library remove <title>
```

### Playlist

```bash
./bin/raag playlist create <name>
./bin/raag playlist delete <name>
./bin/raag playlist list
./bin/raag playlist add <playlist> <song>
./bin/raag playlist remove <playlist> <index>
./bin/raag playlist songs <playlist>
./bin/raag playlist play <playlist>
```

### Playback commands

```bash
./bin/raag play <song>
./bin/raag pause
./bin/raag resume
./bin/raag stop
./bin/raag next
./bin/raag previous
./bin/raag queue
./bin/raag volume [0-100]
./bin/raag seek <seconds>
./bin/raag nowplaying
```

### Share

```bash
./bin/raag share <peer-multiaddr> <song-title>
```

### Status

```bash
./bin/raag status
```

If the daemon socket is available, `status` reads from the daemon. Otherwise it initializes a local runtime and reports standalone status.

## Configuration

Default config values:

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

### Precedence

Effective precedence is:

1. CLI flags
2. config file values
3. built-in defaults

Special runtime behavior:

- passing `--wifi` forces `offline=false` for that run
- `--tracker` on `raag daemon` is saved back into `config.yaml`
- discovered peer addresses are saved into `discovery.bootstrap_peers`

## Network Modes

### Offline mode

Use this when you only want local playback and no discovery.

```bash
./bin/raag daemon --offline --host 127.0.0.1 --no-tui
```

### Wi-Fi mode

Use this for real LAN addresses and mDNS discovery.

```bash
./bin/raag daemon --wifi --host 0.0.0.0 --no-tui
```

### Wi-Fi mode without DHT

Useful when you only want mDNS and optional tracker discovery.

```bash
./bin/raag daemon --wifi --host 0.0.0.0 --dht=false --no-tui
```

### Custom rendezvous namespace

All nodes must use the same rendezvous string to discover one another through DHT/mDNS.

```bash
./bin/raag daemon --wifi --host 0.0.0.0 --rendezvous raag-team-a --no-tui
```

### Bootstrap peers

You can seed DHT startup with explicit peers:

```bash
./bin/raag daemon \
  --wifi \
  --host 0.0.0.0 \
  --bootstrap /ip4/192.168.0.10/tcp/4001/p2p/<peer-id> \
  --bootstrap /ip4/192.168.0.11/tcp/4001/p2p/<peer-id> \
  --no-tui
```

## mDNS Testing Across Multiple Laptops

For pure mDNS, use multiple physical machines on the same LAN.
Loopback-only local testing is not representative.

On every laptop:

```bash
./bin/raag daemon --wifi --host 0.0.0.0 --no-tui
```

Then verify on any laptop:

```bash
./bin/raag peers list
./bin/raag peers info
```

Expected behavior:

- self address should show a real LAN address, not only `127.0.0.1`
- `network_peers` should reflect current remote libp2p peers
- with 4 terminals total, fully connected nodes should report 3 remote peers

## Logs And Counters

A typical DHT line looks like this:

```text
DHT status: routing_table_size=3 connected_peers=3 network_peers=3
```

Meaning:

- `routing_table_size` - peers currently present in the DHT routing table
- `connected_peers` - peers Raag currently tracks as connected
- `network_peers` - live remote peers from libp2p's network view

These numbers can differ briefly because DHT memory, app bookkeeping, and live transport connections are different layers.

## Operational Notes

- `GetMultiaddr()` now reports actual bound libp2p addresses instead of config placeholders
- `ui.tui_enabled` is honored when no explicit `--tui` / `--no-tui` override is provided
- `ui.log_level` is applied at startup
- `discovery.dht_enabled`, `network.rendezvous`, and `discovery.bootstrap_peers` are now active runtime settings

## Known Limitations

- the daemon-backed workflow is the most reliable path today for networking, peer inspection, and status queries
- some direct playback and library subcommands still assume an initialized in-process runtime, so `raag`, `raag --tui`, or `raag daemon` should be started first when validating end-to-end behavior
- peer discovery state and live libp2p connections can diverge briefly, so `routing_table_size`, `connected_peers`, and `network_peers` should not always be expected to match exactly at every instant

## Troubleshooting

### No peers discovered

- for offline mode, this is expected
- for LAN discovery, use `--wifi --host 0.0.0.0`
- verify all nodes share the same `--rendezvous`
- try the tracker if mDNS is flaky

### `network_peers` lower than expected

- one remote peer may be discovered but not currently connected
- compare `peers info` output across all nodes
- manually test with `peers connect <multiaddr>`
- make sure the displayed value is taken from the latest build

### mDNS not working

- test across real laptops on the same network
- avoid loopback-only assumptions
- check firewall rules for multicast/UDP 5353

### Port conflicts

Use either a random port:

```bash
./bin/raag daemon --port 0 --no-tui
```

Or a fixed one:

```bash
./bin/raag daemon --fixed-port 4001 --no-tui
```

### Playback/library commands

Raag's most reliable workflows today are:

- `raag` / `raag --tui` for an in-process session
- `raag daemon` for background networking and daemon-backed inspection commands

If you are debugging command behavior, start there first.

## Development

Build and validate:

```bash
go build ./...
go test ./...
staticcheck ./...
```

## Contributing

Contributions are welcome. Please use the project issue tracker and PR flow in the repository.

## License

Raag is licensed under the Apache License 2.0.
