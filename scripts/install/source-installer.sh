#!/usr/bin/env bash
#
# source-installer.sh — in-repo source guided installer (task #92).
#
# The thin top-level install.sh bootstrap clones/fetches the managed checkout,
# resolves + checks out the ref, then hands off to THIS script (which is
# versioned with the code it builds). This script owns the authoritative
# command-surface parse, preflight, wizard, build-to-staging, and the handoff
# to the existing `./install center|worker` path.
#
# It can also be run directly from a checkout for development:
#   bash scripts/install/source-installer.sh center --prefix ~/.agent-center
#
set -euo pipefail

# Resolve this script's dir so it works regardless of cwd / how it's invoked.
SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
LIB_DIR="${SELF_DIR}/lib"
# Repo root = two levels up from scripts/install/.
REPO_ROOT="$(cd "${SELF_DIR}/../.." && pwd)"

# shellcheck source=scripts/install/lib/common.sh
. "${LIB_DIR}/common.sh"
# shellcheck source=scripts/install/lib/preflight.sh
. "${LIB_DIR}/preflight.sh"
# shellcheck source=scripts/install/lib/wizard.sh
. "${LIB_DIR}/wizard.sh"
# shellcheck source=scripts/install/lib/build.sh
. "${LIB_DIR}/build.sh"
# shellcheck source=scripts/install/lib/stage-release.sh
. "${LIB_DIR}/stage-release.sh"

usage() {
  cat >&2 <<'EOF'
AgentCenter source installer (in-repo)

USAGE:
  source-installer.sh [center|worker|dev] [options]

See `install.sh --help` (the bootstrap) for the full flag and environment
reference. This script accepts the same flags.
EOF
}

# ---------------------------------------------------------------------------
# Configuration globals — seeded from env (flags override below). The
# bootstrap exports AGENT_CENTER_RESOLVED_REF / _COMMIT / _PREFIX / _SOURCE_DIR.
# ---------------------------------------------------------------------------
AC_MODE="${AGENT_CENTER_MODE:-}"
AC_PREFIX="${AGENT_CENTER_PREFIX:-${HOME}/.agent-center}"
# Track whether the prefix was supplied explicitly (flag or env) so the
# wizard does not re-prompt to "confirm" a value the operator already chose.
AC_PREFIX_EXPLICIT=0
[ -n "${AGENT_CENTER_PREFIX:-}" ] && AC_PREFIX_EXPLICIT=1
AC_SOURCE_DIR="${AGENT_CENTER_SOURCE_DIR:-$REPO_ROOT}"
AC_VERSION="${AGENT_CENTER_VERSION:-}"
AC_RESOLVED_REF="${AGENT_CENTER_RESOLVED_REF:-}"
AC_RESOLVED_COMMIT="${AGENT_CENTER_RESOLVED_COMMIT:-}"
AC_CENTER_URL="${AGENT_CENTER_CENTER_URL:-}"
AC_SERVER_FINGERPRINT="${AGENT_CENTER_SERVER_FINGERPRINT:-}"
AC_ENROLL_TOKEN="${AGENT_CENTER_ENROLL_TOKEN:-}"
AC_WORKER_NAME="${AGENT_CENTER_WORKER_NAME:-}"
AC_DRY_RUN=0
AC_ASSUME_YES=0
AC_NON_INTERACTIVE=0
export AC_DRY_RUN AC_ASSUME_YES AC_NON_INTERACTIVE

# First bare token may be the mode.
if [ "$#" -gt 0 ]; then
  case "$1" in
    center|worker|dev) AC_MODE="$1"; shift ;;
  esac
fi

# ---------------------------------------------------------------------------
# Full argument parse (authoritative). Flags win over env.
# ---------------------------------------------------------------------------
while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)            AC_VERSION="${2:-}"; shift 2 ;;
    --version=*)          AC_VERSION="${1#*=}"; shift ;;
    --channel)            shift 2 ;;            # resolved by the bootstrap
    --channel=*)          shift ;;
    --repo)               shift 2 ;;            # used by the bootstrap only
    --repo=*)             shift ;;
    --prefix)             AC_PREFIX="${2:-}"; AC_PREFIX_EXPLICIT=1; shift 2 ;;
    --prefix=*)           AC_PREFIX="${1#*=}"; AC_PREFIX_EXPLICIT=1; shift ;;
    --source-dir)         AC_SOURCE_DIR="${2:-}"; shift 2 ;;
    --source-dir=*)       AC_SOURCE_DIR="${1#*=}"; shift ;;
    --expected-commit)    shift 2 ;;            # verified by the bootstrap
    --expected-commit=*)  shift ;;
    --center)             AC_CENTER_URL="${2:-}"; shift 2 ;;
    --center=*)           AC_CENTER_URL="${1#*=}"; shift ;;
    --server-fingerprint) AC_SERVER_FINGERPRINT="${2:-}"; shift 2 ;;
    --server-fingerprint=*) AC_SERVER_FINGERPRINT="${1#*=}"; shift ;;
    --token)              AC_ENROLL_TOKEN="${2:-}"; shift 2 ;;
    --token=*)            AC_ENROLL_TOKEN="${1#*=}"; shift ;;
    --worker-name)        AC_WORKER_NAME="${2:-}"; shift 2 ;;
    --worker-name=*)      AC_WORKER_NAME="${1#*=}"; shift ;;
    --dry-run)            AC_DRY_RUN=1; shift ;;
    --yes|-y)             AC_ASSUME_YES=1; shift ;;
    --non-interactive)    AC_NON_INTERACTIVE=1; shift ;;
    -h|--help)            usage; exit 0 ;;
    *)                    ac_die "unknown argument: $1 (see install.sh --help)" ;;
  esac
