# 0044. Supervisor 砍除（Cognition BC 瘦身，v2.6）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-27 |
| Delivered | v2.6 design phase；详 [v2.6-design § 11.3](../../plans/v2.6-design.md) |
| Supersedes | ~~[ADR-0011 派单可靠性协议](0011-dispatch-reliability-protocol.md)~~（dispatch 中所有 supervisor branch 移除；可靠性机制保留 retry / timeout 部分）<br>~~[ADR-0013 Supervisor 并发模型](0013-supervisor-invocation-concurrency.md)~~（SupervisorInvocation AR 删除）<br>~~[ADR-0029 Supervisor as Built-in AgentInstance](0029-supervisor-as-builtin-agent-instance.md)~~（不再有 built-in supervisor 实例） |
| Related | [ADR-0040 Identity BC carve-out](0040-identity-bc-carve-out.md) / [ADR-0042 Member AR](0042-member-ar.md) |

## Context

v2 时设计的 Supervisor 概念：
- [ADR-0029](0029-supervisor-as-builtin-agent-instance.md) 把 supervisor 实现为 built-in AgentInstance（kind=supervisor）
- [ADR-0013](0013-supervisor-invocation-concurrency.md) 立 SupervisorInvocation AR + 并发模型 + DecisionRecord 审计
- [ADR-0011](0011-dispatch-reliability-protocol.md) Dispatch 协议中包含 supervisor 决策回路

### v2.6 重审 Supervisor 的必要性

@oopslink 2026-05-27 提出："Agent 可以自定义,完全可以按项目自定义一个 agent 来行使 supervisor 得行为 不需要特制一个"。

确实：
1. **AgentInstance 一等公民化**（[ADR-0024](0024-agent-instance-first-class.md)）后，agent 自身就是可任意 launch 的自定义 actor
2. **v2.6 引入 Member.role**（[ADR-0042](0042-member-ar.md)），admin role 给 agent 后，该 agent 可拿到管理级权限完成原 supervisor 行为
3. **supervisor 的核心动作**（task 派单决策 / 失败兜底 / 跨 Issue 仲裁）实际可由用户写一个 agent 监听 channel + 调用 task API 完成
4. **supervisor 用得非常少**：v2.0-v2.5 实际使用中 supervisor 决策回路使用频率极低；维护一个独立的 supervisor 概念性价比低

### 决策出发点

