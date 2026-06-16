# Cognition BC — Agent↔Agent 唤醒护栏 DDD 设计(I7)

> **DDD 战术层** · 主 BC: Cognition(唤醒/调度)· 跨 BC: Conversation(mention 语义)
> 来源 issue I7(issue-f9d15cc5)。回答:agent @ 另一个 agent 时,被 @ 的 agent 如何响应,以及如何防止 A@B→B@A→… 唤醒风暴。
> 方法论:遵循 [ddd-blueprint](../../../ddd-blueprint.md) + [conventions §0 统一语言](../../../../rules/conventions.md)。统一语言术语见 §1,需回填 [03-bounded-contexts §1 UL](../../strategic/03-bounded-contexts.md)。

---

## § 0. 设计意图与关键决策

| # | 决策 | 理由 |
|---|---|---|
| D1 | **唤醒护栏归 Cognition**(WakeScheduler 扩展),不新增 BC | "Wake/唤醒/调度"是 Cognition 既有语言;护栏是唤醒投递路径上的策略闸 |
| D2 | mention 的**响应语义**归 Conversation(跨 BC) | mention 检测/inbox 在 Conversation;"可回可不回 + 静默确认"是会话侧语义 |
| D3 | human 唤醒**绕过护栏**(必达),仅 agent→agent 唤醒受闸 | 护栏只为治 agent 间风暴,绝不误伤人机通路 |
| D4 | 护栏阈值全部**可配置(center settings)**,默认保守 | 暴露到 System/Settings(I7-D3);可按真实负载调参 |
| D5 | 不硬打断:被 @ 到**停顿点**处理 | 对齐现有 agent loop"新任务不打断当前任务" |

---

## § 1. 统一语言(Ubiquitous Language)

> 需纳入 strategic/03-bounded-contexts §1 Cognition 切片。

- **WakeChain(唤醒链)**:由一次源头消息触发、经 agent 互相唤醒形成的因果链。承载 `root_message_id / depth / members / token_budget_remaining / actor_kind`。
- **WakeDepth(唤醒深度)**:当前唤醒在链中的层级(源头=0,每被下一跳唤醒 +1)。
- **WakeGuardrail(唤醒护栏)**:唤醒投递前的策略闸集合(四闸)。
- **CycleDetection(环检测)**:识别同一对 agent 在窗口内往返唤醒的熔断闸。
- **WakeRateLimit(唤醒频控)**:每 agent「被 agent 唤醒」的令牌桶限流。
- **WakeTokenBudget(唤醒成本预算)**:一条唤醒链可消耗的 token 上限,沿链继承扣减。
- **MentionActorKind**:mention 发起者类型 `human | agent`(决定是否受护栏)。
- **SilentAck(静默确认)**:被 agent @ 后"已读但按内容判定无需回复",直接 `mark_seen` 不产出消息。
- 行为动词:`Wake(唤醒)/ Throttle(限流)/ Trip(熔断)/ Drop(丢弃唤醒)/ SilentlyAck(静默确认)`。

---

## § 2. 跨 BC 定位

- **Cognition(主)**:WakeScheduler 在派生/投递 wake 前执行 WakeGuardrail;维护 WakeChain 元数据与 trace。
- **Conversation(协作)**:mention 检测(`mention.Present`)+ agent_inbox 过滤已存在;新增"agent mention → 注入 SilentAck 语义提示";向 Cognition 透传 `MentionActorKind`。
- Context Map:`Conversation → Cognition`(mention 事件携 actor_kind)/ `Cognition` 内部 WakeScheduler 决策。

---

## § 3. 战术设计

### 3.1 Value Objects
| VO | 字段 | 不变性 |
|---|---|---|
| **WakeChain** | root_message_id, depth(≥0), members(set<agentId>), token_budget_remaining(≥0), actor_kind | 子唤醒继承父链:depth+1、members∪{next}、budget-=cost;actor_kind 沿链不变 |
| **WakeGuardrailConfig** | max_depth, cycle_window, cycle_threshold, rate_per_min, chain_token_budget | 来自 settings;均 >0;有保守默认 |
| **MentionActorKind** | enum(human/agent) | 闭集 |

### 3.2 Domain Service:WakeScheduler 扩展 — WakeGuardrail 四闸
派生一个 agent→agent 唤醒前,按序判定(任一命中 → **Drop** 该唤醒并记 trace,不投递):
1. **深度闸**:`chain.depth > max_depth` → Drop(默认 max_depth=4)。
2. **环检测闸**:同一对 (from,to) 在 `cycle_window` 内往返次数 ≥ `cycle_threshold` → Trip 熔断(默认窗口 5min / 阈值 3)。
3. **频控闸**:目标 agent「被 agent 唤醒」令牌桶超 `rate_per_min` → Throttle(默认 10/min)。
4. **预算闸**:`chain.token_budget_remaining ≤ 0` → Drop(链耗尽)。

**actor 区分**:`actor_kind=human` 的唤醒**不进四闸**(必达);`agent` 全程受闸。

### 3.3 Invariants
1. human 发起的唤醒永不被护栏 Drop/Throttle/Trip。
2. WakeChain.depth 单调递增;members 单调增(用于环检测)。
3. 被 Drop 的唤醒必须留 trace(命中哪闸 + 链快照),不静默吞。
4. 护栏只影响 **agent→agent** 唤醒的投递,不改变 mention 检测/inbox 授权。
5. 阈值缺省即生效(settings 缺失用保守默认),不因配置缺失而关闭护栏。

### 3.4 Domain Events / 可观测
- `cognition.wake.dropped`(reason: depth|cycle|rate|budget, chain 快照)— 供 Observability 投影 + 调参。
- WakeChain trace 串入现有 agent_trace/事件体系(不新增重表)。

### 3.5 配置项契约(供 I7-D1 settings + I7-D3 Settings UI)
| key | 默认 | 含义 |
|---|---|---|
| `wake.max_depth` | 4 | 唤醒链最大深度 |
| `wake.cycle_window_sec` | 300 | 环检测窗口(秒) |
| `wake.cycle_threshold` | 3 | 窗口内同对往返熔断阈值 |
| `wake.rate_per_min` | 10 | 每 agent 被 agent 唤醒频控 |
| `wake.chain_token_budget` | (按部署调) | 单链 token 预算 |

### 3.6 Conversation 侧:SilentAck 语义
被 **agent** @mention 时,注入提示:"按内容判定是否回复;无需回复则 `mark_seen` 静默确认"。human directed message 维持"必回"。不硬打断,到停顿点处理。

---

## § 4. Out of scope
跨 org 唤醒治理;agent 发起 DM(见 [issue I3]);mention 之外的唤醒源(plan dispatch / input_request 等,既有路径不变)。
