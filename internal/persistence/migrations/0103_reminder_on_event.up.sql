-- 0103_reminder_on_event.up.sql — Cognition BC Reminder event-driven trigger
-- (reminder-event feature, issue-68ccb310). Adds the on_event trigger to the
-- reminders aggregate: an event-driven reminder stays DORMANT (next_run_at NULL,
-- status=active = "armed, awaiting event") until a matching pm entity state-change
-- event ARMS it (next_run_at = event_time + delay); it then fires exactly once and
-- completes (one-shot consumption). The @target is the existing remindee_agent_id
-- (the wake target), so no new target column is needed.
--
-- ADDITIVE (nullable columns, no folding into 0062) so a dev DB that already applied
-- 0062/0063 picks these up; existing time/cron reminders leave them NULL. The
-- schedule JSON's kind becomes "on_event" for these rows (decoded by the repo).
ALTER TABLE reminders ADD COLUMN on_event_entity_type  TEXT;    -- plan | task | issue (NULL for time/cron reminders)
ALTER TABLE reminders ADD COLUMN on_event_entity_id    TEXT;    -- the watched entity's id
ALTER TABLE reminders ADD COLUMN on_event_event        TEXT;    -- the watched transition (completed|failed|stopped|blocked|reopened|discarded|closed)
ALTER TABLE reminders ADD COLUMN on_event_delay_seconds INTEGER; -- delay after the event before firing (>=0)

-- The ReminderEventProjector's hot path: find DORMANT armed reminders matching an
-- incoming entity event (status=active AND next_run_at IS NULL AND the on_event
-- triple). Partial index keeps it to just the event-driven rows.
CREATE INDEX idx_reminders_on_event
    ON reminders (on_event_entity_type, on_event_entity_id, on_event_event)
    WHERE on_event_entity_type IS NOT NULL;
