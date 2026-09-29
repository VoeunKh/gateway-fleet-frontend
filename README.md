# Gateway Fleet Console

Production version of `docs/reference/sample-console.html`. See `CLAUDE.md` for rules and `docs/TASKS.md` for the plan.

## Commands

`make dev` · `make test` · `make lint` · `make build` (web build → `bin/fleetd`)

```
fleetd migrate   # create/upgrade the SQLite schema
fleetd seed      # load the sample fleet (60 gateways) into an empty database
fleetd           # serve (applies migrations first)

# create a user; the password is the first line of stdin (min 10 characters)
printf '%s\n' 'my long password' | fleetd user add --role admin alice
```

Roles: `admin`, `release` (release engineer), `viewer`. API reference: `docs/API.md`.

## Configuration (env vars)

| Variable | Default | Meaning |
|---|---|---|
| `FLEET_DB` | `fleet.db` | SQLite database file (WAL mode; `-wal`/`-shm` files sit next to it) |
| `FLEET_HTTP_ADDR` | `:8080` | HTTP listen address |
| `FLEET_ENV` | (dev) | `prod` switches logs to JSON and marks the session cookie `Secure` |
