# T1457 Team Roles / RAM Role Mapping Evidence

- Fresh base SHA verified before work: `c9b462d0b2da57896753d8f2dc142d783d138210`.
- Canonical mockup attachment recorded: `ac://files/01M0HRMZEV7XS8A3MNGG64ZZW1`.
- Canonical mockup SHA256 recorded: `80e51bb4aa74d5a437b6c35b84b5fda1906c7bb7e08bd0e2335c14bb4d1a7d56`.
- Canonical mockup file used for this remediation: `/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/agents/01KV01ZG5T332EYTFCVTNAZB9B/tasks/t1457-canonical.png` (`1672 x 941`).
- Remediation change: the Access module secondary navigation entry `Team Role mappings` now routes to Team IA `/organizations/:slug/teams/roles`, not the Access aggregate query page. Team IA Roles navigation is also unit-locked.
- Fresh verification instance: an isolated `bin/agent-center` server was launched by `capture-t1457.mjs`, signed up through the public auth API, seeded through public org-scoped Web API endpoints, then captured in Chromium at `1672 x 941`. The instance was intentionally torn down after capture.
- Stable external preview: blocked in this executor. No repository/environment Sites or shared preview mechanism is available here, and this isolated worker has no deploy credentials. No durable non-`127.0.0.1` URL is claimed.

## Screenshots

Fresh exact-size screenshots are `1672 x 941`.

- `teams-roles-main-1672x941.png` — Teams / Platform Team / Roles / Developer IA, role list, work config, RAM mapping table, safeguards/audit section.
- `teams-roles-mapping-drawer-1672x941.png` — Team Role RAM mapping edit drawer with work config, CAS version, immediate impact panel, preview/apply controls.
- `teams-roles-ram-role-drawer-1672x941.png` — RAM Role create drawer with permission picker, safeguard/audit panel, versioned write controls.
- `teams-roles-main-canonical-overlay.png` — true same-size overlay between final candidate main state and canonical mockup.
- `teams-roles-main-canonical-pixel-diff.png` — true same-size pixel diff between final candidate main state and canonical mockup.
- `teams-roles-main-canonical-diff-stats.json` — machine-readable canonical diff stats.
- `capture-state.json` — captured URL, width guard, seeded Team/RAM Role ids, and checked state list.
- Legacy `1280 x 720` screenshots from the prior pass remain in this directory for continuity and were not deleted.

## Canonical Diff

Candidate: `docs/acceptance/t1457/teams-roles-main-1672x941.png`

Canonical: `/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/agents/01KV01ZG5T332EYTFCVTNAZB9B/tasks/t1457-canonical.png`

- Same-size canvas: `1672 x 941`, `1,573,352` pixels.
- Changed pixels: `1,488,215`.
- Changed ratio: `0.9458881420`.
- MAE per RGB channel: `12.7330673619`.
- RMSE per RGB channel: `38.0095613481`.
- Max absolute channel delta: `255`.

The changed-pixel ratio is high; this records a real canonical-vs-candidate delta, not a state-to-state substitute overlay.

## State Coverage

`capture-t1457.mjs` checks these states on the fresh binary instance:

- Team IA route `/teams/roles`.
- Role list and role detail entry.
- Work config values.
- RAM Role mapping table.
- Mapping edit drawer.
- Immediate impact / CAS version.
- RAM Role create drawer.
- Versioned write controls.
- Delete safeguard for referenced RAM Roles.
- Audit copy.
- Width guard: `clientWidth=1672`, `scrollWidth=1672`.

## Verification

- `pnpm --dir web install --frozen-lockfile`
- `pnpm --dir tests/e2e/v2 install --frozen-lockfile`
- `pnpm --dir web test -- App.test.tsx TeamUISecondaryNav.test.tsx Access.test.tsx`
  - Vitest argument forwarding ran the full frontend suite: `191` test files, `1791` tests passed.
- `pnpm --dir web typecheck`
- `pnpm --dir web build`
- `make build-backend`
- `node docs/acceptance/t1457/capture-t1457.mjs`
- Python/Pillow canonical overlay and pixel diff generation.

Build completed with the existing CSS minifier warning at generated CSS line 3031; TypeScript, Vite build, backend build, full frontend tests, and fresh capture completed successfully.

## Repository Constraints

- This executor could not use `get_team_rule` because agent-center/MCP access is unavailable and the task explicitly forbids using center tools, raw center endpoints, database files, sockets, worker tokens, or equivalent fallbacks.
- The starting branch had `HEAD=41a2f7e631760d617dc0513af2ee5ba777b75aa7` equal to `origin/main`; `bd5a8a0bf34af170e4b896631311a718ce6189ea` was not an ancestor of that HEAD (`git merge-base --is-ancestor` returned 1). History was not rewritten in this isolated executor.
