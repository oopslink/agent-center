# T1450 UI Hard Gate Evidence Index

Scope: fresh detached `origin/main` target `69d5b662bef16882f1e163546da7b7168f80e1cd`, evidence-only execution on 2026-08-20/21 Asia-Shanghai. Prior green commit `d3ca6c14` was not reused as evidence.

## Build and Instance

| Check | Expected | Actual | Evidence |
| --- | --- | --- | --- |
| Release binary from target SHA | Formal binary built from `69d5b662` | `agent-center ui-hard-gate-69d5b662 (commit 69d5b662)` | `logs/01-build.log`, `logs/02-version.log` |
| Binary SHA | Installed `bin/agent-center` matches release binary | both `7c3b019b8cbe126036ecaa0c5480f63462faa97416f29ab6e3df9a6444ae44ff` | `logs/02-binary-sha256.log` |
| Health SHA/version | Health returns formal version | `/api/health` returned `{"status":"ok","version":"ui-hard-gate-69d5b662"}` | `logs/07-api-health-http.txt` |
| Fresh install with agent | Exposes missing runtime model if seed lacks catalog | `runtime_model_not_found` for `claude-opus-4-8` reproduced | `logs/03-install-test-instance.json`, `logs/04-list-after-failed-agent.log` |
| Fresh seed instance | New isolated instance installed | `t1450-seed-234910`, org `org-d6bba9bb` | `logs/05-install-seed-test-instance.json` (secrets redacted) |
| Org-bound workers | Real workers online and versioned | two workers online, version `ui-hard-gate-69d5b662+69d5b662` | `logs/10-org-bound-workers.log`, `logs/11-workers-poll.json`, `logs/12-browser-steps.json` |

## Browser UI Flow

Final passing browser run: `logs/12-browser-run-11.log`. Structured steps: `logs/12-browser-steps.json`. Network API index: `logs/12-browser-api-events.json`. Console index: `logs/12-browser-console-events.json`.

| Operation | Expected | Actual | Screenshots / Logs |
| --- | --- | --- | --- |
| Sign in | Owner signs in through real UI | `/signin` to org projects succeeded | `screenshots/01-signin-default-*`, `screenshots/02-signed-in-*` |
| Access loading | Loading state visible | delayed Access overview route produced loading screenshot | `screenshots/03-access-loading-*` |
| Access entry contract | Access has exactly Roles/Mappings/Subject Access and no Profiles | `hasRolesMappings=true`, `hasSubjectAccess=true`, `hasProfiles=false` | `screenshots/04-access-default-roles-mappings-*`, `logs/12-browser-steps.json` |
| AI Runtime create/edit/save | Model catalog can be created and edited in UI | `claude-opus-4-8` created during this execution, final reruns skipped because present; edit/save remained functional | `screenshots/05-ai-runtime-empty-models-*`, `06-ai-runtime-create-model-editing-*`, `07-ai-runtime-model-saved-*`, `08-ai-runtime-edit-model-*`, `09-ai-runtime-edit-saved-*` |
| Direct binding | Agent bound directly to org worker | `agent-0106c003` bound to `worker-d1385464`, cli `claude-code`, model `claude-opus-4-8` | `screenshots/10-agents-before-create-full.png`, `11-agent-create-bound-worker-*`, `12-agent-created-direct-binding-*`, `logs/12-browser-steps.json` |
| Multi-project | Second project exists in same org | `project-a03fa740` created as `Beta UI Gate mt1pn5ze` | `logs/12-browser-steps.json` |
| Team Role mappings non-empty | Real mappings render | `mappingRows=56`; created team roles use `Team basic` and `Team contributor` | `screenshots/13-team-role-mappings-nonempty-*`, `logs/12-browser-steps.json` |
| Preview / save / instant refresh | Mapping preview appears, save updates list immediately | Preview and saved refreshed view captured | `screenshots/14-team-role-mapping-preview-*`, `15-team-role-mapping-save-refresh-*` |
| RAM role create / publish / revoke | Custom role can be edited, versioned, then revoked with immediate refresh | role `UI release operator mt1pn5ze` created v1, published v2, revoked | `screenshots/16-ram-role-create-editing-*`, `17-ram-role-created-*`, `18-ram-role-publish-editing-*`, `19-ram-role-published-v2-*`, `20-ram-role-revoked-refresh-*` |
| UI-triggered 409 | Stale publish shows CAS conflict | POST `/access/ram-roles/role-43bdb1f293336366/versions` returned `409 version_conflict` | `screenshots/21-ui-triggered-409-cas-*`, `logs/12-browser-api-events.json`, `logs/12-browser-console-events.json` |
| UI 403 | Non-manager member sees Access forbidden | UI showed `Access unavailable (403)`; member effective permissions lacked `org.member.role.manage` | `screenshots/22-ui-403-forbidden-*`, `logs/12-browser-steps.json`, `logs/12-browser-api-events.json` |
| Subject access | Direct/inherited subject view renders | Subject Access page captured after grant/revoke and team mapping changes | `screenshots/23-subject-access-direct-and-inherited-*` |

## Error/Disabled Notes

`runtime_model_not_found` was not a UI blocker. It was reproduced by a `--with-agent` install against an empty seed runtime catalog, then eliminated by creating `claude-opus-4-8` in AI Runtime through the UI. After that, direct agent binding and worker readback succeeded.

Observed disabled publish states during script development were caused by selecting the first global `team.memory.review` occurrence, which belonged to a system role, not the current custom role detail. The final script scopes permission clicks to `[data-testid="access-role-detail"]`, and publish/revoke/409 paths complete against the intended custom roles.

For 403, the backend `access/overview` endpoint still returns read data to a basic member, but the Access route is gated by effective permissions. Final evidence records the UI forbidden page and a member-session effective-permissions probe with `hasManagePermission=false`.

## Cleanup

| Check | Expected | Actual | Evidence |
| --- | --- | --- | --- |
| Uninstall | Test instance and worker labels booted out | center, seed workers, and org-bound workers booted out; prefix removed | `logs/90-uninstall.log`, `logs/90-uninstall-status.log` |
| Post-uninstall list | No remaining test instance | `list-test-instances --output=json` returned `null` | `logs/91-list-after-uninstall.json` |
| Prefix cleanup | Instance prefix absent | `/Users/oopslink/.agent-center-test/t1450-seed-234910 exists=no` | `logs/92-prefix-cleanup.log` |

## Artifact Hygiene

Cookie/HAR files were removed before commit because they contain session headers. Redacted fields remain marked as `<redacted-after-test-uninstall>` in `logs/05-install-seed-test-instance.json`.
