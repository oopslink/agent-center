#!/usr/bin/env bash
set -uo pipefail

if [ "$#" -lt 2 ]; then
  echo "usage: $0 <gate-name> <command> [args...]" >&2
  exit 64
fi

gate="$1"
shift
root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
log="$root/logs/${gate}.log"
meta="$root/meta/${gate}.json"

start_iso="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
start_epoch="$(date -u +%s)"

{
  printf 'gate=%s\n' "$gate"
  printf 'start_utc=%s\n' "$start_iso"
  printf 'cwd=%s\n' "$(pwd)"
  printf 'command='
  printf '%q ' "$@"
  printf '\n\n'
  "$@"
} >"$log" 2>&1
rc=$?

end_iso="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
end_epoch="$(date -u +%s)"
duration=$((end_epoch - start_epoch))
status="PASS"
if [ "$rc" -ne 0 ]; then
  status="FAIL"
fi

cat >"$meta" <<EOF
{
  "gate": "$gate",
  "status": "$status",
  "exit_code": $rc,
  "start_utc": "$start_iso",
  "end_utc": "$end_iso",
  "duration_seconds": $duration,
  "log": "logs/${gate}.log"
}
EOF

exit "$rc"
