#!/bin/bash
# Audit-module acceptance driver — drives the REAL web API on the isolated
# test instance (audit1) to generate every high-value change_type, then reads
# each object's audit ledger. Reproducible: re-run against a fresh instance.
set -uo pipefail
WEB="${WEB:-http://127.0.0.1:57211}"
ORG="${ORG:-org-e302f2cd}"
PROJ="${PROJ:-project-5ec5315a}"
JAR="${JAR:-/tmp/audit-jar}"
OUT="${OUT:-/tmp/audit-results}"
mkdir -p "$OUT"
BASE="$WEB/api/orgs/$ORG/projects/$PROJ"
CT='content-type: application/json'

j() { curl -s -b "$JAR" -H "$CT" "$@"; }
jid() { python3 -c 'import sys,json;print(json.load(sys.stdin).get("id",""))'; }
show() { python3 -m json.tool 2>/dev/null || cat; }

echo "############ ITEM B/C/D — TASK ############"
echo "== B1 create task =="
TID=$(j -X POST "$BASE/tasks" -d '{"title":"Audit demo task","description":"lifecycle"}' | jid)
echo "TID=$TID"
echo "== B2 SetTaskStatus override open->running =="
j -X POST "$BASE/tasks/$TID/status" -d '{"status":"running"}' >/dev/null
echo "== B3 block =="
j -X POST "$BASE/tasks/$TID/block" -d '{"reason":"waiting upstream"}' >/dev/null
echo "== B4 unblock =="
j -X POST "$BASE/tasks/$TID/unblock" -d '{}' >/dev/null
echo "== B5 complete =="
j -X POST "$BASE/tasks/$TID/complete" -d '{}' >/dev/null
echo "== B6 reopen =="
j -X POST "$BASE/tasks/$TID/reopen" -d '{}' >/dev/null
echo "== B7 batch update (PATCH) status=running =="
j -X PATCH "$BASE/tasks/$TID" -d '{"status":"running"}' >/dev/null
echo "== C1 assign user:38eefc04 =="
j -X POST "$BASE/tasks/$TID/assign" -d '{"assignee":"user:38eefc04"}' >/dev/null
echo "== C2 reassign user:helper-bob =="
j -X POST "$BASE/tasks/$TID/assign" -d '{"assignee":"user:helper-bob"}' >/dev/null
echo "== C3 unassign =="
j -X POST "$BASE/tasks/$TID/unassign" -d '{}' >/dev/null
echo "== TASK AUDIT LEDGER =="
j "$BASE/tasks/$TID/audit" | tee "$OUT/task-audit.json" | show
echo "TID=$TID" > "$OUT/ids.env"

echo "############ ITEM E — ISSUE ############"
echo "== E1 create issue =="
IID=$(j -X POST "$BASE/issues" -d '{"title":"Audit demo issue","description":"issue body"}' | jid)
echo "IID=$IID"
echo "== E2 transition issue =="
j -X POST "$BASE/issues/$IID/transition" -d '{"status":"in_progress"}' >/dev/null
echo "== E3 set issue status =="
j -X POST "$BASE/issues/$IID/status" -d '{"status":"resolved"}' >/dev/null
echo "== E4 metadata edit (PATCH) =="
j -X PATCH "$BASE/issues/$IID" -d '{"title":"Audit demo issue (edited)"}' >/dev/null
echo "== ISSUE AUDIT LEDGER =="
j "$BASE/issues/$IID/audit" | tee "$OUT/issue-audit.json" | show
echo "IID=$IID" >> "$OUT/ids.env"

echo "############ ITEM F — PLAN ############"
echo "== F1 create plan =="
PID=$(j -X POST "$BASE/plans" -d '{"title":"Audit demo plan","description":"plan body"}' | jid)
echo "PID=$PID"
echo "PID=$PID" >> "$OUT/ids.env"
echo "== plan create raw (inspect shape) =="
j "$BASE/plans/$PID" | show | head -40
