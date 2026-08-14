# T1344 Independent Security And Consistency Acceptance

Date: 2026-08-14

Verdict: REJECT

Candidate evaluated:

- Candidate branch: `delivery/task-fcf46142-human-agent-permissions`
- Candidate SHA: `aab7b8d3ad46c1d994a00a7be19ec313800e8ab7`
- Required baseline SHA: `1ff3a401e393a998762f892140305e7dc895555c`
- Known rejected SHA from shared finding: `fa4cabf57b9cc6af4f42bf821404bc3fe8b01969`

Isolation note: this executor did not use center tools, center DB files, admin sockets, worker tokens, or raw HTTP fallback. Git remote checks and local blackbox/whitebox tests were used.

## Hard Precondition Evidence

Commands run:

```sh
git fetch --prune origin
git ls-remote --heads origin
git merge-base --is-ancestor 1ff3a401 delivery/task-fcf46142-human-agent-permissions; printf 'delivery_ancestry=%s\n' $?
git merge-base --is-ancestor 1ff3a401 main; printf 'main_ancestry=%s\n' $?
git merge-base --is-ancestor 1ff3a401 fa4cabf; printf '%s\n' $?
```

Observed:

- `delivery/task-fcf46142-human-agent-permissions` resolves to `aab7b8d3ad46c1d994a00a7be19ec313800e8ab7`.
- `delivery_ancestry=0`: the delivery candidate contains the required T1340 baseline.
- `fa4cabf` ancestry check returned `1`: the shared finding remains valid for the older candidate.
- `main` resolves to `af795b013ac71eef2f30b0116dc7b6045db3899d`.
- `main_ancestry=1`: current `main` does not contain required baseline `1ff3a401`.

Because the reviewed team rule requires final completion only after merge to `origin/main`, the current remote main state is not acceptable for a final PASS even though the delivery branch candidate itself contains the baseline.

## Blocking Findings

### BLOCKER-1: Legacy `/access` Apply Reports Success But Does Not Mutate Unified Authorization State

New reproducible test:

- `internal/webconsole/api/t1344_access_consistency_test.go`
- Test: `TestT1344LegacyAccessApplyMustMatchPermissionsEffectiveState`

Command:

```sh
go test ./internal/webconsole/api -run TestT1344 -count=1 -v
```

Observed failure:

```text
legacy /access apply reported success but /permissions effective did not include org.settings.manage
```

Whitebox evidence:

- `internal/webconsole/api/server.go:319` says lower-level `/permissions/*` is the unified authorization service contract, while `server.go:321-324` still exposes `/access/*`.
- `internal/webconsole/api/handlers_access.go:204-226` implements `/access/batch/apply` by computing DTO items and returning `200 OK`; it does not call `AuthorizationService.ApplyBatch`.
- `internal/webconsole/api/handlers_access.go:713-760` sets successful items to `allowed`, with text claiming the grant was accepted by the permission API, but this is only projection logic.
- `internal/webconsole/api/handlers_permissions.go:182-216` shows the new `/permissions/batch/*` path correctly calls `PreviewBatch`, `ApplyBatch`, or `RevokeBatch`.
- `internal/authorization/service.go:249-290` is the actual transactional/idempotent apply/revoke entry.

Impact:

The old Web entry can tell the operator that a grant succeeded while the authoritative `/permissions/effective` state remains unchanged and no authorization assignment/audit is produced. This violates old/new entry consistency, Web/internal consistency, idempotency expectations, and auditability.

### BLOCKER-2: Legacy `/access/overview` Omits Custom Role Permissions

New reproducible test:

- `internal/webconsole/api/t1344_access_consistency_test.go`
- Test: `TestT1344LegacyAccessOverviewMustIncludeCustomRolePermissions`

Command:

```sh
go test ./internal/webconsole/api -run TestT1344 -count=1 -v
```

Observed failure:

```text
legacy /access overview omitted custom_role permission that /permissions effective exposes
```

Whitebox evidence:

