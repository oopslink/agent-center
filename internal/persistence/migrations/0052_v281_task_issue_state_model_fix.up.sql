-- 0052_v281_task_issue_state_model_fix.up.sql — v2.8.1 state machine model fix
--
-- @oopslink domain catch: "assigned 和 open 不是一个层级的状态，assigned 还没开始
-- 做，还是 open 状态" — the Task "assigned" STATE is removed; assignee is pure
-- METADATA (an assigned task is "open" until the agent starts it). The abandoned
-- terminal is renamed to a uniform "discarded" (废弃) semantic across Task + Issue
-- (Task "canceled" → "discarded", Issue "withdrawn" → "discarded").
--
-- pm_tasks.status / pm_issues.status are plain TEXT with no CHECK constraint, so
-- this is a pure data rewrite. The assignee column is NOT touched: assigned→open
-- preserves the assignee as metadata.
UPDATE pm_tasks  SET status = 'open'      WHERE status = 'assigned';
UPDATE pm_tasks  SET status = 'discarded' WHERE status = 'canceled';
UPDATE pm_issues SET status = 'discarded' WHERE status = 'withdrawn';
