# t525f2500 Test Instance Acceptance Report

Date: 2026-08-20
Branch: `ac-exec/task-525f2500/exec-db58108b`
Candidate source SHA: `69d5b662bef16882f1e163546da7b7168f80e1cd`
Candidate version: `origin-main-69d5b662`

## Verdict

PASS with noted negative observations captured as expected evidence. A fresh installed test instance was built from the current `origin/main` SHA, started, validated through the production binary, exercised through a real browser plus same-session production APIs, then formally uninstalled with purge and cleanup readback.

## Build And Install Chain

- Origin main readback: `logs/02-origin-main-lsremote.txt` shows `69d5b662bef16882f1e163546da7b7168f80e1cd refs/heads/main`.
- Local candidate HEAD: `logs/01-head.txt`.
- Release layout build: `logs/11-make-release-dir.log`.
- Candidate binary SHA256: `logs/12-candidate-sha256.txt`.
- Fresh install: `logs/20-install-center.log`.
- Installed binary version/SHA: `logs/22-installed-version.txt`, `logs/23-installed-binary-sha256.txt`.
- Runtime health/version readback: `logs/32-http-probes-after-signup.txt`, `logs/41-runtime-catalog-probes.json`.

## Browser Coverage

Screenshots are under `screenshots/`; accessibility snapshots, console, and page errors are under `browser/`.

- Fresh setup/default state: `00-initial.png`, `01-signup-filled.png`, `02-after-signup.png`.
- Access entry point and three allowed Access subentries: `10-access-full.png`, `60-access-overview-error-snapshot.txt`, `62-access-empty-search-snapshot.txt`.
- RAM Roles: `11-access-ram-full.png`, `11-access-ram-detail.png`.
- Team Role mappings: `11-access-team-full.png`, `11-access-team-detail.png`.
- Subject access: `11-access-subject-full.png`, `11-access-subject-detail.png`.
- After data refresh with two projects: `42-access-after-seed-refresh-full.png`.
- After direct revoke refresh: `52-after-revoke-refresh-full.png`.
- Error state: `60-access-overview-error-full.png`, `60-access-overview-error-detail.png`.
- Recovery after error: `61-access-recovered-full.png`.
- Empty state: `62-access-empty-search-full.png`, `62-access-empty-search-detail.png`.
- Project filter: `63-access-project-filter-full.png`.
- Mobile full-page state: `64-access-mobile-full.png`.

The Access secondary navigation showed exactly `RAM Roles`, `Team Role mappings`, and `Subject access`; no `Profiles` Access entry was observed in browser snapshots. The only `Profiles` text match in evidence is a built asset filename inside the build log, not UI navigation.

## API Workflow Evidence

- Auth/session/org/project/member probes: `logs/34-browser-fetch-org-probes.json`.
- Seed attempts and negative API behavior: `logs/40-seed-browser-api.json`.
  - Two projects were created: `Access Project Alpha`, `Access Project Beta`.
  - Human member `QA Human Member` was created.
  - Agent creation failed as expected without `worker_id`.
  - Team creation failed against empty model catalog with `runtime_model_not_found`, leaving Team Role mappings covered as the empty/default UI state.
- Access workflow: `logs/50-access-api-workflow.json`.
  - Created custom RAM role `QA Direct Binder`.
  - Saved v2 role permissions.
  - Stale expected version returned `409 version_conflict`.
  - Direct grant preview/apply succeeded for `QA Human Member` on `Access Project Beta`.
  - Effective permission readback showed `project.read`.
  - Revoke preview produced a token; confirm returned `409 revoke_preview_rejected`.
  - RAM role revoke returned `204`.
  - Forbidden check returned `403 permission_denied`.
- Direct revoke workflow: `logs/51-direct-revoke-workflow.json`.
  - `/permissions/batch/revoke` returned `status: revoked`.
  - Effective permissions for the direct binding immediately became `null`.

## Console And Network

- Console/page error summary: `logs/70-console-errors-summary.txt`.
- Page error files were empty.
- Console contains expected 401/404/400/409/403/ERR_FAILED lines from explicit negative probes and the intentional network-abort error-state test.
- Network abort evidence for error state: `network/60-access-overview-error-requests.txt`.

## Screenshot Readability

`logs/71-screenshot-file-metadata.txt` records PNG dimensions for 20 captured screenshots, including full-page desktop images and a mobile full-page image.

## Cleanup

- Server stop log: `logs/73-server-tail-after-stop.txt`.
- Formal uninstall: `logs/80-uninstall-center.log`.
- Cleanup readback: `logs/81-cleanup-readback.txt` shows `prefix_exists=no` and both web/server ports refusing connections.
- Post-uninstall browser readback: `logs/91-post-uninstall-browser-open.txt` shows `ERR_CONNECTION_REFUSED`.

## Notes

- `screenshots/90-post-uninstall-browser-state.png` is not used as browser acceptance evidence. It was captured after an unquoted shell URL prevented navigation. The authoritative post-uninstall browser evidence is `logs/91-post-uninstall-browser-open.txt`.
