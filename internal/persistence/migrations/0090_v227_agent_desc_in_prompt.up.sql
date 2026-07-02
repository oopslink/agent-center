-- T728 (issue-0619f315, v2.27.0): per-agent switch controlling whether the agent's
-- `description` is injected into its system prompt.
--
--   agents.include_description_in_system_prompt — DEFAULT 1 (inject): the feature
--       default is ON (decision: "默认设置到系统提示词"), so an existing agent keeps
--       the intended default behaviour — when its description is non-empty it is
--       injected — unless its owner explicitly opts out (sets 0).
--
-- UPGRADE SAFETY: additive ADD COLUMN only (NOT NULL with a constant DEFAULT) — no
-- row is touched, no data migration. Existing agents read
-- include_description_in_system_prompt = 1 (inject), preserving current-behaviour
-- semantics (the description feeds the persona段 by default).
--
-- DIALECT NOTE: ADD COLUMN with a constant DEFAULT and DROP COLUMN (down) are in the
-- SQLite (>=3.35) + PG common subset (mirrors 0086 auto_assignable).

ALTER TABLE agents ADD COLUMN include_description_in_system_prompt INTEGER NOT NULL DEFAULT 1;
