#!/usr/bin/env bash
# no-raw-colors-spa.sh — fail if the React SPA reintroduces raw Tailwind
# palette classes without a paired `dark:` override or `// raw-color-ok:`
# annotation. Companion to the design-token migration; semantic tokens
# live in web/src/index.css + web/tailwind.config.js.
#
# Run: ./scripts/lint/no-raw-colors-spa.sh   |   make lint-no-raw-colors-spa
#
# Exit codes: 0 clean / 1 violations / 64 setup error.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
TARGET="$ROOT/web/src"

if [[ ! -d "$TARGET" ]]; then
  echo "no-raw-colors-spa: missing target dir: $TARGET" >&2
  exit 64
fi

# ERE. text-white / text-black are out of scope (paired with bg-brand
# on purpose; danger family is also guarded by src/a11y.test.tsx).
PATTERN='(^|[^-a-zA-Z0-9])((hover|focus|disabled|active|group-hover):)?(bg|text|border|divide|ring)-(slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-[0-9]{2,3}'

set +e
RAW="$(grep -rnE --include='*.ts' --include='*.tsx' "$PATTERN" "$TARGET" 2>/dev/null)"
rc=$?
set -e
if [[ $rc -gt 1 ]]; then
  echo "no-raw-colors-spa: grep failed (rc=$rc)" >&2
  exit 64
fi

VIOLATIONS=""
while IFS= read -r hit; do
  [[ -z "$hit" ]] && continue
  content="${hit#*:*:}"
  # Per-line escape hatch: same-line `dark:` utility OR `raw-color-ok:` tag.
  if [[ "$content" == *"dark:"* ]] || [[ "$content" == *"raw-color-ok:"* ]]; then
    continue
  fi
  VIOLATIONS+="$hit"$'\n'
done <<< "$RAW"

if [[ -n "$VIOLATIONS" ]]; then
  echo "no-raw-colors-spa: raw Tailwind palette classes found in web/src/" >&2
  printf '%s' "$VIOLATIONS" >&2
  echo "" >&2
  echo "Use semantic tokens from web/tailwind.config.js (bg-bg-elevated," >&2
  echo "text-text-primary, border-border-base, bg-brand, text-accent, …)" >&2
  echo "so <html class=\"dark\"> flips correctly. If a raw class is" >&2
  echo "intentional (e.g. terminal-style block), pair it with a dark:" >&2
  echo "override or annotate the line with // raw-color-ok: <reason>" >&2
  exit 1
fi

echo "no-raw-colors-spa: clean"
exit 0
