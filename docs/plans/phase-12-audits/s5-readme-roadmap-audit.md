# P12 S5 — decisions README + roadmap polish audit

> Run 2026-05-24 · per x9527 oversight: decisions README must list
> the 17 v2 ADRs with their status + key change; roadmap must show
> three explicit columns (v2 shipped / v2.1 backlog / v3 deferred).
> Audit log lands SEPARATELY from the edits.

## § 0. Scope

Two files in this ST:

| File | Current state | Target state |
|---|---|---|
| `docs/design/decisions/README.md` | Already partially updated in S4 commit (paths flipped, statuses set to Accepted, rule-note appendix added). | Verified table sort + status text; add "key change" column to v2 ADR rows; verify rule-note is right. |
| `docs/design/roadmap.md` | v1-era doc with vendor strikethroughs scattered throughout + v2/v3/v3+/长期愿景 sections mixed. | Reorganized into three top-level sections: **v2 已完成** (consolidated, links to ADRs) / **v2.1 backlog** (refs `docs/plans/v2.1-backlog.md`) / **v3 推迟** (everything that was v3 / v3+ / 长期愿景, vendor-clean). |

## § 1. decisions/README.md state after S4

```
$ grep -c '| Accepted' docs/design/decisions/README.md
```

S4 already shipped the path flip + status flip. Remaining work for
S5:

1. Verify all v2 ADR rows actually say Accepted (visual scan).
2. Either add a "key change" column or keep the current single-sentence
   title column. **Decision**: keep current single-sentence titles —
   adding a fourth column would make the table too wide for a 80-col
   terminal render; the title already says the key change ("Worker
   Enroll 轻量化", "AgentInstance 一等公民化"). Document this
   decision here so a future reader knows it was considered.
3. Verify the rule-note appendix is correct and complete.

## § 2. roadmap.md restructure plan

Current ToC:
1. v2（短期，紧接 v1 后） — has vendor-strikethrough entries
2. v3（中期） — large block: AgentImage, Cloud Worker, Web flamegraph,
   Task/Execution扩展, Dispatch可靠性进阶, Supervisor进阶, Conversation
   扩展, Workspace, Observability, 性能优化, DAG, ...
3. v3+ / 低优先级 — Supervisor 自动收敛, Agent 主动加入 Issue, 容器化,
   Prometheus, Per-project 观测扩展
4. 长期愿景 — 多用户 / SaaS

Target ToC:
1. **v2 ✅ 已完成** — top-level list of what shipped in P8-P12; links
   to ADRs.
2. **v2.1 backlog** — references `docs/plans/v2.1-backlog.md` as the
   canonical list; mentions the 2 items there (unread tracking + SPA
   coverage micro-pass) inline.
3. **v3 推迟** — everything currently in v3 / v3+ / 长期愿景 sections,
   cleaned of vendor-strikethrough prose (replace the "~~飞书~~" /
   "(v2 删 vendor)" inline parenthetic notes with vendor-neutral
   language). Existing v3 entries (AgentImage, Cloud Worker, Task
   model 扩展, 等) preserve their content; only the v1-era inline
   notes get rewritten.
4. Internal: keep the "内容维护" section as the editorial rule.

### Specific rewrites

| Before | After |
|---|---|
| `> ⚠ **v1-era doc** — pending v2 update. ...` (line 1 banner) | (deleted — no longer v1-era after this sweep) |
| `### ~~飞书 Slash 命令（高优）~~ → v3+: 外部 IM / 渠道接入重新设计` | Move entire section into the v3 group as "外部 IM / 渠道接入（重新设计）"; drop strikethrough body and Feishu specifics. |
| `### v3+: 外部 IM / 渠道接入重新设计（per ADR-0031 后规划）` (line 47) — duplicate header | Merge into the v3 section described above. |
| `DAG 可视化：Web Console ~~/ 飞书~~ (v2 删 vendor per ADR-0031)` | `DAG 可视化：Web Console`. |
| `直接让 worker agent 在 Issue thread 内发评论` (Agent 主动加入 Issue): "v1 一律推 ~~飞书~~ vendor (v2 删 per ADR-0031) 等用户" → rewrite without ~~. |
| `~~FeishuBridge~~ 单一渠道（v2 删 vendor 集成 per ADR-0031）` | `单一 Web Console 入口（v2 删 vendor 集成 per ADR-0031）`. |
| `[ADR-0017](decisions/0017-task-as-conversation.md) / [ADR-0021]...` references to deleted ADRs | Replace with [ADR-0039](decisions/0039-conversation-business-model-v2-unified.md). |

### v2.1-backlog cross-reference

`docs/plans/v2.1-backlog.md` exists with two entries. S5 roadmap
should point users there rather than duplicate the content.

## § 3. Acceptance criteria

- Audit log committed first (this file).
- Edits committed second.
- `grep -ic 'feishu\|~~.*~~' docs/design/roadmap.md` returns minimal
  hits (only intentional historical references with `(per ADR-0031)`
  annotation, no bare strikethrough).
- `grep -ic 'feishu\|~~.*~~' docs/design/decisions/README.md` returns
  only references to deleted-via-ADR rows (~~0009~~ / ~~0017~~ /
  ~~0020~~ / ~~0021~~ / ~~0022~~ — those ARE deleted and the
  strikethrough is intentional).
- `make lint-vendor` clean.
- `make lint-vendor-selftest` both phases OK.

## § 4. What S5 does NOT do

- Does NOT touch other docs (S6 / S7 scope).
- Does NOT add new ADRs (v2 caps at 0039).
- Does NOT modify the v2.1-backlog.md content itself.

## § 5. Execution log

To be appended by the edit commit.
