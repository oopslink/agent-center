-- 0048_v271_identity_email_last_session.up.sql — v2.7.1 #213/#214 (@oopslink)
--
-- Adds two nullable columns to identities (BC9):
--   email           — user contact email. NOT verified in v2.7.1 (no mail sent /
--                     no click). UNIQUE among users, but MULTIPLE NULLs allowed:
--                     pre-v2.7.1 users (v2.7.0 upgrade path) have no email and
--                     must NOT collide on a NULL. New signups supply a (unique)
--                     email; the handler maps a dup to 409.
--   last_session_at — RFC3339Nano timestamp stamped on each successful signin
--                     (incl. signup auto-signin). NULL until the user logs in
--                     under v2.7.1 (so all v2.7.0-upgraded users start NULL).
--
-- UPGRADE-safe (v2.7.0 → v2.7.1, NOT fresh-install-only like v2.6→v2.7): existing
-- rows get NULL for both columns; the partial UNIQUE index treats NULL as distinct
-- (SQLite default + the WHERE guard), so any number of legacy email-less users
-- coexist without violating uniqueness.
ALTER TABLE identities ADD COLUMN email TEXT;
ALTER TABLE identities ADD COLUMN last_session_at TEXT;

CREATE UNIQUE INDEX uniq_identities_email_user
    ON identities (email) WHERE kind = 'user' AND email IS NOT NULL;
