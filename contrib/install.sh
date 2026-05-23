#!/usr/bin/env bash
# install.sh — bootstrap agent-center on a fresh VPS.
#
# Per implementation/06-deployment § 10.1 + plan-7 § 3.6.
#
# Usage:
#   sudo bash install.sh [--binary=<path>] [--dry-run]
#
# Steps:
#   1. create system user `agent-center`
#   2. create /var/lib/agent-center/{blobs,memory}, /etc/agent-center,
#      /var/log/agent-center, /var/backups/agent-center
#   3. install binary to /usr/local/bin/agent-center
#   4. install systemd unit files (agent-center.service +
#      agent-center-backup.{service,timer})
#   5. systemctl daemon-reload + enable agent-center.service +
#      agent-center-backup.timer
#
# Strict validation:
#   - --binary must exist + be executable
#   - all writes are idempotent; re-runs preserve existing config
#   - --dry-run prints planned actions without mutating state.

set -euo pipefail

BINARY=""
DRY_RUN=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary=*)
      BINARY="${1#--binary=}"; shift ;;
    --binary)
      shift; BINARY="$1"; shift ;;
    --dry-run)
      DRY_RUN=true; shift ;;
    -h|--help)
      sed -n '2,20p' "$0"; exit 0 ;;
    *)
      echo "unknown arg: $1" >&2; exit 64 ;;
  esac
done

step() { printf '[install] %s\n' "$*"; }
run() {
  if "$DRY_RUN"; then
    printf '[dry-run] %s\n' "$*"
  else
    eval "$@"
  fi
}

if [[ -z "$BINARY" ]] && ! "$DRY_RUN"; then
  # Look in script dir for a same-name binary; otherwise insist.
  candidate="$(dirname -- "$(readlink -f "$0")")/agent-center"
  if [[ -x "$candidate" ]]; then
    BINARY="$candidate"
  else
    echo "install.sh: --binary=<path> required (or place a binary alongside this script)" >&2
    exit 64
  fi
fi

if [[ -n "$BINARY" ]] && ! "$DRY_RUN" && [[ ! -x "$BINARY" ]]; then
  echo "install.sh: binary $BINARY is not executable" >&2
  exit 64
fi

step "Create system user agent-center (if missing)"
if ! id -u agent-center >/dev/null 2>&1; then
  run useradd --system --shell /usr/sbin/nologin --home-dir /var/lib/agent-center agent-center
else
  step "  user already exists; skip"
fi

step "Create directories"
for d in /var/lib/agent-center /var/lib/agent-center/blobs /var/lib/agent-center/memory \
         /etc/agent-center /var/log/agent-center /var/backups/agent-center; do
  run install -d -m 0750 -o agent-center -g agent-center "$d"
done

if [[ -n "$BINARY" ]]; then
  step "Install binary to /usr/local/bin/agent-center"
  run install -m 0755 "$BINARY" /usr/local/bin/agent-center
else
  step "  --dry-run; skipping binary install"
fi

CONTRIB_DIR="$(dirname -- "$(readlink -f "$0")")"

step "Install systemd unit files"
for unit in agent-center.service agent-center-backup.service agent-center-backup.timer; do
  src="$CONTRIB_DIR/$unit"
  if [[ ! -f "$src" ]]; then
    echo "install.sh: unit file $src missing" >&2
    exit 64
  fi
  run install -m 0644 "$src" "/etc/systemd/system/$unit"
done

# Strong validation: agent-center.service must NOT contain KillMode=
# control-group / mixed — server is allowed to use the default (it does
# not spawn shims).
# Worker is installed via install-worker.sh; the validation for KillMode=
# process happens there + at runtime via `agent-center bootstrap
# --check-systemd`.

step "systemctl daemon-reload"
run systemctl daemon-reload

step "Enable services"
run systemctl enable agent-center.service
run systemctl enable agent-center-backup.timer

step "Done. Next steps:"
cat <<'EOF'
  - Edit /etc/agent-center/config.yaml (copy from 04-configuration.md § 8.1)
  - sudo systemctl start agent-center
  - sudo journalctl -u agent-center -f
  - Visit http://127.0.0.1:7100 in a local browser for the Web Console
    (loopback-only; tunnel via SSH if accessing remotely).
EOF
