# T1282 Team Memory Controlled Writes Pre-Merge Acceptance

Date: 2026-08-09

Verdict: REJECT

This evidence was produced in an isolated executor workspace. No production
implementation files were edited by this validation; only this evidence document
was added after forming local test-merge commits.

## Scope And Inputs

Expected contract: controlled Team Memory writes must pass as an integrated
pre-merge tree, not as isolated unit tests. Required coverage includes real bare
repo add/update/disable/delete, UUID/blob CAS, idempotency, concurrent proposals,
single-winner promotion, deterministic MEMORY index, auth matrix, safety gates,
safe rendering, proposal exclusion from get_team_rules, generation/fork freeze,
and Git-to-events restart reconciliation.

Remote refs were verified with `git ls-remote --heads origin` and local
remote-tracking refs after `git fetch origin --refmap='' 'refs/heads/*:refs/remotes/origin/*'`.
The repository's configured fetch refspec is `+refs/*:refs/*`, so the first
plain fetch was intentionally not reused after Git refused to write a branch
checked out by another worktree.

| Input | Remote ref | SHA | merge-base with origin/main | Result |
|---|---|---:|---:|---|
| main | refs/heads/main | `0656908def91c7d3d7d53fce7a4a4183d7622bb1` | n/a | baseline |
| T1283 | refs/heads/ac-exec/task-dbd3bb1a/exec-5480b38f | `fe5e05c64ee7dd9332007d0a23ef5faf9c9cbeb4` | `1b53eb037d121185e87db444af05f6d4f36195fb` | contained by T1286 recovery |
| T1284 | refs/heads/ac-exec/task-f7f20103/exec-64e804aa | `ab75bde95731e4d37ae72ed4eb1443cf416d5c71` | `1b53eb037d121185e87db444af05f6d4f36195fb` | merged into test branch, build fails |
| T1286 recovery | refs/heads/recovery/t1286-mcp-admin-security-observability | `005df8e1299b5c9997647b1a5ae2ccbe4aa2b844` | `1b53eb037d121185e87db444af05f6d4f36195fb` | conflicts with current main/test branch |
| T1290 candidate | refs/heads/ac-exec/task-44d3e9f1/exec-2d8541f4 | `11f1b2a176020a45d8f9521c023c3cd4b5b39603` | `0656908def91c7d3d7d53fce7a4a4183d7622bb1` | only remote branch found with current-main Team Memory proposal recovery shape |

I did not have access to supervisor plan messages in this isolated workspace;
the T1290 candidate is identified from remote refs, commit date, subject
`feat(memory): add controlled team memory proposals`, and merge-base on current
main.

## Test Merge Attempts

### Current-main test merge

Command:

```text
git switch -c premerge/t1282-team-memory-validation origin/main
git merge --no-ff 11f1b2a176020a45d8f9521c023c3cd4b5b39603 -m "test-merge: integrate T1290 team memory recovery"
git merge --no-ff ab75bde95731e4d37ae72ed4eb1443cf416d5c71 -m "test-merge: integrate T1284 team memory web review"
```

Expected: a buildable integrated tree containing current main + T1290 + T1284.

Actual: both merges completed, producing:

```text
63ae62e11fb4e538d688ae522471d29a107e70f3 test-merge: integrate T1290 team memory recovery
28f032a1275e3ea70e680f79d0a7ddf22b1cd8c9 test-merge: integrate T1284 team memory web review
```

Judgement: merge step PARTIAL PASS; build step REJECT.

### T1286/T1283 on current main

Command:

```text
git merge --no-ff --no-commit 005df8e1299b5c9997647b1a5ae2ccbe4aa2b844
```

Expected: T1286 recovery, which contains T1283, can be added to the current
test-merge without manual production edits.

Actual output:

