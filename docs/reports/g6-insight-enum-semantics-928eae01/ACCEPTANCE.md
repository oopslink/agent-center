# G6 Insight enum semantics independent acceptance

Reviewed candidate: `928eae010f2f8925e3deab6e4cb6cf2f1da58e7b`

Remote ref: `refs/heads/candidate/g6-insight-enum-semantics-928eae01`

Verdict: `REJECT`

## Provenance

- `git fetch origin +refs/heads/candidate/g6-insight-enum-semantics-928eae01:refs/remotes/origin/candidate/g6-insight-enum-semantics-928eae01 --prune`: PASS.
- `git rev-parse refs/remotes/origin/candidate/g6-insight-enum-semantics-928eae01`: `928eae010f2f8925e3deab6e4cb6cf2f1da58e7b`.
- Worktree was reviewed detached at `928eae010f2f8925e3deab6e4cb6cf2f1da58e7b`.
- Task-input package was present but stale for `T1850`, with no attachments; it did not match the G6 task contract. This is recorded as provenance risk, not used as candidate evidence.

## Gate Results

| Gate | Command | Result | Evidence |
| --- | --- | --- | --- |
| Focused Go | `go test ./internal/insight ./internal/webconsole/api -count=1` | PASS | `evidence/01-focused-go.txt` |
| Focused SPA | `cd web && pnpm exec vitest run src/utils/insightPresentation.test.ts src/pages/InsightOverview.test.tsx src/pages/InsightProjects.test.tsx src/pages/InsightAgents.test.tsx` | PASS after dependency install; initial run failed because `vitest` was not installed | `evidence/02-focused-spa.txt`, `evidence/02a-pnpm-install.txt`, `evidence/02-focused-spa-rerun.txt` |
| Full Go | `go test ./...` | PASS | `evidence/03-full-go.txt` |
| Full SPA | `cd web && pnpm test` | PASS, 197 files / 1892 tests | `evidence/04-full-spa-vitest.txt` |
| Focused race | `go test -race ./internal/insight ./internal/webconsole/api -count=3` | FAIL: `internal/insight` passed, `internal/webconsole/api` timed out after 10m | `evidence/05-focused-race.txt` |
| Default race | `make test-race` | PASS for `./internal/agentruntime/...` | `evidence/06-make-test-race.txt` |
| Lint/typecheck | `make lint` | PASS, includes `go vet`, gofmt guard, repository lints, `npx tsc -b --force`, and SPA ESLint | `evidence/07-make-lint.txt` |
| Build | `make build` | PASS; Vite emitted existing CSS/chunk-size warnings but exited 0 | `evidence/08-make-build.txt` |

## Blocking Finding

`go test -race ./internal/insight ./internal/webconsole/api -count=3` did not complete. It failed with:

- `panic: test timed out after 10m0s`
- running test at timeout: `TestListMembers_AgentWorkerBinding_FromAgentBC`
- package failure: `FAIL github.com/oopslink/agent-center/internal/webconsole/api 600.784s`

Because the requested acceptance explicitly includes race verification, this is a hard gate failure for the exact reviewed SHA even though the default `make test-race` target passes a narrower package set.

## Contract And Fact-Chain Observations

- Backend v2 execution unknown-count semantics are present: `v2ExecutionCount` counts finished execution facts whose `outcome` is not in `succeeded|failed|crashed|quiet_finalized`, and surfaces `unknown_source_state` through `v2Health`.
- Backend delivery/evolution/lineage routes are wired through `internal/webconsole/api/handlers_insights.go`; existing focused HTTP tests exercise migrated SQLite, Insight projector refresh, authenticated HTTP handlers, and 24h window validation.
- Frontend enum presentation tests verify arbitrary future enum tokens are hidden or folded to explicit unknown/backend-defined labels across overview execution detail, projects, project detail, lineage, and agents.
- A deployed-binary live probe was attempted with a temporary config, SQLite DB, master key, real server, and browser. The first two attempts exposed key-format startup failures; later attempts reached signup/signin but my curl cookie extraction did not authenticate the HTTP checks. I do not count those attempts as passing real HTTP/UI evidence.

## Residual Risks

- The required "real SQLite -> projector -> HTTP -> UI" chain was not fully proven in a single deployed-binary/browser run in this pass.
- The task-input package was stale and unrelated to G6, so the review relied on Git remote provenance plus repository-local tests and code inspection.
- The focused race failure is in `internal/webconsole/api`; I did not patch or isolate whether it is candidate-introduced, pre-existing, or toolchain/environment-sensitive. It remains an acceptance blocker.

