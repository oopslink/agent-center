#!/usr/bin/env bash
set -u

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
CANDIDATE="${ROOT}/../insight-candidate-16dc5815"
EVIDENCE="${ROOT}/docs/reports/t1578-s3-independent-recheck/evidence"
mkdir -p "$EVIDENCE"

LOG="$EVIDENCE/terminal.log"
SUMMARY="$EVIDENCE/verdict.jsonl"
: > "$LOG"
: > "$SUMMARY"

run() {
  local id="$1"
  shift
  local start end rc
  start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  {
    printf '\n===== COMMAND %s START %s =====\n' "$id" "$start"
    printf 'cwd=%s\n' "$(pwd)"
    printf 'argv='
    printf '%q ' "$@"
    printf '\n'
  } | tee -a "$LOG"
  "$@" 2>&1 | tee -a "$LOG"
  rc=${PIPESTATUS[0]}
  end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  {
    printf '===== COMMAND %s END %s rc=%s =====\n' "$id" "$end" "$rc"
  } | tee -a "$LOG"
  printf '{"id":"%s","start":"%s","end":"%s","rc":%s}\n' "$id" "$start" "$end" "$rc" >> "$SUMMARY"
  return "$rc"
}

cd "$ROOT" || exit 2
run root_status pwd
run root_git_status git status --short --branch
run task_input_find find . -maxdepth 4 -path '*task-input*' -print
run root_head git show --no-patch --format=fuller HEAD
run remote_refs git ls-remote origin refs/heads/s3/insight-phase1-candidate-20260827 refs/heads/ac-exec/task-5493aaeb/exec-9bd82743 refs/heads/ac-exec/task-eebbbe21/exec-26365c40 refs/heads/main

cd "$CANDIDATE" || exit 2
run candidate_status git status --short --branch
run candidate_head git show --no-patch --format=fuller HEAD
run candidate_describe git describe --always --dirty --tags
run candidate_ancestor_s2a git merge-base --is-ancestor 16a4120322f23007511d4609d0cb64d5982d0600 HEAD
run candidate_ancestor_s2br git merge-base --is-ancestor b9e25a6381b55a687b0d894a1f56fefc8ccbc5e0 HEAD
run candidate_ancestor_s2c git merge-base --is-ancestor 738bc0a6769b413dd4d04c6834207c62c2918fae HEAD
run candidate_ancestor_remediation git merge-base --is-ancestor 968dd76157d4b79755cc59a23163bbffbb1e5dc7 HEAD
run toolchain go version
run node_version node --version
run pnpm_version pnpm --version

run insight_unit go test -v ./internal/insight -count=1
run insight_race go test -race -p 1 ./internal/insight -count=3
run insight_api go test -v ./internal/webconsole/api -run 'TestInsights' -count=1

run build_backend make build-backend
run binary_sha shasum -a 256 ./bin/agent-center
run binary_version ./bin/agent-center --version

run system_version_unit go test -v ./internal/webconsole/api -run 'TestAPI_(Health|SystemVersion)' -count=1

cd "$CANDIDATE/web" || exit 2
run insight_ui pnpm exec vitest run src/pages/InsightOverview.test.tsx
run insight_tsc pnpm exec tsc -b --force
