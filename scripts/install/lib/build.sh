#!/usr/bin/env bash
# build.sh — build the checked-out source into a release-like staging dir
# (§4.3). Delegates to the `make release-dir` target so the layout matches
# tarball releases exactly; the installer never duplicates build logic.

# ac_build_stage <source-dir> <version> <out-dir>
# Runs `make release-dir VERSION=<version> OUT=<out-dir>` inside the source
# checkout. On build failure the existing install is left untouched (§5.3):
# nothing in this function touches the install prefix.
ac_build_stage() {
  local src="$1" version="$2" out="$3"

  [ -f "${src}/Makefile" ] || ac_die "no Makefile at ${src}; cannot build this ref."

  ac_log ""
  ac_log "Building ${version} into staging dir ${out} (make release-dir)…"
  ac_log "This compiles the web console + Go binaries and may take a few minutes."

  # Build runs through ac_run so --dry-run prints the command without
  # executing it. We cd via a subshell so the caller's cwd is unaffected.
  if [ "${AC_DRY_RUN:-0}" -eq 1 ]; then
    ac_run make -C "$src" release-dir "VERSION=$version" "OUT=$out"
    return 0
  fi

  if ! ( cd "$src" && make release-dir "VERSION=$version" "OUT=$out" >&2 ); then
    ac_die "build failed. The current install (if any) was not modified. Inspect the output above and re-run."
  fi

  [ -x "${out}/install" ] || ac_die "staging dir ${out} is missing the ./install entrypoint after build."
  ac_log "Staged release layout at ${out}."
}