```text
Auto-merging internal/admin/api/agent_tools_team_memory.go
CONFLICT (add/add): Merge conflict in internal/admin/api/agent_tools_team_memory.go
Auto-merging internal/agentruntime/executor_runtime.go
Auto-merging internal/cli/handlers_migrate_v1_to_v2.go
CONFLICT (content): Merge conflict in internal/cli/handlers_migrate_v1_to_v2.go
Auto-merging internal/cognition/memory/teammemory/projector.go
CONFLICT (add/add): Merge conflict in internal/cognition/memory/teammemory/projector.go
Auto-merging internal/cognition/memory/teammemory/service_test.go
CONFLICT (add/add): Merge conflict in internal/cognition/memory/teammemory/service_test.go
Auto-merging internal/concurrency/concurrency.go
CONFLICT (content): Merge conflict in internal/concurrency/concurrency.go
Auto-merging internal/mcphost/team_tools.go
CONFLICT (content): Merge conflict in internal/mcphost/team_tools.go
Auto-merging internal/team/errors.go
CONFLICT (content): Merge conflict in internal/team/errors.go
Automatic merge failed; fix conflicts and then commit the result.
```

Unmerged paths also included migration tests and conversation upgrade tests.

Judgement: REJECT. The recovery branch cannot enter the current pre-merge tree
without manual integration across MCP, projector, concurrency, team errors, and
migration expectations.

### Old-base combination check

Command:

```text
git worktree add -b premerge/t1282-oldbase-combo /tmp/t1282-oldbase-combo 1b53eb037d121185e87db444af05f6d4f36195fb
git merge --no-ff 005df8e1299b5c9997647b1a5ae2ccbe4aa2b844 -m "test-merge: integrate T1286 recovery on old base"
git merge --no-ff --no-commit ab75bde95731e4d37ae72ed4eb1443cf416d5c71
git commit -m "test-merge: integrate T1284 web on T1286 old base"
go test ./internal/cognition/memory/centergit ./internal/cognition/memory/teammemory ./internal/admin/api ./internal/mcphost ./internal/webconsole/api
```

Expected: the three older deliveries can at least build together on their common
base.

Actual output:

```text
# github.com/oopslink/agent-center/internal/cognition/memory/centergit
internal/cognition/memory/centergit/team_memory_service.go:20:2: proposalsDir redeclared in this block
	internal/cognition/memory/centergit/team_memory_repository.go:24:7: other declaration of proposalsDir
internal/cognition/memory/centergit/team_memory_service.go:194:6: proposalFrontmatter redeclared in this block
	internal/cognition/memory/centergit/team_memory_repository.go:891:6: other declaration of proposalFrontmatter
internal/cognition/memory/centergit/team_memory_service.go:353:3: unknown field ID in struct literal of type proposalFrontmatter
internal/cognition/memory/centergit/team_memory_service.go:354:3: unknown field UUID in struct literal of type proposalFrontmatter
internal/cognition/memory/centergit/team_memory_service.go:357:3: unknown field Slug in struct literal of type proposalFrontmatter
internal/cognition/memory/centergit/team_memory_service.go:358:3: unknown field Title in struct literal of type proposalFrontmatter
internal/cognition/memory/centergit/team_memory_service.go:359:3: unknown field Description in struct literal of type proposalFrontmatter
internal/cognition/memory/centergit/team_memory_service.go:362:3: unknown field UpdatedAt in struct literal of type proposalFrontmatter
internal/cognition/memory/centergit/team_memory_service.go:363:3: unknown field Enabled in struct literal of type proposalFrontmatter
internal/cognition/memory/centergit/team_memory_service.go:714:6: parseProposalFile redeclared in this block
	internal/cognition/memory/centergit/team_memory_repository.go:1004:6: other declaration of parseProposalFile
FAIL	github.com/oopslink/agent-center/internal/cognition/memory/centergit [build failed]
ok  	github.com/oopslink/agent-center/internal/cognition/memory/teammemory	0.463s
FAIL	github.com/oopslink/agent-center/internal/admin/api [build failed]
ok  	github.com/oopslink/agent-center/internal/mcphost	0.672s
FAIL	github.com/oopslink/agent-center/internal/webconsole/api [build failed]
FAIL
```

Judgement: REJECT. T1284 is not build-compatible with the T1283/T1286 service
implementation even before current main integration.

## Build And Test Evidence

Command:

```text
go test ./internal/cognition/memory/centergit ./internal/cognition/memory/teammemory ./internal/admin/api ./internal/mcphost ./internal/webconsole/api
```

Expected: all relevant service, MCP, and Web API packages compile and pass.

