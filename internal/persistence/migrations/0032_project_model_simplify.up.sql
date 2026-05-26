-- 0032: v2.5.5 (task #59, design @ #agent-center:68d33af4) — Project
-- model simplification.
--
-- The user-typed slug + open enum (kind=coding|writing|investing) +
-- per-project default_agent_cli proved to be friction in actual use:
-- the slug forced a naming decision before doing any work, the kind
-- enum was a guess that rarely matched reality, and default_agent_cli
-- was redundant once agent instances grew their own bindings.
--
-- v2.5.5 replaces all three with: server-generated `proj-<8hex>` id,
-- mutable `name` only, free-text `tags` list (suggestions in UI,
-- but server doesn't validate the set). No backwards compat — pre-
-- existing projects, mappings, and proposals are wiped (single-user
-- dogfood at v2.5.x; no production data to preserve).
--
-- Tables affected:
--   projects                  → drop + recreate (id format + columns change)
--   worker_project_mappings   → wipe rows (FK by app layer to projects.id)
--   worker_project_proposals  → wipe rows + drop suggested_kind column

DROP TABLE IF EXISTS worker_project_mappings;
DROP TABLE IF EXISTS worker_project_proposals;
DROP TABLE IF EXISTS projects;

-- projects: server-gen id (proj-<8hex>) + mutable name + JSON tags
CREATE TABLE projects (
    id                          TEXT PRIMARY KEY,           -- proj-<8hex> server-gen immutable
    name                        TEXT NOT NULL,              -- mutable
    description                 TEXT NOT NULL DEFAULT '',
    tags                        TEXT NOT NULL DEFAULT '[]', -- JSON array of strings
    created_at                  TEXT NOT NULL,
    updated_at                  TEXT NOT NULL,
    created_by_identity_id      TEXT NOT NULL,
    version                     INTEGER NOT NULL DEFAULT 1
);

-- worker_project_mappings: schema unchanged; rows wiped so any
-- stranded references to old slug-style project_id are gone.
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

-- worker_project_proposals: suggested_kind column dropped along with
-- the ProjectKind type. Rows wiped.
CREATE TABLE worker_project_proposals (
    id                          TEXT PRIMARY KEY,
    worker_id                   TEXT NOT NULL,
    candidate_path              TEXT NOT NULL,
    suggested_project_id        TEXT NOT NULL,
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
