#!/usr/bin/env bash
# no-idtail-hash.sh — fail if the React SPA reintroduces the retired
# `#<id-tail>` short-hash id-as-content encoding (T126). A work item / task /
# issue / plan with no human-facing org_ref must degrade to its FULL id (via
# refLabel in web/src/components/workItemDisplay.tsx), NEVER a 6-char hash like
# "#4e2e71" — which reads as noise and (the T126 bug) leaked for completed tasks
# whose org_ref the old FE re-resolver missed.
#
# Forbidden id-as-content hash forms (the regression net T100 lacked):
#   `#${<expr>.slice(-N)}`   — a `#` + trailing-id-tail template
#   `#${idHandle(<expr>)}`   — the removed idHandle() short-handle helper
#
# Run: ./scripts/lint/no-idtail-hash.sh   |   make lint-no-idtail-hash
#
# Exit codes: 0 clean / 1 violations / 64 setup error.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
TARGET="$ROOT/web/src"

if [[ ! -d "$TARGET" ]]; then
  echo "no-idtail-hash: missing target dir: $TARGET" >&2
  exit 64
fi

# ERE. A `#` immediately followed by a `${…}` interpolation whose body slices an
# id tail or calls the retired idHandle(). Tolerates whitespace inside `${ }`.
PATTERN='#\$\{[^}]*(slice\(-|idHandle\()'

set +e
HITS="$(grep -rnE --include='*.ts' --include='*.tsx' "$PATTERN" "$TARGET" 2>/dev/null)"
rc=$?
set -e
if [[ $rc -gt 1 ]]; then
  echo "no-idtail-hash: grep failed (rc=$rc)" >&2
  exit 64
fi

if [[ -n "$HITS" ]]; then
  echo "no-idtail-hash: retired #<id-tail> short-hash id encoding found in web/src/" >&2
  printf '%s\n' "$HITS" >&2
  echo "" >&2
  echo "Use refLabel(orgRef, id) from web/src/components/workItemDisplay.tsx:" >&2
  echo "it shows the human org_ref (T123 / I7 / P12) when present, else the FULL" >&2
  echo "id — never a #<id-tail> hash. (T126)" >&2
  exit 1
fi

echo "no-idtail-hash: clean (no #<id-tail> short-hash id encoding)"
exit 0
