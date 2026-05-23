# P12 S1 — v1 vendor residue audit

> Run 2026-05-24 · per x9527 oversight: 6 dimensions × 7 patterns
> grep; classify each hit; cleanup commit lands SEPARATELY from this
> audit doc commit; lint script lands as a third commit.

## § 0. Scope

Search across the whole repo (excluding `web/node_modules` /
`sites/node_modules` / `.git`) for legacy v1 vendor terms that should
no longer appear in v2 runtime code, configs, assets, or active docs.

## § 1. Patterns × dimensions

7 patterns: `feishu` / `Feishu` / `FEISHU`, `lark` / `Lark`, `dingtalk` / `DingTalk`, `wechat` / `WeChat`, `bridge` (as BC name, not generic noun), `vendor_*` (column / field prefix), `webhook`.

6 dimensions:
1. Go source (`internal/**/*.go`, `cmd/**/*.go`)
2. SQL migrations (`internal/persistence/migrations/*.sql`)
3. Active docs (`docs/**/*.md`)
4. Configs (`*.yaml` / `*.yml` / `*.toml` at non-node_modules paths)
5. CI / scripts (`scripts/`, `.github/`, `Makefile`)
6. assets (`assets/skills/*`, `contrib/*`)

Whitelist (intentional historical references — **kept**):
- ADR-0031 (Bridge BC drop ADR) + ADR-0007 / 0016 / 0019 (v1-era ADRs with "superseded by ADR-0039" banner)
- Migration files (`internal/persistence/migrations/*.sql`) — historical immutable; v1→v2 drop migrations themselves reference the dropped names by necessity
- Plan docs (`docs/plans/phase-*.md`) explaining what was dropped
- Test report docs that describe earlier-phase work

## § 2. Findings — to DELETE (real v1 residue)

### 2.1 Go source

| File | Line(s) | Issue | Action |
|---|---|---|---|
| `internal/discussion/origin.go` | 14-17 | `OriginFeishuAt` constant + comment | Remove constant + comment |
| `internal/discussion/origin.go` | 33, 49 | switch cases including `OriginFeishuAt` | Remove from `IsValid` + `NeedsSyncConversationBuild` + `AllowsConversationBind` |
| `internal/discussion/origin_test.go` | 9, 19, 34 | uses `OriginFeishuAt` + string `"feishu_at"` | Remove + drop the `feishu_at` IsValid test entry |
| `internal/discussion/issue_test.go` | 340 | uses `OriginFeishuAt` in fixture | Replace with `OriginWebConsole` |
| `internal/discussion/service/issue_lifecycle_service.go` | 115, 126 | comments reference `feishu` as sync-build branch + `PrimaryChannelHint` field that no longer maps to anything | Remove `PrimaryChannelHint` field + rewrite comment to v2 reality (web / supervisor only) |
| `internal/discussion/service/issue_lifecycle_service_test.go` | 122-189 | `TestOpen_SyncBuild_Feishu` test + 4 fixtures using `OriginFeishuAt` + `PrimaryChannelHint: "feishu"` | Rename test → `_WebConsole`; change Origin / drop PrimaryChannelHint usage |
| `internal/discussion/service/issue_conclude_test.go` | 73, 163, 213 | `OriginFeishuAt` fixtures | Replace with `OriginWebConsole` |
| `internal/discussion/service/issue_bind_conversation_service_test.go` | 51, 214, 238, 269 | same | same |
| `internal/discussion/service/issue_comment_service_test.go` | 28 | same | same |
| `internal/config/config_test.go` | 41 | string fixture `"feishu:user:hayang:dm"` | Replace with `"dm:peer-id"` (v2-shaped) |
| `internal/config/config.go` | 23 | doc-comment lists `bridge` in subtree examples | Drop `bridge` from list |
| `internal/cli/handlers_supervisor.go` | 200, 202 | Summary "via Bridge (audience=S)" + channel flag default `"feishu / dingtalk / ..."` | Rewrite to v2: drop Bridge / drop channel-vendor list |
| `internal/cli/handlers_supervisor_extra_test.go` | 1 hit | feishu test fixture | Replace |
| `internal/cli/handlers_issue.go` | 2 hits | feishu strings (likely default channel hint) | Replace / remove |
| `internal/cli/handlers_issue_test.go` | 1 hit | test fixture | Replace |
| `internal/cli/handlers_task.go` | 1 hit | "feishu" string in code / comment — investigated: phrase "happens at the daemon-side RPC bridge" is `bridge` not `feishu`; the `feishu` hit needs spot inspection | Inspect + clean |
| `internal/cli/handlers_system.go` | 1 hit | the line is the comment explaining current state ("Bridge BC inbound + feishu adapter removed in P10 § 3.9 per ADR-0031") — this is an INTENTIONAL historical reference explaining the current single-escalator state | **KEEP** |
| `internal/taskruntime/service/task_service_test.go` | 4 hits | feishu fixtures | Replace |
| `tests/integration/phase3_test.go` | 8 hits | feishu fixtures across phase-3 integration tests | Replace with v2 channels |
| `internal/conversation/identity/identity.go` | 12 | comment "(channel, vendor_user_id) unique across all identities" — refers to a v1 invariant that's no longer enforced (channel was a v1 thing) | Rewrite comment to v2 invariants only |

