-- 0034_organization_scope.up.sql — v2.6 § 5.1 Organization-scoped entities
--
-- Adds organization_id to the four top-level entity tables that represent
-- Organization-owned resources (v2.6-design § 5.1):
--   conversations (channels), projects, workers, user_secrets
--
-- SQLite constraint: ALTER TABLE ADD COLUMN requires a DEFAULT when NOT NULL.
-- DEFAULT '' is used here as the SQLite-level sentinel; application layer
-- ensures a valid Organization.id is always written on insert (conventions § 9.w).
--
-- FK to organizations.id is enforced at the application layer (see
-- IdentityBCFacade.GetActiveOrganization) consistent with other soft-FK
-- columns in this schema.  A logical FK comment is added for documentation.
--
-- Fresh install required for v2.6; no data migration is performed.
-- Existing rows (if any) will have organization_id = '' (empty string),
-- which is an invalid Organization id and will surface as a bug if found.

-- ---- conversations (Conversation BC channels / DMs / threads) ---------------

ALTER TABLE conversations ADD COLUMN organization_id TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_conversations_org
    ON conversations (organization_id);

-- ---- projects (Workforce BC) -------------------------------------------------

ALTER TABLE projects ADD COLUMN organization_id TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_projects_org
    ON projects (organization_id);

-- ---- workers (Workforce BC) --------------------------------------------------

ALTER TABLE workers ADD COLUMN organization_id TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_workers_org
    ON workers (organization_id);

-- ---- user_secrets (SecretManagement BC) -------------------------------------

ALTER TABLE user_secrets ADD COLUMN organization_id TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_user_secrets_org
    ON user_secrets (organization_id);
