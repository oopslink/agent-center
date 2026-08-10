# T1309 CLI/Model Entry Audit Acceptance

Date: 2026-08-10

Branch: `ac-exec/task-973dc130/exec-a7b22492`

## Scope

Audited CLI/Model entry points in the Web Console SPA, Web Console HTTP API, AI Runtime catalog adapters, Team template import/save flows, Agent config/create flows, and PM task creation model override.

`/opt/homebrew/bin/rg` was used for the audit because the bundled `rg` on this worker hangs in this checkout.

## Coverage

| Surface | Status | Evidence |
| --- | --- | --- |
| Shared runtime contract | Covered | `internal/webconsole/api/runtime_contract.go` validates catalog default, CLI-only, model-only, explicit CLI+Model, executor profiles, team roles, and template slots. |
| Agent create | Covered | `handlers_members.go` validates main `{cli,model}`, `allowed_models`, `allowed_executors`, `orchestrator_model`, and `default_executor_model`. |
| Agent config update | Covered | `handlers_agent.go` validates the same runtime fields before `UpdateAgentConfig`. |
| PM task model override | Covered | `handlers_pm.go` validates non-empty task `model` through the runtime catalog. |
| Team create/update/instantiate | Covered | `handlers_teams.go` and `handlers_teams_p2.go` validate all role CLI/Model pairs. |
| Team template save/import | Covered | `handlers_teams_p3.go` validates slots and no longer uses static CLI/Model fallbacks. Missing runtime fields resolve through the org AI Runtime default profile when a catalog is wired. |
| Legacy Model Catalog adapter | Covered | `handlers_model_catalog.go` maps legacy model writes to AI Runtime with compatible CLI keys derived from enabled catalog CLIs instead of a static CLI. |
| Agent create UI | Covered | `AgentCreateModal.tsx` uses `useAIRuntimeCatalog`, catalog-backed selects, CLI-filtered model options, and model descriptions. |
| Member Add Agent UI | Covered | `MemberNew.tsx` uses the same catalog-backed runtime choice flow. |
| Agent config UI | Covered | `AgentConfigEditModal.tsx` uses catalog-backed selects, compatible executor rows, normalized choices, and disabled save for invalid runtime state. |
| Team role builder UI | Covered | `RoleBuilder.tsx` derives role CLI/Model defaults and options from AI Runtime catalog. |
| AI Runtime management UI | Covered | This remains the authoritative catalog authoring surface. Free entry is intentional here and owner/admin guarded. |
| AI Runtime redirect/config changes | Covered | Unit tests and Playwright e2e cover direct `/ai-runtime` redirect, System nav, import preview/apply, model edit, profile create, and member read-only state. |

## Scan Results

Static selection/default scan:

```sh
/opt/homebrew/bin/rg -n 'DEFAULT_AGENT_MODEL|KNOWN_MODELS|MODEL_SUGGESTIONS|CLI_OPTIONS|\bCLIS\b|\bMODELS\b|defaultAgentModel|firstNonEmpty\([^\n]*(claude|sonnet|gpt|codex|gemini)|model is free text|suggestions but allows custom|CompatibleCLIKeys: \[\]string\{"codex"\}' web/src/components web/src/pages web/src/api web/src/utils internal/webconsole/api -g '!**/*test*' -g '!web/src/mocks/**'
```

Result: no matches.

Literal model/CLI scan:

```sh
/opt/homebrew/bin/rg -n 'sonnet-5|gemini-cli|claude-opus-4-8|claude-sonnet-4-6|gpt-5|claude-code|codex' web/src/components web/src/pages web/src/api web/src/utils internal/webconsole/api -g '!**/*test*' -g '!web/src/mocks/**'
```

Remaining matches are classified as non-input: fixture data (`teamsFixtures.ts`), read-only analytics/display helpers (`modelColors.ts`, `executorProfiles.ts`), historical comments, and the AI Runtime authoring placeholder example.

Backend JSON runtime input scan:

```sh
/opt/homebrew/bin/rg -n 'json:"(cli|model|allowed_models|allowed_executors|orchestrator_model|default_executor_model)"' internal/webconsole/api -g '*.go'
```

All matched request structs are now covered by `validateAgentRuntimeConfig`, `validateRuntimeModelValue`, `validateTeamRuntimeRoles`, or `validateTeamRuntimeSlots`.

## Automated Evidence

Commands run on the final code state unless noted:

| Command | Result |
| --- | --- |
| `go test ./internal/webconsole/api -run 'TestAIRuntime|TestLegacyModelCatalogImportUsesRuntimePreviewApplyAdapter|TestAPI_RuntimeContract'` | PASS |
| `go test ./internal/webconsole/api -run 'TestAPI_RuntimeContract|TestImportTemplate_UncuratedWithDefaults|TestSaveTemplate_CuratedThenListed|TestAIRuntime|TestAPI_AddAgentMember_236'` | PASS |
| `go test ./...` | PASS |
| `cd web && pnpm typecheck` | PASS |
| `cd web && pnpm exec vitest run src/pages/AiRuntime.test.tsx` | PASS, 8 tests |
| `cd web && pnpm exec vitest run` | PASS, 186 files / 1709 tests. This was run before the final comment cleanup and legacy-adapter backend tweak; affected targeted suites were rerun after final edits. |
| `make lint` | PASS |
| `make build` | PASS; existing Vite CSS minify warning and chunk-size warning observed. |
| `cd tests/e2e/v2 && pnpm exec playwright test tests/ai-runtime.spec.ts` | PASS, 1 test. |

## Known Warnings

- `make build` reports the existing Vite CSS minify warning: `Expected identifier but found "-"`.
- `make build` reports the existing Rollup chunk-size warning for a chunk over 500 kB.
- Full Vitest emitted existing MSW unhandled-request and React `act(...)` warnings in unrelated tests; all tests passed.
- Playwright emitted `NO_COLOR` ignored because `FORCE_COLOR` is set; the test passed.

## Residual Risk

No production CLI/Model business input path remains intentionally free-text or hardcoded by this audit. The only remaining literal runtime names are fixtures, comments, display-only helpers, tests, mocks, or AI Runtime catalog authoring examples.
