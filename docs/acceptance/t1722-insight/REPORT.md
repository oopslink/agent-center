# T1722 Insight Semantics IA Acceptance Notes

## Captures

- `overview-desktop.png`: routed `/organizations/acme/insights/overview`, desktop 1440x960.
- `executions-filtered-desktop.png`: routed `/organizations/acme/insights/executions?window=24h&agent_ref=agent%3Abuilder&project_id=proj-1`.
- `execution-detail-desktop.png`: routed `/organizations/acme/insights/executions/exec-24h-1`.
- `overview-mobile.png`: routed overview at 390x844.

## Reproduction

1. Run `node docs/acceptance/t1722-insight/mock-api.mjs`.
2. Run `cd web && pnpm exec vite --host 127.0.0.1 --config ../docs/acceptance/t1722-insight/vite.config.mjs`.
3. Open the routes listed above on `http://127.0.0.1:5174`.

## Design Clause Mapping

| Frozen clause | Component / path |
| --- | --- |
| Overview owns situation awareness; executions are not inlined there | `web/src/pages/InsightOverview.tsx` default `InsightOverview` renders KPI, coverage, queue/duration, agent and project leaderboards only. |
| Object hierarchy is Overview -> Agent/Project -> Execution | `InsightOverview` links to `InsightExecutionsPage` with exact `agent_ref` or `project_id`; `InsightExecutionsPage` links rows to `InsightExecutionDetailPage`. |
| 24h context and filters survive drilldown | `executionListHref`, `InsightExecutionsPage`, and cursor links preserve `window=24h`, `agent_ref`, `project_id`, and `cursor` in the URL. |
| Low or missing coverage must not be shown as zero capacity/utilization | `coverageState`, `CoverageBadge`, and `UtilizationCell` classify null/0 as unknown, `<0.5` as insufficient, and hide utilization when coverage is not usable. |
| No raw internal enum exposure in main UI | `executionStatus`, `QualityBadge`, and `reasonCopy` translate outcomes, quality, and failure reasons on overview/list/detail summary surfaces. Raw fields appear only inside the detail technical disclosure. |
| Durations, timestamps, samples, p50/p95 must be readable | `formatDurationMs`, `WindowContext`, `PercentileLine`, `PercentilePair`, and table cells render readable local times and sample counts. |
| Stale/rebuilding/unavailable states must be explicit | `FreshnessNotice` maps API freshness state to visible status copy and non-fake error states. |
| API unavailable must fail closed instead of fake zeroes | `internal/webconsole/api/handlers_insights.go` returns 503 with window/as_of/freshness envelope; UI error path renders explicit unavailable copy. |
| Detail must be an object/timeline layout, not a detached table row | `InsightExecutionDetailPage` renders `Execution timeline`, key object fields, failure message, and technical details. |
| Additive execution semantics fields | `internal/insight/types.go`, `internal/insight/service.go`, and `web/src/api/insights.ts` expose `failure_message`, `command_status`, `status_reason`, and `status_message`. |