### 2.2 SQL migrations

| File | Action |
|---|---|
| `internal/persistence/migrations/0001_init.up.sql`, `0001_init.down.sql` | **KEEP** — historical immutable v1 schema |
| `internal/persistence/migrations/0003_discussion_issues.up.sql` | **KEEP** — historical v1 migration |
| `internal/persistence/migrations/0005_bridge_feishu_outbound.up.sql`, `.down.sql` | **KEEP** — historical v1 migration creating the bridge tables |
| `internal/persistence/migrations/0020-0023_v2_*` | **KEEP** — v2 cleanup migrations (DROP statements necessarily reference the dropped identifiers) |
| `internal/persistence/migrations/0025_v2_drop_bridge_feishu_tables.up.sql`, `.down.sql` | **KEEP** — v2 drop migration |

**Schema cleanup is already complete from P10** — no NEW migration needed in S2 (this audit confirms it; S2 ST can be downscoped to "verify migration up/down still pass" rather than write a new drop migration).

### 2.3 Active docs

| File | Action |
|---|---|
| `docs/index.md` | 4 feishu hits — likely tagline/intro residue; rewrite |
| `docs/rules/conventions.md` | 2 feishu hits — likely §9.y vendor-name rule examples; check + update if stale |
| `docs/rules/testing.md` | 1 hit — check |
| `docs/design/ddd-blueprint.md` | 2 hits — likely intro / status table; check |
| `docs/design/architecture/strategic/01-subdomain-classification.md` | 1 hit |
| `docs/design/architecture/strategic/02-system-overview.md` | 2 hits |
| `docs/design/architecture/strategic/03-bounded-contexts.md` | 2 hits |
| `docs/design/architecture/tactical/agent-harness/02-skill-cli-tooling.md` | 1 hit |
| `docs/design/architecture/tactical/cognition/00-overview.md` | 1 hit |
| `docs/design/architecture/tactical/discussion/00-overview.md` | 4 hits |
| `docs/design/architecture/tactical/observability/00-overview.md` | 2 hits |
| `docs/design/architecture/tactical/task-runtime/00-overview.md` | 2 hits |
| `docs/design/architecture/tactical/task-runtime/01-task.md` | 3 hits |
| `docs/design/architecture/tactical/task-runtime/02-task-execution.md` | 1 hit |
| `docs/design/architecture/tactical/task-runtime/03-input-request.md` | 3 hits |
| `docs/design/architecture/tactical/workforce/00-overview.md` | 2 hits |
| `docs/design/architecture/tactical/workforce/03-worker-project-proposal.md` | 1 hit |
| `docs/design/implementation/03-cli-subcommands.md` | 3 hits |
| `docs/design/implementation/04-configuration.md` | 10 hits — `feishu.*` config block residue |
| `docs/design/implementation/06-deployment.md` | 11 hits — most already marked as strikethrough; verify clean |
| `docs/design/roadmap.md` | 3 hits — check if all in v3-bring-back-bridge bullet |

These will be handled by **S6 docs sweep** (Wave 2 Group A banner clean) — S1 only inventories them. **Audit decision**: defer doc cleanup to S6; S1 cleanup commit focuses on Go source + configs.

### 2.4 Configs / contrib / scripts

