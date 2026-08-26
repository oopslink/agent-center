# Insight Phase 1: 24-hour capacity and execution-efficiency contract

Status: **frozen for Phase 1**  
Plan: `plan-3be87045`  
Contract task: T1575 (`task-bc0c490e`)  
Frozen source baseline: `origin/main@2ce603b125594cb72369ed31ec871a946d7d4e3e`  
Window: fixed trailing 24 hours

This document is the implementation and acceptance contract for S2-S6. A change
to a formula, join key, timestamp, schema grain, endpoint, or freshness rule is a
contract change and must go back through the S1 gate. It must not be inferred
independently by the projector, API, UI, or test fixture.

## 1. Boundary and ownership

SQLite and the agent runtime remain the business facts and control plane. DuckDB
is a disposable, single-writer analytical read model:

- no scheduler, task, Plan, worker, Agent, or runtime state is written from
  DuckDB;
- deleting the DuckDB file and replaying the retained SQLite/runtime observation
  sources must reproduce the same facts and aggregates;
- one in-process projector owns all DuckDB writes. HTTP handlers only read;
- an unavailable/rebuilding DuckDB degrades Insight only and must not block task
  dispatch, heartbeat ingestion, execution finalization, or recovery;
- Phase 1 is organization-scoped and fixed to the trailing 24 hours. It does not
  include Fleet live state, alerts, arbitrary date ranges, cost, tokens, or
  complex filters.

The existing per-Agent usage analytics (`internal/usage/analytics.go`) is a
separate product surface. Its daily usage/cost rollups and task-completion count
must not be reused as execution throughput or failure-rate facts.

## 2. Production source chain and canonical keys

The projector adapters must consume the real production writes below. Tests may
seed these tables through their owning services, but production code must not
invent a second execution model.

| Analytical input | Authoritative production source | Event identity |
|---|---|---|
| execution lifecycle | `agent_activity_events` rows whose `interaction_ref = 'executor:' || execution_id`; payload events `executor.start`, `executor.stop`, and recovery terminal events | `agent_activity_events.id` |
| queue lifecycle | `worker_control_events` where `command_type='agent.fork_executor'` (the existing fork command constant), including `created_at`, status fields, `execution_id`, worker/agent/task | `queue:` + command id + `:` + status + `:` + normalized `status_updated_at` |
| slot observations | the exact `concurrency.AgentSnapshot` accepted by the center heartbeat path, persisted before/alongside updating the in-memory latest-value store | ULID observation id; one id covers the whole Agent snapshot |
| task/project dimensions | `pm_tasks.id/project_id/org_number/title` at projection time | task id |
| project/org dimensions | `pm_projects.id/organization_id/name` at projection time | project id |

Canonical joins:

- `execution_id`: executor id after a fork command starts. It is the execution
  fact primary key. A pre-start command is not renamed into a fake execution;
  `command:<id>` remains an API compatibility presentation only.
- `command_id`: `worker_control_events.id`; queue fact primary key.
- `task_id`: `worker_control_events.task_id` and execution activity `task_ref`.
- `agent_ref`: canonical `agent:<agent_id>` form. Runtime/activity bare IDs are
  normalized exactly once at adapter ingress.
- `project_id`: snapshot from `pm_tasks.project_id`; `organization_id`: snapshot
  from the project. The snapshots are retained even if the task/project is later
  archived or deleted. An unresolved historical join is retained with nullable
  dimensions and counted only in the organization when that organization is
  known; it must never be attributed to a guessed project.
- `worker_id` and `slot_index` together identify a physical runtime slot. Agent
  reassignment or a daemon restart does not merge slots across workers.

The heartbeat adapter must durably append slot observations in SQLite (or the
existing append-only observability `events` table) before acknowledging them to
the projector. The in-memory `concurrency.LiveStateStore` is a latest snapshot,
not a replay source. Directly writing heartbeat data only to DuckDB is forbidden.

## 3. Time semantics

