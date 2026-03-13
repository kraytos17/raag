# Raag Roadmap

This document turns the full codebase review into an execution framework.

It contains three things:

1. A severity-ranked issue list by file
2. A phased implementation roadmap with checklists
3. A target architecture proposal aligned with modern P2P system design

The goal is to move Raag from a promising prototype to a reliable, secure, modern peer-to-peer music-sharing system.

## Executive Summary

Raag already has the right top-level ingredients:

- persistent libp2p identity
- CLI, TUI, and daemon modes
- local music library and playlist management
- mDNS, DHT, and tracker-based discovery
- tracker-side auth allowlisting
- early libp2p-based file sharing

But the current implementation is still prototype-grade in the areas that matter most for a real P2P music product:

- the new P0 framed transfer protocol still needs broader end-to-end integration validation
- daemon mode now owns core network control paths, but not yet all stateful app commands
- tracker registration and peer refresh are now periodic, but still need long-running integration coverage
- tracker now stores peer records by `peer_id` with multiple advertised addresses
- the host now listens on both TCP and QUIC and uses libp2p ResourceManager
- discovered peers are now separated from explicit bootstrap configuration
- relay and cross-NAT behavior are intentionally not advertised until fully wired and verified
- incoming file transfer and library integration are not hardened
- testing is too shallow for distributed runtime behavior

The roadmap below is organized around that reality.

## Current State Assessment

### What Is Solid

- `identity.key` persistence keeps peer IDs stable across restarts
- tracker auth is derived from the persistent identity key
- tracker request validation now checks request peer ID, token peer ID, and multiaddr peer ID consistency
- the codebase structure is understandable and mostly modular
- the project already separates control plane concerns from media/data handling conceptually

### What Is Not Yet Production-Ready

- end-to-end transfer validation across live peers
- daemon ownership of all stateful operations
- long-running lifecycle validation for tracker refresh/heartbeats
- replay resistance and stronger peer-identity binding for auth
- relay correctness under real-world NAT conditions
- bounded resource usage on inbound streams
- resilient integration tests for multi-node behavior

### P0 Progress Snapshot

The following P0 items are now implemented in code:

- daemon-backed `network status`
- daemon-backed peer connect/disconnect/tracker/bootstrap commands
- periodic tracker registration heartbeat
- periodic tracker peer refresh loop
- improved discovery loop and shutdown hygiene
- framed share protocol with length-prefixed JSON metadata
- file extension preservation on receive
- max metadata size and max file size enforcement
- temp-file receive path with cleanup and hash verification
- tracker relay advertising disabled until relay support is verified
- tracker peer records redesigned around `peer_id + addrs[] + last_seen`
- multi-address advertisement replaces single-address tracker registration
- host listens on TCP and QUIC
- ResourceManager added to host construction
- discovered-peer cache no longer rewrites bootstrap config

The following P0 items still remain:

- full daemon ownership of all stateful commands
- remaining command bootstrap cleanup outside the networking slice
- daemon/standalone shutdown persistence unification
- multi-node integration tests for tracker retention and real transfers

## System Overview

### Current High-Level Architecture

Raag currently consists of two runtime programs:

- `raag`: the user-facing peer node
- `tracker`: a registry node with optional libp2p host support; relay advertising is disabled in current P0

The current control and data flow looks like this:

```text
User CLI / TUI
   |
   +-- config + storage
   +-- library + playlist + player
   +-- network manager
            |
            +-- persistent libp2p identity
            +-- discovery manager
            |      +-- mDNS
            |      +-- DHT
            |      +-- tracker registration/fetch
            |
            +-- libp2p transport
            +-- song sharing stream

Tracker
   |
   +-- HTTP registry
   +-- peer auth allowlist
   +-- in-memory peer table
   +-- optional libp2p host (relay not advertised in current P0)
```

### Current Architectural Gap

The intended product is a stateful P2P daemon with reliable music sharing.

The actual implementation is still split between:

- a long-lived daemon process that only exposes a small socket API
- many CLI commands that create fresh runtimes instead of talking to the daemon

That mismatch is one of the main reasons the product feels incomplete end to end.

## Severity-Ranked Issue List By File

This section is intended to become the authoritative engineering backlog seed.

### Critical Severity

#### `internal/network/network.go`

