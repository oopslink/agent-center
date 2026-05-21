#!/usr/bin/env bash
# install-worker.sh — bootstrap a worker machine.
#
# Per implementation/06-deployment § 10.2 + plan-7 § 3.6.
#
# Usage:
#   bash install-worker.sh [--binary=<path>] [--bootstrap-token=<token>] [--dry-run]
#
# Steps:
#   1. mkdir ~/.agent-center-worker
#   2. store bootstrap token (chmod 0600)
#   3. install binary to ~/.local/bin/agent-center
#   4. install user systemd unit (~/.config/systemd/user/agent-center-worker.service)
#   5. validate the unit contains KillMode=process (ADR-0018 hard
#      requirement; missing → exit 75)
#   6. systemctl --user daemon-reload + enable
#
# The KillMode validation also runs at daemon startup via
# `agent-center bootstrap --check-systemd` so an operator who hand-edits
# the unit file later still gets an error.

set -euo pipefail

BINARY=""
TOKEN=""
DRY_RUN=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary=*)
      BINARY="${1#--binary=}"; shift ;;
    --binary)
      shift; BINARY="$1"; shift ;;
    --bootstrap-token=*)
      TOKEN="${1#--bootstrap-token=}"; shift ;;
    --bootstrap-token)
      shift; TOKEN="$1"; shift ;;
    --dry-run)
      DRY_RUN=true; shift ;;
    -h|--help)
      sed -n '2,22p' "$0"; exit 0 ;;
    *)
      echo "unknown arg: $1" >&2; exit 64 ;;
  esac
done

run() {
  if "$DRY_RUN"; then
    printf '[dry-run] %s\n' "$*"
  else
    eval "$@"
  fi
}

CONTRIB_DIR="$(dirname -- "$(readlink -f "$0")")"
WORKER_DIR="$HOME/.agent-center-worker"
UNIT_FILE="$HOME/.config/systemd/user/agent-center-worker.service"
UNIT_SRC="$CONTRIB_DIR/agent-center-worker.service"

if [[ ! -f "$UNIT_SRC" ]]; then
  echo "install-worker.sh: unit file $UNIT_SRC missing" >&2
  exit 64
fi
if ! grep -q '^KillMode=process$' "$UNIT_SRC"; then
  echo "install-worker.sh: source unit $UNIT_SRC is missing KillMode=process" >&2
  echo "  ADR-0018 hard requirement (per-execution shim must outlive daemon)." >&2
  exit 75
fi

if [[ -z "$BINARY" ]] && ! "$DRY_RUN"; then
  candidate="$CONTRIB_DIR/agent-center"
  if [[ -x "$candidate" ]]; then
    BINARY="$candidate"
  else
    echo "install-worker.sh: --binary=<path> required" >&2
    exit 64
  fi
fi

run install -d -m 0700 "$WORKER_DIR"
if [[ -n "$TOKEN" ]]; then
  if "$DRY_RUN"; then
    echo "[dry-run] write bootstrap token to $WORKER_DIR/bootstrap-token"
  else
    printf '%s' "$TOKEN" > "$WORKER_DIR/bootstrap-token"
    chmod 0600 "$WORKER_DIR/bootstrap-token"
  fi
fi

if [[ -n "$BINARY" ]]; then
  run install -d -m 0755 "$HOME/.local/bin"
  run install -m 0755 "$BINARY" "$HOME/.local/bin/agent-center"
fi

run install -d -m 0700 "$(dirname "$UNIT_FILE")"
run install -m 0644 "$UNIT_SRC" "$UNIT_FILE"

# Validate installed unit a second time (catches manual tampering).
if ! "$DRY_RUN"; then
  if ! grep -q '^KillMode=process$' "$UNIT_FILE"; then
    echo "install-worker.sh: installed unit missing KillMode=process" >&2
    exit 75
  fi
fi

run systemctl --user daemon-reload
run systemctl --user enable agent-center-worker.service

# Final guard: invoke the installed binary's bootstrap check-systemd so
# the same logic that runs at daemon startup ratifies the install (defence
# in depth against script vs. CLI drift).
if [[ -x "$HOME/.local/bin/agent-center" ]] && ! "$DRY_RUN"; then
  if ! "$HOME/.local/bin/agent-center" bootstrap check-systemd --unit="$UNIT_FILE"; then
    echo "install-worker.sh: bootstrap check-systemd failed" >&2
    exit 75
  fi
fi

cat <<'EOF'
Worker install complete. Next steps:
  - Edit ~/.agent-center-worker/config.yaml (copy from 04-configuration § 8.2)
  - systemctl --user start agent-center-worker
  - journalctl --user -u agent-center-worker -f
EOF
