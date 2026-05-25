#!/usr/bin/env bash
# deploy-smoke.sh — deployed-binary smoke gate (conventions § 0.4
# enforce mechanism #4).
#
# Builds fresh binaries (agent-center + worker-daemon + fakeagent),
# then drives the full task-dispatch pipeline against the REAL binaries
# (no in-process shortcuts):
#
#   bin/agent-center server                ──┐  unix socket
#                                            admin endpoint
#   bin/agent-center-worker-daemon       ──┘
#         │
#         └── spawns bin/fakeagent --script=...
#
# Implementation strategy: delegate to the Playwright spec that
# already encodes the topology + assertions:
#
#   tests/e2e/v2/tests/v22-deployed-pipeline.spec.ts
#
# Rationale (see docs/plans/v2.2-audits/v22-E-process-gates-audit.md
# § 2.3): the spec auto-attaches stderr / stdout on failure and runs
# the same fixture other deployed e2e suites use; reimplementing the
# topology in bash would drift.
#
# Phase close hard rule (per testing.md § 2.3): a phase whose test
# report shows deployed-smoke count = 0 MUST NOT close. This script is
# the canonical "smoke count = 1+" entry point.
#
# Run locally:
#   ./scripts/smoke/deploy-smoke.sh
# Or via make:
#   make smoke
#
# Output:
#   On success the last line is `smoke pass: NN seconds`.
#   On failure the last line is `smoke FAIL at step <name> (NN seconds)`.
#
# Exit codes:
#   0  — smoke passed
#   1  — smoke failed (binary build, missing deps, or spec failure)
#   64 — usage / environment setup error
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

SPEC="tests/e2e/v2/tests/v22-deployed-pipeline.spec.ts"
START_TS=$(date +%s)
CURRENT_STEP="init"

fail() {
  local end_ts now_ts
  now_ts=$(date +%s)
  local elapsed=$(( now_ts - START_TS ))
  echo "smoke FAIL at step ${CURRENT_STEP} (${elapsed} seconds)" >&2
  exit 1
}
trap fail ERR

step() {
  CURRENT_STEP="$1"
  echo "--- ${CURRENT_STEP} ---"
}

# --- pre-flight: confirm Playwright fixtures are installed ---------
step "preflight"
if [[ ! -f "$SPEC" ]]; then
  echo "missing spec: $SPEC" >&2
  CURRENT_STEP="preflight:spec-missing"
  exit 64
fi
if ! command -v pnpm >/dev/null 2>&1; then
  echo "pnpm not on PATH — install pnpm to run the e2e suite" >&2
  CURRENT_STEP="preflight:pnpm-missing"
  exit 64
fi
if [[ ! -d "tests/e2e/v2/node_modules" ]]; then
  echo "tests/e2e/v2/node_modules missing — run \`make e2e-install\` first" >&2
  CURRENT_STEP="preflight:node_modules-missing"
  exit 64
fi

# --- build fresh binaries -------------------------------------------
# `make build` already chains build-frontend + build-backend +
# build-worker-daemon + build-fakeagent (Makefile § build target).
step "build"
make build

for b in bin/agent-center bin/agent-center-worker-daemon bin/fakeagent; do
  if [[ ! -x "$b" ]]; then
    echo "expected binary not built: $b" >&2
    CURRENT_STEP="build:missing-${b##*/}"
    exit 1
  fi
done

# --- run the deployed-pipeline spec ---------------------------------
# The spec spawns server + worker-daemon as real processes, hits the
# admin unix socket, and asserts the task reaches `done`. Diagnostics
# (server / worker stderr) are auto-attached on failure.
step "spec:v22-deployed-pipeline"
( cd tests/e2e/v2 && pnpm exec playwright test "$(basename "$SPEC")" )

# --- success --------------------------------------------------------
trap - ERR
END_TS=$(date +%s)
ELAPSED=$(( END_TS - START_TS ))
echo "smoke pass: ${ELAPSED} seconds"
