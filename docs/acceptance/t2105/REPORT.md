# 测试报告 — T2105 Collaboration Insight 新 main 真实链路验收

## 被测基线

- 远端：`origin/main`
- 精确 SHA：`7cf8beae46c7072e77a5e7871fcccd60905d3f01`
- 验收日期：2026-09-03
- 环境：macOS 隔离 git worktree；真实 SQLite；race detector；fresh-built binaries；Playwright Chromium。

## 计划项执行结果

| # | 用例 | 状态 | 证据 / 备注 |
|---|---|---|---|
| 1 | dependency direction 与释放链路 | PASS | `TestCollaborationEffectDependencyReleaseUsesDependentToUpstreamDirection` 在 `-race -count=10` 下通过（10.630s）。生产 `AddPlanDependency(B,A)` 镜像为 `from=B,to=A`；A 完成前 B dispatch=0，完成后 B dispatch=1；B 得到唯一 `dependency_release` effect 和 2 个 evidence event IDs。 |
| 2 | Query / pagination / summary / Evidence | PASS | `TestQueryServiceGraphPaginationSummaryAndEvidence` 与 `TestQueryServiceStableValidationErrors` 纳入 collaborationeffect 的 `-race -count=10` 运行并通过。Evidence 命中源 event，跨 project fail-closed。 |
| 3 | 幂等和确定性重放 | PASS | `TestProjectorDuplicateTenTimesAndVersionCoexistence`、`TestOutOfOrderDependencyReplayConvergesWhenOrderedLedgerIsReplayed` 与 Query tests 一起 `-race -count=10` 通过；package 用时 315.090s。 |
| 4 | Web 展示 | PASS | `pnpm exec vitest run src/pages/InsightCollaboration.test.tsx`：1 file / 3 tests passed；覆盖图、summary、timeline、URL filters、lazy Evidence、empty/403/500。完整 Web 复跑：200 files / 1910 tests passed。 |
| 5 | deployed-binary smoke | PASS | `make smoke`：Playwright `v22-deployed-pipeline.spec.ts` 1/1 passed；Go runtime-version deployed test passed；fresh binary、unix socket、worker enroll、agent-tools dispatch/claim 真实子进程链路全绿，总计 71s。 |
| 6 | build | PASS | `make build` exit 0；`tsc -b && vite build`、`go build ./cmd/agent-center`、`go build ./cmd/fakeagent` 全绿；前端 bundle 含 `InsightCollaboration-*.js`。 |

## 测试分层 (Layered Test Inventory)

| 层 | 计数 | 入口 |
|---|---:|---|
| Unit (in-package) | 1 Web file / 3 targeted cases；全量 Web 200 files / 1910 cases | `pnpm exec vitest run src/pages/InsightCollaboration.test.tsx`; `pnpm test` |
| Integration with real local persistence | 5 Go cases，均 `-race -count=10` | projectmanager/service dependency-release case；collaborationeffect projector/replay/query/validation 4 cases |
| Deployed-binary smoke | 2 entry points / 2 passed suites | Playwright `v22-deployed-pipeline.spec.ts`；Go `tests/e2e` runtime-version smoke |

## 异常路径

- [x] invalid query / invalid cursor 稳定拒绝。
- [x] Evidence 跨 project 返回 not found。
- [x] Web empty / forbidden / 5xx 状态显式展示。
- [x] 同一 event 重复 10 次与乱序输入经 ordered ledger replay 收敛。

## 补充观察

- 首次误用 `pnpm test -- InsightCollaboration.test.tsx --run` 实际触发全量 suite，其中 `AgentCreateModal.test.tsx` 出现一次与本功能无关的 disabled-state 时序失败（1909/1910）；该文件立即独立复跑 4/4 通过，随后按正确命令执行的完整 `pnpm test` 为 1910/1910。判定为非阻断基线偶发，不影响 Collaboration Insight 验收。
- Vite 构建输出既有 CSS minify warning（`Expected identifier but found "-"`）和大 chunk warning，但退出码为 0；不作为本任务阻断项。

## 出口标准核对

- [x] 计划 1–6 全部通过。
- [x] dependency release、projection、replay 与 query 均通过 `-race -count=10`。
- [x] Web targeted 与完整 suite 通过。
- [x] `make build` 和 deployed-binary smoke 通过。
- [x] 三层测试分开统计，deployed-smoke > 0。
- [x] 报告将提交并推送；任务仅在证据进入 `origin/main` 后完成。

## 结论

**PASS（blocking=false）**。`origin/main@7cf8beae46c7072e77a5e7871fcccd60905d3f01` 满足 T2105 对 dependency release、Collaboration Effect projection/query/Evidence、Web 展示及可回放一致性的真实链路验收要求。
