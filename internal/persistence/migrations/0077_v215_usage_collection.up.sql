-- 0077_v215_usage_collection.up.sql — v2.15.0 I28/F1 (issue-a7ff560e Build Spec).
-- Per-agent analytics dashboard, collection-schema slice. Additive only: two new
-- tables (model_prices, usage_events) + seed list prices. Touches no existing
-- table; the rollup (agent_activity_daily) is F3 and report_usage is F2 — neither
-- is created here.
--
-- SQLite adaptation of the spec's logical types (PD-confirmed): the spec writes
-- TIMESTAMPTZ / BIGINT, but this repo stores every timestamp as RFC3339 TEXT and
-- every integer as INTEGER — we follow the repo's existing convention. Tables are
-- unprefixed (no pm_) to match the observability-domain naming; this is the new
-- `internal/usage` bounded context. No FK REFERENCES — the schema is uniformly
-- app-managed (agent_ref / project_id / task_id are logical references).

-- model_prices: historical unit-price table. Composite PK (model, effective_from);
-- cost lookup picks the row with the greatest effective_from <= the event ts, so a
-- price change is non-retroactive (decision 4: past events keep the price in force
-- at their time). Prices are micros (1e-6 USD) per MILLION tokens.
--   cost_micros(tokens) = round(tokens * <unit>_per_mtok_micros / 1_000_000)
-- cache is split read vs write (Anthropic charges them differently). cache_write is
-- the 5-minute-TTL rate (1.25x input) for MVP; a future 1h tier would be a new
-- column, smoothed in via effective_from without a backfill.
CREATE TABLE model_prices (
    model                       TEXT    NOT NULL,
    effective_from              TEXT    NOT NULL,  -- RFC3339; price in force from this instant
    input_per_mtok_micros       INTEGER NOT NULL,
    output_per_mtok_micros      INTEGER NOT NULL,
    cache_read_per_mtok_micros  INTEGER NOT NULL,
    cache_write_per_mtok_micros INTEGER NOT NULL,
    PRIMARY KEY (model, effective_from)
);

-- usage_events: raw per-turn usage detail, drill-down source for the dashboard.
-- cost_micros is materialized at write time by joining model_prices at (model, ts)
-- — hot path / rollup read it directly — but model_prices.effective_from is retained
-- so a mispriced batch can be recomputed. source distinguishes the 'report' path
-- (F2 report_usage MCP) from the 'transcript' reconciliation fallback, for dedup.
CREATE TABLE usage_events (
    id                  TEXT    PRIMARY KEY,
    agent_ref           TEXT    NOT NULL,
    project_id          TEXT    NOT NULL,
    task_id             TEXT,                       -- nullable: not every turn is task-scoped
    model               TEXT    NOT NULL,
    input_tokens        INTEGER NOT NULL DEFAULT 0,
    output_tokens       INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens   INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens  INTEGER NOT NULL DEFAULT 0,
    cost_micros         INTEGER NOT NULL DEFAULT 0,  -- materialized at write time
    ts                  TEXT    NOT NULL,            -- RFC3339 event time
    source              TEXT    NOT NULL             -- 'report' | 'transcript'
);

-- Per-agent time-series scan (heatmap / trend / overview cards) and per-task
-- drill-down (Top Cost Tasks).
CREATE INDEX idx_usage_events_agent_ts ON usage_events (agent_ref, ts);
CREATE INDEX idx_usage_events_task ON usage_events (task_id);

-- Seed: current Anthropic list prices for the four served models (decision 2:
-- list-price estimate). Micros per Mtoken. cache_write = 1.25x input (5m TTL),
-- cache_read = 0.1x input. effective_from is a launch baseline well before any
-- collection begins so every future event resolves a price; ops add later rows
-- (or correct these) without retroactive effect.
--   opus 4.8 : $5 / $25      sonnet 4.6: $3 / $15
--   haiku 4.5: $1 / $5       fable 5  : $10 / $50
INSERT INTO model_prices (model, effective_from, input_per_mtok_micros, output_per_mtok_micros, cache_read_per_mtok_micros, cache_write_per_mtok_micros) VALUES
    ('claude-opus-4-8',   '2026-01-01T00:00:00Z',  5000000, 25000000,  500000,  6250000),
    ('claude-sonnet-4-6', '2026-01-01T00:00:00Z',  3000000, 15000000,  300000,  3750000),
    ('claude-haiku-4-5',  '2026-01-01T00:00:00Z',  1000000,  5000000,  100000,  1250000),
    ('claude-fable-5',    '2026-01-01T00:00:00Z', 10000000, 50000000, 1000000, 12500000);
