-- 0073_v214_drop_agent_work_items.down.sql — reverse of the F7 drop.
--
-- Recreate the legacy tables in their final pre-drop shape so the migrator
-- round-trip stays reversible. agent_work_items is restored with the indexes from
-- 0043 PLUS the UNIQUE idx_awi_agent_active that 0051 swapped in (the table's last
-- state before this drop); agent_work_item_projections is restored from 0046. The
-- tables come back EMPTY — no data is reconstructed (the rows were retired by the
-- F4 0071 backfill into pm_tasks). agent_activity_events is untouched (never
-- dropped).

CREATE TABLE agent_work_items (
    id            TEXT PRIMARY KEY,
    agent_id      TEXT NOT NULL,
    task_ref      TEXT NOT NULL,
    status        TEXT NOT NULL,
    interactions  INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    version       INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_awi_agent ON agent_work_items (agent_id);
CREATE INDEX idx_awi_task  ON agent_work_items (task_ref);
-- 0051's UNIQUE replacement of the original non-unique idx_awi_agent_active.
CREATE UNIQUE INDEX idx_awi_agent_active
    ON agent_work_items (agent_id)
    WHERE status IN ('active', 'waiting_input');

CREATE TABLE agent_work_item_projections (
    work_item_id                 TEXT PRIMARY KEY,
    agent_id                     TEXT NOT NULL,
    status                       TEXT NOT NULL DEFAULT '',
    current_activity             TEXT,
    current_activity_at          TEXT,
    total_tool_calls             INTEGER NOT NULL DEFAULT 0,
    total_tokens_input           INTEGER NOT NULL DEFAULT 0,
    total_tokens_output          INTEGER NOT NULL DEFAULT 0,
    working_seconds_accumulated  INTEGER NOT NULL DEFAULT 0,
    last_activity_at             TEXT NOT NULL
);
CREATE INDEX idx_awip_agent       ON agent_work_item_projections (agent_id);
CREATE INDEX idx_awip_status      ON agent_work_item_projections (status);
CREATE INDEX idx_awip_last_active ON agent_work_item_projections (last_activity_at DESC);
