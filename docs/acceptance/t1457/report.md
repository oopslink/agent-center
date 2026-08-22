# T1457 Team Roles / RAM Mapping Gate-Reject Repair Evidence

This evidence set is generated from a fresh branch based on the fetched
`origin/main` baseline. The prior rejected baseline/head
`ddba9b10816b803b0563e97de574ebe7378c8ef2` is not a deliverable candidate SHA.

## Baseline And Canonical

- First command for this repair: `git fetch origin main`.
- `CURRENT_MAIN=$(git rev-parse origin/main)` at repair start:
  `ddba9b10816b803b0563e97de574ebe7378c8ef2`.
- Required canonical attachment SHA256:
  `80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56`.
- Canonical file used by the capture script:
  `/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/agents/01KV01ZG5T332EYTFCVTNAZB9B/tasks/t1457-canonical.png`.
- The capture script verifies that canonical SHA before any screenshot or diff work.

## Production Change

- Added `GET /version.commit`, a public plain-text build commit probe for exact
  stable-instance HEAD verification.
- Existing `GET /api/system/version` remains the structured build identity
  endpoint and is captured as a cross-check.

## Evidence Index

`capture-t1457.mjs` launches a real isolated `bin/agent-center` server, signs up
through the public Web auth API, seeds org-scoped Teams/RAM Roles through public
Web API endpoints, and drives Chromium at `1672 x 941`.

For each critical state, it writes:

- `*-candidate-1672x941.png`
- `*-canonical-overlay.png`
- `*-canonical-pixel-diff.png`
- `*-canonical-diff-stats.json`

State list:

- `01-role-list-detail-work-config-ram-mapping`
- `02-mapping-edit-drawer-cas`
- `03-create-ram-role-drawer`
- `04-edit-ram-role-drawer-versioned`
- `05-duplicate-ram-role-drawer`
- `06-delete-safeguard-modal`
- `07-delete-safeguard-notice`
- `08-cas-error-mapping`
- `09-error-state-after-real-api-409`

Additional raw evidence:

- `capture-state.json` - baseline, HEAD, merge-base values, version probes,
  canonical SHA, seeded ids, state stats, console/network evidence.
- `console-network.json` - browser console entries, request failures, and >=400
  responses. The 409 response is intentional CAS/error evidence.
- `fresh-1280-overflow-candidate.png` - independent 1280px overflow proof.

## Browser Coverage

The real-browser flow covers:

- Team Role list and detail entry.
- Work config values.
- RAM Role mapping table.
- Mapping edit drawer.
- RAM Role create drawer.
- RAM Role edit drawer and versioned controls.
- Duplicate RAM Role drawer.
- Referenced-role delete safeguard modal and notice.
- CAS conflict caused by a real backend version race.
- Stale delete API error state.
- `1672 x 941` no-horizontal-overflow guard for every captured state.
- Fresh `1280 x 941` no-horizontal-overflow guard.

## Commands

```sh
git fetch origin main
CURRENT_MAIN=$(git rev-parse origin/main)
go test ./internal/webconsole/api -run 'TestAPI_(SystemVersion|VersionCommit)'
node --check docs/acceptance/t1457/capture-t1457.mjs
make build-backend
node docs/acceptance/t1457/capture-t1457.mjs
git merge-base HEAD "$CURRENT_MAIN"
git merge-base HEAD ddba9b10816b803b0563e97de574ebe7378c8ef2
```

For a stable review instance:

```sh
make build-backend
T1457_KEEPALIVE=1 node docs/acceptance/t1457/capture-t1457.mjs
cloudflared tunnel --url "$(node -e 'console.log(require("./docs/acceptance/t1457/stable-instance.json").baseURL)')"
curl "$STABLE_URL/version.commit"
curl "$STABLE_URL/api/system/version"
```

Use `T1457_OUTDIR=/tmp/t1457-final` for post-commit stable verification that
must not modify the committed evidence tree.

The final candidate SHA and stable URL are intentionally verified after the
delivery commit, because writing the final SHA into committed files is
self-referential and would change the SHA being reported.
