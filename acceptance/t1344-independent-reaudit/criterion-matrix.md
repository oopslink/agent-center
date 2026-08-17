# T1344 Independent Security/Auth/Migration Reaudit Matrix

Reviewed SHA: `1467848d08a43395b2740d921836727d989aafff`

Delivery branch: `evidence/t1344-independent-security-auth-migration-reaudit-exec76b52883`

Date: 2026-08-18

| Criterion | Evidence | Result | Notes |
|---|---|---:|---|
| Explicit delivery branch, not detached HEAD | `raw/00-branch-head.txt` | PASS | Branch was created and HEAD verified at the reviewed SHA before validation. |
| Security and authorization service consistency | `raw/01-go-auth-web-admin-mcp.log` | PASS | Covered `internal/authorization`, `internal/webconsole/api`, `internal/admin/api`, and `internal/mcphost`; includes cross-org denial, fail-closed paths, bearer scope delegation, file/attachment authz, and MCP forwarding. |
| Unified authorization migration | `raw/02-go-migrations.log` | PASS | Includes `TestMigration_0129_UnifiedAuthorizationRollback` and migration round-trip coverage. |
| Candidate Access UI behavior tests | `raw/04-web-access-vitest.log` | PASS | `src/pages/Access.test.tsx`: 5 tests passed. |
| TypeScript build-mode check | `raw/05-web-tsc.log` | FAIL | `tsc -b --force` failed in `web/src/pages/Access.test.tsx:145` because `publishBody.permissions` is optional and cannot satisfy `string[]`; also reports `web/src/mocks/handlers.ts:743` unused `failed`. |
| Production build / real product startup prerequisite | `raw/06-make-build.log` | FAIL | `make build` fails during `pnpm run build` at `tsc -b`, so a production binary with embedded SPA cannot be built or started for real E2E. |
| Screenshots or error context | `context/access-test-ts-error-context.txt`, `context/mock-handler-ts-error-context.txt`, `context/reviewed-sha-access-test-commit.patch` | PASS | Build failure contexts persisted; screenshots are not applicable because product build failed before browser E2E could start. |
| Server output | `raw/06-make-build.log` | FAIL | No server was started because the production build failed. This is preserved as the server-start blocker. |

## Blocking Findings

1. `web/src/pages/Access.test.tsx:145` makes the reviewed SHA fail TypeScript build-mode validation. The handler stores `publishBody` as `{ permissions?: string[]; expected_latest_version?: number }`, then uses `publishBody.permissions` in a profile version object typed with required `permissions: string[]`.
2. `make build` fails before producing the product binary. This blocks real E2E execution against the production SPA/server artifact.

## Verdict

REJECT, blocking=true.