- `/permissions/effective` returns the custom role grant from the unified service.
- `/access/overview` is built by `accessDerivedState` from legacy member/project/team projections and does not consume `authorization_role_assignments`.
- The failing test creates a custom role assignment through `AuthorizationService.ApplyBatch`; `/permissions/effective` sees `org.settings.manage`, but `/access/overview` only returns legacy org rows for the same subject.

Impact:

Custom Role impact is not consistently visible across old and new Web entrypoints. Operators can inspect one screen and miss live custom role permissions that another screen and the internal authorizer enforce.

### BLOCKER-3: Final Main State Still Fails Required Ancestry

Remote `main` currently points to `af795b013ac71eef2f30b0116dc7b6045db3899d`, and `git merge-base --is-ancestor 1ff3a401 main` returns `1`.

Impact:

Even if `delivery/task-fcf46142-human-agent-permissions` is the intended candidate, final acceptance cannot be marked PASS under the reviewed rule requiring completion from `origin/main`.

## Passing Evidence

Service-level T1344 contract test added:

- `internal/authorization/t1344_acceptance_test.go`
- Test: `TestT1344ServiceContractMixedBatchSourcesExpiryReclaimAndAudit`

Command:

```sh
go test ./internal/authorization -run TestT1344 -count=1 -v
```

Observed:

```text
=== RUN   TestT1344ServiceContractMixedBatchSourcesExpiryReclaimAndAudit
--- PASS: TestT1344ServiceContractMixedBatchSourcesExpiryReclaimAndAudit
PASS
```

This test covers:

- Human/Agent mixed batch.
- Idempotency replay.
- Source inheritance and evidence refs.
- Team membership removal reclaiming derived team grants without affecting an unrelated custom grant.
- Expired assignment denial.
- Last org owner revoke denial.
- Agent high-risk assignment denial.
- Non-owner self-escalation denial.
- Custom Role permission edit changing effective authorization.
- Web/MCP/internal transport consistency at the service boundary.
- Audit ledger count after batch operations.

Existing related backend tests:

```sh
go test ./internal/authorization -count=1
go test ./internal/webconsole/api -run 'TestPermissionsHTTP|TestAccessEffectiveBatchAndRevokeContract' -count=1 -v
go test ./internal/admin/api -run TestRequireScope_DelegatesToAuthorizationService -count=1 -v
```

Observed:

- `internal/authorization`: PASS.
- Existing `/permissions` HTTP tests: PASS.
- Existing legacy `/access` contract test: PASS, but it does not check authoritative authorization state.
- Admin HTTP scope gate delegates to `AuthorizationService.Check`: PASS.

Frontend checks:

```sh
cd web
pnpm install --frozen-lockfile
pnpm exec vitest run src/components/AccessPermissionsPanel.test.tsx src/pages/Access.test.tsx
pnpm exec tsc --noEmit
```

Observed:

- Relevant Vitest files: 2 passed, 5 tests passed.
- TypeScript: PASS.

## Additional MCP/Internal Consistency Evidence

Source search:

```sh
rg -n "Name:\s*\".*(permission|access|role|grant|revoke)|permissions/batch|AuthorizationService|Authorizer|ApplyBatch|RevokeBatch" internal/mcphost internal/admin/api -g '*.go'
```

Observed:

- Admin HTTP scope enforcement calls `Authorizer.Check`.
- No agent-facing MCP permission batch/effective tool equivalent to `/permissions/batch/*` was found under `internal/mcphost`.

This reinforces that the unified authorization management surface is currently Web/internal only; MCP parity for permission management is not demonstrated by the candidate.

## Repro Summary

Run from a worktree at `aab7b8d3ad46c1d994a00a7be19ec313800e8ab7` with the two T1344 test files added:

```sh
go test ./internal/authorization -run TestT1344 -count=1 -v
go test ./internal/webconsole/api -run TestT1344 -count=1 -v
```

Expected current result:

- Authorization service T1344 test passes.
- Web consistency T1344 test fails with the two blockers above.

Final structured verdict: REJECT. Do not mark PASS until `/access` either delegates to the unified `/permissions` service or is removed/redirected so old and new entrypoints cannot diverge, custom role grants are visible consistently, audit/idempotency are preserved, and the accepted candidate is merged to `origin/main`.
