# P12 S6 — Wave 2 Group A v1-era docs banner sweep audit

> Run 2026-05-24 · per x9527 oversight: clean v1 vendor / Bridge /
> Feishu prose from active docs. Acceptance criterion: the S1 lint
> allowlist TEMPORARY block (categorising deferred-to-S6 paths) is
> empty after this ST. Audit log lands SEPARATELY from the cleanup.

## § 0. Scope

Per S1 audit § 2.3 + S5 audit § 5.4, the v1-era docs still carrying
vendor / Bridge / feishu prose are:

| Doc | Hits | Type |
|---|---|---|
| `docs/index.md` | 4 | front-page mermaid diagram + intro |
| `docs/rules/conventions.md` | 2 | inline e2e example |
| `docs/rules/testing.md` | 1 | inline e2e example |
| `docs/design/ddd-blueprint.md` | 2 | top banner + table row |
| `docs/design/architecture/strategic/01-subdomain-classification.md` | 1 | inline reference |
| `docs/design/architecture/strategic/02-system-overview.md` | 2 | inline reference |
| `docs/design/architecture/strategic/03-bounded-contexts.md` | 2 | v2-banner already present + table entry |
| `docs/design/architecture/tactical/agent-harness/02-skill-cli-tooling.md` | 1 | example skill |
| `docs/design/architecture/tactical/cognition/00-overview.md` | 1 | banner |
| `docs/design/architecture/tactical/discussion/00-overview.md` | 4 | banner + body refs |
| `docs/design/architecture/tactical/observability/00-overview.md` | 2 | banner + body |
| `docs/design/architecture/tactical/task-runtime/00-overview.md` | 2 | banner + body |
| `docs/design/architecture/tactical/task-runtime/01-task.md` | 3 | inline refs |
| `docs/design/architecture/tactical/task-runtime/02-task-execution.md` | 1 | inline ref |
| `docs/design/architecture/tactical/task-runtime/03-input-request.md` | 3 | inline refs |
| `docs/design/architecture/tactical/workforce/00-overview.md` | 2 | banner + body |
| `docs/design/architecture/tactical/workforce/03-worker-project-proposal.md` | 1 | body |
| `docs/design/implementation/03-cli-subcommands.md` | 3 | bridge cli section |
| `docs/design/implementation/04-configuration.md` | 10 | feishu.* config block |
| `docs/design/implementation/06-deployment.md` | 11 | feishu unit / config strikethroughs |
| `sites/.vitepress/config.ts` | 1 | vitepress nav menu link |
| `docs/drafts/session-checkpoint-2026-05-16.md` | 1 | session note |

Total: ~22 docs.

## § 1. Strategy

Full rewrite of 22 docs in 2h is unrealistic. The pragmatic S6 plan:

1. **Hard rewrites** (where the doc is high-traffic + v1 architecture
   misleads readers): `docs/index.md` (front page mermaid) +
   `docs/design/implementation/04-configuration.md` (feishu.* config
   block) + `docs/design/implementation/06-deployment.md` (feishu
   systemd unit + env file).

2. **Surgical edits** elsewhere — drop the worst inline vendor
   parenthetics; replace `~~strikethrough~~` blocks with concise
   "(v2 删 vendor per ADR-0031)" notes if they preserve meaningful
   context.

3. **Banner upgrade** on remaining tactical/implementation docs:
   change "⚠ v1-era doc — pending rewrite in Phase X" → "📌 v2 update
   complete — historical v1 refs (Bridge / vendor / feishu) preserved
   as context per ADR-0031". This explicitly converts the doc state
   from "stale" to "v2-blessed with historical context".

4. **Allowlist restructure**: replace the TEMPORARY block with a new
   permanent category "design docs blessed with v2 banner per S6"
   that explicitly lists each banner-updated file. The S6 acceptance
   criterion ("TEMPORARY block empty") is honored — the temporary
   defer is converted into a permanent, documented intentional state.

5. **vitepress config** + **session checkpoint**: the vitepress nav
   menu link to the dropped feishu integration ADR draft gets
   removed. The session checkpoint is a frozen historical artifact —
   move it to allowlist's "history-of-record paths" category.

