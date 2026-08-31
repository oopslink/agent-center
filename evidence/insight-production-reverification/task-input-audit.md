# Insight Production Reverification Input Audit

Audit timestamp UTC: 2026-08-31T05:05:22Z

## Required Task

The assigned task requires an independent production reverification for Insight:

- lock the task-specified candidate exact SHA;
- verify remote ref, binary/running identity, and merge-base;
- run real production positive/negative checks for the four README-defined hard reds;
- verify provenance and regression coverage;
- archive raw request/response/page evidence;
- release only on fresh structured PASS.

## Blocking Result

`task-input/v1/README.md` and `task-input/v1/manifest.json` do not define the Insight production reverification contract. The local task-input package identifies a different task:

- Task: `T1850`
- Title: `Replay worker authoritative readback implementation onto fresh current-main`
- Source implementation: `65eab385a93199d98fe3b367d4d07bc627cda7f2`
- Required chain anchor: `7f4cfcc43e0360f31e756bb453e675db50cc26c6`
- Fresh base: `3b2b45f480c297f44b0e2deb877ebc6cdad7f5f5`
- Attachments: none

Because the required Insight README hard-red definitions, production target, credentials/procedure, and task-specified candidate exact SHA are absent, production reverification cannot proceed without guessing or reusing unrelated evidence. Per the assignment, this is a `BLOCK`.

## Raw Directory Output

Command:

```sh
pwd && ls -la && find task-input/v1 -maxdepth 3 -type f -print
```

Output:

```text
/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/runtime/worktrees/exec-d98c423f
total 496
drwxr-xr-x@ 27 oopslink  staff     864 Aug 31 13:04 .
drwxr-xr-x@ 24 oopslink  staff     768 Aug 31 13:04 ..
drwxr-xr-x@  3 oopslink  staff      96 Aug 31 13:04 .agent-center
drwxr-xr-x@  3 oopslink  staff      96 Aug 31 13:04 .claude
-rw-r--r--@  1 oopslink  staff     180 Aug 31 13:04 .git
drwxr-xr-x@  3 oopslink  staff      96 Aug 31 13:04 .github
-rw-r--r--@  1 oopslink  staff    1473 Aug 31 13:04 .gitignore
-rw-r--r--@  1 oopslink  staff  120421 Aug 31 13:04 CHANGELOG.md
-rw-r--r--@  1 oopslink  staff    2589 Aug 31 13:04 CLAUDE.md
-rw-r--r--@  1 oopslink  staff   11357 Aug 31 13:04 LICENSE
-rw-r--r--@  1 oopslink  staff   16404 Aug 31 13:04 Makefile
-rw-r--r--@  1 oopslink  staff   24692 Aug 31 13:04 README.md
-rw-r--r--@  1 oopslink  staff   17255 Aug 31 13:04 README.zh-CN.md
drwxr-xr-x@  5 oopslink  staff     160 Aug 31 13:04 assets
drwxr-xr-x@  5 oopslink  staff     160 Aug 31 13:04 cmd
drwxr-xr-x@  8 oopslink  staff     256 Aug 31 13:04 contrib
drwxr-xr-x@ 11 oopslink  staff     352 Aug 31 13:04 docs
-rw-r--r--@  1 oopslink  staff    2474 Aug 31 13:04 go.mod
-rw-r--r--@  1 oopslink  staff   12429 Aug 31 13:04 go.sum
-rwxr-xr-x@  1 oopslink  staff   13600 Aug 31 13:04 install.sh
drwxr-xr-x@ 42 oopslink  staff    1344 Aug 31 13:04 internal
drwxr-xr-x@  3 oopslink  staff      96 Aug 31 13:04 notes
drwxr-xr-x@  7 oopslink  staff     224 Aug 31 13:04 scripts
drwxr-xr-x@ 11 oopslink  staff     352 Aug 31 13:04 sites
drwxr-xr-x@  3 oopslink  staff      96 Aug 31 13:04 task-input
drwxr-xr-x@  4 oopslink  staff     128 Aug 31 13:04 tests
drwxr-xr-x@ 17 oopslink  staff     544 Aug 31 13:04 web
task-input/v1/README.md
task-input/v1/manifest.json
```

Command:

```sh
find task-input/v1 -maxdepth 4 -print -exec stat -f '%N %z bytes %Sm' {} \;
```

Output:

```text
task-input/v1
task-input/v1 128 bytes Aug 31 13:04:45 2026
task-input/v1/README.md
task-input/v1/README.md 921 bytes Aug 31 13:04:45 2026
task-input/v1/manifest.json
task-input/v1/manifest.json 755 bytes Aug 31 13:04:45 2026
```

## Input File Integrity

Command:

```sh
shasum -a 256 task-input/v1/README.md task-input/v1/manifest.json && file --mime-type task-input/v1/README.md task-input/v1/manifest.json
```

Output:

```text
64a8014e162df29f88f83962103555524f1ad563aa50f77deff03b00a0f8efe4  task-input/v1/README.md
db83e31afb3d8a3fdb7d7137a2b901acd5214a3f8e1f52219069969a89592092  task-input/v1/manifest.json
task-input/v1/README.md:     text/plain
task-input/v1/manifest.json: application/json
```

## Git Identity Observed

Command:

```sh
git rev-parse HEAD && git branch --show-current && git remote -v
```

Output:

```text
36de9846a82bca5e3f8de97e62688387d7ca8d47
ac-exec/task-a2ff95c2/exec-d98c423f
origin	git@github.com:oopslink/agent-center.git (fetch) [blob:none]
origin	git@github.com:oopslink/agent-center.git (push)
origin-push	git@github.com:oopslink/agent-center.git (fetch)
origin-push	git@github.com:oopslink/agent-center.git (push)
```

Command:

```sh
git merge-base HEAD origin/main
```

Output:

```text
36de9846a82bca5e3f8de97e62688387d7ca8d47
```

## Actions Not Performed

Production positive/negative hard-red validation, provenance checks, running identity verification, empty collections `null` to `[]` verification, and regression runs were not performed because the required Insight contract and candidate exact SHA were not present in the task-input package.
