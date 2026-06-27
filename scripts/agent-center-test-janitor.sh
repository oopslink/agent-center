#!/bin/bash
#
# agent-center test-instance janitor
# ==================================
# Acceptance testing (see docs/rules/acceptance-methodology.md) spins up throwaway
# center instances under ~/.agent-center-test/<name>/, each launchd-managed
# (KeepAlive) and running a worker daemon + several `claude` CLI sessions. They do
# NOT self-terminate, so they accumulate and oversubscribe the CPU, starving the
# real instance (the 2026-06-27 load=80 / "agent 没收到消息" incident).
#
# This janitor stops + removes test instances older than MAX_AGE_SECONDS. Install
# it once; a launchd timer then runs it hourly.
#
# SAFETY: it only ever touches children of ~/.agent-center-test/ and launchd jobs
# whose plist references that tree. The live instance ~/.agent-center/ (no "-test")
# and its (non-launchd) processes are NEVER matched.
#
# Usage:
#   agent-center-test-janitor.sh              run the cleanup once
#   agent-center-test-janitor.sh --dry-run    print what would happen, change nothing
#   agent-center-test-janitor.sh install      deploy to ~/.local/bin + register the hourly launchd timer
#   agent-center-test-janitor.sh uninstall    stop + remove the launchd timer (leaves the deployed copy)
#
# Portable across machines/users: every path derives from $HOME — nothing is
# hardcoded. macOS-only by nature (it manages launchd jobs).
#
# This is a bash script (needs arrays + BASH_SOURCE). If launched under another
# shell — e.g. a `bash`→zsh alias, where $0 inside a function is the function name
# and BASH_SOURCE is unset — re-exec under the real bash first.
if [ -z "${BASH_VERSION:-}" ]; then exec /bin/bash "$0" "$@"; fi

set -uo pipefail

# ---- tunables -------------------------------------------------------------
TESTROOT="$HOME/.agent-center-test"          # only this tree is ever touched
MAX_AGE_SECONDS=18000                         # 5h — instances older than this are cleaned
PURGE_DIRS=true                               # also `rm -rf` the instance dir (false = stop only, keep data)
# ---------------------------------------------------------------------------

LABEL="com.agent-center.test-janitor"
INSTALL_PATH="$HOME/.local/bin/agent-center-test-janitor.sh"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
LOG="$HOME/Library/Logs/agent-center-test-janitor.log"
UID_="$(id -u)"

log() { printf '%s %s\n' "$(date '+%Y-%m-%dT%H:%M:%S')" "$*" | tee -a "$LOG"; }

# ---- install / uninstall --------------------------------------------------
do_install() {
  mkdir -p "$(dirname "$INSTALL_PATH")" "$(dirname "$LOG")" "$(dirname "$PLIST")"
  local src
  src="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
  if [ "$src" != "$INSTALL_PATH" ]; then
    cp "$src" "$INSTALL_PATH"
  fi
  chmod +x "$INSTALL_PATH"

  cat > "$PLIST" <<PLISTEOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>$LABEL</string>
    <key>ProgramArguments</key>
    <array>
        <string>/bin/bash</string>
        <string>$INSTALL_PATH</string>
    </array>
    <key>StartInterval</key>
    <integer>3600</integer>
    <key>RunAtLoad</key>
    <true/>
    <key>ProcessType</key>
    <string>Background</string>
    <key>LowPriorityIO</key>
    <true/>
    <key>Nice</key>
    <integer>10</integer>
    <key>StandardOutPath</key>
    <string>$LOG</string>
    <key>StandardErrorPath</key>
    <string>$LOG</string>
</dict>
</plist>
PLISTEOF

  launchctl bootout "gui/$UID_/$LABEL" 2>/dev/null || true   # idempotent re-install
  launchctl bootstrap "gui/$UID_" "$PLIST"
  echo "installed: $INSTALL_PATH"
  echo "timer:     $LABEL (hourly) → $PLIST"
  echo "log:       $LOG"
}