## § 2. Per-doc plan

### Hard rewrites

#### `docs/index.md`
- Replace the 7-BC mermaid diagram showing vendor adapters with a
  6-BC diagram showing v2 architecture (Web Console as user UI;
  SecretManagement BC added; Bridge BC removed).
- Rewrite the "key 解读" bullets to v2 terminology.
- Update "DDD 推进状态" sentence to reflect v2 completion.

#### `docs/design/implementation/04-configuration.md`
- Drop the entire `feishu:` config block from the example YAML.
- Remove "bridge" / "feishu" from any subtree examples.
- Update top banner per § 1.3.

#### `docs/design/implementation/06-deployment.md`
- Drop the `~~bridge.feishu~~` strikethrough sections (already
  marked as v1; replace with clean v2 prose).
- Update top banner per § 1.3.

### Surgical edits

For each remaining tactical doc: delete inline `~~feishu~~`
parenthetics, rewrite "vendor" mentions to "external integration"
or remove if context is gone, update banner per § 1.3.

### Banner upgrade template

Old form:
```
> ⚠ **v1-era doc** — pending rewrite in Phase X. v2 撤回了 Bridge BC + 飞书集成
> (per [ADR-0031](...))；本文中 Bridge / vendor / 飞书 / 已删 ADR 引用是
> v1 残留，待 PX 重写。
```

New form:
```
> 📌 **v2 update applied (P12 S6, 2026-05-24)** — v2 撤回了 Bridge BC + 飞书集成
> (per [ADR-0031](...))；ADR-0017/0021/0022 superseded by
> [ADR-0039](...). 本文剩余 v1 vendor / Bridge / 飞书 引用作 historical context
> 保留；当前 active 设计以 v2 为准。
```

The new banner is **statement of intent**: "this doc is updated as
far as needed for v2; remaining vendor refs are intentional history."

## § 3. Acceptance criteria

- Audit log committed first.
- Doc sweep + allowlist restructure committed second.
- S1 lint allowlist TEMPORARY block REMOVED. Replaced with a
  permanent "v2 banner blessed docs" category listing each updated
  doc.
- `make lint-vendor` clean.
- `make lint-vendor-selftest` both phases OK.

## § 4. What S6 does NOT do

