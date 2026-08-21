# T1461 S3 fresh remediation acceptance

Date: 2026-08-21 (Asia/Shanghai)

## Verdict

The four reported product defects are remediated and verified on the real install path. Code/runtime acceptance is **PASS** for candidate `bb29dbd55e08174830bb6742f08fff6741627c63`.

Formal approved-mockup comparison remains **BLOCKED BY MISSING INPUT**. The three approved mockup originals named by the task were not present in the task payload available to this isolated executor, the plan files, the repository tree, or any reachable Git history. This is not a code defect and must not be represented as a visual-comparison PASS.

## Baseline and runtime identity

- Authoritative baseline: `origin/main` = `00adf6b7b3c6871e3dff118d4af2b85a2c23a1c6`.
- Delivery branch: `delivery/t1461-s3-fresh-remediation-v2`.
- Tested code candidate: `bb29dbd55e08174830bb6742f08fff6741627c63`.
- Build command: `make build`.
- Fresh production-chain install: `bin/agent-center install test-instance --id s3bb29 --with-seed --workers 1 --output json`.
- Runtime authority readback: `GET /api/system/version` returned branch `delivery/t1461-s3-fresh-remediation-v2`, commit `bb29dbd5`, and installed binary path `/Users/oopslink/.agent-center-test/s3bb29/center/current/bin/agent-center`.

## Blocking defects

| Defect | Implementation | Authoritative terminal state |
|---|---|---|
| Deleted RAM Role leaves stale selection/detail/history/edit and later GET 404 | The UI enters an explicit null selection before DELETE resolves, clears edit state, disables the detail query, and restores selection only on delete failure. The deleted detail query is no longer invalidated. | Browser shows `Deleted RAM Role S3 cleanup role.` and `No RAM Role selected`; no edit controls remain and browser error log is empty. Unit test asserts no additional detail request after DELETE. |
| RAM Role filter with zero matches leaves stale detail | Selection is constrained to the filtered collection; zero matches render a dedicated list empty state and a cleared detail empty state. | Browser shows `No matching RAM Roles` plus `No RAM Role selected / No role matches the current search`; old detail/history/edit is absent. |
| Team Roles with no teams lacks guidance | Replaced `No teams.` with a structured empty state describing the prerequisite and a `Create a team` navigation entry. | Fresh seeded org with zero teams renders `No teams or Team Roles yet` and the creation link. |
| Direct binding accepts project+team mixed resources until apply partial failure | Direct mode is explicitly one RAM Role + one resource in the UI. The server rejects any direct-binding request whose resource count is not exactly one before role evaluation or `PreviewBatch`/`ApplyBatch`. | On the fresh installed instance both `/access/batch/preview` and `/access/batch/apply` returned HTTP 422 `mixed_direct_binding_scope`; no operation was applied. Go integration test asserts assignment count is unchanged. |

## Browser evidence

All captures use a real Chromium session against the fresh installed instance, not MSW or a dev server.

- `screenshots/00-signed-in.png` — seeded owner signed into the installed instance.
- `screenshots/01-roles-team-empty.png` — RAM Role surface and Team Role no-team empty state.
- `screenshots/01b-team-roles-empty-focused.png` — focused no-team explanation and creation entry.
- `screenshots/02-ram-filter-no-results.png` — zero-match list and cleared detail state.
- `screenshots/02b-ram-filter-no-results-focused.png` — focused zero-match and cleared-detail evidence.
- `screenshots/03-ram-role-created-detail.png` — custom RAM Role created through the real SPA/API.
- `screenshots/04-delete-second-confirmation.png` — typed-name plus second confirmation.
- `screenshots/05-delete-cleared-detail.png` — post-delete authoritative UI state with stale detail/edit removed.
- `screenshots/06-direct-binding-single-scope.png` — direct-binding drawer with RAM Role selection and explicit single-scope contract.
- `ram-role-delete.webm` — recorded confirmation-to-cleared-terminal-state interaction.

## Written-contract comparison

Because the approved mockup originals are missing, only the committed written contract can be compared:

| Contract surface | Structural/behavior result |
|---|---|
| Roles & mappings | RAM Role catalog, create form, selected detail, immutable version history, edit, typed delete, Team Role mapping section, and permission catalog are distinct visible regions. PASS. |
| RAM Role search | Search changes both the result collection and detail availability; empty state includes recovery guidance. PASS. |
| Team Role empty state | Explains why the section is empty and provides a next action. PASS. |
| Subject access / direct binding | Separate top-level view; direct drawer selects a RAM Role rather than manufacturing permissions and communicates the one-resource invariant. PASS. |
| Pixel/spacing/text comparison to three approved originals | BLOCKED: originals unavailable. |

## Regression results

- `go test ./...` — PASS.
- `go test ./internal/webconsole/api -run 'TestAccessEffectiveBatchAndRevokeContract|TestAccessRAMRoleV4EditDeleteAndReferenceBlocking|TestRAMRoleWriteCatalogMatchesAuthoritativePermissionRegistry' -count=1` — PASS.
- `pnpm exec vitest run --reporter=dot --maxWorkers=1 --minWorkers=1 --no-file-parallelism` — PASS, 191 files / 1791 tests. The first parallel attempt was interrupted by host process quota (`spawn ... EAGAIN`); the same complete set passed serially.
- `pnpm exec tsc --noEmit` — PASS.
- `pnpm exec playwright test tests/access-ram-role-crud.spec.ts --project=chromium-mac --workers=1 --reporter=list` — PASS, 2/2 against freshly started real binaries/databases.
- `make build` — PASS; built binary self-reported commit `bb29dbd5`.

## Missing approved mockups

Search evidence:

- `docs/plans/t1460-access-team-subject-acceptance-plan.md` says T1461 owns the mockup comparison but provides no mockup locator.
- `docs/design/features/2026-08-20-t1435-dual-profile-ram-access-contract.md` names expected acceptance screenshots, not approved mockup originals.
- No T1461 plan/attachment or Access/RAM Role/Team Role approved mockup file exists in the repository tree.
- Git history filename and content searches found older unrelated product mockups and acceptance screenshots only.

Required unblock: provide the three immutable approved originals (or durable repository paths plus hashes). A reviewer can then perform the remaining image-to-image structure/spacing/text comparison without changing the code candidate.
