-- 0044_environment_worker.up.sql — v2.7 D1 (ADR-0050, task #102)
--
-- Environment bounded context: the machine-deployed Worker AR + its ordered,
-- replayable WorkerControlEvent command stream. The Worker carries the
-- connection status + cumulative ack cursor (last_acked_offset) the center uses
-- to decide what to replay on reconnect. The control stream is append-only with
-- a strictly increasing per-worker offset and a per-command idempotency key so a
-- re-issued logical command (e.g. a destructive stop/reset) never appears twice.
-- App-layer referential integrity per conventions § 9.w (no FK declarations).
--
-- NOTE: `offset` is a reserved-ish word; it is quoted in DDL + queries to stay
-- safe with modernc.org/sqlite.

-- v2.7 #140 step-3: org is NOT stored on the control-channel Worker AR — a
-- worker's org is the canonical workforce.Worker's org (resolved + guarded at the
-- handler layer). This table carries only control-channel state (status / ack
-- cursor / heartbeat). (fresh-install: column squashed out of the create, no drop
-- migration per the standing fresh-install rule.)
CREATE TABLE env_workers (
    id                TEXT PRIMARY KEY,
    name              TEXT,
    status            TEXT NOT NULL,             -- offline | online
    last_acked_offset INTEGER NOT NULL DEFAULT 0,
    last_heartbeat_at TEXT,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    version           INTEGER NOT NULL
);

CREATE TABLE worker_control_events (
    id              TEXT PRIMARY KEY,
    worker_id       TEXT NOT NULL,
    "offset"        INTEGER NOT NULL,
    idempotency_key TEXT NOT NULL,
    command_type    TEXT NOT NULL,
    payload         TEXT,
    created_at      TEXT NOT NULL,
    UNIQUE (worker_id, "offset"),
    UNIQUE (worker_id, idempotency_key)
);
-- Replay seek for ListAfter (offset strictly greater, ascending).
CREATE INDEX idx_wce_worker_offset ON worker_control_events (worker_id, "offset");
