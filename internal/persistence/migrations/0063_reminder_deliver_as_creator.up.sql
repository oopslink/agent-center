-- 0063_reminder_deliver_as_creator.up.sql — Cognition BC Reminder F-B (v2.11.0
-- 验收 finding F-B): the create dialog's「以本人身份创建提醒文本」toggle. When ON
-- the to-the-remindee delivery is posted as the CREATOR's identity instead of the
-- system identity. Stored as a per-reminder flag, set once at creation.
--
-- Default 1 (ON) per the mockup. ALTER (not folded into 0062) so a dev DB that
-- already applied 0062 picks up the column; existing rows backfill to ON.
ALTER TABLE reminders ADD COLUMN deliver_as_creator INTEGER NOT NULL DEFAULT 1;
