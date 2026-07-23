# Agent operational read models

Agents must use the official read-only MCP tools for operational inspection:

- `get_task_audit` for the paginated task lifecycle ledger.
- `list_task_executions` and `get_task_execution` for task-linked executor runs.
- `get_agent_runtime_effective_config` for desired versus observed runtime config.

These tools enforce the same project-membership boundary as `get_task`. Audit
notes are bounded and secret-shaped values are redacted. Runtime configuration
never returns environment values, tokens, credentials, or raw process
environment dumps.

Executor runs are reconstructed from the Center's persisted lifecycle events, so
completed runs remain visible after worker restart or local executor-directory
cleanup. If the worker has not reported an effective snapshot, the API returns
`effective.status=unknown`; it does not present desired configuration as applied.

Handlers consume repository and service interfaces. Callers and agents must not
query Center database tables directly.
