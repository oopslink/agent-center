# 0012. Supervisor Memory 走 file-based + git，不走 DB 表

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-17 |

## Context

Supervisor 是事件驱动的 LLM agent，每次唤醒 spawn 一个短命 `claude` 子进程；进程结束状态全在外部 —— 需要一个**持久记忆层**让"经验"跨 invocation 延续。

初稿（[06-supervisor-model.md](../architecture/06-supervisor-model.md) TBD-partial）和 [01-bounded-contexts.md § BC5](../architecture/01-bounded-contexts.md) 把 Memory 设计成 SQL 表 `agent_memory`：

- 按 scope（global / project / task / issue / worker / supervisor）分组
- CLI `agent-center memory add / list / prune` 是供 supervisor 调的接口
- Center 在 spawn 前预先按"靠祖 scope 映射"拉 top-N 条拼到 prompt 顶
- Schema 自带 `expires_at` / TTL / pinned 等管理字段

讨论中发现这个设计**跟项目的 AI native 哲学错位**：

- [ADR-0003](0003-supervisor-not-brain.md) 明确"supervisor 是 agent 不是 brain，跟 worker agent 同源（都是 claude 实例）"
- [ADR-0002](0002-no-llm-sdk-use-cli-agents.md) 明确"零 LLM SDK，走 CLI agent 原生能力"

供给 supervisor 的"记忆"如果是 SQL 表，就需要造一层专用 CLI（`memory add / list / prune`）封装。这是把 agent 当成"特殊种类"对待 —— 跟 ADR-0003 的同源哲学冲突。worker agent 用的是项目仓库里的 `CLAUDE.md` / `AGENTS.md` 这种 file-based 记忆（[ADR-0005](0005-project-charter-stays-in-project-repo.md)），supervisor 没理由用一套不同的机制。

更关键的是 **claude code 原生的 `CLAUDE.md` 加载机制完全可以承担这个职责**：

- 启动时沿 CWD 向上扫 `CLAUDE.md`，把链上每一份**自动**拼进 system prompt
- 支持 `@path/...` 跨路径 import
- Agent 用原生 `Read` / `Edit` / `Write` 工具就能管理（压缩 / 改写 / 删除），不需要专用 CLI

继续做 SQL 表 = 重发明一个能力差的 wheel。

## Decision

### 1. Memory 改为 file-based，存储在 git 管理的目录树

**`$AGENT_CENTER_MEMORY_DIR`**（默认 `/var/lib/agent-center/memory/`）是一个 **git 仓**，每个 scope 一个 `CLAUDE.md` 文件：

```
$AGENT_CENTER_MEMORY_DIR/        ← git 仓根
├── .git/
├── CLAUDE.md                    ← global scope
├── supervisor.md                ← supervisor 自反（不在 ancestor walk 链）
├── projects/
│   └── <project_id>/
│       ├── CLAUDE.md            ← project scope
│       ├── tasks/<task_id>/CLAUDE.md     ← task scope
│       └── issues/<issue_id>/CLAUDE.md   ← issue scope
├── workers/<worker_id>/CLAUDE.md
└── conversations/<conv_id>/CLAUDE.md
```

**不引入 `agent_memory` SQL 表**。无 `expires_at` / `pinned` / `tags` 等管理字段。

### 2. 利用 claude code 原生 ancestor walk

每次 invocation 按 scope_kind 设 CWD：

| Invocation scope | CWD | Ancestor walk 自动加载 |
|---|---|---|
| `task:T-42`（project X）| `projects/X/tasks/T-42/` | T-42 + X + global ✅ |
| `issue:I-7`（project X）| `projects/X/issues/I-7/` | I-7 + X + global ✅ |
| `conversation:C-3` | `conversations/C-3/` | C-3 + global ✅ |
| `worker:W-1` | `workers/W-1/` | W-1 + global ✅ |
| `global` | `$MEMORY_DIR/` | global ✅ |

把 task / issue 嵌在 project 目录下 → ancestor walk 自动覆盖 task + project + global 三层，**不需要任何 `@import` 头**。

