# T1458 Subject Access Remediation Evidence

Base: `41a2f7e631760d617dc0513af2ee5ba777b75aa7`
Ancestry: `c9b462d0b2da57896753d8f2dc142d783d138210` is an ancestor of the base.
Recovery reviewed: `7444e6a9b729d75eab531ba4fa9f35bcb121d4df`.

## Implemented Surface

- Subject access now renders as a subject workbench, not a permission catalog card.
- Subject roster supports filtered API results and pagination.
- Selected subject detail includes metrics, why access, direct binding union, explicit deny/N/A, summary/activity, trace, audit, and the full decision table.
- Subject view no longer renders the permission catalog; catalog remains in Roles & mappings.
- Backend `/access/overview` discovers direct-binding resource scopes from `authorization_role_assignments`, then uses the unified authorization resolver for all effective `custom_role` decisions so expiry and fail-closed behavior are shared with production authz.

## Verification

- `go test ./internal/webconsole/api -run 'TestAccess(EffectiveBatchAndRevokeContract|OverviewShowsTeamRAMAndDirectBindingUnion|OverviewDirectBindingUsesResolverExpiryFailClosed|RAMRolesPersistVersionsCASRevokeAndReferences)'`
- `go test ./...`
- `pnpm --dir web typecheck`
- `pnpm --dir web test -- Access.test.tsx` (Vitest ran the full suite: 191 files / 1791 tests passed)
- `node docs/acceptance/t1458-subject-access/capture-subject-access.mjs`

## Screenshot Evidence

All screenshots were captured fresh from this working tree with a `1672x941` viewport. `sips -g pixelWidth -g pixelHeight docs/acceptance/t1458-subject-access/*.png` confirms every PNG is `1672x941`.

- `01-subject-workbench.png` sha256 `034ae15e68082244dc4668c8cc459fd7f899710b9198df0c31b9132e33e7aa00`
- `02-filter-agent-detail.png` sha256 `a4084038ee096efef63b67cdfce207f005a80a1838300a28d4427d7c5bf6c3aa`
- `03-add-binding-drawer.png` sha256 `0c34d21b89e3895192a9e1f3d0549e2c1f4a36237da9c7ac401fd4153f61a423`
- `04-impact-preview.png` sha256 `74833d12eaf5f9f077fe8be8eb980a1f16e528971bda8796431b515c1d20a461`
- `05-grant-success.png` sha256 `5a5558cef270cb5aede4b528fd4b776d568c1a3531cadaa6a78e12b8c5125973`
- `06-revoke-preview.png` sha256 `3e57342a420f9bd32d5a31075cdcbff0e05f8ba4ec72781cb81edb50b95dfdb4`
- `07-revoke-success.png` sha256 `9b2f08465f7f451c13c870a1ee8d1f8f533e5e54b2b745166390e8c670a53807`
- `08-forbidden-403.png` sha256 `428040732cb22d54e9b7de6d77146d8d380dc377a0594f18e3b7a22a7785299b`
- `09-conflict-409.png` sha256 `8ab0977bbb39d816dd7b67cb25b3fbe412465bd3b2f87d10bfd57de9b91ffe5d`

## Baseline Limitation

The required baseline attachment `ac://files/01M0HRMZFD0Q7NA194RQNYYVBW` is not accessible from this isolated executor workspace, and no local copy matching SHA256 `9d903c702ac15c6cbb08e4b46f58c60fee40b9ef22924628ffc9daf606f1ce3c` exists under the workspace. No overlay or pixel diff artifact is claimed in this report because producing one without the canonical baseline would be fabricated evidence.
