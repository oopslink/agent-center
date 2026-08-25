# I145 Plan Owner, Block Event, Evolution and Delivery Contract

> Status: Frozen executable contract
> Date: 2026-08-25
> Scope: Plan Owner responsibility loop, Block Event read model, active generation/frontier, Owner actions, atomic evolution, explicit delivery contract, API/MCP/UI schema, compatible migration and rollback.

## 1. Repository Scan Baseline

Current consumers scanned before freezing this contract:

- Domain and services: `internal/projectmanager/{plan.go,task.go,plan_generation.go,plan_frontier.go,plan_blocked_on.go,remediation.go}` and `internal/projectmanager/service/{plan_flow.go,plan_lifecycle.go,plan_generation_evolution.go,plan_remediation.go,plan_blocked_on_read.go}`.
- Persistence: `internal/projectmanager/sqlite/{plan_repo.go,issue_task_repo.go,remediation_repo.go}` and migrations `0054`, `0055`, `0108`, `0122`, `0127`.
- API/MCP/UI: `internal/webconsole/api/handlers_pm_plans.go`, `internal/webconsole/api/server.go`, `internal/mcphost/tools.go`, `web/src` Plan/task consumers.
- Tests: Plan generation/evolution, remediation, blocked frontier, API handler, MCP tool and sqlite round-trip suites.

Existing implementation already has Plan status, task blocked status, `pm_plan_blocked_on`, `pm_plan_generations`, `active_generation_id`, evolution CAS/idempotency, `delivery_contract`, audit log and Plan generation API. This contract freezes the target shape and forbids older auto-remediation and natural-language inference paths.

## 2. State Machines

Plan:

```text
pending --start--> running --pause--> paused --resume--> running
pending/running/paused --discard--> discarded
running/paused --settle when all effective nodes terminal and all continuations closed--> done
```

Attention is orthogonal:

```text
none --active block--> attention_required --ack/owner still deciding--> attention_required
attention_required --timeout policy--> escalated
attention_required/escalated --block resolved--> none
```

Task:

```text
open --dispatch/start--> running --complete--> completed
running --block--> blocked --owner resume--> running
open/running/blocked --terminalize--> discarded
```

`blocked` means recoverable external decision/input is needed. It is not Plan failure and does not authorize the system to create remediation.

## 3. Owner and Recovery Policy

Plan fields:

```text
owner_ref TEXT NOT NULL
backup_owner_ref TEXT NOT NULL DEFAULT ''
attention_status TEXT NOT NULL CHECK none|attention_required|escalated
attention_since TEXT NOT NULL DEFAULT ''
last_attention_event_id TEXT NOT NULL DEFAULT ''
recovery_notify_after_seconds INTEGER NOT NULL DEFAULT 0
recovery_remind_after_seconds INTEGER NOT NULL DEFAULT 900
recovery_escalate_after_seconds INTEGER NOT NULL DEFAULT 3600
```

Create/start rules:

- `create_plan` requires explicit `owner_ref`; UI may prefill creator but must require confirmation.
- `start_plan` revalidates owner membership plus topology/evolution permission.
- Owner transfer is atomic CAS on Plan version, leaves no ownerless window, and writes audit.
- Owner disabled, removed or losing permission immediately sets `attention_required` and targets `backup_owner_ref`, else Project Owner.

Owner permissions are the minimum required for this Plan: read Plan/Task/Execution evidence, acknowledge/resolve block events, resume or terminalize related tasks, evolve Plan generation, create and assign continuation tasks. Code, deployment and security permissions are not inherited automatically; the Owner must choose an assignee/approver with those permissions.

## 4. Block Event Schema

Stable read model table:

```text
pm_plan_block_events(
  event_id TEXT PRIMARY KEY,
  idempotency_key TEXT NOT NULL UNIQUE,
  plan_id TEXT NOT NULL,
  generation_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  node_id TEXT NOT NULL DEFAULT '',
  execution_id TEXT NOT NULL DEFAULT '',
  block_version INTEGER NOT NULL,
  blocked_reason TEXT NOT NULL,
  reason_type TEXT NOT NULL,
  blocked_by TEXT NOT NULL,
  blocked_at TEXT NOT NULL,
  active INTEGER NOT NULL DEFAULT 1,
  effective INTEGER NOT NULL DEFAULT 1,
  impacted_downstream_json TEXT NOT NULL DEFAULT '[]',
  owner_ref TEXT NOT NULL,
  next_actions_json TEXT NOT NULL,
  acknowledged_at TEXT NOT NULL DEFAULT '',
  acknowledged_by TEXT NOT NULL DEFAULT '',
  resolved_at TEXT NOT NULL DEFAULT '',
  resolved_by TEXT NOT NULL DEFAULT '',
  resolution_kind TEXT NOT NULL DEFAULT '',
  resolution_note TEXT NOT NULL DEFAULT '',
  notification_state TEXT NOT NULL DEFAULT 'pending',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
)
UNIQUE(plan_id, generation_id, task_id, block_version)
```

Idempotency key format:

```text
plan_block:{plan_id}:{generation_id}:{task_id}:{block_version}
```

Replay of the same task/generation/block version returns the existing event and does not notify again. Reason, responsibility or state changes advance `block_version` and create a new event.

Task blocked transaction:

1. Persist blocker, reason, actor, time and execution.
2. Stop node dispatch.
3. Set Plan `attention_required`.
4. Recompute active generation/effective-node frontier.
5. Create/update Block Event idempotently.
6. Notify and wake Plan Owner.
7. Expose the four Owner actions.

Notification failure is observable and retryable but never rolls back the Block Event.

## 5. Active Generation and Effective Frontier

