# T1578 S3 Insight Phase 1 Integration Report

Date: 2026-08-27

Base: `origin/main` at `b66fe30eb3c3d5bbcedda4ef711150d391f67b81`

Delivery branch: `delivery/t1578-s3-insight-phase1-candidate`

## Upstream Coverage Matrix

| Upstream | Role | Coverage decision |
| --- | --- | --- |
| `16a4120322f23007511d4609d0cb64d5982d0600` | S1 frozen Insight read model baseline | Already in `origin/main`; also ancestor of both integration lines. |
| `448ac450aa786486930e37397b83968e0685e452` | T1592 verified remote HEAD | Merged directly; independent from `968dd761`. |
| `738bc0a6769b413dd4d04c6834207c62c2918fae` | T1592 frozen harness actual SHA | Ancestor of `968dd761`; not merged separately. |
| `968dd76157d4b79755cc59a23163bbffbb1e5dc7` | T1616 remediation PASS | Merged directly; includes `738bc0a6` and crash seam lineage. |
| `0f53752a` | Crash seam candidate | Ancestor of `968dd761`; not merged separately. |

Remote verification after push:

```text
16a4120322f23007511d4609d0cb64d5982d0600 ancestor_remote=0
448ac450aa786486930e37397b83968e0685e452 ancestor_remote=0
738bc0a6769b413dd4d04c6834207c62c2918fae ancestor_remote=0
968dd76157d4b79755cc59a23163bbffbb1e5dc7 ancestor_remote=0
0f53752a ancestor_remote=0
```

## Conflict Decisions

Merge order:

1. `968dd76157d4b79755cc59a23163bbffbb1e5dc7` into `origin/main`: no conflicts.
2. `448ac450aa786486930e37397b83968e0685e452` into the candidate: one content conflict in `internal/insight/service_test.go`.

Resolution:

- Kept the T1616 restart/crash recovery tests and projector lifecycle behavior.
- Kept the T1592 admission-cap/heartbeat TTL test intent.
- The only manual conflict was a test comment around `TestInsightSlotObservation_AdmissionCapOnlyChangeClosesCapacityInterval`; no production logic was manually rewritten during conflict resolution.

Final diff against `origin/main` is limited to:

```text
internal/insight/helpers.go
internal/insight/service.go
internal/insight/service_test.go
internal/insight/types.go
internal/webconsole/api/handlers_insights.go
internal/webconsole/api/handlers_insights_test.go
internal/webconsole/api/server.go
docs/plans/reports/t1578-s3-insight-phase1-integration.md
```

## Verification Evidence

Passed:

```text
go test ./internal/insight ./internal/webconsole/api
ok github.com/oopslink/agent-center/internal/insight 2.620s
ok github.com/oopslink/agent-center/internal/webconsole/api 70.287s

go test ./internal/persistence ./internal/cli -run 'Migration|Migrate|migrat|RoundTrip|Schema|Upgrade' -count=1
ok github.com/oopslink/agent-center/internal/persistence 60.347s
ok github.com/oopslink/agent-center/internal/cli 7.785s

pnpm --dir web install --frozen-lockfile && pnpm --dir web test
Test Files 192 passed (192)
Tests 1809 passed (1809)

make build
tsc -b, vite build, go build ./cmd/agent-center, and go build ./cmd/fakeagent all exited 0.
```

Full backend sweep:

```text
go test ./...
```

Result: one failure in existing `internal/admin/api` test `TestTaskInputPlan569_RealAdminHandlersEndToEnd`. The failure path attempted to load team-rule index via local agent-center tooling in this isolated executor environment and then failed executor writeback after temp executor cleanup. The candidate changes do not touch `internal/admin/api`; all listed non-admin packages in the full sweep passed, including `internal/insight`, `internal/webconsole/api`, `internal/persistence`, `tests/e2e`, and `tests/integration`.

Targeted rerun:

```text
go test ./internal/admin/api -run TestTaskInputPlan569_RealAdminHandlersEndToEnd -count=1 -v
```

Result: reproduced the same environment/tooling-dependent failure with `team service is not wired on this center` and missing temp executor `input.json`.
