> ⚠️ **SUPERSEDED (v2.7 #131 carve-out).** The bounded context / feature this ADR decided has been RETIRED in v2.7 (taskruntime/discussion deleted; see sites/designs/v2.7/). Retained as historical record; does not reflect current architecture.

# 0019. BC1 Scheduling + BC4 Execution 合并为 TaskRuntime

> ⚠ **v1-era ADR** — vendor / Bridge / deleted-ADR refs are v1 residue. v2 撤回了 Bridge BC + 飞书集成 (per [ADR-0031](drafts/0031-v2-drop-bridge-vendor-integration.md))；ADR-0017/0021/0022 已被 [ADR-0039](drafts/0039-conversation-business-model-v2-unified.md) supersede + 删除。本 ADR 仍 Accepted（核心结论延续）；只是 vendor 附属内容失效。

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-19 |
| Supersedes-Partial | 原 BC 划分（[03-bounded-contexts](../architecture/strategic/03-bounded-contexts.md) § 2 中 BC1 Scheduling / BC4 Execution 两段） |
| Affects-Wording | [ADR-0010](0010-task-execution-two-layer-model.md) / [ADR-0011](0011-dispatch-reliability-protocol.md) / [ADR-0014](0014-event-sourcing-level.md) / [ADR-0015](0015-agent-trace-not-in-events-table.md) / [ADR-0039](drafts/0039-conversation-business-model-v2-unified.md) (formerly ADR-0017) / [ADR-0018](0018-detached-agent-via-per-execution-shim.md)（仅 BC 措辞同步，不动决策本身） |

## Context

原始 BC 划分（[strategic/03-bounded-contexts](../architecture/strategic/03-bounded-contexts.md) § 2）8 个 BC，其中：

- **BC1 Scheduling**：Task / TaskExecution / InputRequest 聚合的状态机 + 派单协议（dispatch envelope / ACK-NACK / 单活 / retry）+ kill 协议两阶段 + 任务依赖 + 超时策略
- **BC4 Execution**：Worker 机器上的运行时上下文 —— Workspace 物理管理（worktree / direct）、shim 进程（[ADR-0018](0018-detached-agent-via-per-execution-shim.md)）、Agent CLI 子进程生命周期、JSONL trace 解析、Artifact 收集、per-execution 目录、reconcile 上报

P3 战术设计推进到第 1 个 BC（Scheduling）时盘点现 `tactical/scheduling/01-task-model.md`，发现 **6 处协议↔实现的人工分割**：

1. **DispatchEnvelope schema + ACK/NACK 协议**（BC1）vs **worker 端 11 步处理 + env 注入 + shim spawn**（BC4）—— 同一 dispatch 流的"声明性约定"和"物理实施"
2. **Kill 两阶段协议（kill_requested → killed）**（BC1）vs **SIGTERM / 5s grace / SIGKILL 进程级机制**（BC4）—— 同一 kill 动作的两层
3. **Workspace mode VO（worktree / direct 字段）**（BC1）vs **worktree 物理创建 + 24h GC**（BC4）—— 同一 workspace 概念的状态描述和运行时实施
4. **Artifact 归属**：strategic/03 标 Artifact 属 BC4 Execution，但事实上塞在 `01-task-model § 10`（即 BC1 文档内）—— 物理塞错说明边界本来就糊
5. **JSONL trace 解析 + milestone 判定**（BC4 worker daemon 行为）混在 `01-task-model § 3.7 进度上报到 Conversation` —— 进度上报作为 TaskExecution 字段事件 vs worker daemon 实际边解析边判断 milestone，被错塞同一节
6. **Reconcile 协议（worker enroll / 重连 active/stale/unknown 三类回填）**（BC1）vs **worker 端 SIGTERM 本地僵尸 + emit reconcile_stale/unknown**（BC4）—— 同一 reconcile 流的协议层和 worker 端响应

**UL 重合度证据**：`Task` / `TaskExecution` / `dispatch` / `kill` / `workspace_mode` / `artifact` 全部是 BC1 + BC4 共用词；它们不是两个不同领域，而是**同一组业务对象**的"协议视角"vs"运行时视角"。

按 DDD 经典 BC 划分原则（"按 Ubiquitous Language 划"），这两个所谓的 BC 实际上是**同一个 BC 的不同物理表达层**，被错误切成两个。

## Decision

合并 BC1 Scheduling + BC4 Execution → 新 BC **TaskRuntime**（任务运行时）。

**BC 总数 8 → 7**。

新 BC 职责（合并 BC1 + BC4）：
- Task / TaskExecution / InputRequest 聚合的全生命周期
- 派单协议 + 派单可靠性 + 单活约束 + retry 策略
- Kill 两阶段（协议 + 进程级机制）+ Abandon / Suspend / Resume
- 任务依赖（depends_on_task_ids）
- 4 类 timeout 扫描（submitted / execution / input / worker_heartbeat）
- InputRequest 协议 + 三响应路径 + conversation_id 硬规则 fallback
- Worker 侧运行时（workspace 物理创建 / shim 模型 / Agent CLI 子进程 / JSONL 解析 / per-execution 目录 / reconcile worker 端响应）
- Artifact 收集

**Workforce BC 不动**：Worker 注册 / heartbeat / 在线状态 / WorkerProjectMapping / WorkerProjectProposal / Project 元数据 + worker daemon 自身（非 per-execution 的）生命周期 —— 这些是"谁能干活"的元数据管理域，跟 TaskRuntime 是不同关注点。

**BC 编号**：BC1 / BC2 / BC3 保留（BC1 = TaskRuntime；BC2 = Discussion；BC3 = Workforce）；原 BC4 Execution 删除；原 BC5-BC8 编号 -1（BC4 Cognition / BC5 Observability / BC6 Conversation / — v2 删 Bridge per ADR-0031）。

## Rationale

1. **DDD 第一原则**：BC 按 UL 划。Scheduling 和 Execution 的 UL 是同一组词，按 UL 它们应该是一个 BC
2. **物理 split ≠ 概念 split**：状态权威在 center / 实际执行在 worker —— 这是部署形态，不是领域模型。一个 BC 完全可以在多个物理位置上分布式实现（DDD 跟 microservice 不是 1:1）
3. **"协议 vs 实现"不是合法 BC 切分线**：同一域的两个表述层次属于同一 BC 内的不同视角，而不是两个 BC
4. **维护成本证据**：每改一处 dispatch / kill / workspace / artifact / JSONL 细节都得想"是 BC1 还是 BC4"，认知负担显著
5. **现实证据**：strategic/03 中 Artifact 标为 BC4 但实物塞在 BC1 文档 `§ 10`；JSONL 解析（BC4）混在 BC1 `§ 3.7` —— 文档已经事实合编

## Consequences

### Strategic 层

- `strategic/03-bounded-contexts.md` § 2 BC 列表 8→7；BC1 段重写（合并 BC4 内容）+ BC4 段删除；BC5-BC8 顺移
- `strategic/03-bounded-contexts.md` § 3 Context Map 图节点 / 边更新
- `strategic/03-bounded-contexts.md` § 3.1 上下游表合并相关行
- `strategic/01-subdomain-classification.md` 总数 8→7；TaskRuntime 标 Core；Supporting-Essential 由 3 缩为 2（去掉 Execution）

### 战术层

- 新 dir：`tactical/task-runtime/`
- 按 P3 升级模板（见 ddd-blueprint § 3.1）多文件按聚合切：
  - `00-overview.md` —— BC wrap（X.1 聚合清单 / X.2 Invariants 索引 / X.3 Domain Services / X.4 Factories / X.5 Repositories / X.6 跨聚合引用 / 跨 BC 交互 / OOS / Refs）
  - `01-task.md` —— Task 聚合
  - `02-task-execution.md` —— TaskExecution 聚合（含 worker 侧运行时；吸收原 BC4 内容）
  - `03-input-request.md` —— InputRequest 聚合
- 老 `tactical/scheduling/` 删
- `tactical/workforce/01-worker-model.md` carve：BC4 内容迁出到 `tactical/task-runtime/02-task-execution.md § 9-12`；BC3 真内容保留

### 蓝图 P3 模板升级

ddd-blueprint § 3.1 P3 段落升级：从"每个 BC 加一节 § X.1-X.6"升级为"**按聚合骨架重组每个 BC 战术文档 + 补 § X.1-X.6 wrap**"。新模板细则：

- 多聚合 BC（≥2 聚合）：`00-overview.md` + `0N-{aggregate}.md` 多文件
- 单聚合 BC：`00-overview.md` 内合并聚合详情
- 无业务聚合 BC（）：仅 `00-overview.md`，说明"无聚合 / 仅 ACL 翻译职责"
- **明令禁止**：不按主题切（dispatch / kill / workspace / retry / timeout）—— 这就是 BC1+BC4 leaky 的根源

### ADR 措辞

ADR-0010 / 0011 / 0014 / 0015 / 0017 / 0018 + 部分其它 ADR（0007 / 0008 / 0012 / 0013 / 0016）含"Scheduling BC" / "Execution BC" / `tactical/scheduling/...` / `tactical/workforce/...BC4 部分` 措辞 / 路径，统一改 "TaskRuntime BC" / `tactical/task-runtime/...`。**仅措辞同步，不动决策内容**。

## Alternatives Considered

### (A) 保持 3 BC 现状 + 在文档里逐处加 "BC4 → BC1 指针" 注

把 6 处人工分割固化为长期技术债，每改一处仍要决策 BC 归属。**驳回** —— 治标不治本。

### (C) Scheduling + Workforce + Execution 三 BC 合并

Workforce 的 enrollment / project mapping / discovery 跟 task 执行 lifecycle 关系弱（worker enroll 时还没 task 概念；project 是元数据不是动作对象）。过度合并 → 违反 DDD "high cohesion within BC" 原则。**驳回**。

### (D) 解散 BC4 拆给 BC1 + BC3

- 协议 / Task / Execution / Artifact / InputRequest → Scheduling
- worker daemon impl / shim / workspace 物理创建 / JSONL 解析 → Workforce

等价于"承认 BC4 不是真 BC"。**驳回** —— Artifact / JSONL 这种 per-execution 产物塞 Workforce 别扭（它们跟 worker 关系弱，跟 execution 关系强）；边界仍不爽。

## References

- 起源：brainstorming 期 spec（已 git rm；BC 合并决策永久档案以本 ADR 为准）
- DDD 方法论：[conventions § 0](../../rules/conventions.md)
- 蓝图 P3 升级：[ddd-blueprint § 3.1](../ddd-blueprint.md)
- 新战术文档：[tactical/task-runtime/00-overview.md](../architecture/tactical/task-runtime/00-overview.md)
- 受影响 ADR（措辞审）：[0010](0010-task-execution-two-layer-model.md) / [0011](0011-dispatch-reliability-protocol.md) / [0014](0014-event-sourcing-level.md) / [0015](0015-agent-trace-not-in-events-table.md) / ([ADR-0039](drafts/0039-conversation-business-model-v2-unified.md) supersede) / [0018](0018-detached-agent-via-per-execution-shim.md) + 0007 / 0008 / 0012 / 0013 / 0016（路径 / 措辞引用）
