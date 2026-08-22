# T1457 Team Roles / RAM Role Mapping Gate Evidence

- Recovery base: `ddba9b10816b803b0563e97de574ebe7378c8ef2` (`origin/main`, verified before edits).
- Canonical mockup attachment SHA256: `80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56`.
- Canonical PNG used by the capture script: `/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/agents/01KTVBJCXN6XV8MXK3B9S5VS2S/tasks/team-roles-ddba-gate/t1457-canonical.png`.
- Capture method: `docs/acceptance/t1457/capture-t1457.mjs` starts a fresh `bin/agent-center` binary, signs up through the public auth API, seeds Team/RAM Role data through org-scoped Web API endpoints, drives Chromium at `1672 x 941`, and writes same-size canonical overlays/diffs with `docs/acceptance/t1457/canonical-diff.py`.
- Exact-HEAD local instance: `capture-state.json` records `/api/system/version.commit` from the fresh `bin/agent-center` instance. For this executor pass the binary was rebuilt with `COMMIT=$(git rev-parse HEAD)` so the version endpoint returns the full commit SHA, not the Makefile default short SHA.
- Stable shared preview: not completed in this isolated executor. The workspace has no `.openai/hosting.json`, `wrangler.toml`, `vercel.json`, `netlify.toml`, or project deploy script for a public preview, and the environment exposes no Cloudflare/Vercel/Netlify/Sites/GitHub deploy credential variables. No owner/reviewer-accessible URL is claimed.

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
- `FULL_SHA=$(git rev-parse HEAD); make build-backend COMMIT=$FULL_SHA VERSION=$(git rev-parse --abbrev-ref HEAD)-$FULL_SHA`
- `node docs/acceptance/t1457/capture-t1457.mjs`

Build completed with the existing CSS minifier warning at generated CSS line 3031.

## Stable Preview Handoff

This repository currently provides a local deployed-binary path, not a public preview path. To produce the missing owner/reviewer-accessible stable instance, the owner needs to supply one concrete hosting target plus credentials/metadata:

- Sites: `.openai/hosting.json` with the existing project id, plus the Sites connector/session available to the executor.
- Cloudflare/Vercel/Netlify: project config (`wrangler.toml`, `vercel.json`, or `netlify.toml`) and the corresponding deploy credential variables.
- VPS/systemd path: host, SSH/deploy key, target config path, and the external base URL.

Minimum owner-side build/deploy commands once a target exists:

```sh
git fetch origin
git checkout <candidate-sha>
FULL_SHA=$(git rev-parse HEAD)
make build-backend COMMIT=$FULL_SHA VERSION=$(git rev-parse --abbrev-ref HEAD)-$FULL_SHA
# deploy ./bin/agent-center and the matching config via the project hosting path
curl -fsS "$PUBLIC_BASE_URL/api/system/version" | jq -e --arg sha "$FULL_SHA" '.commit == $sha'
```

Minimum post-deploy evidence capture command:

```sh
T1457_CANONICAL=/path/to/t1457-canonical.png \
T1457_BASE_URL="$PUBLIC_BASE_URL" \
node docs/acceptance/t1457/capture-t1457.mjs
```

The current capture script starts its own isolated local server. A public-target variant should reuse the same browser assertions and diff pipeline while replacing the local spawn/signup base URL with the supplied stable URL and owner/reviewer seed credentials.
