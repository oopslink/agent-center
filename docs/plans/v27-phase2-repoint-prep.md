# v2.7 #107 Phase-2 — old-model repoint + delete prep (Dev2 investigation)

Read-only map for PD sequencing. Principle (oopslink): NO transition/compat/dual-read — repoint to pm/agent new model, then DROP old tables + delete dead code cleanly. Tester disconnect-FIXED hard-verifies: fleet/stats/admin/cli/webconsole-api read ONLY new model, old tables DROPped on fresh DB, KEEP-set (IR/artifact) intact.

## Old → New mapping
| Old | New |
|-----|-----|
| `task_execution_projections` + `projection.TaskExecutionProjection` | `agent_work_item_projections` (mig 0046) + `projection.AgentWorkItemProjection` (#1) |
| `task_executions` (taskruntime) | `agent_work_items` + `agent_activity_events` + pm_tasks status |
| `discussion.Issue` / discussion BC | projectmanager Issues (pm) |
| old `tasks` | `pm_tasks` |

## Read paths to repoint (core = internal/observability/query/, behind fleet/stats/admin/cli/webconsole-api)
- **fleet.go** `fetchExecutions`: taskruntime executions + `TaskExecutionProjection` → `agent_work_item_projections` (via new `List(filter)`). = disconnect-FIXED core. **MUST preserve org/project scoping** (old: Tasks.FindByID(org); new: task_ref→pm task→org) — hard gate, no cross-org leakage. + live-status filter (non-terminal).
- **stats.go** `aggregateExecutions`→agent_work_item_projections; `aggregateTasks`/`aggregateIssues`(discussion)→pm.
- **projections.go / service.go**: projectProjection/projectIssueRow + Issues=discussion.IssueRepository → new projection + pm issue repo.

## Slices (per-read-surface, PD-approved): fleet → stats → projections → issue-API(discussion).
fleet step 1 (DONE): add `AgentWorkItemProjectionRepository.List(filter)` (status-set + agent + ORDER BY last_activity_at DESC, index-backed).
fleet step 2: rewrite fetchExecutions; resolve task_ref per WI via agent.WorkItemRepository (org-scope, same N+1 as old) → FleetWorkItemRow (rename, drop AgentCLI/WorkspaceMode; working_seconds=0 defer v2.8).

## BCs / delete (after repoint + verify green)
- **taskruntime — PARTIAL, NOT wholesale**: drop execution/old-task model (task_executions, dispatch, kill, timeoutscan, task_execution_repo). **KEEP input_request(IR) + artifact = LIVE** (only IR-observability deferred v2.8).
- **discussion BC**: external consumers = issue API across cli/handlers_issue + derivation_shims + admin/api/discussion.go + webconsole/api/handlers.go + observability/query. ALL repoint to pm issues → then delete BC + drop tables. = separate big slice after fleet/stats.
- **dispatchq** (internal/admin/dispatchq): admin/api/dispatch_queue.go endpoint — confirm dead before delete.
- observability/projection task_status_projection.go + TaskExecutionProjection types — drop after fleet/stats repoint.

## Drop tables (NEW migration 0047+, no data migration): task_execution_projections / task_executions / discussion tables / old tasks.

## Drop-safety (Q3, Tester-endorsed): before drop, enumerate NO live WRITER to old tables (OnExecutionTerminal old-projection writer / discussion issue writes must be dead). READ+WRITE double-clean.
