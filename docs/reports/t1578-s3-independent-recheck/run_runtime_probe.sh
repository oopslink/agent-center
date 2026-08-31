#!/usr/bin/env bash
set -u

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
CANDIDATE="${ROOT}/../insight-candidate-16dc5815"
EVIDENCE="${ROOT}/docs/reports/t1578-s3-independent-recheck/evidence"
mkdir -p "$EVIDENCE"

LOG="$EVIDENCE/runtime-probe.log"
SUMMARY="$EVIDENCE/runtime-probe.jsonl"
: > "$LOG"
: > "$SUMMARY"

run() {
  local id="$1"
  shift
  local start end rc
  start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  {
    printf '\n===== COMMAND %s START %s =====\n' "$id" "$start"
    printf 'cwd=%s\n' "$(pwd)"
    printf 'argv='
    printf '%q ' "$@"
    printf '\n'
  } | tee -a "$LOG"
  "$@" 2>&1 | tee -a "$LOG"
  rc=${PIPESTATUS[0]}
  end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf '===== COMMAND %s END %s rc=%s =====\n' "$id" "$end" "$rc" | tee -a "$LOG"
  printf '{"id":"%s","start":"%s","end":"%s","rc":%s}\n' "$id" "$start" "$end" "$rc" >> "$SUMMARY"
  return "$rc"
}

cd "$CANDIDATE" || exit 2
TMPDIR="$(mktemp -d /tmp/insight-runtime-probe.XXXXXX)"
PORT_FILE="$EVIDENCE/runtime-port.txt"
SERVER_LOG="$EVIDENCE/runtime-server.log"
CONFIG="$TMPDIR/config.yaml"
DB="$TMPDIR/center.db"
MASTER="$TMPDIR/master.key"
SOCK="$TMPDIR/admin.sock"

PORT="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
printf '%s\n' "$PORT" > "$PORT_FILE"

python3 - "$CONFIG" "$DB" "$MASTER" "$SOCK" "$PORT" <<'PY'
import base64, os, sys
config, db, master, sock, port = sys.argv[1:6]
with open(master, "wb") as f:
    f.write(base64.b64encode(os.urandom(32)))
os.chmod(master, 0o600)
with open(config, "w", encoding="utf-8") as f:
    f.write(f"""server:
  listen_addr: "127.0.0.1:0"
  sqlite_path: "{db}"
  admin_socket_path: "{sock}"
  admin_tcp_listen: ""
web_console:
  listen_addr: "127.0.0.1:{port}"
secret_management:
  master_key_file: "{master}"
  skip_perms_check: true
""")
PY

run runtime_config_redacted sed -E 's#(sqlite_path|admin_socket_path|master_key_file): ".*"#\1: "<temp-redacted>"#' "$CONFIG"
run runtime_migrate ./bin/agent-center server -config "$CONFIG" -migrate-only

./bin/agent-center server -config "$CONFIG" > "$SERVER_LOG" 2>&1 &
PID=$!
printf '%s\n' "$PID" > "$EVIDENCE/runtime-pid.txt"
printf 'started pid=%s at=%s\n' "$PID" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" | tee -a "$LOG"

cleanup() {
  if kill -0 "$PID" 2>/dev/null; then
    kill "$PID" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

for _ in $(seq 1 100); do
  if curl -fsS "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done
run runtime_server_log cat "$SERVER_LOG"
if ! curl -fsS "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
  printf '{"id":"runtime_probe","verdict":"REJECT","reason":"health endpoint not reachable","port":"%s"}\n' "$PORT" >> "$SUMMARY"
  exit 1
fi

run runtime_health curl -sS -i "http://127.0.0.1:${PORT}/api/health"
run runtime_system_version curl -sS -i "http://127.0.0.1:${PORT}/api/system/version"
run runtime_process ps -p "$PID" -o pid=,ppid=,stat=,command=
cleanup
trap - EXIT
printf 'stopped pid=%s at=%s\n' "$PID" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" | tee -a "$LOG"
