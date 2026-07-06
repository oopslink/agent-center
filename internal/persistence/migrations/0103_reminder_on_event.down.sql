-- 0103_reminder_on_event.down.sql — revert the event-driven reminder trigger.
DROP INDEX IF EXISTS idx_reminders_on_event;
ALTER TABLE reminders DROP COLUMN on_event_delay_seconds;
ALTER TABLE reminders DROP COLUMN on_event_event;
ALTER TABLE reminders DROP COLUMN on_event_entity_id;
ALTER TABLE reminders DROP COLUMN on_event_entity_type;
