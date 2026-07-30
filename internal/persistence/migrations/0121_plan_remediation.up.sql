-- ADR-0055: immutable GateVerdict + cross-generation Continuation + proposal ledger.
CREATE TABLE IF NOT EXISTS pm_gate_verdicts (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL,
    plan_id         TEXT NOT NULL,
    stage_id        TEXT NOT NULL,
    gate_task_id    TEXT NOT NULL UNIQUE,
    outcome         TEXT NOT NULL CHECK (outcome IN ('pass', 'reject')),
    evidence        TEXT NOT NULL,
    reviewed_sha    TEXT NOT NULL,
    actor_ref       TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    created_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_pm_gate_verdicts_plan
    ON pm_gate_verdicts(plan_id, created_at, id);

CREATE TABLE IF NOT EXISTS pm_plan_continuations (
    id                     TEXT PRIMARY KEY,
    project_id             TEXT NOT NULL,
    plan_id                TEXT NOT NULL,
    root_stage_id          TEXT NOT NULL,
    current_stage_id       TEXT NOT NULL,
    trigger_verdict_id     TEXT NOT NULL UNIQUE,
    status                 TEXT NOT NULL,
    generation             INTEGER NOT NULL DEFAULT 0,
    remaining_budget       INTEGER NOT NULL DEFAULT 3,
    boundary_fingerprint   TEXT NOT NULL,
    pending_proposal_id    TEXT NOT NULL DEFAULT '',
    closed_by_verdict_id   TEXT NOT NULL DEFAULT '',
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL,
    version                INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_pm_plan_continuations_plan
    ON pm_plan_continuations(plan_id, status, created_at, id);

CREATE TABLE IF NOT EXISTS pm_remediation_proposals (
    id                    TEXT PRIMARY KEY,
    project_id            TEXT NOT NULL,
    plan_id               TEXT NOT NULL,
    continuation_id       TEXT NOT NULL,
    trigger_verdict_id    TEXT NOT NULL,
    idempotency_key       TEXT NOT NULL UNIQUE,
    based_on_plan_version INTEGER NOT NULL,
    boundary_fingerprint  TEXT NOT NULL,
    payload_json          TEXT NOT NULL,
    status                TEXT NOT NULL,
    diagnostics_json      TEXT NOT NULL DEFAULT '[]',
    created_by            TEXT NOT NULL,
    created_at            TEXT NOT NULL,
    committed_at          TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_pm_remediation_proposals_continuation
    ON pm_remediation_proposals(continuation_id, created_at, id);

CREATE TABLE IF NOT EXISTS pm_plan_topology_outbox (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL,
    plan_id      TEXT NOT NULL,
    proposal_id  TEXT NOT NULL UNIQUE,
    event_type   TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    status       TEXT NOT NULL,
    attempts     INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL,
    delivered_at TEXT NOT NULL DEFAULT ''
);

ALTER TABLE pm_stages ADD COLUMN origin_verdict_id TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_stages ADD COLUMN continuation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_stages ADD COLUMN generation INTEGER NOT NULL DEFAULT 0;
ALTER TABLE pm_stages ADD COLUMN acceptance_contract TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_stages ADD COLUMN topology_fingerprint TEXT NOT NULL DEFAULT '';

ALTER TABLE pm_tasks ADD COLUMN follows_task_id TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_tasks ADD COLUMN origin_verdict_id TEXT NOT NULL DEFAULT '';

-- Existing authored gates keep their acceptance contract but adopt the new
-- monotonic reject route. The replacement is deliberately narrow and leaves
-- hand-authored JSON fields untouched.
UPDATE pm_stages
SET gate_spec = replace(gate_spec, '"reject_route":"reopen_stage"', '"reject_route":"append_remediation"')
WHERE gate_spec LIKE '%"reject_route":"reopen_stage"%';
