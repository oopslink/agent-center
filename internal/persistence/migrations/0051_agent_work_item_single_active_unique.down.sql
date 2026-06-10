-- 0051_agent_work_item_single_active_unique.down.sql — revert to the v2.7 #100
-- non-unique partial index shape. (Demoted rows are not restored — the down
-- only reverts the index; the demotion was a one-time data cleanup.)
DROP INDEX IF EXISTS idx_awi_agent_active;
CREATE INDEX idx_awi_agent_active
    ON agent_work_items (agent_id, status)
    WHERE status IN ('active', 'waiting_input');
