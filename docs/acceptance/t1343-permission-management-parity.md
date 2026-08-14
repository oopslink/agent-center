# T1343 Permission Management Parity Evidence

Date: 2026-08-14

Required baseline:

- `1ff3a401e393a998762f892140305e7dc895555c`
- Remote baseline ref: `origin/ac-exec/task-917b6950/exec-a14fed2c`

Review ref:

- `review/t1343-permission-management-parity`

Scope:

- Replayed Access management UI/API and detail permissions tabs onto the formal T1340 lineage.
- Kept `/permissions/*` as the unified authorization contract.
- Kept `/access/*` as the Web workspace/compatibility surface, backed by the same authorization service handlers.
- Covered Web/internal parity and MCP transport parity at the authorization service boundary.

Reproducible checks:

```sh
go test ./internal/authorization -run TestT1343 -count=1 -v
go test ./internal/webconsole/api -run TestT1343 -count=1 -v
go test ./internal/authorization ./internal/webconsole/api ./internal/admin/api ./internal/mcphost -count=1
cd web && pnpm exec vitest run src/pages/Access.test.tsx src/components/AccessPermissionsPanel.test.tsx src/pages/AgentDetail.test.tsx src/pages/UserDetail.test.tsx
git merge-base --is-ancestor 1ff3a401e393a998762f892140305e7dc895555c HEAD
```

Expected ancestry result:

- `git merge-base --is-ancestor 1ff3a401e393a998762f892140305e7dc895555c HEAD` exits `0`.

Regression coverage:

- `internal/authorization/t1343_permission_parity_test.go`
  - mixed human/agent batch assignment
  - idempotent apply replay
  - custom-role source inheritance
  - Web/MCP/System transport consistency for the same effective permission
  - high-risk agent grant denial
  - non-owner self-escalation denial
  - expired grant denial
  - last org owner revoke denial
  - audit ledger write coverage
- `internal/webconsole/api/t1343_access_consistency_test.go`
  - `/access/batch/apply` success is visible through `/permissions/effective`
  - `/access/overview` includes custom-role permissions visible through `/permissions/effective`
