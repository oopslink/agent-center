-- 0007_v2_worker_config_fields.down.sql — revert v2 Worker config 服务端化

ALTER TABLE workers DROP COLUMN capabilities_json;
ALTER TABLE workers DROP COLUMN discovery_json;
ALTER TABLE workers DROP COLUMN concurrency_json;
ALTER TABLE workers ADD COLUMN capabilities TEXT NOT NULL DEFAULT '[]';
