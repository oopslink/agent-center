# G7 Insight A-F Final Verification

Branch: `ac-evidence/g7-insight-af-final-20260901`  
Candidate SHA: `fddd073cd4208825086fc62d11fb6ba5a4d4655f`

## Provenance

- Read local task package: `task-input/v1/README.md`, `task-input/v1/manifest.json`.
- Fresh fetch command: `git fetch origin main --prune`.
- Fresh fetched `main` / `FETCH_HEAD`: `fddd073cd4208825086fc62d11fb6ba5a4d4655f`.
- Stale remote-tracking ref observed: `origin/main = ad7959c60ea0d3004c44d65cfbe2c93de34c9406`.
- Cause observed: `remote.origin.fetch = +refs/*:refs/*`, so fetch updated local `refs/heads/main` rather than `refs/remotes/origin/main`.
- Executor branch created from fetched `main`: `git checkout -B ac-evidence/g7-insight-af-final-20260901 fddd073cd4208825086fc62d11fb6ba5a4d4655f`.
- Ancestry readback: `git merge-base --is-ancestor ad7959c60ea0d3004c44d65cfbe2c93de34c9406 HEAD` exited `0`.

Raw: `raw/00-provenance.log`.

## Isolated Instance

Full-agent attempt:

```sh
./bin/agent-center install test-instance --id g7-insight-af-final --with-agent --workers 1 --output=json
```

Result: failed during seeded agent creation:

```text
runtime_model_not_found: runtime model was not found, model=claude-opus-4-8
```

Fallback isolated seeded center:

```sh
./bin/agent-center install test-instance --id g7-insight-af-final --with-seed --workers 1 --output=json
```

Observed instance:

- Prefix: `/Users/oopslink/.agent-center-test/g7-insight-af-final`
- Web URL: `http://127.0.0.1:56892`
- Server port: `56893`
- Admin port: `56894`
- Org slug/id: `org-67eba184` / `organization-2bdefd27`
- Project id: `project-360a6a8f`

Secrets in raw logs were redacted before commit. Raw: `raw/07-install-test-instance.json`, `raw/09-install-test-instance-seed.json`.

## Commands

- `go test ./internal/insight ./internal/webconsole/api -run 'TestInsight|Test.*Insight|Insights' -count=1 -v`
- `pnpm --dir web install --frozen-lockfile`
- `pnpm --dir web exec vitest run src/pages/InsightOverview.test.tsx --reporter=verbose`
- `make lint-spa-tsc`
- `make build`
- `go test -race -count=1 ./internal/insight`
- Live HTTP: signin, `/api/auth/me`, `/api/orgs/org-67eba184/insights/overview?window=24h`, `/insights/executions`, `/insights/v2/overview`, `/insights/v2/projects`
- Live UI: `agent-browser` signin and `/organizations/org-67eba184/insights/overview` text snapshot

One incorrect command was attempted and recorded separately:

- `pnpm --dir web vitest ...` failed because the correct pnpm form is `pnpm --dir web exec vitest ...`.

## A-F Observations

| Item | Verdict | Evidence | Observation |
| --- | --- | --- | --- |
| A SQLite -> projector -> aggregate read model | PASS | `raw/01-go-insight-targeted.log`, `raw/16-go-insight-race.log` | Migrated SQLite fixtures projected into DuckDB; idempotent replay, late-event recomputation, rebuild parity, quantiles, invalid time diagnostics, crash recovery and applied-store behavior passed. |
| B HTTP routes | PASS | `raw/01-go-insight-targeted.log`, `raw/10-live-http-insights.log` | Authenticated live instance returned `fresh` Insight envelopes for overview/executions/v2 overview/v2 projects. Handler tests verified route validation, no read-triggered projection, unavailable envelope, execution detail and cross-org hiding. |
| C UI overview/drilldowns | PASS | `raw/04-web-insight-vitest-rerun.log`, `raw/15-browser-insight-body.log` | Live SPA rendered Insight overview with `View all executions`; component test verified all executions, agent drilldown, and project drilldown links. |
| D state matrix | PASS | `raw/04-web-insight-vitest-rerun.log`, `raw/01-go-insight-targeted.log` | Null/zero/low/partial/representative coverage copy passed; status/recovery/quality mapping passed; unknown enum tokens are hidden from main rows. |
| E drilldown/detail semantics | PASS | `raw/01-go-insight-targeted.log`, `raw/04-web-insight-vitest-rerun.log` | Filtered execution list, cursor preservation/removal, single execution detail, failure messages, fallback reasons, invalid quality and not-found state passed. |
| F delivery/evolution/lineage and ancestry | PASS with limitation | `raw/00-provenance.log`, `raw/01-go-insight-targeted.log`, `raw/07-install-test-instance.json` | `TestInsightV2DeliveryEvolutionAndLineage` passed and candidate ancestry is preserved. The live seeded-agent execution path could not be produced because `install test-instance --with-agent` fails on missing runtime model `claude-opus-4-8`. |

## Live HTTP Readback

The isolated seeded center returned:

- `/insights/overview`: `freshness.state = fresh`, `completed_executions = 0`, `failed_executions = 0`, empty agents/projects lists for legacy overview.
- `/insights/executions`: `freshness.state = fresh`, empty execution collection.
- `/insights/v2/overview`: `metric_version = insight.metrics.v2`, project `project-360a6a8f` surfaced with `health.status = unknown` and `reason_codes = ["coverage_unknown"]`.
- `/insights/v2/projects`: same seeded project row surfaced.

This proves the live server wiring and projector checkpoint are active, but does not prove live execution rows because the full-agent seeded path failed before execution generation.

## Warnings And Failures

- Product failure point: full-agent isolated instance could not be completed due `runtime_model_not_found` for `claude-opus-4-8`.
- Build warning: `make build` emitted a Vite CSS minifier warning (`Expected identifier but found "-"`) and large chunk warnings, exit `0`.
- Browser screenshot warning: `agent-browser snapshot` succeeded, but its screenshot command hung; a direct Playwright fallback failed because the repo does not include Playwright as a dependency.
- Rule access: `get_team_rule` was not available in this isolated executor, and the task explicitly forbids agent-center/MCP/control-plane fallback access. I therefore used the provided Team Rule Index as constraints and did not query center state.

## Verdict

Overall: `PASS_WITH_LIMITATION`.

Frozen A-F are covered by reproducible repo tests, production build, live seeded instance HTTP, and live SPA text readback. The only failed full-fidelity element is generation of real live execution rows through `install test-instance --with-agent`; fresh `main` currently cannot create the seeded agent because its default runtime model is absent.
