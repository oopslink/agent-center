#!/usr/bin/env bash
# no-stale-e2e-config.sh — fail if a tests/e2e/v2 server config reintroduces a
# REMOVED config key. The deployed-binary smoke (the `make smoke` gate) and the
# shared e2e fixture write a YAML config and boot bin/agent-center against it; a
# stale key makes the server refuse to boot (`config: unknown YAML key …`) which
# is exactly how the deployed-smoke gate silently rotted (T212):
#
#   identity:
#     default_user: "hayang"   ← removed in v2.7 #162 (f0b92833)
#
# The smoke test itself re-pins this (it boots the server), but this lint gives
# fast, build-free feedback and guards EVERY e2e config — not just the one the
# gate happens to run.
#
# Forbidden (as a YAML config key, NOT in a // or * comment):
#   `default_user`        — the removed key
#   a top-level `identity:` block
#
# Run: ./scripts/lint/no-stale-e2e-config.sh   |   make lint-no-stale-e2e-config
#
# Exit codes: 0 clean / 1 violations / 64 setup error.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
TARGET="$ROOT/tests/e2e/v2"

if [[ ! -d "$TARGET" ]]; then
  echo "no-stale-e2e-config: missing target dir: $TARGET" >&2
  exit 64
fi

# Removed config keys, as they appear inside the YAML template literals. ERE.
# `identity:` is matched only as a line-leading YAML key (indented or not), so it
# never collides with prose; `default_user` is specific enough on its own.
PATTERN='(^[[:space:]]*identity:[[:space:]]*$|\bdefault_user[[:space:]]*:)'

set +e
# Grep .ts under the e2e tree, then drop // and * comment lines so the header
# docs that EXPLAIN the retired key (by name) are not flagged.
HITS="$(grep -rnE --include='*.ts' "$PATTERN" "$TARGET" 2>/dev/null \
  | grep -vE ':[[:space:]]*(//|\*)' )"
rc=$?
set -e
# grep returns 1 when the post-filter yields nothing — that's CLEAN, not error.
if [[ -n "$HITS" ]]; then
  echo "no-stale-e2e-config: removed config key found in tests/e2e/v2/" >&2
  printf '%s\n' "$HITS" >&2
  echo "" >&2
  echo "The 'identity.default_user' config key was removed in v2.7 #162; a server" >&2
  echo "booted with it errors 'config: unknown YAML key \"identity\"'. Drop the key" >&2
  echo "from the e2e config (current schema: server / web_console / secret_management)." >&2
  exit 1
fi

echo "no-stale-e2e-config: clean (no removed config keys in tests/e2e/v2)"
exit 0