v2.6 周期内**砍除 Supervisor 整个概念**（原计划 v2.6a 独立 cycle，现合并到 v2.6 内，详 [v2.6-design 决策 #17 → revised #27](../../plans/v2.6-design.md)）。

## Decision

### 1. 砍除清单（DDD 视角）

| 删除项 | 所在 BC | 操作 |
|---|---|---|
| **SupervisorInvocation AR** | BC4 Cognition | 删 AR + Repository + Factory + Domain Service |
| **DecisionRecord 子从属 Entity** | BC4 Cognition | 一并删 |
| **5 Domain Services**：WakeScheduler / InvocationFactory / InvocationTimeoutHandler / InvocationCrashRecovery / DecisionWriter | BC4 Cognition | 全删 |
| **AgentInstance.kind=supervisor** | BC3 Workforce | enum 改单值 `agent`；built-in supervisor 注册路径删 |
| **`agent-center supervisor` CLI 子命令** | CLI surface | 删（含 `start` / `stop` / `status` / `logs`） |
| **派单流程中 "supervisor 决策回路"** | BC1 TaskRuntime | DispatchService 中所有 supervisor branch 简化删 |
| **event_type `supervisor.*`** | BC5 Observability | 退役；audit 投射移除 supervisor 维度 |

### 2. BC4 Cognition 调整后形态

```
v2.5 BC4 Cognition (2 AR):
  ├── SupervisorInvocation AR (含 DecisionRecord 子从属)
  └── Memory AR

v2.6 BC4 Cognition (1 AR):
  └── Memory AR
```

- BC4 仍存在；不缩并到其它 BC
- Memory AR + scope walk + git 仓 file-based 完全不动（per [ADR-0012](0012-memory-file-based.md)）
- Cognition BC § X.3 Domain Service 节大幅瘦身（5 个 svc 全删，无剩余）
- BC4 在 v2.6 后约等于"Memory BC"；命名是否改名留 v3+ 再视情况

### 3. 行为替代路径

| 原 Supervisor 行为 | v2.6 替代 |
|---|---|
| Task 派单决策（哪个 worker 跑） | Project / Worker 静态绑定 + admin 主动指定（v2.5 已基本走这条路；supervisor 决策回路实际未广泛使用） |
| Task 失败兜底 / 重派 | TaskRuntime BC 的 DispatchService 自身重试机制（保留 ADR-0011 retry / timeout 部分）；超出范围让用户介入 |
| 跨 Issue 决策 / 优先级仲裁 | 用户自定义 agent（如 `supervisor-mbp`）以 admin role 加入 Org，主动监听 channel + 调用 task API 完成同类行为 |
| Agent provisioning / lifecycle | 已 v2 移到 G1 CLI Endpoint（[ADR-0025](0025-agent-create-via-cli-not-protocol.md)）；与 supervisor 无关 |

### 4. 跨 BC Cleanup 影响

| BC | Cleanup |
|---|---|
| **BC1 TaskRuntime** | DispatchService 移除 supervisor branch；[ADR-0011](0011-dispatch-reliability-protocol.md) 修订（dispatch 可靠性不再依赖 supervisor 决策；retry / timeout 部分保留） |
| **BC3 Workforce** | AgentInstance.kind 枚举改单值 `agent`；built-in supervisor 注册路径删 |
| **BC4 Cognition** | 见 § 2 |
| **BC5 Observability** | event_type 的 `supervisor.*` 全部退役；audit 投射移除 supervisor 维度 |
| **BC6 Conversation** | v2.5 已无 supervisor kind Identity（[ADR-0033](0033-identity-model-refactor.md) 已合并到 agent）；只需 cleanup leftover refs |

### 5. Migration

per drop-and-recreate（[v2.6-design § 9](../../plans/v2.6-design.md)）：

```sql
DROP TABLE supervisor_invocations;
DROP TABLE decision_records;
-- agent_instances CHECK 约束改：kind IN ('agent') only
-- event_type rows 旧 supervisor.* rows 一并清（清库重装时自然消失）
```

### 6. ADR-0011 修订（不完全 supersede）

[ADR-0011 Dispatch 可靠性协议](0011-dispatch-reliability-protocol.md) 不是全文 supersede：

| ADR-0011 内容 | v2.6 处置 |
|---|---|
| Dispatch retry / exponential backoff | 保留 |
| Timeout 处理 + IR / TR 回收 | 保留 |
| Supervisor 决策回路 | **删除** |
| Idempotency token | 保留 |

ADR-0011 在本 ADR ratify 后会做一次 "Amended" status 标注（不撤销，但去除 supervisor branch 部分内容）。

## Consequences

### 正面

- 模型大幅简化：BC4 Cognition 单 AR (Memory)，无 SupervisorInvocation 复杂状态机
- Cognition BC § 1.2 战术设计表大幅缩短
- AgentInstance.kind 单值 `agent`，UI / CLI / observation 全部对称
- 行为替代路径走"自定义 agent + admin role"是更通用的方案，避免 special-case
- 5 个 Domain Service + 2 个表 + 数十处代码引用一次性清理

### 负面 / 待跟进

- **现有 supervisor 实际使用者需迁移**：使用 supervisor 行为的项目改用自定义 agent；v2.6 升级时 release note 明确说明
- **ADR-0011 amend 需 careful**：dispatch retry 机制保留，但 supervisor 决策部分删；ADR-0011 文档要做一次 in-place 修订（标注 amended date）
- **Observability dashboard 投射改写**：Fleet View / 历史 audit 中含 supervisor 维度的投射要清
- **代码 search + 删除**：grep supervisor / SupervisorInvocation / DecisionRecord 等关键字，逐一清理；估算 ~3-5h dev 工作量

## Alternatives Considered

### A. 保留 SupervisorInvocation AR 不用

- ✅ 不动现有
- ❌ 闲置 schema + 概念污染
- ❌ DDD 蓝图里要标注"这个 AR 存在但不用"，认知负担大
- 否决

### B. 简化 SupervisorInvocation 为 fixed singleton

- ✅ 不删 AR，只是收窄
- ❌ 本质上还是特例化 agent，不如全砍
- ❌ singleton 自身就是 anti-pattern
- 否决

### C. 引入 "Coordinator" 新概念取代 Supervisor

- ✅ 概念上"协调者"听起来比 supervisor 中性
- ❌ 增加复杂度而不解决核心问题（agent 已经能自定义 = 重命名也无意义）
- 否决

### D. 留 v2.6a 独立 cycle（原计划）

- ✅ v2.6 周期更小
- ❌ supervisor cut 跟 v2.6 Identity / Member 强相关（admin role 替代 supervisor 行为）
- ❌ 一起做一波 schema 大改更经济
- 否决（[v2.6-design 决策 #27](../../plans/v2.6-design.md)）

## References

### v2.6 ADRs

- [ADR-0040 Identity BC carve-out](0040-identity-bc-carve-out.md)
- [ADR-0042 Member AR](0042-member-ar.md)

### 被 supersede 的 ADRs

- [ADR-0011 Dispatch 可靠性协议](0011-dispatch-reliability-protocol.md) — 部分修订（dispatch retry/timeout 保留，supervisor branch 删）
- [ADR-0013 Supervisor 并发模型](0013-supervisor-invocation-concurrency.md) — 全文 supersede
- [ADR-0029 Supervisor as Built-in AgentInstance](0029-supervisor-as-builtin-agent-instance.md) — 全文 supersede

### 来源

- 2026-05-27 #agent-center DM（@oopslink "agent 自定义 即可行使 supervisor 行为，不需要特制"）
- [v2.6-design.md § 11.3](../../plans/v2.6-design.md)