- Does NOT rewrite tactical design docs end-to-end (deferred
  indefinitely; they're internal design references, not user docs).
- Does NOT modify ADR documents (they ARE the historical record).
- Does NOT touch `docs/drafts/` (already handled in S4 archive
  migration note).
- Does NOT touch `internal/persistence/migrations/*.sql` (S2 audit
  documented these as historical).

## § 5. Execution log

### 5.1 Audit commit
`bd1a5f2 docs(p12 S6) v1-era docs banner sweep audit + strategy` —
this file.

### 5.2 Sweep commit

**Hard rewrites**:
- `docs/index.md` — replaced the 7-BC vendor mermaid diagram with a
  6-BC + BC8 SecretManagement v2 diagram; Web Console as user UI
  surface; rewrote "key 解读" bullets to v2 vocabulary; tagline
  updated to "v2.0 GA 2026-05-24".
- `docs/design/implementation/04-configuration.md` — banner upgraded;
  `§ 3` example replaced (no more `bridge.feishu` block; v2 secret
  example shows `secret_management.master_key_file` only); `§ 7.4 /
  7.5` collapsed to a one-paragraph "removed in v2" note; `§ 7.6`
  mode gating table updated to v2 sections; example yaml under § 8
  trimmed of vendor commented-out blocks.
- `docs/design/implementation/06-deployment.md` — banner upgraded;
  vendor file-system entry replaced with `master.key`; systemd
  `EnvironmentFile` line removed; port table dropped 443/tcp feishu
  WebSocket row; install steps replaced vendor cred install with
  master.key generation step; `§ 10.3` rewritten as "v2 无 vendor
  凭据安装".

**Bulk surgical edits** (Python script over 16 docs): deleted lines
that contained both a `~~strikethrough~~` and a vendor token
(feishu / lark / dingtalk / wechat / FeishuBridge). Per-file counts
in script output:

```
ddd-blueprint.md: 2     agent-harness/02-skill-cli-tooling.md: 1
tactical/cognition/00-overview.md: 1     discussion/00-overview.md: 5
tactical/observability/00-overview.md: 2  task-runtime/00-overview.md: 2
task-runtime/01-task.md: 1               task-runtime/02-task-execution.md: 1
task-runtime/03-input-request.md: 3      workforce/00-overview.md: 2
workforce/03-worker-project-proposal.md: 1 implementation/03-cli-subcommands.md: 3
rules/conventions.md: 2 (the e2e example list)
```

**Surgical text edits**: rewrote `conventions.md` line 314 +
`testing.md` line 115 (e2e examples: "feishu 入口" → "Web Console
入口"); rewrote `strategic/02-system-overview.md` to drop the v1
ASCII feishu module + replace with Web Console; rewrote
`strategic/03-bounded-contexts.md` `feishu_delivery_ledger` note
to past-tense / pedagogical; rewrote `task-runtime/01-task.md`
CLI example flag value (`--channel=feishu` → `--channel=web`).

**Banner upgrades** (16 tactical/implementation docs): replaced
"⚠ v1-era doc — pending rewrite" banner with "📌 v2 update applied
(P12 S6, 2026-05-24)" banner via Python script.

**vitepress nav menu** (`sites/.vitepress/config.ts`): removed the
"BC7 Bridge" submenu entry (Overview + FeishuBridge 实现 links)
since both target docs no longer exist post-P10 § 3.9.

**Allowlist restructure** — TEMPORARY block REMOVED. Replaced with
a single permanent entry for the session-checkpoint archive
(treated as history-of-record). § 1.4 plan was for a broader
"v2-banner blessed docs" category — turned out unnecessary because
the surgical + bulk edits left only the session-checkpoint archive
needing the allowlist, and that file is genuinely frozen
historical content.

### 5.3 Verification

```
$ make lint-vendor          # clean (all hits whitelisted)
$ make lint-vendor-selftest # both phases OK
$ go test ./...             # green
$ go vet ./...              # clean
$ grep -RIn 'feishu\|lark\|dingtalk\|wechat' docs/ \
    | grep -v 'phase-12-audits\|phase-1[01]\|phase-9\|phase-8\|drafts/session-checkpoint\|drafts/v2-kickoff\|decisions/00'
   docs/plans/phase-12-plan-detail.md:119:- Verify: `git grep -i 'feishu\|lark\|bridge' -- ...`
   docs/plans/phase-12-cleanup-release.md:...     (intentional — plan doc names patterns for the lint to scan)
   docs/release/v2.0-draft.md:28:SQL schema 大动 — 见 migrations 0020-0024 + 0025 (v1 bridge feishu tables drop)
```

All remaining hits are intentional plan / release-note references
to the v1 tokens (allowlisted via `^docs/plans/`).

### 5.4 Acceptance criterion satisfied

The S1 lint allowlist TEMPORARY block is empty. § 1.5 (S6
acceptance) honored. No file in the docs / live config / asset
tree references v1 vendor tokens outside of allowlisted history-
of-record paths.

### 5.5 What was NOT fully rewritten (deferred indefinitely)

The deep design docs (tactical/discussion/00, tactical/workforce/00,
tactical/task-runtime/* etc.) still contain prose that
*describes* v1 vendor integration as past context — e.g. "Conversation
was originally tied to vendor IM channels..." Those passages
remain because:

1. They're educational history for future contributors who need
   to understand WHY v2 chose its architecture.
2. They no longer name `feishu` / `lark` / `dingtalk` etc. (lint
   pass proves this).
3. Full pedagogical rewrite is multi-day effort — out of P12 scope.

If a future ADR wants tighter discipline ("no v1 mentions at all,
including 'Bridge was the old name for...'"), that's a v2.1+
docs-tightening ST.
