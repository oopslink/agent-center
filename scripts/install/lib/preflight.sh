#!/usr/bin/env bash
# preflight.sh — environment checks for the source guided installer (§4.4).
# Detects unsupported platforms and missing build/runtime dependencies and
# prints copy-pasteable remediation hints. It does NOT install system
# packages (a first-version non-goal, §4.4 / §10).

# ac_preflight <mode>
# Returns non-zero (via ac_die) if a hard requirement is missing.
ac_preflight() {
  local mode="$1"
  ac_log "Running preflight checks…"

  # --- OS / arch (must match existing release support: macOS+Linux, arm64+amd64). ---
  local os arch
  os="$(uname -s)"
  arch="$(uname -m)"
  case "$os" in
    Darwin) ac_info "os:   macOS ($os)" ;;
    Linux)  ac_info "os:   Linux ($os)" ;;
    *) ac_die "unsupported OS '$os'. Source install supports macOS and Linux only." ;;
  esac
  case "$arch" in
    arm64|aarch64) ac_info "arch: arm64 ($arch)" ;;
    x86_64|amd64)  ac_info "arch: amd64 ($arch)" ;;
    *) ac_die "unsupported architecture '$arch'. Source install supports arm64 and amd64 only." ;;
  esac

  # --- Required commands. Collect all misses so the operator fixes them in one pass. ---
  local missing=0
  _need() {
    if command -v "$1" >/dev/null 2>&1; then
      ac_info "found: $1"
    else
      ac_warn "missing required command: $1 — $2"
      missing=1
    fi
  }
  _need git  "install git (https://git-scm.com/downloads)"
  _need go   "install Go 1.22+ (https://go.dev/dl/)"
  _need curl "install curl"
  _need tar  "install tar"

  # Node + a pnpm provider are needed for the embedded web console build.
  _need node "install Node.js 20+ (https://nodejs.org/)"
  if command -v pnpm >/dev/null 2>&1; then
    ac_info "found: pnpm"
  elif command -v corepack >/dev/null 2>&1; then
    ac_info "found: corepack (will provide pnpm)"
  else
    ac_warn "missing pnpm: enable corepack ('corepack enable') or install pnpm (https://pnpm.io/installation)"
    missing=1
  fi

  if [ "$missing" -ne 0 ]; then
    ac_die "preflight failed: install the missing dependencies above and re-run. (No packages are installed automatically.)"
  fi

  # --- Optional service manager (warn only; install path handles absence). ---
  if [ "$mode" != "dev" ]; then
    case "$os" in
      Darwin)
        command -v launchctl >/dev/null 2>&1 \
          && ac_info "service: launchctl present" \
          || ac_warn "launchctl not found; the install path may not be able to manage a background service." ;;
      Linux)
        command -v systemctl >/dev/null 2>&1 \
          && ac_info "service: systemctl present" \
          || ac_warn "systemctl not found; the install path may not be able to manage a background service." ;;
    esac
  fi

  ac_log "Preflight OK."
}
