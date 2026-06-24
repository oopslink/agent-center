-- 0081_v2153_org_disabled.up.sql — v2.15.3 I41 (T470)
-- Org-login gate: a reversible "disabled" organization state, DISTINCT from the
-- soft-delete (deleted_at). A disabled org stays resolvable by slug so its owner
-- can still enter (full access) to manage / re-enable; the login gate
-- (requireOrgMember) blocks only NON-owner members. nil = enabled. Nullable TEXT
-- (RFC3339) mirroring deleted_at — every existing row reads NULL = enabled.
ALTER TABLE organizations ADD COLUMN disabled_at TEXT;
