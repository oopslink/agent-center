-- 0082_f3_agent_model_routing.up.sql — F3 model routing & profile config
-- (design §5 & §10). Add the agent model-routing profile fields, carried the SAME
-- way as the T236 reasoning/mode/provider columns:
--   orchestrator_model     — the orchestrator's own model (cheap/fast tier);
--                            nullable TEXT, NULL/empty = center default.
--   default_executor_model — fallback executor model; nullable TEXT, NULL = default.
--   max_concurrent_tasks   — executor concurrency cap; INTEGER NOT NULL DEFAULT 3
--                            (existing rows backfill to 3; 0 is treated as the
--                            EffectiveMaxConcurrentTasks default in the domain).
--   allowed_models         — candidate executor models as a JSON array of strings,
--                            NOT NULL DEFAULT '[]' (mirrors capability_tags/skills).
ALTER TABLE agents ADD COLUMN orchestrator_model TEXT;
ALTER TABLE agents ADD COLUMN default_executor_model TEXT;
ALTER TABLE agents ADD COLUMN max_concurrent_tasks INTEGER NOT NULL DEFAULT 3;
ALTER TABLE agents ADD COLUMN allowed_models TEXT NOT NULL DEFAULT '[]';
