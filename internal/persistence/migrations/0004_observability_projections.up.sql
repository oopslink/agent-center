-- 0004_observability_projections.up.sql — Phase 4 Observability projections
--
-- 落盘 Observability BC 独占物理表（conventions § 9.z BC 物理隔离）：
--   - Observability: task_execution_projections (与 TaskRuntime task_executions PK 1:1，但物理独占)
--
-- 元层规则按 02-persistence-schema § 1-7 + § 8.2.2：
--   - PK = task_execution_id（也是引用 task_executions.id；引用完整性由应用层 Repository 负责）
--   - 不声明 FOREIGN KEY（conventions § 9.w）
--   - 不带 version 列；UPSERT 路径靠 last_push_at 做 staleness 防护
--   - TEXT 存 ISO8601 时间戳
--
-- 字段对位 02-persistence-schema § 8.2.1 + observability/00-overview § 1.4。
-- worker daemon push 路径走 TaskExecutionProjectionService.UpdateProjection，
-- 完全不触碰 task_executions 主表（§ 9.z 物理隔离）。

CREATE TABLE task_execution_projections (
    task_execution_id              TEXT PRIMARY KEY,            -- = task_executions.id (app-layer integrity)
    current_activity               TEXT,                        -- 人话描述，e.g. "正在分析 src/foo.go"
    current_activity_at            TEXT,                        -- 该 activity 切到时刻
    total_tool_calls               INTEGER NOT NULL DEFAULT 0,
    total_tokens_input             INTEGER NOT NULL DEFAULT 0,
    total_tokens_output            INTEGER NOT NULL DEFAULT 0,
    working_seconds_accumulated    INTEGER NOT NULL DEFAULT 0,
    last_push_at                   TEXT NOT NULL                -- last successful UPSERT timestamp
);
CREATE INDEX idx_proj_last_push ON task_execution_projections (last_push_at DESC);
