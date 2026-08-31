# T1986 G9 independent technical acceptance

## Frozen subject and verdict

- Remote ref: `origin/candidate/g9-insight-secondary-nav-20260901-0357`
- Reviewed SHA: `f4c35357ea4a93286ea1b7926df91fd7a0e77859`
- Sole parent (G8): `230fca7008eb66c1ffcf51dffe1a71d834a78793`
- Evidence branch: `evidence/t1986-g9-f4c35357`
- Verdict: **PASS (independent technical acceptance)**

The fetched remote ref and pre-test worktree `HEAD` both resolved exactly to the reviewed SHA.
The G9 delta is one frontend-only commit: 7 files, 208 insertions and 32 deletions.

## Test plan and results

| ID | Contract | Result | Evidence |
|---|---|---|---|
| G9-1 | Immutable candidate provenance | PASS | Remote ref and HEAD equal the reviewed SHA; `HEAD^` equals exact G8 SHA |
| G9-2 | A/C/D facts and SQLite→projection→HTTP chain | PASS | `go test -count=1 ./internal/insight`; `go test -count=1 ./internal/webconsole/api -run Insight` |
| G9-3 | Desktop Insight secondary navigation | PASS | Overview, Agents, Projects and Task executions links exist, preserve the Insight module, navigate, and expose correct active state |
| G9-4 | Mobile navigation parity | PASS | Mobile sheet contains the same four stable routes, closes after navigation, and routes to Agents correctly |
| G9-5 | Drilldown active semantics | PASS | Task executions remains active on execution detail routes; exact `?window=24h` list link is preserved |
| G9-6 | Focused frontend contract | PASS | 8 files / 110 tests, including 52 enum/null/zero/state presentation cases |
| G9-7 | Full repository regression | PASS | Final isolated `go test -count=1 ./...`; SPA 199 files / 1899 tests |
| G9-8 | Race and release gates | PASS | `make test-race` (`-race -count=10`), `make lint`, explicit `tsc -b --force`, `make build` |

## Layered inventory

| Layer | Entry / count |
|---|---|
| Unit | Insight/navigation focused SPA 110 cases; full SPA 1899 cases; full Go packages |
| Integration | Migrated SQLite → Insight refresh/projector → DuckDB; authenticated Insight HTTP handlers |
| Deployed/e2e | `tests/e2e` passed in the final full Go run, including deployed-binary paths |
| Race | All `internal/agentruntime/...` packages under `-race -count=10` |

## Navigation contract observations

- Desktop col② now exposes all four intended Insight surfaces instead of only Overview.
- All routes remain organization-scoped in production and use stable `/insights/...` suffixes.
- The execution-list link retains the authoritative rolling window query and remains active for detail drilldowns.
- Desktop and mobile both use the same `InsightSecondaryNav`, eliminating divergent hard-coded item sets.
- English labels and the Chinese section/Overview/Agents/Projects labels come from Insight i18n resources.

## Retry audit

The first full Go run hit the known asynchronous `TestTaskInputPlan569_RealAdminHandlersEndToEnd`
five-second timing window; the unchanged test passed alone in 0.12 seconds. The next full run passed
that package but hit a startup timing miss in `TestE2E_RestartRecovery_DeployLevel`; the unchanged
deployed recovery test then passed alone in 10 seconds, proving persistence, SIGKILL recovery, session
resume and directed-message reinjection. A final unchanged `go test -count=1 ./...` passed completely.
No candidate or test code was modified between attempts, so these are recorded as baseline timing flakes.

## Non-blocking observations

- The Chinese `insight.nav.executions` value intentionally remains `Task executions` in this candidate,
  matching its explicit regression assertions. Product/visual acceptance should decide whether that
  mixed-language label is acceptable; it does not break routing or the requested technical contract.
- Production build retains the existing CSS syntax and large-chunk warnings but exits zero.

## Conclusion

The exact G9 candidate satisfies the requested independent A/C/D, fact-chain, desktop/mobile navigation,
race and release gates. Structured verdict: **PASS**, bound only to reviewed SHA
`f4c35357ea4a93286ea1b7926df91fd7a0e77859`.
