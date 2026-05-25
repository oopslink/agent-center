#!/usr/bin/env bash
# doc-impl-drift.sh — encode "ADR claims X → grep code condition Y" so
# documentation that's no longer true (or never was) fails fast.
#
# Background: v2.0 GA shipped with ADR-0038 § 88 stating "CLI 通过 admin
# endpoint 走 server 的 AppService" while `internal/cli/admin_client.go`
# did not exist. The CLI was opening sqlite directly. This kind of
# "doc says X / code says ¬X" drift is what conventions § 0.4 enforce
# mechanism #3 targets:
#
#   ADR 写"X 通过 transport Y"，自动 grep 是否反例
#   (如 ADR-0038 说"CLI 通过 admin endpoint"则 CLI handlers 不应
#   有 `persistence.Open`)
#
# Run locally:
#   ./scripts/lint/doc-impl-drift.sh
# Or via make:
#   make lint-doc-impl-drift
#
# Exit codes:
#   0  — clean (every check passes)
#   1  — one or more checks failed
#   64 — usage / setup error
#
# ## Adding a new check
#
# A check is a bash function named `check_<short_name>` that prints
# either "OK <name>: <evidence>" and returns 0, or "FAIL <name>:
# <message>" and returns non-zero. Register it by appending the name to
# the CHECKS array at the bottom of the script.
#
# Anchor each check to a specific doc claim (ADR section / conventions
# § / design doc § ) so the lint output points reviewers at WHY the
# code condition matters, not just at the failing grep.
#
# Constraints:
#   - macOS BSD compatibility — POSIX `grep -E` only, no PCRE
#   - no external deps beyond what the repo already uses
#   - keep each check fast (<1s) so this can stay in pre-commit /
#     pre-PR loops
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

# ----------------------------------------------------------------------
# Check #1 — conventions § 0.4 + ADR-0038: CLI talks to server via the
# admin endpoint; `internal/cli/admin_client.go` MUST exist.
#
# v2.0 GA shipped without this file; CLI handlers each called
# `persistence.Open` to bypass the server process entirely. v2.2-B
# (commit 397fa68) introduced it; this check stops a future "just
# inline the DB calls again" regression.
check_admin_client_exists() {
  local f="internal/cli/admin_client.go"
  if [[ -f "$f" ]]; then
    echo "OK admin_client_exists: $f present"
    return 0
  fi
  echo "FAIL admin_client_exists:
  conventions § 0.4 + ADR-0038 say the CLI talks to the server via
  the admin endpoint, which requires the Client type at:
    $f
  This file is missing — either the CLI is bypassing the server again
  (recreate $f and migrate handlers via v2.2 Phase B pattern) or the
  ADR claim needs to be retracted with a new ADR."
  return 1
}

# ----------------------------------------------------------------------
# Check #2 — conventions § 0.4 enforce mechanism #1 (already enforced
# by `internal/cli/arch_test.go`). Mirror it as a lint so the drift
# signal is visible WITHOUT needing to run the Go test (e.g. quick
# pre-commit). Same whitelist as arch_test.go.
check_handlers_no_persistence_open() {
  local whitelist_re='handlers_(migrate_v1_to_v2|system)\.go'
  local hits
  set +e
  hits="$(grep -lE 'persistence\.Open' internal/cli/handlers_*.go 2>/dev/null)"
  set -e
  local offenders=""
  while IFS= read -r f; do
    [[ -z "$f" ]] && continue
    base="$(basename "$f")"
    # Test files are mock-as-default territory — arch_test.go skips
    # them and so do we.
    if [[ "$base" == *_test.go ]]; then
      continue
    fi
    if [[ "$base" =~ $whitelist_re ]]; then
      continue
    fi
    offenders+="$f"$'\n'
  done <<< "$hits"
  if [[ -z "${offenders// }" ]]; then
    echo "OK handlers_no_persistence_open: no off-whitelist handler opens sqlite"
    return 0
  fi
  echo "FAIL handlers_no_persistence_open:
  conventions § 0.4 (AppService is the only entry to domain state)
  forbids CLI handlers from opening sqlite directly. The whitelist is
  handlers_migrate_v1_to_v2.go + handlers_system.go (the actual server
  boot path). These files violate the rule:
$offenders
  Either route through the admin endpoint via internal/cli/admin_client.go,
  or — if the handler is genuinely DB-owning (rare) — extend the
  arch_test.go whitelist and document why."
  return 1
}

# ----------------------------------------------------------------------
# Check #3 — conventions § 4 (zero LLM SDK dependency): no
# `import` of any LLM vendor SDK in the Go tree.
#
# The rule is about LLM SDKs specifically (anthropic / openai / gemini
# / google-genai etc.), not all vendor SDKs. The lark SDK is still
# tolerated here because it's a v1 vendor leftover under a separate
# remediation track (ADR-0031); flagging it would be out-of-scope
# noise. See no-vendor-refs.sh for the v1-vendor track.
check_no_llm_sdk_import() {
  local pat='github\.com/(anthropics|openai|sashabaranov/go-openai|google/generative-ai|google-deepmind)/'
  local hits
  set +e
  hits="$(grep -rnEI --include='*.go' "$pat" internal cmd 2>/dev/null)"
  set -e
  if [[ -z "$hits" ]]; then
    echo "OK no_llm_sdk_import: no LLM vendor SDK imports in internal/ or cmd/"
    return 0
  fi
  echo "FAIL no_llm_sdk_import:
  conventions § 4 (zero LLM SDK dependency): LLM capability MUST go
  through spawn agent CLI, not vendor SDK import. ADR-0002 is the
  binding decision. These imports violate the rule:

$hits

  Refactor to spawn the corresponding agent CLI (claudecode, codex,
  opencode) via internal/taskruntime/agent/* adapters."
  return 1
}

# ----------------------------------------------------------------------
# Registry. Each entry is the name of a `check_*` function above.
CHECKS=(
  check_admin_client_exists
  check_handlers_no_persistence_open
  check_no_llm_sdk_import
)

# ----------------------------------------------------------------------
# Run the registry. Each check prints its own OK/FAIL line; we
# aggregate the exit codes and print a summary at the end.
fails=0
total=${#CHECKS[@]}
echo "doc-impl-drift: running $total checks"
echo
for c in "${CHECKS[@]}"; do
  if ! "$c"; then
    fails=$(( fails + 1 ))
  fi
  echo
done

if [[ $fails -gt 0 ]]; then
  echo "doc-impl-drift: $fails of $total check(s) FAILED" >&2
  echo "(conventions § 0.4 enforce mechanism #3)" >&2
  exit 1
fi

echo "doc-impl-drift: all $total checks passed"
exit 0
