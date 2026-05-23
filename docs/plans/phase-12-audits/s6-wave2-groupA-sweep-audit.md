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

To be appended by the sweep commit.
