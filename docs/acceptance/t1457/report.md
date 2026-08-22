# T1457 Team Roles / RAM Role Mapping Evidence

- Fresh base fetched at start: `origin/main = ddba9b10816b803b0563e97de574ebe7378c8ef2`.
- Candidate work is on branch `ac-exec/task-t1477-fresh-t1457`; final candidate SHA is assigned after committing this evidence and must be verified with `git merge-base HEAD ddba9b10816b803b0563e97de574ebe7378c8ef2`.
- Canonical baseline is committed at `docs/acceptance/t1457/canonical-t1457-1672x941.png`.
- Canonical SHA256: `80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56`.
- `/version.commit` was added as a plain-text exact build-commit probe; `/api/system/version` remains the JSON build identity endpoint.

## Evidence Index

Each 1672x941 state has a candidate screenshot, canonical overlay, canonical pixel diff, and pixel statistics JSON:

| State | Candidate | Overlay | Diff | Stats |
| --- | --- | --- | --- | --- |
| Role list | `01-role-list-candidate-1672x941.png` | `01-role-list-canonical-overlay-1672x941.png` | `01-role-list-canonical-diff-1672x941.png` | `01-role-list-canonical-diff-stats.json` |
| Role detail | `02-role-detail-candidate-1672x941.png` | `02-role-detail-canonical-overlay-1672x941.png` | `02-role-detail-canonical-diff-1672x941.png` | `02-role-detail-canonical-diff-stats.json` |
| Create drawer | `03-create-drawer-candidate-1672x941.png` | `03-create-drawer-canonical-overlay-1672x941.png` | `03-create-drawer-canonical-diff-1672x941.png` | `03-create-drawer-canonical-diff-stats.json` |
| Edit drawer | `04-edit-drawer-candidate-1672x941.png` | `04-edit-drawer-canonical-overlay-1672x941.png` | `04-edit-drawer-canonical-diff-1672x941.png` | `04-edit-drawer-canonical-diff-stats.json` |
| Work config | `05-work-config-candidate-1672x941.png` | `05-work-config-canonical-overlay-1672x941.png` | `05-work-config-canonical-diff-1672x941.png` | `05-work-config-canonical-diff-stats.json` |
| RAM mapping | `06-ram-mapping-candidate-1672x941.png` | `06-ram-mapping-canonical-overlay-1672x941.png` | `06-ram-mapping-canonical-diff-1672x941.png` | `06-ram-mapping-canonical-diff-stats.json` |
| Version / duplicate / delete safeguard | `07-version-duplicate-delete-safeguard-candidate-1672x941.png` | `07-version-duplicate-delete-safeguard-canonical-overlay-1672x941.png` | `07-version-duplicate-delete-safeguard-canonical-diff-1672x941.png` | `07-version-duplicate-delete-safeguard-canonical-diff-stats.json` |
| CAS error | `08-cas-error-candidate-1672x941.png` | `08-cas-error-canonical-overlay-1672x941.png` | `08-cas-error-canonical-diff-1672x941.png` | `08-cas-error-canonical-diff-stats.json` |
| API error | `09-api-error-candidate-1672x941.png` | `09-api-error-canonical-overlay-1672x941.png` | `09-api-error-canonical-diff-1672x941.png` | `09-api-error-canonical-diff-stats.json` |

Additional raw evidence:

- `10-overflow-1280-candidate.png` — fresh 1280x941 browser overflow check.
- `capture-state.json` — route, seeded IDs, version probe, canonical SHA, viewport metrics, state index, console/network evidence.
- `browser-verification.json` — browser CRUD/mapping/CAS/error action log plus console/network events.
- `capture-t1457.mjs` — one-command reproduction script.

## Browser Verification

`node docs/acceptance/t1457/capture-t1457.mjs` launches an isolated `bin/agent-center` instance, signs up through the public auth API, seeds Team/RAM data through org-scoped Web API endpoints, and drives Chromium.

Verified browser actions:

- Role list, role detail, work config, RAM mapping table, duplicate drawer, referenced delete safeguard.
- CRUD create/edit/delete for RAM Roles through the Team Roles UI.
- Mapping stale CAS flow through a real browser action, producing a 409 response.
- Duplicate stable-key API error state through the create drawer, producing a 409 response.
- Console/network capture includes the expected 409 responses; aborts at shutdown are recorded in raw JSON.
- Width guards: `1672 clientWidth=1672 scrollWidth=1672`; `1280 clientWidth=1280 scrollWidth=1280`.

## Verification Commands

- `node --check docs/acceptance/t1457/capture-t1457.mjs`
- `go test ./internal/webconsole/api -run 'TestAPI_(SystemVersion|VersionCommitPlainText)'`
- `make build-backend`
- `pnpm --dir tests/e2e/v2 install --frozen-lockfile`
- `node docs/acceptance/t1457/capture-t1457.mjs`

`make build-backend` completed with the existing generated CSS minifier warning at CSS line 3031.

## Isolation Notes

`get_team_rule` was not used because this isolated executor has no permitted center/MCP access, and the task explicitly forbids center database, socket, token, admin HTTP, raw HTTP, or process-argument fallbacks. The applicable rules were honored by local evidence, public app endpoints, exact git checks, and post-action readback.
