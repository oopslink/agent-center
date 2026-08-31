# T1980 G8 independent technical acceptance

## Frozen subject and verdict

- Remote ref: `origin/candidate/g8-insight-product-visual-a9c45725`
- Reviewed SHA: `230fca7008eb66c1ffcf51dffe1a71d834a78793`
- G6 parent: `928eae010f2f8925e3deab6e4cb6cf2f1da58e7b`
- Evidence branch: `evidence/t1980-g8-230fca70`
- Verdict: **PASS (independent technical acceptance)**

Before testing, the fetched remote ref and worktree `HEAD` both resolved exactly to the reviewed SHA.
The G8 delta is one commit, 9 frontend files, 129 insertions and 13 deletions.

## Test plan and results

| ID | Contract | Result | Evidence |
|---|---|---|---|
| G8-1 | Exact frozen provenance | PASS | Remote ref and pre-test HEAD both equal `230fca7008eb66c1ffcf51dffe1a71d834a78793`; merge-base with G6 equals the exact G6 SHA |
| G8-2 | A/C/D Insight facts and real persistence→projection→HTTP chain | PASS | `go test -count=1 ./internal/insight`; `go test -count=1 ./internal/webconsole/api -run Insight` |
| G8-3 | Forward enum, null/zero/state and localization contract | PASS | 6 focused files / 105 tests, including 52 presentation matrix cases and new unknown-freshness/detail assertions |
| G8-4 | Desktop/mobile IA and bilingual navigation | PASS | New `InsightSecondaryNav` and mobile-nav regression tests verify shared localized labels; no raw translation keys |
| G8-5 | Full repository regression | PASS | Isolated `go test -count=1 ./...`; isolated SPA 197 files / 1894 tests |
| G8-6 | Race gate | PASS | Isolated `make test-race` → `go test -race -count=10 ./internal/agentruntime/...` |
| G8-7 | Release gates | PASS | `make lint`; explicit `pnpm exec tsc -b --force`; `make build` (2713 modules and Go binaries) |

## Layered test inventory

| Layer | Entry / count |
|---|---|
| Unit | Focused Insight SPA 105 cases; full SPA 1894 cases; full Go packages |
| Integration with real persistence / in-process HTTP | Migrated SQLite → Insight refresh/projector → DuckDB projection; authenticated v2 HTTP handler tests |
| E2E package | `tests/e2e` passed in the isolated full Go run |
| Race | All `internal/agentruntime/...` packages, `-race -count=10` |

## G8 contract observations

- Arbitrary future overview freshness now renders curated `Freshness unknown` / localized copy and does not expose `insight.freshness.*` or `insight.state.*` keys.
- Unknown execution outcome, failure reason and quality render curated fallbacks without raw enum or translation-key leakage.
- Insight secondary navigation is an explicit module component rather than a hard-coded fallback; desktop col② and the mobile sheet share localized labels.
- Chinese TaskExecution, Task and Worker user-facing labels are localized while domain/API values remain unchanged.
- Existing coverage boundaries, null-versus-zero behavior, fact-chain semantics and v2 response envelopes remain green.

## Retry audit

The first concurrent full-gate attempt oversubscribed the test host: SPA reported 12 timeout-driven
failures, the deployed-binary smoke could not create its socket, and race timed out while spawning git
fixtures. After all competing commands stopped, the exact gates were rerun in isolation. One initial
isolated SPA run retained a single 60-second timeout in the large `App.test.tsx` route aggregate; that
same file had already passed focused, and a final exact isolated `pnpm test` run passed all 197 files / 1894
tests in 166 seconds. The initially failing Go test also passed by itself, and the final exact isolated full
Go and race commands exited zero. These retries are classified as host-resource flakiness, not candidate
failures, because the unchanged commands passed without code or test modification.

## Non-blocking build observations

The production build retains the existing CSS syntax and large-chunk warnings, but exits zero. No
candidate-specific build error was observed.

## Conclusion

The exact G8 candidate satisfies the requested independent technical A/C/D, fact-chain, contract and
quality gates. Structured verdict: **PASS**, bound only to reviewed SHA
`230fca7008eb66c1ffcf51dffe1a71d834a78793`.
