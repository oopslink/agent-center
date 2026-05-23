# 0025. `agent:create` 协议 = G1 CLI Endpoint；Supervisor 不持创建权

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-24 |
| Delivered | P10 F5 — `bbfa27a` (agent CLI + App wiring + Identity auto-register) |
| Related | v2 议题 G2（[v2-kickoff-2026-05-22 § Group 2](../../drafts/v2-kickoff-2026-05-22.md)）；建立于 [ADR-0024](0024-agent-instance-first-class.md)（AgentInstance 一等公民化）之上 |

## Context

[竞品报告 § 4.3.8](../../../research/competitive-analysis-2026-05-21.md) 介绍 Slock 的 `agent:create` action card —— 让用户/supervisor 都能在 chat 里动态拉起新 agent。v2 kickoff 议题 G2 把这条列入候选，**但 Slock 的实现细节带有 polymorphic actor 假设**（agent / human 平权），这跟 agent-center 的 [domain vision § S4 Center 单一权威](../../architecture/strategic/00-domain-vision.md)直接冲突。

讨论中（2026-05-22 #agent-center）逐步确认：

- AgentInstance 创建是「**资产配置**」决策（跟 Project / Worker 同质），非「任务调度」决策
- S4 不仅约束「不主动造 task」，也约束「不主动造长期实体」—— Supervisor 是一个 agent 实例（[ADR-0003 Supervisor not brain](../0003-supervisor-not-brain.md)），不是「Center 自身」，给它持 propose-entity 权会让「替人盯」的 thesis 失效
- agent-center 已有现成的「surface concern」通道：[Issue](../../architecture/tactical/discussion/00-overview.md)

因此最初设想的「propose-then-commit 两步式协议」（类比 [WorkerProjectProposal](../../architecture/tactical/workforce/03-worker-project-proposal.md)）**被否决**。

## Decision

### 1. agent:create 协议 = G1 endpoint

「agent:create」在 agent-center 的实现就是 [G1 ADR-0024](0024-agent-instance-first-class.md) 给的 endpoint：

- CLI: `agent-center agent create --name=<n> --worker=<id> --agent-cli=<cli> [--max-concurrent=<n>]`
- Service: `AgentInstanceManagementService.Create`（[workforce/00-overview.md § 3.4](../../architecture/tactical/workforce/00-overview.md)）
- Event: `agent_instance.created`

**单步即完成**，不引入 propose-then-commit 中间态。

### 2. 授权矩阵

| 动作 | 允许方 |
|---|---|
| 创建 AgentInstance (`agent create` CLI) | **用户 only** |
| 修改配置 (`agent config set`) | **用户 only** |
| Archive (`agent archive`) | **用户 only** |
| Propose 创建（任何形式的「提议建 agent」实体）| **❌ 不存在此动作** |

**Supervisor 不能：**

- 调 `agent-center agent create` 等命令
- 持有任何形式的「agent 创建权」（直接 / 提议 / 自动）
- 创建跟 AgentInstance 生命周期相关的 Proposal / Suggestion / 等聚合（不存在此类聚合）

**Worker agent 不能**：同上（S4 一致）。

### 3. Supervisor 怎么「surface 需求」

允许 Supervisor 在 [Issue](../../architecture/tactical/discussion/00-overview.md) 中**纯文字**描述「需要一个 X 类 agent / 建议在 worker Y 上装 Z CLI」等观察。

> 「说」≠「建」：Issue 是 surface concern 通道；Supervisor 在 Issue 里指出「需要 reviewer agent」是表达观察，不是创建实体。

具体场景：

- **派单失败（NACK）**：
  - `agent_unavailable` / `capability_missing` / `agent_at_capacity` → Supervisor 决策开 Issue 描述需求
  - Issue 内容 = 纯文字（不引入结构化 `proposed_agent_spec` 字段）
  - 用户在 thread 内讨论 → conclude → 自己决定是否 `agent create`
- **首次 worker.online 该 worker 上无 agent**：Supervisor 可以 surface 但不强制
- **观察到「某 project 缺 agent」**：可在 Issue 中提

> Supervisor 在 Issue 里**不能**模拟「propose-then-commit」（不能给用户列 [接受 / 改后接受 / 忽略] 卡片让用户「commit」一个事先准备好的 agent spec）—— 这种 UX 未来若有需求，可在 presentation 层（Web Console / CLI wizard）做「卡片化」，但**模型层不引入**对应的状态机 / 实体。

