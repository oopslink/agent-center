#!/usr/bin/env bash
# test-no-vendor-refs.sh — positive-fail self-test for no-vendor-refs.sh
#
# Per P12 S3 audit § 5: assert that the lint actually fires when a v1
# vendor token is introduced into one of each high-risk file type. The
# regular `make lint-vendor` only proves the tree is currently clean —
# it does not prove the lint catches a regression.
#
# Strategy:
#   1. Stage three throwaway files under scripts/lint/.selftest/ —
#      a .go, a .yaml, and a .json — each containing a v1 token.
#   2. `git add -N` (intent-to-add) so git grep — which the lint uses —
#      treats them as tracked. This mirrors the realistic scenario:
#      a contributor adds a new file, hits `git add`, runs the lint.
#   3. Run no-vendor-refs.sh; expect exit 1 + all three paths in
#      the violation output.
#   4. Remove the throwaway files + `git reset HEAD`; re-run; expect 0.
#
# Exit: 0 on full pass, 1 on any assertion failure.
set -uo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
LINT="$ROOT/scripts/lint/no-vendor-refs.sh"
STAGE="$ROOT/scripts/lint/.selftest"

cleanup() {
  # reverse the intent-to-add markers first, then remove files
  if [[ -d "$STAGE" ]]; then
    (cd "$ROOT" && git reset -q HEAD scripts/lint/.selftest 2>/dev/null || true)
    rm -rf "$STAGE"
  fi
}
trap cleanup EXIT

mkdir -p "$STAGE"

# Stage one file per high-risk extension. Use distinct tokens so the
# violation output is asserted on a per-extension basis.
cat > "$STAGE/sample.go" <<'EOF'
package selftest

// feishu_at must be rejected — sample injection for lint self-test
const Sample = "feishu"
EOF

cat > "$STAGE/sample.yaml" <<'EOF'
# sample yaml for lint self-test
feishu:
  app_id: injected-for-selftest
EOF

cat > "$STAGE/sample.json" <<'EOF'
{
  "feishu_app_id": "injected-for-selftest"
}
EOF

# Mark the new files as intent-to-add so `git grep` (which the lint
# uses) treats them as tracked.
(cd "$ROOT" && git add -N scripts/lint/.selftest/sample.go scripts/lint/.selftest/sample.yaml scripts/lint/.selftest/sample.json)

# --- Phase A: inject + expect violation -----------------------------------
echo "[selftest] phase A — expect lint to fail with 3 paths"
set +e
OUT_A="$("$LINT" 2>&1)"
RC_A=$?
set -e

if [[ "$RC_A" -eq 0 ]]; then
  echo "[selftest] FAIL: lint returned 0 with injected violations" >&2
  echo "$OUT_A" >&2
  exit 1
fi

MISSED=()
for f in sample.go sample.yaml sample.json; do
  if ! grep -q "scripts/lint/\.selftest/$f" <<< "$OUT_A"; then
    MISSED+=("$f")
  fi
done

if (( ${#MISSED[@]} > 0 )); then
  echo "[selftest] FAIL: lint missed violations in: ${MISSED[*]}" >&2
  echo "[selftest] full lint output was:" >&2
  echo "$OUT_A" >&2
  exit 1
fi

echo "[selftest] phase A OK — lint flagged all 3 extensions"

# --- Phase B: clean up + expect green -------------------------------------
echo "[selftest] phase B — expect lint to return clean"
cleanup
trap - EXIT

set +e
OUT_B="$("$LINT" 2>&1)"
RC_B=$?
set -e

if [[ "$RC_B" -ne 0 ]]; then
  echo "[selftest] FAIL: lint still failing after cleanup" >&2
  echo "$OUT_B" >&2
  exit 1
fi

echo "[selftest] phase B OK — lint clean after cleanup"
echo "[selftest] all assertions passed"
exit 0
