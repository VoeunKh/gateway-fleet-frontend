# Gateway Fleet Console

Production version of `docs/reference/sample-console.html`. See `CLAUDE.md` for rules and `docs/TASKS.md` for the plan.

## Commands

`make dev` · `make test` · `make lint` · `make build` (web build → `bin/fleetd`)

## Configuration (env vars)

| Variable | Default | Meaning |
|---|---|---|
| `FLEET_HTTP_ADDR` | `:8080` | HTTP listen address |
| `FLEET_ENV` | (dev) | `prod` switches logs to JSON |