- [x] Replace the raw song transfer stream with a framed protocol
  - Implemented with a dedicated share protocol ID, length-prefixed JSON metadata, exact payload copying, temp-file writes, and hash verification.
- [x] Introduce explicit message boundaries
  - Metadata is now framed explicitly before payload transfer.
- [x] Stop writing inbound files directly without admission control
  - P0 adds metadata/file-size limits, idle timeouts, temp-file cleanup, and stream reset on failure.
- [x] Preserve original file format instead of forcing `.mp3`
  - Receive path now preserves original extension.
- [x] Add TCP and QUIC listen addresses
  - Host now listens on both transports for better WAN dialability.
- [x] Add libp2p ResourceManager
  - Host construction now includes a ResourceManager and peer-aware connection watermarks.
- [x] Stop writing discovered peers back into bootstrap config
  - Bootstrap peers and discovered peers are now treated separately.

#### `cmd/raag/socket.go`

- [x] Expand the daemon socket protocol to support the current P0 network workflow
  - Playback commands
  - library commands
  - peer connect/disconnect
  - share requests
  - network status
- [ ] Make the daemon the single runtime owner for all stateful operations
  - P0 network control paths are daemon-owned; playback/library/share are still incomplete.

#### `cmd/raag/commands.go`

- [ ] Remove fresh-runtime behavior for commands that should target the daemon
  - Network control paths now prefer the daemon; playback/library/share still need follow-up.
- [x] Fix `network status` daemon routing
  - `network status` is now socket-backed and structured.
- [ ] Fix initialization gaps in commands that touch `lib` or `netMgr`

#### `internal/discovery/manager.go`

- [x] Add periodic tracker registration refresh / heartbeat
  - Heartbeat loop now refreshes tracker presence periodically.
- [x] Add periodic tracker fetch refresh for new peers
  - Peer refresh loop now runs periodically.
- [x] Fix DHT retry loop structure and context handling
  - Long-lived context hygiene has been improved and the incorrect retry wrapper removed.
- [x] Audit all shared maps for proper locking
  - Network-state assembly now uses locking more safely for known peers.

### High Severity

#### `tracker/server.go`

- [ ] Upgrade tracker auth from allowlisted token verification to stronger peer-identity proof
  - Current model verifies a trusted derived auth key and consistency of claims.
  - It still does not fully prove possession of the libp2p identity key for the claimed peer in a network-native way.
- [ ] Add token replay protection if registration tokens remain long-lived
  - Nonce exists but is not tracked.
- [ ] Verify relay mode against real libp2p relay-v2 expectations
  - P0 intentionally disables relay advertising until this is implemented and validated.

#### `internal/discovery/tracker.go`

- [ ] Fully wire tracker relay metadata into discovery and dialing behavior
  - Ensure client logic uses relay information when direct dialing is not possible.
- [x] Normalize tracker API contracts
  - Tracker client now uses typed response/request structures for current P0 fields.
- [x] Support tracker peer records with multiple addresses
  - Tracker fetch/registration now operate on `peer_id + addrs[] + last_seen` records.

#### `internal/library/library.go`

- [ ] Replace title-keyed song storage with stable IDs
  - Current title-based indexing causes collisions and poor handling of duplicates or missing tags.
- [ ] Make library mutations durable and predictable
  - `AddSong` and `RemoveSong` currently modify in-memory state, not the underlying music directory.
- [ ] Add automatic ingest path for received files
  - Receiving a shared file should not require fragile manual recovery to appear in the library.

#### `internal/player/player.go`

- [ ] Implement actual queue progression instead of infinite looping on a single track
  - Current playback loops one song forever using `beep.Loop(-1, streamer)`.
- [ ] Add proper playlist semantics
  - advancing tracks
  - stopping at end
  - repeat mode if desired
  - shuffle if desired
- [ ] Separate player state transitions from CLI output side effects

#### `cmd/raag/daemon.go`

- [ ] Persist state on daemon shutdown just like standalone shutdown
  - Playlists, player state, and config should not diverge by runtime mode.
- [ ] Clarify daemon lifecycle guarantees
  - startup order
  - socket readiness
  - network readiness
  - clean shutdown semantics

### Medium Severity

#### `internal/auth/auth.go`

- [ ] Consider moving to challenge-response with peer-key proof
  - The current model is good for allowlisting but still app-layer oriented.
- [ ] Tighten token lifetime and refresh semantics
  - 24-hour tokens are acceptable for early development but long for production security posture.

