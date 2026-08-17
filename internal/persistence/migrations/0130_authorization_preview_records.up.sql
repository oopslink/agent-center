CREATE TABLE IF NOT EXISTS authorization_preview_records (
    preview_id             TEXT PRIMARY KEY,
    actor_ref              TEXT NOT NULL,
    org_id                 TEXT NOT NULL,
    operation              TEXT NOT NULL,
    normalized_request_json TEXT NOT NULL,
    request_hash           TEXT NOT NULL,
    result_hash            TEXT NOT NULL,
    revision_hash          TEXT NOT NULL,
    status                 TEXT NOT NULL CHECK (status IN ('pending', 'applied', 'expired')),
    expires_at             TEXT NOT NULL,
    created_at             TEXT NOT NULL,
    applied_at             TEXT,
    apply_idempotency_key  TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_authorization_preview_actor_org
    ON authorization_preview_records(actor_ref, org_id, created_at);
