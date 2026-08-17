# Criterion Matrix

reviewed_sha: 1467848d08a43395b2740d921836727d989aafff
executor_branch: ac-exec/task-c109f83f/exec-522d9363
completed_at_utc: 2026-08-17T21:03:17Z

| Criterion | Evidence | Result |
| --- | --- | --- |
| Branch identity preserved | `branch-pin.txt`; post-pin branch stayed `ac-exec/task-c109f83f/exec-522d9363`; HEAD pinned to reviewed SHA. | PASS |
| Candidate tree under review | `branch-pin.txt`; `git rev-parse HEAD` was `1467848d08a43395b2740d921836727d989aafff` before tests. | PASS |
| Dependency install reproducible | `logs/01-web-pnpm-install.log`, `logs/02-e2e-pnpm-install.log`; both lockfile installs succeeded. | PASS |
| Focused auth consistency: SPA Access profile CAS behavior | `logs/03-web-access-vitest.log`; `src/pages/Access.test.tsx` passed 5 tests. | PASS |
| SPA product build/typecheck | `logs/04-web-build.log`; `tsc -b` failed. Blocking errors include `Access.test.tsx(145,45)` / `(145,64)` because `publishBody.permissions` is `string[] | undefined`, plus `src/mocks/handlers.ts(743,13)` unused `failed`. | FAIL |
| Backend access authorization/profile version contract | `logs/05-go-webconsole-access.log`; `go test ./internal/webconsole/api -run 'TestAccess' -count=1` passed. | PASS |
| Core persistence migration acceptance | `logs/06-go-migration-core.log`; `go test ./internal/persistence ./tests/integration -count=1` passed. | PASS |
| Conversation migration acceptance | `logs/07-go-conversation-sqlite-migration.log`; `TestUpgradeAcc_Migrate0057_PreservesLegacyMessagesAsRoots` failed with post-upgrade version `132`, expected `129`. | FAIL |
| Team migration/auth consistency | `logs/08-go-auth-team-migration.log`; team service and webconsole add-member/access tests passed. | PASS |
| Real deployed product E2E smoke | `logs/09-deployed-smoke.log`; `./scripts/smoke/deploy-smoke.sh` failed at build before server/browser startup because SPA build failed. | FAIL |
| Screenshots captured | No screenshots produced because deployed smoke failed during build before Playwright/browser execution. | NOT AVAILABLE |
| Server output captured | `server-output/no-server-started.txt`; no server process was started because deployed smoke failed during build. | NOT AVAILABLE |

Verdict: REJECT, blocking=true.
