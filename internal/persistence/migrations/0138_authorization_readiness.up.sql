CREATE TABLE IF NOT EXISTS authorization_readiness_gates (
    id TEXT PRIMARY KEY,
    mode TEXT NOT NULL,
    transports_json TEXT NOT NULL,
    permissions_json TEXT NOT NULL,
    resources_json TEXT NOT NULL,
    checks INTEGER NOT NULL,
    mismatches INTEGER NOT NULL,
    started_at TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_authorization_readiness_gates_observed
    ON authorization_readiness_gates(observed_at DESC, id DESC);

INSERT OR IGNORE INTO authorization_roles
    (id, org_id, kind, name, description, created_by, created_at, updated_at, version)
VALUES
    ('sys-background-project-writer', '', 'system', 'Background project writer', 'System background sweeps for project/task/issue/plan maintenance.', 'system', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 1);

INSERT OR IGNORE INTO authorization_role_permissions
    (role_id, permission_key, resource_kind, delegatable, created_at)
VALUES
    ('sys-background-project-writer', 'project.write', 'project', 0, CURRENT_TIMESTAMP),
    ('sys-background-project-writer', 'task.write', 'task', 0, CURRENT_TIMESTAMP),
    ('sys-background-project-writer', 'issue.write', 'issue', 0, CURRENT_TIMESTAMP),
    ('sys-background-project-writer', 'plan.write', 'plan', 0, CURRENT_TIMESTAMP);

INSERT OR IGNORE INTO authorization_role_assignments
    (id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at, version)
VALUES
    ('asgn-bg-project-writer-global', '*', 'worker:background-reconciler', 'sys-background-project-writer', 'project', '*', 'system', CURRENT_TIMESTAMP, 1),
    ('asgn-bg-task-writer-global', '*', 'worker:background-reconciler', 'sys-background-project-writer', 'task', '*', 'system', CURRENT_TIMESTAMP, 1),
    ('asgn-bg-issue-writer-global', '*', 'worker:background-reconciler', 'sys-background-project-writer', 'issue', '*', 'system', CURRENT_TIMESTAMP, 1),
    ('asgn-bg-plan-writer-global', '*', 'worker:background-reconciler', 'sys-background-project-writer', 'plan', '*', 'system', CURRENT_TIMESTAMP, 1);

INSERT OR IGNORE INTO authorization_role_assignments
    (id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at, version)
SELECT
    'asgn-bg-project-writer-' || id,
    id,
    'worker:background-reconciler',
    'sys-background-project-writer',
    'project',
    '*',
    'system',
    CURRENT_TIMESTAMP,
    1
FROM organizations
WHERE deleted_at IS NULL;

INSERT OR IGNORE INTO authorization_role_assignments
    (id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at, version)
SELECT
    'asgn-bg-task-writer-' || id,
    id,
    'worker:background-reconciler',
    'sys-background-project-writer',
    'task',
    '*',
    'system',
    CURRENT_TIMESTAMP,
    1
FROM organizations
WHERE deleted_at IS NULL;

INSERT OR IGNORE INTO authorization_role_assignments
    (id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at, version)
SELECT
    'asgn-bg-issue-writer-' || id,
    id,
    'worker:background-reconciler',
    'sys-background-project-writer',
    'issue',
    '*',
    'system',
    CURRENT_TIMESTAMP,
    1
FROM organizations
WHERE deleted_at IS NULL;

INSERT OR IGNORE INTO authorization_role_assignments
    (id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at, version)
SELECT
    'asgn-bg-plan-writer-' || id,
    id,
    'worker:background-reconciler',
    'sys-background-project-writer',
    'plan',
    '*',
    'system',
    CURRENT_TIMESTAMP,
    1
FROM organizations
WHERE deleted_at IS NULL;
