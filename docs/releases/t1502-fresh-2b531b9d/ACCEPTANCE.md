# T1502 Fresh Candidate Acceptance Report

Candidate: `2b531b9d5a81a7857b707ed425886b2d2ac96774`

Source branch: `ac-exec/task-3cc86539/f733c4cf-direct-binding-fix`

Verdict: **REJECT**

Reason: the RAM Role/direct-grant backend safety and migration equivalence checks pass, but the candidate does not pass the production frontend build gate. A fresh test-instance consequently serves `503 SPA not built`, so the requested fresh deployment evidence and two rounds of real browser acceptance for the actual Access UI cannot be completed for this SHA.

## Fresh Checkout

Evidence: `evidence/git-fresh-check.txt`

- `HEAD=2b531b9d5a81a7857b707ed425886b2d2ac96774`
- `merge_base=7611db9afb385b9622408577ab9e4c6de57aadb1`
- `behind_ahead=0 1`
- `task-input/v1` was not present in this workspace; no agent-center/MCP fallback was used.

## Safety And Migration Checks

Passing evidence:

- `evidence/go-persistence-migration.txt`
  - `TestMigration_0143_RAMRoleClassificationContract`
  - `TestMigration0144FailsClosedWhenRoleAccessReferencedByTeamRole`
  - `TestMigration0144FailsClosedForNonSinglePermissionRoleAccess`
  - `TestMigration0144DirectCarrierEquivalenceIdempotentAndRollback`
  - migration snapshot preserves permission count, live/revoked assignments, expiry, wrong-scope rows, expired active rows, and deny precedence before/after/up-replay/down.
- `evidence/go-authorization-direct-grant.txt`
  - direct grant creates `managed/internal`, not visible `custom/reusable`.
  - effective permission honors expiry, explicit deny, revoke, and replay/idempotency.
- `evidence/go-webconsole-access.txt`
  - managed/internal RAM Role detail returns 404.
  - system RAM Role edit is rejected.
  - reserved `Access grant...` reusable names are rejected.
  - overview hides expired direct grants, preserves explicit deny precedence, and surfaces resolver failures fail-closed.
  - batch apply returns real item details without phantom `unknown` rows.
- `evidence/web-access-vitest.txt`
  - `web/src/pages/Access.test.tsx`: 23 tests passed.

## Blocking Evidence

Production build:

- Command: `cd web && npm run build`
- Evidence: `evidence/web-build-failure.txt`
- Failure:
  - `src/pages/Access.test.tsx(411,25): error TS2339: Property 'permission_keys' does not exist on type 'never'.`
  - `src/pages/Access.test.tsx(412,25): error TS2339: Property 'resources' does not exist on type 'never'.`

Fresh deployment:

- Test instance started and `/api/health` returned `200`.
- Root web URL returned `503 Service Unavailable` with `SPA not built`.
- Evidence:
  - `evidence/fresh-test-instance-http.txt`
  - `evidence/browser-round1-spa-not-built.png`
  - `evidence/browser-round1-snapshot.txt`
  - `evidence/browser-round2-health.png`
  - `evidence/browser-round2-health-snapshot.txt`

## Conclusion

The backend security and migration properties requested by T1502 are supported by targeted tests on `2b531b9d`, but this SHA is not releasable and not acceptable as the final candidate because `tsc -b` fails and the fresh browser-deployable SPA is absent.
