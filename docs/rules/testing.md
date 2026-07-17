# 测试规约 (Testing Conventions)

> **所有 agent-center 测试相关工作（写测试 / 改测试 / 设计可测接口 / 提交带测试改动）必读。** 跟 [项目规约](conventions.md) 平级，是测试这条切面的权威文档。

本规约对 [`conventions.md` § 14](conventions.md#-14-测试) 的指针展开。可测性原则同时在 [`conventions.md` § 14.x](conventions.md#-14x-设计与实现必须可测) 出现，那里是设计/实现端的硬约束，此处给测试端的细则。

## § 1. 覆盖率要求

| 指标 | 阈值 | 不达标后果 |
|---|---|---|
| **单元测试行覆盖率（line coverage）** | **≥ 90%** | **阻断合并**：CI 跑覆盖率统计，<90% 直接 fail |
| 单元测试分支覆盖率 | 不强制阈值，但报告必须输出 | 用于评审参考，下降需说明 |
| 集成 / e2e 覆盖率 | 不计入上述阈值 | 走 § 3 关键路径清单 |

**操作要求：**

- CI 必须**生成覆盖率统计报告并归档**（产物保留至少与构建产物同周期）
- 单元测试覆盖率统计**强制启用**，PR 描述里给出本次覆盖率数字与基线对比
- 新增代码（diff coverage）部分覆盖率 ≥ 90%，避免"整体 90% 但新代码没测"
- 允许通过显式 `// coverage:ignore` 类注释跳过的代码，必须**逐处给出理由**（不可测、平台桥接、生成代码等），评审需确认

**自检：** 我的改动是否会把整体或 diff 覆盖率拉到 90% 以下？覆盖率报告是否产出并附在 PR？

## § 2. 测试计划与测试报告

**每次测试**（每个功能 / bugfix / 重要重构的测试环节）必须有**测试计划**和**测试报告**两份产物，提交在 PR 描述或独立 markdown 中。**逐条对应、逐条确认**。

- 测试**前**写计划：列清要验证什么、怎么验证、出口标准
- 测试**后**写报告：对计划**每一条**给出实际结果（pass / fail / skip + 理由）
- 计划与报告条目**编号对齐**，评审能 1:1 核对
- 不允许"看着结果反推计划"。计划在写测试代码前定稿

### § 2.1 测试计划模板

```markdown
# 测试计划 — <功能/改动名>

## 范围
- 被测对象：<模块 / 接口 / 路径>
- 改动摘要：<本次主要改了什么>
- 不在范围：<哪些显式不测，为什么>

## 前置条件
- 环境：<本地 / CI / 容器 / 依赖服务版本>
- 数据：<测试 fixture / seed>

## 用例清单
| # | 类型 | 目标 | 输入 | 预期 | 异常 mock |
|---|---|---|---|---|---|
| 1 | unit | <要验证的不变式> | <输入> | <预期结果> | <无 / 或注入哪类异常> |
| 2 | unit | … | … | … | … |
| 3 | integration | … | … | … | … |
| 4 | e2e | … | … | … | … |

## 异常路径覆盖
- [ ] <依赖 A> timeout
- [ ] <依赖 B> 返回 5xx
- [ ] <依赖 C> 返回不一致状态
- [ ] …

## 出口标准
- [ ] 上述每条用例 pass
- [ ] 单元行覆盖率 ≥ 90%（或 diff coverage ≥ 90%）
- [ ] 异常路径用例全部覆盖
- [ ] <其它本次必须达成的条件>
```

### § 2.2 测试报告模板

```markdown
# 测试报告 — <功能/改动名>

## 计划项执行结果
| # | 用例 | 状态 | 证据 / 备注 |
|---|---|---|---|
| 1 | <计划 #1 摘要> | ✅ pass | <log / 截图 / 关键断言> |
| 2 | <计划 #2 摘要> | ❌ fail → 已修 / 待修 | <root cause + 链接> |
| 3 | … | ⏭ skip | <跳过原因> |

## 覆盖率
- 单元行覆盖率：xx.x%（基线 yy.y%，diff coverage zz.z%）
- 报告归档：<CI artifact 链接 / 路径>

## 异常路径
- [x] 依赖 A timeout
- [x] 依赖 B 5xx
- [ ] 依赖 C 不一致状态 → 未覆盖，理由：<…>

## 出口标准核对
- [x] 全部计划项通过 / 显式接受
- [x] 行覆盖率达标
- [x] 异常路径满足或有书面豁免
- [x] <其它>

## 结论
✅ 通过 / ⚠️ 有保留通过 / ❌ 不通过

<如有保留或不通过，列出后续动作清单及 owner>
```

**自检：** 我提交的改动是否同时有计划 + 报告？每条计划项是否在报告里被显式确认（即使是 skip）？

### § 2.3 分层测试报告标准 (Layered Test Report Standard)

> 任何 phase / GA / release 测试报告**必须**把测试数按下面三类**分别**列出。一次性的"55 packages ok / 12 e2e cases pass"那种打包数字是 [§ 0.4 enforce mechanism #5](conventions.md#-04-ddd-原则下做正确的事顶层架构铁律) 防的反面教材。

#### 三类测试

| 类 | 定义 | 例 |
|---|---|---|
| **Unit (in-package)** | `go test ./internal/<pkg>/...`；不出包；外部依赖全是 test double | `internal/cli/handlers_*_test.go`、`internal/taskruntime/dispatch/service_test.go` |
| **Integration with mocks** | 跨包；起 in-process service；走 in-memory channel / sqlite-in-tmpdir / sse-in-process；agent CLI 仍 stub；其它跨进程组件仍 mock | `tests/integration/phase2_test.go`、`internal/cli` 的 `setupAdminServerForTests` 起 in-process admin server 后通过 Client 调 → 真 sqlite + 真 Service，但没 spawn 真 binary |
| **Deployed-binary smoke** | 真 `bin/agent-center` / `bin/agent-center-worker-daemon` / `bin/fakeagent` 子进程；真 unix socket；真 subprocess spawn；no in-process shortcut | `tests/e2e/v2/tests/v22-deployed-pipeline.spec.ts`，通过 `make smoke` / `scripts/smoke/deploy-smoke.sh` 跑 |

#### 报告模板（必填段）

```markdown
## 测试分层 (Layered Test Inventory)

| 层 | 计数 | 入口 |
|---|---|---|
| Unit (in-package) | NN packages / MM cases | `go test ./internal/...` |
| Integration with mocks | NN cases | `go test ./tests/integration/... ./tests/e2e/...`（in-process harness） |
| **Deployed-binary smoke** | NN cases | `make smoke` / Playwright `v22-deployed-pipeline.spec.ts` 等 |
```

#### 硬约束

- **Deployed-smoke 计数 = 0 的 phase / release 不许 close**。这一条覆盖 [§ 0.4 enforce mechanism #4 + #5](conventions.md#-04-ddd-原则下做正确的事顶层架构铁律)：不强制部署 = v2.0 GA 反面教材重演
- 三类之间**不能合并**报。"e2e 12 cases" 不告诉读者其中几个是 in-process / 几个真起 binary
- 数字**必须**对应到具体测试文件 / spec 路径，方便审计 reproduce
- 新增 deployed-smoke spec 的话，同步登记到 [conventions.md § 14](conventions.md#-14-测试) 的关键路径表

#### 反面教材（what NOT to do）

[`docs/plans/reports/phase-12-test-report.md` § 3.1-3.2](../plans/reports/phase-12-test-report.md#-3-test-inventory) 是 v2.0 GA 的测试报告，里面写：

```
55 Go packages compile + test
12 Playwright e2e cases pass
```

这两行数字加起来 67 看着稳，**但 deployed-binary smoke 实际数 = 0**——`bin/agent-center-worker-daemon` 在 v2.0 GA 根本没 build target，所有"e2e"都是 in-process server + 模拟 worker。这正是 @oopslink 在 2026-05-24 firsthand 部署验证时识破整个 transport 层缺失的根因：CI 上 67 个 green test，部署上一个都 cover 不了 "server 进程实际做了什么" 这件事。

v2.2 起每份报告必须分层；分层后这个盲点就**算得出来**——deployed-smoke = 0 = phase 不许 close。

**自检：** 我的报告把 unit / integration-mocked / deployed-smoke 三类分开列了吗？deployed-smoke 计数 ≥ 1 吗？数字能 1:1 对应到具体 test 文件 / spec 路径吗？

## § 3. 测试分层与关键路径

| 层 | 范围 | 强制要求 |
|---|---|---|
| **单元测试 (unit)** | 单个函数 / 包内部逻辑 | 行覆盖 ≥ 90%（§ 1） |
| **集成测试 (integration)** | 跨包 / 持久层 / IO 边界 | 关键集成路径必测（见下） |
| **端到端 (e2e)** | 全链路最小工作流 | 关键路径必测 |
| **契约测试 (contract)** | 多实现共享同一接口 | 一份契约，所有实现都过 |

**必须有 e2e 覆盖的关键路径：**

- `worker enroll → task dispatch → task 结束`（含正常结束、失败结束、取消三条分支）
- `Web Console 入口 → supervisor → task 创建 → 结果回流`（端到端可观测一致）
- 后续新增关键路径同步登记到本节

**契约测试范围：**

- **BlobStore**：local 实现 / S3 mock 实现**走同一份契约测试**，新增后端实现必须过
- 其它"接口 + 多实现"模式（如 store backend）出现时按同样原则建契约测试

## § 4. 可测性（呼应 conventions §14.x）

测试侧的展开见 [`conventions.md` § 14.x](conventions.md#-14x-设计与实现必须可测)。本节给测试端要确认的事：

- 任何外部依赖（DB / blob store / 飞书 SDK / 时钟 / 随机数 / 子进程）都要能在测试中替换为可控实现
- 异常路径（timeout / 5xx / 网络中断 / 状态不一致 / 部分失败）要能通过 mock **显式注入**，不依赖运行时巧合
- 测试不允许 `sleep` 等待 —— 时间走可注入时钟；事件走显式同步原语
- 不允许测试代码绕过 BlobStore 抽象直接读写 DB 大字段，否则就是被测对象设计不合规（§ 8）

**自检：** 我要写的测试是否需要 sleep / 真连外部服务 / 真起后台进程？如果是，对应代码的可测性设计就是错的，先改设计再补测试。

## § 5. 构建 / 发布门禁（测试绿 ≠ 构建绿 ≠ 发布绿）

> 真实事故（2026-06-01，v2.7）：`make release` 被前端 `tsc -b` 的 **TS6133**（`web/src/pages/Environment.test.tsx` 第 1 行 import 了 `beforeEach` 却未使用）挡死。此前 `go test` + `vitest run` + e2e **全绿**，但 release 构建红。这是测试门的真漏洞——不是工具缺失，而是**前端类型检查没被纳入发布前验收**。

### § 5.1 为什么会漏

- `make test` = `go test ./...`，**只覆盖后端**。
- 前端 `pnpm run test` = `vitest run`，走 esbuild 转译，**会丢类型**：`noUnusedLocals`(TS6133)、import type 当值用、类型不匹配等错误在 vitest 下**一律不报**。
- 前端 `tsc` 只在两处执行：① `make build` / `make release`（`tsc -b`）；② `make lint` → `lint-spa-tsc`（`cd web && npx tsc -b --force`，专为防这类而设）。
- 结论：**"测试全绿"不等于"构建能过"**。只跑 `go test` + `vitest` + e2e，前端类型检查根本没执行。

### § 5.2 硬约束

- **任何 ship / release / 关键 PR 验收前，必须跑：**
  1. `make lint`（含 `lint-spa-tsc`，即前端 `tsc -b --force`）—— **必须绿**；
  2. 一次 `make build`（或 `make release` 干跑）—— **必须绿**，确认 `tsc -b && vite build` 整体通过。
- 上述两项任一红 = **阻断合并 / 阻断发布**，与 § 1 覆盖率同级。
- 前端类型检查用 **`tsc -b`（build 模式）**，**不能**用 `tsc --noEmit`：`web/tsconfig.json` 是 `files:[] + references` 结构，裸 `tsc --noEmit` 一个文件都不检查（历史教训：`import type React` 当值用，`tsc --noEmit` 放行但 `make build` 的 `tsc -b` 红）。
- **不得以 "vitest 全绿" 作为前端可发布的证据。** 测试报告（§ 2）若涉及前端改动，出口标准必须显式包含"`make lint` + `make build` 绿"。

### § 5.3 自检

我声明"可发布 / 可合并"前，是否真的跑过 `make lint` 和 `make build`（或 `make release` 干跑）并看到绿？还是只看了 `go test` / `vitest` 的绿就下结论？

## § 6. 竞态门禁（`go test` 绿 ≠ 无 data race）

> 真实事故（2026-07，I110）：`LocalRuntime.State()` 交出裸 `*SessionState`，让 `rt.State().CurrentTaskID` 这种**无锁裸读**成为最自然的写法。它被逮到**两次**：`TestSpawnExecutor_OwnAssignee_ForksNormally`（issue-573e81c4）修好的**当天**，I105 的新测试由一个完全不知情的作者**原样复发**。两次都是人肉 `-race` 逮到的——因为 `-race` 不在任何清单里。

### § 6.1 为什么会漏

- 自测/CI 清单只有 `build` / `vet` / `test`，**`-race` 不在里面**。
- 无锁裸读**短、直观、编译通过**，`go vet` 不拦，**不带 `-race` 时永远绿**。
- data race 是否被检出**取决于时序**：`-race -count=1` 绿**不能证明**无 race。
- 结论：**"测试全绿"不等于"无 data race"**。不显式跑 `-race`，这类 bug 只能靠有人恰好想起来。

### § 6.2 硬约束

- **涉及并发 / goroutine 的包，自测清单与 CI 必须跑 `go test -race -count=N`（N ≥ 5）**，`make test-race` 即此门（当前覆盖 `./internal/agentruntime/...`）。新增并发包**同步加进该 target**。
- **判据**：`-race -count=1` 绿**不算**。红→绿必须用**同一条命令**证明——**没红过判不了绿**。
- **直看退出码，不走管道**：`go test -race ... ; echo $?`。管道吞 `PIPESTATUS` 会给你**假绿**。
- **不许「只把测试改绿」**——把断言删掉 / 加 sleep 躲开时序，是把真 race 藏起来，等同放行事故。
- 任一 race 报告 = **阻断合并**，与 § 1 覆盖率、§ 5 构建门同级。

### § 6.3 设计约束：别把错的写法留成最自然的写法

复发两次证明**这不是纪律问题，是 API 形状问题**。约定（"记得用 `withState`"）不拦人，**类型系统才拦**：

- **不要交出被锁保护的结构的裸指针**。`LocalRuntime` 现在只暴露**带锁 getter**（`CurrentTaskID()`）和**闭包式** `withState(func(*SessionState))`；裸指针出口 `State()` / `StateMu()` 已删除 ⇒ `rt.State().CurrentTaskID` **编译不过**，而不是"能编译但错"。
- 自检：我这个 API，**忘记加锁的写法能编译吗**？如果能，下一个不知情的人一定会那么写。

### § 6.4 自检

我声明"无并发问题"前，是否真的跑过 `-race -count≥5` 并看到绿？基线上**红过**吗？还是只看了 `go test ./...` 的绿就下结论？

## § 7. 自检清单

提交 PR 前：

- [ ] 单元行覆盖率 ≥ 90%（整体 + diff）
- [ ] 覆盖率报告已生成并附在 PR
- [ ] 测试计划 / 测试报告两份齐全，条目编号 1:1 对齐
- [ ] 计划里的每条用例在报告里被显式确认（pass / fail / skip + 理由）
- [ ] **报告按 § 2.3 三层分类列计数**（unit / integration-mocked / deployed-smoke）；phase / release 报告 deployed-smoke ≥ 1
- [ ] **涉及前端改动：`make lint`（含 lint-spa-tsc / `tsc -b`）+ `make build` 均绿（§ 5）；不以 vitest 绿替代前端类型检查**
- [ ] **涉及并发 / goroutine 改动：`make test-race`（`-race -count≥5`）绿（§ 6）；`-count=1` 绿不算；不以 `go test ./...` 绿替代**
- [ ] 关键路径 e2e 覆盖（§ 3）
- [ ] 异常路径用 mock 显式注入，没有靠真实超时 / 真实网络抖动来"自然触发"
- [ ] 没用 sleep；时间 / 随机 / IO 通过注入控制
- [ ] BlobStore 等多实现接口走契约测试
- [ ] 跳过覆盖的代码（`coverage:ignore`）每处有书面理由

任何一项 ❌ → 退回修改。
