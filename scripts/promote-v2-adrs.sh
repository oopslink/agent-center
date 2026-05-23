#!/usr/bin/env bash
# promote-v2-adrs.sh — one-shot promote of v2 ADR drafts to Accepted.
#
# Per P12 S4 audit (docs/plans/phase-12-audits/s4-adr-promote-audit.md)
# all 17 v2 ADRs in docs/design/decisions/drafts/ have evidence of
# implementation. This script:
#
#   1. Prepends a Status table to each ADR (idempotent — skips if the
#      table is already present).
#   2. `git mv`s the file from decisions/drafts/ to decisions/.
#   3. sed-replaces every reference to `decisions/drafts/00NN-` →
#      `decisions/00NN-` throughout the repo.
#
# Usage:
#   ./scripts/promote-v2-adrs.sh [--dry-run]
#
# After this script lands its single commit, the drafts/ directory is
# empty (modulo .gitkeep if added). Future ADRs author with Status:
# Draft directly under decisions/ and flip in place when mature; the
# drafts/ pattern was a one-time v2 staging area.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

DRY_RUN=false
if [[ "${1:-}" == "--dry-run" ]]; then
  DRY_RUN=true
fi

# Each entry: <ADR-id>|<delivered phrase>
#
# The "delivered phrase" goes verbatim into the Status table as the
# `| Delivered | ... |` row value. Wrap in backticks for SHA refs so
# they don't word-break in narrow renders.
ADR_EVIDENCE=$(cat <<'EOF'
0023|P8 § 3.3 — `462f3a3` (WorkerEnrollService.Exchange), `c0f6373` (BootstrapToken AR)
0024|P8 § 3.5 — `80ee406` (AgentInstance AR + Repo + Management + Lifecycle services). Amended by ADR-0029.
0025|P10 F5 — `bbfa27a` (agent CLI + App wiring + Identity auto-register)
0026|P8 § 3.7-3.8 — `6d39ee8` (UserSecret AR + SecretRef VO). P11 F11 — `fc433f9` (UI no-plaintext-echo).
0027|P9 § 3.8 — `9841f74` (PromptAssembly v2 + MCPInjectionService)
0028|P9 § 3.9 — `80e5c89` (supervisor.md skill file mount; assets/skills/supervisor.md)
0029|P8 § 3.6 — `f5c6a86` (SupervisorInvocation.AgentInstanceID field). Amends ADR-0024.
0030|P9 § 3.3-3.6 — `009a6e6` (AgentAdapter v2 interface + claudecode / codex / opencode impls)
0031|P10 § 3.9 — `91a2d40` (delete Bridge BC code). P10 F6 — `7cf72fb` (drop v1 bridge tables migration 0025). P12 S1 — `44b298a` (v1 vendor cleanup) + `d8a2d26` (lint guard).
0032|P10 § 3.0-3.3 — `cefc135` (Conversation v2 schema + AR/Repo + Identity refactor), `1e3918a` (ChannelManagementService CV1)
0033|P10 § 3.2 — `cefc135` (Identity 4→3 kinds + kind:id prefix as part of Conversation v2 schema reset)
0034|P10 § 3.4 — `052c708` (ParticipantManagementService CV2b + CLI)
0035|P10 § 3.5 — `a6e175d` (CarryOverService + ConversationMessageReferenceRepository CV3)
0036|P10 § 3.6 + F2 + P11 SPA F9 — `c97a1ca` (MessageDerivationService CV4), `da9fa2d` (derive CLI), `8c57dde` (derive UI)
0037|P11 § 3.2-3.4 + SPA F1-F16 — `ebb8c22` (SSE fan-out), `b85d4c6` (SPA scaffold), `07cca9b` (go:embed binary), `ab98065` (P11 closeout)
0038|P11 § 3.8-3.9 — `ff2b137` (--format universal flag), `3baa1fc` (help discoverability)
0039|P10 unified docs effort — supersedes ADR-0017/0021/0022; backed by the CV1-CV4 sequence in 0032/0033/0034/0035/0036.
EOF
)

DATE="2026-05-24"
DRAFTS_DIR="docs/design/decisions/drafts"
TARGET_DIR="docs/design/decisions"

# -- Step 1+2: per-ADR transformation -------------------------------------

