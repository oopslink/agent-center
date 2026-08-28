# T1510 Fresh Acceptance Report

Execution metadata:

- execution_id: exec-be668465
- canonical executor branch: ac-exec/task-10e532df/exec-be668465
- initial canonical HEAD: d0da255cd220bbdd4fe45787f2660401f1e18e48
- candidate: 2b531b9d5a81a7857b707ed425886b2d2ac96774
- baseline: 7611db9afb385b9622408577ab9e4c6de57aadb1
- acceptance worktree: /private/tmp/t1510-fresh-accept-2b531b9d-56111
- acceptance mode: detached candidate worktree; canonical executor branch was not switched
- task-input package: task-input/v1 was absent in this workspace; task contract came from the executor prompt
- recorded_at_utc: 2026-08-25T02:07:12Z

## Verdict

FAILED.

The candidate cannot be accepted because two hard gates failed:

1. Origin/main gate mismatch: candidate is behind=0/ahead=2 relative to origin/main, not the required behind=0/ahead=1.
2. Fresh build gate failed: `make build` exits 2 during `tsc -b`.

Because fresh build failed, fresh deployment and two new browser-session passes were not executable without violating the production-chain gate. They are recorded as blocked by build failure, not skipped as optional.

## Git Gates

- `git merge-base candidate baseline`: `7611db9afb385b9622408577ab9e4c6de57aadb1` PASS
- `git rev-list --left-right --count origin/main...candidate`: `0 2` FAIL, required `0 1`
- `git rev-parse origin/main`: `d0da255cd220bbdd4fe45787f2660401f1e18e48`
- `git rev-parse origin/ac-exec/task-3cc86539/f733c4cf-direct-binding-fix`: `2b531b9d5a81a7857b707ed425886b2d2ac96774`

## Fresh Validation Results

PASS:

- `go test ./internal/webconsole/api ./internal/authorization/... ./tests/integration`
  - exit code: 0
  - result: `internal/webconsole/api`, `internal/authorization`, and `tests/integration` passed
- `pnpm install --frozen-lockfile`
  - exit code: 0
- `pnpm test -- --run src/pages/Access.test.tsx`
  - exit code: 0
  - note: this invocation ran the full Vitest suite; 192 files / 1805 tests passed
- `pnpm exec vitest run src/pages/Access.test.tsx`
  - exit code: 0
  - result: 1 file / 23 tests passed

FAIL:

- `make build`
  - exit code: 2
  - failure:
    - `src/pages/Access.test.tsx(411,25): error TS2339: Property 'permission_keys' does not exist on type 'never'.`
    - `src/pages/Access.test.tsx(412,25): error TS2339: Property 'resources' does not exist on type 'never'.`
- `make lint-spa-tsc`
  - exit code: 2
  - same TypeScript failure as `make build`

BLOCKED BY BUILD FAILURE:

- fresh deployed-binary smoke
- fresh deployment
- two fresh browser sessions

## Raw Evidence

Raw logs are committed under `docs/acceptance/t1510-fresh-2b531b9d/raw/`.

- `candidate-status.txt`
- `go-targeted.log`
- `make-build.log`
- `make-lint-spa-tsc.log`
- `web-access-vitest.log`
- `web-access-vitest-targeted.log`
- `web-pnpm-install.log`
