CREATE TABLE IF NOT EXISTS pm_delivery_subjects (
    id                       TEXT PRIMARY KEY,
    subject_type             TEXT NOT NULL,
    plan_id                  TEXT NOT NULL,
    task_id                  TEXT NOT NULL,
    node_id                  TEXT NOT NULL DEFAULT '',
    execution_id             TEXT NOT NULL DEFAULT '',
    repo_id                  TEXT NOT NULL DEFAULT '',
    remote                   TEXT NOT NULL,
    branch                   TEXT NOT NULL,
    base_sha                 TEXT NOT NULL,
    candidate_sha            TEXT NOT NULL,
    candidate_ref            TEXT NOT NULL,
    pushed_remote            TEXT NOT NULL,
    delivery_contract_hash   TEXT NOT NULL,
    acceptance_contract_hash TEXT NOT NULL,
    created_at               TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_pm_delivery_subjects_plan_task_created
    ON pm_delivery_subjects(plan_id, task_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS pm_acceptances (
    id               TEXT PRIMARY KEY,
    subject_id       TEXT NOT NULL,
    subject_digest   TEXT NOT NULL,
    plan_id          TEXT NOT NULL,
    task_id          TEXT NOT NULL,
    gate_task_id     TEXT NOT NULL DEFAULT '',
    contract_hash    TEXT NOT NULL,
    verdict          TEXT NOT NULL,
    actor_ref        TEXT NOT NULL,
    authority_rank   INTEGER NOT NULL,
    authority_source TEXT NOT NULL,
    evidence_ref     TEXT NOT NULL DEFAULT '',
    evidence_sha     TEXT NOT NULL DEFAULT '',
    findings_json    TEXT NOT NULL DEFAULT '[]',
    created_at       TEXT NOT NULL,
    FOREIGN KEY(subject_id) REFERENCES pm_delivery_subjects(id)
);
CREATE INDEX IF NOT EXISTS idx_pm_acceptances_subject_contract_authority
    ON pm_acceptances(subject_id, contract_hash, authority_rank DESC, created_at DESC, id DESC);
