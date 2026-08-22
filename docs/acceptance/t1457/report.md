# T1457 Team Roles / RAM Role Mapping Evidence

- Git parent at task start: `ddba9b10816b803b0563e97de574ebe7378c8ef2`.
- Final candidate HEAD at capture time: `4d0319f2b672e34a54c1ba9d7855cd1e1565aff0`.
- Runtime /api/system/version.commit: `4d0319f2b672e34a54c1ba9d7855cd1e1565aff0`.
- Canonical attachment SHA256: `80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56`.
- Canonical file used: `/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/agents/01KV01ZG5T332EYTFCVTNAZB9B/tasks/t1457-canonical.png`.
- Canonical file verified SHA256: `80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56`.
- Fresh browser instance: `http://127.0.0.1:53566` (isolated local server, torn down after capture).
- Stable external preview: `BLOCKED: no deploy/Sites/shared-preview credential or mechanism is available in this isolated executor`.
- 1280 overflow check: clientWidth=`1280`, scrollWidth=`1280`, url=`http://127.0.0.1:53566/organizations/org-0aadd151/teams/roles`.
- Console events captured: `1`; network events captured: `31`.

## State Matrix

| State | Candidate | Overlay | Pixel diff | Changed px | Changed ratio |
| --- | --- | --- | --- | ---: | ---: |
| roles-list | docs/acceptance/t1457/t1457-roles-list-candidate-1672x941.png | docs/acceptance/t1457/t1457-roles-list-canonical-overlay.png | docs/acceptance/t1457/t1457-roles-list-canonical-pixel-diff.png | 1488233 | 0.945900 |
| ram-role-detail | docs/acceptance/t1457/t1457-ram-role-detail-candidate-1672x941.png | docs/acceptance/t1457/t1457-ram-role-detail-canonical-overlay.png | docs/acceptance/t1457/t1457-ram-role-detail-canonical-pixel-diff.png | 1488233 | 0.945900 |
| mapping-drawer | docs/acceptance/t1457/t1457-mapping-drawer-candidate-1672x941.png | docs/acceptance/t1457/t1457-mapping-drawer-canonical-overlay.png | docs/acceptance/t1457/t1457-mapping-drawer-canonical-pixel-diff.png | 1519368 | 0.965689 |
| mapping-preview | docs/acceptance/t1457/t1457-mapping-preview-candidate-1672x941.png | docs/acceptance/t1457/t1457-mapping-preview-canonical-overlay.png | docs/acceptance/t1457/t1457-mapping-preview-canonical-pixel-diff.png | 1519579 | 0.965823 |
| mapping-cas-error | docs/acceptance/t1457/t1457-mapping-cas-error-candidate-1672x941.png | docs/acceptance/t1457/t1457-mapping-cas-error-canonical-overlay.png | docs/acceptance/t1457/t1457-mapping-cas-error-canonical-pixel-diff.png | 1520268 | 0.966261 |
| ram-role-create-drawer | docs/acceptance/t1457/t1457-ram-role-create-drawer-candidate-1672x941.png | docs/acceptance/t1457/t1457-ram-role-create-drawer-canonical-overlay.png | docs/acceptance/t1457/t1457-ram-role-create-drawer-canonical-pixel-diff.png | 1523247 | 0.968154 |
| ram-role-edit-drawer | docs/acceptance/t1457/t1457-ram-role-edit-drawer-candidate-1672x941.png | docs/acceptance/t1457/t1457-ram-role-edit-drawer-canonical-overlay.png | docs/acceptance/t1457/t1457-ram-role-edit-drawer-canonical-pixel-diff.png | 1520730 | 0.966554 |
| ram-role-duplicate-drawer | docs/acceptance/t1457/t1457-ram-role-duplicate-drawer-candidate-1672x941.png | docs/acceptance/t1457/t1457-ram-role-duplicate-drawer-canonical-overlay.png | docs/acceptance/t1457/t1457-ram-role-duplicate-drawer-canonical-pixel-diff.png | 1520796 | 0.966596 |
| ram-role-delete-safeguard | docs/acceptance/t1457/t1457-ram-role-delete-safeguard-candidate-1672x941.png | docs/acceptance/t1457/t1457-ram-role-delete-safeguard-canonical-overlay.png | docs/acceptance/t1457/t1457-ram-role-delete-safeguard-canonical-pixel-diff.png | 1565589 | 0.995066 |

## Browser/API Coverage

- Public signup API created a fresh owner session and organization.
- Public org APIs seeded AI runtime, three custom RAM Roles, Team Roles, and Team Role RAM mappings.
- Chromium verified role list, detail, create/edit/duplicate drawers, mapping preview/save path, CAS conflict, delete safeguard, version/error surfaces, console/network capture, and exact runtime SHA.
- Mapping CAS raw result: `409` `{"error":"version_conflict","message":"team: RAM role mapping version conflict"}
`.
- RAM Role CAS raw result: `409` `{"error":"version_conflict","message":"RAM role latest version changed"}
`.
- CRUD raw result: created `role-d88f5641c1526ba8`, edited to version `2`, deleted status `204`.

## Raw Evidence Files

- `docs/acceptance/t1457/capture-state.json`
- `docs/acceptance/t1457/t1457-console.log`
- `docs/acceptance/t1457/t1457-network.log`
