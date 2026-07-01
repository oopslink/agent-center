# Memory 聚合（file-based + git 仓）

> **DDD 战术层** · BC: Cognition · 聚合: Memory（独立 AR）

Memory 是 Supervisor 的**持久脑** —— 跨 invocation 累积的笔记 / 经验 / 元认知。**物理形态 = file-based + git 仓**（每 scope 一个 `CLAUDE.md`），不走 DB 表。

详细 ADR：[0012 Supervisor Memory 走 file-based + git](../../../decisions/0012-memory-file-based.md)。

---

## § 1. 存储形态

`$AGENT_CENTER_MEMORY_DIR`（默认 `/var/lib/agent-center/memory/`）整个目录是一个 git 仓，每个 scope 一个 `CLAUDE.md`：

```
$AGENT_CENTER_MEMORY_DIR/         ← git 仓根
├── .git/
├── CLAUDE.md                     # global scope
├── supervisor.md                 # supervisor 自反（不在 ancestor walk 链）
├── projects/<project_id>/
│   ├── CLAUDE.md                 # project scope
│   ├── tasks/<task_id>/CLAUDE.md         # task scope
│   └── issues/<issue_id>/CLAUDE.md       # issue scope
├── workers/<worker_id>/CLAUDE.md
└── conversations/<conv_id>/CLAUDE.md
```

不引入 `agent_memory` SQL 表。无 `expires_at` / `pinned` / `tags` 等管理字段。

---

## § 2. 7 种 scope_kind

| scope_kind | scope_key | 例子 |
|---|---|---|
| `task` | `<task_id>` | "T-42 必须在邻迁后重跳" |
| `issue` | `<issue_id>` | "I-7 用户要求 ASCII art 风格" |
| `conversation` | `<conversation_id>` | "C-3 用户偏好简答" |
| `worker` | `<worker_id>` | "W-1 偶尔丢心跳但主机正常" |
| `project` | `<project_id>` | "X-billing 一律 worktree 起 PR" |
| `global` | `_global_` | "用户在太平洋时区" |
| `supervisor` | `_supervisor_` | "我偶尔过于急于派单，决定多等 30s" |

> **Memory 的 7 种 scope 多于 Invocation 的 5 种 scope_kind**（多 `project` 和 `supervisor`）—— invocation 是唤醒驱动的，没有 "project 事件" 这类；Memory 是经验维度的，可按 project 累积。

---

## § 3. 加载机制：claude code 原生 ancestor walk

Invocation 启动按 scope_kind 设 CWD：

| Invocation scope | CWD | Ancestor walk 自动加载 |
|---|---|---|
| `task:T-42`（project X） | `projects/X/tasks/T-42/` | T-42 + project X + global ✅ |
| `issue:I-7`（project X） | `projects/X/issues/I-7/` | I-7 + project X + global ✅ |
| `conversation:C-3` | `conversations/C-3/` | C-3 + global ✅ |
| `worker:W-1` | `workers/W-1/` | W-1 + global ✅ |
| `global` | `$MEMORY_DIR/` | global ✅ |

把 task / issue 嵌在 project 目录下 → ancestor walk 自动覆盖 task + project + global 三层，**不需要 `@import` 头**。

### 3.1 `supervisor.md` 自反 memory

文件名不是 `CLAUDE.md`，**不在 ancestor walk 路径上**。`supervisor.md` skill 顶部硬指令：

```
Always Read $AGENT_CENTER_MEMORY_DIR/supervisor.md first to recall your meta-cognitive notes.
```

### 3.2 HOME 隔离

Spawn `claude` 时显式设 `HOME` / `CLAUDE_CONFIG_DIR` 到隔离目录，避免吃到 user 私人 `~/.claude/CLAUDE.md` 污染。

---

## § 4. 冷启动骨架（MemorySkeletonFactory）

Center 订阅事件，立即创建对应 `CLAUDE.md` 空骨架（H1 + 空白）并 git commit（author=`system:bootstrap`）：

