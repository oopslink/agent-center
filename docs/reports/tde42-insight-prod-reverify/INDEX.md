# TDE42 Insight production revalidation evidence

Verdict: **FAIL / BLOCK**

Run timestamp: 2026-08-31T04:27:05Z
Executor branch: `ac-exec/task-de42de6f/exec-79c42637`
Executor start HEAD: `3b2b45f480c297f44b0e2deb877ebc6cdad7f5f5`

## Target provenance

- Authoritative S3 candidate ref read back from origin:
  `refs/heads/s3/insight-phase1-candidate-20260827`
- Delivery alias read back from origin:
  `refs/heads/ac-exec/task-5493aaeb/exec-9bd82743`
- Both refs resolved to:
  `16dc58155dfa0cafd79c08595c8a8e378b5eede9`
- Candidate `16dc5815` differs from verified code candidate
  `9419d5da2e21c9b9e15183efe33c869ef2413ccc` only by
  `docs/reports/t1578-s3-insight-phase1-integration.md`.

Raw evidence: `evidence/raw/02-candidate-provenance.txt`.

## Blocking evidence gaps

- The required local `task-input/v1/README.md` and `task-input/v1/manifest.json`
  were not present in this workspace. The absence is recorded in
  `evidence/raw/03-task-input-presence.txt`.
- No explicit production target URL or port was present in environment variables
  matching the allowed URL/port names checked in
  `evidence/raw/05-explicit-url-env.txt`.
- Because this executor was instructed not to use agent-center databases,
  admin sockets, worker tokens, `mcp_config.runtime.json`, process arguments, or
  raw HTTP as a fallback for center access, I did not probe hidden local center
  state. Therefore actual production running SHA/version/health could not be
  independently established.
- Team rules listed in the recovery index could not be fetched because this
  isolated executor has no `get_team_rule` tool; I applied the quoted rule
  descriptions from the task text directly.

## Hard-red result

At least one Insight hard red is still present in the S3 candidate:

- `agents`, `projects`, and `executions` empty collections encode as JSON
  `null` instead of the frozen contract's empty arrays `[]`.
- Direct probe against S3 candidate `16dc5815` failed with:
  `field agents = null, want []`,
  `field projects = null, want []`,
  `field executions = null, want []`.
- The known fix ref `origin/fix/t1820-hard-reds` contains
  `fix(insight): restore frozen empty collection contract`
  (`ed4ef11a30e996f8855c2f37f15561b94b20ea95`) but is not an ancestor of either
  S3 candidate `16dc5815` or executor HEAD `3b2b45f4`.

Raw evidence:

- `evidence/raw/06-hard-red-fix-not-contained.txt`
- `evidence/raw/25-candidate-hard-red-empty-collections-probe.txt`
- `evidence/raw/18-hard-red-empty-collections-probe-current-fixed.txt`

## Fresh regression runs completed

S3 candidate `16dc5815`:

- `go test ./internal/insight -count=1`: PASS.
- `go test ./internal/webconsole/api -run Insights -count=1`: PASS.
- `go test ./internal/projectmanager ./internal/webconsole/api -run 'Plan|Project|Evolution|Insight' -count=1`: PASS.
- `pnpm exec vitest run src/pages/InsightOverview.test.tsx src/pages/InsightExecutionDetail.test.tsx`: PASS, 11 tests.
- IA/Project/Plan page suite: PASS, 7 files / 215 tests.

Executor/current HEAD `3b2b45f4`:

- `go test ./internal/insight -count=1`: PASS.
- `go test ./internal/webconsole/api -run Insights -count=1`: PASS.
- `go test ./internal/projectmanager ./internal/webconsole/api -run 'Plan|Project|Evolution|Insight' -count=1`: PASS.
- `pnpm exec vitest run src/pages/InsightOverview.test.tsx src/pages/InsightExecutionDetail.test.tsx`: PASS, 6 tests.
- IA/Project/Plan page suite: PASS, 8 files / 216 tests.

These passes do not override the hard-red failure above.

## Raw evidence index

- `00-worktree.txt` — initial executor worktree identity and status.
- `01-candidate-worktree-add.txt` — detached candidate worktree creation.
- `02-candidate-provenance.txt` — remote ref readback and code-tree diff.
- `03-task-input-presence.txt` — missing task-input package.
- `04-hard-reds-branch.txt` — known hard-red fix branch metadata.
- `05-explicit-url-env.txt` — explicit production URL/port env check.
- `06-hard-red-fix-not-contained.txt` — fix patch and ancestor checks.
- `10-go-test-internal-insight.txt` — current HEAD Insight service tests.
- `11-go-test-webconsole-insights.txt` — current HEAD Insight API tests.
- `12-vitest-insight-pages.txt` — first frontend attempt before dependencies.
- `13-pnpm-install.txt` — dependency installation.
- `14-vitest-insight-pages-rerun.txt` — current HEAD Insight page tests.
- `15-vitest-ia-project-plan.txt` — current HEAD IA/Project/Plan page tests.
- `16-go-test-project-plan-insight.txt` — current HEAD Plan/Project/Insight Go tests.
- `17-hard-red-empty-collections-probe-current.txt` — invalid first probe, kept for audit.
- `18-hard-red-empty-collections-probe-current-fixed.txt` — current HEAD hard-red reproduction.
- `20-candidate-go-test-internal-insight.txt` — candidate Insight service tests.
- `21-candidate-go-test-webconsole-insights.txt` — candidate Insight API tests.
- `22-candidate-vitest-insight-pages.txt` — candidate Insight page tests.
- `23-candidate-vitest-ia-project-plan.txt` — candidate IA/Project/Plan page tests.
- `24-candidate-go-test-project-plan-insight.txt` — candidate Plan/Project/Insight Go tests.
- `25-candidate-hard-red-empty-collections-probe.txt` — candidate hard-red reproduction.
