# Usage task_id attribution fix (issue-af03da2f / I54)

## Problem

The Analytics **Top Cost Tasks** panel was permanently empty
("No task-scoped usage this month.") even though total spend / project trend /
model trend all had data. Root cause: every `usage_events` row had an empty
`task_id` (513/513 on the live DB), so `Analytics.TopTasks`
(`internal/usage/sqlite/analytics.go`, predicate `task_id IS NOT NULL AND != ''`)
grouped zero rows.

`task_id` is lost on the report path: the worker's per-turn hook
(`AgentController.maybeReportUsage`) reports `currentTaskID`, which is only set when
the daemon injects a WorkItem brief. In the live (pull-model) deployment agents
self-manage their queue via MCP `start_task` / `list_my_tasks`, so the daemon never
learns the current task_id (and a converse turn explicitly clears it) → it is
always empty.

## Fix — ② center-side fallback (the live fix)

The center is the running-task authority, so `report_usage`
(`internal/admin/api/agent_tools_usage.go`) now backfills an empty `task_id` from
the agent's **sole** running task:

- New `Service.SoleRunningTask(ctx, assignee)` (`assign_flow.go`) returns the
  assignee's running task **iff it has exactly one** running-unblocked task,
  else `(nil, nil)`. Backed by new repo method
  `TaskRepository.ListRunningUnblockedByAssignee` — the SAME run-slot predicate
  (`status='running' AND blocked_reason IS NULL/''`) as the concurrency cap's
  `CountRunningUnblockedByAssignee`.
- The handler attributes the turn only when `task_id` is empty **and** exactly one
  running task exists; `project_id` is then derived from that task (authoritative,
  as before).

The **"exactly one" guard** is what satisfies the acceptance criteria:

| Agent state | Backfill | Why |
|---|---|---|
| 0 running tasks | no (stays `""`) | converse / idle → non-task overhead bucket (AC#2) |
| exactly 1 running | yes | unambiguous → the live revival case (AC#1, AC#3) |
| >1 running (≤N concurrency, W4c) | no (stays `""`) | ambiguous — center can't know which; per-executor source binding is the right fix there (see ① below) |

Known approximation: if an agent with exactly one running task replies to a DM
(converse) mid-task, that turn — reported with an empty `task_id` — is attributed
to the running task. The center cannot distinguish a converse turn from task work
on the wire; precise per-turn exclusion would need a worker-side turn-context hint
(deferred — ties into ①). This errs toward "cost incurred while that task was the
agent's sole active work", which is a reasonable bound.

## ① W1 executor source binding — verification finding (no code change)

Verified the executor fork path (`workViaExecutor` → `orchestrator.Engine`):

- The source binding **already exists**: `WorkItem.TaskRef` → `input.json`
  `Source.TaskRef` (`orchestrator/engine.go`), which the W2 writeback already uses
  as the task id for `complete_task`.
- The executor process emits **no usage events at all** today — the
  `internal/workerdaemon/executor` package has zero usage-reporting code (the
  executor is isolated: no MCP, no center credentials). So there is no executor
  `report_usage` call site into which to "pass task_id through".
- Moreover the entire `agent.work` / `work()` handler that forks executors has **no
  live producer** (the pull model sends `work_available` and the resident claude
  self-pulls), so this path is not exercised in production.

Conclusion: ① is a no-op against the current code — the binding is present, and
executor-emitted usage accounting is a separate, larger feature (parse the executor
CLI's token totals → report to center tagged with the bound `Source.TaskRef`). The
② fallback covers the only path producing usage in production.

## Cleanup (dead resident-inject branch) — deferred

The issue gates the cleanup of `work()`'s legacy `sess.Inject(pl.Brief)` branch on
"after the concurrent execution path is stable". That path is not yet live (no
producer for `agent.work`; the W4a live trigger is still blocked), and the whole
`work()` handler is currently unreachable — so removing only its inject sub-branch
is zero-value churn in mid-flight wiring. Deferred; flagged for PD.

## Tests / coverage

- `service`: `TestSoleRunningTask_*` — none / exactly-one / >1-ambiguous /
  blocked-excluded / empty-assignee / repo-error. `SoleRunningTask` 100%.
- `sqlite`: `TestTaskRepo_ListRunningUnblockedByAssignee[_QueryError]` — predicate +
  closed-DB error. `ListRunningUnblockedByAssignee` 91.7%.
- `admin/api`: `TestReportUsage_TaskIDFallback` — empty task_id + sole running task
  → backfilled; no running task → stays `""`. Existing `TestReportUsage` unchanged
  (no regression: its task-less posts precede any running task).
- `go build ./...`, `go vet`, `gofmt` clean; usage / workerdaemon / admin /
  projectmanager suites green.
