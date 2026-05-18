# 0014. 事件溯源走 L1：状态表为权威，事件表是审计流

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-17 |

## Context

[NF9](../requirements/02-non-functional.md) 原文："所有 domain event 进入 append-only 事件表；任何状态都可从事件流重放推导"。[05-observability.md O1](../architecture/tactical/observability/01-observability.md) 也写 "events 表是 source of truth for 重放、审计、跨上下文 join"，Fleet View 段落更明确 "这张表可从 events 表全量重算（崩了能恢复）"。这套措辞指向**严格事件溯源 / CQRS**：事件是底层真相，状态表纯投影。

但同一份设计里：

- `task.status` / `task_execution.status` / `supervisor_invocation.status` / `issue.status` 都是真实存在的状态字段，且被多处直接 UPDATE（[05 O3](../architecture/tactical/observability/01-observability.md)：`supervisor_invocations` 占位行 → 进行中 UPDATE → 结束回填）
- CLI（`inspect` / `query` / `ps`）和 Web Console 是热查询路径，要求"派单后立刻 list 看得到" —— 投影 lag 不可接受
- 跨聚合写入（如关 issue 时 spawn task）若走 CQRS 需要 saga，复杂度爆
- 单用户 / 单 VPS 规模，从未真正"从事件流重建状态"

**话术指向 L3，实操在 L1.5/L2** —— 这种漂移在写 schema 时会撞墙。本 ADR 显式收口到 L1。

## Decision

### 1. 状态表是查询权威

`task` / `task_execution` / `issue` / `supervisor_invocation` / `conversation` / `worker_enrollment` 等都有自己的状态字段；CLI / Web Console / supervisor 查询直接读状态表，**不重放事件**。

### 2. 状态变更与事件 INSERT 在同一事务内

任何 mutate 状态字段的操作必须经统一封装：

```
tx {
  UPDATE <state_table> SET ..., version=version+1 WHERE id=? AND version=?   -- 乐观锁，conventions § 9.0
  INSERT INTO domain_events (id, occurred_at, seq, type, refs, actor, payload, ...) VALUES (...)
}
```

Repository / service 层**禁止**直接 UPDATE 状态字段；走 `mutator(stateChange) → tx { UPDATE + INSERT event }` 单一通道。具体强制手段（lint 规则 / 测试 helper / repository 接口约束）见 [02-persistence-schema.md](../implementation/) (TBD)，必要时回填到 [conventions § 9](../../rules/conventions.md)。

### 3. 事件 schema 只需描述"发生了什么"

事件 payload 满足以下用途即可，**不要求"足以重建状态"**：

- 审计："谁在何时改了什么" + `reason + message`（[conventions § 16](../../rules/conventions.md)）
- Supervisor 输入：`trigger_event_ids` 拉取 → 决策上下文
- 跨上下文 timeline：`inspect <kind>` 时序展示
- 新增投影：实时性要求不高的派生视图（如 Web Console 历史图表）

例：`task_status_changed { from: "open", to: "done", actor: "supervisor:I-3", reason: "completed_normally", message: "execution E-7 finished" }` 即可，无需把 `task` 全字段塞进 payload。

### 4. 灾难恢复走 DB 备份

状态表损坏 → 从 DB 备份（含 events 表本身）恢复。事件流**不**承担"丢了状态表能重新算出来"的兜底承诺。

### 5. CQRS / 严格 ES 显式驳回

未来即使 agent-center 规模化，加投影读模型时也不退到 CQRS。两条延伸路：

- 新增 projection 表 + 同事务三写（主状态表 + projection + event）
- 后台 worker 从 events 慢生成派生视图（接受最终一致性，仅用于非热查询）

不引入"command → event → 投影器异步更新状态"的 CQRS 写路径。

## Consequences

正面：

- **实现简单**：每个写操作一个 tx，ACID 单库搞定，无 saga / 无最终一致性窗口
- **CLI / Web Console 无 lag**：查询直读状态表
- **Schema 演化自由**：事件 schema 不绑死状态结构；状态表 ALTER 不破坏旧事件可读性
- **跨聚合写直接**：tx 内同时改 issue + spawn task 状态 + emit 两个事件
- **跟单库 SQLite 范式贴合**：不需要 outbox / 投影器 / 重放协调器等基建（跨存储 / 跨网络的可靠投递另立 ADR）

负面 / 待跟进：

- **"为什么" 可能查无可考**：若 mutator 封装外漏了直接 UPDATE、或 emit event 那行因 bug 漏掉 → 状态表对得上但事件流缺一条，supervisor / 审计追不回。预防靠**统一 mutator 封装 + 单元测试 + lint 检查**，细则待定
- **事件 schema 没有"自描述状态"承诺**：想加新投影 / dashboard 时若发现旧事件缺字段，只能从那一刻往后跑（不补历史）；接受
- **NF9 / 05-observability / Fleet View 多处措辞要校准**：详见 § 影响范围

## Alternatives Considered

### A. L0：纯状态表 + DB 审计触发器

- Pro: 最简单
- Con: supervisor 无事件输入流（[06 § 2](../architecture/tactical/cognition/01-supervisor-model.md) 的 `trigger_event_ids` 模型成立不了）；CLI timeline / inspect 体验差；跨上下文 join 难
- 不选

### B. L1.5：事件 payload 自律"足以重建状态"

- Pro: 保留"丢状态表能重算"的兜底
- Con: 每条事件 schema 设计负担显著上升（要塞 derived fields / artifact refs / 全字段快照）；实际从不真的重建（DB 备份就能恢复）；保留收益低
- 不选

### C. L2 / L3：CQRS / 严格事件溯源

- Pro: 时间旅行、多读模型、状态完全派生
- Con: 投影 lag 与 CLI / Web Console 即时查询冲突；跨聚合写要 saga；schema 演化痛；v1 单用户单 VPS 完全过度工程
- 不选

## 影响范围

- 改写 [NF9](../requirements/02-non-functional.md)：删 "任何状态都可从事件流重放推导"，改为 "状态表权威 + 事件表审计 / supervisor 输入流"
- 改写 [05-observability.md O1](../architecture/tactical/observability/01-observability.md)：删 "source of truth for 重放、审计、跨上下文 join"，改为 "与状态表同事务双写；审计 / supervisor 输入 / 跨上下文 timeline / 新增投影；状态恢复走 DB 备份"
- 改写 [05-observability.md Fleet View § 数据基础](../architecture/tactical/observability/01-observability.md)：删 "可从 events 表全量重算（崩了能恢复）"，改为 "崩了从 DB 备份恢复"
- 后续 / 待立独立 ADR：
  - agent_trace.event 从 events 表拆出（量级 / 用途差异，避免拖累 events 表索引 / 审计扫描）
  - 跨存储写入 outbox 模式（worker → center 事件、BlobStore.Put 等跨网络 / 跨进程的可靠投递）
  - mutator 封装的强制手段（lint 规则 / repository 接口设计）→ 必要时并入 conventions § 9
- [实现层 02-persistence-schema.md](../implementation/) (TBD)：events 表 schema 不变；状态表加 `version` 列（乐观锁，配合 [conventions § 9.0](../../rules/conventions.md)）
