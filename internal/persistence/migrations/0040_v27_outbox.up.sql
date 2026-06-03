-- 0040_v27_outbox.up.sql — v2.7 A0 (plan §10 OQ1, task #95)
--
-- Cross-bounded-context reliability seam: a producing AppService writes its
-- own BC state AND an outbox_events row in one local transaction; an async,
-- idempotent (dedup by event_id), replayable relay applies the cross-BC
-- effect via projectors. A0 ships the tables + relay skeleton; the first
-- producer (ProjectManager subscriber→ConversationParticipant projection,
-- ADR-0052) lands in phase B.
--
--   outbox_events  — the event log. processed_at NULL = not yet fully relayed.
--   outbox_applied — per-(projector, event) dedup ledger; the PK is the
--                    idempotency key so redelivery is a harmless no-op even if
--                    a projector body is not strictly idempotent.

CREATE TABLE outbox_events (
    id            TEXT PRIMARY KEY,        -- event_id (ULID), the dedup key
    event_type    TEXT NOT NULL,           -- <bc>.<entity>.<action>
    refs          TEXT NOT NULL DEFAULT '{}',
    payload       TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL,
    processed_at  TEXT                      -- NULL = unprocessed
);

CREATE INDEX idx_outbox_unprocessed
    ON outbox_events (id)
    WHERE processed_at IS NULL;

CREATE TABLE outbox_applied (
    projector_name  TEXT NOT NULL,
    event_id        TEXT NOT NULL,
    applied_at      TEXT NOT NULL,
    PRIMARY KEY (projector_name, event_id)
);
