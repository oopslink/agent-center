# Conversation BC — Directed 消息回复漏发治理:运行时 ReplyGuardrail DDD 设计（草案）

> **DDD 战术层** · 主 BC: Conversation(定向回复语义)· 跨 BC: Agent-Harness/Workerdaemon(turn-end 强制挂点)、Cognition(re-inject nudge 复用 wake 机制)
> 来源 issue-cd968730 / task T341。回答:human-directed 消息被 agent 感知后**漏发回复**时,系统如何在运行时强制 agent 本人补回(方案 A),N 次耗尽后如何兜底(方案 B 仅兜底)。
> 方法论:遵循 [ddd-blueprint](../../../ddd-blueprint.md) + [conventions §0 统一语言](../../../../rules/conventions.md)。结构对齐姊妹篇 [cognition/04-wake-guardrail](../cognition/04-wake-guardrail.md)。
> **状态:草案 — 待 oopslink 过目 §0/§5 决策后再落实现。**

---

## § 0. 设计意图与关键决策

现状:"必回定向消息"只是 system prompt 里的**软契约**(`internal/claudestream/agent_system_prompt.go:47-57` "You MUST reply to every message directed at you")。agent 可以感知一条 human 定向消息(被唤醒 / get_my_unread 列出),却既不 post_message 也不显式拒绝,甚至只 `mark_seen` 就放掉 —— **人就被静默挂起**。本设计把这条软契约硬化成运行时护栏。

| # | 决策 | 理由 |
|---|---|---|
| D1 | **回复义务语义归 Conversation**(DirectedReplyObligation),不新增 BC | "directed message / inbox / reply / mark_seen" 是 Conversation 既有语言;ActorKind 已在此分类 |
| D2 | **强制挂点归 Agent-Harness/Workerdaemon**(AgentController turn-end + TrueIdle) | session 生命周期、turn 边界、re-inject 通道都在 worker controller |
| D3 | **方案 A 优先**:运行时检测 + bounded re-inject nudge 强制 **agent 本人**补回 | 让责任人自己回,语义/上下文最对;符合"agent loop 自驱"模型 |
| D4 | **方案 B 仅兜底**:N 次 nudge 耗尽后,系统在该会话标注"感知未回",**不**替 agent 编造回复 | 保证人不被静默丢;但不伪造 agent 意图,只如实回灌一条系统标注 |
| D5 | re-inject **复用 wake nudge 机制**(`sess.Inject`),阈值进 center settings 并**对齐 WakeGuardrailConfig** | 不造轮子;`agent_controller.go:1028 workAvailableNudge` 已是同款注入 |
| D6 | 只对 **human 作者**的定向消息生效;agent→agent 走 SilentAck **不**纳入(见 §5-①) | 与 wake-guardrail I7-D2 一致,避免把刚熄的 ping-pong 又点着 |
| D7 | 不硬打断:仅在 **turn-end + TrueIdle** 触发,绝不打断在跑的任务 | 对齐"新任务不打断当前任务";避免误伤合法 defer(见 §5-②) |

---

## § 1. 统一语言(Ubiquitous Language)

> 需纳入 strategic/03-bounded-contexts §1 Conversation 切片。

- **DirectedReplyObligation(定向回复义务)**:agent 感知一条 **human 作者**的定向消息(DM 或在 channel/task/issue 被 human @mention)后产生的"必须回复该会话"的义务。承载 `conversation_id / trigger_message_id / actor_kind(=human) / perceived_at / nudge_count / status`。
- **ReplyGuardrail(回复护栏)**:在 turn-end + TrueIdle 时检测未履约义务,驱动 bounded re-inject,耗尽后 Backfill 的策略闸。
- **Perceive(感知)**:系统已把该定向消息送达 agent(被唤醒 / 进入其 inbox 并经过 ≥1 个完成的 turn)。**感知 ≠ 履约。**
- **Discharge(履约/回复)**:感知后向**同一**会话 `post_message` ≥1 次(见 §5-③④)。义务即清。
- **TrueIdle(真 idle)**:agent 既无在飞 WorkItem(`currentTaskID==""`)、无在飞会话(`currentConversationID==""`),也无 queued/runnable 工作 —— 唯一安全的强制时刻。
- **ReplyNudge(补回提示)**:re-inject 给 agent 的短提示,强制其履约或显式拒绝(方案 A)。
- **Backfill(兜底回灌)**:方案 B —— nudge 耗尽后,系统在该会话标注一条"agent 感知未回",使人不被静默丢。
- 行为动词:`Perceive / Discharge / Nudge(补回) / Backfill(回灌) / SilentlyAck(静默确认,仅 agent→agent)`。

---

## § 2. 跨 BC 定位

