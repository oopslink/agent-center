# RAM Roles canonical S3 acceptance report

Candidate source before this change: `7232c04df2bc902353107404d18b4bc3f5bd5712`.

## Scope and plan

| # | Type | Acceptance target | Exit condition |
|---|---|---|---|
| 1 | component | canonical list, toolbar, search/risk/scope, density, pagination, selected detail | filters and pagination change rendered rows; both density modes render |
| 2 | component | permission registry and summary | every permission shows key, label, description, risk, resources, and actions; writes select only registry entries |
| 3 | component | loading, empty, error, forbidden | each state has a distinct rendered UI and recovery/authority copy |
| 4 | component | create/edit/delete/CAS/reference migration | version-pinned writes, typed delete, blocked reference action, success/error/409 toast |
| 5 | deployed browser | 1672 and 1280, light and dark, all state evidence | real embedded SPA, authenticated session, one screenshot per state |
| 6 | build | production frontend and binary | `tsc -b`, Vite production build, and Go binary build pass |

## Result

All six plan items passed. The browser case starts the real `bin/agent-center`, signs up an owner, loads the embedded production SPA, and captures rendered UI. Domain responses are deterministic route fixtures for otherwise hard-to-reproduce loading/empty/error/409 states; they are not used as a substitute for the browser assertions.

| Layer | Result | Command / evidence |
|---|---|---|
| Unit/component | 16 pass | `pnpm exec vitest run src/pages/Access.test.tsx` |
| Integration with controlled service responses | 1 pass | `access-ram-role-visual.spec.ts` |
| Deployed-binary browser | 1 pass | real `bin/agent-center` fixture, Chromium/macOS |
| Production build | pass | `make build` |

## Width and theme coverage

1672 light and dark:

![1672 light](01-ready-1672-light.png)

![1672 dark](02-ready-1672-dark.png)

1280 light and dark, including compact density:

![1280 light](ready-1280-light.png)

![1280 dark](03-ready-1280-dark.png)

![1280 compact dark](04-compact-1280-dark.png)

## CRUD, CAS, and references

Referenced delete is blocked and exposes the read-only Team Role reference plus navigation/migration actions:

![Referenced delete blocked](05-referenced-delete-blocked-1280-dark.png)

Create and edit drawers:

![Create drawer](06-create-drawer-1280-dark.png)

![Edit drawer](08-edit-drawer-1280-dark.png)

Success and 409 conflict toasts:

![Create success](07-create-success-toast-1280-dark.png)

![CAS 409](09-cas-409-toast-1280-dark.png)

Typed deletion gate:

![Typed delete](10-typed-delete-1280-dark.png)

## Service states

Empty:

![Empty](11-empty-1672-light.png)

Error with retry:

![Error](12-error-1672-light.png)

Loading:

![Loading](13-loading-1672-light.png)

Forbidden with backend-authority explanation:

![Forbidden](14-forbidden-1672-light.png)

## Known constraints

- `task-input/v1/README.md` and `task-input/v1/manifest.json` were not present in this executor worktree, so no attachment hash could be independently re-read.
- The repository's later ADR-0059 branch proposes moving RAM Role editing under Team Roles, but this task explicitly freezes an independent canonical page; this candidate follows the task contract and current mainline route.
