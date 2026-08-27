# Pre-main Independent Acceptance: 58626ffe

Verdict: **REJECT**

Reviewed candidate: `origin/s3/phase0-candidate-58626ffe` at `58626ffebe569bc04daf10e5f195ba57b8f181b9`.

## Scope

Independent evidence-only acceptance for:

- continuous progress invariants
- `human_decision` obligation and auto re-evaluation
- stale/old projection clearing
- `DeliverySubject` / `Acceptance` fail-closed behavior
- gate evidence integrity

The task input package requested by the task (`task-input/v1/README.md`, `task-input/v1/manifest.json`) was not present in this workspace. I proceeded from the frozen remote ref and candidate-local tests/contracts.

## Source Integrity

- Fresh fetch command: `git fetch origin +refs/heads/s3/phase0-candidate-58626ffe:refs/remotes/origin/s3/phase0-candidate-58626ffe +refs/heads/main:refs/remotes/origin/main --prune`
- Candidate ref resolved to: `58626ffebe569bc04daf10e5f195ba57b8f181b9`
- Expected SHA: `58626ffebe569bc04daf10e5f195ba57b8f181b9`
- Initial fetched `origin/main` before test execution: `b66fe30eb3c3d5bbcedda4ef711150d391f67b81`
- Final fresh-fetched `origin/main` after test execution: `16dc58155dfa0cafd79c08595c8a8e378b5eede9`
- Final merge-base: `b66fe30eb3c3d5bbcedda4ef711150d391f67b81`
- Final `origin/main` is ancestor of candidate: no
- Final candidate is ancestor of `origin/main`: no
- Candidate verification worktree: detached HEAD at `58626ffebe569bc04daf10e5f195ba57b8f181b9`
- Candidate tracked status after verification: clean

## Code Evidence Reviewed

- Progress fence/watchdog/backpressure: `internal/projectmanager/service/progress_control_s2d.go`, `internal/projectmanager/service/progress_control_loop.go`, `internal/projectmanager/sqlite/progress_control_repo.go`
- Human decision obligation and prerequisite wake: `internal/projectmanager/service/progress_control.go`, `internal/projectmanager/service/progress_human_decision_contract_test.go`
- Old projection clearing: `internal/projectmanager/service/plan_orchestration_graph.go`, `internal/projectmanager/service/progress_human_decision_contract_test.go`
- Delivery subject and acceptance fail-closed path: `internal/projectmanager/delivery.go`, `internal/projectmanager/service/delivery_acceptance.go`, `internal/projectmanager/service/delivery_acceptance_s2b_test.go`
- API/web gate evidence: `internal/admin/api/progress_control_contract_test.go`, `internal/webconsole/api/progress_control_contract_test.go`, `web/src/pages/PlanProgressCockpit.test.tsx`

## Command Evidence

| Gate | Command | Exit | Key output |
| --- | --- | ---: | --- |
| focused Go contracts | `go test ./internal/projectmanager/service ./internal/projectmanager/sqlite ./internal/admin/api ./internal/webconsole/api ./internal/projectmanager -run 'Test(ProgressControl|HumanDecision|S2B|PMProgressControl|Acceptance|Delivery)' -count=1` | 0 | all listed packages `ok`; projectmanager had no matching tests |
| web dependency restore | `pnpm --dir web install --frozen-lockfile` | 0 | lockfile up to date; 648 packages installed/reused |
| focused web | `pnpm --dir web exec vitest run src/pages/PlanProgressCockpit.test.tsx` | 0 | 1 file passed, 1 test passed |
| full Go | `go test ./...` | 0 | all packages passed, including `tests/e2e` and `tests/integration` |
| repository race gate | `make test-race` | 0 | `go test -race -count=10 ./internal/agentruntime/...` passed |
| candidate-related race | `go test -race -count=10 ./internal/projectmanager/service` | 1 | **panic: test timed out after 10m0s**, running `TestAutoAssign_ConcurrentSweeps_NoDoubleAssign`; package failed after 601.118s |
| web full tests | `pnpm --dir web test` | 0 | 193 files passed, 1810 tests passed |
| web build/type gate | `pnpm --dir web run build` | 0 | `tsc -b && vite build` completed; CSS/chunk-size warnings only |

## Findings

1. **Race gate RED for the changed service package.**
   `go test -race -count=10 ./internal/projectmanager/service` timed out after 10 minutes while running `TestAutoAssign_ConcurrentSweeps_NoDoubleAssign`. The candidate changes add progress-control loops and service-level concurrency/wake paths in `internal/projectmanager/service`, so this is a relevant race/stress gate, not an unrelated smoke. Under the stated rule, any RED gate requires structured REJECT.

2. **Final ancestry is no longer current-main based.**
   A final fresh fetch after the validation run found `origin/main=16dc58155dfa0cafd79c08595c8a8e378b5eede9`; the candidate and current `origin/main` are now mutually non-ancestor with merge-base `b66fe30eb3c3d5bbcedda4ef711150d391f67b81`. The candidate ref itself remained immutable at the reviewed SHA, but it is no longer based on current main.

3. **Focused contract coverage is present and green.**
   The focused Go tests cover stale fence conflict persistence without mutation, watchdog independence, missing heartbeat fail-closed behavior, wake backpressure/resume/drain, human decision prerequisite release and stale `WaitHumanDecision` projection clearing, and DeliverySubject/Acceptance fail-closed authority/remote checks.

4. **Web and API evidence surfaces are present and green.**
   The API map contract tests and `PlanProgressCockpit` tests verify `progress_control` fields, required actions, owner actions, prerequisite waits, fact refs, and action options reach the UI contract. Full web tests and build also pass.

## Verdict Rationale

Rejecting solely on machine-verifiable RED evidence:

- Candidate SHA is correct and immutable; final current-main ancestry is not acceptable.
- Focused contracts, full Go, repository race gate, web focused/full/build gates pass.
- But candidate-related `projectmanager/service` race/stress gate fails with a 10 minute timeout.
- Final fresh fetch also shows current `origin/main` moved to `16dc58155dfa0cafd79c08595c8a8e378b5eede9`; candidate and current main are mutually non-ancestor.

Because this candidate modifies service-level progress control, wake, and reconciliation behavior, accepting it with a failing `-race -count=10` service package run would not satisfy the continuous progress / race evidence requirement.