while IFS='|' read -r adr_id evidence; do
  [[ -z "$adr_id" ]] && continue

  src=$(ls "$DRAFTS_DIR"/${adr_id}-*.md 2>/dev/null | head -1)
  if [[ -z "$src" ]]; then
    # already promoted? look in target
    if ls "$TARGET_DIR"/${adr_id}-*.md >/dev/null 2>&1; then
      echo "[skip] ADR ${adr_id} already in $TARGET_DIR"
      continue
    fi
    echo "[error] ADR ${adr_id} not found in $DRAFTS_DIR" >&2
    exit 1
  fi

  base=$(basename "$src")
  dest="$TARGET_DIR/$base"

  # Transform the Status table: flip Status → Accepted, update Date,
  # and add a Delivered row if missing. All drafts already carry the
  # table per ADR convention; if a future caller hands us a fileless
  # of one, the python below errors clearly.
  if "$DRY_RUN"; then
    echo "[dry-run] would promote $src (Status → Accepted; add Delivered row)"
  else
    DATE="$DATE" EVIDENCE="$evidence" SRC="$src" python3 - <<'PY'
import os, re, sys
path = os.environ["SRC"]
date = os.environ["DATE"]
evidence = os.environ["EVIDENCE"]
with open(path) as fh:
    text = fh.read()

# Replace any Status: Draft / Accepted / etc row with Status | Accepted.
new = re.sub(r"^\|\s*Status\s*\|.*?\|\s*$", "| Status | Accepted |", text, count=1, flags=re.MULTILINE)
# Update the Date row (first occurrence).
new = re.sub(r"^\|\s*Date\s*\|.*?\|\s*$", f"| Date | {date} |", new, count=1, flags=re.MULTILINE)
# Insert / update Delivered row directly after the Date row.
if re.search(r"^\|\s*Delivered\s*\|", new, flags=re.MULTILINE):
    new = re.sub(
        r"^\|\s*Delivered\s*\|.*?\|\s*$",
        f"| Delivered | {evidence} |",
        new, count=1, flags=re.MULTILINE,
    )
else:
    # Insert a Delivered row right after the Date row.
    new = re.sub(
        r"^(\|\s*Date\s*\|.*?\|\s*\n)",
        rf"\1| Delivered | {evidence} |\n",
        new, count=1, flags=re.MULTILINE,
    )
if new == text:
    print(f"[warn] {path}: no Status/Date rows matched — file untouched", file=sys.stderr)
    sys.exit(1)
with open(path, "w") as fh:
    fh.write(new)
PY
  fi

  if "$DRY_RUN"; then
    echo "[dry-run] would git mv $src $dest"
  else
    git mv "$src" "$dest"
    echo "[ok] $base → $TARGET_DIR/"
  fi
done <<< "$ADR_EVIDENCE"

# -- Step 3: sed batch over cross-references ------------------------------

echo ""
echo "[xref] updating cross-references repo-wide"

# Files that reference decisions/drafts/ — we replace the path prefix
# wholesale. Two patterns to keep replacements safe (avoid grabbing
# `something-drafts/`):
#   `decisions/drafts/00NN-` → `decisions/00NN-`
#   `decisions/drafts/0NNN-` → `decisions/0NNN-` (future-proof; same)

# Use git ls-files to operate only on tracked files (avoids /node_modules
# / build artifacts). Exclude scripts/promote-v2-adrs.sh itself + the
# v2-kickoff draft archive which intentionally references the draft state.
TARGETS=$(git ls-files \
  -- '*.md' '*.go' '*.ts' '*.tsx' '*.yaml' '*.yml' '*.toml' '*.json' \
  | grep -v '^scripts/promote-v2-adrs\.sh$' \
  | grep -v '^docs/design/drafts/v2-kickoff-' \
  || true)

count=0
for f in $TARGETS; do
  if grep -q 'decisions/drafts/' "$f" 2>/dev/null; then
    if "$DRY_RUN"; then
      echo "[dry-run] would sed $f"
    else
      # macOS / Linux sed compatibility — use a -i'' marker variant.
      python3 -c '
import sys, re
p = sys.argv[1]
with open(p) as fh: text = fh.read()
new = re.sub(r"decisions/drafts/(0\d{3}-)", r"decisions/\1", text)
if new != text:
    with open(p, "w") as fh: fh.write(new)
    print(f"[xref] {p}")
' "$f"
      count=$((count + 1))
    fi
  fi
done

if ! "$DRY_RUN"; then
  echo "[xref] updated $count files"
fi

echo ""
echo "Done."
