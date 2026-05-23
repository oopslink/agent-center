# P12 S3 — assets + configs vendor strip + lint extension audit

> Run 2026-05-24 · per x9527 oversight: scan all non-code / non-docs
> runtime config / asset / example files for v1 vendor residue; delete
> what's left; verify the S1 lint actually catches these — extend
> INCLUDE_GLOBS if there's a gap and add a self-test that exercises a
> deliberate injection. Audit log lands SEPARATELY.

## § 0. Scope

Runtime artifacts ONLY (not docs, not code). The S6 docs sweep
(deferred from S1) covers active markdown / design docs. Out of scope
for S3:

- `docs/**/*.md` — S6
- `internal/**/*.go` — S1
- migrations — S2
- generated SPA bundle (`internal/webconsole/spa/dist/*`) — built artifact, excluded from lint

In scope for S3:

| Surface | Files inventoried |
|---|---|
| `assets/` | `assets/skills/supervisor.md` (1 file) |
| `cmd/` | `cmd/fakeagent/{main.go,main_test.go}`, `cmd/agent-center/main.go` |
| `contrib/` | `agent-center.service`, `agent-center-backup.service`, `agent-center-backup.timer`, `agent-center-worker.service`, `install.sh`, `install-worker.sh` |
| Root configs | `.gitignore`, `Makefile` |
| Web tooling | `web/tsconfig*.json`, `web/package.json` (deps only — lockfile excluded) |
| Examples / env templates | (none — repo has none) |
| Dockerfiles | (none) |

## § 1. Patterns × dimensions

Seven patterns (consistent with S1): `feishu` / `lark` / `dingtalk` /
`wechat` / `vendor_*` / `bridge` / `webhook`. Case-insensitive grep.

Six file dimensions:

1. assets (`assets/**`)
2. cmd (`cmd/**`)
3. contrib (`contrib/**`)
4. yaml/yml/toml (`**/*.yaml`, `*.yml`, `*.toml`)
5. json / env (`**/*.json`, `**/*.env*`, `**/*.template`, `**/*.tmpl`)
6. shell / systemd / dockerfile (`*.sh`, `*.service`, `*.timer`, `Dockerfile*`)

## § 2. Findings

### 2.1 Grep results

```
$ rg -i -n 'feishu|lark|dingtalk|wechat|vendor_|webhook' \
    assets/ cmd/ contrib/
(zero matches)

$ rg -i -n 'feishu|lark|dingtalk|wechat|vendor_|webhook' \
    --glob '*.yaml' --glob '*.yml' --glob '*.toml' \
    --glob '*.json' --glob '*.env*' --glob '*.template' --glob '*.tmpl' \
    --glob '*.sh' --glob '*.service' --glob '*.timer' \
    --glob 'Dockerfile*' \
    -- . ':!web/node_modules' ':!.git' ':!sites/node_modules' \
       ':!internal/webconsole/spa/dist'
(zero matches)
```

**Real v1 residue in S3 scope: 0 files.**

### 2.2 Why is the cleanup commit (S1-pattern step 2) null?

`contrib/agent-center.service` (line 11 `EnvironmentFile=-/etc/agent-center/feishu.env`)
and `contrib/install.sh` (line 118 instruction to install `feishu.env`)
were the only real config-surface residue. **S1 cleanup commit
`44b298a`** already removed both, so by the time S3 starts there is
nothing left to delete.

This is **not a skip**: this is the S1 audit's § 7 row "Configs |
2 (contrib)" being honored — that 2-file delta was bundled into S1
because it touched the same lint surface. S3 audit's job is to verify
the residue stays cleaned + the lint surface is wide enough to catch
re-introduction.

### 2.3 Examples / env templates inventory

`find . -name "*.example*" -o -name "config.yaml" -o -name "config-sample*" -o -name "*-sample*"`
returned zero hits. There are **no committed example config files** in
the repo. Users get config guidance from `docs/design/implementation/
04-configuration.md § 8.1` (covered by S6 doc sweep), and
`contrib/install.sh` does not write a template — it only creates
directories + installs the binary + unit files.

## § 3. S1 lint coverage analysis

