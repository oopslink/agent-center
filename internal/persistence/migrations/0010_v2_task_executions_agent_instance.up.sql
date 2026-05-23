-- 0010_v2_task_executions_agent_instance.up.sql — Phase 8 (v2) TaskExecution agent_instance_id 字段
--
-- 覆盖 ADR-0024 § 6：TaskExecution.agent_instance_id 字段
--
-- 本 phase 仅加列；dispatch 路径仍走 agent_cli（v1 兼容）。
-- Phase 9 dispatch 重写后切到 agent_instance_id；届时新 migration drop agent_cli 列。
--
-- 字段 NULLABLE 因为：
--   - v1 已有 task_execution 行（开发 / 测试 DB）没 agent_instance_id
--   - P9 dispatch 切换前新建 task_execution 也可能没 agent_instance_id
--   - 应用层强制：v2 dispatch 必须设 agent_instance_id（P9 落）

ALTER TABLE task_executions ADD COLUMN agent_instance_id TEXT;

-- 反查同 agent 的 active executions（dispatch concurrency 校验 + LifecycleService）
CREATE INDEX idx_task_executions_agent_instance ON task_executions (agent_instance_id, status)
    WHERE agent_instance_id IS NOT NULL;
