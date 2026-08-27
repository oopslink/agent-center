# Plan Progress Control Phase 0 contract

Status: **frozen for Phase 0**  
Plan: `plan-f4e88e01`  
Contract stage: S1  
Frozen source baseline: repository branch `ac-exec/task-a1b9b2cd/exec-5b3e7a8c` before this change at `16a41203`  
Task-input package: `task-input/v1` was required by the assignment but is not present in this isolated workspace; this document is frozen from the task contract text plus current repository sources.

This document is the contract S2A-S2E must implement and accept against. A
change to the domain vocabulary, obligation rule, stability gate, table grain,
API state, migration boundary, or failure-matrix disposition is a contract
change and must return to S1. S2 implementers must not infer replacement
semantics independently from old Plan/Task status fields, worker logs, executor
exit, branch presence, or chat summaries.

## 1. Phase 0 invariant

Every incomplete Plan node is always in exactly one of two auditable conditions:

1. it has a verifiable progress fact that can be read back from the authoritative
   production source; or
2. it has a named, time-bounded, acknowledged, and escalatable responsibility.

There is no third state. `cannot_determine`, stale projections, quiet workers,
missing acknowledgements, and ambiguous delivery evidence are never neutral.
They become a named `Obligation`, `Incident`, or `Hold` with a deadline, owner,
and escalation path.

The invariant is evaluated per Plan, per effective node, per active
continuation. It does not create a new `PlanNode` state machine and does not
rewrite topology.

## 2. Unified language

| Term | Kind | Frozen meaning |
|---|---|---|
| `ObservationVector` | Value Object | Immutable snapshot of the facts used to judge one effective Plan node at one `as_of`: task lifecycle, Plan graph readiness, dispatch record, executor lifecycle, delivery record, gate verdicts, continuation status, blocker records, wake/ack facts, and projection freshness. |
| `ProgressFact` | Value Object | One verifiable, source-addressed fact inside an `ObservationVector`: source kind, source id, occurred_at, observed_at, source cursor/version, summary, and quality. |
| `suspect` | Quality label | A fact exists but is insufficiently stable for advancement because source lag, contradictory evidence, missing ack, clock skew, lease uncertainty, or delivery ambiguity is present. `suspect` can keep a node visible but cannot satisfy a gate. |
| `cannot_determine` | Decision outcome | The evaluator cannot prove either progress or responsibility from authoritative sources within the freshness SLA. It must fail closed into `Obligation` or `Incident`; it is not a display-only unknown. |
| `Obligation` | Entity | A named actor owes a concrete action by a deadline for a node or continuation: acknowledge, produce evidence, decide, repair, re-run, accept, reject, or escalate. |
| `Incident` | Entity | A system-owned abnormal condition that prevents reliable progress classification: projector lag, lost wake ack, lease conflict, migration gap, corrupt evidence, or watchdog failure. |
| `Hold` | Entity | A deliberate fail-closed gate that prevents node advancement while an `Obligation` or `Incident` is open. Holds are not Plan lifecycle states and do not pause the whole Plan unless an explicit `PausePlan` command is issued. |
| `DeliverySubject` | Value Object | Immutable description of what is being delivered: Plan id, node/task id, execution id where applicable, repository id/ref, base SHA, candidate SHA, branch, pushed remote, and subject type. |
| `Acceptance` | Entity | Immutable verdict over one `DeliverySubject` and acceptance contract: `passed`, `rejected`, or `waived_by_authority`, with actor, timestamp, evidence refs, and findings. |
| `Ack` | Entity | Durable acknowledgement that a named actor or service received a wake/obligation. In-memory delivery, log lines, or executor stdout are not acks. |
| `Escalation` | Entity | Durable handoff created when an obligation misses deadline, an incident exceeds SLA, or a wake budget suppresses delivery. |
| `progress_hold` | Read/write concern | The second-layer clock and durable hold ledger used to avoid invisible waits. It complements `pm_plan_blocked_on`; it does not replace node readiness logic. |

