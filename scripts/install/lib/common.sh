#!/usr/bin/env bash
# common.sh — shared helpers for the source guided installer (task #92).
# Sourced by source-installer.sh and the other lib/*.sh units. No top-level
# side effects beyond defining functions, so it is safe to source more than
# once.

# All human-facing output goes to stderr; this keeps stdout clean for any
# future machine-readable handoff and is pipe-safe under `curl | bash`.
ac_log()  { printf '%s\n' "$*" >&2; }
ac_info() { printf '  %s\n' "$*" >&2; }
ac_warn() { printf 'WARNING: %s\n' "$*" >&2; }
ac_die()  { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

# ac_run prints a command then runs it — unless AC_DRY_RUN=1, in which case
# it prints with a [dry-run] prefix and does NOT execute. This is the single
# mutation gate: every state-changing command in the installer goes through
# it, so --dry-run is guaranteed not to touch files or services.
ac_run() {
  if [ "${AC_DRY_RUN:-0}" -eq 1 ]; then
    printf '[dry-run] %s\n' "$*" >&2
    return 0
  fi
  printf '+ %s\n' "$*" >&2
  "$@"
}

# ac_confirm prompts the operator unless --yes / --non-interactive is set.
# Returns 0 to proceed, 1 to abort. In non-interactive mode without --yes it
# aborts (fail-closed); --yes accepts after the plan has been printed.
ac_confirm() {
  local prompt="${1:-Proceed?}"
  if [ "${AC_ASSUME_YES:-0}" -eq 1 ]; then
    return 0
  fi
  if [ "${AC_NON_INTERACTIVE:-0}" -eq 1 ]; then
    ac_die "non-interactive mode: refusing to proceed without --yes. ($prompt)"
  fi
  local reply
  printf '%s [y/N] ' "$prompt" >&2
  read -r reply || return 1
  case "$reply" in
    y|Y|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

# ac_secret_present echoes "set" / "unset" for logging a secret's presence
# without ever printing its value (token / fingerprint stay out of logs).
ac_secret_present() { [ -n "${1:-}" ] && printf 'set' || printf 'unset'; }
