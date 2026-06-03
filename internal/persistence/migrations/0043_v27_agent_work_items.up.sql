-- 0043_v27_agent_work_items.up.sql — v2.7 C2 (ADR-0049, task #100)
--
-- AgentWorkItem (logical work-queue item, references a Task but does NOT own
-- its state) + AgentActivityEvent (append-only observation stream — there is
-- no AgentRun, so "what the agent did" is reconstructed from this). The
-- outbox-driven AssignTask→EnqueueWorkItem projector is wired in B2.

CREATE TABLE agent_work_items (
    id            TEXT PRIMARY KEY,
    agent_id      TEXT NOT NULL,
    task_ref      TEXT NOT NULL,             -- Task id/URI (referenced, not owned)
    status        TEXT NOT NULL,             -- queued|active|waiting_input|blocked|done|failed|canceled|superseded
    interactions  INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    version       INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_awi_agent ON agent_work_items (agent_id);
CREATE INDEX idx_awi_task  ON agent_work_items (task_ref);
-- availability (OQ2) input: agent has an active/waiting_input item.
CREATE INDEX idx_awi_agent_active
    ON agent_work_items (agent_id, status)
    WHERE status IN ('active', 'waiting_input');

CREATE TABLE agent_activity_events (
    id              TEXT PRIMARY KEY,
    agent_id        TEXT NOT NULL,
    work_item_ref   TEXT,
    interaction_ref TEXT,
    event_type      TEXT NOT NULL,
    payload         TEXT NOT NULL DEFAULT '{}',
    occurred_at     TEXT NOT NULL
);
CREATE INDEX idx_aae_agent     ON agent_activity_events (agent_id, occurred_at);
CREATE INDEX idx_aae_work_item ON agent_activity_events (work_item_ref) WHERE work_item_ref IS NOT NULL;
