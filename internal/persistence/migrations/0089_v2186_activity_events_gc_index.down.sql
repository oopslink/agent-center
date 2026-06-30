-- 0089_v2186_activity_events_gc_index.down.sql — reverse of the up migration: drop the
-- occurred_at GC support index. The periodic activity-event GC would then fall back to a
-- full scan per delete (functionally correct, just slower); the table itself is untouched.
DROP INDEX IF EXISTS idx_aae_occurred_at;
