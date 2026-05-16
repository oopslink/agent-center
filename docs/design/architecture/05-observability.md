# 可观测性 & Fleet View

可观测性是 agent-center 的一级关注点。

## 为什么 AI agent 场景的观测特别关键

对人类驱动的脚本，"日志看 stdout" 够用。对 LLM agent，观测对象多一个维度：**它思考过什么、调用了哪些工具、为什么做这个决定**。没有这层观测：

- Agent 跑飞，看不出原因
- Supervisor 决策不合理，不知道它"想了啥"
- 出 bug 没法复现（同样 prompt 跑两次结果不同）
- 成本 / token 用量是黑盒
- 跨任务模式分析做不了

所以观测不是"日志收集"，是 **结构化、可追溯、可重放、可聚合的 agent 行为档案**。

## 可观测对象分层

| 层 | 观测内容 |
|---|---|
| Task | 状态机历史、累计时长、所属 worker / agent、worktree 路径、产物（diff、分支、生成文件清单）、成败原因 |
| AgentExecution | Agent 类型 + 版本、本次会话的 prompt（含注入的 memory / 约束）、**逐条 tool call** 记录（工具名、参数、返回、耗时）、token 用量、子进程退出码 |
| Supervisor | 每次调用：触发事件、注入的 memory / 上下文、prompt 全文、LLM 输出、tool call 序列、最终决策、耗时 |
| Worker | 在线 / 离线、心跳、并发任务数、累计任务统计、连接断开次数、host 资源（可选） |
| Issue | 开启 / 讨论 / 收敛全过程、参与方、产生的 Task |
| System | 任务吞吐率、成败率、Issue → Task 转化率、Supervisor 唤醒频率、DB 大小、归档大小 |

## 设计承诺

### O1. 一切状态变化进 `events` 表，不丢

- 单一 append-only 事件流，所有上下文的 domain event 都落到这一张表
- 字段（概念）：`id, occurred_at, context, aggregate_type, aggregate_id, event_type, payload (JSON), correlation_id`
- 是 source of truth for 重放、审计、跨上下文 join
- 结构跟事件流系统兼容（未来切 Kafka / NATS 无痛）

### O2. Agent 子进程必须开启结构化输出

- Claude code：`--output-format stream-json`，每行 JSONL（tool call / response / thinking 片段）
- Codex / OpenCode：各自的等价模式（adapter 层负责）
- Worker daemon 边收边解析 → 转 `AgentTraceEvent` → 推到 center 事件通道 + 写 task 本地 trace 文件
- 任务结束时，trace 文件作为产物之一打包上传

### O3. Supervisor 调用全留档

- 每次 supervisor 启动：先在 DB 写 `supervisor_invocations(id, trigger_event_id, started_at)` 占位
- 调 claude code 时把 `--session-id <invocation-id>` 传进去，让 claude code 自己产的 JSONL trace 跟 invocation 关联
- Supervisor 子进程结束：回填 `ended_at, status, decisions_made, token_usage`
- Decisions 是 supervisor 通过 CLI 命令显式 emit 的（每次调 `dispatch` / `promote-suggestion` 等，CLI 内部记录 "decision" 关联到 invocation_id）

### O4. 日志的双写

- 实时通道走结构化事件 + agent JSONL trace（量小、价值高）
- 原始 stdout / stderr 任务结束打包一次性上传到 **BlobStore**（见 [implementation/01-blob-store.md](../implementation/01-blob-store.md)），DB 只存相对路径（`task.log_blob_path`）

### O5. 查询接口

CLI:

- `agent-center inspect task <id>` — 时间线（事件 + tool call + 状态变化 + 产物）
- `agent-center inspect supervisor <invocation-id>` — supervisor 一次决策的全过程
- `agent-center inspect session <agent-session-id>` — 单次 agent 执行的全 trace
- `agent-center logs task <id> --tail / --follow` — 实时尾日志
- `agent-center stats --since=7d` — 聚合指标

Supervisor 工具：`query_*` 系列让它能回答用户"task 42 跑得怎么样"等问题。

飞书：任务完成卡片附 [查看 trace] [查看 supervisor 思考] 按钮，点击拉详细页（v1 回 Markdown 摘要文本）。

## Fleet View — 跨任务实时全景

**现在有哪些 agent 活着、各自在干嘛**。这是跟单任务下钻并列的关键视角。

### 数据基础

每个 worker daemon 边解析 agent 子进程 JSONL，边维护 `AgentSession` 投影（schema 见 [implementation/02-persistence-schema.md](../implementation/02-persistence-schema.md) TBD）。投影字段含义：

- `current_activity` 自由文本: `"edit src/x.go"` / `"bash: go test"` / `"thinking"`
- `current_activity_at` 当前活动开始时间
- `status` running / waiting_input / completed / failed
- `total_tool_calls` / `total_tokens` 累计

这张表是**实时投影**，可从 events 表全量重算（崩了能恢复）。

### CLI: `agent-center ps`（仿 `docker ps` / `kubectl get pods`）

```
$ agent-center ps
SESSION   WORKER       AGENT         TASK    PROJECT          STATUS          ACTIVITY                       DURATION
a3f2-1d   mac-mini-1   claude-code   #42     agent-center     running         edit src/worker/main.go        0:03:21
a3f2-1e   mac-mini-1   codex         #43     agent-center     running         bash: go test ./...            0:00:08
b1c2-2a   home-server  claude-code   #44     personal-site    waiting_input   request_human_input            0:00:45 ⏸
```

变体：`ps --watch`、`ps --by-worker | --by-project | --by-agent-type`。

### 飞书侧

用户问"现在在跑啥" → supervisor 调 `query_agent_sessions` → 组装卡片回复。周期 review 卡片附此刻活跃 agent 小卡。

## 关键 Aggregate / Event 摘要

- `Event` —— 跨上下文唯一事件流
- `SupervisorInvocation` —— 一次 supervisor 调用的审计单元
- `AgentSession` —— 一次 agent 子进程执行的实时投影
- `AgentTraceEvent` —— agent JSONL 单行事件（落 events 表）
