# 0049. Agent BC：long-running Agent, no AgentRun（v2.7）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-29 |
| Delivered | v2.7 design phase；详 [v2.7-domain-refactor-plan § 2.3 / § 2.4 / § 3.2 / § 5 C](../../plans/v2.7-domain-refactor-plan.md) |
| Supersedes | amends [ADR-0024 AgentInstance 一等公民化](0024-agent-instance-first-class.md) / [ADR-0029 Supervisor as Built-in AgentInstance](0029-supervisor-as-builtin-agent-instance.md)（v2.7 Agent 升为独立 BC，逻辑长驻，取消 run 概念） |
| Related | [ADR-0046 ProjectManager BC](0046-projectmanager-bc.md)（Task 指派来源） / [ADR-0050 Environment BC](0050-environment-bc.md)（Worker 绑定 + 进程控制） / [ADR-0051 ClaudeCode 契约](0051-claudecode-headless-contract.md) |

## Context

v2 的 AgentInstance（[ADR-0024](0024-agent-instance-first-class.md)）偏向"执行实例 + 每次执行"的模型。v2.7 把 Agent 视为**逻辑长驻**的产品实体：它持续在线、接收工作、产生活动，而不是"一次运行"。需要明确 Agent 的 BC 边界、聚合、生命周期，以及"工作项"与"运行/进程"的区分。

## Decision

### 1. Agent 为独立 BC，逻辑长驻

- Agent 是独立 BC；逻辑上长期存在。
- **取消核心 AgentRun 概念**：不存在"运行/run"作为业务 AR。可观测性 = Agent 状态 + 活动，而非运行态。

### 2. 聚合 / 流

| AR / Stream | 职责 |
|---|---|
| Agent | profile（name/description/model/CLI/env）、skill 列表、runtime config（绑定 Worker、home、memory）、生命周期意图 |
| AgentWorkItem | 派给 Agent 的工作队列项（非进程/运行） |
| AgentActivityEvent | append-only 活动/进度/状态观测流 |

- AgentWorkItem 以 URI/ID 引用 Task，但**不拥有 Task 状态**。

### 3. AgentWorkItem 状态机（plan § 2.4）

```text
queued → active → waiting_input → active
active → blocked
active → done
active → failed
queued/active/waiting_input/blocked → canceled
queued/active/waiting_input/blocked → superseded
```

- Task 指派给 Agent → 建 AgentWorkItem；重派 → 旧的 `superseded`、新建一个；Task 保留单条稳定 Conversation（[ADR-0047](0047-conversation-owner-ref-and-context-refs.md)）。
- 一个 AgentWorkItem 可多 AgentInteraction（逻辑回合，非进程）；首版 Message 需 `work_item_ref`，interaction 细节落 AgentActivityEvent。

### 4. 可用性派生（plan § 10 OQ2）

`Agent.availability` ∈ `available | busy | unavailable`，派生、不存、不进不变量；Worker.status 优先级最高（详 [ADR-0050](0050-environment-bc.md)）。派发器只往 available 推。

### 5. 生命周期与 runtime config

- AppServices：CreateAgent（必选 Worker）、StartAgent、StopAgent、RestartAgent、ResetAgent、UpdateRuntimeConfig、EnqueueWorkItem、MarkWaitingInput、CompleteWorkItem、SupersedeWorkItem。
- CreateAgent 必须选 Worker；**Worker 绑定 v2.7 不可变**（换 Worker = 新建 Agent）。
- StopAgent 是操作态，**不**自动把活跃 WorkItem 置 blocked；blocked 是显式业务态（需消息/原因）。
- ResetAgent 有 scope，每次 reset 二次确认。UpdateRuntimeConfig 不自动重启，下次重启生效。
- runtime home：`~/.agent-center/workers/{worker_id}/agents/{agent_id}/`，下含 `config/ logs/ tmp/ memory/ workspace/`；默认 cwd = `{home}/workspace`（plan § 10 OQ7）。v1 不配置 `work_dir`。
- Agent degraded 态 v1 不做。

### 6. MCP 能力面与域隔离

Agent 经 Worker MCPHost 调 AppService 工具（[ADR-0051](0051-claudecode-headless-contract.md)），全局护栏：所有操作限本 Project/Org（plan § 10 OQ4 / OQ6）。

## Consequences

### 正面

- "长驻 Agent + 工作队列 + 活动流"模型贴合实际，去掉了 run 这层人造概念。
- WorkItem/Interaction 区分让多轮任务执行与重派分段清晰。

### 负面 / 待跟进

- Worker 绑定不可变 = 换机/上云需新建 Agent + 数据迁移，列入 roadmap（cloud Agent 实体、memory/home 云存储、Worker failover）。
- 取消 AgentRun 后，"历史运行审计"靠 AgentActivityEvent 重建，需保证事件足够完备。

## Alternatives Considered

### A. 保留 AgentRun/Execution AR

- ❌ Agent 逻辑长驻，run 概念与之冲突、徒增状态。否决，用 WorkItem + ActivityEvent。

### B. 把 WorkItem 放进 ProjectManager（Task 内）

- ❌ 指派/队列是 Agent 域职责；放 ProjectManager 会让 Task 同时承载工作管理与执行队列。否决，跨 BC 引用。

### C. Worker 绑定可变（v2.7）

- ❌ 涉及 runtime 数据迁移/同步，复杂度高。v2.7 不做，列 roadmap。

## References

- [v2.7-domain-refactor-plan § 2.3](../../plans/v2.7-domain-refactor-plan.md) / [§ 2.4](../../plans/v2.7-domain-refactor-plan.md) / [§ 3.2](../../plans/v2.7-domain-refactor-plan.md) / [§ 4.3-4.4](../../plans/v2.7-domain-refactor-plan.md) / [§ 5 C](../../plans/v2.7-domain-refactor-plan.md) / [§ 10 OQ2/OQ4/OQ7](../../plans/v2.7-domain-refactor-plan.md)
- [ADR-0046](0046-projectmanager-bc.md) / [ADR-0050](0050-environment-bc.md) / [ADR-0051](0051-claudecode-headless-contract.md)
- 来源：2026-05-27 ～ 2026-05-29 DM 设计讨论（@oopslink ↔ @AgentCenterPD）
