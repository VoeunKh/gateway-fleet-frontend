-- +goose Up
-- Times are INTEGER unix milliseconds (UTC). Devices are keyed by serial number;
-- other entities use ULIDs, except rollouts which keep the human id R-001.

CREATE TABLE models (
    id        TEXT PRIMARY KEY,
    name      TEXT NOT NULL,
    soc       TEXT NOT NULL,
    arch      TEXT NOT NULL,
    ram_mb    INTEGER NOT NULL,
    flash_mb  INTEGER NOT NULL,
    os        TEXT NOT NULL
);

CREATE TABLE sites (
    name        TEXT PRIMARY KEY,
    ntp_server  TEXT NOT NULL,
    apn         TEXT NOT NULL
);

CREATE TABLE devices (
    sn           TEXT PRIMARY KEY,
    model        TEXT NOT NULL REFERENCES models(id),
    hw_rev       TEXT NOT NULL DEFAULT '',
    site         TEXT NOT NULL REFERENCES sites(name),
    fw           TEXT NOT NULL,
    cfg_ver      INTEGER NOT NULL DEFAULT 0,
    online       INTEGER NOT NULL DEFAULT 0,
    bricked      INTEGER NOT NULL DEFAULT 0,
    last_seen    INTEGER NOT NULL DEFAULT 0,
    temp         REAL NOT NULL DEFAULT 0,
    cpu          REAL NOT NULL DEFAULT 0,
    ram          REAL NOT NULL DEFAULT 0,
    tmp_free_mb  REAL NOT NULL DEFAULT 0,
    rssi         REAL,
    uptime_h     REAL NOT NULL DEFAULT 0
);
CREATE INDEX devices_model ON devices(model);
CREATE INDEX devices_site ON devices(site);

CREATE TABLE device_interfaces (
    sn     TEXT NOT NULL REFERENCES devices(sn) ON DELETE CASCADE,
    kind   TEXT NOT NULL,
    name   TEXT NOT NULL,
    ident  TEXT NOT NULL,
    up     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (sn, name)
);
CREATE INDEX device_interfaces_ident ON device_interfaces(ident);

CREATE TABLE device_packages (
    sn       TEXT NOT NULL REFERENCES devices(sn) ON DELETE CASCADE,
    name     TEXT NOT NULL,
    version  TEXT NOT NULL,
    PRIMARY KEY (sn, name)
);

CREATE TABLE device_events (
    id   TEXT PRIMARY KEY,
    sn   TEXT NOT NULL REFERENCES devices(sn) ON DELETE CASCADE,
    at   INTEGER NOT NULL,
    msg  TEXT NOT NULL
);
CREATE INDEX device_events_sn_at ON device_events(sn, at);

CREATE TABLE metrics_raw (
    sn           TEXT NOT NULL,
    at           INTEGER NOT NULL,
    temp         REAL,
    cpu          REAL,
    ram          REAL,
    tmp_free_mb  REAL,
    rssi         REAL
);
CREATE INDEX metrics_raw_sn_at ON metrics_raw(sn, at);
CREATE INDEX metrics_raw_at ON metrics_raw(at);

CREATE TABLE metrics_5m (
    sn        TEXT NOT NULL,
    bucket    INTEGER NOT NULL, -- start of the 5-minute bucket
    temp_avg  REAL,
    temp_max  REAL,
    cpu_avg   REAL,
    ram_avg   REAL,
    tmp_min   REAL,
    rssi_avg  REAL,
    PRIMARY KEY (sn, bucket)
) WITHOUT ROWID;
CREATE INDEX metrics_5m_bucket ON metrics_5m(bucket);

CREATE TABLE firmware (
    model     TEXT NOT NULL REFERENCES models(id),
    version   TEXT NOT NULL,
    channel   TEXT NOT NULL,
    released  TEXT NOT NULL, -- date, YYYY-MM-DD
    size_mb   REAL NOT NULL,
    file      TEXT NOT NULL,
    sha256    TEXT NOT NULL,
    blocked   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (model, version)
);

