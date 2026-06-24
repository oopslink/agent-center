#!/usr/bin/env bash
#
# gofmt-check.sh — fail if any .go file is not gofmt-clean under the toolchain
# pinned in go.mod (canonical = go1.25.x; see issue-71690608 / T458).
#
# Why force GOTOOLCHAIN instead of running a bare `gofmt`:
#   gofmt's column-alignment output has drifted across Go releases. A developer
#   on a newer local Go (e.g. 1.26) running bare `gofmt -w` could reformat the
#   tree with a *different* gofmt than the team's canonical one and re-introduce
#   churn. A `toolchain` directive in go.mod only forces an *upgrade*, never a
#   downgrade — so on a 1.26 local it would NOT pick 1.25. We therefore read the
#   pin from go.mod and set GOTOOLCHAIN explicitly so every machine formats with
#   the identical gofmt.
#
# Run `make fmt` to auto-fix.
set -euo pipefail

cd "$(dirname "$0")/../.."

PIN="$(awk '/^toolchain /{print $2; exit}' go.mod)"
if [[ -z "${PIN:-}" ]]; then
  echo "gofmt-check: no 'toolchain' directive found in go.mod" >&2
  exit 1
fi

dirty="$(GOTOOLCHAIN="$PIN" go run cmd/gofmt -l .)"
if [[ -n "$dirty" ]]; then
  echo "gofmt-check: the following files are not gofmt-clean under $PIN:" >&2
  echo "$dirty" | sed 's/^/  /' >&2
  echo >&2
  echo "Fix with:  make fmt   (or: GOTOOLCHAIN=$PIN go run cmd/gofmt -w .)" >&2
  exit 1
fi

echo "gofmt-check: all .go files are gofmt-clean under $PIN"