- **Conversation(主)**:定义"什么算定向消息(human DM / human @mention)"、"什么算已回复(同会话 post_message ≥1)"、义务生命周期(感知即生、履约即清)。`agent_inbox.go` 的 ActorKind 分类 + read-state 复用。
- **Agent-Harness/Workerdaemon(强制挂点)**:`AgentController.onEvent` 在 `ev.Type=="result"`(turn-end,`agent_controller.go:1386` 一带)+ TrueIdle 判定 → 向 center 查询未履约义务 → `sess.Inject(replyNudge)`(对齐 `:1028 workAvailableNudge`),bounded by `max_nudges`。
- **Cognition(复用)**:re-inject 即 wake nudge 的同款下行;阈值进 center settings,wiring 对齐 `wakeguard`(`NewGuardFunc + GetByPrefix + ConfigFromMap + Validate`)。
- Context Map:`Conversation`(义务真值源)→ `Workerdaemon`(turn-end 拉取并注入)→ `Conversation`(Backfill 回写)。

### 2.1 放置方案(turn-end hook vs server sweep)— 待拍板

| 方案 | 机制 | 取舍 |
|---|---|---|
| **A. Worker turn-end hook(主)** | onEvent `result` + 本地 in-flight=空 → 问 center"我欠回复吗 & 我 runnable 为空吗"→ 注入 | turn-end 时机精准;需新增一条 center 查询。task 明确点名"controller turn-end 强制挂点",**推荐为 Phase 1**。 |
| **B. Server 周期 sweep(第二张网,可选 Phase 2)** | 仿 `WakeReconcileLoop`,周期算"desired-running + 无在飞 + 无 runnable + 欠回复"的 agent,下发 reply-nudge 控制命令 | 复用现成 control-command + 幂等设施;**兜住 session 已死/turn-end hook 漏触发**的情形。≤1 tick 延迟。对齐 T335/T337 defense-in-depth。 |

> 推荐:Phase 1 落 A;B 作为后续第二张网(与 A 正交)。下方 §3 以 A 为主线描述,B 复用同一 §3.1 VO / §3.4 事件。

---

## § 3. 战术设计

### 3.1 Value Objects
| VO | 字段 | 不变性 |
|---|---|---|
| **DirectedReplyObligation** | conversation_id, trigger_message_id, actor_kind(=human), perceived_at, nudge_count(≥0), status(pending/discharged/backfilled) | 仅 human 作者定向消息生成;履约=同会话 post_message(ts>perceived_at)→discharged;nudge_count ≤ max_nudges |
| **ReplyGuardrailConfig** | max_nudges, idle_grace_sec, obligation_ttl_sec, nudge_cooldown_sec | 来自 settings;均 >0;有保守默认 |

> **义务状态承载(待拍板,推荐 derived-first)**:
> - (a) **派生/无状态(推荐)**:turn-end+idle 时即时查询"human 定向消息 ∧ 已感知 ∧ 同会话无 ts 更晚的 post_message ∧ 在 ttl 内"。复用 read-state(感知锚点 = 该消息有 wake 投递记录 / 已在 inbox)+ message log(履约 = 之后的 post_message)。**不新增重表**(对齐 wake "不新增重表")。
> - (b) **物化投影**:perceive/discharge 各更新一张小义务表。更显式可观测,但要维护新状态。
> - 关键实现点 = **"感知锚点"**:最稳的信号是"系统**为这条 human 定向消息唤醒过该 agent**"(有 wake record)——无歧义的"系统已告知"。建议据此 + read-state 派生,避免误判 agent 从未被告知。

### 3.2 Domain Service:ReplyGuardrail @ turn-end + TrueIdle
`onEvent` 收到 `result`(turn-end)且 agent TrueIdle 时,按序:
1. **去抖**:距上次 nudge < `nudge_cooldown_sec`,或 idle 不足 `idle_grace_sec` → 跳过(让刚结束的 turn 沉淀,避免连环 nudge)。
2. **查询义务**:向 center 取本 agent 的未履约 DirectedReplyObligation(human 作者 ∧ 已感知 ∧ 同会话未履约 ∧ 在 ttl 内)。空 → 结束。
3. **方案 A — 补回**:对每条 `nudge_count < max_nudges` 的义务 → `sess.Inject(replyNudge(ob))`,`nudge_count++`,发 `conversation.directed_reply.nudged`。首次检出未履约时发 `conversation.directed_reply.missed`。
4. **方案 B — 兜底**:`nudge_count ≥ max_nudges` → 系统向该会话 post 一条标注"agent 感知 trigger_message 但 N 次仍未回复",义务置 backfilled,发 `conversation.directed_reply.backfilled`。**恰好一次**(幂等,不刷屏);**不**伪造 agent 回复内容。

