# T1344 Independent Security And Consistency Retry Evidence

Date: 2026-08-14

Verdict: REJECT

Executor isolation: no center tools, center database files, admin sockets, worker tokens, `mcp_config.runtime.json`, process arguments, or raw HTTP center fallback were used. Evidence was collected from this git worktree, git remote refs, local code inspection, and local test runs only.

## Required Fields

- Reviewed SHA: `aab7b8d3ad46c1d994a00a7be19ec313800e8ab7`
- Reviewed SHAs:
  - T1341: `c5279376e415c6af422b9f25c5d0db2891526017`
  - T1342: `aab7b8d3ad46c1d994a00a7be19ec313800e8ab7`
  - T1343: not found in remote refs
- Expected base SHA: `1ff3a401e393a998762f892140305e7dc895555c`
- Evidence branch: `evidence/t1344-independent-security-consistency-retry-ffe5dfde`
- Artifact path: `docs/acceptance/t1344-independent-security-consistency-retry.json`
- Artifact digest SHA-256: recorded in the JSON artifact after report generation

## Git And Remote Probe

Commands:

```sh
git status --short --branch
git remote -v
git fetch --all --prune
git fetch git@github.com:oopslink/agent-center.git '+refs/heads/*:refs/remotes/origin/*' --prune
git for-each-ref --sort=-committerdate --format='%(refname:short) %(objectname) %(committerdate:iso8601) %(subject)' refs/remotes/origin | head -n 20
git ls-remote --refs git@github.com:oopslink/agent-center.git | rg -i 't1343|1343' || true
```

Observed:

- Initial worktree was real and clean at `/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/runtime/worktrees/exec-ffe5dfde`.
- The configured `remote.origin.fetch` is broad (`+refs/*:refs/*`), so `git fetch --all --prune` refused to update a local branch checked out by another worktree. This was recorded as a git protection failure, not bypassed by touching that worktree.
- A safe URL fetch into `refs/remotes/origin/*` succeeded.
- Remote refs identify T1341 and T1342 delivery heads, but no T1343/1343 head or tag was found.
- `origin/evidence/t1344-independent-security-consistency-exec1712faaa` exists at `a652403bc1e460727e003994b3146a33779f82b9`, but it is an evidence branch whose parent is T1342 and whose own verdict is REJECT; it is not a T1343 implementation delivery ref.

## Ancestry Evidence

Commands:

```sh
for s in c5279376e415c6af422b9f25c5d0db2891526017 aab7b8d3ad46c1d994a00a7be19ec313800e8ab7 a652403bc1e460727e003994b3146a33779f82b9 af795b013ac71eef2f30b0116dc7b6045db3899d; do
  git merge-base --is-ancestor 1ff3a401e393a998762f892140305e7dc895555c "$s" && echo "$s contains_base=yes" || echo "$s contains_base=no"
done
git merge-base c5279376e415c6af422b9f25c5d0db2891526017 aab7b8d3ad46c1d994a00a7be19ec313800e8ab7
git merge-base --is-ancestor c5279376e415c6af422b9f25c5d0db2891526017 aab7b8d3ad46c1d994a00a7be19ec313800e8ab7; printf 'c527_ancestor_of_aab=%s\n' $?
git merge-base --is-ancestor aab7b8d3ad46c1d994a00a7be19ec313800e8ab7 c5279376e415c6af422b9f25c5d0db2891526017; printf 'aab_ancestor_of_c527=%s\n' $?
```

Observed:

- T1341 contains the required base: yes.
- T1342 contains the required base: yes.
- Prior T1344 evidence branch contains the required base: yes.
- `origin/main` is `af795b013ac71eef2f30b0116dc7b6045db3899d` and does not contain the required base.
- T1341 and T1342 share merge-base `b6d2431b903856187660e00a1af0800b45b4117e`; neither is ancestor of the other.

## Matrix Results

Service-level command run on T1341 and T1342 with the evidence tests injected:

```sh
go test ./internal/authorization -run 'TestT1344|TestService_RevokeAssignmentConcurrentRaceIdempotent' -count=1 -v
```

