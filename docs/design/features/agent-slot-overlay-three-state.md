# Agent slot overlay: three-state freshness (T606 / issue-af03da2f)

## Symptom

The agent detail → Tasks tab concurrency overlay showed
`?/3 · overlay stale · ⚠️ snapshot 0s ago · worker unreachable` permanently, even
when the agent and its worker were online. "worker unreachable" was a lie.

## Root cause

The handler `GET /agents/{id}/concurrency` collapsed three distinct situations into
one `stale` boolean, and the FE rendered all of them as "worker unreachable":

1. the bound worker is genuinely **offline**;
2. a snapshot exists but has aged past the TTL (**expired** / last-known);
3. the agent **never reported a snapshot** at all (the actual case here).

Case 3 happens because the per-agent live snapshot is only emitted by the worker
when the executor engine `ma.exec` is attached, which requires the reconcile payload
to be concurrency-enabled (`MaxConcurrentTasks>0 && AllowedExecutors non-empty`). The
agent's *persisted* profile is concurrency-enabled (overlay cap=3 proves it —
`EffectiveConcurrencyCap` returns 1 otherwise), but the *running session* has a stale
profile (see 治本 below), so `ma.exec` is nil and no snapshot is ever sent →
`LiveState.Get` returns `found=false` → `stale=true, age=0` → "0s ago, unreachable".

## 治标 (shipped) — three-state handler + UI

Handler (`handlers_agent_concurrency.go`) now also returns:

- `reachable` — is the bound worker ONLINE? (looked up via `WorkerRepo`; defaults
  true, only a found+offline worker flips it false — a missing worker record never
  fabricates "offline").
- `has_snapshot` — has this agent ever reported a live snapshot (`found`)?

`stale` is retained as the coarse "live view not usable" flag for back-compat.

FE (`AgentTasks.tsx` `concurrencyMode`) renders four states:

| Mode | Condition | UI |
|---|---|---|
| `live` | fresh snapshot | `N/cap · slots in use · updated Ns ago` (neutral) |
| `offline` | `reachable=false` | `—/cap · worker offline` (amber) — the only worker-blaming state |
| `expired` | `has_snapshot && stale` | `—/cap · snapshot Ns ago · last known` (amber) |
| `nodata` | `!has_snapshot` (worker online) | `—/cap · no real-time slot data · concurrency not active` (**neutral**, no longer "unreachable") |

`reachable`/`has_snapshot` are optional on the FE type so a pre-T606 Center degrades
to the legacy live-vs-stale split.

## 治本 (NOT a code fix here — operational + blocked) — finding

Why the running session has a stale (non-concurrent) profile: `UpdateAgentConfig`
(which sets `MaxConcurrentTasks`/`AllowedExecutors`) is **persist-only — it emits no
reconcile** by deliberate contract ("a pure config write must not enqueue a reconcile;
the change applies on the next spawn, so the UI pairs it with a restart"). The
lifecycle-event → reconcile chain DOES carry the concurrency fields correctly
(`agent/service.emit` reads them from the persisted profile, and the
`AgentControlProjector` passes them through) — verified, **no payload bug**. So:

- **Restarting** the affected agent re-emits `lifecycle_changed` with the now-enabled
  profile → the worker attaches `ma.exec` → it reports `active:0` snapshots even with
  zero executors → the overlay flips to `0/cap · slots in use · updated Ns ago`. This
  is an operator action, not a code change.
- Making the agent **truly fork executors** (so `active>0`) needs the live
  `work_available → daemon fork` trigger, which is the blocked W4a work-stream
  (escalated architectural decision), not in scope here.
- **Executor-emitted usage with a bound `task_id`** (issue ① / this task's AC#3) is a
  separate, unbuilt feature: the executor process emits no usage events at all today
  (the `executor` package has zero usage code). The source binding it would use
  already exists (`input.json Source.TaskRef`).

Pending PD/oopslink decision on the 治本 path (restart the agents / auto-reconcile on
concurrency-profile edits / defer real-concurrency + executor-usage to the W4a track).

## Tests

- Go: `TestAPI_AgentConcurrency_OnlineNoSnapshot_NoData` (online + no snapshot →
  `has_snapshot=false, reachable=true`), `..._WorkerOffline_NotReachable`
  (`reachable=false`); existing join/stale tests unchanged.
- FE: `AgentTasks.test.tsx` — expired (last-known, not "unreachable"), worker offline,
  and neutral no-data states. `tsc --noEmit` clean.
