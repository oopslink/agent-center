# Codebase Residue Review - 2026-08-26

## Scope

This report expands the 2026-08-22 code audit for task #196. It
rechecks the 12 originally reported findings against current `main` and
turns them into an actionable repair plan.

Baseline checked:

- Repository: `agent-center`
- Local path: `/Users/oopslink/works/codes/oopslink/ttt/agent-center`
- Baseline commit: `5a9531969f9f032c106401cc2f874df2f09a1f1e`
  (`feat(web): bulk archive project plans`)
- Existing unrelated local dirty file intentionally ignored:
  `docs/design/features/2026-08-17-access-management-redesign.md`

The review is about code shape and maintenance risk. It does not claim
that all items are user-facing bugs today. Several findings are dead
code or stale implementation drift that increase the chance of future
wrong fixes.

## Executive Summary

The 12 original findings resolve as follows:

| # | Area | Verdict | Priority |
|---|---|---|---|
| 1 | SPA `typecheck` script | Confirmed issue | P0 |
| 2 | Workforce BootstrapToken / Exchange | Confirmed product/implementation drift | P2 decision |
| 3 | Agent adapter `ParseEvent` / unknown event reporter | Confirmed dead wiring | P1/P2 |
| 4 | Old direct Claude session and MCP secret injection | Confirmed dead and security-sensitive residue | P1/P2 |
| 5 | `peek-trace` | Confirmed retired feature residue | P2 |
| 6 | AI Runtime resolver / legacy adapter | Confirmed unused production path | P2 |
| 7 | Team legacy workflow-template migration helper | Confirmed uncalled migration library | P2/P3 |
| 8 | SPA unused API hooks | Confirmed mixed unused surface | P2/P3 |
| 9 | `@headlessui/react` dependency | Confirmed unused dependency | P0 |
| 10 | `pmUnblockTaskHandler` message-id validation | Confirmed data integrity gap | P1 |
| 11 | Codex executor usage | Original finding corrected: implemented, stale comments remain | P0 cleanup |
| 12 | Ring buffers and stubs | Ring duplication is intentional; stubs confirmed removable | P0/P3 |

Recommended order:

1. **P0 cleanup PR**: fix SPA typecheck script, remove unused
   Headless UI dependency/comment, remove stale Codex TODO/comments,
   delete obvious unused stubs. Low product risk, quick validation.
2. **P1 data correctness PR**: validate `input_request_message_id` in
   `pmUnblockTaskHandler` before writing input replies.
3. **P1/P2 runtime residue PR**: delete or quarantine dead
   adapter/direct-session/MCP secret-injection paths, then remove
   `secret:resolve` from worker token scope only after confirming no
   live caller remains.
4. **P2 product decisions**: choose whether BootstrapToken v2 enroll,
   `peek-trace`, and AI Runtime resolver snapshots are official product
   directions or retired code.
5. **P2/P3 API/docs cleanup**: process unused hooks, team migration
   helper, and documentation drift in smaller topic PRs.

## Findings

### 1. SPA `typecheck` Script Is Misleading

**Verdict:** confirmed.

**Evidence:**

- `web/package.json` still defines:
  - `typecheck`: `tsc --noEmit`
- `Makefile` defines the real SPA TypeScript gate:
  - `lint-spa-tsc`: `cd web && npx tsc -b --force`
- The Makefile comment explicitly says project references require
  `tsc -b --force`; plain `tsc --noEmit` does not reliably traverse
  the referenced project graph.

**Risk:**

Developers can run `pnpm --prefix web run typecheck` and get a false
green that does not match the build/lint gate. This is worse than a
missing script because it creates confidence in the wrong check.

**Fix plan:**

- Change `web/package.json`:
  - from `tsc --noEmit`
  - to `tsc -b --force`
- Optionally make `make lint-spa-tsc` call `pnpm --prefix web run
  typecheck` so there is exactly one command definition.

**Validation:**

- `pnpm --prefix web run typecheck`
- `make lint-spa-tsc`
- `make lint`

