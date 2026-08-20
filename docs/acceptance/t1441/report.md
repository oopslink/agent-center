# T1441 Independent Security / Migration / Authz Acceptance

## Verdict

**REJECT**

Bound candidate: `origin/main` = `dec77bc7d8724dde3fd2f6162fe5839dc85a9540`.
Branch: `review/t1441-independent-authz-acceptance`.

This is an evidence-only acceptance run. I did not change business code.

## Gate Matrix

| Gate | Result | Evidence |
|---|---:|---|
| Fresh `origin/main` SHA binding | PASS | `logs/00-environment.txt` records `origin/main` and `HEAD` both at `dec77bc7d8724dde3fd2f6162fe5839dc85a9540`. |
| Dual Profile production residue | PASS with whitelist | `logs/30-static-profile-residue.log`: no active Access Profile UI/API production code surfaced; remaining hits are migration/down SQL, retired import guards, tests, and design docs. AI Runtime retired fields are explicitly rejected by handler/tests. |
| Migration / rollback / permission equivalence | PASS | `logs/10-go-targeted.log`, `logs/20-go-full.log`, `logs/32-test-inventory-authz.log`: migration 0126/0129/0137 and T1343/T1413/T1416/T1438 suites present and green in normal runs. |
| Immediate revoke / fail closed | PASS | `logs/32-test-inventory-authz.log` includes `TestT1410TeamRoleRAMEffectiveScopesCacheAndImmediateRevoke`, `TestT1412EnforceIncludesTeamRoleRAMAndRevokesImmediately`, web/MCP revoke-deny tests; targeted packages pass in `logs/10-go-targeted.log`. |
| Multi-project scope | PASS | Covered by `TestT1410TeamRoleRAMEffectiveScopesCacheAndImmediateRevoke` and Team RAM preview/project tests listed in `logs/32-test-inventory-authz.log`; targeted and full Go runs pass. |
| Direct union | PASS | `TestT1410TeamRoleRAMEffectiveScopesCacheAndImmediateRevoke` validates team RAM + direct binding union; `TestAccessEffectiveBatchAndRevokeContract` covers effective source surfacing. |
| Concurrent CAS / idempotent audit | PASS | `TestRAMRoleMapping_ReplaceIsAtomicCASAndAudited`, `TestService_RevokeAssignmentConcurrentRaceIdempotent`, `TestService_RevokeAssignmentSameOrgValidAndIdempotent`, and `TestService_RevokePreviewConfirmStrongCAS` are listed and pass in targeted/full Go logs. |
| Cache fail closed | PASS | Team RAM cache and revoke behavior covered by T1410/T1412/T1438 tests in `logs/32-test-inventory-authz.log`; normal runs pass. |
| MCP / HTTP / UI / background consistency | PARTIAL PASS | Targeted Go covers MCP/admin HTTP/web/background entrypoints and full Vitest passes. Full Playwright e2e fails, so this cannot be signed off end-to-end. |
| Unauthorized negative cases | PASS | Listed and passing: cross-org revoke fail-closed, non-member forbidden, missing bearer/unknown/revoked token, file/secret forbidden, project/team cross-org rejection. |
| Full Go / frontend / lint / build | PASS | `logs/20-go-full.log`: `go test ./...` pass. `logs/12-web-targeted-after-install.log`: Vitest full suite `191 files / 1784 tests` pass. `logs/40-make-lint.log` and `logs/41-make-build.log` pass. |
| Race gate | FAIL | `logs/21-test-race.log` and `logs/22-test-race-serial-rerun.log`: `make test-race` fails due fork/resource exhaustion in `internal/agentruntime/...`; focused authz race also fails by 10-minute timeout in `logs/23-authz-race-focused.log`. No `DATA RACE` report observed, but gate is not green. |
| Deployed-binary smoke | PASS | `logs/42-make-smoke.log`: v22 deployed pipeline Playwright spec pass and runtime-version Go smoke pass. |
| Full Playwright e2e | FAIL | `logs/43-make-e2e.log`: `3 passed`, `15 failed`. Dominant failures are unauthenticated `401` in old API/UI specs, missing legacy `projects` table direct seed, and one old admin route `404`. |

## Commands Run

```bash
git fetch origin refs/heads/main:refs/remotes/origin/main
git checkout -B review/t1441-independent-authz-acceptance origin/main
go test ./internal/persistence ./internal/authorization ./internal/team/service ./internal/webconsole/api ./internal/admin/api ./internal/mcphost ./internal/cli ./internal/workerdaemon
(cd web && pnpm install --frozen-lockfile)
(cd web && pnpm test -- --run src/pages/Access.test.tsx src/pages/TeamDetail.test.tsx src/pages/AiRuntime.test.tsx)
go test ./...
make test-race
GOMAXPROCS=2 GOFLAGS=-p=1 make test-race
go test -race -count=10 ./internal/authorization ./internal/team/service ./internal/webconsole/api ./internal/admin/api ./internal/workerdaemon
make lint
make build
make smoke
make e2e
```

## Failure Details

1. `make test-race` is not green.
   - First run failed in `internal/agentruntime/reporepo` with repeated `fork/exec /usr/bin/git: resource temporarily unavailable`.
   - Serial rerun failed in `internal/agentruntime` with repeated `fork/exec /usr/bin/true`, `/bin/sh`, and `/usr/bin/git` resource errors.
   - Focused authz race reached 10-minute timeouts in `internal/webconsole/api` and `internal/admin/api`; `internal/workerdaemon` passed.

2. `make e2e` is not green.
   - Summary: `15 failed`, `3 passed`.
   - Representative failures:
     - multiple old specs hit `401 unauthenticated` / signup redirect instead of authenticated UI/API state;
     - `respond-input-request` seeds old `projects` table and fails with `no such table: projects`;
     - `v23-real-agent-pipeline` expects an old admin project add route and gets `404 page not found`.

## Evidence Inventory

Raw logs are under `docs/acceptance/t1441/logs/`.

| Layer | Result | Entrypoint |
|---|---:|---|
| Unit / integration Go | PASS | `go test ./...` |
| Focused authz integration | PASS | targeted Go packages in `logs/10-go-targeted.log` |
| Frontend unit/component | PASS | Vitest full suite in `logs/12-web-targeted-after-install.log` |
| Lint/type/build | PASS | `make lint`, `make build` |
| Deployed-binary smoke | PASS | `make smoke` |
| Full Playwright e2e | FAIL | `make e2e` |
| Race | FAIL | `make test-race`; focused authz race timeout |