All timestamps are UTC RFC3339Nano at boundaries and DuckDB `TIMESTAMPTZ`
internally. The API captures one `as_of` instant per request and defines the
half-open window:

```text
window_start = as_of - 24 hours
window_end   = as_of
membership  = timestamp >= window_start AND timestamp < window_end
```

There is no local-calendar or “today” interpretation.

- `queued_at`: fork command `created_at`, when the center durably accepted the
  execution request. Plan creation, task assignment, and Plan dispatch timestamps
  are not queue start.
- `started_at`: `executor.start` activity `occurred_at`. A command status becoming
  `started` is control-plane acknowledgement, not execution start; it is only a
  fallback telemetry-gap marker and is excluded from latency percentiles.
- `finished_at`: terminal execution activity `occurred_at`: `executor.stop`, or
  the recovery event that produces the durable `quiet_finalized` terminal state.
- `observed_at`: source ingestion time (`created_at` where present, otherwise the
  center receive time). It is for freshness/late-arrival diagnostics only and is
  never substituted for business time.
- `refreshed_at`: commit time of the last successful DuckDB projector transaction.

Negative intervals indicate corrupt/clock-skewed input. Preserve the fact with
`quality='invalid_time_order'`, exclude it from percentile/utilization arithmetic,
and surface it in diagnostics; never clamp it to zero.

## 4. Frozen metrics

All aggregate metrics use the same request `as_of` and organization scope.

### 4.1 Completed executions

`completed_executions` is the count of terminal **execution attempts** whose
`finished_at` is in the window. It is not completed Tasks and is not deduplicated
by task. Retries are separate attempts.

Terminal outcomes are `succeeded`, `failed`, `crashed`, and `quiet_finalized`.
Pending/running/spawned commands are not completed.

### 4.2 Failure rate

```text
failed_executions = count(outcome IN ('failed','crashed','quiet_finalized')
                          AND finished_at in window)
failure_rate      = failed_executions / completed_executions
```

`succeeded` is the only success outcome. Non-delivery that is classified by the
runtime as crashed remains a failure. When the denominator is zero, JSON returns
`failure_rate: null` (not zero) and the UI renders `—`.

### 4.3 Queue wait p50/p95

For each command linked to a real `executor.start`:

```text
queue_wait_ms = started_at - queued_at
```

The sample belongs to the window by `started_at`. Pending, rejected, failed, and
expired commands without a real executor start are visible in drill-down but are
excluded from percentiles. Duplicate status updates do not create samples.

### 4.4 Execution duration p50/p95

For each terminal execution:

```text
execution_duration_ms = finished_at - started_at
```

The sample belongs to the window by `finished_at`. A terminal fact lacking a real
start remains visible with null duration and is excluded from percentiles.

### 4.5 Percentile algorithm

Use DuckDB continuous quantiles, equivalent to
`quantile_cont(value, 0.50)` and `quantile_cont(value, 0.95)`, over valid non-null
samples. Round the final API values to the nearest integer millisecond. Empty
sample sets return null. Fixtures must test interpolation with a non-trivial
ordered set; nearest-rank implementations are not equivalent.

### 4.6 Slot utilization

Heartbeat snapshots become non-overlapping slot intervals. For each observation,
close the preceding open interval for the same `(worker_id, agent_ref, slot_index)`
at the new snapshot's `observed_at`; then open the new state. A repeated identical
snapshot still advances the boundary/freshness but must coalesce into the existing
interval when safe.

Only slots with `slot_index < admission_cap` are admissible denominator capacity.
`draining` slots are excluded from the denominator. Occupied states are
`starting`, `running`, `finishing`, and `orphan`; `idle` is unoccupied. `unknown`,
missing snapshots, degraded-integrity snapshots, and intervals after the heartbeat
freshness TTL are unknown coverage and excluded from numerator and denominator.

```text
occupied_slot_ms = sum(intersection(interval, window) for occupied admissible slots)
available_slot_ms = sum(intersection(interval, window) for all known admissible slots)
slot_utilization = occupied_slot_ms / available_slot_ms
slot_coverage_ratio = available_slot_ms /
                      integral(configured admissible capacity over the window)
```

