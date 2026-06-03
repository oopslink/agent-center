-- 0045_file_transfer_session.up.sql — v2.7 D3-a (ADR-0048, files transfer)
--
-- The FileTransferSession AR: one row per upload or download of a blob. The id
-- IS the transfer-session id (carried in the transfer URI ac://transfers/{id}).
-- For uploads the file_uri is minted fresh at create time so the caller always
-- holds the final ac://files/{ulid} reference up front (ADR-0048 §2); for
-- downloads it points at an existing blob. sha256 + final size are filled on
-- completion (upload integrity metadata). Sessions are TTL-bounded: the D3-c GC
-- reaps open-but-expired sessions + their partially-written blobs.
-- App-layer referential integrity per conventions § 9.w (no FK declarations).

CREATE TABLE file_transfer_sessions (
    id            TEXT PRIMARY KEY,
    file_uri      TEXT NOT NULL,
    transfer_uri  TEXT NOT NULL,
    direction     TEXT NOT NULL,            -- upload | download
    status        TEXT NOT NULL,            -- open | completed | canceled | expired
    content_type  TEXT,
    size          INTEGER NOT NULL DEFAULT 0,
    sha256        TEXT,                      -- set on completion
    scope         TEXT,                      -- optional task|issue|project|conversation|agent|tmp
    scope_id      TEXT,                      -- optional
    created_by    TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    expires_at    TEXT NOT NULL,
    UNIQUE (transfer_uri)
);

-- Expiry scan for the D3-c GC: open sessions ordered by expiry.
CREATE INDEX idx_fts_status_expires
    ON file_transfer_sessions (status, expires_at);

-- Lookups by the blob FileURI (download-session reuse, reachability later).
CREATE INDEX idx_fts_file_uri
    ON file_transfer_sessions (file_uri);
