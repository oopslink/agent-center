-- 0064_center_settings.up.sql — I7-D1 (T216) center settings store.
--
-- A generic key/value store for center-wide (system) settings. The first
-- consumer is the wake-guardrail config (design 04-wake-guardrail.md §3.5): the
-- five `wake.*` threshold keys the WakeGuard reads, tunable via the I7-D3
-- Settings UI. Absent keys fall back to conservative code defaults (the guard is
-- never disabled by a missing setting), so this table is purely an override layer.
--
-- §9.0: TEXT keys, TEXT values (callers parse/validate per key), TEXT ISO8601
-- updated_at. No FKs — keys are an open namespace owned by their reading code.
CREATE TABLE center_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL  -- ISO8601 UTC
);
