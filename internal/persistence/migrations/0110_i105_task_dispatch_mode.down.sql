-- 0110_i105_task_dispatch_mode.down.sql — reverse the I105 per-node fork override.
-- The column is additive with a safe default ('' = executor_fork = the pre-I105
-- routing), so dropping it reverts every task to the per-AGENT fork decision. Any
-- 'supervisor_inline' marks are lost, which means those nodes fork again — the
-- pre-I105 behavior, caught by the N4 writeback net. No data restore is needed.
ALTER TABLE pm_tasks DROP COLUMN dispatch_mode;
