-- 0062_reminders.down.sql — drop the Reminder aggregate tables.
DROP INDEX IF EXISTS idx_reminder_firings_reminder;
DROP TABLE IF EXISTS reminder_firings;
DROP INDEX IF EXISTS idx_reminders_creator;
DROP INDEX IF EXISTS idx_reminders_remindee_status;
DROP INDEX IF EXISTS idx_reminders_next_run_at;
DROP TABLE IF EXISTS reminders;
