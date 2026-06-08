-- 0053_v281_task_issue_tags_status_changed.up.sql — v2.8.1 Edit-Task (@oopslink).
--
-- Adds two columns to tasks + issues for the Edit-Task modal:
--   tags              — freeform labels, stored as a JSON array string (e.g.
--                       '["bug","urgent"]'); '' = none. Max 10, each 1..16 chars
--                       (enforced in the AR, not the schema).
--   status_changed_at — RFC3339 timestamp of the last status change, for the
--                       "status duration" render (now - status_changed_at).
--                       '' for rows predating this migration (the AR falls back
--                       to updated_at/created_at on rehydrate).
ALTER TABLE pm_tasks  ADD COLUMN tags              TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_tasks  ADD COLUMN status_changed_at TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_issues ADD COLUMN tags              TEXT NOT NULL DEFAULT '';
ALTER TABLE pm_issues ADD COLUMN status_changed_at TEXT NOT NULL DEFAULT '';
