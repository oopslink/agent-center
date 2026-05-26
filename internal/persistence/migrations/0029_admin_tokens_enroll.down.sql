DROP INDEX IF EXISTS idx_admin_tokens_enroll_expires;

-- SQLite doesn't support DROP COLUMN before 3.35; we use the table
-- recreation pattern. The down migration recreates the original
-- v2.3-3a (migration 0028) shape.

CREATE TABLE admin_tokens_v0028 (
  id              TEXT NOT NULL PRIMARY KEY,
  owner           TEXT NOT NULL,
  scopes_json     TEXT NOT NULL,
  value_hash      BLOB NOT NULL UNIQUE,
  created_at      TEXT NOT NULL,
  created_by      TEXT NOT NULL,
  revoked_at      TEXT,
  revoked_by      TEXT,
  revoked_reason  TEXT,
  last_used_at    TEXT,
  version         INTEGER NOT NULL DEFAULT 1
);

INSERT INTO admin_tokens_v0028 (id, owner, scopes_json, value_hash, created_at, created_by, revoked_at, revoked_by, revoked_reason, last_used_at, version)
SELECT id, owner, scopes_json, value_hash, created_at, created_by, revoked_at, revoked_by, revoked_reason, last_used_at, version FROM admin_tokens;

DROP TABLE admin_tokens;
ALTER TABLE admin_tokens_v0028 RENAME TO admin_tokens;

CREATE INDEX idx_admin_tokens_owner ON admin_tokens (owner);
CREATE INDEX idx_admin_tokens_revoked ON admin_tokens (revoked_at);
