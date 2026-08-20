# T1441 Evidence-Only Continuation: exec-aafae534

## Binding

- Execution id: `exec-aafae534`
- Branch: `ac-exec/task-525f2500/exec-aafae534`
- Required fresh base: `origin/main@69d5b662bef16882f1e163546da7b7168f80e1cd`
- Pre-report `HEAD`: `69d5b662bef16882f1e163546da7b7168f80e1cd`
- Pre-report merge-base with `origin/main`: `69d5b662bef16882f1e163546da7b7168f80e1cd`
- Candidate binary: `agent-center ac-exec/task-525f2500/exec-aafae534-69d5b662 (commit 69d5b662)`
- Installed prefix: `/tmp/ac-t1441-exec-aafae534`
- Web URL: `http://127.0.0.1:17100`

This run was a minimal evidence-only continuation. It did not change business code and did not rerun already-green gates outside the two requested UI gaps.

## Reused Green Evidence

- `d3ca6c14`: reused for the previously accepted deployed-instance and UI/API state coverage that this continuation was instructed not to rerun.
- `eaebddbfc01fcb42259ee98a4ee398b806ac16e5`: reused for the existing green evidence for default, empty, loading, error, edit, preview, save, revoke, UI 409, 403, Team Role non-empty, direct worker binding, `runtime_model_not_found`, and disabled states.

New evidence in this directory covers only:

1. Multi-project flow through real Projects UI.
2. UI-created project direct grant plus team inherited mapping, with Subject access source chain expanded and direct revoke immediate refresh.

## Verdict For This Continuation

PASS for the two requested supplemental gaps.

- Multi-project was proven through real browser UI. `T1441 Alpha Project` and `T1441 Beta Project` were created from the Projects UI, the Projects list displayed both, and the browser entered each project context through the UI.
- Direct plus inherited access was constructed through UI controls. The current user was added to `T1441 Access Team`, the team was linked to `T1441 Beta Project`, the team `coder` role was mapped to RAM role `project.member`, and a direct project-scoped `project.stage.manage` grant was added via Access batch grant UI.
- Subject access source chain was expanded. Before revoke it showed the team membership chain and `custom_role` direct source; after revoke it refreshed to remove `custom_role` / `project.stage.manage` while retaining inherited project permissions through `project_member`.
- Access UI entrypoints visible during the run were the three expected entries: `RAM Roles`, `Team Role mappings`, and `Subject access`; no Access `Profiles` entry was present.

## Instance And Cleanup

- Fresh detached base build: see `logs/05-git-state-pre-report.txt`.
- Binary and system version SHA: see `logs/01-version-health.txt`.
- `/api/system/version` returned commit `69d5b662`.
- `/health` returned HTTP 200 with the SPA HTML fallback, not a JSON health body; this is recorded verbatim in `logs/01-version-health.txt`.
- Browser console/network evidence is under `logs/02-browser-console.txt`, `logs/03-browser-errors.txt`, and `logs/04-browser-network.txt`.
- Formal uninstall with purge completed; `/tmp/ac-t1441-exec-aafae534` was removed and port `17100` no longer accepted connections. See `logs/07-uninstall.txt` and `logs/08-cleanup-check.txt`.

## UI Objects

- Organization route id: `org-ec4ad7e5`
- Organization resource id shown in UI/API: `organization-f8093932`
- User: `T1441 Executor`, `user:user-0286841a`
- Alpha project: `T1441 Alpha Project`, `project-5d182a5a`
- Beta project: `T1441 Beta Project`, `project-57cc842d`
- Team: `T1441 Access Team`, `team-f0aa10d0`
- Team inherited mapping: `T1441 Access Team / coder -> project.member`
- Direct grant before revoke: `user:user-0286841a -> project.stage.manage` on `project-57cc842d`

## Screenshot Index

