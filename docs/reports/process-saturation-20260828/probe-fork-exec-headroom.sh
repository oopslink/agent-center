#!/usr/bin/env bash
set -u

out_dir="${1:-docs/reports/process-saturation-20260828/raw}"
n="${2:-250}"
mkdir -p "$out_dir"
out="$out_dir/20-fork-exec-headroom.txt"

{
  printf 'timestamp_start=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'command=bash %s %s %s\n' "$0" "$out_dir" "$n"
  printf 'probe_process=/bin/sleep 5\n'
  printf 'requested_spawns=%s\n' "$n"
  printf 'pre_total_processes='
  ps -axo pid= | wc -l | tr -d ' '
  printf '\npre_users='
  ps -axo user= | awk 'NF{u[$1]++} END{print length(u)}'
  printf '\n'
} > "$out"

pids=()
spawn_success=0
spawn_failure=0

for i in $(seq 1 "$n"); do
  /bin/sleep 5 &
  pid=$?
  child=$!
  if [ "$pid" -eq 0 ] && [ -n "${child:-}" ]; then
    pids+=("$child")
    spawn_success=$((spawn_success + 1))
    printf 'spawn index=%s pid=%s status=started\n' "$i" "$child" >> "$out"
  else
    spawn_failure=$((spawn_failure + 1))
    printf 'spawn index=%s exit=%s status=failed\n' "$i" "$pid" >> "$out"
  fi
done

{
  printf 'during_total_processes='
  ps -axo pid= | wc -l | tr -d ' '
  printf '\nduring_users='
  ps -axo user= | awk 'NF{u[$1]++} END{print length(u)}'
  printf '\nspawn_success=%s\n' "$spawn_success"
  printf 'spawn_failure=%s\n' "$spawn_failure"
} >> "$out"

wait_success=0
wait_failure=0
for child in "${pids[@]}"; do
  wait "$child"
  status=$?
  printf 'wait pid=%s exit=%s\n' "$child" "$status" >> "$out"
  if [ "$status" -eq 0 ]; then
    wait_success=$((wait_success + 1))
  else
    wait_failure=$((wait_failure + 1))
  fi
done

{
  printf 'wait_success=%s\n' "$wait_success"
  printf 'wait_failure=%s\n' "$wait_failure"
  printf 'post_total_processes='
  ps -axo pid= | wc -l | tr -d ' '
  printf '\npost_users='
  ps -axo user= | awk 'NF{u[$1]++} END{print length(u)}'
  printf '\ntimestamp_end=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
} >> "$out"

if [ "$spawn_failure" -eq 0 ] && [ "$wait_failure" -eq 0 ]; then
  printf 'probe_exit=0\n' >> "$out"
  exit 0
fi

printf 'probe_exit=1\n' >> "$out"
exit 1
