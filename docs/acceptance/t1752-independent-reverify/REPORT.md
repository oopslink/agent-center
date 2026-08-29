# T1752 Independent Reverify

Verdict: reject

Reviewed SHA: unavailable
Base SHA: unavailable

Summary: REJECT. The required self-contained `task-input/v1` package is absent from the workspace, so the unique immutable candidate SHA and expected base SHA could not be locked. Per the task contract, any INCOMPLETE condition is REJECT.

## Evidence Collected

- `raw/00-task-input-search.log`: `task-input/v1/README.md` and `task-input/v1/manifest.json` were not found.
- `raw/01-git-provenance.log`: current executor branch and HEAD provenance.
- `raw/02-fetch-and-ls-remote.log`: plain fetch failed because the repo has broad `+refs/*:refs/*`; safe fresh fetch with `--refmap=` succeeded; ls-remote was sampled.
- `raw/03-merge-base-and-clean.log`: current HEAD equals `origin/main`; this was not used as a reviewed candidate.
- `raw/04-gates-not-run.log`: required gates are incomplete because the candidate SHA is unavailable.

## Gate Result

All required gates are `incomplete`, not `pass`, because running them against an inferred SHA would make the evidence non-auditable:

- `go test ./...`
- broad race with explicit timeout
- focused terminal state-machine
- focused historical migration
- `tests/integration`
- focused HTTP/API

## Remediation And Retest Contract

1. Materialize `task-input/v1/README.md` and `task-input/v1/manifest.json` in the executor workspace with the unique immutable candidate exact SHA and expected base SHA.
2. From a fresh fetch, verify the candidate SHA using `git ls-remote` against the immutable ref and verify `merge-base` equals the expected base SHA.
3. Create or reset only an isolated clean detached worktree at the locked candidate SHA.
4. Run and persist raw output for `go test ./...`, a bounded broad race gate with explicit timeout over all related packages, focused terminal state-machine tests, focused historical migration tests, `tests/integration`, and focused HTTP/API tests.
5. Set `verdict=pass` only if every required gate exits green and no gate is incomplete; otherwise keep `verdict=reject` with the failing raw logs.
