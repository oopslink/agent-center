# Collaboration Effect Projection — Tactical Design

> DDD tactical layer · BC: Observability · frozen for issue-f1bad8db
>
> Base evidence: `origin/main` was fetched before design work and pinned at
> `cf014cc8a5d83b757c0f33b5387e5a7f4d344221`.

## 1. Decision Summary

`CollaborationEffectProjection` is an Observability BC replayable read model. It
derives Agent-Task contact and effect edges from committed production events and
from an explicit ProjectManager audit mirror. It is not a Domain Event, not a
ProjectManager truth source, and not an Agent performance score.

MVP attribution is deterministic. No LLM SDK, prompt call, sentiment analysis, or
natural-language adjudication is allowed in the write, replay, or query path.

The projector may consume only append-only event ledgers owned by their BC:

| Lane | Use | Current status |
|---|---|---|
| `events` | Historical replay of Domain Events that are already mirrored into Observability | Available, append-only |
| `outbox_events` | Realtime tail for PM and Conversation cross-BC events before they are marked processed | Available, but not sufficient for every MVP rule |
| `pm.audit_recorded` Domain Event | Required producer mirror for `pm_audit_log` semantic changes | Producer gap |

The projector must not read ProjectManager repositories, `pm_*` tables, or
Conversation repositories to fill missing fields.

## 2. Production Evidence

| Concern | Production evidence | Contract consequence |
|---|---|---|
| Observability event shape | `internal/observability/event.go` defines `Event{id, occurred_at, seq, event_type, refs, actor, payload, correlation_id, decision_id}` and `EventRefs` keys including `project_id`, `task_id`, `issue_id`, `conversation_id`, `message_id`, `agent_id`. | Frozen API schemas use these names and keep evidence as event ids plus refs. |
| Domain Event persistence | `internal/persistence/migrations/0001_init.up.sql` creates append-only `events`; `internal/observability/sqlite/event_repo.go` exposes `Append`, `FindByID`, `Find`. | Historical replay cursor is `events.id` with `occurred_at` window filters. |
| Realtime fan-out | `internal/webconsole/sse/fanout.go` bootstraps to latest event and does not replay history to SSE. | Realtime UI fan-out is not a replay source; projector needs its own cursor. |
| PM outbox | `internal/projectmanager/service/service.go:34` defines `pm.task.assigned`, `pm.task.reassigned`, `pm.task.state_changed`, plan events, and wake/input events; `service.go:827` appends outbox records. | Realtime projector can tail outbox only by registered projector name and idempotency key. |
| Outbox retention | `internal/outbox/relay.go` marks rows processed after all projectors apply; no history query API is exposed. | Outbox is not sufficient for historical replay after processing. |
| PM audit ledger | `internal/persistence/migrations/0100_v229_pm_audit_log.up.sql` documents permanent `pm_audit_log`; `internal/projectmanager/service/audit_record.go` writes semantic task/plan/issue changes. | Required as producer-side facts, but must be mirrored to Observability before this projection can consume it. |
| Task state payload | `internal/projectmanager/service/service.go:686` and `assign_flow.go:1200` carry `status`, `prev_status`, `assignee`, `reason`. | `complete`, `block`, and state regressions can be classified when `prev_status` and `status` are present. |
| Assign payload | `internal/projectmanager/service/assign_flow.go:1169` carries `assignee`, `previous_assignee`, `status`. | `assign` and `reassign` are covered by production outbox. |
| Review verdict | `internal/projectmanager/service/decision_auto.go:40` records `ReviewVerdict` and `pm_audit_log` entry `review_verdict`; no outbox event is emitted there. | Producer gap: must mirror audit before review accept/reject can be projected. |
| Dependency edits | `internal/projectmanager/service/plan_flow.go:415` states add dependency emits no event and writes audit only. | Producer gap for dependency add/remove authoring; dependency release is inferable only from task completion plus plan state events when mirrored. |
| Agent trace boundary | `docs/design/decisions/0015-agent-trace-not-in-events-table.md` and `docs/rules/conventions.md` keep `AgentTraceEvent` out of `events`. | This design forbids using `AgentTraceEvent` as graph/effect evidence. |

