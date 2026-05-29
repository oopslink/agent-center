# 0051. ClaudeCode headless 集成契约（v2.7）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-29 |
| Delivered | v2.7 design phase；详 [v2.7-domain-refactor-plan § 2.6 / § 5 D](../../plans/v2.7-domain-refactor-plan.md) |
| Supersedes | amends [ADR-0002 不用 LLM SDK，走 CLI agent](0002-no-llm-sdk-use-cli-agents.md) / [ADR-0018 Detached agent via per-execution shim](0018-detached-agent-via-per-execution-shim.md)（v2.7 改长驻 headless 会话，取消每次交互拉新进程默认） |
| Related | [ADR-0050 Environment BC](0050-environment-bc.md)（AgentController/MCPHost） / [ADR-0049 Agent BC](0049-agent-bc-no-agentrun.md) / [ADR-0047 Conversation context_refs](0047-conversation-owner-ref-and-context-refs.md) |

## Context

[ADR-0018](0018-detached-agent-via-per-execution-shim.md) 采用"每次执行拉新进程"的 shim 模型。v2.7 Agent 逻辑长驻（[ADR-0049](0049-agent-bc-no-agentrun.md)），且 ClaudeCode headless 契约已由 @oopslink 手工验证，应直接面向**长驻 headless ClaudeCode 会话**集成。需固化通道职责，并加契约测试防 CLI 漂移。

## Decision

### 1. 两通道，各管一件事

**通道① 本地控制通道 = stream-json over stdin/stdout（驱动对话）**

- AgentController 以 `--input-format stream-json --output-format stream-json` 长驻拉起 `claude`，不在回合间退出。
- stdin（进）：任务简报、人/他 Agent 的回复作为 user message 注入；**对话回合走 stdin 注入，不靠 MCP 拉**。
- stdout（出）：assistant 文本、tool-use、结果逐行 JSON 出；AgentController 解析为 **AgentActivityEvent**。
- **stdout 不自动进 Conversation**，模型啰嗦中间输出只沉 AgentActivityEvent。

**通道② 业务能力通道 = MCP AppService 工具（让模型反向操作 AgentCenter）**

- Worker MCPHost 经 `--mcp-config` 暴露 plan § 10 OQ4 工具集；调用打到中心 AppService（不碰 DB），受域隔离约束。

### 2. 人可见通信 = 显式 only

stdout 不给人看，所以一切需人看到的内容由显式消息携带：

- `post_task_message` 是 Agent 文本进 Task Conversation 的**唯一**路径。
- `request_input(question, ...)`：question 进 Conversation 并置 WorkItem `waiting_input`。
- `block_task(reason, ...)`：reason 进 Conversation（§2.2 要求 blocked 必带消息）。
- `complete_task(summary?, ...)`：可带交付小结进 Conversation。

净效果：Task Conversation 只含 Agent 主动说的话 + 状态变更系统消息。

### 3. 等待→唤醒闭环（plan § 10 OQ5）

- 模型缺信息 → 调 `request_input` → 回合结束、进程 idle。
- 系统**不判语义**：WorkItem 处 `waiting_input` 时，进来的消息直接作为回应注入同会话 stdin，唤醒（`waiting_input → active`，新 AgentInteraction）；够不够由 Agent 自决，不够再 `request_input`。
- 输入来源 = 除等待中的该 Agent 自身外任何参与者（人 + 他 Agent）；忙时缓存到下次 waiting_input；同时到的合并成一轮。
- 唤醒经 outbox（plan § 10 OQ1）。

### 4. 生命周期与会话

- AgentController 拥有 start/stop/restart/reset；进程长驻，**每次交互拉新进程不是默认**。
- 一个 WorkItem 可多 AgentInteraction = 同一长驻会话内多回合。
- AgentInteraction 是逻辑回合。Environment 内部可记 claude session_id/进程（AgentRuntimeSession），但 Agent BC 不暴露 ClaudeCodeSession 为业务 AR。
- runtime：cwd = `{home}/workspace`；memory 在 `{home}/memory/`；env vars 来自 Agent profile。

### 5. 契约测试（防 CLI 漂移）

覆盖：start → 注入任务 → 收到输出 → `request_input` → 调 MCP 工具 → stop/restart。归 D 阶段。

## Consequences

### 正面

- 长驻会话保上下文连续、多轮高效；两通道职责清晰（驱动 vs 反向操作）。
- 人机界面清爽：显式消息可见、观测细节入活动流。

### 负面 / 待跟进

- 强依赖 ClaudeCode headless CLI 的 stream-json/`--mcp-config` 行为；契约测试是必需防线。
- 长驻进程的资源占用、崩溃恢复、会话超时需工程策略。
- 其它 CLI（Codex 等）适配需各自契约（沿用 AgentAdapter 思路 [ADR-0030](0030-agentadapter-matrix-expansion.md)）。

## Alternatives Considered

### A. 保留每次交互拉新进程（[ADR-0018](0018-detached-agent-via-per-execution-shim.md)）

- ❌ 丢上下文、启动开销大、与长驻 Agent 模型冲突。否决。

### B. stdout 自动进 Conversation

- ❌ 模型中间输出刷屏、不可控。否决，改显式 post_task_message + stdout→ActivityEvent（@oopslink 2026-05-29 选 B）。

### C. 任务/回复经 MCP 拉取而非 stdin 注入

- ❌ 对话回合天然是 user 侧输入，stdin 注入更自然；MCP 拉取留作按需重取详情。否决主路径。

## References

- [v2.7-domain-refactor-plan § 2.6](../../plans/v2.7-domain-refactor-plan.md) / [§ 4.3-4.4](../../plans/v2.7-domain-refactor-plan.md) / [§ 5 D](../../plans/v2.7-domain-refactor-plan.md) / [§ 10 OQ4/OQ5](../../plans/v2.7-domain-refactor-plan.md)
- [ADR-0050](0050-environment-bc.md) / [ADR-0049](0049-agent-bc-no-agentrun.md) / [ADR-0047](0047-conversation-owner-ref-and-context-refs.md)
- 来源：2026-05-29 DM 设计讨论（@oopslink ↔ @AgentCenterPD）；headless 契约由 @oopslink 手工验证