### 4. 不引入的东西

| 项 | 不引入理由 |
|---|---|
| `AgentInstanceProposal` 聚合 / 表 | 没 propose 概念 |
| `agent_instance_proposal.proposed / committed / rejected` 事件 | 同上 |
| `Issue.proposed_agent_spec` 结构化字段 | v2 保持简单；Issue 仍是纯文字 surface 通道；未来真有需求再升级 |
| `agent-center agent propose ...` CLI | 不存在 propose 动作 |
| 「supervisor 派单 NACK → 自动 create agent」逻辑 | 跨 S4 红线 |

### 5. 派单失败后的 Supervisor 标准 SOP（写进 supervisor.md skill）

```
派单 NACK reason in {agent_unavailable, capability_missing, agent_at_capacity}:
  1. 不要 retry 同 agent_instance_id（除非 agent_at_capacity 且短暂等待可解）
  2. 评估有无其他 agent_instance 适合
    a. 有 → 重派
    b. 无 → 开 Issue 描述需求
       - opener=supervisor
       - title 含 "需要 agent X / 缺 capability Y" 等
       - 内容描述：task 是什么、为什么当前 agent 不够、建议怎么改（纯文字）
  3. NEVER 调 agent create / agent config set / agent archive
```

## Consequences

**正面**：

- S4 边界更清晰：「任务调度」+「资产配置」双轴都归用户
- 零新增模型 / 表 / 协议 / 事件 —— v2 G2 实质工作量接近为零
- Supervisor SOP 更窄：surface need only；不模糊「建议」与「执行」
- Issue 通道复用率提升

**负面 / 待跟进**：

- Supervisor 派单失败时用户介入路径多一跳（读 Issue → 跑 CLI），UX 不如 Slock 的 action card 顺滑
  - 缓解：v3+ 若用户抱怨可在 Web Console / CLI 加「卡片化 wizard」前端，但**协议层仍不变**（wizard submit 触发的仍是 G1 endpoint）
- Issue 内容是纯文字，conclude 时不能一键 spawn create —— 用户必须手敲 CLI
  - 接受：v2 个人工具规模可接受

## Alternatives Considered

### A. Propose-then-commit 两步协议（前期推荐过，被否决）

- 新聚合 `AgentInstanceProposal`（pending → committed / rejected / superseded / expired）
- Supervisor propose；用户 commit
- ❌ Supervisor 持「propose-entity」权跨 S4 红线
- ❌ 跟 Issue 通道职能重叠（都是 surface concern）
- 否决

### B. Supervisor 直接 create

- ❌ 完全跨 S4 红线
- ❌ 用户失去 agent 实体的掌控权
- 否决

### C. Issue.proposed_agent_spec 结构化字段

- Issue 挂可选结构化 spec，conclude 时一键 spawn create task
- ⚠️ Issue intent 体系升级到结构化是 v3+ 话题
- ⚠️ v2 用户手敲 CLI 不算痛点
- 否决（v2）

### D. Presentation 层卡片 wizard（v3+ 议题）

- Web Console / CLI 提供 form / wizard 加速 G1 endpoint 操作
- ✅ 改善 UX，不动协议层
- 推迟到 v3+（**不在 G2 范围**）

## References

### 同 BC

- [ADR-0024 AgentInstance 一等公民化](0024-agent-instance-first-class.md) —— G1 endpoint 定义
- [workforce/04-agent-instance.md](../../architecture/tactical/workforce/04-agent-instance.md) —— AgentInstance AR

### 跨 BC

- [discussion/00-overview.md](../../architecture/tactical/discussion/00-overview.md) —— Issue 通道
- [cognition/00-overview.md § 7](../../architecture/tactical/cognition/00-overview.md) —— Supervisor 行为边界

### 相关 ADR / Vision

- [ADR-0003 Supervisor not brain](../0003-supervisor-not-brain.md)（Supervisor 是 agent，不是 Center 自身）
- [ADR-0021 Issue as Conversation](../0021-issue-as-conversation.md)
- [Domain Vision § S4 Center 单一权威](../../architecture/strategic/00-domain-vision.md)

### 来源

- [docs/design/drafts/v2-kickoff-2026-05-22.md § Group 2 G2](../../drafts/v2-kickoff-2026-05-22.md)
- [竞品报告 § 4.3.8 + § 5.1](../../../research/competitive-analysis-2026-05-21.md)（Slock action card 在 agent-center 的 reshape）
