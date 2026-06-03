-- 0047_v27_channel_name_org_unique.up.sql — v2.7 #195 (@oopslink)
--
-- Channel name uniqueness becomes ORG-SCOPED: composite UNIQUE(organization_id,
-- name) instead of the global UNIQUE(name) from 0020. Two organizations may each
-- have a "general" channel; one organization may not have two. This is the
-- prerequisite for the v2.8 #190 `#<name>` cross-reference syntax (an org-scoped
-- name needs an org-scoped uniqueness guarantee).
--
-- Partial index (same predicate as the old one): only kind='channel' rows with a
-- non-null name participate — DM/task conversations (name nullable) are unaffected.
-- conversations.organization_id is NOT NULL DEFAULT '' (mig 0034), so legacy
-- admin-created channels with empty org collapse into the '' org bucket (still
-- unique among themselves, matching the prior global guarantee for that bucket).

DROP INDEX IF EXISTS uniq_conversations_channel_name;

CREATE UNIQUE INDEX uniq_conversations_channel_name_org
    ON conversations (organization_id, name)
    WHERE kind = 'channel' AND name IS NOT NULL;
