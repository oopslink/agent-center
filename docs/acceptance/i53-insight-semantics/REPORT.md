# I53 Insight Semantics Remediation

## Scope

- Base checked from latest fetched main: `b942af983755639d74b24c5f41bfb47a523b749c`.
- Remediation is presentation/information-architecture only. Insight APIs, Collaboration Graph APIs, permission checks, and projector truth source were not changed.
- Real-page verification used the production install path: `agent-center install test-instance`, HTTP signin, production SPA routes, and Playwright screenshots/video.

## Real-Page Evidence

Both before and after attempted `--with-agent`; this machine rejected the seeded agent because runtime model `claude-opus-4-8` was not configured. The capture script recorded that provenance and fell back to `--with-seed`, still using an isolated real organization, real HTTP API, and production SPA.

Before (`b942af98`):

- `before/01-overview.png`
- `before/02-executions.png`
- `before/04-agents.png`
- `before/05-projects.png`
- `before/page@f11b6cdb05163232b8e98d815a8eb26e.webm`
- `before/RESULTS.json`: completed `0`, coverage `null`, executions `0`, projects `1`, console errors `0`.

After:

- `after/01-overview.png`
- `after/02-executions.png`
- `after/04-agents.png`
- `after/05-projects.png`
- `after/page@4b185a36feb7f6025a1a86e45bef3629.webm`
- `after/RESULTS.json`: completed `0`, coverage `null`, executions `0`, projects `1`, console errors `0`.

## Before To After Mapping

| Blocker | Before observation | Remediation | Result |
| --- | --- | --- | --- |
| Unknown shown as zero under low/no coverage | Empty org has no computable capacity baseline; prior page did not consistently explain evidence around metric rows. | Shared metric evidence now states known/unknown sample counts and coverage; capacity still renders `Cannot determine` instead of `0%`. | PASS |
| Internal outcome/quality enums exposed | Outcome/quality mixes were visible without enough user-level explanation around samples and method. | Existing semantic status/quality labels are preserved; added explicit method text and evidence strings, and tests assert no raw `known=false` leakage. | PASS |
| Seconds and windows unreadable | Overview window was readable, but drilldown/ranking metric context did not consistently expose window/sample evidence. | Existing duration/window formatters remain the single presentation path; ranking/evidence copy now names the rolling window and valid sample counts. | PASS |
| Overview and TaskExecution IA/context mixed | Overview jumped directly from high-level cards into execution surfaces without an object/execution scope map. | Added Overview scope map: Object agents, Object projects, Execution attempts. Execution list now has an explicit filter context panel. | PASS |
| Rankings lack sample count and methodology | Project rows showed bare values such as `0`; ranking charts lacked consistent sample/coverage details. | Agent/project ranking charts and tables include sample count, window, coverage, and retry-counted attempt methodology. | PASS |

## Verification

- `cd web && pnpm exec vitest run src/utils/insightPresentation.test.ts src/pages/InsightOverview.test.tsx src/pages/InsightAgents.test.tsx src/pages/InsightProjects.test.tsx`: PASS, 72 tests.
- `cd web && pnpm exec vitest run src/utils/insightPresentation.test.ts src/pages/InsightOverview.test.tsx src/pages/InsightAgents.test.tsx src/pages/InsightProjects.test.tsx src/components/WorkItemFilterBar.test.tsx src/pages/OrgWorkItems.test.tsx`: PASS, 102 tests.
- `cd web && pnpm test`: PASS, 1920 tests.
- `cd web && pnpm run typecheck`: PASS.
- `cd web && pnpm run lint`: PASS.
- `make build`: PASS.
- `go test ./internal/insight ./internal/webconsole/api -run 'Insights|Insight' -count=1`: PASS.
- `node --check tests/e2e/v2/capture-i53-insight-semantics.mjs`: PASS.

Owner/independent real-data review is still required before I53 closure; this implementer self-check is not a substitute for that gate.