| File | Line | Action |
|---|---|---|
| `contrib/agent-center.service` | 11 | `EnvironmentFile=-/etc/agent-center/feishu.env` | Delete line |
| `contrib/install.sh` | 118 | "Install /etc/agent-center/feishu.env with App ID + secret (see § 10.3)" | Delete instruction |
| `Makefile`, `.github/`, `scripts/` | (none) | grep clean ✅ | n/a |

### 2.5 Assets

| Path | Status |
|---|---|
| `assets/skills/` | Only `supervisor.md` present; **no vendor-specific skill files** ✅ |

### 2.6 go.mod / go.sum

Hits in `go.mod` / `go.sum` are Go module-style `// indirect` markers; pattern matched `vendor_` or `bridge` only as substring inside unrelated package names. **No action needed** (verified by sampling).

## § 3. Decisions retained (whitelist)

These hits are **kept intentionally**:

| Location | Reason |
|---|---|
| ADR-0031 `decisions/drafts/0031-v2-drop-bridge-vendor-integration.md` | This IS the ADR documenting the drop |
| ADR-0007 `decisions/0007-conversation-as-unified-session.md` | v1 ADR with v2 superseded-by banner |
| ADR-0013 `decisions/0013-supervisor-invocation-concurrency.md` | historical reference |
| ADR-0016 `decisions/0016-task-progress-via-bound-thread.md` | v1 ADR with v2 superseded-by banner |
| `internal/conversation/message.go` + `repository.go` + `sqlite/message_repo.go` + `identity/types.go` comments | Document WHY a field is missing in v2 — useful context for reader |
| `internal/cli/build.go:163` + `internal/cli/handlers_system.go:91` comments | Explain current state via historical reason |
| `docs/release/v2.0-draft.md` | v2 release notes explicitly list dropped vendor terms in breaking-changes section |
| Plan / report docs (`docs/plans/phase-10-*.md`, `phase-12-*.md`, `reports/phase-10-test-report.md`) | Documentation of completed / planned work |
| `docs/design/decisions/drafts/0031-v2-drop-bridge-vendor-integration.md` | The decision record itself |
| `docs/drafts/*.md` (session checkpoints) | Historical session notes; not active spec |
| All migration `.sql` files | Immutable history |

## § 4. Cleanup scope (next commit)

**Go source (≤ 14 file edits):**
1. `internal/discussion/origin.go` — remove OriginFeishuAt + 3 switch cases
2. `internal/discussion/origin_test.go` — remove "feishu_at" test entries
3. `internal/discussion/issue_test.go` — replace OriginFeishuAt usage
4. `internal/discussion/service/issue_lifecycle_service.go` — remove PrimaryChannelHint field + clean comment
5. `internal/discussion/service/issue_lifecycle_service_test.go` — rename _Feishu test, drop PrimaryChannelHint
6. `internal/discussion/service/issue_conclude_test.go` — replace 3 fixtures
7. `internal/discussion/service/issue_bind_conversation_service_test.go` — replace 4 fixtures
8. `internal/discussion/service/issue_comment_service_test.go` — replace 1 fixture
9. `internal/config/config_test.go` — replace `"feishu:user:hayang:dm"` fixture
10. `internal/config/config.go` — drop `bridge` from doc-comment list
11. `internal/cli/handlers_supervisor.go` — rewrite escalation Summary + flag default
12. `internal/cli/handlers_supervisor_extra_test.go` — replace fixture
13. `internal/cli/handlers_issue.go` + `handlers_issue_test.go` — replace fixtures
14. `internal/cli/handlers_task.go` — spot-fix the lone feishu hit (if real)
15. `internal/taskruntime/service/task_service_test.go` — replace 4 fixtures
16. `tests/integration/phase3_test.go` — replace 8 fixtures
17. `internal/conversation/identity/identity.go` — rewrite v1-invariant comment

**Configs:**
- `contrib/agent-center.service` line 11 delete
- `contrib/install.sh` line 118 delete

**Docs:** **Deferred to S6** (Wave 2 Group A banner sweep).

## § 5. Acceptance criteria for S1 cleanup commit

- All "to DELETE" items in § 4 actioned
- `go test ./...` still green
- `go vet ./...` clean
- Run `git grep -in 'feishu\|lark\|dingtalk\|wechat\|bridge bc' -- internal/ contrib/ scripts/ Makefile` → only whitelist hits remain (§ 3)
- Comment whitelist documented in S1 lint script (§ 6)