`Task.status`, Stage projection, GateVerdict, PlanContinuation, dispatch records,
and Delivery remain owned by ProjectManager. The new progress-control concepts
observe and constrain those aggregates; they do not reclassify completed work or
change legal state transitions.

## 3. ObservationVector contract

`ObservationVector` is built by a ProjectManager AppService read path from
production sources only:

| Component | Authoritative source | Advancement rule |
|---|---|---|
| Plan topology/readiness | `pm_plans`, Plan nodes/edges, stages, continuations, dispatch records | effective topology and dependencies must be read with one Plan version/CAS snapshot. |
| Task lifecycle | `pm_tasks` and Task domain methods | terminal Task facts are authoritative; no executor exit can complete a Task. |
| Gate and acceptance | Stage/Gate verdict tables, review verdicts, immutable `Acceptance` records | merge/ship nodes remain fail-closed until required pass/waive facts exist. |
| Delivery | Task `delivery`, DeliveryContract, repository remote verification | valid only when candidate SHA is pushed to the named remote and base advancement is positive when required. |
| Executor lifecycle | TaskExecution/runtime activity and worker control events | executor terminal signals are diagnostic unless tied to valid delivery or Task transition. |
| Wake/Ack | durable wake ledger and ack rows | a wake without durable ack creates an obligation after ack deadline. |
| Blocked-on | `pm_plan_blocked_on` plus `progress_hold` | blocked reason must name release condition and deadline or become incident. |
| Projection freshness | source watermark/checkpoint rows | stale beyond SLA produces `cannot_determine` and a hold. |

The evaluator returns one of:

```text
progress_fact_verified
responsibility_bound
cannot_determine
```

`progress_fact_verified` requires at least one non-suspect fact newer than the
node's last evaluated frontier point, or a terminal immutable verdict that closes
the node/continuation. `responsibility_bound` requires an open `Obligation` or
`Incident` with owner, deadline, ack status, escalation target, and hold id.
`cannot_determine` must be persisted as an incident before the API returns.

## 4. Stability gates for `suspect` and `cannot_determine`

Facts are `suspect` until all relevant fences hold:

- source checkpoint is within the configured watermark lag SLA;
- the fact's source cursor is not behind the Plan snapshot cursor used by the
  evaluator;
- active-active writers agree on the per-Plan lease/fencing token;
- wake delivery is either durably acked or represented by an open obligation;
- delivery SHA is confirmed on the remote ref named by `DeliverySubject`;
- gate/acceptance verdict is immutable and tied to the exact subject version;
- clock order is sane: `occurred_at <= observed_at <= evaluated_at` unless an
  incident explicitly records skew;
- duplicate/replayed source events collapse by source id, not payload text;
- migration compatibility has backfilled required fields or created holds for
  rows that cannot be classified.

`cannot_determine` is emitted when a required source cannot be read, the source
watermark is older than SLA, contradictory facts remain after de-duplication, or
the evaluator lacks authority to see a source. The default action is fail closed:
create `Incident(kind='progress_classification_unknown')` and a `Hold`.

## 5. Obligation, Incident, Hold

An `Obligation` is valid only when all fields are present:

```text
id, plan_id, node_id/task_id or continuation_id, kind, owner_ref,
owner_display, deadline_at, ack_required, acked_at nullable,
escalate_to_ref, escalation_deadline_at, source_fact_refs[],
status, created_at, updated_at, version
```

Allowed `kind` values for Phase 0:

- `ack_wake`
- `produce_delivery`
- `repair_delivery`
- `acceptance_verdict`
- `human_decision`
- `remediation_plan`
- `source_recovery`
- `lease_conflict_resolution`

`Incident` fields are the same shape but `owner_ref` is a service owner or named
on-call role and `kind` is one of:

- `watermark_lag`
- `projector_unavailable`
- `wake_ack_lost`
- `lease_fence_conflict`
- `watchdog_silent`
- `delivery_subject_ambiguous`
- `migration_gap`
- `api_contract_violation`
- `source_authorization_unknown`

