# Raag

Raag is a terminal-first music player with local playback, playlists, and libp2p-based peer discovery for sharing music between machines.

## What It Does

- Play music from a local directory
- Manage playlists and playback from the CLI or TUI
- Discover peers over mDNS, DHT, and an optional tracker with multi-address peer records
- Listen on TCP and QUIC, and traverse NAT with hole punching, UPnP, and AutoNAT
- Run as a daemon so peer connections stay alive between commands
- Authenticate tracker registrations with a key derived from the peer's persistent libp2p identity

## Quick Start

Build the binaries:

```bash
go build -o bin/raag ./cmd/raag
go build -o bin/tracker ./tracker/cmd/tracker
```

Play locally:

```bash
./bin/raag --tui
```

Start in network mode:

```bash
./bin/raag --network
```

For persistent peer connectivity, prefer daemon mode:

```bash
./bin/raag daemon --network
```

Print the full derived tracker auth key:

```bash
./bin/raag --network network auth-key
```

## Core Commands

Playback:

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

Library:

```bash
./bin/raag library list
./bin/raag library search [query]
./bin/raag library rescan
./bin/raag library add <path>
./bin/raag library remove <title>
```

Playlists:

```bash
./bin/raag playlist create <name>
./bin/raag playlist delete <name>
./bin/raag playlist list
./bin/raag playlist add <playlist> <song>
./bin/raag playlist remove <playlist> <index>
./bin/raag playlist songs <playlist>
./bin/raag playlist play <playlist>
```

Peers and networking:

```bash
./bin/raag peers list
./bin/raag peers info
./bin/raag peers connect <multiaddr>
./bin/raag peers disconnect <peerID>
./bin/raag peers tracker <url>
./bin/raag peers bootstrap <multiaddr>
./bin/raag network status
```

Daemon and config:

```bash
./bin/raag daemon
./bin/raag status
./bin/raag config show
./bin/raag config set <key> <value>
./bin/raag config reset
```

## Important Flags

Client flags:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--network` | `false` | Enable peer discovery and libp2p networking |
| `--host` | `0.0.0.0` | Bind address |
| `--port` | `45678` | Peer listen port |
| `--tracker` | `https://raag-production.up.railway.app` | Tracker URL |
| `--dht` | `true` | Enable DHT discovery |
| `--bootstrap` | empty | Explicit DHT bootstrap peers |
| `--max-peers` | `100` | Peer limit |
| `--music-dir` | `./music` | Local music directory |
| `--tui` | `false` | Start the TUI |
| `--json` | `false` | JSON log output |

Tracker flags:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--http-port` | `8080` | HTTP API port |
| `--libp2p-port` | `45678` | Tracker libp2p host port |
| `--relay` | `true` | Starts tracker libp2p host; relay is not advertised in current P0 |
| `--auth-key` | none | Trusted derived auth public key; repeatable |

## Runtime Files

Raag stores runtime state in `~/.config/raag/`:

- `config.yaml` - persisted configuration
- `identity.key` - persistent libp2p identity key
- `daemon.sock` - daemon control socket
- `playlists.json` - playlist data
- `state.json` - playback state
- `peers.json` - persisted discovered-peer cache

`identity.key` is especially important: it stabilizes the peer ID across restarts and is now the root of tracker authentication.

## Architecture

Raag has two runtime pieces:

- the `raag` client, which owns the music library, player, peer identity, and libp2p host
- the optional `tracker`, which provides peer registration over HTTP for cross-network connectivity

### End-to-End Design

At a high level, the system works like this:

1. A Raag client starts and loads `~/.config/raag/identity.key`
2. That identity key determines the libp2p peer ID
3. The client derives a tracker auth key from the identity key
4. The client starts a libp2p host and enables discovery and NAT traversal features
5. The client discovers peers via mDNS, DHT, tracker, or a combination of them
6. If a tracker is configured, the client registers multiple advertised addresses and a signed auth token with the tracker
7. The tracker validates the registration and returns peer records keyed by peer ID, including multiple addresses
8. Peers connect directly when possible, and use the currently implemented discovery plus NAT-traversal path
9. The daemon keeps those connections alive so the CLI can query or control the node without rebuilding network state each time

### Client Architecture

The `raag` binary is responsible for:

- configuration loading and persistence
- local library and playlist management
- playback control
- libp2p host creation
- peer discovery and registration
- daemon socket support for persistent background operation

Conceptually:

```text
raag CLI / TUI
   |
   +-- config + storage
   +-- library + playlists + player
   +-- network manager
          |
          +-- persistent identity.key
          +-- libp2p host
          +-- mDNS discovery
          +-- DHT discovery
          +-- tracker registration / fetch (peer_id + multiple addrs)
          +-- TCP + QUIC listeners
          +-- ResourceManager
          +-- NAT traversal (hole punching, UPnP, AutoNAT)
