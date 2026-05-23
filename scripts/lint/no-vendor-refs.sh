#!/usr/bin/env bash
# no-vendor-refs.sh — fail if v1 vendor refs leak back into the tree.
#
# v2 撤回了飞书 / 钉钉 / 微信 / Bridge BC 等 vendor 集成（ADR-0031）。
# 这个脚本扫描代码 / 配置 / 脚本里残留的 v1 词汇，命中即 fail，除非
# 命中位于下面 WHITELIST 中（每条都附了 "intentional" 理由）。
#
# Run locally:
#   ./scripts/lint/no-vendor-refs.sh
# Or via make:
#   make lint-vendor
#
# Exit codes:
#   0  — clean (only whitelist hits)
#   1  — v1 vendor residue found
#   64 — usage / setup error
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

# Patterns we care about. -E (POSIX ERE) because git grep -P needs PCRE
# linked, and BSD/macOS git often ships without it.
PATTERN='feishu|lark|dingtalk|wechat|vendor_msg_ref|internal/bridge'

# Globs we scan. We restrict by path to keep the signal high — generated
# JSON, node_modules, vendored Go modules, and build outputs are out of
# scope (those would only echo upstream noise).
INCLUDE_GLOBS=(
  '*.go'
  '*.md'
  '*.yaml'
  '*.yml'
  '*.toml'
  '*.sql'
  '*.sh'
  '*.service'
  '*.timer'
  'Makefile'
  '*.mk'
  '*.ts'
  '*.tsx'
  # P12 S3 extensions: config-shaped formats that could harbor v1
  # vendor blocks if a future contributor reintroduces them.
  '*.json'
  '*.env'
  '*.env.*'
  '*.template'
  '*.tmpl'
  'Dockerfile'
  'Dockerfile.*'
)

# Whitelist — each line is `path:line_substring`. A hit is allowed iff
# both its path AND a substring of the matched line appear in this list.
# Keep these tight; broad allowances defeat the purpose.
#
# Reasons live in docs/plans/phase-12-audits/s1-v1-residue-audit.md.
WHITELIST_FILE="$ROOT/scripts/lint/no-vendor-refs.allowlist"

if [[ ! -f "$WHITELIST_FILE" ]]; then
  echo "no-vendor-refs: missing allowlist file: $WHITELIST_FILE" >&2
  exit 64
fi

# Build the git grep args. Use -I to skip binary files; -n for line nums.
ARGS=(-I -n -E "$PATTERN" --)
for g in "${INCLUDE_GLOBS[@]}"; do
  ARGS+=("$g")
done

# Always exclude these path prefixes (vendored, generated, third-party).
EXCLUDES=(
  ':!web/node_modules'
  ':!web/dist'
  ':!web/coverage'
  ':!internal/webconsole/spa/dist'
  ':!vendor'
  ':!.git'
  # P12 S3 extensions: lockfiles + build caches that can contain
  # transitive package names matching e.g. `vendor_*` without being v1
  # vendor residue.
  ':!**/package-lock.json'
  ':!**/pnpm-lock.yaml'
  ':!.vitepress/cache'
  ':!sites/.vitepress/dist'
  ':!sites/.vitepress/cache'
)
ARGS+=("${EXCLUDES[@]}")

# Run git grep; tolerate "no match" (exit 1) so we can build the diff.
set +e
RAW="$(git grep "${ARGS[@]}" 2>/dev/null)"
set -e

if [[ -z "$RAW" ]]; then
  echo "no-vendor-refs: clean (no hits at all)"
  exit 0
fi

# Filter out whitelist hits. Each whitelist entry is a regex matched
# against the FULL `path:line:content` line. Comments (#) and blank
# lines are ignored.
VIOLATIONS=""
while IFS= read -r hit; do
  allowed=false
  while IFS= read -r rule; do
    [[ -z "$rule" || "${rule:0:1}" == '#' ]] && continue
    if [[ "$hit" =~ $rule ]]; then
      allowed=true
      break
    fi
  done < "$WHITELIST_FILE"
  if ! "$allowed"; then
    VIOLATIONS+="$hit"$'\n'
  fi
done <<< "$RAW"

if [[ -n "$VIOLATIONS" ]]; then
  echo "no-vendor-refs: v1 vendor residue found (NOT in allowlist):" >&2
  printf '%s' "$VIOLATIONS" >&2
  echo "" >&2
  echo "Either remove the reference, or add an allowlist entry to" >&2
  echo "  scripts/lint/no-vendor-refs.allowlist" >&2
  echo "and document the reason in" >&2
  echo "  docs/plans/phase-12-audits/s1-v1-residue-audit.md" >&2
  exit 1
fi

echo "no-vendor-refs: clean (all hits whitelisted)"
exit 0
