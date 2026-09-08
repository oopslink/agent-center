# T2169 Insight Independent Acceptance

Verdict: **BLOCKED**

Reviewed candidate: `ac-exec/task-54483391/exec-eab25743@180edce16ffd102571914a2f5b55300b03509828`.

## Ref Readback

- `git ls-remote origin refs/heads/ac-exec/task-54483391/exec-eab25743`: `180edce16ffd102571914a2f5b55300b03509828`.
- Expected SHA: `180edce16ffd102571914a2f5b55300b03509828`.
- Candidate reachable: yes.
- Ref drift: no.
- `origin/main` at review: `9cd47759fc25bbd34423425aa937a6e93f6eb085`.
- Candidate merge base with `origin/main`: `b942af983755639d74b24c5f41bfb47a523b749c`.

The task-input package in this workspace describes an older T1850 replay package, so this review uses the user-provided T2169 contract as authority.

## Candidate Diff Scope

The single candidate commit changes Insight presentation and acceptance evidence only:

- `web/src/utils/insightPresentation.ts`
- `web/src/pages/InsightOverview.tsx`
- `web/src/pages/InsightAgents.tsx`
- `web/src/pages/InsightProjects.tsx`
- `web/src/i18n/locales/en/insights.json`
- `web/src/i18n/locales/zh/insights.json`
- `tests/e2e/v2/capture-i53-insight-semantics.mjs`
- `docs/acceptance/i53-insight-semantics/*`

The inspected diff does not modify Insight API handlers, Collaboration Graph handlers, authorization logic, or projector code.

## Production-Like Probe

I ran the candidate binary built from `180edce16ffd102571914a2f5b55300b03509828` through `agent-center install test-instance`.

The required real data path is blocked:

```text
test_instance_with_agent_failed: create agent: status 400:
{"reason":"runtime_model_not_found","message":"runtime model was not found","details":{"model":"claude-opus-4-8"}}
```

The capture script then fell back to `--with-seed`, which produced a real isolated Web Console instance but no agent execution data:

- agents: `0`
- projects: `1`
- TaskExecution rows: `0`
- completed executions: `0`
- slot coverage: `null`
- freshness: `fresh`
- console errors: `0`

Evidence:

- `current-capture/RESULTS.json`
- `current-capture/01-overview.png`
- `current-capture/02-executions.png`
- `current-capture/04-agents.png`
- `current-capture/05-projects.png`
- `current-capture/page@4696dc120fb2b4e668b32d27d1417155.webm`

I also ran a HAR/network probe against a separate seed instance:

- No-cookie `GET /api/orgs/{slug}/insights/overview?window=24h`: `401` with `unauthenticated`.
- Authenticated overview/v2/project/Collaboration requests returned `200`.
- `GET /insights/v2/executions?window=24h` without agent/project context returned `400 execution_context_required`.
- Collaboration Graph returned a project node, no edges, and a graph version.
- Browser console capture was empty.

Evidence:

- `har-probe/insight-pages.har`
- `har-probe/api-probes.json`
- `har-probe/console.json`
- `har-probe/overview.png`
- `har-probe/executions.png`
- `har-probe/agents.png`
- `har-probe/projects.png`
- `har-probe/collaboration.png`

## Hard Gate Disposition

1. Low coverage/no data must show unknown or insufficient, not fake `0%`: **blocked**. No-data overview was captured, but low and partial coverage cases were not available.
2. Internal outcome/quality enums must be user-facing: **blocked**. There were no execution rows, so failed/recovered outcome and quality labels could not be inspected.
3. Seconds, windows, freshness readable: **partial**. Window and freshness are visible in the seed UI; execution duration samples were absent.
4. Overview -> Agent/Project -> TaskExecution IA and context: **partial**. Overview scope map and execution filter context are visible; real drilldown/return context could not be exercised.
5. Rankings show sample count/window/coverage/methodology: **blocked**. No non-empty ranking rows existed.

Per the conclusion rule, unrun product hard gates must not be rejected. Because the environment/fixture cannot produce the required real multi Agent/Project/TaskExecution dataset, the correct result is **BLOCKED**.

## Regression Gates

- `go test ./internal/insight ./internal/webconsole/api -run 'Insights|Insight' -count=1`: exit `0`.
- `cd web && pnpm exec vitest run src/utils/insightPresentation.test.ts src/pages/InsightOverview.test.tsx src/pages/InsightAgents.test.tsx src/pages/InsightProjects.test.tsx src/components/WorkItemFilterBar.test.tsx src/pages/OrgWorkItems.test.tsx`: exit `0`, `102` tests passed.
- `cd web && pnpm test`: exit `0`, `1920` tests passed.
- `cd web && pnpm run typecheck`: exit `0`.
- `cd web && pnpm run lint`: exit `0`.
- `make lint-spa-tsc`: exit `0`.
- `make lint-no-raw-colors-spa`: exit `0`.
- `go test ./internal/webconsole/api -run 'Collaboration|Authorization|Insight' -count=1`: exit `0`.
- `make build`: exit `0`.
- `node --check tests/e2e/v2/capture-i53-insight-semantics.mjs`: exit `0`.

## Conclusion

BLOCKED. Candidate ref is exact and reachable, and automated regression gates passed, but the required product acceptance could not be run with the mandated real multi-agent/project/task-execution dataset. The only observed isolated instances had seed-only data with zero executions after `--with-agent` failed on missing runtime model configuration.
