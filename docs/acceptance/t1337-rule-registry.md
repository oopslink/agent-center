# T1337 Rule Registry independent acceptance

Candidate: `d9764333d824cfa77f6338d1270d55798b5aea5b`

Verdict: PASS against the nine contracts in
`docs/design/features/2026-08-13-team-rule-registry.md` section 9.

## Evidence

1. A dedicated regression test creates 100 rules with 32 KiB bodies, partitions them
   across the four phases, and proves the execute startup index contains only its 25
   index entries, stays below 16 KiB, and contains no body marker.
2. `TestTeamTools_GetTeamRuleIndexAndCommitBoundBodyRead` proves the index response has
   no body and body reads require the frozen commit.
3. `TestTeamRuleIndexAndBodyReadAreCommitBound` moves HEAD and proves the old commit
   still returns the old body.
4. Consumer and API tests prove missing commit and wrong-phase reads fail closed.
   Disabled rules are excluded by the same phase-scoped index/read path; a commit from
   another team's repository is absent from the resolved team's git object database and
   maps to `rule_snapshot_not_found`.
5. API and SQLite tests prove duplicate execution/planning-session reads persist one
   idempotent audit row; the audit schema does not store rule bodies.
6. `TestTaskExecutionsIncludesTeamRuleAuditSnapshot` proves snapshot metadata and
   sorted, de-duplicated `loaded_rule_ids` in the TaskExecution read model.
7. Runtime tests prove a fresh execution carries a newly loaded snapshot and recovery
   preserves the existing `input.json` snapshot without refreshing the index.
8. The legacy `get_team_rules` contract and MCP registration tests remain green.
9. Targeted package tests and relevant race tests pass. `go test ./...` had one unrelated
   first-run failure in `TestExecGitRunner_ContextCancelKillsGitProcessGroup` because the
   fake SSH pid file was not created within its one-second window; the exact test then
   passed 10/10. All Rule Registry, admin API, E2E and integration packages passed.

## Commands

```text
go test ./internal/cognition/memory/centergit ./internal/cognition/ruleregistry/sqlite ./internal/admin/api ./internal/agentruntime ./internal/agentruntime/orchestrator ./internal/mcphost ./internal/projectmanager/service
go test -race ./internal/admin/api -run 'TestTeamTools_GetTeamRuleIndexAndCommitBoundBodyRead|TestTaskExecutionsIncludesTeamRuleAuditSnapshot' -count=1
go test -race ./internal/cognition/ruleregistry/sqlite -count=3
go test ./...
go test ./internal/agentruntime/executor -run TestExecGitRunner_ContextCancelKillsGitProcessGroup -count=10
```

