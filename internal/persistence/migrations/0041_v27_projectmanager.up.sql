-- 0041_v27_projectmanager.up.sql — v2.7 B1 (ADR-0046, task #96)
--
-- ProjectManager bounded context tables. Named with a `pm_` prefix so the new
-- work-management Project AR coexists with the legacy Workforce `projects`
-- table (migration 0032) until B3 retires the old derived model (ADR-0036)
-- and decides any data migration. App-layer referential integrity per
-- conventions § 9.w (no FK declarations).
--
-- Scope invariant (ADR-0046 §3): every issue/task carries project_id; there
-- are no global or cross-project work items (enforced in the domain too).

CREATE TABLE pm_projects (
    id              TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT,
    status          TEXT NOT NULL,           -- active | archived
    created_by      TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    version         INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_pm_projects_org ON pm_projects (organization_id);

CREATE TABLE pm_project_members (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL,
    identity_id TEXT NOT NULL,
    role        TEXT NOT NULL,               -- member | owner (v1: unenforced, OQ6)
    added_by    TEXT NOT NULL DEFAULT 'system',
    created_at  TEXT NOT NULL
);
CREATE UNIQUE INDEX uniq_pm_member_project_identity
    ON pm_project_members (project_id, identity_id);
CREATE INDEX idx_pm_members_project ON pm_project_members (project_id);

CREATE TABLE pm_issues (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL,
    title       TEXT NOT NULL,
    description TEXT,
    status      TEXT NOT NULL,               -- open|in_progress|resolved|closed|withdrawn|reopened
    created_by  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_pm_issues_project ON pm_issues (project_id);

CREATE TABLE pm_tasks (
    id                 TEXT PRIMARY KEY,
    project_id         TEXT NOT NULL,
    title              TEXT NOT NULL,
    description        TEXT,
    status             TEXT NOT NULL,        -- open|assigned|running|blocked|completed|verified|canceled|reopened
    assignee           TEXT,                 -- empty when unassigned
    derived_from_issue TEXT,                 -- empty when independent
    completed_by       TEXT,                 -- who set completed (no self-verify)
    blocked_reason     TEXT,
    created_by         TEXT NOT NULL,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    version            INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_pm_tasks_project  ON pm_tasks (project_id);
CREATE INDEX idx_pm_tasks_assignee ON pm_tasks (assignee) WHERE assignee IS NOT NULL AND assignee != '';

CREATE TABLE pm_task_subscribers (
    task_id     TEXT NOT NULL,
    identity_id TEXT NOT NULL,
    added_by    TEXT NOT NULL DEFAULT 'system',
    created_at  TEXT NOT NULL,
    PRIMARY KEY (task_id, identity_id)
);

CREATE TABLE pm_issue_subscribers (
    issue_id    TEXT NOT NULL,
    identity_id TEXT NOT NULL,
    added_by    TEXT NOT NULL DEFAULT 'system',
    created_at  TEXT NOT NULL,
    PRIMARY KEY (issue_id, identity_id)
);

CREATE TABLE pm_code_repo_refs (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL,
    url         TEXT NOT NULL,
    label       TEXT,
    added_by    TEXT NOT NULL DEFAULT 'system',
    created_at  TEXT NOT NULL
);
CREATE INDEX idx_pm_code_repo_refs_project ON pm_code_repo_refs (project_id);