## 3. Ubiquitous Language

| Term | Meaning |
|---|---|
| `CollaborationEffectProjection` | Observability read model derived from events. |
| `CollaborationGraph` | Query DTO containing Agent and Task nodes plus relation/effect edges. |
| `CollaborationEffect` | One deterministic effect edge against a target task. |
| `EvidenceRef` | Link from an effect to committed source event facts. |
| `relation_type` | Contact category: `assign`, `reassign`, `complete`, `block`, `unblock`, `dependency_release`, `review_accept`, `review_reject`. |
| `polarity` | `positive`, `negative`, `neutral`, or `mixed`; it describes task progress, not person quality. |
| `magnitude` | Integer `1`, `2`, or `3`; coarse impact only. |

## 4. Read Model Boundary

### Aggregate / Projection Shape

`CollaborationEffectProjection` is not an Aggregate Root. It is rebuilt from
event ledgers and stores only derived rows:

| Field | Type | Notes |
|---|---|---|
| `effect_id` | string | Deterministic id, e.g. hash of `rule_version + evidence_event_ids + target_task_id + relation_type`. |
| `project_id` | string | Required. |
| `target_task_id` | string | Required for MVP. |
| `source_agent_ref` | string | `agent:<id>` when known; empty only for fail-closed neutral contact rows. |
| `target_agent_ref` | string | Optional. |
| `relation_type` | enum | Frozen in §7. |
| `polarity` | enum | Frozen in §7. |
| `magnitude` | int | `1..3`. |
| `confidence` | enum | MVP emits `high` only for direct structured events. Gaps emit no effect. |
| `occurred_at` | string | ISO8601 from evidence event or audit mirror. |
| `rule_version` | string | Starts at `collaboration-effect.mvp.v1`. |
| `evidence_event_ids` | string[] | Required non-empty for non-neutral effects. |
| `before_state` | object | Minimal typed state before transition. |
| `after_state` | object | Minimal typed state after transition. |
| `explanation_key` | string | UI localizes; no generated prose in projector. |

### Ownership

Observability owns the projection table/API. ProjectManager and Conversation own
business truth. The projection never writes back to PM, Conversation, Agent, or
Identity.

## 5. Producer Gaps

These gaps are fail-closed: downstream code must not infer missing fields by
reading PM tables.

| Gap | Blocks | Required producer/fan-out work |
|---|---|---|
| No `pm.audit_recorded` Domain Event mirror for `pm_audit_log` | Historical replay of task assign/status/review and plan dependency audit | Add PM producer that emits a Domain Event into `events` in the same transaction as `pm_audit_log.Append`, with `audit_id`, `object_type`, `object_id`, `change_type`, `field`, `from_value`, `to_value`, `actor_ref`, `detail`. |
| `RecordReviewVerdict` writes PM plan verdict + audit only | `review_accept`, `review_reject` MVP rules | Mirror the `review_verdict` audit entry; do not let Observability read `pm_plan_review_verdicts`. |
| `AddPlanDependency` / `RemovePlanDependency` write audit only | Dependency authoring edges and release explanation | Mirror plan dependency audit rows; dependency release may then pair upstream `completed` with mirrored dependency facts. |
| Current `outbox_events` is marked processed and has no replay API | Historical backfill from old outbox rows | Use `events` + audit mirror for replay; use outbox only for realtime low-latency projection. |
| `conversation.message_added` payload is contact-oriented, not effect-oriented | Neutral advise/reply only | Keep messages as contact evidence unless paired with a task/plan state change. |

## 6. Event Coverage Matrix

