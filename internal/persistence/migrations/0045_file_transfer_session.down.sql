-- 0045_file_transfer_session.down.sql

DROP INDEX IF EXISTS idx_fts_file_uri;
DROP INDEX IF EXISTS idx_fts_status_expires;
DROP TABLE IF EXISTS file_transfer_sessions;
