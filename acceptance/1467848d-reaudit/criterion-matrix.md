# Acceptance Criterion Matrix

Reviewed SHA: `1467848d08a43395b2740d921836727d989aafff`

| Criterion | Evidence | Result | Notes |
|---|---|---:|---|
| Checkout / binary / Web provenance targets reviewed SHA exactly | `manifest.txt` | PASS | HEAD matched `1467848d08a43395b2740d921836727d989aafff`. No passing binary provenance was possible because build failed. |
| Cross-org / invisible resource 404 and explain non-leakage | `raw/go-focused-auth-team-migration.log`, `raw/go-focused-projection-graph-preview.log` | PASS | Relevant tests include cross-org authorization/resource concealment, API cross-org 404, transfer/file opacity, and no raw activity JSON leak. |
| Preview replay / expiry / CAS / idempotency / concurrency | `raw/go-focused-auth-team-migration.log`, `raw/go-focused-projection-graph-preview.log` | PASS | Relevant tests include revoke preview strong CAS/expiry, batch preview/apply consistency, idempotent concurrent apply, Team Memory CAS/concurrent promotion, control-event idempotency, and concurrency caps. |
| Delegation / least privilege / high-risk confirmation | `raw/go-focused-auth-team-migration.log` | PASS | Relevant tests include delegation edges, owner succession, high-risk assignment safety, custom role negative authorization, and Team Memory curator tags not granting access. |
| Graph pagination must not masquerade as full | `raw/go-focused-projection-graph-preview.log`, `raw/web-vitest-rerun.log` | PASS | Relevant tests include cursor pagination/limit semantics and UI pagination/list controls. |
| Projection vs authorization.Service shadow parity | `raw/go-focused-auth-team-migration.log`, `raw/go-focused-projection-graph-preview.log` | PASS | Relevant tests include legacy access apply/effective state parity and custom-role overview parity; no unexplained allow observed in these logs. |
| Team templates cannot privilege-escalate | `raw/go-focused-auth-team-migration.log`, `raw/go-focused-projection-graph-preview.log` | PASS | Relevant tests include export curation gate, import uncurated defaults, template project-scope drop/scrub, and Team Memory authorization restrictions. |
| Old entry migration and rollback | `raw/go-focused-projection-graph-preview.log`, `raw/go-migration-fail-rerun-pipefail.log`, `raw/go-migration-fail-rerun-pipefail.exit` | FAIL | Migration tests fail reproducibly: post-upgrade schema version is `132`, expected `129`, in `TestFollowState_CrossVersionUpgrade_NonEmptyDB` and `TestUpgradeAcc_Migrate0057_PreservesLegacyMessagesAsRoots`. Exit code 1. |
| Product build | `raw/make-build-pipefail.log`, `raw/make-build-pipefail.exit` | FAIL | `make build` fails in `web` TypeScript compile: unused `failed` in `src/mocks/handlers.ts` and `permissions` typed as `string[] | undefined` in `src/pages/Access.test.tsx`. Exit code 2. |
| Web unit/integration tests | `raw/web-vitest.log`, `raw/web-pnpm-install.log`, `raw/web-vitest-rerun.log` | PASS | Initial run failed due missing `node_modules`; after `pnpm install`, Vitest passed: 191 files, 1772 tests. |
| Real Web E2E | `raw/e2e-playwright.log`, `raw/e2e-smoke-pipefail.log`, `raw/e2e-smoke-pipefail.exit`, `screenshots/`, `error-context/` | FAIL | Full Playwright run: 18/18 failed. Smoke rerun: 2/2 failed. Root cause: `spawn .../bin/agent-center ENOENT`, caused by failed product build. Screenshots/error contexts preserved. |

Overall verdict: REJECT, `blocking=true`.
