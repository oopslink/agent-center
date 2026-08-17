# T1370 Remediation Independent Acceptance Evidence

Verdict: REJECT

Review SHA: `241685f5776cd542572f4c32b767d41690f1f355`

Candidate remote ref: `refs/heads/fix/t1366-access-migration-smoke`

Origin main at review: `997470a51878726145796c39973028ea6813788d`

This review was rerun on the new unique remediation candidate SHA. No verdict from old `1467848d08a43395b2740d921836727d989aafff` was reused.

## Target Lock

- `git ls-remote origin refs/heads/fix/t1366-access-migration-smoke refs/heads/main` returned candidate `241685f5776cd542572f4c32b767d41690f1f355` and main `997470a51878726145796c39973028ea6813788d`.
- `git merge-base 997470a51878726145796c39973028ea6813788d 241685f5776cd542572f4c32b767d41690f1f355` returned `997470a51878726145796c39973028ea6813788d`.
- `git merge-base --is-ancestor 997470a51878726145796c39973028ea6813788d 241685f5776cd542572f4c32b767d41690f1f355` exited `0`.
- Initial worktree was clean before evidence capture; only this evidence directory was added afterward.

## Command Results

| Gate | Command | Exit | Evidence |
| --- | --- | ---: | --- |
| Remote SHA, ancestry, diff lock | see lineage log | 0 | `logs/00-lineage.log` |
| SPA dependencies | `pnpm --dir web install --frozen-lockfile` | 0 | `logs/10-web-pnpm-install.log` |
| SPA typecheck | `pnpm --dir web run typecheck` | 0 | `logs/11-web-typecheck.log` |
| SPA production build | `pnpm --dir web run build` | 0 | `logs/12-web-build.log` |
| Access/team SPA tests | `pnpm --dir web exec vitest run src/pages/Access.test.tsx src/pages/TeamDetail.test.tsx src/pages/Teams.test.tsx` | 0 | `logs/20-web-access-team-tests.log` |
| Access CAS/auth/profile/team backend | `go test ./internal/authorization ./internal/webconsole/api ./internal/team/... -count=1` | 0 | `logs/21-go-access-auth-team.log` |
| Conversation/core/team SQLite migration | `go test ./internal/persistence ./internal/conversation/... ./internal/cli -run ... -count=1` | 0 | `logs/22-go-conversation-migration.log` |
| Full Go regression | `make test` | 2 | `logs/30-make-test.log` |
| Minimal repro for full-test red item | `go test ./internal/workerdaemon -run '^TestSupervisorSession_DetachSurvives$' -count=1 -v` | 0 | `logs/31-repro-workerdaemon-detach.log` |
| Deployed smoke | `make smoke` | 0 | `logs/40-make-smoke.log` |

## Blocking

- Full repository Go regression failed: `make test` exited `2`.
- Failing test: `github.com/oopslink/agent-center/internal/workerdaemon TestSupervisorSession_DetachSurvives`.
- Failure output: `supervisor not reattachable within 45s + 45s generous dial: state=1 reason=dead pidAlive=true (generous dial absorbs load starvation, so this is a real break/death - NOT a starved-out probe)`.
- Minimal reproduction command was executed separately and passed once, so the observed red item is not a stable single-test reproduction, but it is still a real full-gate failure and blocks PASS under evidence-before-verdict.

## Structured Verdict

```json
{
  "verdict": "REJECT",
  "review_sha": "241685f5776cd542572f4c32b767d41690f1f355",
  "candidate_ref": "refs/heads/fix/t1366-access-migration-smoke",
  "origin_main_sha": "997470a51878726145796c39973028ea6813788d",
  "old_sha_reused": false,
  "blocking": [
    {
      "gate": "make test",
      "exit_code": 2,
      "package": "github.com/oopslink/agent-center/internal/workerdaemon",
      "test": "TestSupervisorSession_DetachSurvives",
      "minimal_repro": "go test ./internal/workerdaemon -run '^TestSupervisorSession_DetachSurvives$' -count=1 -v",
      "minimal_repro_exit_code": 0
    }
  ],
  "passed": [
    "remote candidate SHA lock",
    "origin/main ancestry",
    "SPA install",
    "SPA typecheck",
    "SPA build",
    "Access/team SPA tests",
    "Access CAS/auth/profile/team backend tests",
    "conversation/core/team SQLite migration tests",
    "deployed smoke"
  ]
}
```
