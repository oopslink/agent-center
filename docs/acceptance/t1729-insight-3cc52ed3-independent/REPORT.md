# Independent acceptance: insight semantics candidate 3cc52ed3

Reviewed SHA: `3cc52ed300bfe00444d6487aded97e96280d2b16`

Verdict: REJECT

Reason: code-level checks passed, but the acceptance contract is not satisfied because the required `task-input/v1` package was absent in the assigned execution checkout, and the immutable candidate is not based on current `origin-push/main`. The candidate is reachable and the immutable ref has not drifted.

## Provenance

- Candidate ref: `origin-push/immutable/t1729-insight-api-ui-3cc52ed3`
- Candidate ref object: `3cc52ed300bfe00444d6487aded97e96280d2b16`
- Reviewed object: `3cc52ed300bfe00444d6487aded97e96280d2b16`
- Current `origin-push/main`: `d3fadaf404731dacbd74909381f9315ea8714475`
- Merge-base with current `origin-push/main`: `1f27bfe74e3dddaf3ffb7e0156c5a61ad55480b4`
- `origin-push/main` ancestor of candidate: no, exit `1`
- Remote reachability: `origin-push/immutable/t1729-insight-api-ui-3cc52ed3` resolves to the reviewed SHA.

Raw logs:

- `raw/00-provenance.log`
- `raw/04-remote-reachability.log`

## Gate Results

- Immutable pushed candidate: PASS. The immutable remote ref resolves exactly to the reviewed SHA.
- Candidate drift: PASS. No mismatch between requested SHA and immutable ref SHA.
- Current-main ancestry: REJECT. Current `origin-push/main` is not an ancestor of the candidate.
- Task input package: REJECT. `/task-input/v1` was not present in the assigned execution checkout, so `README.md` and `manifest.json` could not be read.
- Unknown vs zero: PASS. Backend distinguishes `zero`, `no_sample`, and `unknown`; frontend tests cover null, zero, low coverage, partial coverage, and representative coverage.
- Coverage/stale/window/sample_count: PASS. Backend envelopes include freshness, window, coverage, and sample counts; tests cover freshness threshold/stale behavior and `window=24h` API validation.
- User status mappings: PASS. Backend maps succeeded, failed, crashed, quiet-finalized, running, rejected/did-not-start, unavailable, recovered, and invalid time-order semantics; frontend tests cover user-facing mappings without raw enum leakage in main rows.
- Auth/org isolation: PASS. API handlers require org membership plus `org.analytics.read`; focused API test verifies foreign-org execution is not found.
- Migration/replay: PASS. Rebuild and replay tests pass, including pre-commit crash replay and post-commit duplicate prevention.
- Aggregate to execution reconciliation: PASS. Insight tests reconcile queue/activity/slot facts and focused projectmanager tests pass for progress, runnable, gate, and execution-related behavior.
- Focused Go tests: PASS.
- Focused API tests: PASS.
- Focused frontend tests: PASS.
- Race tests: PASS for `internal/insight` and `internal/webconsole/api`.
- TypeScript compile: PASS.

## Commands

Captured raw output is under `raw/`.

```sh
git fetch --all --tags --prune
git rev-parse 3cc52ed300bfe00444d6487aded97e96280d2b16^{commit}
git rev-parse origin-push/immutable/t1729-insight-api-ui-3cc52ed3^{commit}
git rev-parse origin-push/main^{commit}
git merge-base origin-push/main 3cc52ed300bfe00444d6487aded97e96280d2b16
git merge-base --is-ancestor origin-push/main 3cc52ed300bfe00444d6487aded97e96280d2b16
git branch -r --contains 3cc52ed300bfe00444d6487aded97e96280d2b16
git ls-remote origin-push refs/heads/immutable/t1729-insight-api-ui-3cc52ed3 refs/heads/main
git ls-remote origin refs/heads/immutable/t1729-insight-api-ui-3cc52ed3 refs/heads/main
find /Users/oopslink/.agent-center/workers/worker-edb09a0c/var/runtime/worktrees/exec-cade9303/task-input/v1 -maxdepth 3 -type f -print
go test ./internal/insight -run 'TestInsight' -count=1 -v
go test ./internal/webconsole/api -run 'TestInsights' -count=1 -v
go test -race ./internal/insight -run 'TestInsight' -count=1 -v
go test -race ./internal/webconsole/api -run 'TestInsights' -count=1 -v
go test ./internal/projectmanager/service -run 'Test.*(Progress|Runnable|Gate|Staged|Execution|Aggregate)' -count=1 -v
pnpm --dir web exec vitest run src/pages/InsightOverview.test.tsx --pool=forks --poolOptions.forks.singleFork=true
pnpm --dir web exec tsc -p tsconfig.app.json --noEmit
```

## Minimal Runnable Remediation

Create a new candidate from current main instead of changing the immutable reviewed SHA:

```sh
git fetch origin-push
git switch -c remediation/t1729-insight-semantics-current-main origin-push/main
git cherry-pick 7a002d7c5db95cd24dd810828e3ae79a9c5314c5 3cc52ed300bfe00444d6487aded97e96280d2b16
go test ./internal/insight -run 'TestInsight' -count=1 -v
go test ./internal/webconsole/api -run 'TestInsights' -count=1 -v
go test -race ./internal/insight -run 'TestInsight' -count=1 -v
go test -race ./internal/webconsole/api -run 'TestInsights' -count=1 -v
go test ./internal/projectmanager/service -run 'Test.*(Progress|Runnable|Gate|Staged|Execution|Aggregate)' -count=1 -v
pnpm --dir web exec vitest run src/pages/InsightOverview.test.tsx --pool=forks --poolOptions.forks.singleFork=true
pnpm --dir web exec tsc -p tsconfig.app.json --noEmit
git push origin-push HEAD:immutable/t1729-insight-api-ui-<newsha>
```

The executor task-input materialization also needs to provide `task-input/v1/README.md` and `task-input/v1/manifest.json` before rerunning strict independent acceptance.
