# Insight metrics and aggregation API contract v2

Status: frozen for Plan `plan-d7ec8ee4` S0. Semantic version:
`insight.metrics.v2`.

This contract is the sole input for the S1/S2 implementation. The backend owns
aggregation, health classification and reason codes. Clients render the
returned decision and must not infer health from metric values.

## Common semantics

All timestamps are UTC RFC3339. The default window is half-open rolling 24h,
`[asOf-24h, asOf)`. Counts use the current entity snapshot unless explicitly
marked as a window event metric; trends and execution metrics use the window.
Percentiles use nearest-rank over known non-negative samples. P50/P95 are null
when there are no samples.

Every aggregate and metric carries `metric_version`, `sample_count`,
`coverage`, `freshness`, `unknown_count`, and `known`:

- `sample_count`: observations included after filters and validation.
- `coverage`: known eligible observations / all eligible observations; null
  when eligibility cannot be established. The ratio is in `[0,1]`.
- `freshness`: age and SLA of the least-fresh required authoritative source.
- `unknown_count`: eligible observations excluded because their state or link
  cannot be interpreted by this metric version.
- `known=false` requires a null value. A known empty population is a count of
  zero; a ratio whose denominator is zero is unknown/null, never 0%.
- stale data, low coverage, no samples, or unknown source states force health
  `unknown`; clients may still display the observed value as partial evidence.

Default confidence thresholds are sample count >= 5, coverage >= 0.90, and
freshness age <= the 120s SLA. Default execution risk thresholds are failure
rate >=10% elevated / >=25% degraded and queue P95 >=30s elevated / >=120s
degraded. Any currently blocked task is elevated; a blocked critical-path task,
failed plan, breached target date, failed evolution recovery, broken lineage,
or stale old-generation running task is degraded. Deployments may tighten these
backend thresholds, but the effective values must be returned in metric
definitions; a client must not copy or independently apply them.

Health precedence is `unknown` (confidence failure), `degraded` (delivery is
blocked/failed or target at risk), `elevated` (threshold exceeded without clear
delivery impact), then `healthy`. `healthy` is legal only when all required
metrics are known. `reason_codes` contains every triggering rule in stable
lexical order. Initial codes are defined in `internal/insight/contract_v2.go`.

## State dictionaries

- Issue snapshot: `open`, `in_progress`, `resolved`, `closed`; other values are
  unknown. Open issue means `open|in_progress`.
- Task snapshot: `backlog`, `pool`, `running`, `blocked`, `completed`, `failed`.
  `completed|failed` are terminal. A non-terminal task may belong to exactly one
  of Plan, Backlog, Assignment Pool; zero or multiple containers is anomalous.
- Plan snapshot: `pending`, `running`, `paused`, `done`, `failed`.
- Execution outcome: completed = an execution with terminal successful outcome;
  failed = terminal unsuccessful outcome; retry is a later execution causally
  linked to the same task/command attempt chain. Unknown terminal values do not
  enter either numerator and increment `unknown_count`.

## Metric dictionary

| Metric | Formula / population | Zero and unknown |
|---|---|---|
| executionCount | known executions whose effective event time is in window | empty = 0 |
| success / failed / retried | executions in each defined outcome class | empty = 0; unknown outcome excluded |
| failureRate | failed / (successful + failed) terminal executions | denominator 0 = null |
| queue P50/P95 | percentile of `startedAt-queuedAt`, non-negative and both known | no samples = null |
| duration P50/P95 | percentile of `finishedAt-startedAt`, non-negative and both known | no samples = null |
| activeSlots / availableSlots | latest fresh slot observation at `asOf` | no fresh observation = null |
| affected projects | distinct project IDs on failed/retried executions | missing project link increments unknown |
| open issues | issue snapshot in `open|in_progress` | known empty = 0 |
| issue new/closed/net trend | created events; terminal transition events; new minus closed | event window |
| issue aging | open issues whose last update exceeds backend threshold; overdue; no owner | missing timestamps/owner semantics recorded |
| blocked tasks | task snapshot `blocked` | known empty = 0 |
| task throughput | task created and terminal-completed events per bucket | event window |
| task cycle time | terminal time minus created time | invalid/missing pair excluded |
| blocked duration | end/unblock/asOf minus blocked start | open interval ends at asOf |
| active plans | `pending|running|paused` | known empty = 0 |
| plans at risk | backend target-date/stall/block rule matched | reason required |
| plan completion | completed effective nodes / all effective business nodes | no nodes = null |
| evolution rate | plans with >=1 evolution / plans entering execution in window | denominator 0 = null |
| evolution count | generation transition events; per-plan distribution also exposes P50/P95 | G0 creation is not evolution |
| generation count | number of generations including G0 | absent lineage = unknown |
| scope change | node identities added/replaced/retained/discarded per transition | each node counted once by precedence replaced, discarded, retained, added |
| rework ratio | nodes added or redone after G0 / distinct final delivery-participating nodes | denominator 0 = null |
| recovery effectiveness | evolved plans reaching new generation/running/completed/failed / evolved plans | each stage has independent rate |
| time to recover | trigger to first new-generation start; trigger to stable progress | incomplete recovery = null duration |
| loop depth | consecutive generation transitions before stable progress/terminal outcome | G0 only = 0 |
| outcome by generation | terminal plans grouped by terminal generation | non-terminal excluded |
| stale/orphan residue | old-generation tasks left backlog/pool/running after successor starts | known empty = 0 |
| lineage integrity | plans with complete Issue→Task→generation→branch→SHA links / eligible delivered plans | denominator 0 = null |

