# v2.2-E — Process gates: arch-test + mock-default lint + drift check + smoke gate + layered test report

> Phase E (slock #17). Per-ST P12 cadence (audit-first + impl). Closes
> the 5 enforce mechanisms in conventions § 0.4. Mechanism #1 (arch
> test on `internal/cli/handlers_*.go`) landed in v2.2-A5
> (`bec2fea`). E delivers mechanisms #2-#5.

## § 0. 必答铁律答卷 (per conventions § 0.4)

| 必答问题 | 答 |
|---|---|
| 起点 = 领域模型要求？ | 是 — 这一 phase 不动领域 / transport，只把 v2.0 GA "全 mock 也过 CI" 的盲点变成 mechanical 阻断。reproducibility 的工具化 |
| 保留 BC invariants？ | 不动 BC，只装 grep + bash smoke 守护 |
| 没省 transport？ | E3 smoke 真起 server + worker daemon + fakeagent 走 admin transport（复用 Phase D 的 deployed-pipeline spec），没省 |
| Mock-as-default 消除？ | 这正是 E1 的工作：lint 阻止 `NoopSender / NoopKillSender / nil Spawner` 类无 `FIXME(prod-wiring)` 注释回流到 production wiring |

## § 1. 范围

### in scope

| ST | Deliverable | Mechanism (§ 0.4) |
|---|---|---|
| **E1** | `scripts/lint/no-mock-default.sh` + Makefile `lint-mock-default` | #2 mock-as-default lint |
| **E2** | `scripts/lint/doc-impl-drift.sh` + Makefile `lint-doc-impl-drift` | #3 doc-impl drift |
| **E3** | `scripts/smoke/deploy-smoke.sh` + Makefile `smoke`（封装 Phase D 的 `v22-deployed-pipeline.spec.ts`） | #4 deployed-binary smoke gate |
| **E4** | `docs/rules/testing.md` 加 "分层测试报告标准"（unit / integration-mocked / deployed-smoke 三类） | #5 self-audit deploy-step 的报告层 |

### out of scope

- mechanism #1 已在 A5 完成（`internal/cli/arch_test.go` + 白名单），本 ST 不重做
- mechanism #5 的另一半（人工 self-audit run-binary 步）是流程要求，本 ST 不能用脚本强制；testing.md 里加报告标准把它变成"phase close 必填"
- 不引入新 CI runner / GH Actions（本仓库目前没有 CI workflow；scripts + make targets 是 local + future-CI 通用入口）

## § 2. 设计选择

### 2.1 E1 — mock-as-default lint

**Pattern**: 复用 `no-vendor-refs.sh` 的 grep-then-allowlist 模板，但允许规则不是文件白名单，而是 **per-hit inline annotation**：

- 触发模式：`NoopSender|NoopKillSender|kill\.NoopKillSender|dispatch\.NoopSender` 在 `internal/**/*.go` 里出现
- 允许：
  1. `_test.go` — 测试当然可以用 mock
  2. `tests/**/*.go` — integration / e2e harness
  3. 同行或上一行有 `// FIXME(prod-wiring):` 注释 — 显式承认是产线漏洞
- fail message 引到 conventions § 0.4 mechanism #2
- 期望：当前应该 0 violation —— A3 已把 production wiring 的 NoopSender 全删

**Why not Go test**: 因为 lint 要看注释（comment-aware grep），用 Go AST 写 overkill；bash `grep -B1` 足够，且和 `no-vendor-refs.sh` 工具栈一致。

### 2.2 E2 — doc-impl drift check

**问题**：ADR 写了某个事实，代码里其实没那个事实——v2.0 GA 就是 ADR-0038 写了"CLI 通过 admin endpoint"，但 admin endpoint 根本不存在。

**Pattern**：encode "ADR claim X → grep code condition Y" 的 check 列表。当前装 3 条 anchor check：

| # | Doc claim | Grep contradiction |
|---|---|---|
| 1 | conventions § 0.4 + ADR-0038：CLI 走 admin endpoint，不直读 sqlite | `internal/cli/admin_client.go` **必须存在**（v2.0 GA 缺失就是 drift） |
| 2 | conventions § 0.4：AppService 唯一入口；handlers_*.go 不应直读 DB | `internal/cli/handlers_*.go` (排除白名单) 内不应 `persistence.Open` —— 跟 arch_test 重叠是双层保险 |
| 3 | conventions § 4：零 LLM SDK 依赖 | `go.mod` / `internal/**/*.go` 不应 `import` `github.com/(anthropics\|openai\|google)/.*sdk` 类路径 |

**扩展方法**：脚本 header 写清"如何加 check"——新加一条就是新加一个 `check_N()` bash function。