#### `internal/metadata/metadata.go`

- [ ] Replace the pipe-delimited metadata string with a structured format
  - Use JSON, CBOR, protobuf, or another explicitly framed encoding.
- [ ] Include format, file size, and integrity fields in share metadata

#### `internal/storage/*`

- [ ] Load and apply saved player state on startup
  - State is saved more clearly than it is restored.
- [ ] Clarify separation of config, library cache, peer cache, and playback state

#### `cmd/raag/app.go`

- [ ] Standardize bootstrap behavior for all command paths
  - Some commands initialize fully, others do not.
- [ ] Add explicit runtime modes
  - offline CLI mode
  - local playback mode
  - daemon client mode
  - daemon host mode

## Modern P2P Comparison

### Where Raag Already Matches Modern Patterns

- persistent node identity
- multi-plane discovery
- daemon-style long-lived host concept
- explicit tracker auth allowlist
- NAT traversal features enabled at host creation

### Where Raag Falls Behind Modern P2P Implementations

#### Protocol Design

Modern P2P systems use:

- structured protocol messages
- explicit framing
- request/response or stream subprotocols
- integrity metadata
- backpressure-aware chunking

Raag currently uses:

- a framed transfer stream for song sharing
- length-prefixed JSON metadata
- exact payload-length copying with size checks
- no resumable transfer model yet

#### Identity and Auth

Modern libp2p-aligned systems tend to prefer:

- signed peer records
- proof of possession of the peer identity key
- challenge-response tied to actual peer identity

Raag currently uses:

- derived app auth key from peer identity bytes
- tracker-side allowlist
- consistency checks between request, token, and multiaddr

This is useful and coherent, but still not the strongest peer-identity-native model.

#### Resource Protection

Modern systems usually include:

- per-peer rate limits
- stream caps
- quotas
- message size limits
- bounded memory and disk impact
- admission controls

Raag currently has very limited protection on inbound shared content.

Raag has improved host-level protection with ResourceManager and better connection-manager alignment, but still lacks richer peer admission and scoring policies.

#### Relay and NAT Strategy

Modern libp2p deployments often rely on:

- relay v2
- AutoRelay
- reservation flow
- observed/public address handling
- structured reachability checks

Raag currently enables several useful knobs, but relay/reservation behavior is intentionally not advertised in P0 until it is implemented and verified.

## Target Architecture Proposal

This is the recommended medium-term architecture for Raag.

### Architectural Principles

- one long-lived peer process should own all stateful network behavior
- app protocols must be framed and versioned
- tracker auth must be tightly bound to peer identity and be replay-resistant
- received content must be resource-bounded and policy-controlled
- library, player, and P2P sharing should behave like one coherent product rather than separate subsystems
- every major network promise must have automated multi-node integration coverage

### Target Runtime Architecture

```text
CLI / TUI client
   |
   +-- daemon RPC client
            |
            v
       Raag daemon
            |
            +-- config/state manager
            +-- library indexer
            +-- playlist manager
            +-- player engine
            +-- transfer manager
            +-- libp2p node
            |      +-- identity
            |      +-- peerstore
            |      +-- discovery
            |      +-- relay/autorelay
            |
            +-- policy layer
                   +-- auth
                   +-- transfer limits
                   +-- trust model

Tracker service
   |
   +-- registration API
   +-- peer directory
   +-- optional relay service
   +-- auth verifier
   +-- TTL / liveness manager
```

### Target Protocol Architecture

Replace the current single raw stream with a versioned transfer protocol.

Recommended message families:

- `ShareOffer`
  - song ID
  - title
  - artist
  - album
  - codec / extension
  - total size
  - content hash
  - sender peer ID
- `ShareAccept`
  - accepted / denied
  - reason
  - optional save policy
- `Chunk`
  - transfer ID
  - offset
  - length
  - payload
- `TransferComplete`
  - transfer ID
  - final hash
- `TransferAbort`
  - transfer ID
  - reason

This can be implemented over:

- JSON with length-prefixing for simplicity first
- protobuf or CBOR later for better protocol hygiene

### Target Auth Architecture

Recommended next evolution:

- keep the current derived auth key model for allowlisting if useful
- add tracker challenge-response proving possession of the live peer identity key
- optionally move to signed peer records if appropriate for the stack version
- add replay defense by tracking challenge IDs or short-lived nonces

