# T1975 G7 independent technical acceptance — G6 Insight candidate

## Frozen subject

- Remote ref: `origin/candidate/g6-insight-enum-semantics-928eae01`
- Reviewed SHA: `928eae010f2f8925e3deab6e4cb6cf2f1da58e7b`
- Baseline / merge-base: `ad7959c60ea0d3004c44d65cfbe2c93de34c9406`
- Evidence branch: `evidence/t1975-g6-928eae01`
- Verdict: **PASS (independent technical acceptance)**

Before any candidate test, `git rev-parse origin/candidate/g6-insight-enum-semantics-928eae01`
and the worktree `HEAD` both resolved exactly to the reviewed SHA above.

## Test plan

| ID | Layer | Contract | Exit criterion |
|---|---|---|---|
| G7-1 | provenance | Frozen remote ref and worktree identify one immutable candidate | Both SHAs equal the reviewed SHA |
| G7-2 | unit / integration | Insight service and v2 HTTP handlers exercise real migrated SQLite, projector refresh, DuckDB projection, and HTTP responses | Focused Go tests pass |
| G7-3 | frontend contract | Insight A–E presentation paths preserve null vs zero, coverage boundaries, state semantics, and hide forward enum tokens | Focused UI matrix passes |
| G7-4 | repository regression | Full Go and SPA suites remain green | No failures |
| G7-5 | race | Repository concurrency gate | `make test-race` passes with `-race -count=10` |
| G7-6 | release gates | Vet/format/architecture lint, ESLint, TypeScript build mode, production SPA and Go binaries | `make lint`, explicit `tsc -b --force`, and `make build` pass |

## Execution report

| ID | Result | Evidence |
|---|---|---|
| G7-1 | PASS | Remote ref and pre-test `HEAD` both `928eae010f2f8925e3deab6e4cb6cf2f1da58e7b`; merge-base `ad7959c60ea0d3004c44d65cfbe2c93de34c9406` |
| G7-2 | PASS | `go test -count=1 ./internal/insight`; `go test -count=1 ./internal/webconsole/api -run Insight` |
| G7-3 | PASS | 5 focused files / 97 tests; `insightPresentation.test.ts` contributes 52 forward-enum, null/zero, coverage-boundary, duration, outcome, quality and reason cases |
| G7-4 | PASS | `go test -count=1 ./...`; SPA 197 files / 1892 tests |
| G7-5 | PASS | `make test-race`: all `internal/agentruntime/...` packages pass under `go test -race -count=10` |
| G7-6 | PASS | `make lint`; `pnpm exec tsc -b --force`; `make build` (2712 modules transformed, binaries built) |

## Layered inventory

| Layer | Count / entry |
|---|---|
| Unit (in-package) | Full Go repository plus focused `internal/insight`; focused SPA 97 cases and full SPA 1892 cases |
| Integration with real persistence / in-process HTTP | `internal/insight` migrated SQLite → projector refresh → DuckDB; `internal/webconsole/api` authenticated HTTP handlers; full `tests/integration` package |
| E2E package | Full `tests/e2e` Go package passed as part of `go test -count=1 ./...` |
| Deployed-binary smoke | Not a release/phase-close task; production binaries were built, but no fresh installed instance was required by this frozen-candidate technical contract |

## Matrix observations

- Coverage distinguishes `null`, `0`, `<0.1%`, `<50%`, `50%`, `<90%`, and `>=90%`; utilization zero remains a valid zero only when coverage permits presentation.
- Rates, percentiles, and durations distinguish a measured zero from missing or invalid data.
- Known execution outcomes, command states, quality values, and failure reasons map to curated semantics.
- Arbitrary future health, freshness, lineage, recovery, verdict, reason, break, and anomaly tokens map to safe fallback labels and are not emitted verbatim by the tested presentation helper.
- Real HTTP fixtures verify v2 envelope shape, required execution context, unavailable state, and projected execution reads.

## Non-blocking observations

- The production build emits pre-existing CSS syntax and large-chunk warnings but exits successfully.
- Product/visual acceptance T1972 separately rejected this exact SHA because a served Overview path can expose the raw i18n key `insight.state.unknown`, and lineage evidence remains JSON-formatted. That finding is outside this task's technical-gate verdict and must remain a blocker for product acceptance; this PASS must not be interpreted as overriding T1972.

## Conclusion

The exact frozen G6 candidate passes the requested independent technical contract. All focused,
full, race, lint, typecheck, and build gates are green, and the enum/null/zero/state matrices are
covered. Verdict: **PASS**, bound only to reviewed SHA
`928eae010f2f8925e3deab6e4cb6cf2f1da58e7b`.
