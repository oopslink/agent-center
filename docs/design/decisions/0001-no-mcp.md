# 0001. 不引入 MCP

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-16 |

## Context

agent-center 需要给两类 agent（supervisor 与 worker-agent）暴露工具集：

- Supervisor 需要 dispatch / query / issue comment / conversation add-message / record-decision 等工具
- Worker 内 agent 需要 request-input / report-progress / open-issue / read-task-context 等工具

最初考虑用 **MCP（Model Context Protocol）** 暴露这些工具 —— 因为 claude code 对 MCP 支持原生。

## Decision

**不引入 MCP 协议。** Agent ↔ 工具通过两层实现：

1. **Skill markdown 文档**（`supervisor.md`、`worker-agent.md`）教 agent 怎么用工具
2. **CLI 子命令**（`agent-center <op>`）作为实际的工具执行入口

agent 用其内建 Bash 工具调用 CLI，CLI 自动根据 env 检测运行上下文（server 同机 / worker 内 / 远程）选择 transport。

## Consequences

正面：

- **不绑死特定 agent CLI**：claude code、codex、opencode 都有 Bash 工具，工具暴露不依赖某一家的 MCP 支持
- **同一工具同时供人与 agent 用**：调试时人在 shell 里直接跑 `agent-center dispatch ...`，agent 通过 Bash 跑同样的命令
- **少一层协议**：无需实现 MCP server、不要双轨工具定义
- **可观测**：Bash 工具调用直接出现在 agent 输出流里，trace 自然

负面 / 待跟进：

- 需要维护两份 skill markdown 文档；agent CLI 升级后 skill 要同步
- CLI 没有 schema 校验那么强；得自己保证参数格式
- 不能利用 MCP 的流式 / 通知能力（目前用例不需要）

## Alternatives Considered

### A. 完整 MCP server

- Pro: claude code 原生支持，schema 校验强
- Con: 绑死 claude code（codex / opencode MCP 支持不如 claude 成熟），多一层进程模型 / 协议层，工具定义双轨

### B. 直接库调用（worker 内 agent 通过某种 RPC 直接调 worker daemon Go 函数）

- Pro: 无 fork 开销
- Con: 等同于不通过 CLI 这层 —— 但 agent 跑的是子进程，跨进程必然要 IPC，CLI 已经是足够轻的 IPC 表层

## 何时重新评估

如果出现以下场景之一：

- 需要把 agent-center 的工具暴露给**外部生态**（别人家的 agent）—— A2A + MCP 是标准
- 工具需要**流式订阅 / 主动推送**（CLI 阻塞模型不够）
- 跟某个不支持 Bash 但支持 MCP 的 agent CLI 集成
