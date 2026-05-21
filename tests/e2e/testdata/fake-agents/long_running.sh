#!/usr/bin/env bash
# long_running.sh — emits one line then sleeps until killed; used by kill /
# execution_timeout / shim_crashed tests.
#
# We trap SIGTERM and exit cleanly (143). Bash interleaves `wait` with
# trap delivery — using `wait` instead of `sleep` makes the trap fire
# immediately on SIGTERM.
set -u
printf '{"type":"thinking","text":"starting long work"}\n'
trap 'printf "{\"type\":\"thinking\",\"text\":\"sigterm received\"}\n"; exit 143' TERM
# Use a long sleep in background and `wait` so SIGTERM is delivered to
# bash promptly (rather than buffered behind `sleep 1` chains).
sleep 600 &
wait $!
exit 0
