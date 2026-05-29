#!/usr/bin/env bash
# source-installer_test.sh — offline shell-level tests for the source guided
# installer (task #92, S6). These assert the SAFETY-CRITICAL behaviors the PM
# gates on, without any network or actual build:
#
#   - install.sh --help works and documents the flag surface.
#   - --dry-run mutates nothing (no clone, no staging dir, no service).
#   - The worker enrollment token is never printed, even in --dry-run.
#   - Non-interactive mode fails early with the exact missing flag name.
#   - Preflight fails fast when a required dependency is missing.
#
# Run locally:
#   ./scripts/install/source-installer_test.sh
# Or via make:
#   make test-install
#
# Exit codes:
#   0  — all assertions pass
#   1  — one or more assertions failed
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")/../.." && pwd)"
BOOTSTRAP="${REPO_ROOT}/install.sh"
INSTALLER="${REPO_ROOT}/scripts/install/source-installer.sh"

PASS=0
FAIL=0
note() { printf '  %s\n' "$*"; }
ok()   { PASS=$((PASS+1)); printf 'PASS: %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf 'FAIL: %s\n' "$1"; [ -n "${2:-}" ] && note "$2"; }

# assert_contains <name> <haystack> <needle>
assert_contains() {
  case "$2" in
    *"$3"*) ok "$1" ;;
    *) bad "$1" "expected output to contain: $3" ;;
  esac
}
# assert_not_contains <name> <haystack> <needle>
assert_not_contains() {
  case "$2" in
    *"$3"*) bad "$1" "output unexpectedly contained: $3" ;;
    *) ok "$1" ;;
  esac
}

# --- 1. bootstrap --help -----------------------------------------------------
out="$(bash "$BOOTSTRAP" --help 2>&1)"
assert_contains "help: documents center/worker/dev modes" "$out" "worker"
assert_contains "help: documents --dry-run"               "$out" "--dry-run"
assert_contains "help: documents --version"               "$out" "--version"
assert_contains "help: labels token as sensitive"         "$out" "sensitive"

# --- 2. bootstrap --dry-run with a non-existent checkout: no clone, exit 0 ---
TMP_SRC="$(mktemp -d)/no-such-checkout"
out="$(AGENT_CENTER_SOURCE_DIR="$TMP_SRC" bash "$BOOTSTRAP" center --version v9.9.9 --dry-run 2>&1)"
rc=$?
[ "$rc" -eq 0 ] && ok "dry-run bootstrap exits 0" || bad "dry-run bootstrap exits 0" "rc=$rc"
assert_contains "dry-run: announces it would clone"        "$out" "[dry-run] would clone"
[ -e "$TMP_SRC" ] && bad "dry-run: did not create checkout" "checkout dir was created" || ok "dry-run: did not create checkout"

# --- 3. in-repo installer --dry-run: prints plan, mutates nothing ------------
STAGE_PROBE="${TMPDIR:-/tmp}/agent-center-stage-v2.6.0-srctesttok"
rm -rf "$STAGE_PROBE"
out="$(bash "$INSTALLER" center --version v2.6.0-srctesttok --prefix /tmp/ac-probe --dry-run --yes 2>&1)"
assert_contains "in-repo dry-run: prints resolved commit line" "$out" "version (build): v2.6.0-srctesttok"
assert_contains "in-repo dry-run: would not change files"      "$out" "No files or services were changed"
[ -e "$STAGE_PROBE" ] && bad "in-repo dry-run: no staging dir" "staging dir created" || ok "in-repo dry-run: no staging dir"

# --- 4. worker --dry-run never prints the token value ------------------------
SECRET="enroll_TOPSECRET_$$"
out="$(bash "$INSTALLER" worker --version v0 --center tcp://127.0.0.1:7300 \
        --server-fingerprint sha256:deadbeef --token "$SECRET" --worker-name wtest \
        --dry-run --yes 2>&1)"
assert_not_contains "worker dry-run: token value never logged" "$out" "$SECRET"
assert_contains     "worker dry-run: token shown redacted"     "$out" "token=***"
assert_contains     "worker dry-run: fingerprint redacted"     "$out" "server-fingerprint=***"

# --- 5. non-interactive missing token: fail early with the flag name ---------
out="$(bash "$INSTALLER" worker --center tcp://h:7300 --server-fingerprint sha256:x --non-interactive 2>&1)"
rc=$?
[ "$rc" -ne 0 ] && ok "non-interactive missing token: non-zero exit" || bad "non-interactive missing token: non-zero exit"
assert_contains "non-interactive: names the --token flag" "$out" "--token"

# --- 6. non-interactive tcp worker without fingerprint: names the flag -------
out="$(bash "$INSTALLER" worker --center tcp://h:7300 --token tok --non-interactive 2>&1)"
assert_contains "non-interactive: names --server-fingerprint" "$out" "--server-fingerprint"

# --- 6b. bootstrap --non-interactive without a mode: fail BEFORE any clone ---
# Regression guard (PM P1): the bootstrap must reject a missing mode up front,
# not clone/fetch first and let the in-repo wizard fail late.
TMP_SRC2="$(mktemp -d)/no-such-checkout-2"
out="$(AGENT_CENTER_SOURCE_DIR="$TMP_SRC2" bash "$BOOTSTRAP" --non-interactive 2>&1)"
rc=$?
[ "$rc" -ne 0 ] && ok "bootstrap non-interactive no-mode: non-zero exit" || bad "bootstrap non-interactive no-mode: non-zero exit"
assert_contains "bootstrap non-interactive no-mode: says a mode is required" "$out" "a mode is required"
[ -e "$TMP_SRC2" ] && bad "bootstrap non-interactive no-mode: no clone" "checkout dir was created" || ok "bootstrap non-interactive no-mode: no clone"

# --- 7. preflight fails fast when a required dependency is missing -----------
# Restrict PATH so `go` (and likely node/pnpm) are not found; preflight must
# abort before any build. Keep the shell builtins / coreutils available.
out="$(PATH="/usr/bin:/bin" bash "$INSTALLER" center --version v0 --prefix /tmp/ac-probe2 --yes 2>&1)"
rc=$?
[ "$rc" -ne 0 ] && ok "preflight missing dep: non-zero exit" || bad "preflight missing dep: non-zero exit"
assert_contains "preflight: reports a missing command or failure" "$out" "preflight failed"

# --- summary -----------------------------------------------------------------
printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