ReplyNudge 文案要点:点名"你欠 conversation C(trigger M)一个回复;现在 idle 了,请按三选一(接受-defer / 接受-现在做 / 拒绝并说明)回复该会话,或确认无需回复的理由"。

### 3.3 Invariants
1. **仅 human 作者**定向消息生成义务;agent→agent 走 SilentAck,**不**生成义务(§5-①)。
2. 护栏**仅在 turn-end + TrueIdle** 触发(无在飞 WorkItem ∧ 无在飞会话 ∧ 无 queued/runnable),绝不打断在跑任务(§5-②)。
3. 履约 = 感知后向**同一**会话 post_message ≥1(ts>perceived_at);**单独 mark_seen 永不履约** human 义务(§5-③④)。
4. **有界**:每义务至多 `max_nudges` 次 re-inject;之后**恰好一次** Backfill。
5. **不静默吞**:missed/nudged/backfilled 每步都有可观测事件。
6. 方案 A(agent 本人补回)是首选;方案 B(系统回灌)**仅兜底**,永不作为第一反应。
7. **人机通路只增强不削弱**:本护栏只**新增**强制,绝不阻断/改写任何消息投递或唤醒。

### 3.4 Domain Events / 可观测(对齐 `cognition.wake.dropped`)
- `conversation.directed_reply.missed`(义务在 idle 时被检出未履约;携 conversation_id / trigger / age)
- `conversation.directed_reply.nudged`(发出 re-inject;携 nudge_count)
- `conversation.directed_reply.backfilled`(兜底标注已回灌)

### 3.5 配置项契约(center settings,前缀 `reply.`;wiring 对齐 `wake.`)
| key | 默认 | 含义 |
|---|---|---|
| `reply.max_nudges` | 3 | 兜底前最大 re-inject 次数 |
| `reply.idle_grace_sec` | 30 | turn-end/idle 后首次 nudge 的去抖 |
| `reply.obligation_ttl_sec` | 3600 | 义务存活时长(超时不再 nudge) |
| `reply.nudge_cooldown_sec` | 60 | 同义务两次 nudge 最小间隔 |

落地对齐 `wakeguard`:`settings.go(ConfigFromMap/ToMap/Validate)` + `webconsole_wiring.go(NewGuardFunc + GetByPrefix("reply."))` + `GET/PUT /api/system/reply-guardrail` handler + 改配置无需重启。

---

## § 4. TrueIdle 判定(强制挂点的核心约束)
- 在飞判定 worker 本地已有:`currentTaskID==""` ∧ `currentConversationID==""`(`agent_controller.go` managedAgent)。
- **queued/runnable 为空**是 server 侧真值 → 方案 A 需 controller 在 turn-end 多问 center 一次(义务查询可与 runnable 查询合并为一条),或交由 §2.1-B 的 server sweep 在服务端直接判。
- 注意与 **defer** 不冲突:agent "稍后做"本身就是一次 post_message(已履约);真正要治的是"感知后既不回也不拒、直到 idle 仍空白"。TrueIdle 闸正是为此 —— agent 还有活时不 nudge(它可能做完再回),活清空了仍欠才算 miss。

---

## § 5. 四个待拍板点 —— 默认决策(本草案取默认并文档化)

| # | 待拍板 | **默认决策** | 理由 |
|---|---|---|---|
| ① | 是否排除 agent→agent SilentAck | **排除**:只有 human 作者定向消息生成义务;agent→agent 可回可不回,`mark_seen` 即可,**不**进护栏 | 对齐 wake-guardrail I7-D2 SilentAck;否则会复活刚被唤醒护栏熄掉的 ping-pong |
| ② | 触发条件 | **仅 TrueIdle**(无 running task / 无 queued work)触发,**不误伤 defer** | agent 忙时"稍后回"是合法状态;只有彻底 idle 仍空白才是真 miss |
| ③ | "已回复"的定义 | 感知后向该 conversation **post_message ≥1 次**;**仅 mark_seen 不算**履约(human 义务) | 目的是人真的收到回复;静默 mark_seen 把人挂起,正是要治的病 |
| ④ | 跨目的地回复是否算 | **默认:必须回到消息来源的同一会话/目的地**;回到别处(如改 DM)**不**算履约 | 人盯着来源会话;回别处不闭环。与现软契约"Reply where the message came from"一致。偏严的安全默认,若实践太死可后续放宽并记 CHANGELOG |

---

## § 6. Out of scope
- agent→agent 回复强制(刻意走 SilentAck,由 wake-guardrail 治理)。
- 改动 wake 投递 / mention 检测(既有路径不变)。
- 主动 get_my_unread 巡检节奏(软契约,不变)。
- 跨 org 治理。
- 方案 B 不伪造 agent 语义回复 —— 只回灌系统标注。
