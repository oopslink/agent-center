# Fresh Test-Instance Dogfood Evidence

- Candidate SHA: `69d5b662bef16882f1e163546da7b7168f80e1cd`
- Build version: `main-69d5b662bef1`
- Instance prefix: `/tmp/ac-test-instance-69d5b662bef1`
- Web URL: `http://127.0.0.1:47100/`
- Date: 2026-08-20
- Browser: real Chromium via `agent-browser`

## Status

Completed with limitations noted below.

## Verdict

Fresh detached `FETCH_HEAD` from `origin main` was built into a formal release layout, installed into a fresh isolated prefix, started from the installed `current` binary, verified through live `/api/health`, exercised through a real browser, then formally uninstalled with `--purge` and verified by local filesystem/resource readback.

No reproducible product-blocking browser defect was confirmed during this pass. The run did expose these limitations in the requested matrix:

- A clean authenticated `403` was not produced. The first-run user is org owner; unauthenticated boundary returned `401` (`logs/api-unauth-boundary.json`).
- Access custom role create controls issued POSTs but the custom role was not visible afterward (`network/access-create-network.txt`, `screenshots/030-access-create-filled-full.png`, `screenshots/031-access-after-create-full.png`). This was preserved as evidence, but not classified without deeper endpoint diagnostics.
- Access revoke preview was captured in selected-control state only; I did not execute a destructive revoke against the active admin/system policy.
- `videos/` is present but empty because no reproducible interactive defect was confirmed that required video evidence.

## Evidence Index

- Build/install/SHA:
  - `logs/00-build-env.txt`
  - `logs/01-make-release-dir.log`
  - `logs/02-staged-version.log`
  - `logs/04-install-center.log`
  - `logs/07-installed-version.log`
  - `logs/09-health-probes.log`

- First-run/auth:
  - `screenshots/001-initial-full.png`
  - `screenshots/002-signup-filled-full.png`
  - `screenshots/003-after-signup-full.png`
  - `console/001-initial-errors.txt`
  - `network/002-after-signup-network.txt`

- Empty/default page sweep, each with full screenshot, detail screenshot, console and network logs:
  - Projects: `screenshots/010-projects-empty-full.png`, `screenshots/011-projects-detail.png`
  - Issues: `screenshots/010-issues-empty-full.png`, `screenshots/011-issues-detail.png`
  - Tasks: `screenshots/010-tasks-empty-full.png`, `screenshots/011-tasks-detail.png`
  - Plans: `screenshots/010-plans-empty-full.png`, `screenshots/011-plans-detail.png`
  - Repos: `screenshots/010-repos-empty-full.png`, `screenshots/011-repos-detail.png`
  - Conversations/channels: `screenshots/010-channels-empty-full.png`, `screenshots/011-channels-detail.png`
  - Teams: `screenshots/010-teams-empty-full.png`, `screenshots/011-teams-detail.png`
  - Access: `screenshots/010-access-empty-full.png`, `screenshots/011-access-detail.png`
  - Reminders: `screenshots/010-reminders-empty-full.png`, `screenshots/011-reminders-detail.png`
  - System/Environment: `screenshots/010-environment-empty-full.png`, `screenshots/011-environment-detail.png`

- Access surface:
  - Three-entry Access navigation and no Profiles entry: `logs/page-access-interactive.txt`, `screenshots/010-access-empty-full.png`
  - Create controls: `screenshots/030-access-create-filled-full.png`, `screenshots/031-access-after-create-full.png`
  - Revoke controls selected: `screenshots/060-access-revoke-selected-full.png`
  - API role readback: `logs/api-ram-roles-get.json`

- Multi-project/direct binding/immediate refresh:
  - Project create modal: `screenshots/040-project-create-modal-full.png`
  - Alpha/Beta project creation: `screenshots/041-project-alpha-filled-full.png`, `screenshots/042-project-alpha-created-full.png`, `screenshots/043-project-beta-filled-full.png`, `screenshots/044-projects-two-created-full.png`
  - Task create loading/conditional state: `screenshots/050-task-create-modal-full.png`, `screenshots/051-task-create-alpha-selected-full.png`
  - Task create/save: `screenshots/052-task-alpha-filled-full.png`, `screenshots/053-task-alpha-created-full.png`
  - Direct bound task detail: `screenshots/054-task-direct-detail-full.png`, `logs/task-direct-url.txt`
  - Project count refresh: `screenshots/055-projects-count-refresh-full.png`

- Edit/error/API states:
  - Task edit drawer: `screenshots/056-task-edit-state-full.png`
  - Missing task/error route: `screenshots/057-task-error-state-full.png`
  - Health/404/409/duplicate behavior probes: `logs/api-status-probes-2.json`
  - 401 unauth boundary: `logs/api-unauth-boundary.json`

- Uninstall/resource cleanup:
  - `logs/90-post-stop-health.log`
  - `logs/91-uninstall-center-purge.log`
  - `logs/92-post-uninstall-resource-readback.log`
  - `logs/93-post-uninstall-local-inventory.log`

## Counts

- Screenshots: 40 PNG files in `screenshots/`
- Console logs: per page/workflow in `console/`
- Network logs: per page/workflow in `network/`
- Videos: 0, no confirmed interactive defect requiring repro video