`Hold` is the machine-enforced edge:

```text
id, plan_id, node_id/task_id or continuation_id, reason_kind,
reason_id, blocks_dispatch, blocks_acceptance, blocks_completion,
started_at, deadline_at, released_at nullable, release_fact_ref nullable
```

Open Holds must be visible in Plan detail APIs. A Hold can be released only by a
new `ProgressFact`, `Acceptance`, `Ack`, or `Escalation` fact that names the
original hold id. Deleting or editing a hold in place is forbidden.

## 6. Immutable DeliverySubject and Acceptance

`DeliverySubject` is captured when delivery or acceptance is requested. It is a
Value Object, not a pointer to a moving branch:

```text
subject_id
plan_id
task_id/node_id
execution_id nullable
repo_id nullable
remote
branch
base_sha
candidate_sha
candidate_ref
delivery_contract_hash
acceptance_contract_hash
created_at
```

Acceptance evaluates the exact subject. If a branch advances, a new subject is
required. A passing acceptance cannot be retargeted to a later SHA, and a reject
cannot be erased by force-pushing a branch.

Gate behavior is fail-closed:

- no subject -> hold;
- subject without pushed candidate -> hold;
- pushed candidate not descended from base when required -> reject or hold by
  contract;
- acceptance evidence attached to a different SHA -> `suspect`;
- reviewer verdict without authority -> incident;
- executor exit without DeliverySubject -> no acceptance signal.

## 7. Durable wake, ack, escalation, and token bucket

Wake is a two-step durable protocol:

1. create wake intent with plan/node, assignee, reason, token-bucket decision,
   idempotency key, and deadline;
2. record ack from the target path or create an `ack_wake` obligation when the
   ack deadline expires.

Rate limiting uses a center-wide token bucket per `(plan_id, owner_ref)` and a
global bucket for storm control. Suppressed wake attempts are not dropped: they
create `Obligation(kind='ack_wake')` or `Incident(kind='wake_ack_lost')` with the
bucket state as evidence. Token-bucket refill and consumption must be visible in
diagnostics, but token state is not a Plan lifecycle state.

Escalation is durable and idempotent on `(obligation_id, deadline_at,
escalate_to_ref)`. Re-sending notifications may be best effort; the escalation
fact is not.

## 8. `progress_hold` second-layer clock

`pm_plan_blocked_on` explains why a node is not runnable. `progress_hold` adds
the missing clock and owner discipline:

- every open blocked-on row that has no executable release fact must have a
  matching hold within one evaluation tick;
- hold deadlines are stored in UTC and evaluated by a reconciler, not by UI;
- a missed deadline creates escalation before any retry/re-wake;
- hold evaluation is per Plan with a fenced lease so active-active centers do
  not double-escalate;
- `paused` Plan state suppresses automatic dispatch, but does not suppress hold
  deadline accounting or incident visibility.

This is not automatic execution retry. The only automatic actions are
classification, hold creation, ack timeout, escalation creation, and read-model
refresh.

## 9. Active-active lease, fencing, watchdog

All write-side progress-control reconcilers acquire a per-Plan lease:

```text
lease_scope = progress_control:plan:<plan_id>
fencing_token monotonically increases on acquisition
expires_at
holder_id
```

Every write includes the fencing token. A stale holder's write fails closed with
`Incident(kind='lease_fence_conflict')`. Readers may serve the last committed
state, but must show `freshness.state='stale'` when the current lease/checkpoint
is past SLA.

The watchdog observes expected heartbeat classes:

- projector checkpoint advancement;
- wake ack processing;
- hold-deadline reconciler ticks;
- escalation outbox delivery attempts;
- executor activity for nodes with active execution;
- delivery verification for nodes awaiting acceptance.

Missed watchdog heartbeats create incidents. They never mark nodes complete and
never trigger generic executor retry.

## 10. Persistence and migration boundary

Phase 0 freezes an additive migration path. Existing tables remain authoritative:

