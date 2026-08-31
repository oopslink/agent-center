# Task Input Package

This package was materialized locally for the T1850 replay candidate because the
workspace did not contain the supervisor-declared `task-input/v1` files at
executor start.

Scope:

- Task: replay T1850 worker authoritative readback onto fresh current-main.
- Source implementation: `65eab385a93199d98fe3b367d4d07bc627cda7f2`.
- Required predecessor/production-chain anchor:
  `7f4cfcc43e0360f31e756bb453e675db50cc26c6`.
- Fresh base observed before edits:
  `3b2b45f480c297f44b0e2deb877ebc6cdad7f5f5` (`origin/main`).

Attachments:

- No task attachments were present in this workspace.
- `manifest.json` records an empty attachment list with size, MIME, and SHA256
  fields available for any future attachment entries.

Isolation note:

- No agent-center control-plane tools, databases, sockets, worker tokens,
  runtime config fallbacks, or raw HTTP endpoints were used to recover this
  package.
