# Changelog

All notable changes to Raag are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security

- Full `os.Root` path confinement: the scanner and P2P stream handler now open
  files through `os.Root` scoped to the configured library directories, so a
  symlink or `..` component can never escape the library. Transcodes require the
  track path to be lexically inside a configured root before ffmpeg runs.
- Tightened file/dir permissions: config, data, pidfile, and daemon log are now
  `0600`/`0700`; metrics HTTP server sets `ReadHeaderTimeout`.

### Added

- UDP broadcast discovery as a LAN fallback to mDNS (`p2p.broadcast_enabled`,
  `p2p.broadcast_port`), announced to `255.255.255.255:7846` every 2s.
- Goroutine-leak verification via `goleak` in `app`, `events`, and `p2p`.
- `testing/synctest` coverage for the P2P node ticker loops.

### Changed

- Go 1.26 modernization: `errors.AsType`, `(*sync.WaitGroup).Go`, P2P node
  lifecycle moved from a hand-rolled `done` channel to `context.CancelCauseFunc`.

## [0.1.0]

### Added

- **P2P networking**
  - LAN-only peer gating (RFC1918/ULA/link-local/loopback only).
  - mDNS discovery (`raag-local`) with connect-with-retry queue.
  - Cross-peer track search (`search --peers`).
  - Client-side transcode decision and transcoded chunk serving.
  - Peer score, active latency probing, and live TUI peer metrics.
  - Peer persistence, ban/unban, and recovery.
- **Audio pipeline**
  - Local playback (MP3/FLAC/WAV/OGG) via `beep`.
  - Full DSP: 3-band EQ, loudness normalization, crossfade.
  - Gapless local playback with preloaded next-track handoff.
  - On-demand ffmpeg transcoding for cross-codec P2P streaming.
- **Daemon & persistence**
  - `raagd` background daemon with pidfile, unix-socket IPC, and graceful shutdown.
  - BadgerDB-backed library, playlists, peers, and settings (volume persistence).
  - File watcher auto-rescan.
- **CLI & TUI**
  - Full CLI: `play`, `pause`, `resume`, `stop`, `next`, `prev`, `seek`, `volume`,
    `eq`, `status`, `search`, `lib`, `queue`, `playlist`, `peers`, `daemon`,
    `debug`, `health`.
  - 4-panel TUI (library, queue, peers, playlists) with EQ presets and lyrics.
  - Debug subcommands (`peers`, `streams`, `index`, `db`).
- **Reliability**
  - Event bus → IPC event subscription.
  - Concurrency audit: single IPC write serialization point, session-context
    playback, channel-backed event bus, closable TTL peer cache.

### Fixed

- IPC frame interleaving (unified per-connection write lock).
- Concurrent IPC reads on one connection.
- Inverted `queue shuffle` semantics.
- Queue not populated when playing from the library.
- `handleStatus` not reporting `position_ms`.
- Concurrent scan progress handlers clobbering each other.
- `Request_Subscribe` falling through to "unknown request type".
- Volume-change events not flowing through the bus.
- Dead mDNS retry path; peers now re-queued with backoff.

## Earlier work

Pre-changelog development history is available in the repository's git history.
