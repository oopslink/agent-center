-- 0001_init.down.sql — Phase 1 反操作
--
-- 仅本地开发 / 测试用（plan § 6 R7：v1 仅 DROP TABLE 全表）；生产环境不应跑 down。
-- DROP 顺序考虑 FK：子表先删 → 父表后删。

DROP INDEX IF EXISTS uniq_messages_vendor_ref;
DROP INDEX IF EXISTS idx_messages_conv;
DROP TABLE IF EXISTS messages;

DROP INDEX IF EXISTS uniq_conversations_channel_thread;
DROP INDEX IF EXISTS idx_conversations_status;
DROP INDEX IF EXISTS idx_conversations_kind;
DROP TABLE IF EXISTS conversations;

DROP INDEX IF EXISTS uniq_proposals_active_path;
DROP INDEX IF EXISTS idx_proposals_status;
DROP INDEX IF EXISTS idx_proposals_worker;
DROP TABLE IF EXISTS worker_project_proposals;

DROP INDEX IF EXISTS uniq_mappings_active;
DROP INDEX IF EXISTS idx_mappings_project;
DROP INDEX IF EXISTS idx_mappings_worker;
DROP TABLE IF EXISTS worker_project_mappings;

DROP INDEX IF EXISTS idx_workers_status;
DROP TABLE IF EXISTS workers;

DROP TABLE IF EXISTS projects;

DROP INDEX IF EXISTS uniq_events_seq;
DROP INDEX IF EXISTS idx_events_decision;
DROP INDEX IF EXISTS idx_events_correlation;
DROP INDEX IF EXISTS idx_events_type;
DROP INDEX IF EXISTS idx_events_occurred_at;
DROP TABLE IF EXISTS events;