| File | Evidence |
|---|---|
| `screenshots/00-initial-full.png` | Initial browser state at the fresh instance. |
| `screenshots/01-signed-in-projects-full.png` | Signed-in Projects UI after account/org creation. |
| `screenshots/02a-alpha-created-list.png` | Alpha project visible after UI creation. |
| `screenshots/02b-beta-create-modal-detail.png` | Beta project creation modal from Projects UI. |
| `screenshots/02-projects-list-two-full.png` | Projects UI list showing both Alpha and Beta. |
| `screenshots/03-alpha-project-context-full.png` | Entered Alpha project context through UI. |
| `screenshots/04-projects-list-before-beta-switch-full.png` | Returned to Projects list before switching. |
| `screenshots/05-beta-project-context-full.png` | Entered Beta project context through UI. |
| `screenshots/06-teams-empty-or-list-full.png` | Teams UI before supplemental team setup. |
| `screenshots/07-new-team-modal-initial-detail.png` | New team modal from UI. |
| `screenshots/08-new-team-assignment-detail.png` | Team create path blocked by missing default model during setup. |
| `screenshots/09-system-nav-for-runtime-full.png` | System navigation to AI Runtime for setup unblock. |
| `screenshots/10-ai-runtime-full.png` | AI Runtime page. |
| `screenshots/11-ai-runtime-new-model-modal-detail.png` | New runtime model modal. |
| `screenshots/12-ai-runtime-sonnet5-filled-detail.png` | Runtime model `sonnet-5` filled in UI. |
| `screenshots/13-ai-runtime-sonnet5-created-full.png` | Runtime model created and enabled. |
| `screenshots/14-new-team-assignment-valid-detail.png` | New team creation unblocked after runtime model setup. |
| `screenshots/15-team-created-detail-full.png` | Team detail after UI-created team. |
| `screenshots/16-team-linked-projects-empty-full.png` | Team Linked projects tab before association. |
| `screenshots/17-associate-project-picker-detail.png` | Project picker listing Alpha and Beta. |
| `screenshots/18-team-beta-associated-full.png` | Beta associated to team through UI. |
| `screenshots/19-access-roles-before-mapping-full.png` | Access Team Role mappings before final inherited mapping. |
| `screenshots/20-access-planner-mapping-picker-open-detail.png` | Planner mapping picker opened. |
| `screenshots/21-access-planner-project-member-selected-detail.png` | Planner mapping selection. |
| `screenshots/22-access-planner-mapping-preview-detail.png` | Planner mapping preview. |
| `screenshots/23-access-mapping-confirm-modal-detail.png` | Planner mapping confirm modal. |
| `screenshots/24-access-mapping-saved-full.png` | Planner mapping saved. |
| `screenshots/25-team-members-before-add-full.png` | Team members tab before adding current user. |
| `screenshots/26-add-member-modal-detail.png` | Add member modal. |
| `screenshots/27-add-member-human-planner-selected-detail.png` | Human member selected in UI. |
| `screenshots/29-team-member-add-attempt-full.png` | Current user visible as team member with role `coder`. |
| `screenshots/30-access-coder-project-member-selected-detail.png` | Coder role mapped to `project.member`. |
| `screenshots/31-access-coder-mapping-preview-detail.png` | Coder mapping preview: `1 members`, `+1 / -0 roles`, `1 projects`. |
| `screenshots/32-access-coder-mapping-confirm-detail.png` | Coder mapping confirm modal. |
| `screenshots/33-access-coder-mapping-saved-full.png` | Coder inherited mapping saved. |
| `screenshots/34-access-batch-drawer-initial-detail.png` | Access batch grant drawer from UI; Access entries visible. |
| `screenshots/35-access-batch-resource-picker-detail.png` | Batch grant resource picker. |
| `screenshots/36-access-batch-beta-selected-detail.png` | Beta project selected for direct project grant. |
| `screenshots/37-access-batch-preview-detail.png` | Direct project grant preview. |
| `screenshots/38-access-batch-confirm-detail.png` | Direct project grant confirm modal. |
| `screenshots/39-access-batch-result-detail.png` | Direct grant applied; revoke checkbox visible. |
| `screenshots/40-access-subject-before-expand-full.png` | Subject access search result before expansion. |
| `screenshots/41-access-subject-source-chain-expanded-full.png` | Expanded source chain showing team inherited and direct/custom sources. |
| `screenshots/42-access-subject-before-direct-revoke-detail.png` | Direct grant selected for revoke. |
| `screenshots/43-access-direct-revoke-preview-detail.png` | Revoke preview for direct grant. |
| `screenshots/44-access-direct-revoke-result-full.png` | After revoke: immediate refresh, direct source removed, inherited project access remains. |

## Log Index

| File | Evidence |
|---|---|
| `logs/01-version-health.txt` | Binary version and HTTP health/root response. |
| `logs/02-browser-console.txt` | Browser console capture. |
| `logs/03-browser-errors.txt` | Browser error capture; contains two 401 resource lines from initial auth probing. |
| `logs/04-browser-network.txt` | Browser network capture for real UI calls, including team member add, team role mapping preview/save, batch grant preview/apply, and revoke preview/confirm. |
| `logs/05-git-state-pre-report.txt` | Fresh base, branch, merge-base, ahead, and pre-report status. |
| `logs/06-access-profiles-static.txt` | Static confirmation that Access page labels are `RAM Roles`, `Team Role mappings`, and `Subject access`; `Profile` hits are outside Access production entrypoints. |
| `logs/07-uninstall.txt` | Formal `agent-center uninstall center --purge --yes` output. |
| `logs/08-cleanup-check.txt` | Prefix removed and web port no longer serving. |
