#!/usr/bin/env bash
# silent.sh — fake agent that prints nothing and exits 0 immediately.
# Used by no-hello / fast-exit tests where we care about the spawn lifecycle
# rather than event content.
set -u
exit 0