### Target Discovery Architecture

- LAN: mDNS
- WAN: tracker + DHT
- NAT-heavy environments: verified relay service + optional AutoRelay
- Tracker should provide:
  - peer TTL refresh
  - last-seen timestamps
  - structured dial hints
  - relay capability information

### Target Library and Player Architecture

- library entries keyed by stable content ID or file ID, not title
- transfers feed into an ingest pipeline
- ingest pipeline validates file, stores it safely, extracts metadata, and updates library index
- player queue logic should be deterministic and support:
  - play once
  - next/previous
  - playlist sequencing
  - repeat/shuffle as explicit modes

## Phased Implementation Roadmap

## Phase P0 - Make The Existing Product Honest And Safe

Goal: remove the most dangerous correctness problems and make the current advertised workflow minimally reliable.

### P0 Checklist

- [x] Replace current share stream with a framed transfer protocol
- [x] Preserve original file extension during receive
- [x] Add max size and timeout protections on inbound transfer streams
- [x] Fix `network status` daemon routing
- [x] Route core network commands through the daemon instead of bootstrapping new runtimes
- [ ] Fix command initialization gaps for library and peer bootstrap commands
- [x] Add tracker registration heartbeat
- [x] Add tracker peer refresh loop
- [x] Add integration path so received files are ingested into the library automatically
- [ ] Save daemon state on shutdown just like standalone mode
- [x] Disable tracker relay advertising until relay behavior is real and tested
- [x] Replace single-address tracker registration with multi-address peer records
- [x] Add QUIC listening alongside TCP
- [x] Separate discovered peers from bootstrap config persistence
- [x] Add host ResourceManager

### P0 Acceptance Criteria

- [ ] A daemon can stay online for more than tracker timeout without disappearing from `/peers`
- [ ] A shared song arrives intact and is playable afterward
- [x] `raag network status` reflects the daemon node, not a transient process
- [ ] `raag library list` and related commands work reliably in normal CLI use
- [x] Users can receive non-MP3 content without extension corruption

## Phase P1 - Make The P2P Runtime Cohesive

Goal: turn daemon mode into the real product runtime and eliminate split-brain behavior between short-lived commands and the long-lived node.

### P1 Checklist

- [ ] Define a full daemon RPC surface for stateful operations
- [ ] Move playback commands to daemon RPC
- [ ] Move share commands to daemon RPC
- [ ] Move peer connect/disconnect to daemon RPC
- [ ] Move library mutation/rescan flows to daemon RPC where appropriate
- [ ] Standardize startup and shutdown sequencing across standalone and daemon modes
- [ ] Add status endpoints or commands for transfer progress and current network health

### P1 Acceptance Criteria

- [ ] All stateful commands operate on the daemon when the daemon is running
- [ ] No user-facing command creates a misleading parallel node unexpectedly
- [ ] Playback and network state remain consistent across repeated CLI commands
- [ ] Transfer progress and failures are visible to users

## Phase P2 - Modernize P2P Networking And Auth

Goal: align the networking layer more closely with modern libp2p system design.

### P2 Checklist

- [ ] Rework tracker auth toward peer-identity-native proof
- [ ] Add replay resistance for tracker registration
- [ ] Verify and, if necessary, redesign relay service behavior around relay v2 expectations
- [ ] Add structured address selection and reachability handling
- [ ] Audit and fix concurrency around discovery state maps
- [ ] Add transfer authorization policy for inbound streams
- [ ] Add connection, stream, and disk resource policies

### P2 Acceptance Criteria

- [ ] Tracker auth cannot be replayed trivially within the token lifetime
- [ ] Relay-assisted connections are verified in NAT-heavy test scenarios
- [ ] Discovery state remains correct under churn and passes race detection
- [ ] Untrusted or oversized inbound transfers are rejected safely

## Phase P3 - Productize The Music-Sharing Experience

Goal: move beyond raw file transfer and make the app feel like a deliberate P2P music product.

### P3 Checklist

- [ ] Add explicit transfer offers and accept/deny semantics
- [ ] Add metadata-rich song transfer objects
- [ ] Add library ingest pipeline with duplicate detection
- [ ] Add queue progression and playlist playback correctness
- [ ] Add resumable transfer support if desired
- [ ] Add content hash verification
- [ ] Add UX around received songs, pending transfers, and trust prompts

