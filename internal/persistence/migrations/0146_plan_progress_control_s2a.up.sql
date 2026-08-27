-- 0146_plan_progress_control_s2a.up.sql — S2A ObservationVector + Obligation/Incident.
CREATE TABLE IF NOT EXISTS pm_progress_observations (
    id                                TEXT PRIMARY KEY,
    plan_id                           TEXT NOT NULL,
    task_id                           TEXT NOT NULL DEFAULT '',
    node_id                           TEXT NOT NULL DEFAULT '',
    decision                          TEXT NOT NULL,
    quality                           TEXT NOT NULL,
    as_of                             TEXT NOT NULL,
    evaluated_at                      TEXT NOT NULL,
    source_revisions_json             TEXT NOT NULL DEFAULT '[]',
    facts_json                        TEXT NOT NULL DEFAULT '[]',
    suspect_key                       TEXT NOT NULL DEFAULT '',
    suspect_cycles                    INTEGER NOT NULL DEFAULT 0,
    progress_contract                 TEXT NOT NULL DEFAULT '',
    progress_contract_defaulted       INTEGER NOT NULL DEFAULT 0,
    uncovered_progress_window_seconds INTEGER NOT NULL DEFAULT 0,
    coverage_json                     TEXT NOT NULL DEFAULT '{}',
    created_at                        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_pm_progress_observations_plan_task_eval
    ON pm_progress_observations(plan_id, task_id, evaluated_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS pm_progress_obligations (
    id                     TEXT PRIMARY KEY,
    plan_id                TEXT NOT NULL,
    task_id                TEXT NOT NULL DEFAULT '',
    node_id                TEXT NOT NULL DEFAULT '',
    kind                   TEXT NOT NULL,
    owner_ref              TEXT NOT NULL,
    owner_display          TEXT NOT NULL,
    deadline_at            TEXT NOT NULL,
    ack_required           INTEGER NOT NULL DEFAULT 1,
    acked_at               TEXT NOT NULL DEFAULT '',
    escalate_to_ref        TEXT NOT NULL,
    escalation_deadline_at TEXT NOT NULL,
    source_fact_refs_json  TEXT NOT NULL DEFAULT '[]',
    episode_key            TEXT NOT NULL,
    status                 TEXT NOT NULL,
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL,
    version                INTEGER NOT NULL DEFAULT 1,
    UNIQUE(plan_id, task_id, kind, episode_key)
);
CREATE INDEX IF NOT EXISTS idx_pm_progress_obligations_open_plan
    ON pm_progress_obligations(plan_id, status, deadline_at);

CREATE TABLE IF NOT EXISTS pm_progress_incidents (
    id                     TEXT PRIMARY KEY,
    plan_id                TEXT NOT NULL,
    task_id                TEXT NOT NULL DEFAULT '',
    node_id                TEXT NOT NULL DEFAULT '',
    kind                   TEXT NOT NULL,
    owner_ref              TEXT NOT NULL,
    owner_display          TEXT NOT NULL,
    deadline_at            TEXT NOT NULL,
    ack_required           INTEGER NOT NULL DEFAULT 1,
    acked_at               TEXT NOT NULL DEFAULT '',
    escalate_to_ref        TEXT NOT NULL,
    escalation_deadline_at TEXT NOT NULL,
    source_fact_refs_json  TEXT NOT NULL DEFAULT '[]',
    episode_key            TEXT NOT NULL,
    status                 TEXT NOT NULL,
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL,
    version                INTEGER NOT NULL DEFAULT 1,
    UNIQUE(plan_id, task_id, kind, episode_key)
);
CREATE INDEX IF NOT EXISTS idx_pm_progress_incidents_open_plan
    ON pm_progress_incidents(plan_id, status, deadline_at);

CREATE TABLE IF NOT EXISTS pm_progress_checkpoints (
    plan_id        TEXT NOT NULL,
    source_kind    TEXT NOT NULL,
    source_id      TEXT NOT NULL DEFAULT '',
    revision       TEXT NOT NULL,
    watermark_at   TEXT NOT NULL,
    observed_at    TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    PRIMARY KEY(plan_id, source_kind, source_id)
);
