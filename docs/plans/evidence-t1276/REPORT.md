# T1276 independent verification evidence

- Verdict: **PASS**
- Baseline: `0c321c80b859e91aef37d1d4f8dbe0672fd27546`
- Remediation under review: `bacc8eaf0e6796eeb8d9f8f6dbc52387197e5997`
- Evidence branch: `ac-exec/task-23173e3b/exec-0744b1a2`
- Scope: evidence-only recovery; no merge to `main`

## Acceptance matrix

| Gate | Result | Evidence |
| --- | --- | --- |
| Real plan-tool chain | PASS | `create_plan` and `edit_plan_topology` obtain the MCP session snapshot and forward it through admin handlers into project-manager service commands. Tests: `TestCreatePlan_AutoLoadsPlanTeamRules_OK`, `TestEditPlanTopology_UsesFrozenPlanningRulesFromRequest`, and `TestSearchTools_LoadedPlanToolsAreCallable`. |
| `phase=plan` Team Memory consumption | PASS | Admin reads Team Memory through `ReadTeamRules(..., "plan")`; MCP loads `get_team_rules` with `phase: plan`. |
| Same-session freeze | PASS | `planningRuleCache` loads once and returns cloned snapshots thereafter. Tests: `TestPlanToolsFreezePlanRulesPerMCPSession` and `TestPlanRuleFreezeSurvivesSearchToolsReregister`. |
| New-generation reload boundary | PASS | The cache is owned by the MCP host and session identity includes `Generation`; a new generation creates a fresh host/cache. Configuration propagation is covered by `TestMCPHostGenerationFromEnv`. |
| Complete audit snapshot, including `enabled` | PASS | `RuleContext.Enabled` is copied from Team Memory and `PlanRuleSnapshotAudit` emits `enabled` alongside slug/title/description/body/applies_to/source_path. The snapshot also records team, commit, phase, source, session, generation, skipped paths, load error, and refresh semantics. |
| Isolation and fail-closed metadata | PASS | Cross-org team rows are treated as unreadable without leaking foreign identifiers or content; absent team/repo and malformed rules are covered by `TestCreatePlan_TeamRuleIsolation_NoTeamNoRepoCrossOrgAndBadRules`. Load failures produce an explicit `unavailable` snapshot and error text rather than silently presenting rules as loaded. |
| Execute/review/recovery regression | PASS | Existing phase mapping is unchanged and verified by `TestRulePhaseForTask_ExecuteReviewRecoveryUnchanged`. |

## Independent commands

Run from a clean worktree checked out at remediation SHA `bacc8eaf`:

```text
go test ./internal/admin/api ./internal/mcphost ./internal/agentruntime ./internal/cli
```

Result: PASS.

```text
ok github.com/oopslink/agent-center/internal/admin/api 22.246s
ok github.com/oopslink/agent-center/internal/mcphost 0.718s
ok github.com/oopslink/agent-center/internal/agentruntime 7.350s
ok github.com/oopslink/agent-center/internal/cli 11.887s
```

Repository-level backend gates:

```text
go build ./...     # PASS
go vet ./...       # PASS
gofmt -l <all changed Go files>  # PASS; no output
```

## Conclusion

The remediation satisfies the T1276 contract. The plan-authoring tools consume and preserve a frozen plan-phase Team Memory snapshot across one MCP planning session, a new supervisor generation reloads it, the audit shape retains `enabled`, isolation is enforced, and non-plan task phases remain unchanged.