### 2.3 E3 — deployed-binary smoke

**最小变更路径**：调用 `make e2e` 并显式 filter 到 `v22-deployed-pipeline.spec.ts`。理由：
- spec 已经在做"server + worker daemon + fakeagent → task done"的完整 transport pipeline
- 重新用 bash + curl 写一遍 = 重复工作 + 容易跟 spec 漂移
- spec 自带 diagnostics (stderr 抓取 / artifact attach)，比 bash 输出更易诊断

**输出契约**：脚本最后必须打印一行 `smoke pass: NN seconds` 或 `smoke FAIL at step X`。`set -e` + `time` + trap on ERR 即可。

**前置依赖**：`make build` 已 chain 了 `build-worker-daemon` + `build-fakeagent` (Makefile line 23)，build 步只调 `make build` 就齐。

### 2.4 E4 — 分层测试报告标准

**问题**：phase-12-test-report.md § 3.1 写 "55 Go packages compile + test"，§ 3.2 写 "12 Playwright cases" —— 但 12 个 Playwright 里 **0 个真 deploy binary**（v2.0 GA ship 时 worker daemon binary 都不存在），全是 in-process server + 模拟 worker。统计上看着 67 个 test，实际 deployed-binary 测试数 = 0，刚好对应 mechanism #5 漏掉的根本原因。

**修复**：testing.md § 2.2 测试报告模板下面加新小节 § 2.3 "分层测试报告标准"，要求每份 phase / release / GA 报告把测试拆 3 类显式列出：

| 类 | 定义 | 例 |
|---|---|---|
| **Unit (in-package)** | `go test ./internal/<pkg>/...`；不出包；mock 全是 test double | `internal/cli/handlers_*_test.go` |
| **Integration with mocks** | 跨包 / 起 in-process service / 走 in-memory transport；agent CLI 仍 stub | `tests/integration/phase2_test.go`、in-process admin server test |
| **Deployed-binary smoke** | 真 `bin/agent-center` 子进程；真 unix socket；真 fakeagent subprocess；no in-process shortcut | `tests/e2e/v2/tests/v22-deployed-pipeline.spec.ts` |

**硬约束**：deployed-smoke 计数 = 0 的 phase / release **不许 close**。phase-12-test-report.md 显式 cite 为 "what NOT to do"（数字打包 ≠ 分层验证）。

## § 3. 文件影响清单

新增：
- `scripts/lint/no-mock-default.sh`
- `scripts/lint/doc-impl-drift.sh`
- `scripts/smoke/deploy-smoke.sh`
- `docs/plans/v2.2-audits/v22-E-process-gates-audit.md` (this file)

修改：
- `Makefile` — 加 `lint-mock-default` / `lint-doc-impl-drift` / `smoke` targets；扩展 `lint` 复合 target；可选加 `help` summary
- `docs/rules/testing.md` — § 2.3 新分层标准
- `docs/rules/conventions.md` — 不动（§ 0.4 已经写好；mechanism 列表里把 #1 标 done 不在本 ST 范围）

## § 4. 验收

- `bash scripts/lint/no-mock-default.sh` exit 0（A3 已清理 production wiring，应 clean）
- `bash scripts/lint/doc-impl-drift.sh` exit 0（admin_client.go 存在；handlers_*.go 已过 A5 白名单；无禁 SDK）
- `bash scripts/smoke/deploy-smoke.sh` exit 0 + 末行 `smoke pass: NN seconds`
- `make lint-mock-default` / `make lint-doc-impl-drift` / `make smoke` 全 pass
- `go test ./...` 不退化
- testing.md § 2.3 与 phase-12-test-report.md "what NOT to do" 互引

## § 5. 不在本 ST 范围

- CI workflow yaml — 仓库当前没有 `.github/workflows/`，加 GH Actions 是 v2.3 ops 工作
- mechanism #5 的人工 self-audit 流程改造 — 文档化了"deployed-smoke 计数硬要求"等于把流程要求 codify 进 testing.md，已最大化
- 把 `lint-mock-default` 加入 `lint` 复合 target — 加；`smoke` 因为耗时 ~10-30s + 跨进程，**不**加进 `lint`，单独 target

## § 6. 风险

- macOS BSD `grep` 无 `-P` PCRE — 全用 `-E` ERE + 简单字符类即可
- smoke 依赖 Playwright npm 环境 — 脚本 head 先 check `pnpm` + `chromium` 在；缺失给清晰错误而不是 hang
- doc-impl drift 启动 3 条 check 偏少 — header 写"how to add" + 标 "TODO: add more as ADRs land"