If `available_slot_ms=0`, utilization is null. UI must show coverage and freshness;
it must not present a low-coverage utilization value as fully representative.

## 5. DuckDB read-model schema

Names and grains are frozen; S2 may add indexes or internal columns without
changing wire semantics.

```sql
CREATE TABLE execution_fact (
  execution_id VARCHAR PRIMARY KEY,
  command_id VARCHAR,
  organization_id VARCHAR,
  project_id VARCHAR,
  task_id VARCHAR,
  agent_ref VARCHAR NOT NULL,
  worker_id VARCHAR,
  cli VARCHAR,
  model VARCHAR,
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  outcome VARCHAR,
  failure_reason VARCHAR,
  recovered BOOLEAN NOT NULL DEFAULT false,
  quality VARCHAR NOT NULL DEFAULT 'valid',
  first_event_id VARCHAR NOT NULL,
  last_event_id VARCHAR NOT NULL,
  observed_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE queue_interval_fact (
  command_id VARCHAR PRIMARY KEY,
  execution_id VARCHAR,
  organization_id VARCHAR,
  project_id VARCHAR,
  task_id VARCHAR,
  agent_ref VARCHAR NOT NULL,
  worker_id VARCHAR NOT NULL,
  queued_at TIMESTAMPTZ NOT NULL,
  started_at TIMESTAMPTZ,
  command_status VARCHAR NOT NULL,
  status_reason VARCHAR,
  quality VARCHAR NOT NULL DEFAULT 'valid',
  last_event_id VARCHAR NOT NULL,
  observed_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE slot_interval_fact (
  worker_id VARCHAR NOT NULL,
  agent_ref VARCHAR NOT NULL,
  slot_index INTEGER NOT NULL,
  valid_from TIMESTAMPTZ NOT NULL,
  valid_to TIMESTAMPTZ,
  state VARCHAR NOT NULL,
  occupied BOOLEAN NOT NULL,
  admissible BOOLEAN NOT NULL,
  execution_id VARCHAR,
  task_id VARCHAR,
  integrity VARCHAR,
  source_event_id VARCHAR NOT NULL,
  PRIMARY KEY (worker_id, agent_ref, slot_index, valid_from)
);

CREATE TABLE projected_event (
  source_event_id VARCHAR PRIMARY KEY,
  source_kind VARCHAR NOT NULL,
  source_cursor VARCHAR,
  occurred_at TIMESTAMPTZ NOT NULL,
  projected_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE projector_checkpoint (
  source_kind VARCHAR PRIMARY KEY,
  source_cursor VARCHAR NOT NULL,
  refreshed_at TIMESTAMPTZ NOT NULL,
  state VARCHAR NOT NULL,
  last_error VARCHAR
);
```

`projected_event` and fact changes for one source event commit in the same DuckDB
transaction. Checkpoint advancement is in that transaction. A crash before commit
replays the event; a crash after commit sees the event id and no-ops.

## 6. Replay, lateness, and rebuild

- Event idempotency is by namespaced `source_event_id`, never by payload hash.
- Arrival order is irrelevant. Upserts compare business timestamps and event ids;
  an earlier start arriving after stop fills the missing start and recomputes the
  duration.
- Source adapters use an overlap scan of at least 48 hours on restart/incremental
  polling, plus event-id deduplication. A timestamp cursor alone is forbidden
  because late events can have older `occurred_at`.
- Facts are stored outside the current window. Window membership is evaluated at
  query time, so crossing midnight or aging out requires no destructive update.
- Retained SQLite source history must cover at least the 24-hour window plus the
  48-hour overlap/recovery horizon. GC must not delete source rows earlier.
- Full rebuild: stop reads or report `rebuilding`; create a new DuckDB file,
  replay all retained source rows/observations, validate invariants, atomically
  swap it in, then report fresh. The old file is never partially rewritten.
- Checkpoint corruption or DuckDB open/migration failure triggers rebuild; it does
  not mutate or pause the control plane.
