# T1515 Fresh Build Acceptance

Verdict: PASS

Validated candidate `origin/ac-exec/T1514-access-test-ts2339` at exact SHA `af936471ed2d1a2f011159ef5cb284345346bb48` in detached worktree `/tmp/t1515-fresh-worktree.QfQEmA/candidate`.

## Remote State

- `origin/main`: `1a8a55e9e9b862c15c05300689c1b08cd328df7d`
- Expected main: `1a8a55e9e9b862c15c05300689c1b08cd328df7d`
- Main drift: no
- Merge-base: `1a8a55e9e9b862c15c05300689c1b08cd328df7d`
- Behind/ahead from `origin/main...candidate`: `0	1`

## Diff Scope

Changed file:

- `web/src/pages/Access.test.tsx`

Diff stat: `1 file changed, 6 insertions(+), 4 deletions(-)`.

Manual assertion review: no assertions were removed or weakened. The existing `permission_keys` and `resources` exact equality assertions remain. The fix only adds a concrete `PreviewRequestBody` type and casts the captured request body before those assertions to resolve the TypeScript narrowing issue.

## Commands

| Command | CWD | Exit |
| --- | --- | ---: |
| `git fetch --prune origin '+refs/heads/main:refs/remotes/origin/main' '+refs/heads/ac-exec/T1514-access-test-ts2339:refs/remotes/origin/ac-exec/T1514-access-test-ts2339'` | executor worktree | 0 |
| `git worktree add --detach /tmp/t1515-fresh-worktree.QfQEmA/candidate af936471ed2d1a2f011159ef5cb284345346bb48` | executor worktree | 0 |
| `pnpm install --frozen-lockfile` | candidate root | 1 |
| `pnpm install --frozen-lockfile` | candidate `web/` | 0 |
| `make build` | candidate root | 0 |
| `pnpm exec vitest run src/pages/Access.test.tsx` | candidate `web/` | 0 |
| `git diff --check origin/main..HEAD` | candidate root | 0 |
| `make lint-spa-tsc` | candidate root | 0 |

Note: the root `pnpm install --frozen-lockfile` failed because this repository has no root `package.json`; the frontend package manifest is under `web/`. The successful frozen install from `web/` is the relevant package install evidence.

## Key Results

- `make build`: passed.
- Targeted Access Vitest: `src/pages/Access.test.tsx` passed with 23 tests.
- `git diff --check origin/main..HEAD`: passed.
- `make lint-spa-tsc`: passed.
- Final detached candidate status: clean, `## HEAD (no branch)`.

## Task Input Package

The requested `task-input/v1/README.md` and `task-input/v1/manifest.json` were not present at workspace root. A parent-directory search found no matching task-input files. Evidence is in `raw-logs/10-task-input-search.log`.

## Raw Logs

- `raw-logs/00-worktree-provenance.log`
- `raw-logs/01-pnpm-install-frozen-lockfile.log`
- `raw-logs/02-web-pnpm-install-frozen-lockfile.log`
- `raw-logs/03-make-build.log`
- `raw-logs/04-vitest-access.log`
- `raw-logs/05-git-diff-check.log`
- `raw-logs/06-lint-spa-tsc.log`
- `raw-logs/07-final-candidate-state.log`
- `raw-logs/08-remote-state.log`
- `raw-logs/09-candidate-diff-access-test.log`
- `raw-logs/10-task-input-search.log`
- `raw-logs/11-fetch-remote-refs.log`
- `raw-logs/12-worktree-list.log`