CREATE TABLE packages (
    name     TEXT PRIMARY KEY,
    version  TEXT NOT NULL,
    prev     TEXT NOT NULL,
    channel  TEXT NOT NULL,
    models   TEXT NOT NULL, -- comma-separated model ids
    descr    TEXT NOT NULL
);

CREATE TABLE config_versions (
    model  TEXT NOT NULL REFERENCES models(id),
    v      INTEGER NOT NULL,
    by     TEXT NOT NULL,
    at     INTEGER NOT NULL,
    note   TEXT NOT NULL,
    text   TEXT NOT NULL,
    PRIMARY KEY (model, v)
);

CREATE TABLE model_config (
    model    TEXT PRIMARY KEY REFERENCES models(id),
    desired  INTEGER NOT NULL
);

CREATE TABLE rollouts (
    id           TEXT PRIMARY KEY, -- R-001
    model        TEXT NOT NULL REFERENCES models(id),
    fw           TEXT NOT NULL,
    threshold    REAL NOT NULL,
    counts       TEXT NOT NULL, -- JSON array of cumulative wave ends
    wave         INTEGER NOT NULL DEFAULT 0,
    state        TEXT NOT NULL,
    reason       TEXT NOT NULL DEFAULT '',
    override     INTEGER NOT NULL DEFAULT 0,
    soak_until   INTEGER NOT NULL DEFAULT 0,
    created_by   TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    finished_at  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX rollouts_model_state ON rollouts(model, state);

CREATE TABLE rollout_devices (
    rollout_id  TEXT NOT NULL REFERENCES rollouts(id) ON DELETE CASCADE,
    seq         INTEGER NOT NULL, -- order in the rollout (healthy first)
    wave        INTEGER NOT NULL,
    sn          TEXT NOT NULL REFERENCES devices(sn),
    state       TEXT NOT NULL,
    pct         REAL NOT NULL DEFAULT 0,
    from_fw     TEXT NOT NULL,
    note        TEXT NOT NULL DEFAULT '',
    updated_at  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (rollout_id, seq)
);
CREATE INDEX rollout_devices_wave ON rollout_devices(rollout_id, wave);

CREATE TABLE alerts (
    id           TEXT PRIMARY KEY,
    sn           TEXT NOT NULL REFERENCES devices(sn),
    kind         TEXT NOT NULL,
    severity     TEXT NOT NULL,
    message      TEXT NOT NULL,
    state        TEXT NOT NULL,
    at           INTEGER NOT NULL,
    acked_by     TEXT NOT NULL DEFAULT '',
    resolved_at  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX alerts_state_sn ON alerts(state, sn);

CREATE TABLE users (
    id             TEXT PRIMARY KEY,
    username       TEXT NOT NULL UNIQUE,
    password_hash  TEXT NOT NULL, -- argon2id (task B1)
    role           TEXT NOT NULL CHECK (role IN ('admin', 'release', 'viewer')),
    created_at     INTEGER NOT NULL
);

CREATE TABLE sessions (
    token_hash  TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf        TEXT NOT NULL,
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL
);
CREATE INDEX sessions_expires ON sessions(expires_at);

CREATE TABLE audit_log (
    id       TEXT PRIMARY KEY,
    at       INTEGER NOT NULL,
    user_id  TEXT,
    action   TEXT NOT NULL,
    target   TEXT NOT NULL DEFAULT '',
    detail   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX audit_log_at ON audit_log(at);

-- +goose Down
DROP TABLE audit_log;
DROP TABLE sessions;
DROP TABLE users;
DROP TABLE alerts;
DROP TABLE rollout_devices;
DROP TABLE rollouts;
DROP TABLE model_config;
DROP TABLE config_versions;
DROP TABLE packages;
DROP TABLE firmware;
DROP TABLE metrics_5m;
DROP TABLE metrics_raw;
DROP TABLE device_events;
DROP TABLE device_packages;
DROP TABLE device_interfaces;
DROP TABLE devices;
DROP TABLE sites;
DROP TABLE models;