- Schema compatibility is explicit `schema_version`. Phase 1 supports only the
  current version; incompatible files rebuild instead of best-effort ALTERs.

Required automated cases: duplicate event, stop-before-start, late event inside
the window, event crossing out of the window as `as_of` advances, exact boundary
inclusion/exclusion, duplicate heartbeat, admission-cap change, stale heartbeat
gap, full replay equality, crash before/after fact commit, checkpoint restart,
and invalid time ordering.

## 7. HTTP API contract

Authorization is organization membership plus existing Insight read permission;
cross-org dimensions must never leak. Phase 1 has no caller-provided time range.

```http
GET /api/orgs/{slug}/insights/overview?window=24h
GET /api/orgs/{slug}/insights/executions?window=24h&agent_ref=...&project_id=...&cursor=...&limit=50
```

`window` is required to be absent or exactly `24h`; other values return 400.
Overview response (field names and null behavior are frozen):

```json
{
  "window": {"kind":"rolling","duration":"24h","start":"...","end":"..."},
  "as_of":"...",
  "refreshed_at":"...",
  "freshness":{"state":"fresh|stale|rebuilding|unavailable","age_ms":0,"threshold_ms":0},
  "summary": {
    "completed_executions":0,
    "failed_executions":0,
    "failure_rate":null,
    "slot_utilization":null,
    "slot_coverage_ratio":null,
    "queue_wait_ms":{"p50":null,"p95":null,"samples":0},
    "execution_duration_ms":{"p50":null,"p95":null,"samples":0}
  },
  "agents": [],
  "projects": [],
  "diagnostics":{"invalid_facts":0,"late_events":0}
}
```

Agent leaderboard rows contain `agent_ref`, display name (nullable), and the same
summary shape. Project rows contain `project_id`, name (nullable), and the same
shape. Sort by `completed_executions DESC`, then stable id ascending. Empty data is
200 with zeros/nulls and empty lists, not 404.

Execution drill-down rows contain `execution_id`, `command_id`, task id/ref/title,
agent ref/name, project id/name, worker id, outcome, failure reason, queued/start/
finish timestamps, queue wait ms, duration ms, recovered, quality. Default order is
`COALESCE(finished_at, started_at, queued_at) DESC, execution_id DESC`. Cursor is an
opaque encoding of those keys; limit is 1..100. Filters are exact-match and remain
within the fixed organization/window. The response repeats `window`, `as_of`,
`refreshed_at`, and `freshness` so a drill-down cannot hide stale data.

Freshness threshold is the configured heartbeat/projector SLA and is returned as
`threshold_ms`. `stale` data remains readable with a visible banner. `rebuilding`
or `unavailable` returns 503 with the same freshness envelope and a stable error
code; the UI must not silently render old zeros.

## 8. UI contract

Route: organization `Insight > Overview`.

- always show “Past 24 hours”, exact window start/end, `refreshed_at`, and a
  fresh/stale/rebuilding/unavailable indicator;
- summary cards: completed executions, failure rate, slot utilization, queue wait
  p50/p95, execution duration p50/p95;
- utilization card also shows slot coverage; failure/percentile nulls render `—`
  with an explanatory empty-denominator tooltip;
- Agent and Project leaderboards use the frozen sort and open the execution table
  with the corresponding exact filter;
- execution rows open TaskExecution detail without replacing ids with task state;
- loading, empty, stale, rebuilding, unavailable, and authorization-error states
  are distinct and testable;
- UI performs no metric calculation beyond duration formatting and percentages;
  API values are authoritative.

## 9. Acceptance gates

S2 implementations must cite this baseline and contract. S3 records the exact
upstream SHAs and demonstrates that projector, API, UI, and fixture implement the
same formulas. S4 tests only the single integrated candidate through the real
HTTP/UI entry. S5 may merge only an accepted candidate. S6 independently proves
the accepted result is reachable from remote `origin/main`; branch push, green
tests, or node `done` alone are not completion.
