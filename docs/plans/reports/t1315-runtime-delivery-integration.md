# T1315 Runtime Delivery Integration Report

## Candidate

- Baseline fetch: `origin/main` was fetched from `git@github.com:oopslink/agent-center.git` into `refs/remotes/origin/*` because the local repo has a broad `+refs/*:refs/*` fetch refspec that would update branches checked out by sibling worktrees.
- Real `origin/main` tip at integration start: `db8faefb4cbf012f03a188161eebf7786eed4f13`.
- Candidate branch: `integration/t1315-runtime-delivery-candidate`.
- Candidate SHA: final pushed branch tip, recorded by the delivery response and obtainable with `git rev-parse origin/integration/t1315-runtime-delivery-candidate` after fetch. The exact SHA cannot be embedded into the same content-addressed commit that creates this report.

## Integration Summary

The candidate branch was created from the real `origin/main` tip and does not push or modify `main`.

The T1307 rework branch was merged normally, preserving ancestry for its integrated commits, including the T1310 AI Runtime Profile removal commit. T1308 and T1309 were then recorded with `-s ours` merge commits to preserve upstream ancestry without replaying duplicate or stale trees wholesale; their still-needed unique patches were replayed and resolved manually on top of the profile-free tree.

The resulting tree keeps the T1310 deletion semantics:

- `web/src/config/agent-defaults.ts` remains deleted.
- AI Runtime profile route registration and profile default behavior remain removed.
- Frontend selectors use explicit CLI/model catalog choices rather than profile or default model fallbacks.
- Backend runtime entry validation remains profile-free and chooses CLI/model defaults from enabled catalog entries only.
- Remaining profile references are tests or audit/report text for retired-field rejection and compatibility behavior.

## Relative Commit List

Current non-report commits relative to `origin/main`:

```text
9345409d Remove AI runtime profiles
db55866d T1305 runtime selector contract
cc06031f fix(runtime): remove profile dependency from selector
cc6399a4 fix(runtime): require selectable CLI for model choices
e1d23383 T1305 runtime selector contract
685107fe fix(runtime): remove profile dependency from selector
6d112800 fix(runtime): require selectable CLI for model choices
782c8574 Add shared runtime selectors
6fa3260f feat(web): migrate team role runtime selectors
5c7b46f7 Use runtime capabilities for agent model selection
cfc914d1 Merge commit '9345409d797e8a877eee5515785ba5a67ef9ca5f' into ac-rework/task-4aa3e376
93149411 Merge commit 'cc6399a40a48fc10dd540467bcf0b545b1f3c3ba' into ac-rework/task-4aa3e376
c7dac9d2 Merge commit '5c7b46f7f613577ba6aa7f068c637bfcd4a950c5' into ac-rework/task-4aa3e376
eb4356b2 fix(runtime): remove profile defaults from agent selectors
d8a50701 Audit CLI model runtime entry contracts
51d67182 chore(evidence): persist task-973dc130 verification
204ee521 Merge remote-tracking branch 'origin/ac-exec/task-4aa3e376/rework-integrated' into integration/t1315-runtime-delivery-candidate
388ad878 merge(t1308): record team role runtime selector delivery
71de9bd4 Add shared runtime selectors
fb7eea25 feat(web): migrate team role runtime selectors
59af067f merge(t1309): record CLI model runtime audit delivery
32395400 Audit CLI model runtime entry contracts
3f9b2ec0 chore(evidence): persist task-973dc130 verification
```

This report commit is appended on top of that list.

## Upstream Coverage Matrix