### 2. Workforce BootstrapToken / Exchange Is Not Wired Into Production

**Verdict:** confirmed, but product direction must be decided before
cleanup.

**Evidence:**

- The production worker enroll endpoint remains
  `/admin/workforce/worker/enroll`.
- Worker code calls `EnrollWithExchange`, but that method posts to the
  same enroll endpoint and receives a long-term token from the current
  admin-token flow.
- `NewWorkerEnrollService` is the live service constructor used by
  production wiring.
- `NewWorkerEnrollServiceV2`, `BootstrapTokenService`, and
  BootstrapToken repository/service methods are primarily exercised by
  tests and not wired into a live route or operator flow.

**Risk:**

The codebase describes two competing first-mile enrollment models:

- current production: worker enrolls through admin transport and gets a
  long-term worker token;
- unfinished v2 direction: issue one-time bootstrap tokens, exchange
  them, revoke/reissue/expire them, and audit that lifecycle.

Leaving both models half-present makes future security or first-mile
changes easy to implement against the wrong surface.

**Fix options:**

Option A - current flow is official:

- Delete or retire BootstrapToken aggregate, repo, service, exchange
  service, and related tests.
- Update ADR/design docs to say the current admin-token enroll path is
  the chosen implementation.
- Remove stale comments that imply the exchange path is live.

Option B - one-time BootstrapToken flow is required:

- Add live route(s) for issue/reissue/revoke/scan.
- Wire `Exchange` into the actual worker first-mile path.
- Add CLI/install flow for operators.
- Add token status audit and explicit expiry behavior.
- Keep backward compatibility deliberately documented.

**Recommendation:**

Choose Option A unless there is an active product requirement to restore
ADR-0023's one-time token model. The current production behavior has
been deployed repeatedly, while the v2 token code has no live operator
surface.

### 3. Agent Adapter Event Parsing and UnknownEventReporter Are Dead Wiring

**Verdict:** confirmed.

**Evidence:**

- `internal/agentadapter/adapter.go` still includes `ParseEvent`.
- `internal/agentadapter/unknown_event_reporter.go` provides
  deduplicated unknown-event reporting and parse-failure thresholds.
- Claude and Codex adapter comments still say callers should count parse
  failures through `UnknownEventReporter`.
- Live runtime parsing currently happens elsewhere:
  - Claude supervisor path: `claudestream.ParseStreamLine`
  - Codex supervisor path: `mapCodexLine`
  - Executor path: `executor/command_events.go`, `executor/usage.go`,
    and related runner parsers.
- `ParseEvent` and `UnknownEventReporter` are otherwise test-bound or
  stub-bound.

**Risk:**

This makes parser ownership unclear. A maintainer may add warning or
metrics behavior to `UnknownEventReporter` and believe live agent
runtime streams are covered, when they are not.

**Fix plan:**

- Narrow `agentadapter.Adapter` to its live responsibilities:
  command construction, feature/probe metadata, and runtime capability
  information.
- Delete `ParseEvent`, `AgentTraceEvent`, and `UnknownEventReporter`
  if no live parser uses them.
- If unknown event metrics are still desired, wire them into the live
  parser paths instead of the old adapter interface.

**Validation:**

- `go test ./internal/agentadapter ./internal/agentruntime ./internal/workerdaemon -count=1`
- targeted runtime stream parser tests for Claude and Codex.

### 4. Old Direct ClaudeSession and MCP Secret Injection Are Security-Sensitive Residue

**Verdict:** confirmed.

**Evidence:**

- `internal/agentruntime/claude_session.go` still exposes
  `StartClaudeSession`.
- `internal/agentruntime/mcp_injection.go` still walks JSON and replaces
  secret refs with plaintext values.
- `internal/workerdaemon/secret_resolver.go` comments still say it is
  the worker-side bridge used by `MCPInjector`.
- Live production startup goes through `StartSupervisorSession` via
  `internal/workerdaemon/agentruntime_bridge.go`.
