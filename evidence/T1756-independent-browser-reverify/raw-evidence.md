# Raw Evidence

## Workspace

```text
/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/runtime/worktrees/exec-cffddf3a
```

## Task Input Reads

```text
$ sed -n '1,240p' task-input/v1/README.md
sed: task-input/v1/README.md: No such file or directory

$ sed -n '1,240p' task-input/v1/manifest.json
sed: task-input/v1/manifest.json: No such file or directory
```

## Task Input Discovery

```text
$ rg --files task-input/v1
rg: task-input/v1: No such file or directory (os error 2)
```

## Git Remote

```text
$ git remote -v
origin	git@github.com:oopslink/agent-center.git (fetch) [blob:none]
origin	git@github.com:oopslink/agent-center.git (push)
origin-push	git@github.com:oopslink/agent-center.git (fetch)
origin-push	git@github.com:oopslink/agent-center.git (push)
```

## Fresh Fetch

```text
$ git fetch --prune origin
fatal: refusing to fetch into branch 'refs/heads/ac-exec/task-0acd92ce/exec-949c3245' checked out at '/Users/oopslink/.agent-center/workers/worker-edb09a0c/var/runtime/worktrees/exec-949c3245'

$ git fetch --prune origin refs/heads/main:refs/remotes/origin/main
```

## Git Gates

```text
$ git status --porcelain=v1

$ git rev-parse HEAD
5a18901eaea33c48247e2e8847a29f1d66038d40

$ git rev-parse origin/main
5a18901eaea33c48247e2e8847a29f1d66038d40

$ git merge-base HEAD origin/main
5a18901eaea33c48247e2e8847a29f1d66038d40

$ git rev-list --left-right --count HEAD...origin/main
0	0

$ git for-each-ref --format='%(refname:short) %(objectname)' refs/remotes/origin/HEAD refs/remotes/origin/main
origin 5a18901eaea33c48247e2e8847a29f1d66038d40
origin/main 5a18901eaea33c48247e2e8847a29f1d66038d40

$ git branch --show-current
ac-exec/task-e58bf23b/exec-cffddf3a
```

## Old Lineage Checks

```text
$ git merge-base --is-ancestor 75427e3d HEAD; printf '75427e3d_ancestor=%s\n' $?
75427e3d_ancestor=1

$ git merge-base --is-ancestor bda5d14a HEAD; printf 'bda5d14a_ancestor=%s\n' $?
bda5d14a_ancestor=1
```

Exit code `1` from `git merge-base --is-ancestor` means the named commit is not an ancestor of `HEAD`.