| MVP input | Current production event/fact | Actor | Refs | Payload fields | Coverage |
|---|---|---|---|---|---|
| assign | `pm.task.assigned` outbox | PM caller actor in audit; outbox lacks top-level actor | `task_id`, `project_id` | `assignee`, `status`, `effective_subscribers` | Covered for realtime; actor gap unless audit mirror is present. |
| reassign | `pm.task.reassigned` outbox + `pm_audit_log` | Audit `actor_ref` | `task_id`, `project_id` | `assignee`, `previous_assignee`, `status` | Covered with audit mirror. |
| block | `pm.task.state_changed` outbox + task audit | Audit `actor_ref` | `task_id`, `project_id` | `prev_status`, `status`, `reason`, `assignee` | Covered when `prev_status != blocked` and `status == blocked` or blocked annotation is mirrored. |
| unblock | `pm.task.assigned` outbox after `UnblockTask` + task audit `blocked -> running` | Audit `actor_ref` | `task_id`, `project_id` | Assignment event lacks `prev_status`; audit carries status diff | Requires audit mirror for unambiguous rule. |
| complete | `pm.task.state_changed` outbox + task audit | Audit `actor_ref` | `task_id`, `project_id` | `prev_status`, `status`, `assignee` | Covered when `status == completed`. |
| dependency release | `pm.task.state_changed` for upstream completion; plan dependency audit only | Audit `actor_ref` | `task_id`, `project_id`, required `plan_id` from mirror detail | Dependency detail `from`, `to`, `kind`; task status diff | Producer gap for replay unless dependency audit is mirrored. |
| review accept | `pm_audit_log` `task.review_verdict` with `to_value=pass` | Audit `actor_ref` | audit object id is review task id | `detail.blocking`, `detail.reason`, `detail.round`, `detail.plan_id` | Producer gap: no Domain Event/outbox mirror today. |
| review reject | `pm_audit_log` `task.review_verdict` with `to_value=reject` or `blocking=true` | Audit `actor_ref` | audit object id is review task id | Same as review accept | Producer gap: no Domain Event/outbox mirror today. |
| neutral contact | `conversation.message_added` events/outbox | sender/actor | `conversation_id`, `message_id`; task/issue only via owner ref payload | `sender`, `text`, `mention_refs` | Contact only; not effect unless paired with state change. |

Coverage threshold is met only after the audit mirror exists. Current production
evidence covers 6 of 8 MVP rule inputs directly or via permanent audit, but only
4 of 8 are consumable by an Observability-only replay projector today. Therefore
implementation must first add producer/fan-out mirrors, not cross-BC reads.

## 7. Frozen Rule Table

Rule version: `collaboration-effect.mvp.v1`.

| Rule id | Input contract | Relation | Polarity | Magnitude | Before / after |
|---|---|---|---|---|---|
| `R1_ASSIGN` | `pm.task.assigned`, `assignee` starts `agent:` | `assign` | `neutral` | 1 | `assignee: "" -> agent` when audit mirror has from/to. |
| `R2_REASSIGN` | `pm.task.reassigned`, `previous_assignee` and `assignee` start `agent:` | `reassign` | `mixed` | 2 | `assignee: previous -> next`. |
| `R3_BLOCK` | `pm.task.state_changed` or audit mirror with `to_value=blocked` | `block` | `negative` | 3 | `status: open/running/review -> blocked`; reason required if present in source. |
| `R4_UNBLOCK` | audit mirror `from_value=blocked`, `to_value=running` | `unblock` | `positive` | 3 | `status: blocked -> running`. |
| `R5_COMPLETE` | `pm.task.state_changed` or audit mirror with `to_value=completed` | `complete` | `positive` | 2 | `status: running/review -> completed`. |
| `R6_DEP_RELEASE` | upstream task `completed` plus a prior mirrored `dependency_added` fact where downstream becomes ready/dispatched | `dependency_release` | `positive` | 3 | upstream terminal state releases downstream wait. `dependency_removed` is topology cleanup/removal evidence only and never satisfies this rule. |
| `R7_REVIEW_ACCEPT` | audit mirror `change_type=review_verdict`, `to_value=pass`, `blocking=false` | `review_accept` | `positive` | 2 | review verdict absent/reject -> pass. |
| `R8_REVIEW_REJECT` | audit mirror `change_type=review_verdict`, `to_value=reject` or `blocking=true` | `review_reject` | `mixed` | 2 | progress negative; quality gate positive. |

Rules emit nothing when required fields are absent. They must record a skipped
projection diagnostic, not a guessed effect.

