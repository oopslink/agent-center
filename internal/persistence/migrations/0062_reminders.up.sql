-- 0062_reminders.up.sql — Cognition BC Reminder aggregate (design:
-- docs/design/architecture/tactical/cognition/03-reminder.md §4).
--
-- A Reminder wakes a target agent at a time (once) or on a cron schedule and
-- delivers a text. §9.w: NO foreign keys — project_id / remindee_agent_id /
-- creator_ref are reference columns whose integrity the Cognition Repository /
-- AppService enforces. §9.0: TEXT ULID PK, TEXT ISO8601 times, enum (status) as
-- TEXT, version CAS. The schedule + end_condition value objects are stored as
-- JSON TEXT (opaque to SQL; decoded by the repo).

CREATE TABLE reminders (
    id                TEXT PRIMARY KEY,
    organization_id   TEXT NOT NULL,
    project_id        TEXT NOT NULL,
    creator_ref       TEXT NOT NULL,            -- kind-prefixed identity (user:/agent:)
    remindee_agent_id TEXT NOT NULL,
    schedule          TEXT NOT NULL,            -- JSON: {kind, once_at?, cron_expr?, timezone}
    content           TEXT NOT NULL,
    status            TEXT NOT NULL,            -- active | paused | completed | canceled
    next_run_at       TEXT,                     -- ISO8601 UTC; NULL when paused/terminal
    last_fired_at     TEXT,                     -- ISO8601 UTC; NULL until first fire
    skip_if_overlap   INTEGER NOT NULL DEFAULT 1,
    end_condition     TEXT NOT NULL,            -- JSON: {kind, until?, max_count?}
    fired_count       INTEGER NOT NULL DEFAULT 0,
    version           INTEGER NOT NULL DEFAULT 1,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL
);

-- The scheduler's hot path: scan active reminders due now (FindDue, §3.3).
CREATE INDEX idx_reminders_next_run_at ON reminders (next_run_at);
-- "remind me" management lists (by remindee, filter status).
CREATE INDEX idx_reminders_remindee_status ON reminders (remindee_agent_id, status);
-- "what did I set" lists (by creator).
CREATE INDEX idx_reminders_creator ON reminders (creator_ref);

-- Append-only trigger history (§D6/§4): every (would-be) fire, incl. overlap-skips.
-- No version (append-only convention §4).
CREATE TABLE reminder_firings (
    id          TEXT PRIMARY KEY,
    reminder_id TEXT NOT NULL,
    fired_at    TEXT NOT NULL,                  -- ISO8601 UTC
    outcome     TEXT NOT NULL,                  -- delivered | skipped_overlap | failed
    detail      TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_reminder_firings_reminder ON reminder_firings (reminder_id, fired_at);
