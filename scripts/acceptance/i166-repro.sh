#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PRODUCT_SHA="${I166_PRODUCT_SHA:-724c9b8660c1c269bf6e05913f2815d97828d40b}"

cd "$ROOT"
for tool in git go node pnpm; do
  command -v "$tool" >/dev/null || { echo "missing required tool: $tool" >&2; exit 1; }
done
git cat-file -e "${PRODUCT_SHA}^{commit}"
git merge-base --is-ancestor "$PRODUCT_SHA" HEAD || {
  echo "I166 product SHA $PRODUCT_SHA is not an ancestor of HEAD" >&2
  exit 1
}

echo "[i166] building production frontend and candidate backend"
make build-backend

echo "[i166] installing pinned browser harness dependencies"
pnpm --dir tests/e2e/v2 install --frozen-lockfile
if [[ "${I166_SKIP_BROWSER_INSTALL:-0}" != "1" ]]; then
  pnpm --dir tests/e2e/v2 exec playwright install chromium
fi

echo "[i166] running isolated real-chain fixture and browser verification"
I166_PRODUCT_SHA="$PRODUCT_SHA" node docs/acceptance/i166-plan-dag-real-chain.mjs

echo "[i166] evidence written to docs/acceptance/i166-evidence"