## 8. Frozen API Schemas

### Query

`GET /api/insights/collaboration-effects`

Query parameters:

| Name | Type | Required | Notes |
|---|---|---|---|
| `project_id` | string | yes | Project scope. |
| `task_id` | string | no | Limits graph to one task and one-hop edges. |
| `agent_ref` | string | no | `agent:<id>`. |
| `relation_type` | string | no | Enum from §3. |
| `polarity` | string | no | `positive`, `negative`, `neutral`, `mixed`. |
| `since` / `until` | ISO8601 | no | Half-open time window. |
| `cursor` | string | no | Opaque projection cursor. |
| `limit` | int | no | Default 100, max 500. |

Response:

```json
{
  "graph": {
    "nodes": [
      {"id": "agent:agent-a", "kind": "agent", "label": "Agent A"},
      {"id": "task:T1", "kind": "task", "label": "Implement API", "task_id": "T1"}
    ],
    "edges": [
      {
        "id": "ce_01H...",
        "source": "agent:agent-a",
        "target": "task:T1",
        "relation_type": "complete",
        "polarity": "positive",
        "magnitude": 2,
        "effect_id": "ce_01H..."
      }
    ]
  },
  "effects": [],
  "summary": {
    "positive_count": 0,
    "negative_count": 0,
    "neutral_count": 0,
    "mixed_count": 0,
    "affected_task_count": 0
  },
  "next_cursor": ""
}
```

### Effect DTO

```json
{
  "effect_id": "ce_01H...",
  "project_id": "P1",
  "target_task_id": "T1",
  "source_agent_ref": "agent:agent-a",
  "target_agent_ref": "",
  "relation_type": "complete",
  "polarity": "positive",
  "magnitude": 2,
  "confidence": "high",
  "occurred_at": "2026-09-03T10:00:00Z",
  "rule_version": "collaboration-effect.mvp.v1",
  "evidence_event_ids": ["evt-1"],
  "before_state": {"task_status": "running"},
  "after_state": {"task_status": "completed"},
  "explanation_key": "collaboration.effect.complete"
}
```

### Evidence API

`GET /api/insights/collaboration-effects/{effect_id}/evidence`

Response:

```json
{
  "effect_id": "ce_01H...",
  "evidence": [
    {
      "event_id": "evt-1",
      "event_type": "pm.task.state_changed",
      "occurred_at": "2026-09-03T10:00:00Z",
      "actor_ref": "agent:agent-a",
      "refs": {"project_id": "P1", "task_id": "T1"},
      "payload": {"prev_status": "running", "status": "completed", "assignee": "agent:agent-a"}
    }
  ]
}
```

## 9. Replay And Retention

Realtime projector:

1. Register an outbox projector named `observability-collaboration-effect`.
2. Consume PM/Conversation outbox events before `processed_at` is set.
3. Record applied ids in `outbox_applied`.
4. Use deterministic `effect_id` for duplicate protection.

Historical replay:

1. Read `events` through `EventRepository.Find`.
2. Require `pm.audit_recorded` mirrors for PM audit facts.
3. Rebuild projection into a new rule-version partition.
4. Atomically swap the active rule version for query.

Retention:

- `events` and `pm.audit_recorded` are replay sources.
- `outbox_events` is a realtime reliability lane, not historical retention.
- `agent_activity_events` and AgentTraceEvent JSONL are excluded.

## 10. Fixture

Reusable field-coverage fixture:
`docs/design/fixtures/collaboration-effect-mvp-v1.json`.

The fixture contains 24 production-code/test-anchored event/audit facts. Each
row has `source_evidence` pointing to a producer line, existing production test,
or sanitized historical audit fixture. The executable audit is
`docs/design/fixtures/validate-collaboration-effect-fixture.mjs`; it verifies the
row evidence anchors, required fields, `AgentTraceEvent` exclusion, and
`R6_DEP_RELEASE` pairing semantics.

Current expected consumability is fail-closed for review and dependency rows
until `pm.audit_recorded` is produced. Dependency removal remains non-release
evidence even when its `from/to` detail is present.