`supervisor.md` 不在 ancestor walk 路径上（文件名不是 `CLAUDE.md`），靠 supervisor.md skill 顶部硬指令 "Always Read `$AGENT_CENTER_MEMORY_DIR/supervisor.md` first" 拉进上下文。

### 3. HOME 隔离防污染

Spawn `claude` 子进程时显式设 `HOME` 或 `CLAUDE_CONFIG_DIR` 为隔离目录，避免吃到用户私人 `~/.claude/CLAUDE.md`。

### 4. 冷启动骨架由 center 创建

Center 订阅以下事件，发生时立即创建对应 `CLAUDE.md` 空骨架（H1 标题 + 空白）并 git commit（author=`system:bootstrap`），保证 invocation spawn 时 CWD 一定存在：

- `project.created`     → `projects/<id>/CLAUDE.md`
- `task.created`        → `projects/<X>/tasks/<id>/CLAUDE.md`
- `issue.opened`        → `projects/<X>/issues/<id>/CLAUDE.md`
- `worker.enrolled`     → `workers/<id>/CLAUDE.md`
- `conversation.opened` → `conversations/<id>/CLAUDE.md`

### 5. 写入 / 维护全由 agent 自决

Supervisor 用原生 `Edit` / `Write` 工具直接改对应 scope 的 `CLAUDE.md`。

**压缩 / rewrite / TTL 由 supervisor 自决，center 不掺和**。supervisor.md skill 给一般性建议（"保持条目简练，过期可摘要"），不设硬 cap、不做后台 compaction。理由：supervisor 是 agent，跟 worker agent 在 worktree 内自管 `CLAUDE.md` 一个套路。

### 6. Git commit 走"混合"策略

- 在 invocation 内 supervisor 可用 `Bash` 调 `git -C $MEMORY_DIR commit -am "..."` 写**语义 commit**（message 说"为什么这样改"）
- Invocation 退出时 center 兜底：若仓内仍有 dirty 变化 → `git add -A && git commit -m "auto: invocation <id> (scope=...) - remaining"` 一次性收尾
- Commit author 通过 env 注入：`GIT_AUTHOR_NAME="supervisor"`、`GIT_AUTHOR_EMAIL="supervisor:<invocation-id>@agent-center.local"`
- User 自己 vim + `git commit` 也走同仓，author=`user:<id>`

### 7. 并发写：v1 不加锁，接受偶发 race

共享 scope 文件（`global/CLAUDE.md` / `supervisor.md` / `projects/X/CLAUDE.md`）在跨 scope 并行 invocation 下可能被并发 Edit。v1 单用户 / 低频，丢一条 memory 可接受；v2+ 视场景加 advisory lock（[roadmap](../roadmap.md)）。

### 8. Observability：不引入 `memory.*` 事件类

Memory 变更已有两条审计：

- 现有 `agent_trace.event` —— claude `Edit` / `Write` 工具调用本身就被记录（`payload.tool` + `payload.file_path`），filter 即得
- 新增 `git log` —— commit message + diff + author，原生 audit

不重复发明 `memory.recorded` / `memory.pruned` domain event。

### 9. GC：终态实体的 memory 文件永远保留

Task 走 done/abandoned、Issue closed/withdrawn、worker un-enrolled、conversation closed 后，其对应 `CLAUDE.md` **不归档不删除**。markdown 体积极小，留下供复盘和供 supervisor 跨实体类比（"上一个类似 task 怎么处理的"）。

## Consequences

正面：

- **跟 ADR-0002 / 0003 / 0005 哲学贯通**：Supervisor 用 agent 原生工具管自己的记忆，跟 worker agent 用 `CLAUDE.md` 管 project 约定是同一个套路
- **认知成本低**：supervisor 不需要学 `agent-center memory add` 专用 CLI；它已经会 `Read` / `Edit` / `Write`
- **claude code 原生加载免费用**：ancestor walk 自动把多层 scope 拼进 prompt，零额外代码
- **审计天然**：git log（含 diff / author / message） + agent_trace 双通道，不需要专门设计 audit schema
- **可读 / 可手改**：人直接 `cat` / `vim` / `grep`，不依赖工具就能查阅 / 修改
- **备份简单**：rsync 或推 git remote

