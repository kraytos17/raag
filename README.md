# Raag

[![Go Version](https://img.shields.io/badge/Go-1.26-blue)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue)](https://www.apache.org/licenses/LICENSE-2.0)

A production-ready, terminal-first **LAN-only P2P music player**. Raag streams music
directly between peers on your local network over libp2p — no internet, no tracker,
no cloud. Play your local library, browse what your friends share on the same Wi‑Fi or
hotspot, and stream it to your ears with an interactive TUI, a full CLI, and on-demand
ffmpeg transcoding.

## Table of Contents

- [Features](#features)
- [Quick Start](#quick-start)
- [Prerequisites](#prerequisites)
- [Getting Started](#getting-started)
- [Architecture](#architecture)
- [P2P & Discovery](#p2p--discovery)
- [Security](#security)
- [Configuration](#configuration)
- [Commands](#commands)
- [TUI Reference](#tui-reference)
- [Monitoring & Diagnostics](#monitoring--diagnostics)
- [Troubleshooting](#troubleshooting)
- [Development](#development)
- [Contributing](#contributing)
- [License](#license)

## Features

| Category | Feature | Description |
|----------|---------|-------------|
| **Playback** | Local playback | Play from a local directory (MP3, FLAC, WAV, OGG/Vorbis) |
| | Controls | Play, pause, stop, seek, next/prev, volume |
| | Queue | Add/remove/clear, shuffle, repeat (off / all / one) |
| | DSP | 3-band equalizer, loudness normalization, crossfade |
| | Gapless | Preloaded next-track handoff (local sources) |
| **P2P Streaming** | LAN-only | Private-IP-only libp2p; never dials the public internet |
| | Discovery | mDNS **and** UDP broadcast (Mini-Militia style) |
| | Transcoding | On-demand ffmpeg when codecs differ between peers |
| | Peer health | Active ping latency, bandwidth, and score tracking |
| | Banning | Per-peer ban/unban; connection gater enforced |
| **Operation** | Daemon | `raagd` background process with pidfile + unix-socket IPC |
| | TUI | 4-panel terminal interface (library, queue, peers, playlists) |
| | CLI | Full command surface for playback, library, queue, playlists, peers |
| | Persistence | BadgerDB-backed library, playlists, peers, settings |
| | Watch | fsnotify auto-rescan of the music directory |

## Quick Start

### Build

```bash
git clone https://github.com/p-society/raag.git
cd raag
make build              # produces ./bin/raag and ./bin/raagd
```

### First run

```bash
./bin/raagd --music-path /path/to/your/music   # first-time setup if no config
./bin/raag tui                                 # launch the interface
```

`raagd` auto-scans the library on start (disable with `--no-scan`) and watches the
directory for changes. Once running, `raag tui` shows your library; `/` searches your
library **and every connected peer's**.

## Prerequisites

| Requirement | Minimum | Recommended |
|-------------|---------|-------------|
| Go | 1.26 | 1.26+ |
| CPU | x86-64 | x86-64 / ARM64 |
| Memory | 256 MB | 512 MB+ |
| Audio | ALSA on Linux | PulseAudio/PipeWire compatible device |

### OS packages

```bash
# Ubuntu/Debian
sudo apt-get install -y libasound2-dev   # ALSA for audio playback
sudo apt-get install -y ffmpeg           # optional: cross-codec transcoding

# macOS
brew install ffmpeg                      # optional; no ALSA needed
```

### Network requirements

Raag is designed for **one flat LAN/subnet** — typically a home Wi‑Fi or a phone
hotspot. Default ports:

| Port | Protocol | Purpose |
|------|----------|---------|
| 7844 | TCP + QUIC | libp2p listen (peer connections, sync, streaming) |
| 7845 | — | stream port (reserved) |
| 7846 | UDP | broadcast discovery beacon |
| 5353 | UDP | mDNS discovery |
| 9200 | TCP | optional Prometheus metrics |

**Firewalls** must allow inbound `7844/tcp`, `7844/udp`, `7846/udp`, and `5353/udp`.
Guest networks with AP/client isolation block mDNS *and* broadcast — a private hotspot
or home router does not.

## Getting Started

### 1. Start the daemon

```bash
# Foreground (Ctrl-C stops it)
make daemon-start
# or
./bin/raagd

# Background (detached, pidfile + log in the data dir)
./bin/raag daemon start
./bin/raag daemon status      # exit 0 = running, 1 = stopped
./bin/raag daemon stop
```

Config and data live under `~/.config/raag/` (config.toml) and
`~/.local/share/raag/` (BadgerDB, socket, pidfile, log).

### 2. Scan music

```bash
./bin/raag lib scan                # full scan
./bin/raag lib scan --incremental  # only new/modified files
./bin/raag lib list                # id\t title - artist
./bin/raag lib list --limit 20
```

### 3. Play

```bash
./bin/raag play "never gonna give"     # search → top hit (incl. peer libraries)
./bin/raag play -i <64-hex-track-id>   # exact track ID
./bin/raag pause | resume | stop
./bin/raag next | prev
./bin/raag seek 45                     # seconds
./bin/raag volume                      # print current volume
./bin/raag volume 65                   # set 0–100
./bin/raag status
```

### 4. Share & stream with peers

Run `raagd` on each machine on the **same Wi‑Fi/hotspot**. Both mDNS and UDP
broadcast discovery are on by default, so peers appear automatically:

```bash
./bin/raag peers list           # connected/discovered peers with scores
./bin/raag search --peers "song"   # search local + all peers
./bin/raag play "song"          # streams from the peer that has it
```

## Architecture

```
┌────────────────────────────────────────────────────────────┐
│                         raag (CLI/TUI)                     │
│   play · pause · queue · lib · playlist · peers · ...      │
└──────────────────────────────┬─────────────────────────────┘
                               │ unix socket IPC (length-prefixed protobuf)
┌──────────────────────────────▼─────────────────────────────┐
│                      raagd (daemon)                        │
│  ┌──────────┐ ┌───────────┐ ┌────────────┐ ┌────────────┐  │
│  │ Playback │ │  Queue    │ │  Library   │ │ Playlists  │  │
│  │  (beep)  │ │  / FSM    │ │  (Badger)  │ │  (Badger)  │  │
│  └────┬─────┘ └───────────┘ └────────────┘ └────────────┘  │
│       │ events (event bus → IPC events)                    │
│  ┌────▼──────────────────────────────────────────────────┐ │
│  │                  P2P node (libp2p)                    │ │
│  │  mDNS + UDP broadcast discovery → connect-with-retry  │ │
│  │  sync protocol (manifests, search, track detail)      │ │
│  │  stream protocol (chunked, rate-limited, transcodable)│ │
│  │  PeerGater: private-IP only · ban · max peers         │ │
│  └───────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────┘
```

### Data flow

1. **Local playback** — track resolved from the library, decoded by `beep`, sent to
   the audio device. DSP chain applies EQ / normalization / crossfade.
2. **Peer discovery** — mDNS announces `raag-local`; UDP broadcast announces
   `pb.ConnectedPeer` to `255.255.255.255:7846` every 2s. Both feed the same
   connect-with-retry pipeline; `PeerGater` rejects any non-private address.
3. **Sync** — on connect, peers exchange a library manifest, capabilities, and answer
   remote searches.
4. **Streaming** — a `ChunkRequest` stream fetches byte ranges; if the requesting
   peer's codec differs, the serving side transcodes with ffmpeg first.

## P2P & Discovery

Raag is **deliberately LAN-only**. There is no DHT, no public bootstrap, no relay, and
no tracker — those are internet-scale features that conflict with the LAN goal.

- **mDNS** (`raag-local`, multicast `224.0.0.251:5353`) — fast local discovery, but a
  few APs/Wi‑Fi drivers filter multicast.
- **UDP broadcast** (`255.255.255.255:7846`) — the fallback that works on most private
  hotspots, mirroring how LAN games (e.g. Mini Militia) announce rooms: every node
  broadcasts a tiny protobuf beacon; listeners connect directly.
- **`p2p.lan_only`** (default `true`) — the connection gater accepts only RFC1918,
  ULA, link-local, and loopback addresses. Setting it to `false` is unsupported for
  public operation.

### Peer controls

```bash
./bin/raag peers list           # id, connected dot, addrs, live score
./bin/raag network              # peer id, listen addrs, connected + discovered
./bin/raag peers ban <peer-id>  # block a peer (enforced at dial/accept/secure)
./bin/raag peers unban <peer-id>
./bin/raag health               # daemon health check
```

## Security

- **LAN confinement** — `PeerGater` + `ForceReachabilityPrivate` keep all connections
  on private networks.
- **Transport encryption** — libp2p's Noise/TLS secures every peer connection.
- **Admission control** — a peer becomes admitted only after completing manifest
  exchange; chunk / track-detail / remote-search requests from unadmitted peers are
  rejected.
- **Path confinement (`os.Root`)** — the scanner and the stream handler open files
  through `os.Root` scoped to the configured library directories, so a symlink or
  `..` component can never escape the library. Transcodes additionally require the
  track path to be lexically inside a configured root before ffmpeg is invoked.
- **Bans** — banned peers/IPs are rejected at dial, accept, and handshake.
- **Rate limiting** — per-peer request rate and a global upload bandwidth cap.
- **Key hygiene** — libp2p identity key stored with `0600`/`0700` permissions;
  config, pidfile, and daemon log written `0600`; config/data dirs `0700`.

> The older README described internet-wide DHT/IPFS-bootstrap networking, mutual-auth
> shared secrets, a tracker binary, and port 45678. **Those do not exist in this
> codebase.** Raag is a LAN-only peer-to-peer music player; see [P2P & Discovery]
> above for what it actually does.

## Configuration

Configuration is a **TOML** file at `~/.config/raag/config.toml`, generated by
`raagd --setup`. There is no `config.yaml`.

```toml
[library]
paths = ["~/Music"]
watch = true
scan_on_start = true

[daemon]
socket_path = "~/.local/share/raag/raag.sock"
pid_file = "~/.local/share/raag/raagd.pid"
log_level = "info"

[p2p]
enabled = true
lan_only = true
listen_addrs = ["/ip4/0.0.0.0/tcp/7844", "/ip4/0.0.0.0/udp/7844/quic-v1"]
mdns_service_tag = "raag-local"
broadcast_enabled = true
broadcast_port = 7846
max_peers = 20
announce_library = true
per_peer_rate_limit = 10
upload_bandwidth = 0
cb_failure_threshold = 5
cb_cooldown = "30s"

[transcoder]
ffmpeg_path = "ffmpeg"
stream_codec = "opus"
stream_bitrate = "128k"

[privacy]
share_library_manifest = true
share_play_history = false
announce_on_discovery = true

[playback]
volume = 80
output_device = "default"
buffer_size = 4096
crossfade_ms = 0

[metrics]
enabled = false
port = 9200
path = "/metrics"
```

### `raagd` flags

| Flag | Default | Description |
|------|---------|-------------|
| `--setup` | false | Run the interactive first-time setup wizard |
| `--music-path` | — | Music directory (skips setup prompt) |
| `--socket` | config | Override IPC socket path |
| `--data-dir` | config | Override data directory |
| `--no-scan` | false | Skip library scan on startup |
| `--p2p` | true | Enable P2P networking (`--p2p=false` to disable) |

## Commands

### Playback

```bash
./bin/raag play "query"       # search → play top hit (local + peers)
./bin/raag play --id <id>     # play exact track ID
./bin/raag pause | resume | stop
./bin/raag next | prev
./bin/raag seek <seconds>
./bin/raag volume             # show
./bin/raag volume <0-100>     # set
./bin/raag eq                 # show EQ state
./bin/raag eq --on --bass 4 --mid -1 --treble 2
./bin/raag eq --off
./bin/raag status             # full playback status
```

### Library

```bash
./bin/raag search <query>            # local only
./bin/raag search --peers <query>    # local + all peers
./bin/raag lib scan                  # full scan
./bin/raag lib scan --incremental    # only new/modified
./bin/raag lib list [--limit N]      # id\t title - artist
./bin/raag track get <id>            # full metadata
./bin/raag track get --path <path>   # by file path
```

### Queue

```bash
./bin/raag queue add <track-id>
./bin/raag queue remove <position>   # 0-based
./bin/raag queue clear
./bin/raag queue shuffle on | off
./bin/raag queue repeat all | one | none
./bin/raag queue mode                # show shuffle/repeat state
```

### Playlists

```bash
./bin/raag playlist create <name>
./bin/raag playlist list
./bin/raag playlist show <playlist-id>
./bin/raag playlist add <playlist-id> <track-id>
```

### Peers & network

```bash
./bin/raag network
./bin/raag peers ban <peer-id>
./bin/raag peers unban <peer-id>
./bin/raag health
```

### Daemon

```bash
./bin/raag daemon start      # detach + wait for socket
./bin/raag daemon status     # exit 0 running / 1 stopped
./bin/raag daemon stop
```

### Diagnostics

```bash
./bin/raag debug peers     # per-peer latency/bandwidth/score
./bin/raag debug streams   # active stream pool
./bin/raag debug index     # search index stats
./bin/raag debug db        # BadgerDB size
```

### Global flags

| Flag | Description |
|------|-------------|
| `--socket` | IPC socket path (default `~/.local/share/raag/raag.sock`) |

## TUI Reference

Launch with `./bin/raag tui`. Four panels, cycled with `Tab`:
**Library · Queue · Peers · Playlists**.

| Key | Action |
|-----|--------|
| `/` | Search (local + all peers) into the Library panel |
| `enter` | Play selected track / load playlist |
| `d` | Remove from queue |
| `␣` | Play / pause |
| `n` / `p` | Next / previous |
| `x` | Stop |
| `f` / `b` | Seek forward / back 10s |
| `+` / `-` | Volume up / down |
| `s` | Toggle shuffle |
| `R` | Cycle repeat (off → all → one) |
| `r` | Refresh all panels |
| `L` | Toggle lyrics overlay |
| `e` | Toggle EQ panel |
| `E` | Cycle EQ presets |
| `?` | Help |
| `q` / `ctrl+c` | Quit |

## Monitoring & Diagnostics

- **Logs** — `raagd` writes structured `slog` output to
  `~/.local/share/raag/raagd.log` (log level via `daemon.log_level`).
- **Metrics** — optional Prometheus endpoint (`metrics.enabled = true`, port 9200,
  path `/metrics`).
- **`raag debug *`** — stream pool, search index, peers, and database-size snapshots
  over IPC.
- **Health** — `raag health` checks the daemon via IPC; `raag daemon status` checks
  the pidfile.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| No peers discovered | mDNS multicast filtered by AP | Port `7846/udp` open? Try a private hotspot; check `raag network` |
| `raagd` won't start | missing config / music path | `raagd --setup` or `--music-path /path` |
| Playback silent | wrong ALSA device | `aplay -l`; set `playback.output_device` |
| Peer connects then drops | firewalled `7844` | allow `7844/tcp` + `7844/udp` inbound |
| Track won't stream | codec not locally decodable and ffmpeg missing | `apt install ffmpeg` (transcoding) |
| `daemon status` exit 1 | daemon not running / stale pidfile | `raag daemon start`; remove stale pidfile |
| `gosec` findings | unguarded conversions/perm constants | see `make sec`; trivial ones already hardened |

## Development

```bash
make build          # build ./bin/raag + ./bin/raagd
make test           # go test -race -cover ./...
make lint           # golangci-lint (errcheck-only baseline expected)
make sec            # gosec
make generate       # buf: regenerate proto/gen from proto/raag.proto
make ci             # full pipeline
```

### Testing notes

- Tests run with `-race` and `goleak` (goroutine-leak verification) in `app`,
  `events`, and `p2p`.
- `internal/infra/p2p/node_test.go` uses `testing/synctest` (virtual clock) for the
  ticker loops — no real 30s sleeps.
- `internal/infra/fsroot` tests prove symlink-escape and out-of-root paths are
  refused by the scanner and stream handler.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full guide (dev setup, quality gates,
testing, commit guidelines, PR process). The short version:

1. Fork and clone; `make dev-setup` for tooling.
2. Create a branch; make changes; add tests.
3. Run `make ci` locally.
4. Open a PR.

## Reporting security issues

See [SECURITY.md](SECURITY.md). Report vulnerabilities privately via GitHub
Security Advisories — do not open a public issue.

## Code of conduct

See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## License

Apache License 2.0. See [LICENSE](LICENSE).

## Changelog

See [CHANGELOG.md](CHANGELOG.md).
