# T1456 RAM Roles Standalone Page Evidence

Candidate: local execution branch from `origin/main` `ddba9b10`.

## Implementation

- Added standalone route `/organizations/:slug/access/ram-roles` with `page-RAMRoles`.
- Bare `/organizations/:slug/access` redirects to `access/ram-roles`.
- Access secondary nav now links to:
  - RAM Roles: `/access/ram-roles`
  - Team Role mappings: `/teams/roles`
  - Subject access: `/access/subject-access`
- RAM Roles page includes stats, search, kind/risk/scope filters, page-size pagination, detail/version history, permission summary, create/edit/duplicate full fields, Team Role references, referenced delete blocking, migration, unreferenced delete confirmation, and toast notices.

## Verification

- `pnpm --dir web typecheck` PASS.
- `pnpm --dir web exec vitest run src/pages/RAMRoles.test.tsx` PASS, 3 tests.
- `pnpm --dir web exec vitest run src/App.test.tsx` PASS, 24 tests.
- `pnpm --dir web exec vitest run src/pages/Access.test.tsx` PASS, 14 tests.
- `git diff --check` PASS.
- A broad `pnpm --dir web test -- RAMRoles.test.tsx App.test.tsx` run executed the full suite because the filename filter was not honored by that invocation; it failed only on the stale in-flight RAM Roles test before the final correction. The corrected targeted tests above passed.

## Screenshot Evidence

All fresh screenshots are `1672x941` and were captured from the Vite dev server using `docs/acceptance/t1456/capture-t1456.mjs`.

- `01-list-stats-1672x941.png`
- `02-search-filter-1672x941.png`
- `03-pagination-1672x941.png`
- `04-detail-permission-summary-team-references-1672x941.png`
- `05-delete-confirm-referenced-blocking-1672x941.png`
- `06-migration-toast-1672x941.png`
- `07-create-full-fields-1672x941.png`
- `08-create-toast-1672x941.png`
- `09-edit-full-fields-1672x941.png`

The required canonical mockup `ac://files/01M0HRMZDX20FF5KQT4SBANGC1` with SHA256 `5e085034e927054a59c103aeac30b6217c6a8a1c5f44f20ad9212589381cf43e` was not accessible from this isolated executor workspace. I did not use the current implementation as a baseline. The `*-overlay-unavailable.png` and `*-diff-unavailable.png` files are explicit marker images, not visual comparisons.
