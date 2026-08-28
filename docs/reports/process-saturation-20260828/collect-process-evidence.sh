#!/usr/bin/env bash
set -u

out_dir="${1:-docs/reports/process-saturation-20260828/raw}"
mkdir -p "$out_dir"

count_snapshot() {
  local label="$1"
  {
    printf 'label=%s\n' "$label"
    printf 'timestamp=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf 'total_processes='
    ps -axo pid= | wc -l | tr -d ' '
    printf '\nusers='
    ps -axo user= | awk 'NF{u[$1]++} END{print length(u)}'
    printf '\nprocesses_by_user\n'
    ps -axo user= | awk 'NF{u[$1]++} END{for (k in u) print u[k], k}' | sort -nr
    printf '\nprocesses_by_command_top80\n'
    ps -axo comm= | awk 'NF{c[$1]++} END{for (k in c) print c[k], k}' | sort -nr | sed -n '1,80p'
  } > "$out_dir/${label}-counts.txt"
}

ps_full() {
  local label="$1"
  ps -axo pid,ppid,pgid,uid,user,stat,etime,command -ww > "$out_dir/${label}-ps-full.txt"
}

children_by_parent_command() {
  local label="$1"
  {
    printf 'timestamp=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    ps -axo pid=,ppid=,user=,stat=,etime=,command= -ww |
      awk '{
        cmd=$0
        pid=$1
        ppid=$2
        user=$3
        sub(/^[[:space:]]*[0-9]+[[:space:]]+[0-9]+[[:space:]]+[^[:space:]]+[[:space:]]+[^[:space:]]+[[:space:]]+[^[:space:]]+[[:space:]]*/, "", cmd)
        key=ppid "\t" user "\t" cmd
        counts[key]++
        pids[key]=pids[key] " " pid
      }
      END {
        for (k in counts) print counts[k] "\t" k "\tPIDS:" pids[k]
      }' |
      sort -nr |
      sed -n '1,200p'
  } > "$out_dir/${label}-children-by-ppid-command.txt"
}

{
  printf 'timestamp=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'host=%s\n' "$(hostname)"
  printf 'cwd=%s\n' "$PWD"
  printf 'branch=%s\n' "$(git branch --show-current)"
  printf 'head=%s\n' "$(git rev-parse HEAD)"
  printf 'task_input_readme_present='
  test -f task-input/v1/README.md && echo yes || echo no
  printf 'task_input_manifest_present='
  test -f task-input/v1/manifest.json && echo yes || echo no
} > "$out_dir/00-environment.txt"

ps_full before
count_snapshot before
children_by_parent_command before

{
  printf 'timestamp=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  if command -v pstree >/dev/null 2>&1; then
    pstree -w
  else
    echo 'pstree unavailable'
  fi
} > "$out_dir/before-pstree.txt"

for i in 1 2 3 4 5; do
  ps_full "observation-${i}"
  count_snapshot "observation-${i}"
  if [ "$i" != 5 ]; then
    sleep 15
  fi
done
