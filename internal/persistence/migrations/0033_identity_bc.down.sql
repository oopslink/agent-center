-- 0033 down: remove BC9 Identity tables and restore v2.5 identities schema.
DROP TABLE IF EXISTS invitations;
DROP TABLE IF EXISTS members;
DROP TABLE IF EXISTS organizations;
DROP TABLE IF EXISTS identities;

-- Restore v2.5 Conversation BC identities table.
CREATE TABLE identities (
    id           TEXT PRIMARY KEY,
    kind         TEXT NOT NULL,
    display_name TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    version      INTEGER NOT NULL DEFAULT 1
);
