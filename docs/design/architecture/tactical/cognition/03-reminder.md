# Agent 提醒 / 定时 — DDD 设计 (v0.1 · I4)

> 需求:给 agent 设提醒——一次性(精确时间点)或周期(cron),到点**唤醒目标 agent 并注入提醒文本**;owner + agent 都能设(自己/同伴);全量管理(列表/创建/暂停/恢复/编辑/取消 + 下次触发/历史);经 **MCP 暴露给 agent**;需 **暂停** 能力。
> 方法论:遵循本仓 DDD 蓝图(`docs/design/ddd-blueprint.md` + `architecture/`)。
> 状态:设计稿,供讨论;落点已对照现有代码核实。

---

## § 0. 关键决策(先看这里)

| # | 决策 | 理由 |
|---|---|---|
| D1 | **归入 Cognition BC**,不新增 BC | "Wake/唤醒/调度"是 Cognition 的通用语言(已有 `WakeScheduler` 领域服务);ADR-0019 已把 Scheduling BC 合并掉,不再重立 |
| D2 | 新增**聚合根 `Reminder`** + 领域服务 **`ReminderScheduler`** | Reminder 是独立生命周期对象(状态机 + 不变量),与 SupervisorInvocation/Memory 平级 |
| D3 | **投递复用现有 wake 链路**,不另造唤醒机制 | 触发=发一条 system directed message 到 remindee 会话 → 既有 `WakeProjector`(`internal/environment/service/wake_projector.go`)把它变成 `agent.wake`。符合"center 不硬编码调度决策,agent 自行决策"(Cognition 不变量 #2) |
| D4 | **到期扫描复用 outbox `Pump` ticker 模式**(`internal/outbox/pump.go`,现 1s tick) | 仓内唯一既有周期设施;新增 `ReminderTickProjector` 每 tick `FindDue(now)`,不引入新 cron 框架 |
| D5 | **MCP 工具少而清晰**:create / list / get / **update**(暂停·恢复·取消·改期都走 update 的 status/字段) | 呼应正在进行的 MCP 精简(I9/I10)——避免新增 pause/unpause 动词对,用幂等 setter 风格;但不塞"大杂烩" |
| D6 | 触发历史单独 append-only 表 `reminder_firings` | mockup 要"历史触发 + 是否因重叠跳过",events 投影不够细;与 DecisionRecord append-only 风格一致 |

---

## § 1. 战略设计(Strategic)

- **Subdomain 分类**:Supporting(agent 运维便利,非核心域)。
- **BC 归属**:Cognition。
- **Context Map**:
  - `Cognition → Conversation`:提醒投递 = 向 remindee 会话 post 一条 directed message(事件驱动,最终一致,**非同事务双写**,与现有 wake 一致)。
  - `Cognition → Observability`:emit `cognition.reminder.*` 领域事件(审计 + UI 历史)。
  - `Cognition ← Workforce/Identity`:护栏校验(creator 与 remindee 的 project 归属)读 agent→project 映射。

---

## § 2. 通用语言(Ubiquitous Language)切片

- **Reminder**(聚合根):一条"在某时刻/按某周期唤醒某 agent 并投递文本"的指令。
- **Schedule**(VO):`OnceSchedule{at}` | `CronSchedule{expr, timezone}`。
- **Remindee**(VO):被提醒的目标 agent。
- **Creator**(VO):创建者(`user:owner` 或 `agent:<id>`)——审计 + 护栏。
- **ReminderContent**(VO):提醒文本(到点注入给 agent)。
- **ReminderStatus**(枚举):`active | paused | completed | canceled`。
- **EndCondition**(VO):`never | until(date) | max_count(n)`(仅周期)。
- **NextRunAt**(派生):下次触发时刻。
- 行为动词:`Create / Pause / Resume / Update / Cancel / Fire`。

---

## § 3. 战术设计(Tactical)

### 3.1 Aggregate Root:`Reminder`
- **身份**:ULID,不变。
- **字段**:`id, organization_id, project_id, creator_ref, remindee_agent_id, schedule{kind, once_at?, cron_expr?, timezone}, content, status, next_run_at, last_fired_at, skip_if_overlap(bool, 默认 true), deliver_as_creator(bool, 默认 true — v2.11.0 F-B:投递身份开关,ON 时提醒消息以 creator 本人身份发出,OFF 时系统身份;创建时一次性设定,edit 不改), end_condition, fired_count, version, created_at, updated_at`。
  - **投递身份(F-B)**:`deliver_as_creator=true` 时 delivery projector 以 `creator_ref` 身份发 DM;**自提醒**(creator==remindee agent)例外回落系统身份——否则 WakeProjector 的「不唤醒消息自身 sender」规则会让自提醒不唤醒。
- **状态机**:
  - `active ⇄ paused`
  - `active|paused → canceled`(终态)
  - once:`active --Fire--> completed`
  - 周期:`active --Fire--> active`(重算 next_run_at);满足 EndCondition → `completed`
- **Lifecycle ops**:`Create / Pause / Resume / Update(schedule|content) / Cancel / RecordFire`。

### 3.2 Invariants
1. Schedule 合法:`once.at` 创建时须在未来;`cron.expr` 解析校验通过。
2. **护栏**:`agent` 创建者只能给**同 project 的自己/同伴**设(跨 project 拒 `ErrCrossProjectReminder`);`owner` 可跨 project。创建一律记审计。
3. `paused` 不计算 next_run_at、不触发。
4. `skip_if_overlap=true` 时,上一次 fire 引发的处理未结束则**跳过**本次(记 `skipped_overlap`,不堆积)。
5. once 触发一次 → `completed`;周期按 EndCondition 收敛。
6. `canceled|completed` 为终态,不可再改(`ErrReminderTerminal`)。
7. 时区:cron 按 `schedule.timezone` 解释(默认 owner 时区);UI 显示时区。
8. `next_run_at` = f(schedule, last_fired_at, timezone) 纯函数派生。

### 3.3 Domain Service:`ReminderScheduler`
- `FindDue(now) → []Reminder`:`status=active AND next_run_at <= now`。
- **`ReminderTickProjector`**:挂在 outbox Pump tick(复用 D4),每 tick `FindDue` → 对每条 `Fire`。
- **Fire 流程(单事务)**:`RecordFire`(写 last_fired_at + 重算 next_run_at / 置 completed,version CAS)→ 写 `reminder_firings`(outcome)→ `EventSink.Emit("cognition.reminder.fired")`。
- **反循环/节流**:`cognition.reminder.fired` 只路由到 remindee 会话投递,不进 supervisor 自唤醒白名单(对齐 Cognition 不变量 #5);周期最小间隔 + skip_if_overlap 防风暴。

### 3.4 投递(复用既有 wake)
`cognition.reminder.fired` → **`ReminderDeliveryProjector`** → 向 remindee 的 agent 会话(主 DM / 指定会话)post 一条 system directed message(= content) → 触发既有 `WakeProjector` → `agent.wake`。agent 收到后按内容自行决策。

### 3.5 Repository:`ReminderRepository`
- `Save / Update(CAS version) / Get / ListByCreator / ListByRemindee / FindDue(now)`;过滤 status。
- domain errors(sentinel,BC 前缀):`ErrReminderNotFound / ErrReminderTerminal / ErrInvalidSchedule / ErrCrossProjectReminder`。

### 3.6 Factory:`ReminderFactory`
- 校验 schedule + 护栏 → 计算初始 next_run_at → 产出 active Reminder。

### 3.7 Domain Events
`cognition.reminder.created / .paused / .resumed / .updated / .canceled / .fired / .completed`。Observability 订阅(审计 + UI 历史)。

---

## § 4. 持久化(SQLite + golang-migrate)
- `00XX_reminders.up.sql`:
  - `reminders`(ULID PK, version CAS, ISO8601 时间, schedule 以 JSON TEXT 存, 无 FK)。索引:`(next_run_at)`、`(remindee_agent_id, status)`、`(creator_ref)`。
  - `reminder_firings`(append-only):`{id, reminder_id, fired_at, outcome: delivered|skipped_overlap|failed, detail}`。
- 约定沿用:`ExecutorFromCtx`(tx via ctx)、RowsAffected=0 → CAS 失败映射 domain error、append-only 表无 version。

---

## § 5. 适配器 / Open Host(对外能力)

### 5.1 MCP agent 工具(要求①:暴露给 agent)
按现有注册三件套 + parity 测试落地(`mcphost/tools.go` 的 `makeXxx` + `admin/api/agent_tools_reminders.go` handler + `server.go` 路由 + `agent_tools_test.go` parity):
- `create_reminder`(remindee + schedule{once|cron} + content + 选项)
- `list_reminders`(按 创建者/被提醒/状态 过滤)
- `get_reminder`
- `update_reminder`(改 status=**pause/resume/cancel** 或改 schedule/content —— **暂停能力(要求②)走这里**)
- authz:`requireAgentOnWorker(agent_id)` + 护栏(creator=注入的 `cfg.AgentID`,只能同 project 自己/同伴);**不降附件/越权红线**。

### 5.2 Admin HTTP API(web console 用)
`/admin/cognition/reminders` CRUD(非 agent-tools 路径)。

### 5.3 Web UI(按 mockup 实现)
`web/src/pages/reminders/` + `web/src/api/reminders.ts` + 组件(列表/创建表单 once|cron/暂停-恢复/编辑/取消/历史)。**实现对照 mockup**(mockup 截图挂到 FE task,见 §7)。入口位置待 owner 定(rail 一级 / System 二级 / Members tab)。

---

## § 6. 与 MCP 精简(I9/I10)的关系
新工具顺应统一 task 词法 + 幂等 setter 风格:暂停/恢复/取消**不新增动词对**,收进 `update_reminder` 的 status 字段(对齐 I9 "去冗余动词对"、避免大杂烩)。

---

## § 7. 实现拆解(后续开 task 用;**mockup 截图挂到相关 task,实现逐项对照**)
1. **BE-领域**:Reminder AR + VO(Schedule/Status/EndCondition)+ Factory + Invariants + Repo 接口 + domain errors。
2. **BE-持久化/调度**:migration(reminders + reminder_firings)+ sqlite repo(CAS)+ ReminderScheduler + Tick projector + DeliveryProjector + 7 个领域事件。
3. **BE-MCP/API**:4 个 agent 工具 + admin CRUD + 护栏 + parity 测试 + 时区/cron 校验。
4. **FE-UI**(**挂 mockup 截图**):列表/创建(once|cron)/暂停-恢复/编辑/取消/历史,按 mockup 1:1。
5. **验收 run-real**:once + cron 真触发、自我/同伴设、暂停恢复生效、护栏(跨 project 拒)、时区、重叠跳过、MCP 工具逐个真调。

## § 8. Out of scope(本期不做)
跨 org 提醒;完整 RRULE(只 cron);提醒模板;提醒→任务自动化。