Actual output:

```text
# github.com/oopslink/agent-center/internal/cognition/memory/centergit
internal/cognition/memory/centergit/team_memory_service.go:20:2: proposalsDir redeclared in this block
	internal/cognition/memory/centergit/proposal.go:21:7: other declaration of proposalsDir
internal/cognition/memory/centergit/team_memory_service.go:194:6: proposalFrontmatter redeclared in this block
	internal/cognition/memory/centergit/proposal.go:412:6: other declaration of proposalFrontmatter
internal/cognition/memory/centergit/team_memory_service.go:353:3: unknown field ID in struct literal of type proposalFrontmatter
internal/cognition/memory/centergit/team_memory_service.go:354:3: unknown field UUID in struct literal of type proposalFrontmatter
internal/cognition/memory/centergit/team_memory_service.go:357:3: unknown field Slug in struct literal of type proposalFrontmatter
internal/cognition/memory/centergit/team_memory_service.go:358:3: unknown field Title in struct literal of type proposalFrontmatter
internal/cognition/memory/centergit/team_memory_service.go:359:3: unknown field Description in struct literal of type proposalFrontmatter
internal/cognition/memory/centergit/team_memory_service.go:362:3: unknown field UpdatedAt in struct literal of type proposalFrontmatter
internal/cognition/memory/centergit/team_memory_service.go:363:3: unknown field Enabled in struct literal of type proposalFrontmatter
internal/cognition/memory/centergit/team_memory_service.go:364:3: unknown field AppliesTo in struct literal of type proposalFrontmatter
internal/cognition/memory/centergit/team_memory_service.go:364:3: too many errors
FAIL	github.com/oopslink/agent-center/internal/cognition/memory/centergit [build failed]
FAIL	github.com/oopslink/agent-center/internal/cognition/memory/teammemory [build failed]
FAIL	github.com/oopslink/agent-center/internal/admin/api [build failed]
ok  	github.com/oopslink/agent-center/internal/mcphost	0.742s
FAIL	github.com/oopslink/agent-center/internal/webconsole/api [build failed]
FAIL
```

Judgement: REJECT. The red signal is unique and reproducible: duplicate
Team Memory proposal implementations in `internal/cognition/memory/centergit`.

Command:

```text
go test ./...
```

Expected: full Go suite builds and passes before any real acceptance smoke.

Actual: failed. The first blocker is the same `centergit` compile error above.
The full run also showed downstream build failures in `cmd/agent-center`,
`internal/admin/api`, `internal/cli`, `internal/webconsole/api`, worker/supervisor
integration tests that build the binary, `tests/e2e`, and `tests/integration`.
One unrelated-looking executor test also failed:

```text
--- FAIL: TestExecGitRunner_ContextCancelKillsGitProcessGroup (1.00s)
    worktree_test.go:79: fake ssh was not invoked: open .../ssh.pid: no such file or directory
```

Judgement: REJECT.

## Contract Findings

### F1: Integrated tree does not compile

Expected: Service/MCP/observability/Web deliveries merge into one buildable tree.

Actual:

- T1290 adds `internal/cognition/memory/centergit/proposal.go` with
  `const proposalsDir` and `type proposalFrontmatter`.
- T1284 adds `internal/cognition/memory/centergit/team_memory_service.go` with
  another `proposalsDir` and another incompatible `proposalFrontmatter`.
- T1283/T1286 add `team_memory_repository.go` with the same collision against
  T1284 on the old base.

Judgement: REJECT.

### F2: Web and MCP are not using one TeamMemoryService contract

Expected: Web and MCP call the same service command surface, including
`expected_repo_commit`, `expected_proposal_status=pending`, review comment, and
warning-code acknowledgement for promotion/rejection.

Actual:

- MCP adapter `internal/admin/api/agent_tools_team_memory.go` constructs
  `teammemory.NewService(...).Review(...)` and passes `expected_repo_commit`,
  `expected_proposal_status`, `comment`, and `acknowledge_warnings`.
- Web handler `internal/webconsole/api/handlers_teams_p2.go` calls
  `d.TeamMemory.PromoteProposal(...)` / `RejectProposal(...)` on the separate
  `centergit.TeamMemoryService`.
