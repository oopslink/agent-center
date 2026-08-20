CREATE TABLE IF NOT EXISTS authorization_shadow_readiness (
  id TEXT PRIMARY KEY,
  mode TEXT NOT NULL,
  window_started_at TEXT NOT NULL,
  window_ended_at TEXT NOT NULL,
  transports_json TEXT NOT NULL,
  checks INTEGER NOT NULL DEFAULT 0,
  mismatches INTEGER NOT NULL DEFAULT 0,
  legacy_only INTEGER NOT NULL DEFAULT 0,
  equivalent_only INTEGER NOT NULL DEFAULT 0,
  ready INTEGER NOT NULL DEFAULT 0,
  reason TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);
