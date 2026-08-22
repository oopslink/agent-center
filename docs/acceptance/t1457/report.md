# T1457 Team Roles / RAM Role Mapping Gate Evidence

- Recovery base: `ddba9b10816b803b0563e97de574ebe7378c8ef2` (`origin/main`, verified before edits).
- Canonical mockup attachment SHA256: `80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56`.
- Canonical PNG used by the capture script: `/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/agents/01KTVBJCXN6XV8MXK3B9S5VS2S/tasks/team-roles-ddba-gate/t1457-canonical.png`.
- Capture method: `docs/acceptance/t1457/capture-t1457.mjs` starts a fresh `bin/agent-center` binary, signs up through the public auth API, seeds Team/RAM Role data through org-scoped Web API endpoints, drives Chromium at `1672 x 941`, and writes same-size canonical overlays/diffs with `docs/acceptance/t1457/canonical-diff.py`.
- Instance note: this isolated executor has no Sites/project hosting metadata or deployment credentials. Stable shared deployment must be supplied outside this workspace; this report does not claim an external URL.

## 1672x941 State Matrix

Each state has a candidate screenshot, canonical overlay, pixel diff, and JSON pixel stats under `docs/acceptance/t1457/`.

| State | Candidate |
| --- | --- |
| Role list, detail entries, work config, mapping table | `01-role-list-candidate-1672x941.png` |
| Role detail mapping drawer with CAS work config | `02-role-detail-drawer-candidate-1672x941.png` |
| RAM Role create drawer | `03-ram-role-create-drawer-candidate-1672x941.png` |
| RAM Role edit drawer | `04-ram-role-edit-drawer-candidate-1672x941.png` |
| RAM Role duplicate drawer | `05-ram-role-duplicate-drawer-candidate-1672x941.png` |
| Mapping preview impact before apply | `06-mapping-preview-candidate-1672x941.png` |
| Version/duplicate/delete safeguard | `07-version-duplicate-delete-safeguard-candidate-1672x941.png` |
| Mapping CAS conflict error | `08-cas-conflict-error-candidate-1672x941.png` |
| RAM Role duplicate/create error | `09-create-error-candidate-1672x941.png` |

The matching files are named:

- `NN-...-canonical-overlay.png`
- `NN-...-canonical-pixel-diff.png`
- `NN-...-canonical-diff-stats.json`

`capture-state.json` records the fresh instance URL, org/team/RAM Role ids, `/api/system/version`, browser assertions, API CRUD/CAS checks, console/network audit, and the checked state list.

## Additional Evidence

- Fresh 1280 overflow capture: `fresh-1280-overflow-candidate.png`.
- 1280 overflow result in `capture-state.json`: `clientWidth=1280`, `scrollWidth=1280`.
- Browser CRUD/mapping/CAS/error assertions:
  - Mapping preview and apply succeeded through the browser UI.
  - Stale browser mapping write produced a visible CAS error.
  - Duplicate RAM Role create produced a visible error.
- API CRUD/CAS checks:
  - RAM Role create: ok.
  - RAM Role new version: latest version advanced to `2`.
  - Stale RAM Role version write: `409`.
- Console/network audit:
  - `networkFailures=[]`.
  - `consoleErrors=[]`.
  - Expected browser resource 409s are recorded as CAS/error assertions.

## Verification Commands

- `pnpm --dir web install --frozen-lockfile`
- `pnpm --dir tests/e2e/v2 install --frozen-lockfile`
- `pnpm --dir web test -- TeamDetail.test.tsx Access.test.tsx Version.test.tsx`
  - Vitest argument forwarding ran the full frontend suite: `191` files, `1791` tests passed.
- `pnpm --dir web typecheck`
- `make build-backend`
- `node docs/acceptance/t1457/capture-t1457.mjs`

Build completed with the existing CSS minifier warning at generated CSS line 3031.
