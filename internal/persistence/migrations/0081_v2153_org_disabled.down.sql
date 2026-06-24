-- 0081_v2153_org_disabled.down.sql — reverse of the up migration.
ALTER TABLE organizations DROP COLUMN disabled_at;
