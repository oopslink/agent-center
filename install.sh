#!/usr/bin/env bash
#
# install.sh — AgentCenter source guided installer (thin bootstrap).
#
# This is the first-mile entrypoint for the SOURCE install path (task #92,
# docs/plans/source-guided-installer.md). It is intentionally small and
# auditable so it is safe to run via:
#
#   curl -fsSL https://raw.githubusercontent.com/oopslink/agent-center/main/install.sh | bash
#
# It is an ADDITIONAL path, not a replacement for the release tarball
# (which remains the recommended stable production install).
#
# Responsibilities of this bootstrap (kept deliberately minimal):
#   1. Parse minimal flags / environment, print --help.
#   2. Check `git` and shell compatibility.
#   3. Clone or fetch the managed source checkout.
#   4. Checkout the requested ref and print the resolved commit.
#   5. Verify the optional --expected-commit integrity guard.
#   6. Hand off to the versioned in-repo installer
#      (scripts/install/source-installer.sh), which owns build + staging +
#      reuse of the existing `./install center|worker` path.
#
# Safety posture (§1.2 of the design):
#   - No hidden sudo / privilege escalation in this bootstrap.
#   - Resolved repo, ref, and commit are printed before any build/install.
#   - --dry-run never mutates files or services.
#   - The `main` channel is labeled development/unstable.
#   - Secrets (--token / --server-fingerprint) are forwarded but never logged.
#
set -euo pipefail

# ---------------------------------------------------------------------------
# Defaults (flags override env, env overrides these — §3.2).
# ---------------------------------------------------------------------------
DEFAULT_REPO="https://github.com/oopslink/agent-center.git"
DEFAULT_PREFIX="${HOME}/.agent-center"
# Managed source checkout lives under the prefix by default so a single
# directory owns everything the source install touches.
DEFAULT_SOURCE_DIR="${HOME}/.agent-center/src/agent-center"

# ---------------------------------------------------------------------------
# Small output helpers. Everything goes to stderr except the final handoff,
# so the script is pipe-safe and machine output stays clean.
# ---------------------------------------------------------------------------
log()  { printf '%s\n' "$*" >&2; }
info() { printf '  %s\n' "$*" >&2; }
warn() { printf 'WARNING: %s\n' "$*" >&2; }
die()  { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

usage() {
  cat >&2 <<'EOF'
AgentCenter source guided installer (bootstrap)

USAGE:
  curl -fsSL https://raw.githubusercontent.com/oopslink/agent-center/main/install.sh | bash
  curl -fsSL .../install.sh | bash -s -- center  [options]
  curl -fsSL .../install.sh | bash -s -- worker  --center <url> --token <tok> --server-fingerprint <fp> [options]
  curl -fsSL .../install.sh | bash -s -- dev      [options]

MODES:
  center            Install or upgrade the Center service on this host.
  worker            Install or upgrade a Worker that enrolls against a Center.
  dev               Clone + build, print local run commands (no service).
  (omitted)         Interactive wizard prompts for the mode.

COMMON OPTIONS:
  --version <ref>          Exact git tag or commit SHA to build (preferred for stable).
  --channel stable|main|dev  Convenience channel when --version is not given.
  --repo <url>             Repository URL (default: GitHub oopslink/agent-center).
  --prefix <path>          Install prefix (default: ~/.agent-center).
  --source-dir <path>      Managed source checkout (default: ~/.agent-center/src/agent-center).
  --expected-commit <sha>  Abort if the checked-out commit does not match.
  --yes                    Accept prompts after printing the resolved plan.
  --non-interactive        Fail instead of prompting for missing required values.
  --dry-run                Print planned clone/build/install actions; mutate nothing.
  -h, --help               Show this help.

WORKER OPTIONS:
  --center <url>           Center admin endpoint (tcp://host:7300 or unix:/path).
  --server-fingerprint <v> Pinned server TLS fingerprint (sha256:...). Required for tcp://.
  --token <value>          One-time enrollment token from the Web Console.
  --worker-name <name>     Friendly Worker label.

ENVIRONMENT (flags win over env):
  AGENT_CENTER_MODE, AGENT_CENTER_VERSION, AGENT_CENTER_CHANNEL, AGENT_CENTER_REPO,
  AGENT_CENTER_PREFIX, AGENT_CENTER_SOURCE_DIR, AGENT_CENTER_CENTER_URL,
  AGENT_CENTER_SERVER_FINGERPRINT, AGENT_CENTER_ENROLL_TOKEN, AGENT_CENTER_WORKER_NAME

NOTES:
  - The release tarball remains the recommended STABLE production install path.
  - Pin a tag (--version vX.Y.Z) for stable installs. The `main` channel is unstable.
  - The enrollment token is sensitive; avoid pasting it into shared shell history.
EOF
}

# ---------------------------------------------------------------------------
# Parse the flags this bootstrap needs for clone/checkout. ALL original
# arguments are also forwarded verbatim to the in-repo installer, which
# performs the authoritative full parse — this keeps the command surface
# defined in exactly one place (scripts/install/source-installer.sh).
# ---------------------------------------------------------------------------
REPO="${AGENT_CENTER_REPO:-$DEFAULT_REPO}"
PREFIX="${AGENT_CENTER_PREFIX:-$DEFAULT_PREFIX}"
SOURCE_DIR="${AGENT_CENTER_SOURCE_DIR:-$DEFAULT_SOURCE_DIR}"
VERSION="${AGENT_CENTER_VERSION:-}"
CHANNEL="${AGENT_CENTER_CHANNEL:-}"
EXPECTED_COMMIT=""
DRY_RUN=0
ASSUME_YES=0
NON_INTERACTIVE=0
MODE="${AGENT_CENTER_MODE:-}"

ORIG_ARGS=("$@")

# Capture mode if the first token is a bare mode word.
if [ "$#" -gt 0 ]; then
  case "$1" in
    center|worker|dev) MODE="$1"; shift ;;
  esac
fi

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)         VERSION="${2:-}"; shift 2 ;;
    --version=*)       VERSION="${1#*=}"; shift ;;
    --channel)         CHANNEL="${2:-}"; shift 2 ;;
    --channel=*)       CHANNEL="${1#*=}"; shift ;;
    --repo)            REPO="${2:-}"; shift 2 ;;
    --repo=*)          REPO="${1#*=}"; shift ;;
    --prefix)          PREFIX="${2:-}"; shift 2 ;;
    --prefix=*)        PREFIX="${1#*=}"; shift ;;
    --source-dir)      SOURCE_DIR="${2:-}"; shift 2 ;;
    --source-dir=*)    SOURCE_DIR="${1#*=}"; shift ;;
    --expected-commit) EXPECTED_COMMIT="${2:-}"; shift 2 ;;
    --expected-commit=*) EXPECTED_COMMIT="${1#*=}"; shift ;;
    --dry-run)         DRY_RUN=1; shift ;;
    --yes|-y)          ASSUME_YES=1; shift ;;
    --non-interactive) NON_INTERACTIVE=1; shift ;;
    -h|--help)         usage; exit 0 ;;
    *)                 shift ;;  # forwarded; in-repo script validates the rest
  esac