### P3 Acceptance Criteria

- [ ] Shared songs appear in the library automatically with correct metadata
- [ ] Playlist playback works as users expect
- [ ] Duplicate or corrupt transfers are detected cleanly
- [ ] Users can inspect and control transfer behavior from CLI/TUI

## Phase P4 - Hardening And Operations

Goal: make the project deployable and maintainable in real environments.

### P4 Checklist

- [ ] Add multi-node integration tests in CI
- [ ] Add long-running daemon stability tests
- [ ] Add tracker load/liveness tests
- [ ] Add structured observability for discovery, relay, and transfer events
- [ ] Add documented deployment topologies for Railway and non-Railway environments
- [ ] Add backup/migration guidance for persistent identity and state

### P4 Acceptance Criteria

- [ ] CI covers tracker auth, discovery, transfer, and daemon workflows
- [ ] Long-lived nodes remain healthy over extended runs
- [ ] Deployment docs match actual runtime behavior
- [ ] Operators can safely rotate or recover state with clear instructions

## Workstreams And Ownership Framework

These workstreams can run partly in parallel.

### Workstream A - Protocol And Transfer Plane

- [ ] framed transfer spec
- [ ] sender implementation
- [ ] receiver implementation
- [ ] integrity checks
- [ ] ingest integration
- [ ] transfer tests

### Workstream B - Daemon And RPC Surface

- [ ] command inventory
- [ ] socket/RPC contract
- [ ] daemon ownership rules
- [ ] migration of stateful commands
- [ ] shutdown persistence

### Workstream C - Discovery, Tracker, Relay

- [ ] registration heartbeat
- [ ] peer refresh
- [ ] relay verification
- [ ] dial strategy improvements
- [ ] NAT scenario testing

### Workstream D - Auth And Policy

- [ ] stronger tracker auth binding
- [ ] replay defense
- [ ] inbound share authorization policy
- [ ] trust management UX

### Workstream E - Library / Player Product Correctness

- [ ] stable song IDs
- [ ] queue progression
- [ ] playlist semantics
- [ ] duplicate handling
- [ ] state restore

## Test Strategy Roadmap

### Unit Tests

- [ ] transfer message encoding/decoding
- [ ] auth token generation and validation
- [ ] tracker registration validation cases
- [ ] library indexing rules
- [ ] player queue progression

### Integration Tests

- [ ] client registers with tracker and refreshes before expiry
- [ ] two nodes discover each other through tracker
- [ ] share transfer succeeds end to end with intact file hash
- [ ] daemon receives commands from CLI and updates one live runtime
- [ ] relay path works when direct dialing is unavailable

### Chaos / Stress Tests

- [ ] rapid connect/disconnect peer churn
- [ ] tracker restart during active clients
- [ ] slow consumer / partial stream conditions
- [ ] large file rejection and quota enforcement
- [ ] race detector runs for discovery and network packages

## Definition Of Done For Raag 1.0 P2P Core

Raag should not be considered a reliable P2P music-sharing network until all of the following are true:

- [ ] daemon is the authoritative stateful runtime
- [ ] transfer protocol is framed, versioned, and integrity-checked
- [ ] tracker auth is robust and replay-resistant
- [ ] tracker registrations refresh automatically
- [ ] relay-assisted connectivity is verified in realistic NAT scenarios
- [ ] received songs are safely ingested and playable
- [ ] playlist and queue behavior are correct
- [ ] integration coverage exists for cross-node sharing and daemon behavior
- [ ] README and deployment docs describe what the code actually does

## Recommended Immediate Order Of Execution

If the team wants the fastest path to meaningful improvement, do the work in this order:

1. Finish remaining P0 command/bootstrap cleanup and shutdown persistence
2. Add broader multi-node integration tests for tracker-based discovery, multi-address dialing, and churn
3. Begin P1 daemon-first command model for all stateful operations
4. P1 library ingest and player correctness
5. P2 auth and relay modernization
6. P3 product-level polish and transfer UX
7. P4 hardening and operational maturity

## Notes For Future Planning

- Keep the tracker lightweight. Do not let it turn into an all-purpose media broker.
- Prefer peer-to-peer transfer with tracker as control-plane support, not as a content-plane dependency.
- Avoid adding more user-facing commands until the daemon/runtime split is resolved cleanly.
- Treat protocol framing and test coverage as foundational, not optional cleanup work.