- `pm_plans`, Plan node/edge/stage tables;
- `pm_plan_blocked_on`;
- `pm_plan_continuations`;
- `pm_plan_generations`;
- `pm_tasks.delivery` and `pm_tasks.delivery_contract`;
- gate/review verdict tables;
- worker control and activity event tables.

S2 may add tables or columns equivalent to:

```sql
pm_progress_observations
pm_progress_obligations
pm_progress_incidents
pm_progress_holds
pm_progress_wake_acks
pm_progress_escalations
pm_delivery_subjects
pm_acceptances
pm_progress_leases
pm_progress_checkpoints
```

Compatibility rules:

- all additions are nullable/defaulted or backfilled in the same migration;
- old rows with insufficient data are not guessed. They receive
  `Incident(kind='migration_gap')` plus Hold when still effective;
- migrations must be restartable and idempotent;
- schema version assertions, down migrations, repository round-trips, API
  serialization, and non-empty upgrade smoke must move together;
- no migration may rewrite Plan topology, synthesize GateVerdicts, reopen
  terminal tasks, or mutate delivery SHA history;
- rollback may drop new progress-control tables only after confirming no
  running process requires them. Existing Plan/Task data must remain readable.

## 11. API contract

Phase 0 adds or extends Plan read surfaces; it does not add a write API that
lets clients bypass ProjectManager AppService commands.

Plan detail and node APIs must include:

```json
{
  "progress_control": {
    "as_of": "...",
    "freshness": {"state": "fresh|stale|degraded", "watermark_lag_ms": 0, "threshold_ms": 0},
    "decision": "progress_fact_verified|responsibility_bound|cannot_determine",
    "observation_vector_id": "...",
    "quality": "valid|suspect",
    "open_obligations": [],
    "open_incidents": [],
    "open_holds": []
  }
}
```

HTTP behavior:

- cross-org or unauthorized source access fails closed without leaking existence;
- stale data is 200 only when an open Incident/Hold is included;
- `cannot_determine` without a persisted incident is a 500 contract violation;
- mutation endpoints that would dispatch, accept, complete, or merge while a
  blocking Hold is open return 409 with hold ids;
- clients must treat unknown enum values as degraded and non-advancing.

## 12. Explicit hard boundaries

S2A-S2E must not introduce:

- a new persistent `PlanNode` lifecycle state machine;
- automatic topology rewrite;
- a generic Artifact platform;
- automatic executor retry;
- branch-name-only acceptance;
- UI-only responsibility tracking;
- direct SQLite reads from worker/CLI paths that bypass ProjectManager
  AppService;
- MCP-only writes.

## 13. Failure matrix

| # | Failure | Required classification | Required action | Verification |
|---|---|---|---|---|
| 1 | Source checkpoint exceeds watermark lag SLA | `Incident(watermark_lag)` + Hold | fail closed; expose stale freshness | unit + API stale response |
| 2 | Projector unavailable/rebuilding | `Incident(projector_unavailable)` | no dispatch/acceptance based on projection | integration restart/rebuild |
| 3 | Event arrives late but source id is new | `suspect` until replayed | recompute vector; do not clamp time | replay test |
| 4 | Stop-before-start executor activity | `suspect` or incident if unrepaired by overlap | exclude from progress fact until stable | fixture with reversed events |
| 5 | Executor exits 0 without valid delivery | `Obligation(produce_delivery)` | hold completion/acceptance | service test |
| 6 | Delivery committed but not pushed | `Obligation(repair_delivery)` | reject complete_task; require pushed SHA | delivery round-trip test |
| 7 | Delivery evidence points at moving branch only | `Incident(delivery_subject_ambiguous)` | require immutable subject SHA | API 409 |
| 8 | Acceptance verdict references different SHA | `suspect` | hold gate; require new subject or verdict | contract test |
| 9 | Reviewer lacks authority | `Incident(api_contract_violation)` | fail closed, no gate pass | authz negative test |
| 10 | Wake intent written but no ack by deadline | `Obligation(ack_wake)` | escalate after deadline | clocked reconciler test |
| 11 | Wake token bucket suppresses delivery | responsibility-bound obligation | record bucket evidence, no silent drop | rate-limit test |
| 12 | Obligation deadline missed | `Escalation` | create idempotent escalation fact | deadline test |
| 13 | Open blocked-on row has no owner/deadline | `cannot_determine` -> incident + hold | backfill or escalate | migration/read test |
| 14 | Active-active lease conflict | `Incident(lease_fence_conflict)` | stale writer rejected, single committed owner | fencing test |
| 15 | Watchdog misses reconciler heartbeat | `Incident(watchdog_silent)` | no auto retry; expose service owner | watchdog test |
| 16 | Migration finds legacy effective node with missing delivery contract | `Incident(migration_gap)` | hold only that node/continuation | upgrade smoke |
| 17 | Cross-org source needed for classification | `Incident(source_authorization_unknown)` | fail closed without leaking source | authz test |
| 18 | Duplicate source event replay | no new obligation/acceptance | idempotent by source id | replay/idempotency test |
| 19 | Plan paused with open obligations | responsibility remains bound | no dispatch, but deadlines/escalations continue | paused-plan clock test |

