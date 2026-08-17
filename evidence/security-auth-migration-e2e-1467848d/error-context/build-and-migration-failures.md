# Error Context

reviewed_sha: 1467848d08a43395b2740d921836727d989aafff

Blocking failures observed on the reviewed candidate tree:

1. SPA build/typecheck failure
   - Command: `pnpm --dir web run build`
   - Raw log: `../logs/04-web-build.log`
   - Failure:
     - `src/pages/Access.test.tsx(145,45)` and `(145,64)` assign `publishBody.permissions` into a profile version where the target type requires `string[]`.
     - `publishBody` is typed with optional `permissions?: string[]`, so TypeScript sees `string[] | undefined`.
     - `src/mocks/handlers.ts(743,13)` also fails `noUnusedLocals` for an unused `failed` binding.

2. Conversation SQLite migration acceptance failure
   - Command: `go test ./internal/conversation/sqlite -count=1`
   - Raw log: `../logs/07-go-conversation-sqlite-migration.log`
   - Failure:
     - `TestUpgradeAcc_Migrate0057_PreservesLegacyMessagesAsRoots`
     - `post-upgrade version = 132 want 129`

3. Deployed product smoke failure
   - Command: `./scripts/smoke/deploy-smoke.sh`
   - Raw log: `../logs/09-deployed-smoke.log`
   - Failure:
     - Failed at step `build` because the same SPA build/typecheck errors prevented `make build`.
     - No real server or Playwright browser session was reached.
