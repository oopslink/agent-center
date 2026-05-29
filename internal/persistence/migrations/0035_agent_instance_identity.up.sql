-- 0035_agent_instance_identity.up.sql — v2.6 BE-7
--
-- Adds three columns to agent_instances (v2.6-design § 5.1 + § 5.5):
--
--   identity_id      TEXT NOT NULL — FK Identity; set at provisioning time.
--                    DEFAULT '' for SQLite ADD COLUMN compat (fresh install
--                    guarantee means no real rows with '' will persist).
--   organization_id  TEXT NOT NULL — FK Organization (app-layer enforced).
--   kind             TEXT NOT NULL — closed enum 'agent' only.
--                    Replaces the is_builtin boolean as the type discriminator;
--                    supervisor concept is cut in v2.6 (BE-9 drops is_builtin).
--
-- FK enforcement: identity_id references identities.id; enforced at app layer
-- (WorkforceBC is a Customer of IdentityBC per v2.6-design § 4.7).

ALTER TABLE agent_instances ADD COLUMN identity_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_instances ADD COLUMN organization_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_instances ADD COLUMN kind TEXT NOT NULL DEFAULT 'agent'
    CHECK (kind IN ('agent'));

-- Support org-scoped agent listing.
CREATE INDEX idx_agent_instances_org
    ON agent_instances (organization_id);

-- Support identity→agent lookup.
CREATE INDEX idx_agent_instances_identity
    ON agent_instances (identity_id)
    WHERE identity_id != '';
