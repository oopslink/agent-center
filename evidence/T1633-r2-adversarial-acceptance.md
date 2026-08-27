# T1633 R2 adversarial acceptance

- Verdict: PASS
- Reviewed SHA: `84a000f43121c43ff27c47d61d10fb729f0b2f5f`
- Immutable ref: `origin/candidate/t1631-phase0-84a000f4`
- Baseline and merge-base: `origin/main@ca953dbd2a6b58e04b542d62a1a2ea196d278f68`
- Candidate contains the rejected-candidate remediation chain including
  `aa6ca4b9`; diff-check is clean.

## Adversarial matrix

- Stale controller returns `ErrProgressFenceStale` and creates no Observation,
  Obligation, or Incident.
- Repository-layer fence validation and control-plane writes remain in the
  same immediate transaction, preventing a takeover from entering the
  validate/write window.
- `source_recovery` closes after an authoritative fact and does not leak or
  duplicate an open obligation.
- Torn-read and watermark-lag paths remain fail-closed; watchdog handles stale
  and missing heartbeats independently.
- `progress_hold` gates fresh dispatch and acceptance while preserving
  in-flight resume semantics; the second clock escalates without dispatch.
- Default wake preserves the P0 reserve, refill resumes delivery, and the
  1000-plan suppressed lane aggregates and drains exactly once.
- The previously timing-out action-log block/unblock test passes as part of the
  same race x10 selection.
- `task-input/v1` materialization, fail-closed download, and retry replacement
  pass race x10.

## Commands and results

```text
go test -race ./internal/projectmanager/service \
  -run '<action-log + fencing + recovery + watchdog + hold + wake selection>' \
  -count=10 -timeout=20m
PASS (94.882s)

go test -race ./internal/agentruntime \
  -run '<task-input/v1 selection>' -count=10 -timeout=10m
PASS (2.131s)

go test -race ./internal/projectmanager/sqlite -count=1 -timeout=15m
PASS (258.506s)

go test -race ./internal/projectmanager/service -count=1 -timeout=20m
PASS (80.981s)

go test ./internal/projectmanager/... ./internal/agentruntime/... \
  -count=1 -timeout=15m
PASS (all packages)
```

The candidate was not modified or merged to main. This branch contains only
the durable acceptance evidence.
