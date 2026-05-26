-- 0029: v2.4-D-A3 (task #37) — extend admin_tokens with enroll-token
-- semantics: short TTL + one-time-use, distinct lifecycle from v2.3-3a
-- long-term bearer tokens.
--
-- is_enroll = 1 marks the row as a worker-enrollment bootstrap token
-- (mint via /admin/admintoken/mint-enroll, burn on first verify).
-- v2.3-3a long-term tokens leave is_enroll = 0; their semantics unchanged.
--
-- expires_at + used_at are nullable: long-term tokens get both NULL,
-- enroll tokens get an expires_at and (after consumption) a used_at.

ALTER TABLE admin_tokens ADD COLUMN is_enroll INTEGER NOT NULL DEFAULT 0;
ALTER TABLE admin_tokens ADD COLUMN expires_at TEXT;
ALTER TABLE admin_tokens ADD COLUMN used_at TEXT;

-- Index supports the "find pending enroll tokens" + "expire sweep"
-- queries (admin_tokens is small; index is cheap).
CREATE INDEX idx_admin_tokens_enroll_expires
  ON admin_tokens (is_enroll, expires_at)
  WHERE is_enroll = 1;
