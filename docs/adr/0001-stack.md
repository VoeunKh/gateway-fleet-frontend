# 0001 — Technology stack

Status: accepted

## Context
The console manages OpenWrt gateway fleets and must run on small servers: idle RSS ≤ 40 MB, ≤ 150 MB at 10 000 simulated devices, web bundle < 80 KB gzip, agent < 150 KB installed and < 3 MB RSS.

## Decision

| Part | Choice | Why (low resource) |
|---|---|---|
| Server | Go 1.23+, one binary | ~20–40 MB RAM, no runtime to install |
| HTTP | `net/http` + `go-chi/chi/v5` | tiny, std-lib style |
| DB | SQLite (WAL) via `modernc.org/sqlite` (no cgo) | no DB server |
| SQL | hand-written SQL in `internal/store`, `pressly/goose` migrations embedded | no ORM overhead |
| Device link | MQTT 3.1.1 over TLS, `eclipse/paho.mqtt.golang`; broker Mosquitto | broker uses a few MB |
| Live UI updates | Server-Sent Events, deltas only | no WebSocket lib, no polling |
| Web UI | Svelte 5 + Vite + TypeScript, built to static files, `go:embed` | small bundle |
| Agent | C with libubox, libubus, libuci, libmosquitto, packaged as `.ipk` | fits router flash/RAM |
| Dev env | `docker compose` (server + mosquitto + simulator) | one command up |

## Consequences
- Single static binary and no cgo simplifies multi-arch images.
- Business rules live in `internal/domain` with no I/O, so they are testable without infrastructure.
- Swapping any part of this table requires a new ADR.