负面 / 待跟进：

- **跨 BC 共享或聚合查询变难**：要回答"supervisor 关于 project X 累积了哪些经验"得 `grep -r` 或扫文件树；SQL `WHERE` 查询不可用。v1 单用户单 center，可接受
- **并发写 race**：跨 scope 并行 invocation 同改 `global/CLAUDE.md` 等共享文件时存在丢写风险；接受 + 列 [roadmap](../roadmap.md)
- **跨机器同步麻烦**：v1 单 center 单 VPS，文件即权威，OK；多 center 场景需要 git push/pull 或更复杂同步
- **Web Console 渲染要文件遍历**：不能 `SELECT`，要 fs walk + render markdown；实现上稍复杂但不阻塞
- **CLAUDE.md / `supervisor.md` 体积无硬 cap**：依赖 supervisor 自觉压缩；若 supervisor 不压缩，文件涨大会进 prompt 占 context。v1 200k context 容忍度高，可接受；监控告警可加在 [roadmap](../roadmap.md)
- **目录骨架创建依赖事件订阅器**：实现层需要在 BC2 / BC3 / BC7 等事件发出后 hook 进创建逻辑

## Alternatives Considered

### A. SQL 表 `agent_memory`（初稿）

- Pro: SQL 查询强、TTL / pinned 字段方便管理、跟其他 BC 表统一存储介质
- Con: 跟 ADR-0002 / 0003 哲学错位 —— 给 agent 造专用 CLI 抽象；丢失 claude code 原生 `CLAUDE.md` 加载能力；agent 自然 `Edit` 改不到
- 不选

### B. 当前方案：file-based + git 仓

- Pro: 跟 agent 原生工具 / 项目哲学贯通；零造轮子；多渠道审计；可读可手改
- Con: 跨 BC 聚合查询弱、并发写 race；可接受

### C. 混合（SQL 表存 metadata + 文件存内容）

- Pro: 兼顾 SQL 查询 + 文件可读
- Con: 双系统一致性问题（文件改了 metadata 漏更新）；复杂度高于 A 或 B 单一方案
- 不选

## 影响范围

- 重写 [architecture/06-supervisor-model.md](../architecture/06-supervisor-model.md) § 4 Memory 章节（TBD-partial → Draft）
- 更新 [architecture/01-bounded-contexts.md § BC5](../architecture/01-bounded-contexts.md)：Memory 仍是聚合，注明物理形态 = file-based git 仓
- 更新 [architecture/01-bounded-contexts.md § 1.1](../architecture/01-bounded-contexts.md)：Memory 术语 description 加 "存储形态：file-based git 仓"
- 更新 [architecture/05-observability.md](../architecture/05-observability.md)：删 `memory.recorded` / `memory.pruned` 事件类；§ O3 Memory 子节改为引用本 ADR
- 更新 [architecture/08-prompt-assembly.md](../architecture/08-prompt-assembly.md)：worker prompt 不再注入 supervisor memory；删 `memory_refs`；supervisor 自己的 prompt 组装走 06 § 3
- 更新 [architecture/02-task-model.md § 4.1 DispatchEnvelope](../architecture/02-task-model.md)：确认无 `memory_refs` 字段（已无）
- 更新 [requirements/03-out-of-scope.md](../requirements/03-out-of-scope.md) 或 [roadmap.md](../roadmap.md)：列"Memory 跨 BC 聚合查询" / "Memory 并发写加锁"等推迟项
- 实现层：删 `agent_memory` 表的 schema 规划；加 `MemoryFileRepo` 抽象（封装目录骨架创建 + git ops）
- 删除 `agent-center memory list / add / prune` CLI 命令规划（不再实现）；保留 `agent-center record-decision`（属 DecisionRecord，[ADR](0013-supervisor-invocation-concurrency.md) 之外的事）
