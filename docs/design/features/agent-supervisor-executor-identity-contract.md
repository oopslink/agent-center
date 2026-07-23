# Agent = Supervisor + Executors identity contract

Date: 2026-07-23

## Contract

An Agent is the accountable runtime identity. In concurrent mode it has two internal roles:

- **Supervisor control plane**: the resident session that reads messages, monitors tasks, judges executor results, and performs final center writes such as `complete_task` / `block_task`.
- **Executor unit**: an isolated, per-task process/workspace forked by that same Agent to perform the task work.

Executors are not external agents, outside contractors, or a separate accountable party. The process, workspace, MCP, and center-credential isolation boundaries remain load-bearing permission boundaries, but they do not move delivery responsibility away from the Agent's Supervisor control plane.

## Runtime consumers

The prompt/description consumers that must carry this contract are:

- `internal/claudestream/agent_system_prompt.go`: concurrent Agent system prompt (`OrchestratorSystemPrompt`) used by `BuildStreamingArgv(..., concurrencyEnabled=true)`.
- `internal/agentruntime/orchestrator/runner.go`: executor system prompt used by Claude and prepended into Codex executor prompts.
- `internal/agentruntime/orchestrator/writeback.go`: `[executor finished]` judgment prompt injected into the resident Supervisor session.
- `internal/mcphost/server.go`: agent-facing MCP descriptions for profile, recovery, and task completion tools.
- `internal/mcphost/tools.go`: MCP schema descriptions, especially `dispatch_mode=supervisor_inline`.
- `web/src/i18n/locales/*/members.json`: Agent detail/config UI labels for concurrency slots and executor profiles.
- `README.md` / `README.zh-CN.md`: public terminology.

Regression locks:

- `internal/claudestream/argv_test.go` snapshots the concurrent Supervisor/Executor prompt contract.
- `internal/agentruntime/orchestrator/runner_test.go` verifies both Claude and Codex executor argv carry same-Agent executor framing while preserving no-MCP isolation.
- `internal/agentruntime/orchestrator/writeback_test.go` verifies judgment prompts prohibit "external executor" responsibility shifting.
