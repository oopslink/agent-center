-- 0130_authorization_revoke_previews.up.sql -- persisted two-phase revoke previews.

CREATE TABLE IF NOT EXISTS authorization_revoke_previews (
    preview_id      TEXT PRIMARY KEY,
    token_hash      TEXT NOT NULL,
    actor_ref       TEXT NOT NULL,
    org_id          TEXT NOT NULL,
    subject_hash    TEXT NOT NULL,
    request_hash    TEXT NOT NULL,
    request_json    TEXT NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('pending', 'confirmed')),
    created_at      TEXT NOT NULL,
    expires_at      TEXT NOT NULL,
    confirmed_at    TEXT
);

CREATE INDEX IF NOT EXISTS idx_authorization_revoke_previews_actor_org
    ON authorization_revoke_previews(actor_ref, org_id, status, expires_at);
