-- 0104_model_catalog.up.sql — org-level user-managed model catalog (issue-93dd8daa
-- phase ①). The single source of truth for the models an org's agents may run,
-- mirroring pm_templates (0094): NO built-in rows, all user/agent managed. `tier`
-- is FREE TEXT (a capability/fit description the phase-② difficulty judge matches
-- semantically). model_id is unique within an org.
--
-- Shape mirrors pm_templates: TEXT surrogate PK, org_id dimension, created_by +
-- created_at/updated_at (RFC3339 TEXT) + version. costs are REAL (per-MTok price),
-- context_window INTEGER — the SQLite + PG common subset.
--
-- UPGRADE SAFETY: purely additive — a new empty table + two indexes, nothing else
-- touched. Empty until a user/agent creates entries = 零回归.
CREATE TABLE IF NOT EXISTS pm_model_catalog (
    id             TEXT PRIMARY KEY,
    org_id         TEXT NOT NULL DEFAULT '',
    model_id       TEXT NOT NULL,
    display_name   TEXT NOT NULL DEFAULT '',
    input_cost     REAL NOT NULL DEFAULT 0,
    output_cost    REAL NOT NULL DEFAULT 0,
    context_window INTEGER NOT NULL DEFAULT 0,
    tier           TEXT NOT NULL DEFAULT '',
    created_by     TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    version        INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_pm_model_catalog_org ON pm_model_catalog(org_id);
-- model_id unique per org (the org-scoped business key; import/create rejects dups).
CREATE UNIQUE INDEX IF NOT EXISTS idx_pm_model_catalog_org_model ON pm_model_catalog(org_id, model_id);
