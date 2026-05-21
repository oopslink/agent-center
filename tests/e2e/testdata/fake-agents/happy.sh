#!/usr/bin/env bash
# happy.sh — fake claude-code-style agent that emits a short stream and exits 0.
# Args: ignored. We mimic the JSONL output format the claudecode adapter
# understands (see internal/agentadapter/claudecode/adapter.go).
set -u
printf '{"type":"thinking","text":"plan"}\n'
printf '{"type":"tool_use","name":"echo","input":{"x":1}}\n'
printf '{"type":"tool_result","content":"ok"}\n'
printf '{"type":"end_turn"}\n'
exit 0
