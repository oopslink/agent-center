#!/usr/bin/env bash
set -euo pipefail

WEB="http://127.0.0.1:49515"
SLUG="org-75edacca"
PASSCODE="${AC_PASSCODE:?set AC_PASSCODE to the seeded passcode from the local install access pack}"
ROOT="docs/acceptance/insight-test-instance-20260831/raw"
COOKIE="$ROOT/session-smoke-asserted.cookie"
SMOKE="$ROOT/09-deployed-smoke-asserted.log"

exec >"$SMOKE" 2>&1

check_json() {
  local label="$1"
  local method="$2"
  local url="$3"
  local body="${4:-}"
  local tmp="$ROOT/tmp-response-${label}.txt"
  local code

  echo "## $label"
  if [ -n "$body" ]; then
    code=$(curl -sS -w '%{http_code}' -o "$tmp" -b "$COOKIE" -c "$COOKIE" \
      -H 'content-type: application/json' -X "$method" "$url" -d "$body")
  else
    code=$(curl -sS -w '%{http_code}' -o "$tmp" -b "$COOKIE" -c "$COOKIE" \
      -X "$method" "$url")
  fi
  echo "HTTP $code"
  sed -n '1,40p' "$tmp"
  case "$code" in
    2*) ;;
    *) echo "FAIL $label status=$code"; exit 1 ;;
  esac
}

echo "# deployed-smoke asserted on installed launchd test-instance"
echo "binary=./bin/agent-center"
echo "instance=insight-smoke-20260831"
echo "web=$WEB"

check_json health GET "$WEB/api/health"
check_json signin POST "$WEB/api/auth/signin" "{\"display_name\":\"Owner insight-smoke-20260831\",\"passcode\":\"$PASSCODE\",\"org_slug\":\"$SLUG\"}"
check_json me GET "$WEB/api/auth/me"
check_json orgs-list GET "$WEB/api/orgs"
check_json projects-list GET "$WEB/api/orgs/$SLUG/projects"
check_json seeded-project GET "$WEB/api/orgs/$SLUG/projects/project-0f187407"
check_json conversations-list GET "$WEB/api/orgs/$SLUG/conversations"
check_json seeded-channel GET "$WEB/api/orgs/$SLUG/conversations/channel-9af573ea"
check_json fleet GET "$WEB/api/orgs/$SLUG/fleet"

echo "## files create upload"
create_tmp="$ROOT/tmp-create-upload.json"
code=$(curl -sS -w '%{http_code}' -o "$create_tmp" -b "$COOKIE" -c "$COOKIE" \
  -H 'content-type: application/json' -X POST "$WEB/api/orgs/$SLUG/files" \
  -d '{"content_type":"text/plain","size":19}')
echo "HTTP $code"
sed -n '1,40p' "$create_tmp"
case "$code" in
  2*) ;;
  *) echo "FAIL files create status=$code"; exit 1 ;;
esac

transfer=$(sed -n 's/.*"transfer_id":"\([^"]*\)".*/\1/p' "$create_tmp")
if [ -z "$transfer" ]; then
  echo "FAIL missing transfer_id"
  exit 1
fi
echo "transfer_id=$transfer"

printf 'insight smoke bytes' >"$ROOT/upload-payload.txt"

echo "## files put bytes"
put_tmp="$ROOT/tmp-put.txt"
code=$(curl -sS -w '%{http_code}' -o "$put_tmp" -b "$COOKIE" -c "$COOKIE" \
  -X PUT "$WEB/api/orgs/$SLUG/files/transfer/$transfer" \
  --data-binary @"$ROOT/upload-payload.txt")
echo "HTTP $code"
sed -n '1,40p' "$put_tmp"
case "$code" in
  2*) ;;
  *) echo "FAIL files put status=$code"; exit 1 ;;
esac

echo "## files complete"
complete_tmp="$ROOT/tmp-complete.json"
code=$(curl -sS -w '%{http_code}' -o "$complete_tmp" -b "$COOKIE" -c "$COOKIE" \
  -H 'content-type: application/json' -X POST "$WEB/api/orgs/$SLUG/files/transfer/$transfer/complete" \
  -d '{"size":19}')
echo "HTTP $code"
sed -n '1,40p' "$complete_tmp"
case "$code" in
  2*) ;;
  *) echo "FAIL files complete status=$code"; exit 1 ;;
esac

echo "deployed_smoke_asserted=PASS"
