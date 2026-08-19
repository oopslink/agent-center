-- 0129_unified_authorization.up.sql -- frozen unified permission contract data core.
--
-- Phase 1 keeps legacy membership tables authoritative. These tables hold the
-- permission registry, system/custom role definitions, explicit custom role
-- assignments, idempotency records, and an auditable mutation ledger. They do
-- not copy members, pm_project_members, team_members, conversations, or
-- file_references into grants.

CREATE TABLE IF NOT EXISTS permission_definitions (
    key                 TEXT PRIMARY KEY,
    category            TEXT NOT NULL CHECK (category = 'access'),
    resource_kinds_json TEXT NOT NULL,
    actions_json        TEXT NOT NULL,
    legacy_sources_json TEXT NOT NULL,
    created_at          TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS authorization_roles (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL CHECK (kind IN ('system', 'custom')),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    revoked_at  TEXT,
    version     INTEGER NOT NULL DEFAULT 1
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_authorization_roles_system_name
    ON authorization_roles(name)
    WHERE kind = 'system' AND revoked_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_authorization_roles_custom_org_name
    ON authorization_roles(org_id, name)
    WHERE kind = 'custom' AND revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_authorization_roles_org
    ON authorization_roles(org_id, kind)
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS authorization_role_permissions (
    role_id        TEXT NOT NULL,
    permission_key TEXT NOT NULL,
    resource_kind  TEXT NOT NULL,
    delegatable    INTEGER NOT NULL DEFAULT 0 CHECK (delegatable IN (0, 1)),
    created_at     TEXT NOT NULL,
    PRIMARY KEY (role_id, permission_key, resource_kind)
);

CREATE INDEX IF NOT EXISTS idx_authorization_role_permissions_permission
    ON authorization_role_permissions(permission_key, resource_kind);

CREATE TABLE IF NOT EXISTS authorization_role_assignments (
    id              TEXT PRIMARY KEY,
    org_id          TEXT NOT NULL,
    subject_ref     TEXT NOT NULL,
    role_id         TEXT NOT NULL,
    resource_kind   TEXT NOT NULL,
    resource_id     TEXT NOT NULL,
    created_by      TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    expires_at      TEXT,
    revoked_at      TEXT,
    revoked_by      TEXT NOT NULL DEFAULT '',
    revoked_reason  TEXT NOT NULL DEFAULT '',
    version         INTEGER NOT NULL DEFAULT 1
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_authorization_active_assignment_identity
    ON authorization_role_assignments(org_id, subject_ref, role_id, resource_kind, resource_id)
    WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_authorization_assignments_subject
    ON authorization_role_assignments(org_id, subject_ref, resource_kind, resource_id)
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS authorization_idempotency_keys (
    idempotency_key TEXT PRIMARY KEY,
    actor_ref       TEXT NOT NULL,
    operation       TEXT NOT NULL,
    request_hash    TEXT NOT NULL,
    response_json   TEXT,
    status          TEXT NOT NULL CHECK (status IN ('pending', 'completed')),
    created_at      TEXT NOT NULL,
    completed_at    TEXT
);

CREATE TABLE IF NOT EXISTS authorization_audit_events (
    id            TEXT PRIMARY KEY,
    event_type    TEXT NOT NULL,
    actor_ref     TEXT NOT NULL,
    subject_ref   TEXT NOT NULL DEFAULT '',
    permission_key TEXT NOT NULL DEFAULT '',
    resource_kind TEXT NOT NULL DEFAULT '',
    resource_id   TEXT NOT NULL DEFAULT '',
    role_id       TEXT NOT NULL DEFAULT '',
    assignment_id TEXT NOT NULL DEFAULT '',
    request_id    TEXT NOT NULL DEFAULT '',
    payload_json  TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_authorization_audit_created
    ON authorization_audit_events(created_at, id);

CREATE INDEX IF NOT EXISTS idx_authorization_audit_resource
    ON authorization_audit_events(resource_kind, resource_id, created_at);

CREATE INDEX IF NOT EXISTS idx_authorization_audit_subject
    ON authorization_audit_events(subject_ref, actor_ref, created_at);

INSERT OR IGNORE INTO permission_definitions
    (key, category, resource_kinds_json, actions_json, legacy_sources_json, created_at)
VALUES
    ('org.read', 'access', '["org"]', '["read"]', '["members"]', datetime('now')),
    ('org.settings.manage', 'access', '["org"]', '["manage"]', '["members.role"]', datetime('now')),
    ('org.lifecycle.manage', 'access', '["org"]', '["manage"]', '["members.role"]', datetime('now')),
    ('org.member.list', 'access', '["org"]', '["list"]', '["members"]', datetime('now')),
    ('org.member.create.human', 'access', '["org"]', '["create"]', '["members.role"]', datetime('now')),
    ('org.member.create.agent', 'access', '["org"]', '["create"]', '["members.role"]', datetime('now')),
    ('org.member.role.manage', 'access', '["org"]', '["manage"]', '["members.role"]', datetime('now')),
    ('org.member.disable', 'access', '["org"]', '["manage"]', '["members.role"]', datetime('now')),
    ('org.invitation.manage', 'access', '["org"]', '["manage"]', '["members.role"]', datetime('now')),
    ('org.analytics.read', 'access', '["org"]', '["read"]', '["members.role"]', datetime('now')),
    ('org.work_items.read', 'access', '["org"]', '["read"]', '["members"]', datetime('now')),
    ('project.read', 'access', '["project"]', '["read"]', '["pm_project_members"]', datetime('now')),
    ('project.write', 'access', '["project"]', '["update"]', '["pm_project_members"]', datetime('now')),
    ('project.member.add', 'access', '["project"]', '["create"]', '["pm_project_members"]', datetime('now')),
    ('project.member.remove', 'access', '["project"]', '["delete"]', '["pm_project_members.role"]', datetime('now')),
    ('project.repo_ref.manage', 'access', '["project"]', '["manage"]', '["pm_project_members"]', datetime('now')),
    ('project.stage.manage', 'access', '["project"]', '["manage"]', '["pm_project_members.role"]', datetime('now')),
    ('team.read', 'access', '["team"]', '["read"]', '["members"]', datetime('now')),
    ('team.write', 'access', '["team"]', '["update"]', '["members"]', datetime('now')),
    ('team.member.manage', 'access', '["team"]', '["manage"]', '["members"]', datetime('now')),
    ('team.project.link.manage', 'access', '["team"]', '["manage"]', '["members"]', datetime('now')),
    ('team.runtime_config.manage', 'access', '["team"]', '["manage"]', '["members"]', datetime('now')),
    ('team.memory.read', 'access', '["team"]', '["read"]', '["members","team_members"]', datetime('now')),
    ('team.memory.propose', 'access', '["team"]', '["create"]', '["members.role","team_members"]', datetime('now')),
    ('team.memory.review', 'access', '["team"]', '["review"]', '["members.role","team_memory_policy_curators"]', datetime('now')),
    ('team.git.read', 'access', '["team"]', '["read"]', '["team_members"]', datetime('now')),
    ('team.git.write', 'access', '["team"]', '["update"]', '["team_members"]', datetime('now')),
    ('conversation.read', 'access', '["conversation"]', '["read"]', '["conversations.participants"]', datetime('now')),
    ('conversation.post', 'access', '["conversation"]', '["create"]', '["conversations.participants"]', datetime('now')),
    ('file.upload', 'access', '["file"]', '["upload"]', '["file_references"]', datetime('now')),
    ('file.download', 'access', '["file"]', '["download"]', '["file_references"]', datetime('now')),
    ('file.attach', 'access', '["file"]', '["attach"]', '["file_references"]', datetime('now')),
    ('agent.operate.self', 'access', '["agent"]', '["manage"]', '["agents.worker_id"]', datetime('now')),
    ('worker.capability.report', 'access', '["worker"]', '["report"]', '["admin_tokens.owner"]', datetime('now')),
    ('worker.heartbeat', 'access', '["worker"]', '["heartbeat"]', '["admin_tokens.owner"]', datetime('now')),
    ('worker.enroll', 'access', '["worker"]', '["create"]', '["admin_tokens.scopes_json"]', datetime('now')),
    ('dispatch.pull', 'access', '["worker"]', '["pull"]', '["admin_tokens.scopes_json"]', datetime('now')),
    ('task.internal.report', 'access', '["task"]', '["report"]', '["admin_tokens.scopes_json"]', datetime('now')),
    ('task.read', 'access', '["task"]', '["read"]', '["pm_project_members","pm_tasks.assignee"]', datetime('now')),
    ('task.write', 'access', '["task"]', '["update"]', '["pm_project_members"]', datetime('now')),
    ('task.complete.self', 'access', '["task"]', '["complete"]', '["pm_tasks.assignee"]', datetime('now')),
    ('task.block.self', 'access', '["task"]', '["block"]', '["pm_tasks.assignee"]', datetime('now')),
    ('issue.read', 'access', '["issue"]', '["read"]', '["pm_project_members"]', datetime('now')),
    ('issue.write', 'access', '["issue"]', '["update"]', '["pm_project_members"]', datetime('now')),
    ('plan.read', 'access', '["plan"]', '["read"]', '["pm_project_members"]', datetime('now')),
    ('plan.write', 'access', '["plan"]', '["update"]', '["pm_project_members"]', datetime('now')),
    ('coderepo.workspace.read', 'access', '["org"]', '["read"]', '["members"]', datetime('now')),
    ('coderepo.workspace.manage', 'access', '["org"]', '["manage"]', '["members.role"]', datetime('now')),
    ('coderepo.project_ref.read', 'access', '["project"]', '["read"]', '["pm_project_members"]', datetime('now')),
    ('ai_runtime.catalog.read', 'access', '["org"]', '["read"]', '["members"]', datetime('now')),
    ('ai_runtime.catalog.export', 'access', '["org"]', '["export"]', '["members"]', datetime('now')),
    ('ai_runtime.catalog.manage', 'access', '["org"]', '["manage"]', '["members.role"]', datetime('now')),
    ('model_catalog.manage', 'access', '["org"]', '["manage"]', '["agent_worker_binding"]', datetime('now')),
    ('secret.resolve', 'access', '["secret"]', '["read"]', '["admin_tokens.scopes_json"]', datetime('now')),
    ('blob.put', 'access', '["blob"]', '["put"]', '["admin_tokens.scopes_json"]', datetime('now')),
    ('admin_token.manage', 'access', '["admin_token"]', '["manage"]', '["admin_tokens.scopes_json"]', datetime('now')),
    ('git.global.read', 'access', '["git"]', '["read"]', '["system"]', datetime('now')),
    ('git.agent.read.self', 'access', '["agent"]', '["read"]', '["agents.identity_member_id"]', datetime('now')),
    ('git.agent.write.self', 'access', '["agent"]', '["update"]', '["agents.identity_member_id"]', datetime('now'));

INSERT OR IGNORE INTO authorization_roles
    (id, org_id, kind, name, description, created_by, created_at, updated_at)
VALUES
    ('sys-org-owner', '', 'system', 'org.owner', 'Legacy organization owner role projection', 'system', datetime('now'), datetime('now')),
    ('sys-org-admin', '', 'system', 'org.admin', 'Legacy organization admin role projection', 'system', datetime('now'), datetime('now')),
    ('sys-org-member', '', 'system', 'org.member', 'Legacy organization member role projection', 'system', datetime('now'), datetime('now')),
    ('sys-project-owner', '', 'system', 'project.owner', 'Legacy project owner role projection', 'system', datetime('now'), datetime('now')),
    ('sys-project-member', '', 'system', 'project.member', 'Legacy project member role projection', 'system', datetime('now'), datetime('now')),
    ('sys-team-web-owner', '', 'system', 'team.web.owner', 'Legacy Web team compatibility grants for organization owners', 'system', datetime('now'), datetime('now')),
    ('sys-team-web-admin', '', 'system', 'team.web.admin', 'Legacy Web team compatibility grants for organization admins', 'system', datetime('now'), datetime('now')),
    ('sys-team-web-member', '', 'system', 'team.web.member', 'Legacy Web team compatibility grants for organization members', 'system', datetime('now'), datetime('now')),
    ('sys-team-member', '', 'system', 'team.member', 'Legacy team member and compatibility grants', 'system', datetime('now'), datetime('now')),
    ('sys-admin-token', '', 'system', 'admin_token.scope', 'Legacy admin bearer-scope projection', 'system', datetime('now'), datetime('now'));

INSERT OR IGNORE INTO authorization_role_permissions
    (role_id, permission_key, resource_kind, delegatable, created_at)
VALUES
    ('sys-org-owner', 'org.read', 'org', 1, datetime('now')),
    ('sys-org-owner', 'org.settings.manage', 'org', 1, datetime('now')),
    ('sys-org-owner', 'org.lifecycle.manage', 'org', 1, datetime('now')),
    ('sys-org-owner', 'org.member.list', 'org', 1, datetime('now')),
    ('sys-org-owner', 'org.member.create.human', 'org', 1, datetime('now')),
    ('sys-org-owner', 'org.member.create.agent', 'org', 1, datetime('now')),
    ('sys-org-owner', 'org.member.role.manage', 'org', 1, datetime('now')),
    ('sys-org-owner', 'org.member.disable', 'org', 1, datetime('now')),
    ('sys-org-owner', 'org.invitation.manage', 'org', 1, datetime('now')),
    ('sys-org-owner', 'org.analytics.read', 'org', 1, datetime('now')),
    ('sys-org-owner', 'org.work_items.read', 'org', 1, datetime('now')),
    ('sys-org-owner', 'coderepo.workspace.read', 'org', 1, datetime('now')),
    ('sys-org-owner', 'coderepo.workspace.manage', 'org', 1, datetime('now')),
    ('sys-org-owner', 'ai_runtime.catalog.read', 'org', 1, datetime('now')),
    ('sys-org-owner', 'ai_runtime.catalog.export', 'org', 1, datetime('now')),
    ('sys-org-owner', 'ai_runtime.catalog.manage', 'org', 1, datetime('now')),
    ('sys-org-admin', 'org.read', 'org', 0, datetime('now')),
    ('sys-org-admin', 'org.member.list', 'org', 0, datetime('now')),
    ('sys-org-admin', 'org.member.create.human', 'org', 1, datetime('now')),
    ('sys-org-admin', 'org.member.create.agent', 'org', 1, datetime('now')),
    ('sys-org-admin', 'org.invitation.manage', 'org', 1, datetime('now')),
    ('sys-org-admin', 'org.analytics.read', 'org', 0, datetime('now')),
    ('sys-org-admin', 'org.work_items.read', 'org', 0, datetime('now')),
    ('sys-org-admin', 'coderepo.workspace.read', 'org', 0, datetime('now')),
    ('sys-org-admin', 'coderepo.workspace.manage', 'org', 0, datetime('now')),
    ('sys-org-admin', 'ai_runtime.catalog.read', 'org', 0, datetime('now')),
    ('sys-org-admin', 'ai_runtime.catalog.export', 'org', 0, datetime('now')),
    ('sys-org-admin', 'ai_runtime.catalog.manage', 'org', 0, datetime('now')),
    ('sys-org-member', 'org.read', 'org', 0, datetime('now')),
    ('sys-org-member', 'org.member.list', 'org', 0, datetime('now')),
    ('sys-org-member', 'org.work_items.read', 'org', 0, datetime('now')),
    ('sys-org-member', 'coderepo.workspace.read', 'org', 0, datetime('now')),
    ('sys-org-member', 'ai_runtime.catalog.read', 'org', 0, datetime('now')),
    ('sys-org-member', 'ai_runtime.catalog.export', 'org', 0, datetime('now')),
    ('sys-project-owner', 'project.read', 'project', 1, datetime('now')),
    ('sys-project-owner', 'project.write', 'project', 1, datetime('now')),
    ('sys-project-owner', 'project.member.add', 'project', 1, datetime('now')),
    ('sys-project-owner', 'project.member.remove', 'project', 1, datetime('now')),
    ('sys-project-owner', 'project.repo_ref.manage', 'project', 1, datetime('now')),
    ('sys-project-owner', 'project.stage.manage', 'project', 1, datetime('now')),
    ('sys-project-member', 'project.read', 'project', 0, datetime('now')),
    ('sys-project-member', 'project.write', 'project', 0, datetime('now')),
    ('sys-project-member', 'project.member.add', 'project', 0, datetime('now')),
    ('sys-project-member', 'project.repo_ref.manage', 'project', 0, datetime('now')),
    ('sys-team-web-owner', 'team.read', 'team', 1, datetime('now')),
    ('sys-team-web-owner', 'team.write', 'team', 1, datetime('now')),
    ('sys-team-web-owner', 'team.member.manage', 'team', 1, datetime('now')),
    ('sys-team-web-owner', 'team.project.link.manage', 'team', 1, datetime('now')),
    ('sys-team-web-owner', 'team.runtime_config.manage', 'team', 1, datetime('now')),
    ('sys-team-web-owner', 'team.memory.read', 'team', 1, datetime('now')),
    ('sys-team-web-owner', 'team.memory.propose', 'team', 1, datetime('now')),
    ('sys-team-web-owner', 'team.memory.review', 'team', 1, datetime('now')),
    ('sys-team-web-admin', 'team.read', 'team', 0, datetime('now')),
    ('sys-team-web-admin', 'team.write', 'team', 0, datetime('now')),
    ('sys-team-web-admin', 'team.member.manage', 'team', 0, datetime('now')),
    ('sys-team-web-admin', 'team.project.link.manage', 'team', 0, datetime('now')),
    ('sys-team-web-admin', 'team.runtime_config.manage', 'team', 0, datetime('now')),
    ('sys-team-web-admin', 'team.memory.read', 'team', 0, datetime('now')),
    ('sys-team-web-admin', 'team.memory.propose', 'team', 0, datetime('now')),
    ('sys-team-web-admin', 'team.memory.review', 'team', 0, datetime('now')),
    ('sys-team-web-member', 'team.read', 'team', 0, datetime('now')),
    ('sys-team-web-member', 'team.write', 'team', 0, datetime('now')),
    ('sys-team-web-member', 'team.member.manage', 'team', 0, datetime('now')),
    ('sys-team-web-member', 'team.project.link.manage', 'team', 0, datetime('now')),
    ('sys-team-web-member', 'team.runtime_config.manage', 'team', 0, datetime('now')),
    ('sys-team-web-member', 'team.memory.read', 'team', 0, datetime('now')),
    ('sys-team-member', 'team.memory.read', 'team', 0, datetime('now')),
    ('sys-team-member', 'team.memory.propose', 'team', 0, datetime('now')),
    ('sys-team-member', 'team.git.read', 'team', 0, datetime('now')),
    ('sys-team-member', 'team.git.write', 'team', 0, datetime('now')),
    ('sys-admin-token', 'admin_token.manage', 'admin_token', 0, datetime('now')),
    ('sys-admin-token', 'secret.resolve', 'secret', 0, datetime('now')),
    ('sys-admin-token', 'blob.put', 'blob', 0, datetime('now')),
    ('sys-admin-token', 'dispatch.pull', 'worker', 0, datetime('now')),
    ('sys-admin-token', 'task.internal.report', 'task', 0, datetime('now')),
    ('sys-admin-token', 'worker.enroll', 'worker', 0, datetime('now')),
    ('sys-admin-token', 'worker.heartbeat', 'worker', 0, datetime('now')),
    ('sys-admin-token', 'worker.capability.report', 'worker', 0, datetime('now'));
