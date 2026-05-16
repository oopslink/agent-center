# 0002. 不引入 LLM SDK，走 CLI agent

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-16 |

## Context

agent-center 是 AI native 项目：center 的调度官（Supervisor）需要 LLM 能力来理解意图、规划任务、做决策。

可选路线：

1. 直接 import anthropic / openai SDK，自己写 agent loop
2. 复用现成的 CLI agent（claude code / codex / opencode）作为执行体，center 自己只做 spawn + 组装 prompt + 收 output

用户日常已经在用 claude code、codex 这些 CLI，工作流熟悉。

## Decision

**不引入任何 LLM SDK 依赖。** Supervisor 与 worker agent 都通过 spawn 现成 CLI agent（默认 claude code）实现。

- Supervisor 一次调用 = spawn 一个 `claude` 进程，带 supervisor.md skill + 注入相关 memory + 触发事件描述
- Worker 干活 = spawn `claude`（或 codex / opencode）进 worktree
- center 自己**完全不调** LLM API

## Consequences

正面：

- **零 LLM SDK 依赖**：不维护 anthropic SDK 升级、不管 API key 轮换（认证由 CLI 自己处理）
- **可换大脑**：将来想换 supervisor 引擎（claude → codex / opencode / gemini），换 adapter 即可
- **结构同源**：center 调度 agent，agent 自己也是 CLI agent —— 整个系统是 "agent + 不同工具集 = 不同角色" 的 dogfooding
- **登录问题外部化**：claude code 自己解决 OAuth；agent-center 假设 CLI 可用即可
- **走用户的 Claude 订阅**而非按 API 计费（可选）

负面 / 待跟进：

- 受 CLI 升级影响（claude code 改了 `--output-format` 参数我们要跟进）
- 每次 supervisor spawn 都有进程启动开销（毫秒级，可忽略）
- 各 CLI 对 headless 模式的支持不一致 —— adapter 层负责差异

## Alternatives Considered

### A. 直接 import anthropic SDK，自己写 agent loop

- Pro: 控制粒度细，无外部 CLI 依赖
- Con: 要维护完整 agent 框架（tool use loop、错误处理、token 计数）；锁定单一 provider 或自己抽象 multi-provider；不能用用户已经买的 Claude 订阅

### B. 跑一个常驻 LLM gateway（litellm / openrouter）

- Pro: provider 灵活
- Con: 多一个服务要维护；本质还是回到 "我们调 API" 路线

## 后续约束

为了让本决策能持续工作：

- 严禁在 `internal/` / `pkg/` 任何地方 `import` LLM 厂商的 SDK
- 与 CLI 的所有交互都封装在 `internal/agentadapter/` 一类的 adapter 包内
- Adapter 单测覆盖每个 CLI 的 spawn + output parse 逻辑
