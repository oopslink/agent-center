-- 0095_v228_worker_system_info.up.sql — worker-reported host + build identity
-- (T752 / Worker Profile). Additive nullable column; existing rows default to
-- '{}' (no info reported yet) so the Profile page falls back to its per-field
-- "Coming in v2.9" placeholder until the worker uploads on its next online.
ALTER TABLE workers ADD COLUMN system_info_json TEXT NOT NULL DEFAULT '{}';