Result: PASS on both T1341 and T1342.

Coverage:

- Human/Agent mixed batch.
- Idempotent replay.
- Source inheritance and evidence refs.
- Team membership removal reclaiming derived team grants without removing unrelated custom grants.
- Expired assignment denial.
- Last org owner revoke denial.
- Agent high-risk role assignment denial.
- Non-owner self-escalation denial.
- Custom Role permission updates affecting effective authorization.
- Web/MCP/System transport decision consistency at the authorization service boundary.
- Authorization audit events.
- Concurrent revoke race idempotency.

Web/API consistency command:

```sh
go test ./internal/webconsole/api -run TestT1344 -count=1 -v
```

T1342 result: FAIL.

- `TestT1344AccessApplyEntrypointMustMutateUnifiedStateOrBeRemoved`: `/access/batch/apply` reported success, but `/permissions/effective` did not include `org.settings.manage`.
- `TestT1344AccessOverviewMustIncludeCustomRolePermissionsOrBeRemoved`: `/access/overview` omitted a custom_role permission visible through `/permissions/effective`.

T1341 result: FAIL.

- `TestT1344AccessApplyEntrypointMustMutateUnifiedStateOrBeRemoved`: PASS. The old `/access/batch/apply` endpoint is removed and the new `/permissions/batch/apply` path mutates unified state.
- `TestT1344AccessOverviewMustIncludeCustomRolePermissionsOrBeRemoved`: FAIL. The `/permissions/effective?view=access` overview still omitted a custom_role permission visible through direct `/permissions/effective`.

Existing related checks:

```sh
go test ./internal/authorization -count=1
go test ./internal/webconsole/api -run 'TestPermissionsHTTP|TestAccessEffectiveBatchAndRevokeContract' -count=1 -v
go test ./internal/admin/api -run TestRequireScope_DelegatesToAuthorizationService -count=1 -v
```

Result: PASS on T1341 and T1342 where run.

Frontend checks:

```sh
cd web
pnpm install --frozen-lockfile
pnpm exec vitest run src/pages/Access.test.tsx
pnpm exec vitest run src/components/AccessPermissionsPanel.test.tsx src/pages/Access.test.tsx
pnpm exec tsc --noEmit
```

Result:

- T1341: Access page Vitest and TypeScript passed.
- T1342: AccessPermissionsPanel, Access page Vitest, and TypeScript passed.

MCP/internal consistency search:

```sh
rg -n 'Name:\s*".*(permission|access|role|grant|revoke)|permissions/batch|AuthorizationService|Authorizer|ApplyBatch|RevokeBatch|ListEffective|permissions' internal/mcphost internal/admin/api -g '*.go' || true
```

Observed:

- Admin HTTP scope checks delegate to `AuthorizationService.Check`.
- Worker agent operation checks delegate to `AuthorizationService.Check` with `TransportMCP`.
- No MCP tool equivalent to unified permission batch/effective management was found. The MCP `assign_roles` team tool is team roster assignment, not authorization role grant/revoke management.

## Blocking Findings

1. T1343 final implementation SHA could not be determined from remote refs. No `T1343` or `1343` branch/tag exists in `git ls-remote --refs`, and center access is explicitly out of bounds for this executor.
2. T1341 and T1342 are sibling candidates, not an integrated delivery. There is no verified single implementation SHA containing both T1341 and T1342.
3. T1342 legacy `/access` apply reports success without mutating unified authorization state.
4. T1341 and T1342 do not consistently surface custom_role permissions in the Access overview.
5. `origin/main` does not contain required base `1ff3a401e393a998762f892140305e7dc895555c`.
6. MCP management parity for permission batch/effective surfaces is not demonstrated.

Final verdict: REJECT. Do not mark PASS until a single remote-delivered candidate SHA is identified, contains the T1340 baseline by ancestry, integrates the required Web/API behavior, demonstrates custom role visibility across old/new or removed/replaced entrypoints, demonstrates MCP/internal parity or explicitly removes that requirement, and is merged into `origin/main`.