| 事件 | 创建文件 |
|---|---|
| `project.created` | `projects/<id>/CLAUDE.md` |
| `task.created` | `projects/<X>/tasks/<id>/CLAUDE.md` |
| `issue.opened` | `projects/<X>/issues/<id>/CLAUDE.md` |
| `worker.enrolled` | `workers/<id>/CLAUDE.md` |
| `conversation.opened` | `conversations/<id>/CLAUDE.md` |

骨架内容示例（task scope）：

```markdown
# Task T-42 memory

<!-- supervisor 的笔记写在这里 -->
```

---

## § 5. 写入 / 维护

Supervisor 用原生 `Edit` / `Write` 工具直接改对应文件。**压缩 / rewrite / TTL 全由 supervisor 自决，center 不掺和**。`supervisor.md` skill 给一般性建议（"保持条目简练，过期可摘要"），不设硬 cap。

### 5.1 Git commit 策略（混合）

- Supervisor 在 invocation 内用 `Bash` 调 `git -C $MEMORY_DIR commit -am "..."` 写**语义 commit**（message 说"为什么这样改"）
- Invocation 退出时 center 兜底：仓内有 dirty 变化 → `git add -A && git commit -m "auto: invocation <id> (scope=...) - remaining"`
- Commit author 通过 env 注入：`GIT_AUTHOR_NAME="supervisor"`、`GIT_AUTHOR_EMAIL="supervisor:<invocation-id>@agent-center.local"`
- User 自己 vim + `git commit` 也走同一仓，author=`user:<id>`

### 5.2 并发写：不加锁

共享 scope 文件（`global/CLAUDE.md` / `supervisor.md` / `projects/X/CLAUDE.md`）在跨 scope 并行 invocation 下可能被并发 Edit；接受偶发 race（v1 单用户低频）；v2+ 视场景加 advisory lock（[roadmap](../../../roadmap.md)）。

### 5.3 GC：终态实体永远保留

Task done/abandoned、Issue closed/withdrawn、Worker un-enrolled、Conversation closed 后，其 `CLAUDE.md` **不归档不删除**。markdown 体积极小，留供复盘 + supervisor 跨实体类比。

---

## § 6. Observability：复用现有渠道

不引入 `memory.*` 事件类。变更已有两条审计：

- Supervisor invocation 的 `trace.jsonl.gz`（BlobStore 归档，[ADR-0015](../../../decisions/0015-agent-trace-not-in-events-table.md)）—— claude `Edit` / `Write` 工具调用本身记录在 JSONL 行内（`tool` + `input.file_path`），归档后 `zcat | jq` 即得
- `git log` —— commit message + diff + author，原生 audit

---

## § 7. Memory Invariants

1. **Memory 是 agent 私事**：写入 / 压缩 / TTL 全由 agent 自决，center 不掺和（管理字段不引入）
2. **零 SQL 表**：物理形态固定为文件 + git；不引入 `agent_memory` 表
3. **冷启动骨架同步建**：lifecycle 事件触发后**立即**建空骨架 + commit（避免 supervisor 调用时 file not found）
4. **HOME 隔离**：spawn claude 必显式 `HOME` / `CLAUDE_CONFIG_DIR`，禁污染用户私人配置
5. **终态实体 memory 永远保留**：不归档不删除
6. **commit author 显式注入**：env 区分 supervisor / system / user
7. **Memory 编辑不算 DecisionRecord**：[ADR-0012](../../../decisions/0012-memory-file-based.md) + [ADR-0015](../../../decisions/0015-agent-trace-not-in-events-table.md)：memory 写入由 trace + git log 双渠道审计，不入 decision_records 表

---

## § 8. References

- [ADR-0012 Memory file-based + git](../../../decisions/0012-memory-file-based.md)
- [ADR-0015 agent_trace 不进 events 表](../../../decisions/0015-agent-trace-not-in-events-table.md)
- [00-overview.md § 3 / § 4.2 MemorySkeletonFactory](00-overview.md)
- [conventions § 7 项目本地约定不进 agent-center](../../../../rules/conventions.md) — 跟项目仓自带的 `CLAUDE.md` 区分（worker-side prompt 用项目仓的；本 Memory 是 supervisor 私脑）
- [conventions § 13 安全](../../../../rules/conventions.md) — 备份方案（rsync 或 git remote）
