# ADR-0059 canonical mock validation report

| Field | Value |
|---|---|
| Baseline | `7232c04df2bc902353107404d18b4bc3f5bd5712` |
| Scope | Repo-native mock source, canonical PNG, acceptance contract; no product implementation |
| Viewport | `1672 × 941`, device scale factor 1 |
| Date | 2026-08-23 |

## Input audit

- Read `docs/rules/conventions.md` and `docs/rules/documentation.md` in full.
- Read ADR-0059, the T1435 frozen contract, and the T1435 residual matrix at the
  specified baseline.
- Inspected the three locally available predecessor evidence sets: T1456 RAM Roles,
  T1457 Team Roles, and T1458 Subject access.
- `task-input/v1/README.md`, `task-input/v1/manifest.json`, and
  `task-input/v1/attachments/` were not present in this isolated worktree. Therefore the
  plan-a77ec987 100-item attachment could not be read locally. No control-plane, database,
  socket, token, or raw HTTP fallback was attempted.

## Verification results

| ID | Command / method | Result | Evidence |
|---|---|---|---|
| V01 | `node docs/design/assets/adr-0059/capture.mjs` | PASS | Three PNGs regenerated from HTML sources |
| V02 | `sips -g pixelWidth -g pixelHeight .../*-canonical-1672x941.png` | PASS | All three are exactly `1672 × 941` |
| V03 | `node docs/design/assets/adr-0059/verify.mjs` | PASS | Required states/labels present; Access nav count = 2; retired product labels absent from HTML |
| V04 | Visual inspection of all three canonical PNGs | PASS | Page identity, IA ownership, read-only references, Team context, and source chain are visible |
| V05 | `git diff --check` | PASS | No whitespace errors |
| V06 | `make lint-doc-impl-drift` | PASS | All 3 repository doc/implementation drift checks passed |
| V07 | `make lint-no-raw-colors-spa` | PASS | SPA color guard clean; no product implementation changed |
| V08 | `make lint-spa-tsc` | PASS | TypeScript build-mode check passed |
| V09 | `make lint-spa-eslint` | BASELINE FAIL | Existing `web/src/pages/Access.tsx:925` checkbox rule violation; file is unchanged by this delivery |

V09 is recorded rather than repaired because this S1 explicitly excludes product implementation.
The failing source line is present at the frozen baseline and no file under `web/src/` is in the
candidate diff.
