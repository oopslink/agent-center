-- 0055_v29_plan_dispatch_records.up.sql — v2.9 Plan Orchestration dispatch record (#285).
--
-- The dispatch record is the ONLY orchestrator-owned stored state (§9.2/§9.3):
-- node status itself is DERIVED (blocked/ready/dispatched/running/done/failed),
-- never stored. A dispatch record is written ONCE when the ready node's @mention
-- is posted into the Plan conversation; advance dispatches a ready node only if
-- it has no record, so re-running advance / event replay / a second upstream
-- completing NEVER double-@mentions (§9.3 idempotency). A creator re-run of a
-- failed node CLEARS its record (DELETE) so the next advance re-dispatches.
--
-- §9.8: the record is scoped to one Plan (the PK includes plan_id); two plans'
-- dispatch state is isolated.

CREATE TABLE pm_plan_dispatch_records (
    plan_id             TEXT NOT NULL,
    task_id             TEXT NOT NULL,
    dispatched_at       TEXT NOT NULL,
    dispatch_message_id TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (plan_id, task_id)
);
CREATE INDEX idx_pm_plan_dispatch_records_plan ON pm_plan_dispatch_records (plan_id);
