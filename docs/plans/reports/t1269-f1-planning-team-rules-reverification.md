# T1269 F1 Planning Team Rules Reverification

Date: 2026-08-08

Base:

- Branch: `ac-exec/task-f13f9594/exec-8dfbf25c`
- `HEAD`: `0c321c80b859e91aef37d1d4f8dbe0672fd27546`
- `origin/main`: `0c321c80b859e91aef37d1d4f8dbe0672fd27546` after `git fetch origin main`

Conclusion: REJECT

## Blocking Finding

The Team rules reader exists and can return phase-filtered rule snapshots, but the
real planning production entrypoints do not automatically load `phase=plan`
Team rules.

Evidence:

- `internal/admin/api/agent_tools_team.go:267` implements `get_team_rules`.
  It resolves the agent's team, enforces same-org team ownership, returns empty
  snapshots for no team/no repo, and reads `centergit.ReadTeamRules(team, phase)`.
- `internal/cognition/memory/centergit/consumer.go:91` reads enabled rules that
  apply to the requested phase and records the team repo commit plus refresh
  semantics.
- `internal/admin/api/agent_tools_team.go:353` includes response metadata:
  `commit`, `enabled`, `applies_to`, `source_path`, and
  `refresh_semantics`.
- Runtime automatic loading only occurs at:
  - `internal/agentruntime/executor_runtime.go:308`: `workViaExecutor` always
    loads `rulePhaseExecute`.
  - `internal/agentruntime/executor_runtime.go:956`: `SpawnExecutor` loads
    `rulePhaseForTask(task)`.
- `internal/agentruntime/team_rules.go:82` `rulePhaseForTask` returns only
  `recovery`, `review`, or `execute`; it never returns `plan`.
- `internal/mcphost/tools.go:845` `makeCreatePlan` directly calls admin
  `create_plan`.
- `internal/mcphost/tools.go:959` `makeEditPlanTopology` directly calls admin
  `edit_plan_topology`.
- `internal/admin/api/agent_tools_plans.go:114` and `:307` directly call
  `PMService.CreatePlan` / `PMService.EditPlanTopology` with no Team-rules
  load.

Therefore the code supports a manual `get_team_rules(phase=plan)` tool call, but
there is no non-helper production path that automatically injects plan-phase
rules into plan creation/topology orchestration.

## Isolation / Regression Review

- No Team: `getTeamRulesHandler` returns `emptyTeamRulesView("", phase)`.
- No repo: `getTeamRulesHandler` returns `emptyTeamRulesView(teamID, phase)`
  when `TeamGitHost` is absent; `ReadTeamRules` returns an empty snapshot when
  the team repo does not exist.
- Cross org: `getTeamRulesHandler` rejects if the resolved team org differs
  from the calling agent org.
- Malformed/nonstandard rule files: `ReadTeamRules` reports skipped files and
  does not include them as active rules.
- Execute/review/recovery: still routed by `rulePhaseForTask` and existing
  tests pass.
- Running executors do not refresh: `RuleSnapshot` is copied into `input.json`
  before launch, and tier-1/tier-2 recovery reuses the persisted argv/workspace.
- Tier-3 reset leads to a fresh fork, but the fresh fork still derives phase via
  `rulePhaseForTask`; because that function cannot return `plan`, tier-3 does
  not repair planning-rule loading.

Additional input-shape gap: executor `input.json` rule context
(`internal/agentruntime/executor/protocol.go:117`) carries `applies_to` and
`source_path`, but not `enabled`. The admin response carries `enabled`, and the
consumer filters to enabled rules, but the persisted executor input does not
include an `enabled` field.

## Minimal Repro

Run:

```sh
git fetch origin main
git rev-parse HEAD origin/main
rg -n "item.RuleSnapshot = r.loadTeamRules" internal/agentruntime/executor_runtime.go
nl -ba internal/agentruntime/team_rules.go | sed -n '82,98p'
nl -ba internal/mcphost/tools.go | sed -n '845,856p;959,976p'
nl -ba internal/admin/api/agent_tools_plans.go | sed -n '114,153p;307,352p'
```

Observed on `0c321c80b859e91aef37d1d4f8dbe0672fd27546`:

- The only automatic runtime loads are execute or `rulePhaseForTask(task)`.
- `rulePhaseForTask` has no `plan` branch.
- `create_plan` and `edit_plan_topology` are direct passthroughs to admin/PM
  services with no `get_team_rules(phase=plan)` call.

## Verification Commands

All commands below exited 0:

```sh
go test ./internal/cognition/memory/centergit ./internal/team/migration ./internal/admin/api ./internal/mcphost ./internal/agentruntime ./internal/agentruntime/orchestrator ./internal/workerdaemon -count=1
go test -race -count=1 ./internal/agentruntime/...
go test ./tests/e2e -run TestForkExecutorDeployedBinary_AlreadyRunningTask -count=1 -v
go test ./tests/integration -count=1 -v
make build
make lint
```

`make build` emitted existing Vite CSS/chunk-size warnings but completed with
exit code 0.
