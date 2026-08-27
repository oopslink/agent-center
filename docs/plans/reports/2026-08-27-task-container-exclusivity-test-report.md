# 测试报告 — Task 工作容器互斥快修

对应计划：`docs/plans/2026-08-27-task-container-exclusivity-test-plan.md`。

## 计划项执行结果

| # | 状态 | 证据 |
|---|---|---|
| 1 | ✅ pass | `TestMigration_0147_RemovesOnlyPlannedPoolDuplicates` |
| 2 | ✅ pass | `TestTaskContainerExclusivity_PlanLifecycleAndExplicitReturn`：Plan 选择原子移除 Pool row |
| 3 | ✅ pass | 同上覆盖 ready、future/blocked 与 running Plan；generation blocked supersede 回归覆盖历史节点 |
| 4 | ✅ pass | 同上覆盖权威 Pool read / membership / claim fail-closed；auto-assign tx 增加 Plan gate |
| 5 | ✅ pass | 同上及 API 测试覆盖 pending Plan 显式移除后回 Backlog |
| 6 | ✅ pass | 同上及 API 测试覆盖后续独立 Pool dispatch |
| 7 | ✅ pass | `TestPMTaskContainerExclusivity_AuthoritativeLists` |
| 8 | ✅ pass | `ProjectPlans.test.tsx` 的单容器优先级回归 |
| 9 | ✅ pass | `go test ./...`、`cd web && pnpm test --run`、`make lint`、`make build` 均退出 0 |
| 10 | 待最终验收 | 完成合并前记录真实 binary smoke |

## 测试分层

| 层 | 当前证据 |
|---|---|
| Unit / domain | 106 个 Go package 的全量 `go test ./...`；ProjectManager service + migration 定向回归 |
| Integration with mocks | Web Console API（真实 SQLite + in-process HTTP）与 Vitest/MSW Work Board；全量 Web suite 退出 0 |
| Deployed-binary smoke | 待最终验收记录 |

## 结论

待完成全量门禁、真实 binary smoke 与远端 `origin/main` 回读后定稿。
