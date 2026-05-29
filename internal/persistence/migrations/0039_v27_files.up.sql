-- 0039_v27_files.up.sql — v2.7 A0 (ADR-0048, task #95)
--
-- Horizontal file/blob module seam (identity ≠ placement, plan §2.7 / §10 OQ8).
-- A0 ships the records only; byte transfer (upload/download/GC) lands in D.
--
--   blob_metadata   — write-once blob integrity rows. Identity = opaque ULID
--                     (the FileURI is ac://files/{ulid}); content_sha256 is
--                     integrity/dedup metadata ONLY, never used for addressing.
--   file_references — placement records {scope, scope_id} -> file_uri. One blob
--                     may have many references (sharing = +reference, never copy);
--                     filename/mime/size/display_name live here, not on the blob.
--                     Soft-delete via deleted_at; the phase-D GC reaps a blob
--                     once its live reference count hits zero past a grace period.

CREATE TABLE blob_metadata (
    ulid            TEXT PRIMARY KEY,
    size_bytes      INTEGER NOT NULL DEFAULT 0,
    content_sha256  TEXT,
    created_at      TEXT NOT NULL
);

CREATE TABLE file_references (
    id            TEXT PRIMARY KEY,
    file_uri      TEXT NOT NULL,
    scope         TEXT NOT NULL,          -- task|issue|project|conversation|agent|tmp
    scope_id      TEXT NOT NULL,
    filename      TEXT,
    mime_type     TEXT,
    size_bytes    INTEGER NOT NULL DEFAULT 0,
    display_name  TEXT,
    created_by    TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    deleted_at    TEXT                     -- NULL = live
);

CREATE INDEX idx_file_refs_scope
    ON file_references (scope, scope_id)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_file_refs_uri
    ON file_references (file_uri)
    WHERE deleted_at IS NULL;
