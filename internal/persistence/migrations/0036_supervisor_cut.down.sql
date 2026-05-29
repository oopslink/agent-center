-- 0036 down: restore supervisor tables (for migration round-trip tests).
-- Restores the schema from 0006 + 0011.

CREATE TABLE IF NOT EXISTS supervisor_invocations (
    id                      TEXT PRIMARY KEY,
    scope_kind              TEXT NOT NULL,
    scope_key               TEXT NOT NULL,
    status                  TEXT NOT NULL DEFAULT 'running',
    hard_timeout_seconds    INTEGER NOT NULL DEFAULT 3600,
    max_tokens              INTEGER,
    trigger_event_ids       TEXT NOT NULL DEFAULT '[]',
    started_at              TEXT NOT NULL,
    completed_at            TEXT,
    failed_reason           TEXT NOT NULL DEFAULT '',
    prompt_blob_ref         TEXT NOT NULL DEFAULT '',
    agent_instance_id       TEXT,
    input_tokens            INTEGER,
    output_tokens           INTEGER
);

CREATE TABLE IF NOT EXISTS decision_records (
    id              TEXT PRIMARY KEY,
    invocation_id   TEXT NOT NULL,
    kind            TEXT NOT NULL,
    target_refs     TEXT NOT NULL DEFAULT '{}',
    rationale       TEXT NOT NULL DEFAULT '',
    outcome         TEXT NOT NULL DEFAULT 'succeeded',
    outcome_message TEXT NOT NULL DEFAULT '',
    recorded_at     TEXT NOT NULL
);
