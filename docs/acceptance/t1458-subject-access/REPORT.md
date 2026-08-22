# T1458 Subject Access Remediation Evidence

Base: `origin/main` at `ddba9b10816b803b0563e97de574ebe7378c8ef2`.
Workspace remediation includes Subject access IA, responsive overflow fixes, and authorization conflict/fail-closed fixes.

## Implemented Surface

- Subject access renders as a subject workbench with roster, selected subject detail, metrics, why-access, explicit deny/N/A, direct union, trace, audit, and full decisions.
- Permission catalog remains under Roles & mappings, not Subject access.
- 1280x720 detail/KPI/N/A clipping was addressed by keeping the secondary access aside below `2xl`, giving the workbench full content width at 1280, and wrapping long monospace evidence/role strings.
- Backend `/access/overview` now discovers direct-binding resources from active assignments and routes effective rows through the unified resolver so expiry and fail-closed behavior match production authorization.
- Explicit deny decisions take precedence over direct/custom allow rows for the same subject/permission/resource.
- Resolver failures surface as denied `access.resolve` rows.
- Duplicate direct grants fail before mutation with HTTP 409 and visible `code`/`reason` fields (`direct_grant_conflict`).

## Verification

- `go test ./internal/webconsole/api -run 'TestAccessOverview(ShowsTeamRAMAndDirectBindingUnion|DirectBindingUsesResolverExpiryFailClosed|ExplicitDenyPrecedesDirectBinding|ResolverErrorsSurfaceFailClosedRows)|TestAccessEffectiveBatchAndRevokeContract|TestAccessRAMRoles'`
- `go test ./internal/authorization`
- `pnpm --dir web exec tsc --noEmit`
- `pnpm --dir web test -- src/pages/Access.test.tsx` expanded by the repo script to the full suite: 191 files / 1791 tests passed.
- `AC_WEB_URL=http://127.0.0.1:5173 node docs/acceptance/t1458-subject-access/capture-subject-access.mjs`

## Runtime Evidence

`capture-state.json` records:

- `console_errors: []`
- `request_failed: []`
- `http_errors: []`
- expected visible conflict: one `409` from `/api/orgs/test/access/batch/apply`
- 1280x720 document overflow: `clientWidth=1280`, `scrollWidth=1280`, `overflowX=false`
- key 1280 element overflow: `page-Access=false`, `access-subject-view=false`, `access-subject-detail=false`, `access-subject-metrics=false`, `access-decision-table=false`, `access-permission-trace=false`, `access-direct-binding-union=false`

All screenshots below were captured fresh from this working tree with a `1684x934` viewport. `sips -g pixelWidth -g pixelHeight docs/acceptance/t1458-subject-access/*.png` confirms every PNG is `1684x934`.

- `01-subject-workbench.png` sha256 `f1e79e41fe39513c341658c4d0db4825e31ee8cf179098143aa66008ff26d0fc`
- `02-filter-agent-detail.png` sha256 `a39b78ba0fee050959af2c6d85376c778dfe4a8b4c0801e5909b9dad0d6bfb84`
- `03-add-binding-drawer.png` sha256 `a0cc106ea7fe9c6b166900717d12941b0a0b576529ecd3a461de327f198b8996`
- `04-impact-preview.png` sha256 `1dd932556c84edd99951cf64555d3296d42e84afcf79a5b090defea17bc06ef3`
- `05-grant-success.png` sha256 `94bd11c066b0345fd88611009999f48a9d0456b3b0e6283bf6eadc6f8fe92b3e`
- `06-revoke-preview.png` sha256 `2d095e80af6d865fa7cb4d657a7e10dc0f42bcc968940cead9703bdf583b899d`
- `07-revoke-success.png` sha256 `1e5f4159795ba91720108f4d150d70006a86e782cc9db45248c487f6fb09c6a7`
- `08-forbidden-403.png` sha256 `d1fbf46d3b3af49ec560910ff0aa9ff90d671902caf0d510ebc31e24af38a24b`
- `09-conflict-409.png` sha256 `4680ac7d9ba7f5dafd2c188e6522ca9cd836adce3e2995ae14bf649324a069b8`
- `capture-state.json` sha256 `90580f5ef98376dfc11216408dc2c2faee604aed757c945d02d7992dceb9491a`
- `capture-subject-access.mjs` sha256 `ef800ecbcbbae230b073943aa2953826d1ad086fd3a1d1246f2d9751931718ff`

## Canonical Baseline Limitation

The required canonical artifact with SHA256 `9d903c702ac15c6cbb08e4b46f58c60fee40b9ef22924628ffc9daf606f1ce3` is not present in this workspace or in the fetched `origin/remediation/t1458-subject-access` evidence bundle. The fetched bundle contains T1458 screenshots, but none match that hash. Therefore no overlay/diff/pixel-stat artifact against the exact canonical baseline is claimed here; producing one would be fabricated evidence.
