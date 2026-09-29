# Fleet console HTTP API

Base path `/api`. JSON in and out. Times are ISO-8601 UTC.

## Conventions

**Errors** — every non-2xx response has this body:

```json
{"error": {"code": "forbidden", "message": "Your role (Viewer) can't do this"}}
```

| Status | `code` | When |
|---|---|---|
| 400 | `bad_request` | Body missing or malformed |
| 401 | `unauthenticated` | No valid session cookie |
| 401 | `invalid_credentials` | Login failed |
| 403 | `csrf` | Non-GET request without a matching `X-CSRF-Token` header |
| 403 | `forbidden` | The user's role lacks the permission; message is the sample's tooltip text |
| 404 | `not_found` | Unknown endpoint |
| 415 | `content_type` | Login body is not `application/json` |
| 422 | `validation` | Rule violation (task B3); message matches the sample's wording |
| 500 | `internal` | Server error (details only in the server log) |
| 501 | `not_implemented` | Write endpoint registered but not built yet (until task B3) |

**Auth** — `POST /api/login` sets the `fleet_session` cookie (`HttpOnly`, `SameSite=Lax`, `Path=/`,
`Secure` when `FLEET_ENV=prod` or on TLS; 12 h lifetime). The cookie value is a random 256-bit token; the
database stores only its SHA-256. Every other `/api` endpoint needs the cookie.

**CSRF** — every non-GET request must send the session's CSRF token (from the login or
`GET /api/session` response) in the `X-CSRF-Token` header.

**Audit** — every successful (status < 400) non-GET request writes one `audit_log` row
(`action` = `METHOD route-pattern`, `target` = path, `user_id`). Logins are logged as `auth.login`,
users created with `fleetd user add` as `user.create`.

## Roles

From the sample's `CAN` table:

| Permission | admin | release | viewer |
|---|---|---|---|
| `rollout`, `config`, `packages`, `firmware` | ✓ | ✓ | – |
| `remote` (reboot, logs, ping), `ack` (alerts) | ✓ | – | – |

## Endpoints

### `POST /api/login`

No session needed. `Content-Type: application/json` required.

```json
{"username": "alice", "password": "correct horse battery"}
```

`200` + `Set-Cookie: fleet_session=…`:

```json
{"user": {"username": "alice", "role": "admin", "roleName": "Admin"}, "csrf": "…"}
```

`401 invalid_credentials` for an unknown user or wrong password (same response and timing).

### `GET /api/session`

Current user and CSRF token (same body as login). `401` if not signed in or expired.

### `POST /api/logout`

Needs CSRF. Deletes the session and clears the cookie. `204`.

## Read endpoints

All `GET`, all need a session; every role may read (no 403). JSON responses are gzip-compressed when the
client sends `Accept-Encoding: gzip`.

**Lists** take `?limit=` (default 100, max 500) and `?cursor=` (opaque; copy `nextCursor` from the previous
page). `nextCursor` is `null` on the last page. A bad `limit` or `cursor` is `400 bad_request`.

**Health** is derived on read (sample `health()`): `offline` if not online; `critical` if temp ≥ 80 or
RAM ≥ 90 %; `warning` if temp ≥ 72, RAM ≥ 80 % or /tmp < 8 MB; else `healthy`. **Drift** means the device's
config version differs from its model's desired version.

### `GET /api/overview`

```json
{
  "total": 60, "online": 51, "attention": 11, "drifted": 12,
  "models": [{"id": "GW-100", "name": "Indoor LoRa", "count": 20,
              "board": [{"sn": "GW100-2431-00001", "health": "healthy", "fw": "1.1.0"}],
              "firmware": [{"version": "1.1.0", "count": 9}, {"version": "1.2.0", "count": 11}, {"version": "1.3.0-beta.1", "count": 0}]}],
  "rollouts": [{"id": "R-001", "model": "GW-200", "fw": "1.3.0-beta.1", "state": "running", "done": 3, "total": 20}],
  "alerts": [ /* up to 5 open or acknowledged alerts, newest first; see /api/alerts */ ]
}
```

`attention` = critical + offline. `rollouts` lists active ones (running, soaking, paused).

### `GET /api/devices?q=&model=&status=&limit=&cursor=`

- `q`: case-insensitive substring of the serial number, site, or any interface identifier (MAC, IMEI, ICCID, LoRa EUI).
- `model`: exact model id.
- `status`: `healthy`, `warning`, `critical`, `offline` or `drift`. Anything else is `400`.

```json
{
  "items": [{"sn": "GW100-2431-00001", "model": "GW-100", "hwRev": "A2", "site": "Warehouse 3", "fw": "1.1.0",
             "cfgVer": 1, "drift": true, "health": "healthy", "online": true, "temp": 57,
             "lastSeen": "2026-09-20T11:59:31.254Z"}],
  "matched": 60, "total": 60, "nextCursor": null
}
```

`matched` counts every device matching the filters (for "X of Y gateways shown"); `total` is all devices.
Order is stable (the order gateways were added).

### `GET /api/devices/{sn}`

The list fields plus:

