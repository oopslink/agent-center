-- 0134_authz_s2_builtin_role_equivalence.up.sql
--
-- Completes the S2 built-in RAM role equivalence map used by authorization
-- shadow/enforce modes. Legacy membership tables remain authoritative; these
-- rows describe the equivalent built-in roles and do not copy memberships.

INSERT OR IGNORE INTO authorization_roles
    (id, org_id, kind, name, description, created_by, created_at, updated_at)
VALUES
    ('sys-team-web-owner', '', 'system', 'team.web.owner', 'Legacy Web team compatibility grants for organization owners', 'system', datetime('now'), datetime('now')),
    ('sys-team-web-admin', '', 'system', 'team.web.admin', 'Legacy Web team compatibility grants for organization admins', 'system', datetime('now'), datetime('now')),
    ('sys-team-web-member', '', 'system', 'team.web.member', 'Legacy Web team compatibility grants for organization members', 'system', datetime('now'), datetime('now'));

INSERT OR IGNORE INTO authorization_role_permissions
    (role_id, permission_key, resource_kind, delegatable, created_at)
VALUES
    ('sys-org-owner', 'coderepo.workspace.read', 'org', 1, datetime('now')),
    ('sys-org-owner', 'ai_runtime.catalog.read', 'org', 1, datetime('now')),
    ('sys-org-owner', 'ai_runtime.catalog.export', 'org', 1, datetime('now')),
    ('sys-org-admin', 'coderepo.workspace.read', 'org', 0, datetime('now')),
    ('sys-org-admin', 'ai_runtime.catalog.read', 'org', 0, datetime('now')),
    ('sys-org-admin', 'ai_runtime.catalog.export', 'org', 0, datetime('now')),
    ('sys-org-member', 'coderepo.workspace.read', 'org', 0, datetime('now')),
    ('sys-org-member', 'ai_runtime.catalog.read', 'org', 0, datetime('now')),
    ('sys-org-member', 'ai_runtime.catalog.export', 'org', 0, datetime('now')),
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
    ('sys-team-web-member', 'team.memory.read', 'team', 0, datetime('now'));