## 14. S2 node execution contract

Every S2 node must carry either a verifiable progress fact or a named
responsibility with deadline. Dates are UTC.

| Node | Scope | Entry fact already available | Required owner and deadline if not complete | S1 acceptance fact required from node |
|---|---|---|---|---|
| S2A Domain model | Implement ObservationVector, Obligation, Incident, Hold, DeliverySubject, Acceptance domain types and invariants | This S1 document freezes terms and hard boundaries | DRI `S2A-backend-domain-owner`, deadline 2026-08-28T18:00:00Z, ack required in Plan thread before start | domain tests prove no third state and immutable subject/verdict |
| S2B Persistence/migration | Add additive schema, repositories, backfill/migration gap handling | Existing migrations 0054, 0108, 0109, 0121, 0127 provide base Plan/blocked/delivery/remediation tables | DRI `S2B-persistence-owner`, deadline 2026-08-29T18:00:00Z, ack required before migration PR | non-empty upgrade smoke plus schema assertions and round-trips |
| S2C Evaluator/reconciler | Build ObservationVector evaluator, hold clock, lease/fencing, watchdog, wake ack/escalation | Existing blocked-on, delivery, runnable-gate, stuck-node, and wake guard code provide sources | DRI `S2C-orchestration-owner`, deadline 2026-08-31T18:00:00Z, ack required before enabling reconciler | clocked tests for 19 matrix rows that touch reconcilers |
| S2D API/UI | Surface progress_control envelope and fail-closed mutation responses | Existing Plan detail APIs and blocked_on projection are the read path | DRI `S2D-product-surface-owner`, deadline 2026-09-01T18:00:00Z, ack required before UI merge | API and UI tests show stale/hold/incident/obligation states distinctly |
| S2E Integrated acceptance | Prove one production-chain candidate from source facts through API/UI and migration | S1 contract and S2A-D candidate SHAs | DRI `S2E-acceptance-owner`, deadline 2026-09-02T18:00:00Z, ack required before merge gate | exact candidate SHA on remote; 19-row failure matrix evidence; origin/main readback |

If any owner misses its ack or deadline, the Plan must show an escalation; the
node cannot be counted complete by executor exit, test logs alone, or task status
alone.

## 15. Acceptance gates for this S1 freeze

This S1 freeze is complete only when:

- the contract is committed and pushed to a remote branch with an exact SHA;
- `docs/design/README.md` links this document;
- repository readback confirms the commit contains the document;
- the final report names any missing input-package fact instead of claiming
  unavailable attachment contents;
- S2A-S2E cite this file and the exact S1 SHA.

Before final Plan completion, the accepted candidate must be merged to
`origin/main` and re-read from `origin/main`. A pushed branch or executor
completion is not final acceptance.
