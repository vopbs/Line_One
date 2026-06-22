# Roadmap

## Current baseline

- Single SIP account and one active call per gateway process.
- SIP over UDP with Digest authentication.
- Browser WebRTC with PCMU or PCMA.
- Browser microphone and local audio-file mixing.
- CLI registration, call, WAV playback, and hangup.
- In-memory SIP trace with Call-ID filtering.

## CentOS production deployment

1. Build static Linux binaries or the provided container image.
2. Run behind HTTPS using Nginx or Caddy so browsers can access microphones.
3. Set `ADVERTISE_IP` explicitly and open the SIP/RTP UDP ports.
4. Use systemd or an orchestrator for restart and log collection.
5. Add Prometheus metrics for registrations, call outcomes, RTP packet counts,
   packet loss, jitter, and call duration.

## Multi-call architecture

The current fixed RTP port and global call state intentionally support one call.
For concurrency:

- Introduce a session map keyed by Call-ID.
- Allocate one RTP port pair per call from a configured range.
- Give every session its own PeerConnection, SIP dialog, trace, and timers.
- Persist call records and traces in SQLite or PostgreSQL.
- Add authentication and tenant isolation before exposing the web UI remotely.

## G.729

Browsers do not provide native G.729 WebRTC encoding. Supporting it requires a
server-side transcoder between Opus/G.711 and G.729. Select a codec library or
media server only after confirming its patent/licensing and redistribution
terms for the deployment countries. The current UI lists G.729 as unavailable
instead of silently negotiating another codec.

## Operational hardening

- TLS/WSS or HTTPS termination.
- CSRF protection and authenticated web sessions.
- Configurable SIP ACL and destination-number policy.
- Encrypted secret storage.
- Rate limiting and brute-force protection.
- Structured logs, trace retention limits, and sensitive-header redaction.
