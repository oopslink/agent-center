# T2169 Insight Hard-Gate Revalidation

Verdict: **BLOCKED**. This is not a product REJECT.

## Binding

- Fixed product candidate: `ac-exec/task-54483391/exec-eab25743@180edce16ffd102571914a2f5b55300b03509828`.
- Local ref check: `git rev-parse ac-exec/task-54483391/exec-eab25743` returned `180edce16ffd102571914a2f5b55300b03509828`; reviewed SHA did not drift.
- Referenced T2202 evidence commit exists locally: `d85a95ec4cb3aad8f4ca48707c28f68ad0f1262c`.
- Note: the T2202 evidence JSON embedded in that commit records `reviewed_sha=39fc032aa2bbecdcbb807cc3cd626f1e6b24f615`, so this revalidation independently binds its verdict to `180edce16ffd102571914a2f5b55300b03509828`.

## What Was Unblocked

The known `runtime_model_not_found` blocker was resolved through the official Web API:

- Started isolated instance with `install test-instance --with-seed --workers 1`.
- Initial AI Runtime catalog had CLI entries `claude-code` and `codex`, and no models.
- `POST /api/orgs/{org}/ai-runtime/models` created `claude-opus-4-8` compatible with `claude-code`; response `201`, catalog revision `1`.
- Org-scoped `admintoken/mint-enroll` plus `install worker` enrolled worker `worker-c1a2d1de`; worker was online under org `organization-f2b21ba6`.
- Created and started 3 Agents via `/members/agent` and `/agents/{id}/start`.
- Created 6 dispatched project tasks via `/projects/{project}/tasks` with `dispatch:true`.

## Remaining Blocker

No TaskExecution fixture could be produced without violating this executor's isolation contract.

Primary `claude-code/claude-opus-4-8` attempt:

- Agents reached `lifecycle=running`, `availability=available`.
- Final Agent readback showed `last_activity_content="Failed to authenticate: OAuth session expired and could not be refreshed"`.
- Worker log showed `agent.work_available` delivered, then `SUPERVISOR-WAKE ... executor fork requires explicit fork_executor`.
- `/insights/executions?window=24h&limit=100` stayed empty through 24 polls.

Secondary `codex/gpt-5-codex` attempt:

- Registered `gpt-5-codex` compatible with `codex`.
- Agent readback showed `control loaded: cli=codex model=gpt-5-codex session=codex mcp=preflight_ok`.
- The dispatched task remained pending; `/insights/executions` stayed empty through 12 polls.

The only missing capability/config is a valid Agent supervisor runtime path that can call the supervisor-only `fork_executor` MCP tool. This isolated executor has no permitted Agent Center MCP/control-plane tools, and the task contract forbids using worker tokens, admin endpoints, SQLite, admin sockets, runtime config files, or raw admin HTTP as a fallback.

## Hard Gates

All five product hard gates remain **NOT_RUN** because the required real TaskExecution data was not available:

- Unknown / insufficient coverage.
- Internal enum userization.
- Seconds / window / freshness readability.
- Overview -> Agent/Project -> TaskExecution drilldown with preserved filter context.
- Ranking sample count / window / coverage / methodology.

UI pages were still captured against the real isolated instance for evidence: overview, agents, projects, executions, HAR, and console summary. Console/pageerror count was 0.

## Evidence

- Evidence root: `docs/acceptance/t2169-insight-hard-gates/`.
- Raw API responses and command exits: `raw/`.
- Screenshots: `screenshots/01-overview.png`, `02-agents.png`, `03-projects.png`, `04-executions.png`.
- HAR: `har/insight-hard-gates.har`.
- Fixture hash manifest: `SHA256SUMS`.
- Build SHA: candidate binary built from `180edce16ffd102571914a2f5b55300b03509828`.

## Cleanup

- Primary instance `t2169-hard-195120` uninstalled successfully after correcting the unsupported `--yes` flag.
- Secondary instance `t2169-codex-195806` uninstalled successfully.
- Extra org-bound worker launchd labels created by the manual formal enroll path were booted out.
- Final `list-test-instances --output json` returned `null`.
