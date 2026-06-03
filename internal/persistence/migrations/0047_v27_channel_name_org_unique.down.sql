-- 0047_v27_channel_name_org_unique.down.sql — revert to global channel-name uniqueness.
--
-- NOTE: recreating the global UNIQUE(name) fails if org-scoped data introduced
-- duplicate channel names across organizations while 0047 was applied — that is
-- the expected down-migration constraint (the data now relies on the org-scoped
-- guarantee). Resolve such duplicates before reverting.

DROP INDEX IF EXISTS uniq_conversations_channel_name_org;

CREATE UNIQUE INDEX uniq_conversations_channel_name
    ON conversations (name)
    WHERE kind = 'channel' AND name IS NOT NULL;
