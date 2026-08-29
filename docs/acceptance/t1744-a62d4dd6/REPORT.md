# T1744 Independent Reverification: a62d4dd6

Date: 2026-08-29

## Verdict

REJECT fail-closed.

The implementation candidate appears to contain both requested cumulative parts:

- T1711 state-machine completion repair in `internal/projectmanager/service/plan_completion.go`.
- T1712 orphan-condition migration command and 52-case migration coverage in `internal/cli/handlers_migrate_orphan_conditions.go` and `internal/cli/handlers_migrate_orphan_conditions_test.go`.

However, the acceptance contract requires a complete dynamic matrix with no RED and a self-contained task-input package. Both gates failed:

- `task-input/v1/README.md` and `task-input/v1/manifest.json` were absent from the executor workspace.
- `go test ./...` returned RED in `github.com/oopslink/agent-center/tests/e2e`: `TestDeployedBinaryRuntimeVersion_AdoptedOldRuntimeSkewFails` failed with an old-runtime PID mismatch.

The failing e2e test passed on a focused rerun, but the contract says any RED or evidence gap fails closed.

## Fixed SHAs

- Base / refreshed `origin/main`: `f61c3110eb830f544b87b85d4d4d94e90633a0d5`
- Reviewed candidate: `a62d4dd6aa735c997076b4322768c99760726e8e`
- Candidate remote refs:
  - `origin/immutable/t1711-t1712-state-machine-migration-a62d4dd6`
  - `origin-push/immutable/t1711-t1712-state-machine-migration-a62d4dd6`
  - `origin-push/ac-exec/task-1a0b6739/exec-ed392910`

## Ancestry And Remote Readback

Commands executed:

```sh
git fetch --prune origin-push
git fetch origin main:refs/remotes/origin/main
git ls-remote origin-push refs/heads/immutable/t1711-t1712-state-machine-migration-a62d4dd6 refs/heads/ac-exec/task-1a0b6739/exec-ed392910 refs/heads/main
git ls-remote origin refs/heads/main refs/heads/immutable/t1711-t1712-state-machine-migration-a62d4dd6
git merge-base origin/main a62d4dd6aa735c997076b4322768c99760726e8e
git rev-list --left-right --count origin/main...a62d4dd6aa735c997076b4322768c99760726e8e
git diff --name-status origin/main..a62d4dd6aa735c997076b4322768c99760726e8e
```

Observed:

```text
origin main: f61c3110eb830f544b87b85d4d4d94e90633a0d5
origin immutable candidate: a62d4dd6aa735c997076b4322768c99760726e8e
origin-push main: f61c3110eb830f544b87b85d4d4d94e90633a0d5
origin-push immutable candidate: a62d4dd6aa735c997076b4322768c99760726e8e
origin-push delivery alias: a62d4dd6aa735c997076b4322768c99760726e8e
merge-base(origin/main,candidate): f61c3110eb830f544b87b85d4d4d94e90633a0d5
ahead/behind origin/main...candidate: 0 3
```

Candidate diff surface:

```text
M internal/cli/arch_test.go
M internal/cli/build.go
A internal/cli/handlers_migrate_orphan_conditions.go
A internal/cli/handlers_migrate_orphan_conditions_test.go
M internal/projectmanager/service/plan_completion.go
M internal/projectmanager/service/plan_completion_test.go
```

## Dynamic Matrix

All commands ran in detached clean worktree `/tmp/t1744-candidate-a62d4dd6` at `a62d4dd6aa735c997076b4322768c99760726e8e`.

| Gate | Command | Result |
| --- | --- | --- |
| Candidate clean SHA | `git status --short --branch && git rev-parse HEAD && git rev-parse origin/main` | PASS: detached HEAD at candidate, no dirty output, origin/main fixed to base |
| State-machine focused | `go test ./internal/projectmanager/service -run 'Test(ActiveUnresolvedGraphConditions|PlanCompletion_)' -count=1 -v` | PASS |
| Migration focused | `go test ./internal/cli -run 'Test(MigrateOrphanConditions|Arch_NoDirectPersistenceOpenInHandlers)' -count=1 -v` | PASS |
| Focused race | `go test -race ./internal/projectmanager/service -run 'Test(ActiveUnresolvedGraphConditions|PlanCompletion_)' -count=1 -v` | PASS |
| Full Go / HTTP-adjacent suite | `go test ./...` | RED: `tests/e2e` failed |
| Failed-test rerun | `go test ./tests/e2e -run '^TestDeployedBinaryRuntimeVersion_AdoptedOldRuntimeSkewFails$' -count=1 -v` | PASS on rerun only |
| Integration | `go test ./tests/integration -count=1` | PASS |
| Focused HTTP/API plan surface | `go test ./internal/webconsole/api -run 'Test.*Plan|Test.*Progress|Test.*Gate|Test.*Condition|Test.*Completion' -count=1 -v` | PASS |
| Broader race package matrix | `go test -race -count=1 ./internal/cli ./internal/projectmanager/...` | INCOMPLETE: stopped after extended nonterminal run with no output |

`go test ./...` RED excerpt:

```text
--- FAIL: TestDeployedBinaryRuntimeVersion_AdoptedOldRuntimeSkewFails (24.65s)
    deployed_runtime_version_smoke_test.go:259: old runtime pid mismatch: health=8720 pidstore=8852
FAIL
FAIL github.com/oopslink/agent-center/tests/e2e 142.964s
FAIL
```

## Remediation Contract

1. Rematerialize `task-input/v1/README.md`, `task-input/v1/manifest.json`, and all declared attachments into the executor workspace before rerunning T1744.
2. Reproduce the full-suite e2e failure from a clean detached candidate worktree:

   ```sh
   git fetch origin main refs/heads/immutable/t1711-t1712-state-machine-migration-a62d4dd6:refs/remotes/origin/immutable/t1711-t1712-state-machine-migration-a62d4dd6
   git worktree add --detach /tmp/t1744-remediate-a62d4dd6 origin/immutable/t1711-t1712-state-machine-migration-a62d4dd6
   cd /tmp/t1744-remediate-a62d4dd6
   go test ./tests/e2e -run '^TestDeployedBinaryRuntimeVersion_AdoptedOldRuntimeSkewFails$' -count=20 -v
   go test ./...
   ```

3. Fix or quarantine the runtime-version PID race only with a production-equivalent assertion that proves old-runtime skew detection still works.
4. Rerun the complete matrix, including `go test ./...` and a bounded race suite, and persist raw logs plus structured verdict on a pushed evidence branch.
