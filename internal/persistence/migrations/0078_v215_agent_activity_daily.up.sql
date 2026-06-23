-- 0078_v215_agent_activity_daily.up.sql — v2.15.0 I28/F3 (issue-a7ff560e Build
-- Spec, single source of truth). The daily rollup slice of the per-agent
-- analytics dashboard: one aggregate row per (agent_ref, day, project_id),
-- maintained by an incremental job. Additive only — touches no existing table.
--
-- WHY A ROLLUP. The dashboard's heatmap / overview cards / trend read a per-agent
-- daily time series (≈12 months × per-project). Scanning raw usage_events +
-- activity tables on every page load does not scale; agent_activity_daily is the
-- pre-aggregated read model the dashboard (F4+) reads directly. The raw tables
-- stay the drill-down source (Top Cost Tasks reads usage_events by task).
--
-- GRAIN (Build Spec, 钉死): PK (agent_ref, day, project_id).
--   · agent_ref  — canonical "agent:<id>" ref form (e.g. agent:agent-6cdfa49f).
--     Every source already stores this scheme form (usage_events.agent_ref =
--     "agent:"+IdentityMemberID; task_action_logs.agent_ref / pm_issues.created_by
--     / pm_plans.creator_ref / messages.sender_identity_id are IdentityRef;
--     events.actor is an Actor "kind:id"). The rollup counts ONLY agent-kind rows
--     (ref LIKE 'agent:%') and stores the ref verbatim — no per-source rewrite.
--   · day        — UTC calendar date "YYYY-MM-DD" = substr(<ts>,1,10), relying on
--     the repo-wide UTC-normalized RFC3339 timestamp convention (same basis as
--     issue_task_repo's created_at range filters). UTC matches the 12-month heatmap.
--   · project_id — "" sentinel (NOT NULL DEFAULT '') for the no-project /
--     interaction bucket (converse turns; a message/commit with no resolvable
--     project). NEVER NULL: NULL breaks PK uniqueness and the UPSERT's ON CONFLICT.
--
-- COLUMNS (Build Spec): events_count + token/cost. cache_tokens is the COMBINED
-- cache_read + cache_write (the rollup does not split them; usage_events keeps the
-- split for drill-down). No model dimension — model/project stacked trends are
-- F4's job, read straight off usage_events. tokens/cost come from usage_events;
-- events_count is the activity count (see the job's per-source 口径).
CREATE TABLE agent_activity_daily (
    agent_ref     TEXT    NOT NULL,
    day           TEXT    NOT NULL,            -- 'YYYY-MM-DD' UTC
    project_id    TEXT    NOT NULL DEFAULT '', -- '' = no-project / interaction bucket; never NULL
    events_count  INTEGER NOT NULL DEFAULT 0,  -- Σ activity rows (task_action_logs+message+issue+plan+commit)
    tokens_in     INTEGER NOT NULL DEFAULT 0,  -- Σ usage_events.input_tokens
    tokens_out    INTEGER NOT NULL DEFAULT 0,  -- Σ usage_events.output_tokens
    cache_tokens  INTEGER NOT NULL DEFAULT 0,  -- Σ (cache_read_tokens + cache_write_tokens)
    cost_micros   INTEGER NOT NULL DEFAULT 0,  -- Σ usage_events.cost_micros
    PRIMARY KEY (agent_ref, day, project_id)
);

-- Per-agent time-series scan (heatmap / trend / overview cards across the 12-month
-- window). The PK already orders by agent_ref first, but an explicit (agent_ref,
-- day) index keeps the range scan covering when project_id is rolled up away.
CREATE INDEX idx_agent_activity_daily_agent_day ON agent_activity_daily (agent_ref, day);

-- agent_activity_rollup_cursor: the incremental job's per-source watermark. One row
-- per source; cursor_key is the largest monotonic key already folded into the
-- rollup, so each run only scans rows beyond it. Keys differ by source (the job
-- documents each): usage_events / messages / task_action_logs use their ULID `id`
-- (monotonic by insertion — survives a back-dated ts, e.g. transcript reconcile);
-- events(commit) uses the monotonic `seq`; pm_issues / pm_plans (random ids) use
-- the composite `created_at||'#'||id`. cursor_key is opaque TEXT (max() compared
-- lexicographically / numerically by the job per source).
CREATE TABLE agent_activity_rollup_cursor (
    source      TEXT NOT NULL PRIMARY KEY,     -- 'usage_events'|'task_action_logs'|'messages'|'issues'|'plans'|'commits'
    cursor_key  TEXT NOT NULL DEFAULT '',
    updated_at  TEXT NOT NULL DEFAULT ''
);
