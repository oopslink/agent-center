# 非功能需求

| ID | 需求 |
|---|---|
| NF1 | 零 LLM SDK 依赖（不 import anthropic / openai 等）。理由见 [ADR-0002](../decisions/0002-no-llm-sdk-use-cli-agents.md) |
| NF2 | Web Console 仅 loopback 监听；用户通过 SSH 隧道访问；VPS 无需对外开 HTTP（v2: vendor 接入撤回 per ADR-0031；v3+ Bridge 重新设计）|
| NF3 | 持久化层 dialect-agnostic（SQLite / PG 可切） |
| NF4 | 单一二进制 `agent-center`，多模式（server / supervisor / worker / 各 CLI 子命令） |
| NF5 | 架构允许后续开放 admin RPC 远程访问（transport 抽象），v1 仅本机 unix socket |
| NF6 | 默认 worktree 隔离；并发上限按 `per_agent_type` 配置 |
| NF7 | 事件驱动 supervisor，对同一上下文的事件做合并窗口（debounce）以保证决策质量 |
| NF8 | 所有 agent 子进程必须以结构化（JSONL）模式运行；worker 必须解析并实时上报 trace |
| NF9 | 所有 domain event 进入 append-only 事件表，与状态表在同事务内双写；事件流是审计 / supervisor 输入 / 新增投影的源，**状态表是查询权威，事件流不承担状态重建**。理由见 [ADR-0014](../decisions/0014-event-sourcing-level.md) |
| NF10 | Supervisor 每次调用有不可篡改的审计记录（prompt / 输出 / 决策） |
| NF11 | Worker daemon 暴露本机 unix socket，作为 agent 调用 CLI 的中转入口 |
| NF12 | Agent JSONL 实时解析为 TaskExecution 投影摘要（current_activity / 滚动窗口 / 计数）；完整 JSONL 由 worker daemon 写本地文件、execution 结束打包归档至 BlobStore；**不进 events 表**。理由见 [ADR-0015](../decisions/0015-agent-trace-not-in-events-table.md) |
| NF13 | 不引入 MCP 协议；agent ↔ 工具通过 skill 文档 + CLI 子命令实现。理由见 [ADR-0001](../decisions/0001-no-mcp.md) |
| NF14 | 维护两份 skill markdown：`supervisor.md` 和 `worker-agent.md`，由 binary 携带分发 |
| NF15 | 所有大文件（任务日志归档、trace 归档等）通过 BlobStore 抽象访问；DB 只存相对路径，未来切对象存储无需改 DB。理由见 [ADR-0006](../decisions/0006-blob-store-for-large-content.md) |
| NF16 | v1 BlobStore 默认 LocalDirBlobStore（本地目录）；S3 兼容实现作为未来扩展点，DB schema 已就绪 |