done

# ---------------------------------------------------------------------------
# Resolve which git ref we will build (§2.2 / open question #1).
#   --version wins. Otherwise --channel: stable -> latest vX.Y.Z tag,
#   main/dev -> the main branch (labeled unstable).
# Actual tag discovery happens after fetch in resolve_ref().
# ---------------------------------------------------------------------------
CHANNEL="${CHANNEL:-main}"   # default channel when neither version nor channel given

# Require git up front — no point cloning without it.
command -v git >/dev/null 2>&1 || die "git is required but was not found on PATH. Install git and re-run."

# A POSIX-ish bash is assumed (curl | bash invokes bash). Guard anyway.
if [ -z "${BASH_VERSION:-}" ]; then
  die "this installer must run under bash (e.g. 'curl -fsSL ... | bash')."
fi

# Non-interactive runs must have every required value up front. A missing
# mode is fatal — fail HERE, before any clone/fetch, so CI/script misuse does
# no needless network or checkout mutation. (The in-repo wizard also enforces
# this, but only after the checkout exists; bootstrap catches it earlier.)
if [ "$NON_INTERACTIVE" -eq 1 ] && [ -z "$MODE" ]; then
  die "non-interactive mode: a mode is required (center | worker | dev)."
fi

# ---------------------------------------------------------------------------
# Print the resolved bootstrap plan. This always runs before any mutation
# so the operator can see repo / ref / prefix / mode up front (§1.2).
# ---------------------------------------------------------------------------
log ""
log "AgentCenter source installer"
info "repo:        ${REPO}"
info "mode:        ${MODE:-<wizard>}"
if [ -n "$VERSION" ]; then
  info "ref:         ${VERSION} (explicit --version)"
else
  info "ref:         channel '${CHANNEL}'"
fi
info "source-dir:  ${SOURCE_DIR}"
info "prefix:      ${PREFIX}"
[ "$DRY_RUN" -eq 1 ] && info "dry-run:     yes (no files or services will change)"
log ""

INSTALLER_REL="scripts/install/source-installer.sh"

# ---------------------------------------------------------------------------
# git helpers
# ---------------------------------------------------------------------------
git_in_src() { git -C "$SOURCE_DIR" "$@"; }

# resolve_ref echoes the concrete ref to checkout, given VERSION/CHANNEL,
# after the checkout has been fetched. Latest-stable = highest vX.Y.Z tag.
resolve_ref() {
  if [ -n "$VERSION" ]; then
    printf '%s' "$VERSION"
    return 0
  fi
  case "$CHANNEL" in
    stable)
      local latest
      latest="$(git_in_src tag -l 'v*' | sort -V | tail -1 || true)"
      if [ -n "$latest" ]; then
        printf '%s' "$latest"
      else
        warn "no release tags found; falling back to 'main' (unstable)."
        printf 'main'
      fi
      ;;
    main|dev|*)
      printf 'main'
      ;;
  esac
}

