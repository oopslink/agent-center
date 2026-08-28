# S5 canonical Subject access acceptance report

## Baseline and candidate

- Code baseline at task start: `7232c04df2bc902353107404d18b4bc3f5bd5712` (`origin-push/main`, 2026-08-24 workspace state).
- Canonical IA/design ancestry integrated before S5: `85efb6fe`, `244f6c03`, `03c61e19`, `50e901bc` (rebased equivalents of review/S4 commits).
- Canonical reference: `docs/design/assets/adr-0059/subject-access-canonical-1672x941.png`, SHA-256 `2fd6db8b099b19371157fabefda282fef664377db53c010a451f201ff156dc78`.
- Candidate branch: `review/s5-subject-access-canonical`.
- Candidate SHA: recorded after commit in the delivery message and verified from the remote ref.

## Contract delivered

- Canonical `/access/subject-access` workbench with Subject/name/email/ID, type, project, permission, risk and status filters.
- Subject list, dense subject detail and Trace + audit rail; responsive two-column flow at 1280 and three columns at 1672.
- Structured Why access entries expose Result, Resource and Source chain rather than a prose-only explanation.
- Decision trace presents membership -> Team Role -> RAM Role -> direct union -> explicit deny -> final outcome, including deny precedence, fail-closed and N/A states.
- Summary/activity and subject-scoped direct bindings are visible together. Add direct binding, Preview revoke and Revoke retain the selected subject context and use the production mutation/query-invalidation path.
- Canonical backend filters include email/subject identity, project and permission. Mock mutation semantics mirror direct-union grant/revoke and 409 behavior.
- Loading, empty, 403, 409, N/A, deny precedence, direct coexist, grant and revoke states are covered by tests and UI captures; success/error notifications are visible.

## Verification

| Command | Result |
| --- | --- |
| `go test ./...` | PASS |
| `pnpm --dir web test` | PASS — 192 files, 1801 tests |
| `pnpm --dir web exec vitest run src/pages/Access.test.tsx` | PASS — 19 tests |
| `pnpm --dir web exec tsc --noEmit` | PASS |
| `git diff --check` | PASS |
| `node docs/acceptance/t1458-subject-access/capture-subject-access.mjs` (with `pnpm --dir web dev --host 127.0.0.1`) | PASS — 15 captures |

The full web suite emits existing MSW unmatched-request and local-storage warnings; it exits 0 with no failed tests.

## UI evidence

| State | Capture |
| --- | --- |
| 1672 light, canonical three-column ready/deny precedence/direct coexist | `s5-01-ready-1672-light.png` |
| 1672 dark | `s5-02-ready-1672-dark.png` |
| 1280 dark responsive | `s5-03-ready-1280-dark.png` |
| 1280 light responsive | `s5-04-ready-1280-light.png` |
| Filtered agent dense detail and source trace | `s5-05-filter-agent-detail.png` |
| Subject-scoped Add direct binding | `s5-06-add-binding-context.png` |
| Grant preview | `s5-07-grant-preview.png` |
| Grant result and success toast | `s5-08-grant-success-toast.png` |
| Revoke preview | `s5-09-revoke-preview.png` |
| Revoke result and success toast | `s5-10-revoke-success-toast.png` |
| 403 fail-closed | `s5-11-forbidden-403.png` |
| 409 conflict toast | `s5-12-conflict-409-toast.png` |
| Not applicable | `s5-13-not-applicable.png` |
| Empty at 1280 | `s5-14-empty-1280.png` |
| Loading at 1672 | `s5-15-loading-1672.png` |

All captures are generated through real browser interactions and intercepted HTTP responses, not static page mocks. The capture script is committed beside the evidence.

## Known delivery gaps

- The promised `task-input/v1/README.md`, `manifest.json`, and attachments were not materialized anywhere under this workspace. The implementation therefore used the repository ADR-0058/ADR-0059, design assets and existing S2/S4 lineage as the authoritative local contract.
- This executor can push a review candidate but cannot merge it into `origin/main`; completion under the main-merge rule remains pending review and integration.