- Live MCP config generation goes through `mcphost.GenerateMCPConfig`
  in `LocalRuntime`, not through the old `MCPInjector`.
- The old files are still covered by tests, which keeps them looking
  supported even though the live path has moved.

**Risk:**

This is not only stale code. It handles secrets and plaintext MCP
materialization. Keeping a dead secret-resolution path around makes it
harder to reason about the worker token scope and whether
`secret:resolve` is still needed.

**Fix plan:**

- Remove old direct `ClaudeSession` and tests if no rollback path
  depends on them.
- Remove old `MCPInjector`, `SecretRef` parser use in this path, and
  `workerdaemon/secret_resolver.go` after confirming no live caller
  remains.
- Keep user-secret CRUD and canonical MCP generation. The issue is the
  old injection mechanism, not secret storage itself.
- Audit worker token scope. If no current live caller needs
  `secret:resolve`, remove that scope from default worker token
  provisioning in the same or a follow-up PR.

**Validation:**

- `rg 'StartClaudeSession|NewMCPInjector|secret:resolve' internal`
- `go test ./internal/agentruntime ./internal/workerdaemon ./internal/mcphost -count=1`
- production smoke after deploy: supervisor boot, Codex boot, MCP config
  generation.

### 5. `peek-trace` Is Retired But Still Present in Code, Config, Help, and Docs

**Verdict:** confirmed.

**Evidence:**

- `internal/cli/build.go` comments say observability CLI commands,
  including `peek-trace`, were retired.
- `internal/cli/help_topics.go` still lists `peek-trace`.
- `internal/cli/build_test.go` still expects `peek-trace` in
  observability help grouping.
- `internal/observability/peek/*` still contains the client/server
  implementation and tests.
- `internal/config/config.go` still includes `PeekConfig`.
- Several design docs still describe `peek-trace` as an active or
  intended inspection path.

**Risk:**

Retired behavior remains discoverable in help text and config. This is
especially confusing because trace inspection has moved toward Web
Console and runtime logs.

**Fix plan:**

- Remove help topic and CLI references.
- Remove `internal/observability/peek` implementation if no hidden
  caller remains.
- Keep `config.Peek` for one compatibility window only if strict config
  loading would reject old config files. Mark it deprecated and unused.
- Update ADR/design docs to say `peek-trace` is retired and identify
  the current trace-inspection surface.

**Validation:**

- `go test ./internal/cli ./internal/observability/... ./internal/config -count=1`
- run config load tests with old config containing `peek`.

### 6. AI Runtime RuntimeResolver and LegacyAdapter Are Unused in Production

**Verdict:** confirmed.

**Evidence:**

- `internal/airuntime/resolver.go` defines `RuntimeResolver`.
- `internal/airuntime/legacy.go` defines `LegacyAdapter`.
- Current `rg` evidence shows these types are used by tests and
  repository tests, not by production wiring.
- Current production runtime selection is implemented through catalog
  service and runtime-contract/model-router paths.

**Risk:**

The codebase carries two conceptual models for runtime selection:

- snapshot resolver/legacy adapter;
- current catalog + runtime contract + model router.

This increases the cost of changing model/CLI selection and makes it
unclear where migration logic belongs.

**Fix options:**

Option A - delete unused resolver path:

- Remove `RuntimeResolver`, `LegacyAdapter`, and tests that only protect
  dead behavior.
- Update docs to describe the current runtime-contract path.

Option B - adopt resolver as the official selection boundary:

- Wire resolver into agent/task/executor creation transactions.
- Make snapshot creation explicit and test it end to end.

**Recommendation:**

Use Option A unless a snapshot boundary is explicitly planned. Current
main has already standardized around the catalog/runtime contract path.

### 7. Team Legacy Workflow Template Migration Helper Has No Runner

**Verdict:** confirmed.

**Evidence:**

- `internal/team/migration/legacy_workflow_templates.go` implements:
  - plan migration,
  - apply migration,
  - rollback notes,
  - rule generation.
