-- 0039_v27_files.down.sql

DROP INDEX IF EXISTS idx_file_refs_uri;
DROP INDEX IF EXISTS idx_file_refs_scope;
DROP TABLE IF EXISTS file_references;
DROP TABLE IF EXISTS blob_metadata;