```json
{
  "modelInfo": {"id": "GW-200", "name": "Outdoor LTE", "soc": "MediaTek MT7628AN", "arch": "mipsel_24kc", "ramMB": 128, "flashMB": 32, "os": "OpenWrt 23.05.4"},
  "bricked": false, "uptimeH": 412, "cpu": 21, "ram": 55, "tmpFreeMB": 31.4, "rssi": -81,
  "interfaces": [{"kind": "lte", "name": "wwan0", "ident": "IMEI 080764246623510, ICCID 8928394032859425682", "up": true}],
  "packages": [{"name": "gw-agent", "installed": "0.9.1", "latest": "0.9.2", "upToDate": false}],
  "config": {"desired": 2, "reported": 2, "drift": false, "rendered": "config gateway 'main'\n\toption serial 'GW200-…'\n…\toption pincode '••••••'\n"},
  "rollout": {"id": "R-001", "fw": "1.3.0-beta.1", "state": "downloading", "pct": 40}
}
```

- `rssi` is `null` for models without LTE.
- `config.rendered` is the desired template filled in for this gateway. **Secrets are always `••••••`.**
- `rollout` is the active rollout this gateway is still in (not yet in a final state), else `null`.
- `404 not_found` "No gateway with serial number X." for an unknown serial.

### `GET /api/devices/{sn}/events?limit=&cursor=`

History, newest first: `{"items": [{"at": "…", "msg": "Provisioned with factory certificate"}], "nextCursor": null}`.

### `GET /api/devices/{sn}/metrics?range=24h`

5-minute rollups, oldest first. `range`: `1h`, `6h`, `24h` (default), `7d`, `30d`.

```json
{"range": "24h", "points": [{"at": "…", "tempAvg": 57, "tempMax": 59, "cpuAvg": null, "ramAvg": null, "tmpMin": null, "rssiAvg": null}]}
```

A field is `null` when the bucket had no sample for it.

### `GET /api/firmware`

`{"items": [{"model", "version", "channel", "released", "sizeMB", "file", "sha256", "blocked", "devices"}]}` —
`devices` is how many gateways of that model run the version.

### `GET /api/packages`

`{"items": [{"name", "version", "prev", "channel", "models": ["GW-100"], "desc", "upToDate", "outdated"}]}` —
counts over gateways that have the package installed.

### `GET /api/config/{model}`

```json
{
  "model": "GW-200", "desired": 2, "inSync": 17, "drifted": 3,
  "versions": [
    {"v": 1, "by": "Release engineer", "at": "2026-06-03T00:00:00Z", "note": "Initial template", "text": "…", "diff": null},
    {"v": 2, "by": "…", "at": "…", "note": "…", "text": "…", "diff": [{"op": "=", "text": "config gateway 'main'"}, {"op": "-", "text": "\toption heartbeat '60'"}, {"op": "+", "text": "\toption heartbeat '30'"}]}
  ]
}
```

`text` is the template with `{{…}}` variables, never secret values. `diff` compares with the previous version
(same line diff as the sample). `404` for an unknown model.

### `GET /api/alerts?state=&limit=&cursor=`

`state`: `active` (default: open + acknowledged, the sample's "Open" list), `open`, `ack`, `resolved`, `all`.
Newest first.

```json
{"items": [{"id": "01J…", "sn": "GW100-2431-00006", "site": "Plant B", "kind": "offline", "severity": "critical",
            "message": "Offline for more than 10 minutes", "state": "open", "at": "…", "resolvedAt": null}],
 "nextCursor": null}
```

`ackedBy` appears once acknowledged.

### `GET /api/rollouts?limit=&cursor=`

Newest first, each with its devices in rollout order (healthy first).

```json
{"items": [{
  "id": "R-001", "model": "GW-200", "fw": "1.3.0-beta.1", "threshold": 10,
  "waves": [1, 1, 8, 10], "wave": 1, "state": "running", "reason": "",
  "soakUntil": null, "createdBy": "Admin", "createdAt": "…", "finishedAt": null,
  "summary": {"updated": 1, "inProgress": 1, "rolledBack": 0, "bricked": 0, "skippedOrDeferred": 0, "waiting": 18},
  "devices": [{"sn": "…", "wave": 0, "state": "success", "pct": 0, "from": "1.2.0"}]
}], "nextCursor": null}
```

`waves` holds the number of gateways in each wave; `wave` is the current wave index (0-based).

### Write endpoints (permission checks live; handlers arrive in task B3)

All `POST`, all need a session and CSRF. Until B3 an allowed request returns `501`.

| Endpoint | Permission |
|---|---|
| `/api/rollouts` | `rollout` |
| `/api/rollouts/preview` | `rollout` |
| `/api/rollouts/{id}/pause` · `/resume` · `/abort` | `rollout` |
| `/api/firmware` | `firmware` |
| `/api/firmware/{model}/{version}/block` · `/unblock` | `firmware` |
| `/api/packages/{name}/update-outdated` | `packages` |
| `/api/config/{model}/versions` | `config` |
| `/api/config/{model}/push` | `config` |
| `/api/devices/{sn}/reboot` · `/logs` · `/ping` | `remote` |
| `/api/alerts/{id}/ack` | `ack` |

### `GET /healthz`

Outside `/api`, no auth. `{"status":"ok"}`.