## Delivery funnel and breaks

The four displayed stages are distinct Issues, derived Tasks, Plans containing
those tasks, and Done Plans with terminal tasks and closed/resolved issues.
These are diagnostic counts, not a lossy conversion percentage. Every click
uses the exact backend filter encoded in `DrilldownFilter`.

Exactly seven break kinds are versioned: `issue_without_task`,
`task_without_plan`, `task_multiple_containers`,
`done_plan_non_terminal_task`, `done_plan_open_issue`,
`evolution_old_generation_residue`, and
`delivery_sha_lineage_mismatch`. A task projected into Plan plus Backlog/Pool is
reported as `task_multiple_containers` and is not counted as normal backlog.

## Evolution and lineage

Evolution reasons are `blocked`, `review_reject`, `requirement_change`,
`execution_failure`, `manual_adjustment`, and `unknown`. Unknown remains visible
and lowers confidence; it is never silently folded into manual adjustment.
Trigger stage is the stage at the authoritative evolution event. Generation
lineage returns G0..Gn in ascending order, creator/trigger, evidence, node
changes, recovery duration/outcome, delivery branch/SHA and acceptance verdict.
Missing generations, impossible replacements, or a delivery SHA not belonging
to its generation produce `lineage.integrity_broken`.

## API surface

New endpoints are additive under `/api/orgs/{slug}/insights/v2`:

- `GET /overview?window=24h`
- `GET /agents` and `GET /agents/{agentRef}`
- `GET /projects` and `GET /projects/{projectId}`
- `GET /projects/{projectId}/delivery`
- `GET /projects/{projectId}/evolution`
- `GET /projects/{projectId}/plans/{planId}/lineage`
- `GET /executions?window=24h&agent_ref=...` or `project_id=...`
- `GET /executions/{executionId}` with inherited context query parameters

Execution list requires `agent_ref` or `project_id`; requests without context
are rejected with `400 execution_context_required`. All lists and drilldowns
accept and echo the same half-open time window, status/anomaly filters, and a
stable cursor. Returned metric and entity sets must originate from one filter
plan so displayed counts and clicked rows cannot diverge.

Go wire types are frozen in `internal/insight/contract_v2.go`.
`ProjectDeliveryResponse` and `PlanLineageResponse` demonstrate the common
envelope and nested shapes; S1 may add endpoint-specific rows only by composing
the same versioned primitives.

## Compatibility and evolution

Existing unversioned Insight v1 endpoints and TypeScript normalizers remain
unchanged during migration. v2 is additive and nullable by design. Additive
optional fields and new reason codes are backward compatible within v2;
renames, removals, unit/formula/state-set changes, or nullability changes require
a new metric version and endpoint version. Consumers must tolerate unknown
reason codes and fields but must not reinterpret them. v1 removal requires
production traffic evidence, one released deprecation window, and an explicit
migration task.

## Contract fixtures

`contract_v2_test.go` fixes ratio, empty/zero/unknown, health ownership, and the
seven funnel break values. S1 aggregation tests must add fixtures for every
table row above, including low coverage, stale data, unknown states, duplicate
task containers, old-generation residue, and mismatched SHA lineage.
