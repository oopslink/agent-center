-- 0064_agent_llm_config.up.sql — T236: make the agent LLM tuning fields
-- (reasoning / mode / provider) real, persisted, editable config instead of the
-- hardcoded "Medium/Default/Default" placeholders the AgentDetail page showed.
-- Nullable TEXT (empty/NULL = the runtime/center default), carried alongside the
-- existing model/cli columns. ALTER so a dev DB picks up the columns without a
-- full agents-table rebuild; existing rows backfill to NULL (= default).
ALTER TABLE agents ADD COLUMN reasoning TEXT;
ALTER TABLE agents ADD COLUMN mode TEXT;
ALTER TABLE agents ADD COLUMN provider TEXT;
