-- 0001_init.down.sql — Phase 1 反操作
--
-- 仅本地开发 / 测试用（plan § 6 R7：v1 仅 DROP TABLE 全表）；生产环境不应跑 down。
-- DDL 无 FK 声明（conventions § 9.w），DROP 顺序按拓扑依赖即可。

DROP INDEX IF EXISTS uniq_messages_vendor_ref;
DROP INDEX IF EXISTS idx_messages_conv;
DROP TABLE IF EXISTS messages;

DROP INDEX IF EXISTS uniq_conversations_channel_thread;
DROP INDEX IF EXISTS idx_conversations_status;
DROP INDEX IF EXISTS idx_conversations_kind;
DROP TABLE IF EXISTS conversations;

-- v2.7 #131: worker_project_proposals / worker_project_mappings / projects
-- tables retired (no longer created in up) — nothing to drop.

DROP INDEX IF EXISTS idx_workers_status;
DROP TABLE IF EXISTS workers;

DROP INDEX IF EXISTS uniq_events_seq;
DROP INDEX IF EXISTS idx_events_decision;
DROP INDEX IF EXISTS idx_events_correlation;
DROP INDEX IF EXISTS idx_events_type;
DROP INDEX IF EXISTS idx_events_occurred_at;
DROP TABLE IF EXISTS events;
