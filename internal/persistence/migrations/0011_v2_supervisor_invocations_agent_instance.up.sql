-- 0011_v2_supervisor_invocations_agent_instance.up.sql — Phase 8 (v2) SupervisorInvocation agent_instance_id 字段
--
-- 覆盖 ADR-0029 § 1 + P8 plan § 3.6：
--   - SupervisorInvocation 加 agent_instance_id 字段，强引用 built-in supervisor AgentInstance
--
-- 本 phase 加列 + backfill 由应用层（InvocationFactory）保证新 invocation 必填。
-- 历史行的 backfill 在 0012 migration / 应用层启动 sweep 中处理（指向 built-in supervisor）。

ALTER TABLE supervisor_invocations ADD COLUMN agent_instance_id TEXT;

-- 反查同 supervisor agent 的 invocations
CREATE INDEX idx_supervisor_invocations_agent_instance
    ON supervisor_invocations (agent_instance_id, status)
    WHERE agent_instance_id IS NOT NULL;
