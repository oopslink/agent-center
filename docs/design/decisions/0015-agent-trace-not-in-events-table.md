# 0015. agent_trace 不进 events 表：归 BlobStore + TaskExecution 投影摘要

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-17 |

## Context

[05-observability.md § 事件总览](../architecture/tactical/observability/01-observability.md) 把 `agent_trace.event`（每条 tool call / thinking / tool response）跟 domain event 混存在同一张 `events` 表。但 agent_trace 与 domain event 在多个维度都失衡：

| 维度 | Domain event | `agent_trace.event` |
|---|---|---|
| 单 execution 量级 | 10-50 行 | 100-1000 行（JSONL 每行一条） |
| 用途 | 审计 / supervisor 输入 / CLI timeline | LLM 行为复盘 |
| 字段形状 | refs + payload 按 domain 结构化 | tool / args / response 文本 |
| 保留期 | 长（审计可能要半年） | 短即可（execution 结束后价值递减） |
| 查询入口 | `inspect task` / `query events` | "看 trace" / Web Console trace 视图 |

后果：

- events 表 ~20× 来自 trace，`query events --task-id=T-42` 要穿过 1000 行 trace 才能找到 50 行 domain event
- 同一份数据已经有三份拷贝：events 表实时流 + worker daemon 本地 `trace.jsonl` + BlobStore 归档 `tasks/<id>/trace.jsonl.gz`
- [ADR-0014](0014-event-sourcing-level.md) 已确定状态表权威，事件流不承担状态重建 → agent_trace 在 events 表的存在不再被任何"重放推导状态"用例需要

实时观测需求其实由 [05 Fleet View § 数据基础](../architecture/tactical/observability/01-observability.md) 的 `TaskExecution` 投影承载（`current_activity` 等字段由 worker daemon 边解析 JSONL 边更新），不依赖"agent_trace 在 events 表"。

## Decision

### 1. agent_trace 不再作为事件流入 events 表

删除 `agent_trace.event` event_type。events 表只承载 domain event（状态变化、操作记录）。

### 2. 实时维度：TaskExecution 投影 + `recent_activities` 滚动窗口（γ）

Worker daemon 边解析 JSONL 边更新 TaskExecution 投影：

- 既有字段：`current_activity` / `current_activity_at` / `total_tool_calls` / `total_tokens`（[05 Fleet View](../architecture/tactical/observability/01-observability.md) 已列）
- **新增字段** `recent_activities` (TEXT JSON)：保留最近 N 条 `{at, activity_summary}`，N 默认 10（可配）
- 可选 `tool_call_counts` (TEXT JSON)：如 `{"Edit": 5, "Bash": 2}`，给"用了哪些工具"低成本分析做底

判断"任务卡住"等 90% 实时观测场景由该投影满足。

### 3. 持久化维度：本地 jsonl + BlobStore 归档

- Worker daemon 边跑边追加本地 `trace.jsonl`（崩溃恢复源）
- Execution 结束打包上传 `tasks/<task_id>/trace.jsonl.gz` 到 BlobStore（[ADR-0006](0006-blob-store-for-large-content.md)）
- 回填 `task.trace_blob_path`
- 历史 trace 深挖通过 `agent-center logs <execution>` 走 BlobStore.Get

### 4. Live trace 深挖：worker daemon peek-trace RPC（α）

Execution still running 时若需要看 tool 参数 / thinking 文本，走 worker daemon RPC：

- 新 CLI：`agent-center peek-trace <execution> [--last=N] [--kind=tool_call|thinking|tool_result|all]`
- 实现：center → worker gRPC（复用已有 enroll 长连接）→ worker daemon 读本地 `trace.jsonl` 尾部 N 行返回
- 用途：supervisor 排查 / 用户点飞书卡片"查看详情"等按需场景

不依赖 worker 活着的兜底：worker 死了拿不到，但那种情况下用户的优先级是"kill 重派 + 告警"，不是"看 trace"。

### 5. 显式驳回"粗粒度 tool_call_completed event"（δ）

不引入新的 `task_execution.tool_call_completed` 类粗粒度 event。一旦开口，会演化成"thinking_recorded" / "tool_call_started" / ... ，最后回到混表原状。supervisor 想看工具序列走 peek-trace。

## Consequences

正面：

