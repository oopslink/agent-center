# I6 Insight Independent Acceptance Report

Date: 2026-08-31
Verifier: isolated executor
Verdict: REJECT

## Candidate Identity

- Immutable candidate ref: `origin/immutable/i6-insight-acceptance-43abcd72`
- Exact candidate SHA: `43abcd7250528694a662724c99d0a80e042ce8bb`
- Remote readback:
  - `git ls-remote origin refs/heads/immutable/i6-insight-acceptance-43abcd72 refs/heads/main`
  - `43abcd7250528694a662724c99d0a80e042ce8bb refs/heads/immutable/i6-insight-acceptance-43abcd72`
  - `ad7959c60ea0d3004c44d65cfbe2c93de34c9406 refs/heads/main`
- Ancestry:
  - `git merge-base --is-ancestor origin/main 43abcd7250528694a662724c99d0a80e042ce8bb` exited `0`.
  - `git merge-base --is-ancestor 43abcd7250528694a662724c99d0a80e042ce8bb origin/main` exited `1`; candidate is not merged to `origin/main`.

## Frozen Inputs Read

- `docs/design/features/insight-metric-semantics-and-information-architecture.md` from `2e04ab8610f2c07bef847b11183a27e2b5cd7512`.
- `docs/design/features/insight-phase-1-contract.md` from candidate/current tree; blob `ed45db2fcf220488a6644580cd14897020c2527c`.
- `docs/contracts/insight-metrics-v2.md` and `docs/contracts/insight-v2.schema.json` from `a427a2640621762506f5f98dc3179b0ab43b18e7`.
- Note: those `docs/contracts/*` files do not exist at candidate `43abcd72`; the candidate carries v2 implementation in `internal/insight/contract_v2.go`.

## Commands And Raw Evidence

Raw logs are committed under `evidence/i6-43abcd72/`.

| Gate | Command | Result |
|---|---|---|
| Focused Go | `go test ./internal/insight ./internal/webconsole/api -run 'Insight|insight|Contract|V2|Overview|Execution|Lineage|Delivery' -count=1` | PASS: `ok internal/insight`, `ok internal/webconsole/api` |
| Focused frontend | `pnpm install --frozen-lockfile && pnpm exec vitest run src/utils/insightPresentation.test.ts src/pages/InsightOverview.test.tsx src/pages/InsightAgents.test.tsx src/pages/InsightProjects.test.tsx src/App.test.tsx` | PASS: 5 files, 88 tests |
| Race | `RACE_COUNT=1 make test-race` | PASS: all `./internal/agentruntime/...` packages |
| Full Go | `go test ./...` | PASS, including `internal/insight`, `internal/webconsole/api`, `tests/e2e`, `tests/integration` |
| Full frontend | `pnpm exec vitest run` | FAIL: 2 failed / 1881 passed |
| Lint | `make lint` | FAIL at `lint-spa-eslint`: 10 errors in `web/src/pages/Access.tsx` |
| Build | `make build` | PASS: Vite built and Go binaries built |

Frontend failures from `evidence/i6-43abcd72/web-vitest-all.log`:

- `src/App.test.tsx > App shell + route tree > renders DMs / nested IssueDetail / nested TaskDetail / Agents / AgentDetail / Projects / ProjectDetail / Access / Secrets / Fleet / Settings` timed out at 60000 ms.
- `src/pages/Access.test.tsx > Access page > renders explicit empty, list error, and detail error states with recovery actions` could not find `[data-testid="access-role-empty"]`.

Lint blocker from `evidence/i6-43abcd72/lint.log`:

- `web/src/pages/Access.tsx` lines `2390`, `2400`, `2434`, `2469`, `2525`, `2535`, `2828`, `2862`, `2884`, `2957` violate the ESLint checkbox rule; `make lint` ends with `make: *** [lint-spa-eslint] Error 1`.

## Contract Evidence

Observed source/test coverage in candidate:

