# Executor completion: supervisor-judged (remove auto-writeback "binding")

**issue-68ccb310 (P60 blocker) · option (b) · 2026-07-06**

## 问题

当前(main `e7570fc9`)一个 forked executor 完成后走 **auto-writeback**:
executor 退出 → `agentruntime/executor/monitor.go` `Monitor.Finalize` → `agentruntime/orchestrator/writeback.go`:

- `OutcomeSucceeded` → `reportSuccess` → **`CompleteTask`**（自动完成 task）
- `Failed`/`Crashed` → `reportFailure` → **`BlockTask`**

两个根本毛病:

1. **binding bug** —— 当某 task 的"executor"实际绑在 agent 的 **supervisor 会话**上（没真正 fork 出隔离进程），该会话的一次**普通 turn-success** 被 Monitor 当成 executor 的 `OutcomeSucceeded` → 自动 `CompleteTask`，**零交付也完成**（dev3 在 P60 round 1–3 反复踩，`completed_by=assignee`、feat 分支零 commit）。
2. **完成不交付** —— writeback 只看 exit outcome（exit0 + 有效 output.json），**不看真实交付**（有没有 commit / push / 达成任务）→ 一个没干活的 run 照样被判 Succeeded → Complete。

## 目标（option b）

**去掉 auto-writeback。executor 完成后,由 supervisor（持 MCP 工具的 LLM 会话）复核结果 + 自主调 MCP `complete_task` / `block_task`。** 保留 executor 的 F1 隔离（executor 仍不连 center）。

这同时根治两点:完成成为 supervisor 的**显式、判交付**的决定,不再有"任何 turn-success = task 完成"这条缝。

## 设计

### 1. 核心改动 — `orchestrator/writeback.go`

`reportSuccess` / `reportFailure` **不再直接 `CompleteTask`/`BlockTask`**,改成把 executor 结果**注入 supervisor 会话**作为一次"裁决"turn,prompt 形如:

> 你 fork 的 executor（task `<ref>`）已结束:outcome=`<succeeded|failed|crashed>`,summary=`<...>`,分支=`<branch>`。**复核它的真实交付**（git diff / 是否 push / 是否达成任务）后:交付了 → 调 `complete_task`;没交付或失败 → 调 `block_task(reason)`。

- 无 task 的纯 chat work-item:`relayToChat` 路径**不变**。
- `reportUsage`（token 用量上报)**不变**。

### 2. 注入机制（wiring）

- `CenterWriteback` 加一个 inject seam:`inject func(ctx context.Context, text string) error`。
- 在 `executor_runtime.go` 构造 `NewCenterWriteback` 时,从 `LocalRuntime` 传入一个"把文本注入 supervisor 会话"的 func —— **复用现成的 `sess.Inject`**（`local_runtime.go` 里 work/wake/converse 已走这条注入路径）。
- `reportSuccess`/`reportFailure` → 组裁决 prompt → `w.inject(ctx, prompt)` → supervisor 处理该 turn。

### 3. supervisor 行为 — system prompt

给**并发/orchestrator agent** 的 system prompt（`internal/claudestream/`,受 `concurrencyEnabled` 控制）加一段:

> 你 fork 的 executor 结束时会收到一条裁决通知。复核它的**真实交付**（git diff / 是否 push / 是否达成任务）:交付了调 `complete_task`,没交付或失败调 `block_task(reason)`。**不要机械按 exit 完成** —— 没产出就 block、让它可重试。

让"判交付"成为 LLM 的显式决定 —— **顺带根治"完成不交付"**。

### 4. 低频心跳兜底 — reconcile（oopslink 提）

单发注入可能漏（supervisor 忙 / crash / 漏判)→ 没兜底 task 会永久卡。加一层**低频 reconcile**,复用 runtime 现有 per-agent **Tick**（lease 续约 + GC)+ executor **Monitor**,不新造循环:

1. **裁决漏了**:executor 已 finalize、裁决已注入,但 task 超阈值（如 2–3 min)仍未 complete/block → **重注入 nudge**;超 N 次（如 3）仍不判 → 升级 `block_task(「supervisor 未裁决,需关注」)`,**不丢**。
2. **executor 挂/超时**:run 超时无产出 → 按 crashed 处置（Monitor 已管一部分,补齐）。
3. 状态:runtime 内维护一个 per-agent 的「已 finalize、等 supervisor 裁决」pending 集,Tick 扫它。

**效果**:把老 auto-writeback 的"总会完成"可靠性,换成 **"supervisor 自主判 + 心跳兜底重驱"** —— 既是要的判交付,又不会因一次漏注入而永久卡。

## 保留 / 不破

- executor **F1 隔离**不破（executor 仍不连 center）。
- **双写防护**:注入是幂等 prompt;supervisor 对一个 task 只调一次 MCP;心跳 nudge 幂等（task 已终结就不再 nudge）。
- **boot/recovery**:重启后 pending 裁决集能重建（从"executor 已 finalize 但 task 未终结"推导)。

## 涉及文件

- `agentruntime/orchestrator/writeback.go` —— 去 auto-complete、加 inject seam + 裁决 prompt
- `agentruntime/executor_runtime.go` —— wire inject seam + pending 集 + Tick reconcile
- `agentruntime/local_runtime.go` —— Tick 挂 reconcile;暴露 session inject func
- `internal/claudestream/` —— orchestrator system prompt 加裁决段
- `agentruntime/executor/monitor.go` —— 超时→crashed 兜底（若未覆盖)
- 相应单测 + 一个真跑 e2e

## 真跑验收（硬要求）

隔离全真实例,fork 真 executor:

1. **有交付**（executor 真 push commit)：supervisor 收裁决 → 调 `complete_task` → task completed。
2. **零交付**（executor 退出但没 commit)：supervisor → `block_task` → task blocked/可重试。**这条是根治"完成不交付"的证据。**
3. **裁决漏**（模拟 supervisor 没处理)：心跳 Tick 重注入 → 补判;超 N 次 → 升级 block。

命令 + 原始输出 + 截图/日志归档。

## 开放点（实现时定）

- 裁决 prompt 带多少 executor 产出信息（summary + git 状态提示?executor 是否自报 push?）。
- 阈值 / N 次（裁决超时、nudge 上限)默认值。
- 注入到 BUSY 会话的排队语义（复用 `Inject` 现有处理)。