- **events 表轻量**：去掉 20× 的 trace 噪声，索引 / 查询 / 备份 / 审计扫描都受益
- **跟 BlobStore 哲学贯通**：trace 本质是大文件，跟日志归档一个范式（[ADR-0006](0006-blob-store-for-large-content.md)）
- **Live observability 不缺**：γ 投影满足实时摘要，α RPC 满足按需深挖
- **演化简单**：未来要看 token 趋势 / 工具调用分析，扫 BlobStore 归档 + jq / grep 即可

负面 / 待跟进：

- **跨 execution SQL 分析能力下降**："今天用了几次 WebFetch" 这种查询从 SQL 变成 file scan；v1 单用户低频可接受
- **Live trace 依赖 worker 在线**：worker 死 + execution 未归档 → 这段 trace 不可达；接受（worker 死本身就是更严重的问题）
- **Memory 审计渠道引用调整**：[ADR-0012 § 8](0012-memory-file-based.md) 说 "memory 变更由 `agent_trace.event` + git log 双渠道审计"。本 ADR 后 `agent_trace.event` 不在 events 表里，但 audit 路径仍成立（claude `Edit`/`Write` 工具调用仍记录在 `trace.jsonl.gz` 中），只是 filter 从 SQL 变成 grep / jq

## Alternatives Considered

### A. 同表混存（现状）

- Pro: 单表统一
- Con: 量级 / 用途 / retention / 字段形状全维度失衡；events 表被 trace 淹没
- 不选

### B. 独立 `agent_trace` 表（同 DB）

- Pro: 仍可 SQL 查
- Con: DB 体积按 trace 量级增长（百万行级），索引 / 备份成本与混表差别不大
- 不选

### C. 当前决定：仅 BlobStore 归档 + TaskExecution 投影摘要 + peek-trace

- Pro: DB 轻；live + post-hoc 用例都有覆盖；跟 BlobStore 范式统一
- Con: 跨 execution SQL 分析弱；live trace 依赖 worker
- 选

### D. 粗粒度 `task_execution.tool_call_completed` event 进 events 表

- Pro: live + SQL 查 + 入 supervisor `trigger_event_ids`
- Con: events 表仍 ~10× 膨胀；开口后控不住语义扩张（会演化加 thinking event 等）；C 决定的清洁感破坏
- 不选

## 影响范围

- 改写 [NF12](../requirements/02-non-functional.md)："agent JSONL 解析后实时更新 TaskExecution 投影；完整 JSONL 任务结束归档至 BlobStore；不进 events 表"
- 改写 [05-observability.md](../architecture/tactical/observability/01-observability.md)：
  - O1 例子里删 `agent_trace` 引用
  - O2 改写流程描述：worker daemon 解析 → 更新 TaskExecution 投影 + 写本地 jsonl，**不**推 center events
  - § 事件总览表 Execution 行去掉 `agent_trace.event`
  - § Cognition 行引用 `agent_trace.event` 作为 memory 审计渠道 → 改为 `trace.jsonl.gz`（BlobStore filter）
  - § Aggregate 摘要 `AgentTraceEvent` 描述 "落 events 表" → "实时投影到 TaskExecution + 归档至 BlobStore"
- 改写 [06-supervisor-model.md](../architecture/tactical/cognition/01-supervisor-model.md) § 4.11 / § 5.1 / § 5.2：memory 审计 / DecisionRecord 跟 trace 的关系，引用从 events 表的 `agent_trace.event` 改为 trace.jsonl 文件
- 改写 [03-bounded-contexts.md](../architecture/strategic/03-bounded-contexts.md)：
  - L36 `AgentTraceEvent` 定义加注"不入 events 表"
  - BC1 TaskRuntime 核心事件去掉 `agent_trace.event`（注：[ADR-0019](0019-bc-scheduling-execution-merged-to-task-runtime.md) 后 BC1 = TaskRuntime 合并自原 BC4 Execution）
  - BC4 Cognition 段落里"agent_trace.event 审计渠道"措辞调整（注：[ADR-0019](0019-bc-scheduling-execution-merged-to-task-runtime.md) 后 Cognition 编号 BC5 → BC4）
  - 包前缀 / 表映射表去掉 `agent_trace.*` event_type prefix
- 实现层 02-persistence-schema (TBD)：TaskExecution 表加 `recent_activities` TEXT JSON / `tool_call_counts` TEXT JSON 字段
- 新 CLI 设计：`agent-center peek-trace <execution>`（worker daemon 侧 RPC + center 转发）→ 归 [implementation/03-cli-subcommands](../implementation/) (TBD)
