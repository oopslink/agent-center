-- 0085_v2182_agent_allowed_executors.up.sql — v2.18.2 BE-1 (issue-8746a5b9).
--
-- Upgrade the executor-candidate config from the model-only allowed_models to the
-- authoritative allowed_executors = JSON array of {cli, model}. An executor need not
-- share the orchestrator's CLI (a claude-code supervisor may dispatch a codex
-- executor), which a []string of model names cannot express. allowed_executors
-- becomes the authoritative list the concurrency opt-in predicate
-- (agent.Profile.ConcurrencyEnabled) and the center ≤N cap read; the legacy
-- allowed_models column is RETAINED but DEPRECATED — going forward the app writes it
-- as a derived mirror (the distinct models of allowed_executors) so the F3 model
-- router, which still reads model-only candidates until BE-2 migrates routing to
-- {cli, model}, keeps working.
--
-- UPGRADE SAFETY: additive ADD COLUMN (NOT NULL DEFAULT '[]') + a one-shot backfill
-- — no row is dropped, no other column is touched. Existing rows' allowed_models are
-- mapped element-wise to {cli: the agent's OWN cli (agents.cli; empty → claude-code),
-- model: <m>}; a row with empty/'[]' allowed_models keeps the column default '[]'.
-- The opt-in gate is unchanged in effect: an agent enabled before (max>0 AND a
-- non-empty allowed_models) stays enabled (its backfilled allowed_executors is
-- non-empty); a default agent stays single-active (empty list → effective cap 1).
--
-- DIALECT NOTE: ADD COLUMN, the json1 functions (json_each / json_group_array /
-- json_object / json_valid / json_array_length), and DROP COLUMN (down) are all in
-- the SQLite (>=3.35) + PG common subset.

ALTER TABLE agents ADD COLUMN allowed_executors TEXT NOT NULL DEFAULT '[]';

UPDATE agents
SET allowed_executors = (
    SELECT json_group_array(
        json_object('cli', COALESCE(NULLIF(agents.cli, ''), 'claude-code'), 'model', je.value)
    )
    FROM json_each(agents.allowed_models) AS je
)
WHERE allowed_models IS NOT NULL
  AND allowed_models <> ''
  AND json_valid(allowed_models)
  AND json_array_length(allowed_models) > 0;