## § 6. Acceptance criteria for S1 lint script commit (third commit)

- `scripts/lint/no-vendor-refs.sh` shell script that:
  - greps the same 7 patterns × 6 dimensions
  - has the whitelist baked in (file paths / regex)
  - exits 0 on clean, 1 on hit (with the matching line numbers)
- `Makefile` adds `make lint-vendor` target
- README mentions the lint as part of CI checks

## § 7. Summary

| Dimension | Hits | Real residue | Action |
|---|---|---|---|
| Go source | ~45 | 17 files | S1 cleanup commit |
| SQL migrations | ~24 | 0 (all immutable history) | KEEP |
| Active docs | ~73 | many | DEFER to S6 docs sweep |
| Configs | 2 | 2 (contrib) | S1 cleanup commit |
| Assets | 0 | 0 | n/a |
| CI / scripts | 0 | 0 | n/a |

**S1 cleanup scope**: 17 Go files + 2 contrib files = 19 changed files. Estimated ~1h additional work.

## § 8. Execution log (addendum — 2026-05-24)

### 8.1 Audit commit
`81b9bf6 docs(p12 S1) v1 vendor residue audit log` — this file as initially drafted.

### 8.2 Cleanup commit
`44b298a chore(p12 S1) v1 vendor cleanup — go source + contrib configs` — 21 files changed (17 Go + 2 contrib + 2 extra: `cmd/fakeagent/main.go` doc reference rewrite + `internal/cli/handlers_task.go` comment rewrite to disambiguate "bridge"=RPC vs Bridge BC). `go test ./...` + `go vet ./...` green.

Notable deltas from § 4 plan:
- `cmd/fakeagent/main.go` had a dangling reference to `docs/plans/phase-7-bridge-inbound-deploy.md` (file no longer exists post P10) → rewrote doc comment to v2 purpose statement.
- `internal/cli/handlers_task.go:332` "daemon-side RPC bridge in Phase 5/7" → reworded to avoid the word "bridge" entirely (avoids confusion with the deleted Bridge BC).
- `PrimaryChannelHint` removal was already done in P10 work; only call-sites needed the `OriginFeishuAt`→`OriginWebConsole` swap.

### 8.3 Lint script commit
`scripts/lint/no-vendor-refs.sh` + `scripts/lint/no-vendor-refs.allowlist` + Makefile `lint-vendor` target. Run with `make lint-vendor`; current status: **clean** (all hits whitelisted).

#### Allowlist categories
1. **History-of-record paths** — ADR / plan / report / changelog / migrations / design docs (the design docs section currently catches both intentional v2 history surfaces AND stale v1 narrative; § 8.4 documents the deferred tightening).
2. **In-code historical comments** — explicit `// X removed in P10 § 3.9 per ADR-0031` notes (kept because removing them deletes the rationale a reader would want).
3. **Guard tests** — `origin_test.go` lines that assert `feishu_at` is rejected by `IsValid` / `ParseOrigin`.
4. **Lint scaffolding** — the lint script, its allowlist, the Makefile target description.
5. **TEMPORARY — deferred to P12 S6** — `docs/drafts/session-checkpoint-2026-05-16.md`, `docs/index.md`, `docs/rules/conventions.md` (feishu lines only), `docs/rules/testing.md` (feishu lines only), `sites/.vitepress/config.ts` (feishu link only). The "TEMPORARY — REMOVE WHEN P12 S6 LANDS" block in the allowlist is the explicit marker; **S6 acceptance criterion is "the TEMPORARY block is empty"**.

### 8.4 Open items handed off to S6
- Active doc sweep (Wave 2 Group A): `docs/index.md`, `docs/rules/*.md`, `docs/design/**/*.md` (the ~73 hits from § 2.3 minus what landed in `docs/release/v2.0-draft.md` already) — currently allowlisted under category 1 (history-of-record paths) and category 5 (TEMPORARY). After S6 lands, narrow the `^docs/design/` and `^docs/rules/` allowlist scopes to only the ADR / banner-marked sections.
- `sites/.vitepress/` v1 nav menu cleanup — currently allowlisted under category 5.

### 8.5 Final grep status
```
$ make lint-vendor
./scripts/lint/no-vendor-refs.sh
no-vendor-refs: clean (all hits whitelisted)
```
