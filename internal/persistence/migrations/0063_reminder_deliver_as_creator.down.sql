-- 0063_reminder_deliver_as_creator.down.sql — revert F-B: drop the per-reminder
-- delivery-identity flag.
ALTER TABLE reminders DROP COLUMN deliver_as_creator;