- Web promote request only accepts `warning_acknowledged`; Web reject request
  only accepts `reason`.
- Frontend `web/src/api/teams.ts` posts `{}` for promote and `{reason}` for
  reject, with no expected commit/status/comment.

Judgement: REJECT. The Web path cannot prove promotion CAS, status CAS, or
review-comment enforcement.

### F3: Web service does not implement add/update/disable/delete contract

Expected: proposal command supports `operation=add|update|disable|delete` and
uses UUID/blob CAS for update, disable, and delete.

Actual:

- Web `CreateTeamMemoryProposalInput` contains target kind, slug, title,
  description, body, enabled, applies_to, and warning_acknowledged.
- It has no operation, target source path, target UUID, or expected blob hash.
- `PromoteProposal` writes a new entry/rule through `store.WriteEntry` or
  `store.WriteRule`, then `SyncPush`; it does not perform update, disable, or
  delete CAS semantics.

Judgement: REJECT for Web contract parity and CRUD coverage.

### F4: Web mutation path does not participate in Git-to-events projector

Expected: Git transition is authoritative and a durable projector reconciles
Git proposal transitions to observability events after restart.

Actual:

- MCP path creates `teammemory.NewProjector(...)` when event repo and DB are
  available.
- Web path calls the separate `centergit.TeamMemoryService`; no projector is
  wired in `handlers_teams_p2.go`.

Judgement: REJECT. Even if the tree compiled, Web promotions would not prove the
Git-to-events restart reconciliation contract.

### F5: Static safe-rendering check is partial only

Expected: Markdown is rendered as untrusted content with HTML/URL safety.

Actual:

- `MemoryPane.tsx` uses `ReactMarkdown` with custom `safeMarkdownComponents`.
- Links/images go through `safeUrl`; `javascript:` is rejected and a unit test
  fixture exists in `TeamDetail.test.tsx`.
- Web tests were not run in this executor because the integrated backend/API
  tree is already build-red, and `web/node_modules` was absent.

Judgement: PARTIAL STATIC PASS, not acceptance PASS.

## Acceptance Matrix

| Item | Verdict | Evidence |
|---|---|---|
| Remote refs/SHA/merge-base verified | PASS | table above |
| Current-main test merge | REJECT | T1286/T1283 conflicts with current tree; T1290+T1284 compile fails |
| Relevant Go package tests | REJECT | duplicate `proposalsDir` / `proposalFrontmatter` compile failure |
| Full Go test | REJECT | `go test ./...` failed |
| Real bare repo add/update/disable/delete | BLOCKED/REJECT | integrated tree does not compile; Web path lacks update/disable/delete |
| UUID/blob CAS | BLOCKED/REJECT | integrated tree does not compile; Web path lacks expected blob hash |
| Idempotency | BLOCKED/REJECT | not runnable on integrated tree |
| Concurrent proposals not lost | BLOCKED/REJECT | not runnable on integrated tree |
| Same proposal promotion single winner | BLOCKED/REJECT | not runnable; Web path lacks expected commit/status |
| Deterministic MEMORY index | BLOCKED/REJECT | not runnable on integrated tree |
| member/curator/revoked/cross-org/human owner-admin-member matrix | BLOCKED/REJECT | not runnable on integrated tree |
| secret/path/frontmatter/size gates | BLOCKED/REJECT | not runnable; two service implementations diverge |
| Safe rendering | PARTIAL | static code path and test fixture present; not a full acceptance pass |
| Proposal excluded from get_team_rules | BLOCKED/REJECT | not runnable on integrated tree |
| Promotion only affects new generation/fork | BLOCKED/REJECT | not runnable on integrated tree |
| Git-to-events restart backfill | BLOCKED/REJECT | not runnable; Web path lacks projector |

## Final Judgement

REJECT.

The deliveries are not pre-merge ready. The integrated tree is build-red, the
older common-base combination is also build-red, T1286/T1283 cannot be added to
current main without manual integration conflicts, and Web/MCP expose divergent
Team Memory mutation contracts. No real bare repo or runtime acceptance item can
be counted as passed until the service boundary is unified and the combined tree
builds.