Every topology evolution creates an immutable `pm_plan_generations` row. Plan mutable state only points to the active generation. Old generation nodes remain auditable but are `effective=false` for current progress, blockers, notifications and UI.

Frontier schema returned by all API/MCP/UI readers:

```json
{
  "active_generation_id": "generation-1",
  "effective_nodes": ["task-a", "task-c"],
  "ready_set": ["task-c"],
  "blocked": [{"task_id": "task-b", "event_id": "block-1"}],
  "running": ["task-a"],
  "completed_history": ["task-old"]
}
```

Completed history is immutable and never rewritten by replacement, bypass or rollback.

## 6. Owner Actions

`owner_action` is one of:

- `resume_original`: allowed only after input is supplied or the external obstacle is gone. Writes resolution note, clears block and recomputes frontier. Dispatch follows normal dependency readiness.
- `replace_with_continuation`: terminalizes the original task as discarded/failed-history, creates a new task in the same `evolve_plan_generation` transaction, sets `follows_task_id`/`supersedes`, assignee, explicit `delivery_contract`, dependencies and acceptance gates, then dispatches only after commit.
- `bypass_remove_node`: marks the old effective node false in the new generation after Owner confirms downstream/gate/risk impact. History remains.
- `pause_or_discard_plan`: pauses for more decision or discards all non-terminal effective nodes. The system never chooses this automatically.

No action may create a planless backlog remediation task.

## 7. Evolution Contract

`evolve_plan_generation` is the only mutation surface for replacement/remediation topology changes. It is one transaction:

- CAS `plan.version` and active `parent_generation_id`.
- Validate actor permission, Plan mutability, active generation and idempotency fingerprint.
- Validate topology diff, impacted downstream, lineage, assignee, explicit delivery contract and acceptance gates before creating tasks.
- Create continuation/remediation tasks, graph nodes and edges.
- Terminalize/supersede/bypass old effective nodes as requested.
- Persist generation snapshot and activate it.
- Recompute frontier and dispatch newly ready nodes when Plan is running.
- Write audit and outbox events.

If validation fails, the transaction leaves no task, edge, generation, dispatch record or orphan backlog row.

Concurrent evolution uses `base_version` + `parent_generation_id`; only one sibling wins. Reusing an idempotency key with different payload fails with `idempotency_conflict`.

## 8. Delivery Contract

`delivery_contract` is required for every new Task created by Plan authoring or evolution:

- `code_change`: a forked executor must push a remote-reachable HEAD advancement.
- `evidence_only`: product source may be unchanged, but structured evidence must be durably committed and pushed to the canonical executor branch.
- `supervisor_inline`: only for explicit center control actions; it must not fork an empty workspace executor.

The system must not infer this value from title, description or words such as QA, verify, evidence, 验收 or 补证据. Changing the contract requires Owner evolution/replacement, never silent rewrite.

Legacy stored empty values read as `code_change` for compatibility only. New write paths fail closed unless the request is explicitly marked as a legacy migration/backfill.

## 9. API and MCP Schema

Required surfaces:

- `create_plan(owner_ref, backup_owner_ref?, recovery_policy?)`
- `transfer_plan_owner(plan_id, new_owner_ref, reason, expected_version)`
- `get_plan`: returns owner, backup, recovery policy, attention status, unresolved block events, active generation and frontier.
- `acknowledge_plan_block(event_id)`
- `resolve_plan_block(event_id, resolution_kind, generation_id?, note)`
- `evolve_plan_generation(...)`
- `list_plan_block_events(plan_id, active_only?)`
- Existing `block_task` keeps task-side semantics; when task belongs to an effective Plan node it must project Block Event and notify Owner.

MCP descriptions must match API schema. `delivery_contract` is required in create/evolve task payloads and is never described as defaulted or inferred.

## 10. UI Schema

Plan header always shows owner, backup/escalation target, recovery policy and attention status. `attention_required` uses a distinct banner and must not render as Plan failed.

Blocked panel shows reason, reason type, blocked by, blocked at, current assignee, impacted downstream, wait duration, acknowledge, resume, replace, bypass/remove, pause and discard.

Evolution review shows generation diff, effective-node changes, ready-set preview, impacted downstream, delivery-contract changes, acceptance-gate changes and permission/deploy/data risk. Confirm submits once to `evolve_plan_generation`.

## 11. Audit

Audit records include actor, reason, generation, timestamp and before/after detail for:

- Owner set/transfer and recovery policy changes.
- Block create, acknowledge, resolve, notification and escalation.
- Topology/evolution before/after diff.
- Task lineage and terminalization.
- Delivery contract and acceptance gate changes.
- Permission denials for Owner actions.

## 12. Migration and Rollback

Compatible migration:

- Add owner/attention/recovery columns with safe defaults.
- Backfill pending/running/paused owner from creator; invalid creator falls back to Project Owner and writes audit.
- Do not force backfill terminal Plans.
- Generate one active Block Event for existing blocked effective nodes in active generations only.
- Clear stale frontier projections for terminal Plans.
- Do not migrate discarded/superseded executor failures into active attention.
- Reconcile is idempotent: rerun returns the same events and notification state.

Rollback:

- Drop new projections, indexes and notification queues.
- Keep historical Task facts and generation rows unchanged.
- Empty legacy owner fields can be reconstructed from creator/project owner during downgrade reads.
- Rolling back notifications never changes Task status, Plan status or audit facts.

## 13. Explicit Prohibitions

- No system-created remediation task.
- No silent DAG mutation.
- No delivery-contract inference from natural language.
- No orphan backlog continuation/remediation task.
- No current blocker notification for `effective=false`, discarded or superseded history.
- No Plan failure status for recoverable blocked work.
