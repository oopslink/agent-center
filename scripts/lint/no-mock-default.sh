#!/usr/bin/env bash
# no-mock-default.sh — fail if mock-as-default sinks (NoopSender etc.)
# show up on production wiring paths without an explicit
# `// FIXME(prod-wiring):` annotation.
#
# Background: v2.0 GA shipped with dispatch.NoopSender{} and
# kill.NoopKillSender{} silently wired into the real server boot path,
# which meant dispatch events were swallowed and no one noticed until
# @oopslink hand-deployed the binary 2026-05-24. Conventions § 0.4
# enforce mechanism #2 codifies this rule:
#
#   `NoopSender` / `NoopKillSender` / `nil Spawner` 等出现在
#   `internal/cli/app.go` / `handlers_system.go` 必须有
#   `// FIXME(prod-wiring):` 注释；release tag pre-flight 阻止
#   任何带 `FIXME(prod-wiring)` 的 commit
#
# This script implements the first half (catch + force annotation);
# the release-tag pre-flight is a future ops gate (Phase F+).
#
# Run locally:
#   ./scripts/lint/no-mock-default.sh
# Or via make:
#   make lint-mock-default
#
# Exit codes:
#   0  — clean (no unwhitelisted mock-default literal usage)
#   1  — mock-as-default literal found on production path
#   64 — usage / setup error
#
# Adding a new mock sentinel:
#   1. extend the PATTERN regex below with the new type name (ERE)
#   2. document why it's mock-as-default in the type's godoc
#
# macOS BSD compatibility: uses POSIX `grep -E` only (no -P PCRE).
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

# Pattern: literal instantiation of the mock sentinels, e.g.
#   NoopSender{}            (same-package callsite)
#   dispatch.NoopSender{}   (qualified callsite)
#   kill.NoopKillSender{}
# We DO NOT flag type definitions (`type NoopSender struct{}`) or
# method receivers — those are the legitimate library code that the
# instantiations target. Filtering happens via grep context below.
PATTERN='(dispatch\.)?NoopSender\{\}|(kill\.)?NoopKillSender\{\}'

# Scope: only `internal/**/*.go` and `cmd/**/*.go`. Test files and the
# `tests/` integration / e2e dirs are intentionally out of scope —
# mock-as-default IS the right pattern for tests.
INCLUDE_PATHS=(
  'internal'
  'cmd'
)

# Run grep across the included paths, excluding _test.go.
# BSD grep on macOS supports --include / --exclude / -r.
HITS=""
for p in "${INCLUDE_PATHS[@]}"; do
  if [[ ! -d "$ROOT/$p" ]]; then
    continue
  fi
  # -r recursive, -n line numbers, -E extended regex, -I skip binary,
  # --include='*.go' --exclude='*_test.go' restrict scope.
  set +e
  out="$(grep -rnEI --include='*.go' --exclude='*_test.go' "$PATTERN" "$ROOT/$p" 2>/dev/null)"
  rc=$?
  set -e
  if [[ $rc -gt 1 ]]; then
    echo "no-mock-default: grep failed under $p (rc=$rc)" >&2
    exit 64
  fi
  if [[ -n "$out" ]]; then
    HITS+="$out"$'\n'
  fi
done

if [[ -z "$HITS" ]]; then
  echo "no-mock-default: clean (no mock-as-default literals on prod paths)"
  exit 0
fi

# For each hit, check whether the same line OR the immediately
# preceding line carries `FIXME(prod-wiring)`. If so, it's an
# explicitly acknowledged mock-default and we let it through.
VIOLATIONS=""
while IFS= read -r hit; do
  [[ -z "$hit" ]] && continue
  # Parse path:line:content
  path="${hit%%:*}"
  rest="${hit#*:}"
  lineno="${rest%%:*}"
  content="${rest#*:}"

  # Allow if same line has the FIXME marker.
  if [[ "$content" == *"FIXME(prod-wiring)"* ]]; then
    continue
  fi

  # Allow if the hit is inside a comment line. We're conservative:
  # only skip lines whose first non-whitespace token starts with `//`
  # (single-line comment) or `*` (continuation inside `/* ... */`).
  # This catches godoc that mentions the type literal by name (e.g.
  # `// Replaces v2.0 GA's NoopSender{}` in cli/app.go) without
  # creating a false positive.
  trimmed="$(echo "$content" | sed -E 's/^[[:space:]]+//')"
  if [[ "$trimmed" == //* || "$trimmed" == \** ]]; then
    continue
  fi

  # Allow if any of the 5 immediately preceding lines carries the
  # FIXME marker. Multi-line godoc-style annotations are common
  # (3-4 line comment block explaining the hazard); 5 lines is a
  # generous-but-not-arbitrary window that still catches drift
  # (a FIXME 20 lines away is not "annotating this callsite").
  annotated=false
  start_line=$(( lineno - 5 ))
  [[ $start_line -lt 1 ]] && start_line=1
  end_line=$(( lineno - 1 ))
  if [[ $end_line -ge $start_line ]]; then
    window="$(sed -n "${start_line},${end_line}p" "$path" 2>/dev/null || true)"
    if [[ "$window" == *"FIXME(prod-wiring)"* ]]; then
      annotated=true
    fi
  fi
  if $annotated; then
    continue
  fi

  VIOLATIONS+="$hit"$'\n'
done <<< "$HITS"

if [[ -n "$VIOLATIONS" ]]; then
  cat >&2 <<EOF
no-mock-default: mock-as-default literal found on production wiring path
(conventions § 0.4 enforce mechanism #2):

EOF
  printf '%s' "$VIOLATIONS" >&2
  cat >&2 <<EOF

Mock-as-default sinks (NoopSender / NoopKillSender) silently swallow
dispatch / kill envelopes when reached. v2.0 GA shipped with these
wired into the real server boot path — every dispatched task was
dropped and no one noticed until hand-deploy validation.

Two ways to clear this lint:

  1. Wire a real implementation (the right answer for prod paths).
     For dispatch / kill the canonical wiring is dispatchq:
       sender := dispatchq.DispatchSender{Q: queue}
       killer := dispatchq.KillSender{Q: queue}

  2. If the noop fallback is genuinely intentional (e.g. constructor
     defaulting that calls out the hazard), annotate it explicitly:

       // FIXME(prod-wiring): noop fallback — caller must pass a real
       // sender on the production path. See conventions § 0.4 #2.
       sender = NoopSender{}

Test files (*_test.go) and the tests/ tree are out of scope; mock-
as-default IS the right pattern there.
EOF
  exit 1
fi

echo "no-mock-default: clean (all mock-default hits explicitly annotated)"
exit 0
