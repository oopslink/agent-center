# Runtime RepoCacheManager

## Runtime layout

Each worker runtime owns a reconstructible local repository cache:

```text
<runtime>/
  repos/<repo_key>/
    mirror.git/
    health.json
    repo.lock
  worktrees/<executor_id>/
  registry/worktrees/<executor_id>.json
  gc.lock
```

`repo_key` is the SHA-256 key of the normalized persistent repository URL.
Repository URL and default branch remain in the center's coderepo persistence;
the runtime cache is never their source of truth.

## Lifecycle

1. `EnsureSource` takes a cross-process flock and creates one atomic bare mirror,
   or validates and fetches an existing mirror.
2. Fetches within the 60-second TTL are coalesced. If refresh fails but the
   requested ref resolves locally, the source is returned as stale and
   `health.json` records the degraded state. If the ref is absent,
   `ErrCacheRefUnavailable` becomes `repo_ref_unavailable`, not
   `repo_source_unavailable`.
3. `CreateWorktree`/`PrepareWorktree` cuts a unique executor branch at a pinned
   commit and creates `<runtime>/worktrees/<executor_id>`.
4. The executor recovery record stores the actual workspace path. Normal
   completion, failed launch, cancellation, and restart reconcile all call the
   same `RemoveWorktree` path and retain the audit record with its cleanup state.
5. Boot/spawn reconcile uses a single GC leader flock. It removes orphan
   worktrees and stale branches. Registry owner IDs ensure an agent-runtime only
   applies its liveness view to its own executors. Inactive mirrors are evicted by last access
   after seven days or under the 20 GiB cache waterline policy; the remote can
   rebuild them.

Executors receive a worktree only. They never receive or modify the mirror.
Production wiring always uses RepoCacheManager; the legacy per-task clone switch
is no longer on the normal path.

## Verification

The real-Git integration suite covers:

- repeated fork preparation reusing one bare mirror;
- eight concurrent manager instances sharing one runtime without duplicate or
  corrupt initialization;
- offline fetch with a valid cached ref;
- offline fetch with a missing ref and a diagnostic error;
- registry transitions and terminal worktree cleanup;
- leader reconcile preserving live worktrees and removing orphans.

Local fixture timing on 2026-07-24:

| Path | Duration |
| --- | ---: |
| Cold bare mirror initialization | 134 ms |
| Warm mirror validation/reuse | 52 ms |

The fixture is intentionally tiny, so network repositories will show a larger
absolute improvement. The important behavioral change is that consecutive
forks reuse mirror objects and do not execute another clone.
