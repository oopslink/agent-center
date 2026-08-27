# T1627 adversarial retest

- Candidate: `aa6ca4b9004007502ed39a7993294404fb322b5f`
- Candidate ref: `origin/candidate/t1625-phase0-aa6ca4b9`
- Baseline: `origin/main` at `16dc58155dfa0cafd79c08595c8a8e378b5eede9`
- Merge-base: `16dc58155dfa0cafd79c08595c8a8e378b5eede9`
- Worktree: isolated, detached, clean before evidence creation

## Result

PASS. No blocking failure was observed on the immutable candidate.

## Fencing / zero stale writes

The candidate removes the former out-of-band `context.Background()` conflict
incident write. A stale holder returns `ErrProgressFenceStale` without creating
an Observation, Obligation, or Incident.

`SaveProgressEvaluation` revalidates holder, fencing token, and plan revision
using the transaction-carried SQL executor before any control-plane mutation.
The production SQLite DSN uses `_txlock=immediate`; therefore the evaluation
transaction owns the writer slot before fence validation. A takeover cannot
commit between that validation and the Observation/Obligation/Incident writes.
After the transaction releases the writer slot, the takeover advances the
token; subsequent old-holder evaluation is rejected. This closes the former
check/write TOCTOU window rather than merely adding an earlier service check.

The focused stale/takeover suite passed under the race detector for ten
repetitions. It asserts zero stale Observation and zero
`lease_fence_conflict` Incident.

## Recovery and progress clocks

- `source_recovery` is created once during suspect classification and is
  resolved after an authoritative dispatch fact; no open obligation remains.
- Watermark lag reaches `cannot_determine` with a deduplicated incident.
- The independent watchdog handles stale and missing heartbeat fail-closed.
- `progress_hold` gates new dispatch/acceptance and its second clock escalates
  without dispatch.

## Wake stability

- Default wake respects the P0 reserve; P0 remains deliverable.
- Refill resumes delivery.
- A 1000-plan storm aggregates into one durable suppressed lane and drains
  exactly once, leaving no due intent.

## Commands

```text
go test ./internal/projectmanager/service ./internal/projectmanager/sqlite \
  -run '<focused T1627 selection>' -count=1
PASS (service 2.263s; sqlite 0.914s)

go test -race ./internal/projectmanager/service ./internal/projectmanager/sqlite \
  -run '<focused T1627 selection>' -count=10 -timeout=15m
PASS (service 639.664s; sqlite 1.934s)

go test ./internal/projectmanager/... ./internal/environment/service \
  -count=1 -timeout=15m
PASS (all packages; service 42.614s, sqlite 9.584s, environment 17.827s)
```

Focused selection covered stale conflict, concurrent takeover, watchdog
independence/stale/missing heartbeat, P0 reserve/refill, 1000-plan suppression
and drain, watermark lag, source recovery, and progress hold tests.
