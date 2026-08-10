# T1314 AI Runtime Profile Retirement Final Acceptance

Date: 2026-08-10

## Verdict

PASS. The fixed integration candidate was independently verified at
`c61f983555e9e50257db866bb055d38e2e448cfc`.

## Integration and ancestry

The fetched remote ref `origin/integration/t1315-runtime-delivery-candidate`
resolved exactly to the SHA above. The required upstream deliveries are all
ancestors of that candidate:

- `9345409d` — AI Runtime Profile removal
- `eb4356b2` — Agent runtime selector/config integration
- `6fa3260f` — Team role runtime selector migration
- `51d67182` — CLI/model entry audit evidence

## Acceptance matrix

| Gate | Result | Evidence |
| --- | --- | --- |
| Retired Profile UI | PASS | Deployed-browser test found no Profile tab/create/edit controls for owner or member. |
| Retired Profile URL/API | PASS | Backend HTTP tests assert GET/POST/PATCH `/api/ai-runtime/profiles...` and PUT `/api/ai-runtime/default-profile` return 404; deployed browser confirms canonical `/ai-runtime` redirect and the surviving catalog page. |
| Schema and migration | PASS | Migration 0126 removes `ai_runtime_profiles` and `default_profile_id` when unbound, while active rows or bindings abort migration and preserve the old schema/data (fail-closed). |
| Generated types, permissions, business references | PASS | Production-code scan found no retired route, profile/default-profile field, permission, or business binding residue. Only the 0126 reversible migration files remain. Member catalog access remains read-only. |
| Desired/effective runtime config | PASS | Admin API and agent runtime regression suites passed for desired/effective config projection and reconciliation. |
| `allowed_executors` and reconcile | PASS | Agent runtime, model-router, orchestrator, worker daemon/controller/launcher and web API suites passed, including allowed-executor validation and fail-closed routing paths. |
| Integrated deletion commit | PASS | `9345409d` is an ancestor of the fixed candidate. |

## Commands run on the fixed candidate

| Command | Result |
| --- | --- |
| `go test ./internal/persistence ./internal/webconsole/api ./internal/admin/api ./internal/agentruntime/... ./internal/workerdaemon/...` | PASS |
| `go test ./...` | PASS |
| `cd web && pnpm typecheck` | PASS |
| `cd web && pnpm lint` | PASS |
| `cd web && pnpm exec vitest run` | PASS — 188 files, 1725 tests |
| `make build` | PASS |
| `cd tests/e2e/v2 && pnpm exec playwright test tests/ai-runtime.spec.ts` | PASS — 1 deployed-binary Chromium test |
| retired-field/route static scan and `git diff --check` | PASS |

## Non-blocking observations

The build retains the known CSS minification and large-chunk warnings. Vitest
retains unrelated MSW, React `act`, local-storage, and markup warnings. The
Playwright runner retains the `NO_COLOR`/`FORCE_COLOR` warning. No warning caused
a failed gate or indicated an AI Runtime Profile retirement regression.
