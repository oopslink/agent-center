# T1626 Independent Verification Report

Verdict: **REJECT**

## Subject

- Candidate ref: `origin/candidate/t1625-phase0-aa6ca4b9`
- Locked SHA: `aa6ca4b9004007502ed39a7993294404fb322b5f`
- Fresh `origin/main`: `16dc58155dfa0cafd79c08595c8a8e378b5eede9`
- Final readback: remote candidate still equals locked SHA; `origin/main` is an ancestor of candidate.
- Candidate worktree state during verification: detached at locked SHA; no candidate edits.

## Result Matrix

| Gate | Result | Evidence |
| --- | --- | --- |
| Fresh fetch / exact remote ref / ancestry | PASS | `raw/00-provenance.log`, `raw/90-final-remote-readback.log` |
| Task input package present | FAIL | `task-input/v1` was absent in workspace; captured by empty package search in `raw/00-provenance.log` |
| Candidate-specific `go test -race -count=10 ./internal/projectmanager/service` | FAIL | `raw/10-projectmanager-service-race-count10.log`, rc `1` |
| Focused S2A/S2B semantics | PASS | `raw/20-focused-semantics.log`, rc `0` |
| Repository established race gate | NOT RUN | Stopped after candidate-specific blocker |
| Full Go suite | NOT RUN | Stopped after candidate-specific blocker |
| Web gate | NOT RUN | Stopped after candidate-specific blocker |

## Blocker

The candidate-specific race gate timed out:

```text
panic: test timed out after 10m0s
running tests:
    TestActionLog_BlockUnblockPersisted (0s)
FAIL github.com/oopslink/agent-center/internal/projectmanager/service 600.569s
```

This directly violates the required `projectmanager/service go test -race -count=10` no-timeout gate. Under the task contract, this is a REJECT regardless of the focused semantic subset.

## Focused Semantic Readback

The focused non-race command passed:

```text
go test ./internal/projectmanager/service ./internal/projectmanager/sqlite \
  -run "TestProgressControl_(StaleFenceConflictPersistsWithoutMutation|ConcurrentFenceTakeoverRejectsStaleWriter|TornReadDisappearanceRequiresSecondConfirmation|SourceRecoveryClosesObligationAfterAuthoritativeFact)|TestDeriveProgressRequiredActions_|TestS2B_" \
  -count=1
```

Covered behaviors:

- stale fence takeover rejects the old controller and creates no stale observation or lease-fence incident;
- repository `SaveProgressEvaluation` revalidates the fence before writes;
- torn-read suspect creates one named `source_recovery` obligation;
- authoritative recovery fact closes the source-recovery obligation on re-evaluation;
- required actions derive only from authoritative obligations/incidents/holds, not status-only third states;
- S2B DeliverySubject/Acceptance/Gate rejects wrong SHA, missing candidate, missing push, moving ref, bad ancestry, and unauthorized verdict.

## Evidence SHA256

```text
ee2d5ee4d79c3989bbcbd58679e17acf7e42a2ff889d73c811a37098fcc76915  raw/00-provenance.log
9a271f2a916b0b6ee6cecb2426f0b3206ef074578be55d9bc94f6f3fe3ab86aa  raw/00-provenance.rc
64088e8ce8a813d2c06c402c121dcd548205f8d670cdf711a1023dd701311a39  raw/10-projectmanager-service-race-count10.log
4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865  raw/10-projectmanager-service-race-count10.rc
d4db1091af96d9255425a506ba25c12789a1eb2abd60c3e5bbaae85b3c25b8b3  raw/20-focused-semantics.log
9a271f2a916b0b6ee6cecb2426f0b3206ef074578be55d9bc94f6f3fe3ab86aa  raw/20-focused-semantics.rc
ff7b1b3c480614c3bbc9f080f5abd56134b32c6b58bcafb1cbda87b43e549aac  raw/90-final-remote-readback.log
9a271f2a916b0b6ee6cecb2426f0b3206ef074578be55d9bc94f6f3fe3ab86aa  raw/90-final-remote-readback.rc
```
