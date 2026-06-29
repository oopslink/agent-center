-- 0086_v2183_auto_assign_schema.up.sql — v2.18.3 BE-1 (issue-577a7b0e).
--
-- Data base for the auto-assign reconciler (BE-2): the fields the matcher reads to
-- decide which claimable pool task may be auto-assigned to which idle, eligible
-- agent. This migration adds ONLY the columns; the reconciler logic is BE-2.
--
--   pm_tasks.required_capabilities  — JSON string array (canonical: trimmed,
--       lowercased, deduped) of capability labels a task demands. Empty '[]' =
--       unrestricted (any eligible agent may take it). Matched against an agent's
--       capability_tags by BE-2.
--   agents.auto_assignable          — per-agent opt-OUT flag. DEFAULT 1 (assignable):
--       since the project master switch defaults ON (decision 1), an agent is
--       auto-assignable unless its owner explicitly opts it out (sets 0).
--
-- The PROJECT-level master switch `auto_assign_enabled` is NOT a column — it lives
-- in the center settings key/value store under `auto_assign.enabled.<project_id>`
-- (absent ⇒ ON, decision 1), following the wake.* settings convention. No schema
-- change is needed for it.
--
-- UPGRADE SAFETY: additive ADD COLUMN only (both NOT NULL with a DEFAULT) — no row
-- is touched, no data migration. Existing tasks read required_capabilities '[]'
-- (unrestricted) and existing agents read auto_assignable 1 (assignable), i.e. the
-- reconciler (once BE-2 lands) treats all current work/agents as eligible — the
-- intended default-ON behaviour.
--
-- DIALECT NOTE: ADD COLUMN with a constant DEFAULT and DROP COLUMN (down) are in
-- the SQLite (>=3.35) + PG common subset.

ALTER TABLE pm_tasks ADD COLUMN required_capabilities TEXT NOT NULL DEFAULT '[]';
ALTER TABLE agents ADD COLUMN auto_assignable INTEGER NOT NULL DEFAULT 1;
