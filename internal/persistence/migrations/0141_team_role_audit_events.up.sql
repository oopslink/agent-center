-- 0141_team_role_audit_events.up.sql -- Team Role CRUD audit stream.

CREATE TABLE IF NOT EXISTS team_role_audit_events (
    id              TEXT PRIMARY KEY,
    org_id          TEXT NOT NULL,
    team_id         TEXT NOT NULL,
    team_role       TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    actor_ref       TEXT NOT NULL,
    previous_config TEXT NOT NULL DEFAULT '{}',
    next_config     TEXT NOT NULL DEFAULT '{}',
    affected_members INTEGER NOT NULL DEFAULT 0,
    affected_projects INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_team_role_audit_lookup
    ON team_role_audit_events(team_id, team_role, created_at DESC);
