# T1457 Team Roles / RAM Role Mapping Evidence

- Canonical mockup attachment recorded: `ac://files/01M0HRMZEV7XS8A3MNGG64ZZW1`.
- Canonical mockup SHA256: `80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56`.
- Canonical mockup file used by the harness: `/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/agents/01KV01ZG5T332EYTFCVTNAZB9B/tasks/t1457-canonical.png`.
- The harness fails unless `/api/system/version.commit` exactly equals `git rev-parse HEAD` for the binary under test.
- Stable external preview remains blocked in this isolated executor: this checkout has no `.openai/hosting.json` or other shared deployment mechanism, and the task forbids using agent-center control-plane fallbacks. No non-localhost stable URL is claimed here.

## Repro

Build the final candidate binary with the full commit SHA, then run the browser evidence harness:

```sh
make build-backend COMMIT="$(git rev-parse HEAD)"
node docs/acceptance/t1457/capture-t1457.mjs
```

## State Coverage

`capture-t1457.mjs` launches a fresh isolated binary instance, signs up through the public auth API, seeds data through org-scoped Web API endpoints, drives Chromium through the Team Roles UI, and writes `capture-state.json`.

The 1672x941 states receive candidate screenshots, canonical overlays, pixel diffs, and per-state JSON stats:

- `01-role-list-detail` — role list, selected role detail entry, work config, RAM mapping table, safeguards.
- `02-ram-mapping-filter` — RAM mapping table filter/search.
- `03-mapping-edit-drawer` — mapping edit drawer, work config, CAS read.
- `04-mapping-preview` — mapping preview immediate impact.
- `05-mapping-cas-error` — stale CAS mapping error and refresh affordance.
- `06-ram-role-create-drawer` — RAM Role create drawer and permission picker.
- `07-ram-role-edit-version` — edit drawer and versioned write controls.
- `08-ram-role-duplicate` — duplicate drawer.
- `09-delete-safeguard` — referenced RAM Role delete safeguard confirmation.
- `10-ram-role-error` — stale RAM Role version error.

The harness also captures `11-overflow-1280-1280x941.png` and fails on horizontal overflow.

## Browser/API Checks

The same run verifies:

- CRUD: RAM Role create, metadata update, new version, and unreferenced delete through public Web API endpoints.
- Mapping: Team Role RAM mapping preview and PUT.
- CAS/error: stale mapping PUT returns `409`; stale RAM Role version write returns `409`; both are also exercised in the browser UI.
- Console/network: browser console errors, page errors, failed requests, and API 5xx responses fail the run.

## Generated Evidence

The harness writes:

- `capture-state.json`
- `canonical-diff-stats-all.json`
- `NN-*-1672x941.png`
- `NN-*-canonical-overlay.png`
- `NN-*-canonical-pixel-diff.png`
- `NN-*-canonical-diff-stats.json`
- `11-overflow-1280-1280x941.png`
