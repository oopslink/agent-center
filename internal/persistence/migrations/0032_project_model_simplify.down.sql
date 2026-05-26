-- 0032 down: restore v2.5.4 projects schema (kind / default_agent_cli)
-- + suggested_kind column on proposals. Drops + recreates all three
-- tables; rows are not restored (the up migration wipes them).

DROP TABLE IF EXISTS worker_project_mappings;
DROP TABLE IF EXISTS worker_project_proposals;
DROP TABLE IF EXISTS projects;

CREATE TABLE projects (
    id                          TEXT PRIMARY KEY,
    name                        TEXT NOT NULL,
    kind                        TEXT,
    default_agent_cli           TEXT,
    description                 TEXT,
    created_at                  TEXT NOT NULL,
    updated_at                  TEXT NOT NULL,
    created_by_identity_id      TEXT NOT NULL,
    version                     INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE worker_project_mappings (
    id                    TEXT PRIMARY KEY,
    worker_id             TEXT NOT NULL,
    project_id            TEXT NOT NULL,
    base_path             TEXT NOT NULL,
    source_proposal_id    TEXT,
    status                TEXT NOT NULL,
    invalidate_reason     TEXT,
    invalidate_message    TEXT,
    added_at              TEXT NOT NULL,
    invalidated_at        TEXT,
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,
    version               INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_mappings_worker  ON worker_project_mappings (worker_id);
CREATE INDEX idx_mappings_project ON worker_project_mappings (project_id);
CREATE UNIQUE INDEX uniq_mappings_active
    ON worker_project_mappings (worker_id, project_id)
    WHERE status = 'active';

CREATE TABLE worker_project_proposals (
    id                          TEXT PRIMARY KEY,
    worker_id                   TEXT NOT NULL,
    candidate_path              TEXT NOT NULL,
    suggested_project_id        TEXT NOT NULL,
    suggested_kind              TEXT,
    candidate_metadata          TEXT NOT NULL DEFAULT '{}',
    status                      TEXT NOT NULL,
    proposed_at                 TEXT NOT NULL,
    reviewed_at                 TEXT,
    reviewed_by_identity_id     TEXT,
    resulting_mapping_id        TEXT,
    created_at                  TEXT NOT NULL,
    updated_at                  TEXT NOT NULL,
    version                     INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_proposals_worker        ON worker_project_proposals (worker_id);
CREATE INDEX idx_proposals_status        ON worker_project_proposals (status);
CREATE UNIQUE INDEX uniq_proposals_active_path
    ON worker_project_proposals (worker_id, candidate_path)
    WHERE status IN ('pending');
