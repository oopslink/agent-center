-- 0051_agent_work_item_single_active_unique.up.sql — v2.8.1 #278
--
-- Fix the single-active race in the Agent BC dispatch/activate path (task #277):
-- enforce at the DB layer that an agent has AT MOST ONE active|waiting_input
-- WorkItem at any time. The pre-existing idx_awi_agent_active (v2.7 #100, 3d62a21)
-- was a PARTIAL index but NOT UNIQUE, so concurrent dispatch (check-then-act on
-- HasActiveWorkItem, non-atomic) could land multiple active items for one agent
-- (reproduced: 8 concurrent assign → 9 active). This UNIQUE index is the DB
-- backstop behind the application-layer atomic dispatch (queue-drain, not drop).
--
-- Note: UNIQUE is on (agent_id) ALONE (not (agent_id, status)) — a composite
-- unique would still allow one 'active' AND one 'waiting_input' per agent, which
-- violates the single-active invariant.

-- 1. Demote any pre-existing excess so the unique index can be created on a
--    dirty DB (the #277 bug may have already produced multiple active rows).
--    Keep the oldest active|waiting_input per agent (MIN(id) — ULIDs are
--    creation-ordered); the rest fall back to 'queued' and re-drain sequentially.
--    No-op on a clean DB.
UPDATE agent_work_items
SET status = 'queued'
WHERE status IN ('active', 'waiting_input')
  AND id NOT IN (
    SELECT MIN(id)
    FROM agent_work_items
    WHERE status IN ('active', 'waiting_input')
    GROUP BY agent_id
  );

-- 2. Replace the non-unique partial index with a UNIQUE one.
DROP INDEX IF EXISTS idx_awi_agent_active;
CREATE UNIQUE INDEX idx_awi_agent_active
    ON agent_work_items (agent_id)
    WHERE status IN ('active', 'waiting_input');
