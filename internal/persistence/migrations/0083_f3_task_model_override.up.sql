-- 0083_f3_task_model_override.up.sql — F3 model routing & profile config
-- (design §5 & §10). Add the per-task hard-override executor model: a caller may
-- pin a task to a specific executor model at create time. Nullable TEXT (NULL/empty
-- = unset → fall back to the agent's allowed/default executor model selection).
ALTER TABLE pm_tasks ADD COLUMN model TEXT;
