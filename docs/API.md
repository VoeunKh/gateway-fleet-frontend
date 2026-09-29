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
