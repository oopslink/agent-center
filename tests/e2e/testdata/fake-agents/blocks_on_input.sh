#!/usr/bin/env bash
# blocks_on_input.sh — agent that asks for input via tool_use=request_input
# and then blocks (mimicking real agent waiting for user response).
# The harness will simulate the response by signaling the agent to continue.
set -u
printf '{"type":"thinking","text":"need user input"}\n'
printf '{"type":"tool_use","name":"request_input","input":{"question":"approve?"}}\n'
# Wait for a SIGUSR1 (harness "response received") then emit completion.
trap 'printf "{\"type\":\"thinking\",\"text\":\"got response\"}\n"; printf "{\"type\":\"end_turn\"}\n"; exit 0' USR1
while true; do
  sleep 1
done