- Phase 1 formulas and real source chain: `internal/insight/service_test.go` includes `TestInsightReplay_IdempotentLateEventsBoundariesQuantilesAndRebuild`, `TestInsightSlotObservation_DuplicateHeartbeatAdmissionAndStaleGap`, `TestInsightSlotObservation_AdmissionCapOnlyChangeClosesCapacityInterval`, `TestInsightSlotObservation_HeartbeatTTLExcludesUnknownTail`, `TestInsightInvalidTimeOrder`, `TestInsightExecutionExplanationFieldsAndWindowGate`, and crash replay tests.
- 503 envelope and auth isolation: `internal/webconsole/api/handlers_insights_test.go` includes `TestInsightsAPIUnavailableReturnsFreshnessEnvelope`, `TestInsightsHTTPReadDoesNotTriggerProjection`, `TestInsightsExecutionAPI_ForeignOrgExecutionIsNotFound`, and v2 `execution_context_required`.
- v2 seven breaks and lineage: `internal/insight/service_test.go` includes `TestInsightV2DeliveryEvolutionAndLineage`; source declares the seven break kinds in `internal/insight/contract_v2.go`.
- UI A/C/D-focused coverage exists in `web/src/pages/InsightOverview.test.tsx`, `web/src/pages/InsightAgents.test.tsx`, `web/src/pages/InsightProjects.test.tsx`, and `web/src/utils/insightPresentation.test.ts`.

## Structured Verdict

Overall: REJECT. Any blocker is reject, and both full frontend and lint gates are red.

### A. Information Architecture And Context

Status: PASS for implemented UI contract checks.

Evidence:

- Routes exist for `/insights/overview`, `/insights/executions`, `/insights/executions/:executionId`, `/insights/agents`, `/insights/projects`, and project lineage.
- Sidebar includes `Overview`, `Task executions`, `Agents`, and `Projects`.
- Focused tests assert overview agent/project/all-execution drilldowns preserve exact URL filters and list cursor behavior.

Residual note:

- Detail API hook always requests `?window=24h`; source-list context is carried in navigation state or reconstructed from detail URL search, not sent to the detail API.

### C. Enumerations And Explanations

Status: PASS for focused checks.

Evidence:

- `web/src/utils/insightPresentation.ts` maps `succeeded`, `failed`, `crashed`, `quiet_finalized`, running, queued, rejected/failed/expired, unknown outcome, `invalid_time_order`, and unknown quality to user labels.
- Focused UI tests assert raw `quiet_finalized`, `invalid_time_order`, and raw failure reason do not appear in the main list view.
- `internal/insight/types.go` and `internal/insight/service.go` carry nullable `failure_message`, `command_status`, `status_reason`, and `status_message`; focused Go tests pass.

### D. Time, Statistics, And Ranking

Status: PASS for focused checks.

Evidence:

- Phase 1 aggregation uses half-open window predicates and DuckDB `quantile_cont` in `internal/insight/helpers.go`.
- UI duration/coverage boundary tests cover milliseconds, seconds, minutes, hours, days, null, and negative values.
- Overview tests assert completed-attempt sorting/explanatory labels, P50/P95 samples, and coverage display.

### Backend / Real Fact Chain / v2

Status: PASS for focused checks; not sufficient for overall acceptance because repository gates fail.

Evidence:

- `go test ./internal/insight ./internal/webconsole/api ...` passed.
- Source-chain fixtures seed SQLite production tables/services and run projector/API paths, covering terminal execution, queued command, invalid timestamps, low coverage heartbeat, cap changes, stale heartbeat TTL, crash replay, 503 envelope, foreign-org isolation, v2 health/funnel/lineage.

### Release Gates

Status: REJECT.

Blockers:

- Full frontend suite failed: 2 tests failed, 1881 passed.
- `make lint` failed: 10 ESLint errors in `web/src/pages/Access.tsx`.
- Candidate is not reachable from `origin/main` (`candidate_in_origin_main=1` from `git merge-base --is-ancestor <candidate> origin/main`).

## Final Decision

REJECT `43abcd7250528694a662724c99d0a80e042ce8bb`.

The Insight-specific focused tests and real-chain fixtures are present and pass, but the immutable candidate cannot satisfy the frozen acceptance because full frontend and lint gates are red, and the candidate has not reached `origin/main`.
