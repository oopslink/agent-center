#!/usr/bin/env bash
# stage-release.sh — invoke the staged release's ./install entrypoint, reusing
# the EXISTING install/upgrade path (§1.1, §4.3). This is the single place the
# source installer crosses into install behavior; it adds no service, config,
# rollback, or enrollment logic of its own.

# ac_install_center <staging-dir> <prefix>
ac_install_center() {
  local out="$1" prefix="$2"
  local -a args=(center)
  [ -n "$prefix" ] && args+=("--prefix=$prefix")
  ac_log ""
  ac_log "Installing Center via staged ./install (reuses tarball install path)…"
  ac_run "${out}/install" "${args[@]}"
}

# ac_install_worker <staging-dir> <prefix> <center-url> <fingerprint> <token> <worker-name>
# The token is passed as an argument to the staged installer but is never
# echoed by this function (ac_run would print it, so we invoke directly and
# log a redacted summary instead).
ac_install_worker() {
  local out="$1" prefix="$2" center="$3" fp="$4" token="$5" name="$6"
  local -a args=(worker "--bootstrap=$center" "--token=$token")
  [ -n "$fp" ]     && args+=("--server-fingerprint=$fp")
  [ -n "$name" ]   && args+=("--worker-name=$name")
  [ -n "$prefix" ] && args+=("--prefix=$prefix")

  ac_log ""
  ac_log "Installing Worker via staged ./install (reuses enrollment path)…"
  ac_info "center:            ${center}"
  ac_info "server-fingerprint: $(ac_secret_present "$fp")"
  ac_info "enrollment token:   $(ac_secret_present "$token") (value not logged)"
  [ -n "$name" ] && ac_info "worker-name:        ${name}"

  if [ "${AC_DRY_RUN:-0}" -eq 1 ]; then
    # Print the plan with the token redacted so --dry-run never reveals it.
    printf '[dry-run] %s worker --bootstrap=%s --token=*** %s %s %s\n' \
      "${out}/install" "$center" \
      "${fp:+--server-fingerprint=***}" \
      "${name:+--worker-name=$name}" \
      "${prefix:+--prefix=$prefix}" >&2
    return 0
  fi

  printf '+ %s worker --bootstrap=%s --token=*** [secrets redacted]\n' "${out}/install" "$center" >&2
  if ! "${out}/install" "${args[@]}"; then
    ac_die "Worker install/enrollment failed. The Worker may be installed but NOT enrolled. Check the service status and logs printed above, then re-run with a fresh token."
  fi
}

# ac_dev_finish <staging-dir> <source-dir>
# Dev mode: no service install. Print local run commands pointing at the
# freshly built binaries (§2.4).
ac_dev_finish() {
  local out="$1" src="$2"
  ac_log ""
  ac_log "Dev build complete. No service was installed."
  ac_log "Run locally from the staged build:"
  ac_log "    ${out}/bin/agent-center server       # start the Center daemon"
  ac_log "    ${out}/bin/agent-center --help       # explore the CLI"
  ac_log "Source checkout: ${src}"
  ac_log "    cd ${src} && make build && ./bin/agent-center server"
}