- The code is only referenced by its tests.
- There is no CLI, admin route, task, or migration runner that invokes
  it.

**Risk:**

Migration code without an execution surface tends to become impossible
to trust. It also creates the impression that legacy template migration
is available when an operator cannot actually run it.

**Fix options:**

If migration is complete:

- Delete helper and tests.
- Update team/memory/template docs to say migration is closed.

If migration is still needed:

- Add explicit admin migration command with dry-run/apply/report.
- Keep rollback instructions tied to actual output artifacts.
- Document preconditions and production runbook.

**Recommendation:**

Confirm whether any legacy templates remain. If none, delete the helper.

### 8. SPA API Hooks Contain Unused or Over-Broad Surface

**Verdict:** confirmed, with one correction from the first pass.

**Evidence:**

Confirmed currently unused or suspect exports include:

- `web/src/api/modelCatalog.ts`
  - `useModelCatalog`
  - `useImportModelCatalog`
- `web/src/api/tasks.ts`
  - `useUnblockTask`
  - `useCompleteTask`
  - `useDiscardTask`
  - `useReopenTask`
- `web/src/api/plans.ts`
  - `useStopPlan`
  - `useCompletePlan`
  - `useAdvancePlan`
- `web/src/api/teams.ts`
  - `useTemplateInstances`
  - `useTemplateScrub`
  - `useExtractScrub`
  - `useImportTemplate`
- `web/src/api/conversations.ts`
  - `useConversationRefs`

Correction:

- `useRAMRoleNewVersion` is now used in `web/src/pages/Access.tsx`, so
  it should not be deleted as unused.

**Risk:**

Unused hooks make frontend API contracts look broader than what the UI
actually supports. This increases the chance of stale endpoint shapes
and untested mutations.

**Fix plan:**

- Split into functional groups.
- Delete hooks with no near-term UI plan.
- For planned features, connect the hook to a real page flow and add
  tests.
- Add a periodic `ts-prune` or equivalent report, but do not enable a
  hard gate until generated exports and intentional API surfaces are
  allowlisted.

**Validation:**

- focused page tests for any UI adoption.
- `pnpm --prefix web exec tsc -b`
- `pnpm --prefix web exec eslint src --max-warnings=0`

### 9. `@headlessui/react` Is Unused

**Verdict:** confirmed.

**Evidence:**

- `web/package.json` declares `@headlessui/react`.
- Search finds no real import in `web/src`.
- `web/src/components/ChannelCreateModal.tsx` only has a comment saying
  the modal is "not headlessui yet".

**Risk:**

Small but easy cleanup: unused dependency increases install surface and
keeps a false design-system direction around.

**Fix plan:**

- Run `pnpm --prefix web remove @headlessui/react`.
- Delete the stale "not headlessui yet" comment if it implies a plan no
  longer being followed.

**Validation:**

- `pnpm --prefix web install --frozen-lockfile`
- `pnpm --prefix web exec tsc -b`
- `make build-frontend`

### 10. `pmUnblockTaskHandler` Needs Strict `input_request_message_id` Validation

**Verdict:** confirmed.

**Evidence:**

- `internal/webconsole/api/handlers_pm.go` documents
  `input_request_message_id`.
- The same handler still carries a TODO to validate that the message id:
  - belongs to the task conversation;
  - refers to an unanswered `input_request`;
  - is safe to pair with the unblock reply.
- Existing tests cover the happy path but not rejection of wrong
  conversation/task/message-kind/already-replied cases.

**Risk:**

This is a data-integrity gap. The most likely failure mode is not
cross-task PM state mutation, but wrong or misleading `input_reply`
attachment to the conversation graph. That can make later agent
context, audit history, and UI replay inaccurate.

**Fix plan:**

Before unblocking, validate the referenced message:

1. It exists.
2. It belongs to the task's bound conversation.
3. Its content kind is `input_request`.
4. It is associated with the same task or the task's active input wait.
5. It has no existing `input_reply` child.

Return:

