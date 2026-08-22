# T1458 Subject Access Remediation Evidence

Base: `41a2f7e631760d617dc0513af2ee5ba777b75aa7`
Ancestry: `c9b462d0b2da57896753d8f2dc142d783d138210` is an ancestor of the base.
Recovery reviewed: `7444e6a9b729d75eab531ba4fa9f35bcb121d4df`.

## Implemented Surface

- Subject access now renders as a subject workbench, not a permission catalog card.
- Subject roster supports filtered API results and pagination.
- Selected subject detail includes metrics, why access, direct binding union, explicit deny/N/A, summary/activity, trace, audit, and the full decision table.
- Subject view no longer renders the permission catalog; catalog remains in Roles & mappings.
- Backend `/access/overview` discovers direct-binding resource scopes from `authorization_role_assignments`, then uses the unified authorization resolver for effective decisions so expiry and fail-closed behavior are shared with production authz.
- Explicit denied decisions now take precedence over direct/custom allow rows for the same subject/permission/resource, and resolver failures surface as denied `access.resolve` rows instead of being silently dropped.

## Verification

- `go test ./internal/webconsole/api -run 'TestAccessOverview(ShowsTeamRAMAndDirectBindingUnion|DirectBindingUsesResolverExpiryFailClosed|ExplicitDenyPrecedesDirectBinding|ResolverErrorsSurfaceFailClosedRows)|TestAccessEffectiveBatchAndRevokeContract|TestAccessRAMRoles'`
- `pnpm --dir web test -- src/pages/Access.test.tsx` (Vitest expanded to the full suite: 191 files / 1791 tests passed)
- `node docs/acceptance/t1458-subject-access/capture-subject-access.mjs`

## Screenshot Evidence

All screenshots were captured fresh from this working tree with a `1684x934` viewport. `sips -g pixelWidth -g pixelHeight docs/acceptance/t1458-subject-access/*.png` confirms every PNG is `1684x934`.

- `01-subject-workbench.png` sha256 `6a9e25cd922ec686bd925c03e696c9a4e803556d56838c117a80d0f7f927f7e2`
- `02-filter-agent-detail.png` sha256 `aa2e919209f18f109d973b0424da59ade8d60195b6557cdb06f49c69fd830192`
- `03-add-binding-drawer.png` sha256 `a5f7685d47d8a2364e22e780f9501820537a8becf6215ae12d5929be5ec05cd3`
- `04-impact-preview.png` sha256 `ba020baa6858405847d1b11514c3c3c864716f99708c679eefa4b42de9888654`
- `05-grant-success.png` sha256 `c7d9b0f051f759c4dc51c3306d6d561f3e494d66074b6a8ec0c565e49435317a`
- `06-revoke-preview.png` sha256 `00fdeaafa74209dfc0dd54d12145f4f5d3673a544ddcb1a5ebfba99a79008c2d`
- `07-revoke-success.png` sha256 `3fef29ff7d305d3458eda9a19d4e72e4c4486fc82e9d450793cfbb8ae01293e4`
- `08-forbidden-403.png` sha256 `2b8396c2737ad1ebdda1db8d4e6ca0b71d31540f5c85c21f281197a227ee972e`
- `09-conflict-409.png` sha256 `fd0615c45b000692d45267235b531f3b78b5c69c8b6a8d608ee0d0d255834e22`

## Baseline Limitation

The required baseline attachment `ac://files/01M0HRMZFD0Q7NA194RQNYYVBW` is not accessible from this isolated executor workspace, and no local copy matching SHA256 `9d903c702ac15c6cbb08e4b46f58c60fee40b9ef22924628ffc9daf606f1ce3c` exists under the workspace. No overlay or pixel diff artifact is claimed in this report because producing one without the canonical baseline would be fabricated evidence.
