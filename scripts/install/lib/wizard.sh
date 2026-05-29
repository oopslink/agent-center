#!/usr/bin/env bash
# wizard.sh — interactive prompts for the source guided installer (§2.1).
# Only prompts for values that are still empty after flags + env are applied.
# In --non-interactive mode it never prompts; missing required values fail
# early in the orchestrator with the exact flag name (§6).

# _ask <prompt> <default> -> echoes the answer (default on empty input).
_ask() {
  local prompt="$1" default="$2" reply
  if [ -n "$default" ]; then
    printf '%s [%s]: ' "$prompt" "$default" >&2
  else
    printf '%s: ' "$prompt" >&2
  fi
  read -r reply || reply=""
  printf '%s' "${reply:-$default}"
}

# ac_wizard_mode — prompt for the install mode when none was given.
# Sets the global AC_MODE.
ac_wizard_mode() {
  [ -n "${AC_MODE:-}" ] && return 0
  if [ "${AC_NON_INTERACTIVE:-0}" -eq 1 ]; then
    ac_die "non-interactive mode: a mode is required (center | worker | dev)."
  fi
  ac_log ""
  ac_log "Select install mode:"
  ac_log "  1) center  — run the AgentCenter server on this host"
  ac_log "  2) worker  — enroll this host as a worker against a center"
  ac_log "  3) dev     — build + print local run commands (no service)"
  local choice
  choice="$(_ask "Choice" "1")"
  case "$choice" in
    1|center) AC_MODE="center" ;;
    2|worker) AC_MODE="worker" ;;
    3|dev)    AC_MODE="dev" ;;
    *) ac_die "invalid mode choice '$choice'." ;;
  esac
}

# ac_wizard_prefix — confirm the install prefix (center/worker only).
ac_wizard_prefix() {
  [ "$AC_MODE" = "dev" ] && return 0
  [ "${AC_PREFIX_EXPLICIT:-0}" -eq 1 ] && return 0
  [ "${AC_NON_INTERACTIVE:-0}" -eq 1 ] && return 0
  [ "${AC_ASSUME_YES:-0}" -eq 1 ] && return 0
  AC_PREFIX="$(_ask "Install prefix" "$AC_PREFIX")"
}

# ac_wizard_worker — collect worker enrollment values when missing.
# Center URL, fingerprint, and token are required for a worker install;
# the orchestrator validates them after this returns.
ac_wizard_worker() {
  [ "$AC_MODE" = "worker" ] || return 0
  if [ "${AC_NON_INTERACTIVE:-0}" -eq 1 ]; then
    return 0   # orchestrator validates required values and fails with flag names
  fi
  [ -z "${AC_CENTER_URL:-}" ] && AC_CENTER_URL="$(_ask "Center URL (tcp://host:7300 or unix:/path)" "")"
  [ -z "${AC_SERVER_FINGERPRINT:-}" ] && AC_SERVER_FINGERPRINT="$(_ask "Server fingerprint (sha256:...)" "")"
  if [ -z "${AC_ENROLL_TOKEN:-}" ]; then
    # Read the token without echoing it to the terminal where practical, and
    # never log its value (§1.2 / §2.3).
    ac_log "Enrollment token (input hidden; do not paste into shared shell history):"
    if [ -t 0 ]; then
      read -r -s AC_ENROLL_TOKEN || AC_ENROLL_TOKEN=""
      printf '\n' >&2
    else
      read -r AC_ENROLL_TOKEN || AC_ENROLL_TOKEN=""
    fi
  fi
  [ -z "${AC_WORKER_NAME:-}" ] && AC_WORKER_NAME="$(_ask "Worker name (friendly label, optional)" "")"
  # Explicit success: the trailing `[ -z ] && ...` test is false when the
  # value is already set, which would otherwise make this function (and thus
  # ac_wizard) return non-zero and trip `set -e`.
  return 0
}

# ac_wizard — run the full interactive fill. No-ops for values already set.
ac_wizard() {
  ac_wizard_mode
  ac_wizard_prefix
  ac_wizard_worker
  return 0
}
