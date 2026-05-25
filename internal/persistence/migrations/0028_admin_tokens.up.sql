-- 0028: admin_tokens — bearer tokens for the admin endpoint (task #28,
-- v2.3-3a). The server stores only the sha256(plaintext); plaintext is
-- shown once at creation. Lookup path is hash-indexed.
--
-- scopes_json is a JSON array of free-form scope strings (e.g.
-- ["secret:resolve","blob:put"] or ["*"] for superuser). We use JSON
-- rather than a join table because scopes are immutable once issued
-- (revoke + recreate to change scopes) — there's no benefit to a row
-- per scope.
--
-- value_hash uses BLOB (32 raw bytes) so we don't double-encode hex/b64.

CREATE TABLE admin_tokens (
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

CREATE INDEX idx_admin_tokens_owner ON admin_tokens (owner);
CREATE INDEX idx_admin_tokens_revoked ON admin_tokens (revoked_at);