```

### Tracker Architecture

The `tracker` binary is intentionally smaller than a full peer node. In the current P0 implementation it does two things:

- serves an HTTP API for peer registration and discovery
- optionally starts a libp2p host, but relay advertising is intentionally disabled until relay behavior is fully verified

Conceptually:

```text
tracker
  |
  +-- HTTP API
  |     +-- /register
  |     +-- /peers
  |     +-- /addr
  |     +-- /health
  |
  +-- optional libp2p host (relay not advertised in current P0)
  |
  +-- peer records keyed by peer_id
  |     +-- addrs[]
  |     +-- last_seen
  |
  +-- trusted auth key list
```

### Discovery Flow

Raag supports three discovery paths:

- `mDNS` for machines on the same LAN
- `DHT` for decentralized discovery using bootstrap peers
- `tracker` for cross-network registration and multi-address peer listing

In practice, the tracker is the most useful option when peers are on different networks or behind NAT.

### Connection Flow

Once a peer is discovered, Raag tries to connect over libp2p. In the current P0 implementation, the intended connection paths are:

- direct connection using one of the peer's advertised TCP or QUIC addresses
- hole punching when both peers are reachable enough for NAT traversal

Circuit-relay support is not advertised in P0 because it has not been verified end to end yet.

### Auth Flow in the Architecture

Tracker authentication is not a separate account system. It is a cryptographic proof that the registering client controls the persistent libp2p identity it is claiming.

The important binding is:

```text
identity.key -> libp2p peer ID
identity.key -> derived tracker auth key
derived tracker auth key -> signed registration token
registration token + peer_id + multiaddr -> tracker verification
```

That means frequent disconnects and reconnects are fine: as long as `identity.key` stays the same, the peer ID stays the same and the derived auth key stays the same.

### Daemon Architecture

The daemon mode exists because peer-to-peer networking is stateful. Running one-off commands repeatedly would otherwise recreate the libp2p host and lose active peer state.

Daemon mode gives you:

- one long-lived libp2p host
- stable peer connections
- periodic tracker registration heartbeat
- periodic tracker peer refresh
- one source of truth for current network state
- a Unix socket interface that short-lived CLI commands can query

In the current P0 implementation, these commands are daemon-backed when the daemon is running:

- `network status`
- `network auth-key`
- `peers list`
- `peers info`
- `peers connect`
- `peers disconnect`
- `peers tracker`
- `peers bootstrap`

Recommended pattern:

```bash
./bin/raag daemon --network --tracker https://your-tracker.example.com
./bin/raag peers list
./bin/raag network status
./bin/raag network auth-key
```

## Network Modes

### LAN Discovery

For machines on the same local network:

```bash
./bin/raag --network
```

Raag uses mDNS automatically and will also run the libp2p stack with NAT traversal helpers enabled.

### Cross-Network Discovery

For friends on different networks, run a tracker on a public host and point clients at it:

```bash
./bin/tracker --http-port 8080 --libp2p-port 45678
./bin/raag daemon --network --tracker https://your-tracker.example.com
```

The tracker provides:

- HTTP peer registration and peer listing
- peer records keyed by `peer_id` with multiple advertised addresses
- a stable rendezvous point for peers on different networks
- periodic liveness tracking via client heartbeats

Current P0 note:

- tracker relay advertising is intentionally disabled until relay behavior is fully implemented and tested

## Tracker Authentication

Tracker auth is now derived from the peer's libp2p identity.

There is no separate long-lived `auth.key` file anymore.

### How It Works

1. Raag loads or creates `~/.config/raag/identity.key`
2. Raag derives an ed25519 tracker auth key from the marshaled identity key bytes using a fixed Raag salt
3. Raag signs a tracker registration token containing:
   - `peer_id`
   - derived auth public key
   - issued timestamp
   - expiry timestamp
   - random nonce
4. The tracker verifies:
   - the request includes `addrs`, `peer_id`, and `auth_data`
   - every advertised address embeds the same `peer_id`
   - the `peer_id` in the signed token matches the request
   - the token signature is valid for the derived auth public key
   - the derived auth public key is in the tracker's trusted key list

If any of those checks fail, registration is rejected.

### Why This Is Better

- reconnects keep the same peer identity and the same tracker auth identity
- a separate tracker-only private key is no longer needed
- tracker trust is bound to the libp2p identity that actually owns the peer ID
- spoofing `peer_id` or announcing a different multiaddr is rejected at registration time

### Get Your Trusted Auth Key

Run:

```bash
./bin/raag --network network auth-key
```

That prints the full public key the tracker should trust for this peer.

Example output:

```text
263e6f196c27a0599bc79ef8146ed279bbc098b9143b3ebbd461f24e5f790a1e
```

You can still use `./bin/raag --network network status` if you want the abbreviated key alongside the rest of the network state.

### Start an Authenticated Tracker

To trust one peer:

```bash
./bin/tracker \
  --http-port 8080 \
  --libp2p-port 45678 \
  --auth-key 263e6f196c27a0599bc79ef8146ed279bbc098b9143b3ebbd461f24e5f790a1e
