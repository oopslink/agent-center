# Team Memory rules independent acceptance - 2026-08-08

## Verdict

REJECT.

Target under test: `origin/main` / `0c321c80b859e91aef37d1d4f8dbe0672fd27546`

Acceptance branch: `acceptance/team-memory-rules-20260808-exec51ab0a1e`

Method: independent source audit and local tests from the integrated `origin/main` tree. I did not use agent-center control-plane tools, admin sockets, raw admin HTTP, worker tokens, runtime MCP config, or database files as evidence sources.

## Blocking findings

### F1 - Planning-phase Team Memory rules are not automatically loaded

Expected: Plan planning, execution, review, and recovery phases all load applicable Team Memory rules with an auditable repo commit.

Actual: `plan` exists as an accepted phase in API/MCP normalization, but I found no runtime/planner call path that invokes `loadTeamRules(..., rulePhasePlan)`. Runtime fork paths cover execute/review/recovery only:

- `internal/agentruntime/team_rules.go:11-16` defines `rulePhasePlan`.
- `internal/agentruntime/team_rules.go:82-98` derives only recovery, review, or execute from task metadata.
- `internal/agentruntime/executor_runtime.go:308` hard-codes `rulePhaseExecute` for `agent.work`.
- `internal/agentruntime/executor_runtime.go:955-957` loads `rulePhaseForTask(task)`, whose implementation never returns `plan`.
- `internal/mcphost/team_tools.go:38-40` and `internal/admin/api/agent_tools_team.go:327-340` expose `plan` as a manual `get_team_rules` phase, but that is not automatic planning-stage context loading.

Minimal repro:

```bash
rg -n "rulePhasePlan|loadTeamRules\([^\n]*rulePhasePlan|rulePhaseForTask|get_team_rules.*plan|phase.*plan" internal/agentruntime internal/admin/api internal/mcphost
```

Observed relevant output:

```text
internal/agentruntime/team_rules.go:12: rulePhasePlan = "plan"
internal/agentruntime/team_rules.go:82: func rulePhaseForTask(task *centerTaskDetail) string {
internal/agentruntime/team_rules.go:102: case rulePhasePlan:
internal/agentruntime/executor_runtime.go:956: item.RuleSnapshot = r.loadTeamRules(ctx, agentID, rulePhaseForTask(task))
internal/mcphost/team_tools.go:39: ... phase (plan, execute, review, recovery) ...
internal/admin/api/agent_tools_team.go:280: ... phase must be one of plan, execute, review, recovery
```

No `loadTeamRules(... rulePhasePlan)` caller was found.

### F2 - Full Go regression gate is red on this checkout

`go test ./...` failed in two non-Team-Memory packages:

```text
FAIL github.com/oopslink/agent-center/internal/agentruntime/executor
--- FAIL: TestExecGitRunner_ContextCancelKillsGitProcessGroup
    worktree_test.go:79: fake ssh was not invoked: open .../ssh.pid: no such file or directory

FAIL github.com/oopslink/agent-center/internal/workerdaemon
--- FAIL: TestSupervisorSession_DetachSurvives (48.28s)
    supervisor not reattachable within 45s + 45s generous dial: state=1 reason=dead pidAlive=true
```

Both failed tests passed when rerun individually:

```bash
go test ./internal/agentruntime/executor -run TestExecGitRunner_ContextCancelKillsGitProcessGroup -count=1
go test ./internal/workerdaemon -run TestSupervisorSession_DetachSurvives -count=1
```

The isolated pass makes this look timing-sensitive, but the all-package gate still failed and is a release/regression risk.

## Coverage matrix

