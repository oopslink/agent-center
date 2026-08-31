# G8 Insight Product / Visual Acceptance

Structured verdict: **REJECT**

Candidate under test:

- Frozen SHA: `230fca7008eb66c1ffcf51dffe1a71d834a78793`
- Remote ref: `origin/candidate/g8-insight-product-visual-a9c45725`
- Fresh ref readback: `git ls-remote` returned `230fca7008eb66c1ffcf51dffe1a71d834a78793`
- Isolated runtime worktree: `/tmp/g8-insight-fresh-230fca7.KrNiIn/wt`
- Served build version: `HEAD-230fca70`

Evidence submission readback:

- Evidence commit: see final remote readback for `refs/heads/ac-exec/task-35cebf56/exec-d38114a1`.
- Remote executor ref: `refs/heads/ac-exec/task-35cebf56/exec-d38114a1`
- Remote executor ref readback: performed after evidence push.
- Remote candidate ref readback: `230fca7008eb66c1ffcf51dffe1a71d834a78793`
- Remote main readback: `fddd073cd4208825086fc62d11fb6ba5a4d4655f`
- Evidence branch base vs main after final push: merge-base `fddd073cd4208825086fc62d11fb6ba5a4d4655f`, `0 behind / 1 ahead`
- Evidence branch and candidate are intentionally separate lines: merge-base `ad7959c60ea0d3004c44d65cfbe2c93de34c9406`, `candidate...evidence = 13 / 2`.
- Worktree clean after final push: yes.

Production-chain method:

- Built exact candidate with embedded production SPA via `make build`.
- Started a fresh real test instance with `./bin/agent-center install test-instance --id g8insight230fca7seed --with-seed --workers 0 --output json`.
- Used the real instance SQLite source DB, the server-owned Insight projector, HTTP API, and Chromium-rendered UI.
- API evidence is persisted in `api-overview.json` and `api-executions.json`.
- Browser verdict is persisted in `verdict.json`.

Blocking finding:

- **Insight IA is incomplete on desktop.** The Insight secondary nav renders only `Insight overview`, while the routed Insight surfaces also include Agents, Projects, and Task executions. This fails the requested IA coverage. Code inspection of the exact candidate confirms `web/src/shell/nav/InsightSecondaryNav.tsx:17-35` only defines `overviewPath` and a single `NavLink`.

Passing evidence:

- Overview no longer exposes raw i18n keys in desktop or mobile DOM.
- Specifically checked for raw `insight.*` keys and prior leaked enum/reason tokens such as `coverage_unknown`, `unknown_source_state`, `freshness_stale`, `sample_empty`, and `metric_unknown`.
- Overview -> execution list drilldown preserves `agent_ref`.
- Execution detail breadcrumb return preserves list context.
- State matrix covered through rendered UI: success, failed, recovered crash, unknown outcome, running, rejected before start, invalid time ordering, filtered empty, and missing detail.
- Mobile overview has no body-level horizontal overflow.

Validation commands:

```text
git fetch --refmap='' origin refs/heads/candidate/g8-insight-product-visual-a9c45725:refs/validation/g8-insight-product-visual-a9c45725
go test ./internal/insight ./internal/webconsole/api -run 'TestInsight|TestInsights' -count=1
pnpm --dir web exec vitest run src/pages/InsightOverview.test.tsx src/pages/InsightAgents.test.tsx src/pages/InsightProjects.test.tsx src/utils/insightPresentation.test.ts
make build
go run ./cmd/g8insightseed /Users/oopslink/.agent-center-test/g8insight230fca7seed/center/var/agent-center.db organization-51076edd
node tests/e2e/v2/g8-insight-product-visual-acceptance.mjs
```

Screenshots:

- `screenshots/desktop-01-overview.png`
- `screenshots/desktop-02-agent-drilldown-list.png`
- `screenshots/desktop-03-execution-detail.png`
- `screenshots/desktop-matrix-known-success.png`
- `screenshots/desktop-matrix-known-failure.png`
- `screenshots/desktop-matrix-recovered.png`
- `screenshots/desktop-matrix-unknown-outcome.png`
- `screenshots/desktop-matrix-running.png`
- `screenshots/desktop-matrix-invalid-clock.png`
- `screenshots/desktop-matrix-rejected-before-start.png`
- `screenshots/desktop-04-empty-filtered.png`
- `screenshots/desktop-05-missing-detail.png`
- `screenshots/mobile-01-overview.png`
- `screenshots/mobile-02-executions.png`

Notes:

- `--with-agent` provisioning was attempted first but failed before UI acceptance because the candidate default model `claude-opus-4-8` was not present in the seeded runtime catalog. The acceptance then used `--with-seed` plus explicit Insight source facts to keep the required SQLite -> projector -> HTTP -> UI production chain intact.
