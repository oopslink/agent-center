# T1457 Team Roles / RAM Role Mapping Evidence

- Gate recovery start: `origin/main` / `HEAD=ddba9b10816b803b0563e97de574ebe7378c8ef2`; this SHA is explicitly not reused as the final delivery SHA.
- Canonical attachment SHA256: `80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56`.
- Canonical baseline copied into this workspace as `canonical-t1457-sha80e51bb4.png`; the capture script refuses to run if its SHA differs.
- Remediation: `capture-t1457.mjs` now generates full per-state candidate screenshots, canonical overlays, pixel diffs, pixel stats, 1280 overflow evidence, CRUD/mapping/CAS/error browser checks, console/network capture, and running binary SHA validation.

## State Evidence

All candidate/overlay/diff state screenshots are captured at `1672 x 941`.

- `01-roles-list-detail-work-config-ram-mapping.png`
- `02-role-detail-mapping-drawer-work-config-cas.png`
- `03-mapping-preview-immediate-impact.png`
- `04-mapping-confirm-versioned-write.png`
- `05-mapping-cas-error.png`
- `06-ram-role-create-drawer-permissions-audit.png`
- `07-ram-role-created-crud-success.png`
- `08-ram-role-edit-drawer-version-controls.png`
- `09-ram-role-duplicate-drawer.png`
- `10-delete-safeguard-confirm-blocked.png`
- `11-delete-safeguard-notice.png`
- `12-ram-role-cas-error.png`

For each file above, matching `*-canonical-overlay.png`, `*-canonical-pixel-diff.png`, and `*-canonical-diff-stats.json` were generated against the SHA-verified canonical baseline.

## Runtime Checks

- Fresh local production instance launched from `bin/agent-center`.
- `/api/system/version` was checked against `git rev-parse HEAD`.
- Public auth signup and org-scoped public Web API endpoints were used for seed/readback.
- Real Chromium exercised Team IA route, role list/detail, work config, RAM mapping table, mapping preview/apply, expected mapping CAS 409, RAM Role create/edit/duplicate, delete safeguard, and expected RAM Role CAS 409.
- Console/network evidence is in `capture-state.json`; only the two deliberate 409 responses are recorded as bad responses.
- `fresh-1280-overflow-main.png` plus manifest metrics prove `clientWidth=1280` and `scrollWidth=1280`; 1672 metrics are also equal.

## Verification

- `pnpm --dir web install --frozen-lockfile`
- `pnpm --dir tests/e2e/v2 install --frozen-lockfile`
- `pnpm --dir web test -- App.test.tsx TeamUISecondaryNav.test.tsx Access.test.tsx TeamDetail.test.tsx`
  - Vitest ran the full frontend suite: `191` files, `1791` tests passed.
- `pnpm --dir web typecheck`
- `make build-backend`
- `node docs/acceptance/t1457/capture-t1457.mjs`
- `go test ./internal/webconsole/api -run 'TestTeamRAM|TestAccessRAMRole'`
- `go test ./internal/authorization/...`

Known broad-regex backend check:

- `go test ./internal/webconsole/api -run 'Test.*(TeamRAM|RAMRole|Access)'` failed in existing `TestAccessOverviewShowsTeamRAMAndDirectBindingUnion`, missing `custom_role` union source. The narrower Team RAM/RAM Role handler tests passed.

## Constraints

- `get_team_rule` was unavailable in this isolated executor, and the task forbids agent-center control-plane/database/socket/token/raw-HTTP fallbacks.
- No `.openai/hosting.json` or deploy credential is available in this workspace. The script validates a fresh local exact-HEAD production instance; no external durable URL is claimed from this executor.
