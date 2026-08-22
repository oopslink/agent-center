# T1457 Team Roles / RAM Role Mapping Evidence

- Fresh base SHA verified before work: `c9b462d0b2da57896753d8f2dc142d783d138210`.
- Canonical mockup attachment recorded: `ac://files/01M0HRMZEV7XS8A3MNGG64ZZW1`.
- Canonical mockup SHA256 recorded: `80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56`.
- Local isolated verification instance: `http://127.0.0.1:5179/organizations/acme/teams/roles`.

## Screenshots

All screenshots are `1280 x 720`.

- `teams-roles-main.png` — Teams / Platform Team / Roles / Developer IA, role list, work config, RAM mapping table, safeguards/audit section.
- `teams-roles-mapping-drawer.png` — Team Role RAM mapping edit drawer with work config, CAS version, immediate impact panel, preview/apply controls.
- `teams-roles-ram-role-drawer.png` — RAM Role create drawer with permission picker, safeguard/audit panel, versioned write controls.
- `teams-roles-mapping-drawer-overlay-diff.png` — same-size overlay/diff against main state.
- `teams-roles-ram-role-drawer-overlay-diff.png` — same-size overlay/diff against main state.

## Verification

- `pnpm --dir web typecheck`
- `pnpm --dir web build`

Build completed with the existing CSS minifier warning at generated CSS line 3031; TypeScript and Vite build completed successfully.
