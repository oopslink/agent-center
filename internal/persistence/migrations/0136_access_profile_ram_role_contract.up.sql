-- 0136_access_profile_ram_role_contract.up.sql
--
-- The Team UI exposes access profile ids as RAM role ids for preview/PUT while
-- create/update persist portable RAM role keys by name. Mirror the built-in
-- access profiles as system authorization roles so both contracts resolve to
-- the same production authorization rows.

INSERT OR IGNORE INTO authorization_roles
    (id, org_id, kind, name, description, created_by, created_at, updated_at)
VALUES
    ('team-basic', '', 'system', 'Team basic', 'Read team metadata and memory.', 'system', datetime('now'), datetime('now')),
    ('team-contributor', '', 'system', 'Team contributor', 'Read/write team work and propose memory.', 'system', datetime('now'), datetime('now')),
    ('team-curator', '', 'system', 'Team curator', 'Review team memory in addition to contributor access.', 'system', datetime('now'), datetime('now'));

INSERT OR IGNORE INTO authorization_role_permissions
    (role_id, permission_key, resource_kind, delegatable, created_at)
VALUES
    ('team-basic', 'team.read', 'team', 0, datetime('now')),
    ('team-basic', 'team.memory.read', 'team', 0, datetime('now')),
    ('team-contributor', 'team.read', 'team', 0, datetime('now')),
    ('team-contributor', 'team.write', 'team', 0, datetime('now')),
    ('team-contributor', 'team.memory.read', 'team', 0, datetime('now')),
    ('team-contributor', 'team.memory.propose', 'team', 0, datetime('now')),
    ('team-curator', 'team.read', 'team', 0, datetime('now')),
    ('team-curator', 'team.write', 'team', 0, datetime('now')),
    ('team-curator', 'team.memory.read', 'team', 0, datetime('now')),
    ('team-curator', 'team.memory.propose', 'team', 0, datetime('now')),
    ('team-curator', 'team.memory.review', 'team', 0, datetime('now'));
