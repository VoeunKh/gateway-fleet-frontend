# 0002 — SQLite first

Status: accepted

## Context
Fleet size targets are thousands of devices, with heartbeat writes every 30 s. A separate database server adds install and memory cost.

## Decision
Use SQLite in WAL mode (`busy_timeout=5000`) through the pure-Go `modernc.org/sqlite` driver. A single writer goroutine owns writes; heartbeats are batched (flush every 1 s or 500 rows). Raw metrics are kept 24 h, 5-minute rollups 30 d.

## Consequences
- No DB server to operate; backup is `VACUUM INTO`.
- One writer bounds write throughput (target < 50 tx/s at 10 000 devices), which batching keeps well within.
- Single-node only. If horizontal scale or HA is needed, migrate to Postgres; SQL stays hand-written and in `internal/store` to keep that move contained.