```

To trust multiple peers, repeat `--auth-key`:

```bash
./bin/tracker \
  --auth-key KEY_ONE \
  --auth-key KEY_TWO \
  --auth-key KEY_THREE
```

If you do not pass any `--auth-key` values, the tracker accepts registrations without enforcing auth.

## Deployment

### Local Tracker

```bash
make build-tracker
./bin/tracker --http-port 8080 --libp2p-port 45678 --auth-key <derived-auth-public-key>
```

### Docker

Build and run:

```bash
docker build -t raag-tracker .
docker run --rm -p 8080:8080 -p 45678:45678 raag-tracker \
  ./tracker --http-port 8080 --libp2p-port 45678 --auth-key <derived-auth-public-key>
```

The provided `Dockerfile` builds the tracker binary and exposes `8080` and `45678`.

### Docker Compose

The checked-in `docker-compose.yml` starts the tracker service and exposes both ports. If you want auth enabled, add the trusted keys to the container command or wrap the container with your own compose override.

### Railway

The repo includes `railway.json` for Dockerfile-based deployment.

Recommended Railway setup:

1. Deploy this repo as a Dockerfile service
2. Expose HTTP port `8080`
3. Expose libp2p port `45678` if your deployment needs peer-to-peer reachability metadata
4. Start the tracker with `--auth-key` values for every peer you want to trust

Example start command:

```bash
./tracker --http-port 8080 --libp2p-port 45678 --auth-key <derived-auth-public-key>
```

If you rotate or delete `~/.config/raag/identity.key` on a client, its peer ID and derived auth key both change, and you must update the tracker trust list.

## Typical Cross-Network Setup

On the public server:

```bash
./bin/tracker --http-port 8080 --libp2p-port 45678 --auth-key <peer-a-key> --auth-key <peer-b-key>
```

On each client:

```bash
./bin/raag daemon --network --tracker https://your-tracker.example.com
```

Useful client checks:

```bash
./bin/raag network status
./bin/raag peers list
./bin/raag peers info
```

## Address Advertisement And Peer Records

The current network stack now advertises multiple dial candidates to the tracker instead of a single guessed address.

Tracker peer records now contain:

- `peer_id`
- `addrs[]`
- `last_seen`

This improves small-scale WAN connectivity because peers can try more than one transport/address combination.

In the current implementation:

- peers register a ranked set of advertised addresses
- QUIC addresses are preferred ahead of TCP addresses when ordering candidates
- obvious non-dialable addresses such as loopback and unspecified addresses are filtered out
- fetched peer records are reconstructed into `peer.AddrInfo` with multiple addresses

This is still not a full observed-address or signed-peer-record design, but it is a substantial improvement over single-address registration.

## Peer Persistence And Bootstrap Hygiene

Raag now treats bootstrap peers and discovered peers as different things.

- `bootstrap_peers` remain explicit configuration
- discovered peers are stored in `peers.json`
- discovered peers are no longer written back into bootstrap config automatically

This keeps the bootstrap set stable and avoids gradually polluting config with arbitrary discovered peers.

## Resource Management

The libp2p host now enables a ResourceManager in addition to the connection manager.

That improves robustness by giving the node bounded network-resource behavior under churn or unexpected peer activity.

The connection manager watermarks are also now derived from configured peer limits instead of using only hardcoded defaults.

## File Transfer

Song sharing in the current P0 implementation uses a framed libp2p transfer protocol.

The transfer path now works like this:

- sender opens a dedicated share stream on `constants.ShareProtocolID`
- sender writes a length-prefixed JSON metadata frame
- sender streams the exact file payload bytes after metadata
- receiver reads metadata with explicit framing, validates it, and enforces limits
- receiver writes into a temporary file, verifies byte count and optional hash, then renames on success
- receiver rescans the library after a successful transfer

The framed metadata includes:

- protocol version
- title
- artist
- album
- original filename
- file extension
- total file size
- SHA-256 digest

P0 transfer safety behavior:

- max metadata size limit
- max file size limit
- idle stream timeout
- temp-file writes with cleanup on failure
- extension preservation instead of forcing `.mp3`

## NAT Traversal

Raag enables several traversal strategies in network mode:

- dual TCP and QUIC listeners
- hole punching
- UPnP port mapping
- AutoNAT reachability detection

This reduces the amount of manual router configuration needed for direct peer connections.

Current P0 note:

- relay is intentionally not advertised by the tracker until that path is fully verified

## Development

Build everything:

```bash
go build ./...
```

Run tests:

```bash
go test ./...
```

Useful make targets:

```bash
make build
make build-tracker
make test
make fmt
make vet
```

## Troubleshooting

No peers discovered:

```bash
./bin/raag --network --tracker https://your-tracker.example.com
./bin/raag network status
```

Things to verify:

- the client is running with `--network`
- the tracker URL is reachable
- the tracker trusts the client's derived auth key
- the client's `identity.key` has not changed unexpectedly
- the daemon is running if you expect daemon-backed peer state
- the tracker is returning at least one dialable advertised address for each peer

Tracker rejects registration:

- confirm the `Auth Key:` shown by `./bin/raag --network network status`
- or use `./bin/raag --network network auth-key` for the full key
- confirm the tracker was started with the full matching `--auth-key`
- confirm the client's `identity.key` was not deleted or replaced

Peer identity changed unexpectedly:

- check `~/.config/raag/identity.key`
- if it was removed, Raag generates a new peer identity and a new derived auth key
- update the tracker trust list to match the new derived auth key

## License

Apache License 2.0