Current `scripts/lint/no-vendor-refs.sh` `INCLUDE_GLOBS`:

```
'*.go' '*.md' '*.yaml' '*.yml' '*.toml' '*.sql'
'*.sh' '*.service' '*.timer'
'Makefile' '*.mk'
'*.ts' '*.tsx'
```

Coverage map vs. S3 in-scope dimensions:

| S3 dimension | Pattern | Covered by current globs? |
|---|---|---|
| Go source | `*.go` | ✅ |
| Markdown (docs / readme) | `*.md` | ✅ |
| YAML / TOML configs | `*.yaml *.yml *.toml` | ✅ |
| SQL migrations | `*.sql` | ✅ |
| Shell scripts | `*.sh` | ✅ |
| systemd units | `*.service *.timer` | ✅ |
| Makefile | `Makefile *.mk` | ✅ |
| Frontend TS | `*.ts *.tsx` | ✅ |
| **JSON** | (gap) | ❌ |
| **Env templates** | (gap) | ❌ |
| **Dockerfile** | (gap) | ❌ |
| **`*.template` / `*.tmpl`** | (gap) | ❌ |

Even though no JSON / env / Dockerfile files in the repo currently
contain v1 vendor refs, the **lint contract** should cover them so a
future contributor who drops a `config.example.json` containing
`"feishu": {...}` is caught. The lint extension commit (commit 2)
plugs the gap.

## § 4. Lint extension plan

Add to `INCLUDE_GLOBS`:

- `*.json` (config samples, package.json, tsconfig)
- `*.env` and `*.env.*` (env templates)
- `*.template` and `*.tmpl` (any templating engine)
- `Dockerfile` and `Dockerfile.*` (container builds)

Also extend EXCLUDES so we don't churn on lockfiles / generated /
vendored JSON:

- `web/coverage/**` (vitest coverage artifacts)
- `**/package-lock.json` (npm lockfile noise)
- `**/pnpm-lock.yaml` (pnpm lockfile noise — already excluded by
  vendoring `web/node_modules`, but the lockfile itself sits outside
  node_modules and could trip false positives on transitive deps named
  e.g. `vendor_*`)
- `.vitepress/cache/**` (VitePress build cache)
- `sites/.vitepress/dist/**` (VitePress built site)

## § 5. Lint self-test plan

New script: `scripts/lint/test-no-vendor-refs.sh`. Behavior:

1. Create a temp file in a fresh dir under repo root with one of each:
   - a `.go` file containing `// feishu reference`
   - a `.yaml` file containing `feishu:\n  app_id: x`
   - a `.json` file containing `{"feishu_app_id":"x"}`
2. Run `./scripts/lint/no-vendor-refs.sh`; expect exit 1 + the
   temp file paths in the violation output.
3. Delete the temp files; re-run; expect exit 0.
4. Exit 0 if all assertions pass; exit 1 if the lint either missed a
   violation OR failed to clean up to green.

This is the *positive-fail* check x9527 oversight § ④ asked for —
the lint script's job is to fail when there IS residue, and asserting
just "currently clean" doesn't prove the lint works.

The self-test is wired into a new Makefile target `lint-vendor-selftest`
so CI can run it on demand. It does NOT run under `make lint` (to keep
the default lint cheap), but appears in `make help` if we add one
later.

## § 6. Acceptance criteria

- S3 audit log committed first (this file, § 0-7).
- Lint extension + self-test committed second; `make lint-vendor` still
  clean, `make lint-vendor-selftest` exit 0.
- `go test ./...` + `go vet ./...` unaffected (lint changes are pure
  scaffolding).
- M1 closure: with S3 done, milestone 1 (cleanup + lint) of P12 is
  complete; phase-12-plan-detail.md § M1 ticks.

## § 7. What S3 does NOT do

- **No edits to live config files** — none had residue.
- **No edits to docs** — S6.
- **No new sample configs** — `docs/design/implementation/04-configuration.md`
  is the canonical config spec; we don't fork it.
- **No CI gate change** — `make lint-vendor` already runs whenever
  `make lint` runs (S1 wiring). Self-test stays opt-in.

## § 8. Execution log

To be appended by commit 2 (lint extension).