- `400` for malformed or irrelevant message id.
- `409` for already answered / stale input request.

**Validation:**

- Add handler tests for wrong conversation, wrong kind, already replied,
  missing message, and happy path.
- Add service-level test if the invariant belongs below API.
- `go test ./internal/webconsole/api ./internal/projectmanager/service -count=1`

### 11. Codex Executor Usage Is Implemented; Stale Comments Remain

**Verdict:** original finding corrected.

**Evidence:**

- Codex executor now runs `codex exec --json`.
- `ParseCodexRunnerStream` extracts result text, thread id, and token
  usage.
- `executor/run.go` routes Codex stdout through that parser.
- `orchestrator/writeback.go` reports usage through `UsageReporter`.
- Tests exist for usage reporting, including
  `writeback_test.go` and executor usage parsing tests.
- However, `internal/agentruntime/orchestrator/runner.go` still has an
  old TODO saying Codex usage is not wired because plain-text mode has
  no parseable usage.

**Risk:**

The code is correct, but stale comments can send future maintainers
toward reimplementing an already completed feature or misdiagnosing
usage attribution.

**Fix plan:**

- Delete or rewrite the stale TODO/comment block in `runner.go`.
- Check related tests for outdated references to the old plain-text
  behavior.
- Optionally add one narrow assertion that Codex writeback with parsed
  usage reaches `ReportUsage`, if current tests do not already pin that
  exact path.

**Validation:**

- `go test ./internal/agentruntime/orchestrator ./internal/agentruntime/executor -count=1`

### 12. Ring Buffers Should Stay Separate; Old Stubs Should Be Removed

**Verdict:** split verdict.

The original "duplicated ring buffer" observation is only partially
valid and should not drive a shared abstraction right now.

**Evidence:**

- `internal/webconsole/sse/ring.go` is a synthetic monotonic id replay
  buffer for Web Console SSE.
- `internal/environment/controlstream/ring.go` is an offset-driven
  replay buffer for worker control commands, including offset dedupe and
  reconnect semantics.
- These are similar shapes but different contracts.

**Recommendation for rings:**

- Keep the rings separate.
- Add comments that explain the contract difference, so a future
  cleanup does not force them into an unsafe generic helper.

**Confirmed removable stubs:**

- `internal/webconsole/api/server.go` has `notImplementedHandler` with
  no apparent caller.
- `internal/cli/build.go` has `buildClientOnly`, a post-migration
  helper marked for future shape and not used.

**Fix plan:**

- Delete unused stubs.
- Leave ring implementations separate.
- Consider adding small contract comments/tests if future drift becomes
  a problem.

**Validation:**

- `go test ./internal/webconsole/api ./internal/webconsole/sse ./internal/environment/controlstream ./internal/cli -count=1`

## Repair Backlog

### P0 - No-Product-Decision Cleanup

Expected size: small PR.

Changes:

- Fix `web/package.json` `typecheck`.
- Remove `@headlessui/react` and stale modal comment.
- Remove stale Codex usage TODO/comment.
- Remove `notImplementedHandler` and `buildClientOnly` if compile proves
  no caller.

Validation:

- `pnpm --prefix web run typecheck`
- `pnpm --prefix web install --frozen-lockfile`
- `make build-frontend`
- `go test ./internal/agentruntime/orchestrator ./internal/agentruntime/executor ./internal/webconsole/api ./internal/cli -count=1`
- `make lint`

### P1 - Input Reply Integrity

Expected size: focused backend PR.

Changes:

- Add strict validation for `input_request_message_id` in
  `pmUnblockTaskHandler` or lower service layer.
- Add rejection tests for stale, missing, wrong-kind, wrong-conversation,
  and already-replied request messages.

Validation:

- `go test ./internal/webconsole/api ./internal/projectmanager/service -count=1`

### P1/P2 - Runtime and Secret-Resolution Residue

Expected size: medium PR, possibly split.

Changes:

