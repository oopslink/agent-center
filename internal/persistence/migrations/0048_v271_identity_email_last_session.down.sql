-- 0048_v271_identity_email_last_session.down.sql — revert v2.7.1 #214.
DROP INDEX IF EXISTS uniq_identities_email_user;
ALTER TABLE identities DROP COLUMN last_session_at;
ALTER TABLE identities DROP COLUMN email;
