-- 0046_v27_agent_work_item_projections.up.sql — v2.7 #107 part-2 Phase-1 (task #111)
--
-- The new-model equivalent of the old observability `task_execution_projections`
-- (mig 0004), which was fed by the retired taskruntime execution flow. This one is
-- a read-model over the AGENT model: per agent work-item, the aggregated execution
-- view observability/fleet needs — current activity, tool-call count, token totals,
-- accumulated working seconds — derived from the append-only `agent_activity_events`
-- stream (claude stream-json events: assistant_text/thinking/tool_use/result/...).
--
-- PK is the work-item id (1:1 with agent_work_items.id, app-layer integrity, same
-- pattern as the old projection's 1:1 with task_executions.id). agent_id + status are
-- denormalized so the fleet snapshot can filter/aggregate without a join. Fed
-- incrementally as activity events are appended (the new-model analog of the old
-- worker-pushed ProjectionUpdate); idempotent upsert.

CREATE TABLE agent_work_item_projections (
    work_item_id                 TEXT PRIMARY KEY,            -- = agent_work_items.id
    agent_id                     TEXT NOT NULL,               -- denormalized (fleet-by-agent without join)
    status                       TEXT NOT NULL DEFAULT '',    -- execution/work-item status (active/waiting_input/done/failed/...) — the fleet "execution status"
    current_activity             TEXT,                        -- human-readable latest activity (from newest assistant_text/thinking/tool_use)
    current_activity_at          TEXT,                        -- when current_activity was set
    total_tool_calls             INTEGER NOT NULL DEFAULT 0,  -- count of tool_use events
    total_tokens_input           INTEGER NOT NULL DEFAULT 0,  -- summed from result events' usage
    total_tokens_output          INTEGER NOT NULL DEFAULT 0,
    working_seconds_accumulated  INTEGER NOT NULL DEFAULT 0,  -- summed turn durations (from result events)
    last_activity_at             TEXT NOT NULL                -- newest event occurred_at (the new "last_push_at")
);
CREATE INDEX idx_awip_agent       ON agent_work_item_projections (agent_id);
CREATE INDEX idx_awip_status      ON agent_work_item_projections (status);
CREATE INDEX idx_awip_last_active ON agent_work_item_projections (last_activity_at DESC);
