# 0004 — Server-Sent Events for live UI updates

Status: accepted

## Context
The UI shows a live fleet board, rollout waves and alerts. Updates flow server → browser only; user actions are ordinary HTTP requests.

## Decision
`GET /api/events` streams SSE with delta-only events (`device.updated`, `rollout.updated`, `alert.opened/resolved`). At most one event per entity per second, a heartbeat comment every 25 s, and slow clients are dropped. The browser uses one `EventSource` and keyed store updates.

## Consequences
- No WebSocket library; works through standard proxies and reconnects automatically.
- One-directional only, which is sufficient here.
- Coalescing keeps CPU and memory within budget with many open tabs.