- Remove dead adapter parse interface and unknown event reporter.
- Remove old direct `ClaudeSession`.
- Remove old `MCPInjector` and `workerdaemon/secret_resolver`.
- Audit and possibly remove `secret:resolve` from worker token default
  scope.

Validation:

- `rg 'StartClaudeSession|NewMCPInjector|UnknownEventReporter|ParseEvent|secret:resolve' internal`
- `go test ./internal/agentadapter ./internal/agentruntime ./internal/workerdaemon ./internal/mcphost -count=1`
- production smoke for supervisor boot, Codex boot, MCP config.

### P2 - Product Direction Decisions

Expected size: one decision note plus follow-up PRs.

Decisions needed:

- Keep current worker enroll, or complete BootstrapToken exchange.
- Retire `peek-trace` fully, or reintroduce it as supported.
- Delete AI Runtime resolver/legacy adapter, or wire resolver as the
  official runtime-selection boundary.

Recommended default:

- Retire BootstrapToken v2 code unless specifically required.
- Retire `peek-trace`.
- Delete AI Runtime resolver/legacy adapter and document current
  runtime-contract path.

### P2/P3 - Frontend and Migration Surface Reduction

Expected size: multiple small PRs.

Changes:

- Delete unused API hooks with no UI plan.
- Wire planned hooks to actual pages and tests.
- Delete team legacy workflow-template migration helper if migration is
  complete, or add an explicit operator command if still required.
- Add periodic unused-export reporting with allowlist.

Validation:

- focused page tests for changed UI areas.
- `pnpm --prefix web exec tsc -b`
- `make build-frontend`
- relevant backend tests if team migration changes.

## Suggested Tracking Tasks

1. `P0 cleanup: typecheck, Headless UI, Codex comments, stubs`
2. `P1 unblock input_request_message_id validation`
3. `P1 runtime dead parser/direct-session cleanup`
4. `P2 decide worker enroll bootstrap token direction`
5. `P2 retire or re-own peek-trace`
6. `P2 delete or wire AI Runtime resolver`
7. `P3 SPA unused hooks cleanup`
8. `P3 team legacy workflow-template migration closure`

## Commands Used For This Review

Representative commands:

```bash
rg -n '"typecheck"|lint-spa-tsc|tsc -b --force' web/package.json Makefile
rg -n 'NewWorkerEnrollService|NewWorkerEnrollServiceV2|BootstrapToken|EnrollWithExchange|worker/enroll|Exchange' internal/cli internal/workforce internal/workerdaemon
rg -n 'UnknownEventReporter|ParseEvent|AgentTraceEvent|claudestream.ParseStreamLine|mapCodexLine' internal/agentadapter internal/agentruntime internal/workerdaemon
rg -n 'StartClaudeSession|MCPInjector|SecretRef|secret:resolve|GenerateMCPConfig|StartSupervisorSession' internal/agentruntime internal/workerdaemon internal/mcphost
rg -n 'peek-trace|PeekConfig|Peek' internal docs
rg -n 'RuntimeResolver|LegacyAdapter|runtime_contract|modelrouter|NewCatalogService' internal/airuntime internal/cli internal/webconsole internal/team
rg -n 'legacy_workflow_templates|LegacyWorkflow|workflow template|PlanLegacy|Rollback' internal/team docs
rg -n '@headlessui|headlessui|not headlessui' web/package.json web/src web/pnpm-lock.yaml
rg -n 'use(ModelCatalog|UnblockTask|CompleteTask|DiscardTask|ReopenTask|StopPlan|CompletePlan|AdvancePlan|Template|Extract|Import|RAMRoleNewVersion|ConversationRefs)|pmUnblockTaskHandler|input_request_message_id' web/src internal/webconsole/api internal/projectmanager
rg -n 'codex exec|ParseCodexRunnerStream|report_usage|usage_event|T622|plain-text|Usage' internal/agentruntime internal/workerdaemon internal/projectmanager
rg -n 'notImplementedHandler|buildClientOnly|Ring|replay ring|controlstream' internal/webconsole internal/environment internal/cli
```
