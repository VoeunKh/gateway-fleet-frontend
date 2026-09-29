# Gateway Fleet Console — rules for Claude Code

You are building a production version of the sample in `docs/reference/sample-console.html`.
The sample is the **source of truth for behaviour and UI**. It is a single HTML file that fakes
everything in the browser (localStorage + random simulation). The real product moves that logic
to a server, a database and a small agent on each OpenWrt gateway.

Work from `docs/TASKS.md`. Do tasks **in order**, one task per branch/PR. Do not start a task
until the one before it passes its "Done when" checks.

## Stack (fixed — do not swap without asking)

| Part | Choice | Why (low resource) |
|---|---|---|
| Server | Go 1.23+, one binary | ~20–40 MB RAM, no runtime to install |
| HTTP | `net/http` + `go-chi/chi/v5` | tiny, std-lib style |
| DB | SQLite (WAL) via `modernc.org/sqlite` (no cgo) | no DB server; Postgres later only if needed |
| SQL | hand-written SQL in `internal/store`, migrations with `pressly/goose` embedded | no ORM overhead |
| Device link | MQTT 3.1.1 over TLS, `eclipse/paho.mqtt.golang`; broker = Mosquitto | broker uses a few MB |
| Live UI updates | Server-Sent Events (SSE), **deltas only** | no WebSocket lib, no polling |
| Web UI | Svelte 5 + Vite + TypeScript, built to static files, `go:embed` into the server | JS bundle target < 80 KB gzip |
| Agent | C, `libubox` + `libubus` + `libuci` + `libmosquitto`, packaged as `.ipk` with OpenWrt SDK | target < 150 KB installed, < 3 MB RSS |
| Dev env | `docker compose` (server + mosquitto + simulator) | one command up |

## Repo layout

```
cmd/fleetd/            server main
cmd/fleetsim/          fake gateway fleet (port of the sample's simulation) for dev + tests
internal/domain/       pure types + rules (health, drift, wave plan, rollout state machine, UCI validate)
internal/store/        SQLite access, migrations/
internal/api/          HTTP handlers, auth, RBAC middleware, SSE hub
internal/mqtt/         broker client, topic codec
internal/engine/       rollout engine, alert engine, config push (background workers)
web/                   Svelte app
agent/                 OpenWrt package (Makefile, src/, files/)
deploy/                docker-compose.yml, mosquitto.conf, systemd unit
docs/                  TASKS.md, ADRs, API.md, PROTOCOL.md, reference/
```

## Code rules

- `internal/domain` has **no I/O**. All business rules live there and are unit-tested with table tests.
  Every rule must match the sample (see "Rules to port" in TASKS.md). Quote the sample line in a comment
  when porting, e.g. `// sample: health() — temp>=80||ram>=90 → critical`.
- Handlers are thin: parse → call service → write JSON. No SQL in handlers.
- Errors: wrap with `fmt.Errorf("...: %w", err)`; API returns `{"error":{"code":"...","message":"..."}}`.
- Config via env vars only (`FLEET_DB`, `FLEET_HTTP_ADDR`, `FLEET_MQTT_URL`, ...). Document every one in `README.md`.
- Log with `log/slog`, JSON in prod, text in dev. No `fmt.Println`.
- Time is UTC in storage, ISO-8601 in API. IDs: devices keyed by serial number; others use ULIDs,
  except rollouts which keep the human id `R-001` for display.
- No new dependency without a one-line reason in the PR description.
- Secrets (`{{secret.*}}`) are never returned by the API or written to logs; render as `••••••`.

## Resource budget (check in every PR that touches the hot path)

- Server idle RSS ≤ 40 MB; with 10 000 simulated devices ≤ 150 MB.
- Heartbeat ingest: batch DB writes (flush every 1 s or 500 rows), never one transaction per message.
- Metrics: keep raw samples 24 h, 5-min rollups 30 d, drop after. Device detail sparkline reads rollups.
- UI: list endpoints are paginated (default 100). SSE sends only changed fields for visible entities.
- Web bundle: fail CI if main JS > 80 KB gzip.
- Agent: one process, event loop via `uloop`; no busy loops; heartbeat 30 s (configurable).

## Commands

```
make dev        # docker compose up: fleetd + mosquitto + fleetsim (60 devices, like the sample)
make test       # go test ./... -race  +  cd web && npm test
make lint       # golangci-lint run  +  cd web && npm run check
make build      # web build → go build (embeds web/dist) → bin/fleetd
make agent      # builds .ipk in OpenWrt SDK container (mipsel_24kc and arm_cortex-a7)
```

## Definition of done (every task)

1. `make lint test build` passes.
2. New rules have unit tests; new endpoints have handler tests (httptest) incl. a 403 case per role.
3. `docs/API.md` / `docs/PROTOCOL.md` updated if the contract changed.
4. UI change: checked at 375 px and 1280 px, light and dark, keyboard only.
5. PR description lists: what changed, how it was tested, resource impact.

## Commits

Conventional commits: `feat(rollout): ...`, `fix(api): ...`, `test(domain): ...`, `chore: ...`.
Small commits. Never commit `*.db`, `web/dist`, `bin/`, or secrets.