do_uninstall() {
  launchctl bootout "gui/$UID_/$LABEL" 2>/dev/null || true
  rm -f "$PLIST"
  echo "uninstalled launchd timer $LABEL (deployed copy $INSTALL_PATH left in place)"
}

# ---- cleanup --------------------------------------------------------------
do_run() {
  local dry_run="$1"
  mkdir -p "$(dirname "$LOG")"
  log "=== janitor run (dry_run=$dry_run max_age=${MAX_AGE_SECONDS}s purge=$PURGE_DIRS) ==="

  if [ ! -d "$TESTROOT" ]; then
    log "no $TESTROOT — nothing to do"; return 0
  fi

  local now cleaned=0 orphans=0
  now="$(date +%s)"

  for dir in "$TESTROOT"/*/; do
    [ -d "$dir" ] || continue
    local name birth age
    name="$(basename "$dir")"
    [ -n "$name" ] || continue
    birth="$(stat -f %B "$dir" 2>/dev/null || echo "$now")"
    age=$(( now - birth ))
    if [ "$age" -lt "$MAX_AGE_SECONDS" ]; then
      log "keep   $name (age $((age/3600))h < $((MAX_AGE_SECONDS/3600))h)"
      continue
    fi
    log "CLEAN  $name (age $((age/3600))h)"
    cleaned=$((cleaned+1))

    # 1) bootout + delete launchd jobs whose plist references THIS instance dir.
    for plist in "$HOME"/Library/LaunchAgents/com.agent-center.*.plist; do
      [ -e "$plist" ] || continue
      if grep -qF "$TESTROOT/$name/" "$plist"; then
        local label; label="$(basename "$plist" .plist)"
        log "  bootout $label"
        if [ "$dry_run" = false ]; then
          launchctl bootout "gui/$UID_/$label" 2>/dev/null || true
          rm -f "$plist"
        fi
      fi
    done

    # 2) kill any stray processes still referencing this instance dir.
    local pids; pids="$(pgrep -f "agent-center-test/$name/" || true)"
    if [ -n "$pids" ]; then
      log "  kill: $(echo "$pids" | tr '\n' ' ')"
      [ "$dry_run" = false ] && echo "$pids" | xargs kill -9 2>/dev/null || true
    fi

    # 3) purge the instance data dir (guarded: must be a direct child of TESTROOT).
    if [ "$PURGE_DIRS" = true ]; then
      case "$dir" in
        "$TESTROOT"/?*/)
          log "  rm -rf $dir"
          [ "$dry_run" = false ] && rm -rf "$dir" ;;
        *)
          log "  REFUSE unsafe path: $dir" ;;
      esac
    fi
  done

  # 4) orphan launchd jobs: a plist pointing at a TESTROOT instance dir that no
  #    longer exists would silently re-spawn a dead instance on next login/reboot.
  for plist in "$HOME"/Library/LaunchAgents/com.agent-center.*.plist; do
    [ -e "$plist" ] || continue
    local child; child="$(grep -oE "\.agent-center-test/[^/<\"' ]+" "$plist" 2>/dev/null | head -1 | sed 's#.*/##')"
    [ -n "$child" ] || continue                  # not a test plist → leave it
    [ -d "$TESTROOT/$child" ] && continue         # dir still exists → handled above / live
    local label; label="$(basename "$plist" .plist)"
    log "ORPHAN $label (dir $child gone) — bootout + rm plist"
    orphans=$((orphans+1))
    if [ "$dry_run" = false ]; then
      launchctl bootout "gui/$UID_/$label" 2>/dev/null || true
      rm -f "$plist"
    fi
  done

  log "=== done (cleaned=$cleaned orphans=$orphans) ==="
}

# ---- dispatch -------------------------------------------------------------
case "${1:-}" in
  install)   do_install ;;
  uninstall) do_uninstall ;;
  --dry-run) do_run true ;;
  "")        do_run false ;;
  *) echo "usage: $(basename "$0") [install|uninstall|--dry-run]" >&2; exit 2 ;;
esac
