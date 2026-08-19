# Security Policy

## Supported versions

Raag is pre-1.0. Only the latest commit on `main` receives security fixes.

## Reporting a vulnerability

**Do not open a public issue for security vulnerabilities.**

- Report privately via GitHub's **Security Advisory** channel:
  https://github.com/p-society/raag/security/advisories/new
- Alternatively, email the maintainers (see the `LICENSE`/repository maintainers)
  with the subject `[SECURITY]`.

Please include:

1. A description of the vulnerability and its impact.
2. Affected component / file / function.
3. Steps to reproduce, or a minimal proof of concept.
4. Your suggested fix, if any.

We aim to acknowledge reports within 48 hours and to coordinate a fix and disclosure
timeline with you.

## Security model

Raag is a **LAN-only** peer-to-peer music player. Its security posture is built
around a few deliberate properties:

| Property | Mechanism |
|----------|-----------|
| LAN confinement | `PeerGater` accepts only RFC1918 / ULA / link-local / loopback addresses at dial, accept, and handshake; `libp2p.ForceReachabilityPrivate` is set when `p2p.lan_only=true` (default) |
| Transport encryption | libp2p Noise/TLS secures every peer connection |
| Admission control | A peer is only admitted after completing manifest exchange; chunk / track-detail / remote-search requests from unadmitted peers are rejected |
| Path confinement | Scanner and stream handler open files through `os.Root` scoped to configured library directories, so symlinks or `..` can never escape the library; transcodes additionally require the track path inside a configured root before ffmpeg runs |
| Peer bans | Banned peers/IPs are rejected at dial, accept, and secure handshake |
| Rate limiting | Per-peer request rate cap and a global upload bandwidth cap |
| Key & file hygiene | libp2p identity key stored `0600`; config/data dirs `0700`; config, pidfile, and daemon log written `0600` |

## Reporting scope

We care about:

- Path traversal / arbitrary file read or serve (the highest-impact class for a
  file-sharing daemon).
- Escaping the LAN-only boundary (dialing public addresses, relay misuse).
- Authentication / admission bypass that lets an unadmitted peer read tracks.
- Resource exhaustion (unbounded streams, memory, disk, bandwidth).
- Cryptography misuse in identity/transport handling.

Out of scope (by design, see the README):

- DHT / IPFS-bootstrap / tracker / relay networking — these do not exist in Raag;
  Raag is deliberately LAN-only.
- Shared-secret "auth modes" — not implemented; trust is bounded to the private
  network + admission + bans.

## Security audits

The repository runs `make sec` (gosec + govulncheck + trivy) and CodeQL in CI. We aim
to keep gosec findings at the documented baseline and to fix real issues promptly.