# ---------------------------------------------------------------------------
# Dry-run: print the plan and (if a checkout already exists) hand off so the
# in-repo script can print its build/stage/install plan too. Never clone,
# fetch, or checkout — those are mutations.
# ---------------------------------------------------------------------------
if [ "$DRY_RUN" -eq 1 ]; then
  log "[dry-run] would clone/update ${REPO} -> ${SOURCE_DIR}"
  if [ -n "$VERSION" ]; then
    log "[dry-run] would checkout ref: ${VERSION}"
  else
    log "[dry-run] would resolve channel '${CHANNEL}' to a ref and checkout"
  fi
  [ "$CHANNEL" = "main" ] && [ -z "$VERSION" ] && warn "channel 'main' is development/unstable; pin a tag with --version for stable installs."
  if [ -f "${SOURCE_DIR}/${INSTALLER_REL}" ]; then
    log "[dry-run] handing off to existing checkout for build/install plan…"
    exec bash "${SOURCE_DIR}/${INSTALLER_REL}" "${ORIG_ARGS[@]+"${ORIG_ARGS[@]}"}"
  fi
  log "[dry-run] source not yet cloned; re-run without --dry-run to clone and continue."
  exit 0
fi

# ---------------------------------------------------------------------------
# Clone or fetch the managed checkout.
# ---------------------------------------------------------------------------
if [ -d "${SOURCE_DIR}/.git" ]; then
  log "Updating managed source checkout at ${SOURCE_DIR} …"
  git_in_src remote set-url origin "$REPO" 2>/dev/null || true
  git_in_src fetch --tags --prune origin
else
  if [ -e "$SOURCE_DIR" ] && [ -n "$(ls -A "$SOURCE_DIR" 2>/dev/null || true)" ]; then
    die "source-dir ${SOURCE_DIR} exists and is not a git checkout. Refusing to overwrite a non-managed directory. Pass --source-dir to a fresh path or remove it."
  fi
  log "Cloning ${REPO} -> ${SOURCE_DIR} …"
  mkdir -p "$(dirname "$SOURCE_DIR")"
  git clone "$REPO" "$SOURCE_DIR"
  git_in_src fetch --tags --prune origin || true
fi

# ---------------------------------------------------------------------------
# Resolve + checkout the ref.
# ---------------------------------------------------------------------------
REF="$(resolve_ref)"
[ -n "$REF" ] || die "could not resolve a git ref to build."
if [ "$REF" = "main" ] && [ -z "$VERSION" ]; then
  warn "channel 'main' is development/unstable. Pin a tag with --version vX.Y.Z for stable installs."
fi

log "Checking out ${REF} …"
# Prefer the remote-tracking ref for branches; fall back to the bare ref for
# tags / SHAs.
if git_in_src rev-parse --verify --quiet "origin/${REF}^{commit}" >/dev/null 2>&1; then
  git_in_src checkout -q -B "$REF" "origin/${REF}"
else
  git_in_src checkout -q --detach "$REF"
fi

RESOLVED_COMMIT="$(git_in_src rev-parse HEAD)"
log ""
info "resolved ref:    ${REF}"
info "resolved commit: ${RESOLVED_COMMIT}"
log ""

# ---------------------------------------------------------------------------
# Optional integrity guard (§3.1 --expected-commit).
# ---------------------------------------------------------------------------
if [ -n "$EXPECTED_COMMIT" ]; then
  case "$RESOLVED_COMMIT" in
    "$EXPECTED_COMMIT"*) : ;;  # allow short-sha prefix match
    *) die "expected-commit mismatch: wanted ${EXPECTED_COMMIT}, got ${RESOLVED_COMMIT}. Aborting before build." ;;
  esac
  info "expected-commit OK (${EXPECTED_COMMIT})"
fi

# ---------------------------------------------------------------------------
# Hand off to the versioned in-repo installer. Pass the resolved ref/commit
# via env so the in-repo script does not have to re-resolve, and forward all
# original arguments verbatim for the authoritative parse.
# ---------------------------------------------------------------------------
[ -f "${SOURCE_DIR}/${INSTALLER_REL}" ] || die "in-repo installer ${INSTALLER_REL} not found at the checked-out ref ${REF}. This ref may predate the source installer."

log "Handing off to ${INSTALLER_REL} …"
export AGENT_CENTER_RESOLVED_REF="$REF"
export AGENT_CENTER_RESOLVED_COMMIT="$RESOLVED_COMMIT"
export AGENT_CENTER_PREFIX="$PREFIX"
export AGENT_CENTER_SOURCE_DIR="$SOURCE_DIR"
exec bash "${SOURCE_DIR}/${INSTALLER_REL}" "${ORIG_ARGS[@]+"${ORIG_ARGS[@]}"}"