| Upstream delivery | SHA | Ancestry in candidate | Tree or patch coverage | Notes |
| --- | --- | --- | --- | --- |
| T1310 profile removal | `9345409d797e8a877eee5515785ba5a67ef9ca5f` | yes | kept | Required deletion semantics retained. |
| T1307 manual recovery candidate | `eb4356b2567b56df7aa939fae2d397e4c3a9e1e2` | yes | kept | Merged through `origin/ac-exec/task-4aa3e376/rework-integrated`. |
| T1308 team role selector delivery | `6fa3260f7780e9991cbec17e9c7c015198485704` | yes | replayed as `fb7eea25` after ancestry merge | Kept team role runtime selector work without reintroducing profile defaults. |
| T1309 CLI/model audit evidence | `51d6718295bfb2764d75ec3d8564a7c3e9ed6035` | yes | kept | Evidence commit replayed cleanly. |
| T1309 CLI/model audit code | `d8a5070144112c1f0952154ce83f3b7cc593ac07` | yes | replayed as `32395400` after profile-free conflict resolution | Contract validation retained, but default-pair logic was adapted away from retired profiles. |
| T1308 shared selector base | `782c85741566ee3448b71b8fe003daaada364aa3` | yes | replayed as `71de9bd4` after conflict resolution | Kept shared selector component and tests. |
| T1305 runtime selector contract | `db55866d5b4617863717bd1366313ac3fe8367ef` | yes | kept | Duplicate with the same patch-id as `e1d23383` and `d8e9da7c`. |
| T1305 runtime selector contract variant | `e1d2338384995080949750dbc0b196a2bcc59835` | yes | duplicate patch-id covered | Introduced through T1308 ancestry; no separate tree change needed. |
| T1305 runtime selector contract variant | `d8e9da7c73bbe2851276904c646c89895854c4f6` | no | duplicate patch-id covered | Same patch-id as `db55866d`; intentionally deduped. |
| Runtime selector profile-dependency fix | `cc06031f` / `685107fe` | yes | duplicate patch-id covered | Both variants share patch-id `700d40b27c30b6b042e9cb235ed2ae821644a25c`. |
| Runtime selector selectable-CLI fix | `cc6399a4` / `6d112800` | yes | duplicate patch-id covered | Both variants share patch-id `4eb8c4427e605b9861cdfd1bb511b9f7246a4331`. |
| Runtime capability agent selection | `5c7b46f7f613577ba6aa7f068c637bfcd4a950c5` | yes | kept | Merged through the T1307 recovery branch. |

## Verification

All commands below were run on the candidate branch after conflict resolution.

| Command | Result |
| --- | --- |
| `go test ./internal/webconsole/api -run 'TestAPI_RuntimeContract|TestAPI_AddAgentMember_236|TestImportTemplate_UncuratedWithDefaults|TestSaveTemplate_CuratedThenListed|TestAIRuntime|TestLegacyModelCatalogImportUsesRuntimePreviewApplyAdapter'` | pass |
| `go test ./...` | pass |
| `cd web && pnpm typecheck` | pass |
| `cd web && pnpm exec vitest run src/pages/AiRuntime.test.tsx` | pass, 8 tests |
| `cd web && pnpm exec vitest run` | pass, 188 files and 1725 tests |
| `cd web && pnpm lint` | pass |
| `cd web && pnpm build` | pass, with existing CSS minify and chunk-size warnings |
| `make build` | pass, produced `bin/agent-center` and `bin/fakeagent` |
| `cd tests/e2e/v2 && pnpm exec playwright test tests/ai-runtime.spec.ts` | pass after `make build`, 1 test |
| `git diff --check` | pass |

The first Playwright attempt failed because `bin/agent-center` was not present yet. After building the binary with `make build`, the same AI Runtime e2e passed.

## Residual Risks

- Vite still reports the existing CSS minify warning `Expected identifier but found "-"` and a chunk-size warning for the built web bundle.
- Vitest still emits existing environment warnings from MSW, React `act`, and local storage tests; they did not fail the suite.
- Playwright reports that `NO_COLOR` is ignored when `FORCE_COLOR` is present; the test itself passed.
- The final candidate commit SHA is reported outside this file because a Git commit cannot contain a literal self-hash in its own tracked content.
