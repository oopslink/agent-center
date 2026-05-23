-- 0007_v2_worker_config_fields.up.sql — Phase 8 (v2) Worker config 服务端化
--
-- 覆盖 ADR-0023 § 3：Worker 行为配置从 worker.yaml 迁到 center DB
--
-- 变更：
--   - ADD COLUMN concurrency_json   TEXT NOT NULL DEFAULT '{"per_agent_type":2}'
--   - ADD COLUMN discovery_json     TEXT NOT NULL DEFAULT '{"scan_paths":[],"exclude":[],"scan_interval":"1h"}'
--   - DROP COLUMN capabilities      （v1 list-of-string；v2 改 capabilities_json 含 detected/enabled）
--   - ADD COLUMN capabilities_json  TEXT NOT NULL DEFAULT '[]'
--
-- v2 不考虑向后兼容（用户决策 2026-05-22）；不迁 v1 capabilities 数据。

ALTER TABLE workers ADD COLUMN concurrency_json   TEXT NOT NULL DEFAULT '{"per_agent_type":2}';
ALTER TABLE workers ADD COLUMN discovery_json     TEXT NOT NULL DEFAULT '{"scan_paths":[],"exclude":[],"scan_interval":"1h"}';
ALTER TABLE workers DROP COLUMN capabilities;
ALTER TABLE workers ADD COLUMN capabilities_json  TEXT NOT NULL DEFAULT '[]';
