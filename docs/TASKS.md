# Gateway Fleet Console — build tasks

Reference: `docs/reference/sample-console.html` (open it in a browser and click through before starting).
Rules and conventions: `CLAUDE.md`.

Each task: **Goal → Do → Done when**. Keep PRs to one task. IDs are stable; refer to them in commits.

---

## 0. What the sample does (scope)

Six screens, three roles, one simulated fleet of 60 OpenWrt gateways (3 models × 20).

| Screen | Features |
|---|---|
| Overview | Fleet board (1 square per gateway, colour = health), firmware split bar per model, active rollouts mini-progress, top 5 open alerts |
| Devices | Search by SN / site / MAC / IMEI / ICCID; filter by model and status (incl. "config drift"); table |
| Device detail | Info, health (temp/CPU/RAM//tmp/LTE RSSI), 30-point temp sparkline, interfaces, packages vs latest, rendered config (secrets hidden), history; actions: Reboot, Pull logs, Ping test |
| Firmware & rollouts | Start staged rollout (model, target fw, waves `1,10,50,100`, failure threshold %), live wave board, pause/resume/abort, firmware image table with block/unblock |
| Packages | .ipk list with channel, models, up-to-date/outdated counts, "Update outdated" |
| Configuration | Per-model UCI template, versions list, line diff vs previous, new version editor with validation, push target version, drift count |
| Alerts | Open / resolved lists, acknowledge, static rules table |

Roles (`CAN` in sample):

| Permission | admin | release | viewer |
|---|---|---|---|
| rollout, config, packages, firmware | ✓ | ✓ | – |
| remote (reboot/logs/ping), ack alerts | ✓ | – | – |

Out of scope for v1 (sample says so): A/B partition updates, maintenance-window enforcement
(store the setting, don't enforce), email/Slack/webhook alert delivery.

## 1. Rules to port exactly (put all of these in `internal/domain`)

**Health** (`health()`):
- offline if not online
- critical if `temp >= 80` or `ram >= 90`
- warning if `temp >= 72` or `ram >= 80` or `tmp_free_mb < 8`
- else healthy

**Drift**: `device.cfg_ver != model.desired_cfg_ver`.

**Wave plan** (`startRollout()`): targets = devices of model where `fw != target` and not bricked,
sorted healthy first. For each pct: `count = max(prev+1, ceil(len*pct/100))`, clamp to len, drop duplicates.
Waves input must be numbers in (0,100] ending with 100. Only one active rollout per model.
Target firmware must not be blocked.

**Device update state machine** (`stepDevice()`):
```
queued ──offline──▶ deferred (terminal)
queued ──tmp_free < image size──▶ skipped (terminal, note "/tmp has X MB free, image needs Y MB")
queued ──▶ downloading(pct) ──▶ verifying (sha256 + signature + sysupgrade -T)
       ──▶ flashing (sysupgrade, keep config) ──▶ rebooting ──▶ checking
checking ──▶ success | rolledback | bricked (all terminal)
```
In production the transitions come from agent reports, not a timer.

**Rollout state machine** (`tickRollout()` / `rolloutAction()`):
- `running`: wait until every device in the current wave is terminal.
- then failure rate = (rolledback + bricked) / (success + rolledback + bricked) over all waves so far.
  If rate > threshold and no override → `paused` with reason
  `Auto-paused after wave N: B of P updated gateways failed (R%, limit T%).`
- else if last wave → `completed`; else → `soaking` (soak time, default 5 min, configurable) → next wave, clear override.
- `pause` (running/soaking → paused), `resume` (if wave done: advance wave and set override=true; if last wave done: completed),
  `abort` (not completed → aborted; all `queued` → `deferred`).
- On server restart, running/soaking rollouts resume from DB state (sample paused them only because it was a browser).

**Alerts** (`checkAlerts()`): dedupe by (sn, kind) while not resolved; auto-resolve when condition clears.
- offline > 10 min → critical (not for bricked)
- temp ≥ 80 → critical; ram ≥ 90 → warning
- bricked after update → critical
- states: open → ack (by user) → resolved. Keep resolved 90 days.

**UCI validation** (`validateUci()`): each non-empty line matches
`^config [a-z_]+( '[^']*')?$` or `^\s+(option|list) [a-z_0-9]+ '[^']*'$`; must contain `config gateway 'main'`.
Save also requires a non-empty note and text different from latest.

**Template variables**: `{{device.sn}}`, `{{site.name}}`, `{{site.ntp_server}}`, `{{site.apn}}`, `{{secret.*}}`.
Server renders per device; secrets are injected only in the payload sent to the device, masked everywhere else.

**Seed data**: port `initData()` 1:1 into `fleetsim` / a `seed` command (same models, sites, firmware,
packages, config v1/v2, seed `20260920`) so screenshots match the sample.

---

## Phase A — Foundation

### A1. Repo scaffold
Goal: empty but working skeleton.
Do: layout from CLAUDE.md; `go.mod`; Makefile targets; `.golangci.yml`; Svelte+Vite+TS app in `web/`;
`deploy/docker-compose.yml` (fleetd, mosquitto); GitHub Actions: lint, test, build, bundle-size check.
Done when: `make dev` serves "hello" page from embedded `web/dist` at `:8080`; CI green.

### A2. ADRs
Goal: record decisions so later tasks don't re-argue them.
Do: `docs/adr/0001-stack.md` (table in CLAUDE.md), `0002-sqlite-first.md`, `0003-mqtt-device-protocol.md`, `0004-sse-for-ui.md`.
Done when: four short ADRs merged (context, decision, consequences).

### A3. Domain package
Goal: all rules from §1 as pure Go with tests.
Do: types `Model, Device, Interface, Firmware, Package, ConfigVersion, Rollout, RolloutDevice, Alert, Role, Permission`;
funcs `Health`, `Drift`, `PlanWaves`, `NextDeviceState`, `EvaluateWave`, `ApplyRolloutAction`, `ValidateUCI`, `RenderTemplate`, `DiffLines` (LCS like sample), `Can(role, perm)`.
Done when: table tests cover every bullet in §1, incl. edge cases (waves `50,100` with 3 devices; all offline; threshold exactly equal = not paused). Coverage ≥ 90 % for `internal/domain`.

### A4. Database + migrations + seed
Goal: schema that fits the domain.
Do: tables `models, sites, devices, device_interfaces, device_packages, device_events, metrics_raw, metrics_5m,
firmware, packages, config_versions, model_config (desired), rollouts, rollout_devices, alerts, users, sessions, audit_log`.
Indexes on `devices(model)`, `devices(site)`, `device_interfaces(ident)`, `alerts(state, sn)`, `rollout_devices(rollout_id, wave)`.
WAL mode, `busy_timeout=5000`, one writer goroutine. `fleetd seed` loads sample data.
Done when: `fleetd migrate && fleetd seed` gives 60 devices; store tests run on a temp DB.

---

## Phase B — Server API

### B1. Auth + RBAC
Do: users table (argon2id), login/logout, HttpOnly SameSite=Lax session cookie, CSRF token for non-GET,
middleware `require(perm)`. `fleetd user add --role admin`. Audit log row for every write.
Done when: tests show viewer gets 403 on every write endpoint, release gets 403 on remote/ack.

### B2. Read endpoints
`GET /api/overview` (counts + board cells: sn, model, health, fw), `GET /api/devices?q=&model=&status=&cursor=`,
`GET /api/devices/{sn}`, `GET /api/devices/{sn}/events`, `GET /api/devices/{sn}/metrics?range=24h`,
`GET /api/firmware`, `GET /api/packages`, `GET /api/config/{model}`, `GET /api/alerts?state=`, `GET /api/rollouts`.
Done when: `docs/API.md` lists request/response per endpoint; handler tests; search matches MAC/IMEI/ICCID.

### B3. Write endpoints
`POST /api/rollouts` (+ `/preview` returning wave sizes, skip/defer counts — the sample's text under the form),
`POST /api/rollouts/{id}/{pause|resume|abort}`, `POST /api/firmware/{model}/{version}/block|unblock`,
`POST /api/firmware` (upload .bin, compute sha256, store on disk under `FLEET_DATA/images/`),
`POST /api/packages/{name}/update-outdated`, `POST /api/config/{model}/versions`, `POST /api/config/{model}/push`,
`POST /api/devices/{sn}/{reboot|logs|ping}`, `POST /api/alerts/{id}/ack`.
Done when: every validation message from the sample is returned as a 422 with the same wording.

### B4. SSE hub
Do: `GET /api/events` stream. Topics: `device.updated` (changed fields only), `rollout.updated`, `alert.opened/resolved`.
Coalesce: at most one event per entity per 1 s. Heartbeat comment every 25 s. Drop slow clients.
Done when: 10 000 simulated devices, 20 open tabs → server RSS ≤ 150 MB, CPU < 1 core on a laptop.

---

## Phase C — Engines (background workers)

### C1. Device ingest
Do: MQTT subscribe `fleet/+/hb`, `fleet/+/metrics`, `fleet/+/status`, `fleet/+/ack`; update devices in batches;
write `metrics_raw`; rollup job every 5 min; retention job daily. Offline detection: last_seen > 90 s.
Done when: fleetsim at 10 000 devices, 30 s heartbeat → DB write load stays under 50 tx/s.

### C2. Rollout engine
Do: one goroutine per active rollout, driven by device reports + soak timer; uses `domain` functions only.
Sends `fleet/{sn}/cmd` `{"op":"upgrade","url":...,"sha256":...,"size":...,"sig":...}`. Timeouts per state
(download 10 min, reboot 5 min, health check 3 min → treated as bricked if no heartbeat). Survives restart.
Done when: integration test with fleetsim reproduces sample behaviour: beta 1.3.0 (30 % fail) auto-pauses after wave 1 or 2; stable completes.

### C3. Alert engine
Do: evaluate on ingest (not on a global timer); dedupe/auto-resolve per §1; emit SSE.
Done when: tests for each rule open/resolve path.

### C4. Config and package push
Do: push renders template per device, sends `fleet/{sn}/cmd {"op":"config","ver":N,"uci":...}` (secrets injected here only);
device acks with hash → `cfg_ver` set. Offline devices get it on reconnect (retained per-device desired state).
Package update sends `{"op":"opkg","pkg":...,"ver":...}`. Remote actions: reboot, logs (last 200 lines of logread), ping.
Done when: drift count on Configuration screen drops as fleetsim devices ack.

---

## Phase D — Web UI (Svelte)

Port the sample's CSS tokens, layout and wording 1:1 (colours, light/dark, 820 px breakpoint, reduced motion).
Shared: `api.ts` (fetch + CSRF), `events.ts` (one EventSource, store updates), `stores/*.ts`, role-aware `<Can perm>` wrapper
that disables buttons with the sample tooltip "Your role (X) can't do this".

- **D1** App shell: rail nav with alert count badge, role shown from session, toasts, routing (hash router, no lib).
- **D2** Overview.
- **D3** Devices list (virtualised rows when > 500) + Device detail (sparkline as inline SVG, same as sample).
- **D4** Firmware & rollouts (preview text, wave board with animated cells, pause/resume/abort, image table, upload).
- **D5** Packages.
- **D6** Configuration (versions, diff, editor with server validation messages, push).
- **D7** Alerts.
- **D8** Login page.

Done when (each): matches sample screenshot at 1280 px and 375 px; keyboard reachable; no full-page re-render on SSE (keyed updates only); Playwright smoke test per screen.

---

## Phase E — Gateway agent (OpenWrt)

### E1. Protocol doc
`docs/PROTOCOL.md`: topics, JSON payloads, QoS (1 for cmd/ack, 0 for metrics), retained desired-state topic, mTLS with per-device cert (CN = serial).

### E2. Agent package `gw-agent`
Do: C daemon under procd; reads identity from UCI `gateway.main.serial`; uloop timers for heartbeat (30 s) and metrics (60 s:
temp from `/sys/class/thermal`, cpu from `/proc/stat`, ram from `/proc/meminfo`, `/tmp` free via `statvfs`, LTE RSSI via ubus `modem` if present,
interface link state via ubus `network.interface`); handles cmd ops `upgrade` (download to /tmp, verify sha256 + `usign`, `sysupgrade -T`, then `sysupgrade`),
`config` (write via libuci, `reload_config`, ack hash), `opkg`, `reboot`, `logs`, `ping`. Reports every upgrade state change.
After boot, reports firmware version so the server can close `checking`.
Done when: `.ipk` builds for `mipsel_24kc` and `arm_cortex-a7`; runs in OpenWrt x86 QEMU in CI against mosquitto; installed size < 150 KB, RSS < 3 MB.

### E3. Health check + rollback hook
Do: after upgrade, agent checks required services (`gw-agent`, `lora_pkt_fwd` if LoRa, `netifd`) within 3 min; on failure re-flashes previous image kept in
`/overlay` only if flash space allows, else reports `unhealthy` (server marks rolledback/bricked per report).
Done when: QEMU test with a deliberately broken image reports rolledback.

---

## Phase F — Hardening and ship

- **F1** Security: rate-limit login, secure headers (CSP same-origin), firmware upload size limit, signed images only, audit log screen (admin).
- **F2** Observability: `/metrics` (Prometheus text, hand-written, no client lib), `/healthz`, `/readyz`.
- **F3** Backup: `fleetd backup` uses SQLite `VACUUM INTO`; nightly in compose.
- **F4** Load test: fleetsim 10 000 devices, record RSS/CPU/DB size in `docs/perf.md`; must meet CLAUDE.md budget.
- **F5** Release: multi-arch Docker image (distroless, < 30 MB), systemd unit, `README.md` install guide.

---

## Suggested order and size

| Order | Tasks | Rough size |
|---|---|---|
| 1 | A1–A4 | 2–3 sessions |
| 2 | B1–B4 + C1 | 3–4 sessions |
| 3 | D1–D3 (visible progress early) | 2–3 sessions |
| 4 | C2 + D4 (rollouts: the core feature) | 2–3 sessions |
| 5 | C3–C4 + D5–D8 | 3 sessions |
| 6 | E1–E3 | 3–4 sessions |
| 7 | F1–F5 | 2 sessions |

## Prompt to start each Claude Code session

```
Read CLAUDE.md and docs/TASKS.md. Do task <ID> only.
Check the matching part of docs/reference/sample-console.html first.
Plan briefly, implement, run `make lint test build`, then open a PR with what/how tested/resource impact.
```
