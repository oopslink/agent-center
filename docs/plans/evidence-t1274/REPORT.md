# T1274 final acceptance evidence

- Verdict: **PASS**
- Exact accepted main: `bacc8eaf0e6796eeb8d9f8f6dbc52387197e5997`
- Baseline/ref: `origin/main`
- Scope: final evidence-only acceptance; no product-code changes and no merge to `main`

## Contract results

| Gate | Result | Evidence |
| --- | --- | --- |
| Old Templates Web product removed | PASS | `/ooo/templates` and `/ooo/teams/templates` resolve to `NotFound`; organization/team secondary navigation and command palette expose no Templates entry. Covered by `App.test.tsx`, `WorkspaceSecondaryNav.test.tsx`, `TeamUISecondaryNav.test.tsx`, and `CommandPalette.test.tsx`, all included in the full Web run. |
| `rules/` is the only rule type source | PASS | Rule frontmatter deliberately has no `kind`/`type`; directory placement defines the type, with a regression guard rejecting rendered `kind: rule` and `type: rule`. Repository search found no such data marker outside explanatory docs/code/tests. |
| Phase-filtered rule loading | PASS | Team Memory consumer snapshots enabled rules by phase and retain exact team repo commit and `source_path`; runtime phase mapping covers execute/review/recovery, while plan tools load `phase=plan`. |
| Planning-tool production chain | PASS | `create_plan` and `edit_plan_topology` pass a frozen plan-rule snapshot through MCP host, admin API, and project-manager service. Snapshot includes `enabled`, commit/source/session/generation/skipped/load-error/refresh metadata. |
| Freeze/reload semantics | PASS | One MCP planning session loads once and reuses cloned snapshots, including after deferred-tool registration; a new supervisor generation creates a new MCP host/cache and reloads the current team repo. |
| Safe legacy migration | PASS | Only templates with one unique same-org team owner are claimed. Builtin, ownerless/human-owned, ambiguous, and cross-org candidates remain unclaimed with reasons; unrelated team repos are not provisioned, and legacy DB rows are retained for rollback. |
| Regression suite | PASS | Full Go suite (including integration and E2E), full Web tests, production Web build, and Web lint all passed. |

## Commands and results

```text
git fetch origin --prune
git rev-parse origin/main
# bacc8eaf0e6796eeb8d9f8f6dbc52387197e5997

go test ./...
# PASS, including tests/integration and tests/e2e

cd web
pnpm install --frozen-lockfile
pnpm test
# PASS: 184 files, 1689 tests
pnpm build
# PASS
pnpm lint
# PASS
```

The Web run emitted existing non-fatal test-console and bundle/CSS warnings, but no test, typecheck/build, or lint failure.

## Conclusion

`origin/main` at `bacc8eaf` satisfies the final Team Memory rules consolidation contract. The former organization Templates product surface is retired, rules have one filesystem type source, all four lifecycle phases consume auditable frozen rule snapshots with the required planning-session boundary, migration is isolation-safe, and affected plus repository-wide regressions are green.