done
export AC_DRY_RUN AC_ASSUME_YES AC_NON_INTERACTIVE

# Version to build: explicit --version wins; else the ref the bootstrap
# resolved + checked out; else whatever HEAD is at (direct dev runs).
BUILD_VERSION="${AC_VERSION:-${AC_RESOLVED_REF:-}}"
if [ -z "$BUILD_VERSION" ]; then
  BUILD_VERSION="$(git -C "$AC_SOURCE_DIR" describe --tags --always 2>/dev/null || echo dev)"
fi

# ---------------------------------------------------------------------------
# Wizard fills any missing values (no-op in non-interactive / when set).
# ---------------------------------------------------------------------------
ac_wizard

[ -n "$AC_MODE" ] || ac_die "no install mode resolved (center | worker | dev)."

# ---------------------------------------------------------------------------
# Validate required values per mode (fail early with exact flag names, §6).
# ---------------------------------------------------------------------------
if [ "$AC_MODE" = "worker" ]; then
  [ -n "$AC_CENTER_URL" ]   || ac_die "worker install requires a center URL (--center or AGENT_CENTER_CENTER_URL)."
  [ -n "$AC_ENROLL_TOKEN" ] || ac_die "worker install requires an enrollment token (--token or AGENT_CENTER_ENROLL_TOKEN)."
  case "$AC_CENTER_URL" in
    tcp://*)
      [ -n "$AC_SERVER_FINGERPRINT" ] || ac_die "tcp:// center requires --server-fingerprint (or AGENT_CENTER_SERVER_FINGERPRINT)." ;;
  esac
fi

# ---------------------------------------------------------------------------
# Print the resolved plan, then confirm (skipped with --yes / on dry-run).
# ---------------------------------------------------------------------------
ac_log ""
ac_log "Resolved install plan:"
ac_info "mode:            ${AC_MODE}"
ac_info "version (build): ${BUILD_VERSION}"
[ -n "$AC_RESOLVED_COMMIT" ] && ac_info "commit:          ${AC_RESOLVED_COMMIT}"
ac_info "source-dir:      ${AC_SOURCE_DIR}"
[ "$AC_MODE" != "dev" ] && ac_info "prefix:          ${AC_PREFIX}"
if [ "$AC_MODE" = "worker" ]; then
  ac_info "center:          ${AC_CENTER_URL}"
  ac_info "fingerprint:     $(ac_secret_present "$AC_SERVER_FINGERPRINT")"
  ac_info "token:           $(ac_secret_present "$AC_ENROLL_TOKEN") (value not logged)"
fi
ac_log ""

if [ "$AC_DRY_RUN" -ne 1 ]; then
  ac_confirm "Proceed with build + ${AC_MODE} install?" || ac_die "aborted by user."
fi

# ---------------------------------------------------------------------------
# Preflight, then build into a temp staging dir, then reuse the install path.
# Staging lives under the system temp dir so a failed build never touches the
# install prefix (§5.3).
# ---------------------------------------------------------------------------
ac_preflight "$AC_MODE"

STAGING_DIR="${TMPDIR:-/tmp}/agent-center-stage-${BUILD_VERSION//\//-}"
ac_build_stage "$AC_SOURCE_DIR" "$BUILD_VERSION" "$STAGING_DIR"

case "$AC_MODE" in
  center) ac_install_center "$STAGING_DIR" "$AC_PREFIX" ;;
  worker) ac_install_worker "$STAGING_DIR" "$AC_PREFIX" "$AC_CENTER_URL" "$AC_SERVER_FINGERPRINT" "$AC_ENROLL_TOKEN" "$AC_WORKER_NAME" ;;
  dev)    ac_dev_finish "$STAGING_DIR" "$AC_SOURCE_DIR" ;;
esac

ac_log ""
if [ "$AC_DRY_RUN" -eq 1 ]; then
  ac_log "Dry-run complete. No files or services were changed."
else
  ac_log "Done."
fi