| Area | Result | Evidence |
|---|---:|---|
| Team repo writes real `entries/` and `rules/` | PASS | `internal/cognition/memory/centergit/store.go:18-27`, `171-240` write entries/rules into separate directories. |
| `MEMORY.md` two indexes, derived only | PASS | `store.go:528-573` regenerates `## Entries` and `## Rules` from directory contents. |
| No rule `kind` double truth | PASS | `store.go:69-110` makes `rules/` the type boundary and has no rule kind/type field; `store_test.go:101` forbids `kind: rule` and `type: rule`. |
| Runtime snapshot commit/version audit | PARTIAL | `consumer.go:25-35`, `87-143` snapshots phase, commit, skipped files, and refresh semantics; `orchestrator/engine.go:346-390` renders `team=<id> commit=<sha>`. Blocked for planning stage by F1. |
| `enabled` / `applies_to` filtering | PASS | `store.go:114-133`, `670-711` filters disabled rules and normalizes phases; tests cover execute vs review/off rules. |
| Refresh semantics | PASS | `consumer.go:35` documents fork-time snapshot semantics and tier-1/tier-2 vs fresh fork/tier-3 reload behavior; API returns it via `agent_tools_team.go:370-377`. |
| Legacy workflow-template migration ownership | PASS | `internal/team/migration/legacy_workflow_templates.go:84-128` claims only uniquely owned agent-created templates; `222-234` maps them to enabled `plan` rules. |
| Unknown/ambiguous/builtin legacy templates not broadcast | PASS | `legacy_workflow_templates.go:130-168` writes only planned claims; test `legacy_workflow_templates_test.go:47-81` asserts unrelated team repo is not provisioned. |
| `/templates` and `/teams/templates` routed pages gone | PASS | `web/src/App.tsx:139-140` routes team template URLs to `NotFound`; `git ls-tree` found no `OrgTemplates` or `TeamTemplates` page files. |
| Workspace / Team nav no Templates item | PASS | `web/src/App.test.tsx:156`, `WorkspaceSecondaryNav.test.tsx:49`, and `TeamUISecondaryNav.test.tsx:21` assert absence. |
| Team Memory same-page grouping/filtering | PASS | `web/src/components/teams/MemoryPane.tsx:85-212`, `287-333` groups `MEMORY.md`, `entries/`, `rules/`, filters entries/rules, and derives rules from group/path prefix rather than text. |
| UI permission / empty / error states | PASS | `MemoryPane.tsx:340-373`; tests in `TeamDetail.test.tsx:258-373` cover unavailable, manage, read-only, rules empty, and forbidden doc states. |
| `template_ref` / template consumer scan | PASS with residual compatibility surface | Legacy `workflow_template_ref` remains as documented compatibility in team templates (`team_tools.go:54-55`, `241-247`; `agent_tools_team.go:558-573`, `891-900`). Deprecated MCP workflow-template tools remain but descriptions point to Team Memory rules (`server.go:493-513`). Web Console still mounts `/api/orgs/{slug}/team-templates` endpoints (`internal/webconsole/api/server.go:300-309`) and FE API hooks/mocks remain (`web/src/api/teams.ts`, `web/src/mocks/teamHandlers.ts`), but no routed page or nav consumer remains. |

## Test and build evidence

Passed targeted backend tests:

```bash
go test ./internal/cognition/memory/centergit ./internal/team/migration ./internal/agentruntime ./internal/agentruntime/orchestrator ./internal/admin/api ./internal/mcphost
```

Result: all six packages passed.

Passed frontend install/build/tests:

```bash
pnpm --dir web install --frozen-lockfile
pnpm --dir web test -- web/src/pages/TeamDetail.test.tsx web/src/App.test.tsx web/src/shell/nav/TeamUISecondaryNav.test.tsx web/src/shell/nav/WorkspaceSecondaryNav.test.tsx web/src/api/teams.test.tsx
pnpm --dir web run build
```

Result: Vitest completed `184` test files / `1689` tests passing. SPA build passed with existing CSS minify and large chunk warnings.

Passed pure Go build:

```bash
go build ./...
```

Failed all-package Go regression:

```bash
go test ./...
```

Result: failed as described in F2.

## Conclusion

The storage contract, API/MCP rule loading primitive, migration ownership rules, Team Memory UI grouping/filtering, permissions, empty states, and old template page/nav retirement are mostly implemented and covered.

I reject the integrated closeout because planning-stage automatic rule loading is not wired, and because the full Go regression gate failed on this checkout. Minimum fix for F1 is to add an actual planner/planning-stage call path that loads `get_team_rules` with phase `plan`, persists/renders the resulting commit-bearing snapshot, and tests that `plan` rules enter planning context without requiring the agent to manually call the MCP tool.
