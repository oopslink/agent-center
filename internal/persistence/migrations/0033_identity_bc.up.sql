-- 0033: v2.6 Identity BC (BC9) — Identity / Organization / Member / Invitation tables.
--
-- Introduces BC9 Identity as a standalone bounded context (ADR-0040..0045).
-- ID formats:
--   Identity     → "user-<8hex>" | "agent-<8hex>"
--   Organization → "organization-<8hex>"
--   Member       → "mem-<8hex>"
--   Invitation   → "inv-<8hex>"
--
-- Drops the old identities table from Conversation BC (v2.5 schema) and
-- recreates it with the v2.6 schema. All dependent Conversation BC references
-- (messages.sender_identity_id) keep the column name but now reference the
-- new identities table.
--
-- Per v2.6-design § 9.1: no migration of existing data; fresh install required.

-- =========================================================================
-- Drop v2.5 identities table (Conversation BC era)
-- =========================================================================
DROP TABLE IF EXISTS identities;

-- =========================================================================
-- BC9 Identity — identities
-- =========================================================================
CREATE TABLE identities (
    id               TEXT PRIMARY KEY,                      -- "user-<8hex>" | "agent-<8hex>"
    kind             TEXT NOT NULL CHECK (kind IN ('user', 'agent')),
    display_name     TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    account_status   TEXT NOT NULL DEFAULT 'active' CHECK (account_status IN ('active', 'disabled')),
    passcode_hash    TEXT NOT NULL DEFAULT '',             -- argon2id hash; empty for agent
    passcode_set_at  TEXT,                                 -- RFC3339Nano; NULL for agent
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);
CREATE UNIQUE INDEX uniq_identities_display_name_user
    ON identities (display_name) WHERE kind = 'user';
CREATE INDEX idx_identities_kind_status ON identities (kind, account_status);

-- =========================================================================
-- BC9 Identity — organizations
-- =========================================================================
CREATE TABLE organizations (
    id                      TEXT PRIMARY KEY,               -- "organization-<8hex>"
    slug                    TEXT NOT NULL,                  -- URL key; [a-z0-9-]{3,40}
    name                    TEXT NOT NULL,
    description             TEXT NOT NULL DEFAULT '',
    created_by_identity_id  TEXT NOT NULL,
    created_at              TEXT NOT NULL,
    updated_at              TEXT NOT NULL,
    deleted_at              TEXT                            -- NULL = active; set = soft-deleted
);
CREATE UNIQUE INDEX uniq_organizations_slug
    ON organizations (slug) WHERE deleted_at IS NULL;
CREATE INDEX idx_organizations_created_by ON organizations (created_by_identity_id);

-- =========================================================================
-- BC9 Identity — members
-- =========================================================================
CREATE TABLE members (
    id                       TEXT PRIMARY KEY,              -- "mem-<8hex>"
    organization_id          TEXT NOT NULL,
    identity_id              TEXT NOT NULL,
    role                     TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    status                   TEXT NOT NULL DEFAULT 'joined' CHECK (status IN ('joined', 'disabled')),
    joined_at                TEXT NOT NULL,
    invited_by_identity_id   TEXT,                         -- NULL for signup-bootstrap owner
    invited_at               TEXT,
    disabled_at              TEXT,
    disabled_reason          TEXT NOT NULL DEFAULT ''
);
-- M1: (organization_id, identity_id) unique among joined members
CREATE UNIQUE INDEX uniq_members_org_identity_joined
    ON members (organization_id, identity_id) WHERE status = 'joined';
CREATE INDEX idx_members_identity_status ON members (identity_id, status);
CREATE INDEX idx_members_org_role        ON members (organization_id, role);

-- =========================================================================
-- BC9 Identity — invitations (v2.7 flow; v2.6 schema placeholder only)
-- =========================================================================
CREATE TABLE invitations (
    id                       TEXT PRIMARY KEY,              -- "inv-<8hex>"
    organization_id          TEXT NOT NULL,
    invitee_handle           TEXT NOT NULL,
    role_to_grant            TEXT NOT NULL CHECK (role_to_grant IN ('owner', 'admin', 'member')),
    invited_by_identity_id   TEXT NOT NULL,
    status                   TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'expired', 'revoked')),
    token                    TEXT NOT NULL,                 -- 64-char hex; one-time use
    created_at               TEXT NOT NULL,
    expires_at               TEXT NOT NULL,
    accepted_by_identity_id  TEXT,
    accepted_at              TEXT
);
CREATE UNIQUE INDEX uniq_invitations_token ON invitations (token);
CREATE INDEX idx_invitations_org_status   ON invitations (organization_id, status);
