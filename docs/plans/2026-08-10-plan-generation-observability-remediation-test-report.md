# 测试报告 — PlanGeneration 产品可观测性整改

## 计划项执行结果

| # | 用例 | 状态 | 证据 / 备注 |
|---|---|---|---|
| 1 | G0/Gn lineage read | ✅ pass | `TestGetPlanGenerations_ReadsPersistedG0GnSnapshotsAndOwnership` 验证从 `Plan.active_generation_id` 沿 persisted parent 回溯并按 G0→G1 返回真实 ID。 |
| 2 | generation node ownership | ✅ pass | 同一 service test 验证保留节点、被 supersede 节点、新增节点分别归属首次出现的 generation ID，并验证 `present_in_active`。 |
| 3 | lineage fail closed | ✅ pass | `TestLoadPlanGenerationLineage_FailsClosed` 的 4 个 subtest 覆盖 missing parent、cycle、cross-plan、snapshot identity mismatch，均返回 typed generation conflict。 |
| 4 | generation HTTP read | ✅ pass | `TestPlanGenerationAPI_G0GnLineageSnapshotReplayAndStaleConflicts` 验证 active ID、parent ID、G0/G1 snapshot、real diff 与 node ownership。 |
| 5 | exact Evolution write | ✅ pass | HTTP integration test 发送 parent ID、base version、完整三数组 diff、reason/evidence/key；响应为 persisted G1。另由 UI test 断言精确 POST body。 |
| 6 | stale parent/version | ✅ pass | HTTP integration test 分别断言 stale parent 与 stale base version 返回 409；service 事务测试验证失败不改变 version/ledger/topology。 |
| 7 | idempotent replay | ✅ pass | 同 payload/key 重放返回 `duplicate=true` 与同 generation ID；不同 payload 重用 key 返回 409。core service suite 同时覆盖无重复 task/dispatch。 |
| 8 | in-flight whole-request rejection | ✅ pass | service 与 HTTP 两层均用“合法新增 task + 非法改写已 dispatch dependent”验证 409/typed error，且 task、edge、generation、version 全部回滚。 |
| 9 | UI history/snapshot/ownership | ✅ pass | `PlanDetail.test.tsx` 覆盖 active R2、progress/diff、generation/parent ID、node revision badge，以及 legacy DAG 和 orchestration graph 两条 immutable G0 snapshot 切换路径；另验证 Stage generation 整数不能制造 PlanGeneration 历史。 |
| 10 | UI Evolution form contract | ✅ pass | UI 断言 POST 包含 `parent_generation_id`、`base_version`、non-empty reason/evidence/key 与完整 `PlanGenerationDiff`；非法/缺字段 diff 在 POST 前拒绝。 |
| 11 | UI conflict guidance | ✅ pass | UI 覆盖 stale parent/version、in-flight all-request rejection、idempotency conflict 三类可恢复提示。 |
| 12 | repository/backend/web gates | ✅ pass | `go test ./...`、`pnpm test`、`make lint`、`make build` 最终均 exit 0；`git diff --check` clean。 |

## 测试分层 (Layered Test Inventory)

| 层 | 计数 | 入口 |
|---|---:|---|
| Unit (in-package) | 6 个新增/整改 service case（含 4 个 fail-closed subtest）；相关 core PlanGeneration suite 共 6 个 top-level tests | `internal/projectmanager/service/plan_generation_{read,evolution}_test.go` |
| Integration with mocks | 2 个 SQLite + in-process HTTP tests；6 个 PlanGeneration 专项 UI/MSW tests | `internal/webconsole/api/handlers_pm_plan_graph_test.go`、`web/src/pages/PlanDetail.test.tsx` |
| Deployed-binary smoke | 0 | 本项是 feature remediation，不是 phase / GA / release close；按测试计划明确不执行部署 smoke。 |

全量回归实际入口：102 个 Go packages；186 个 Vitest files / 1,718 tests。

## 覆盖率

- 基线：被拒候选 `5b5ffe89`（remediation commit 的直接 parent）。
- Go remediation diff line coverage：**93.10% (189/203)**。覆盖数据由 `go test -covermode=count -coverpkg=...` 生成于 `/tmp/t1324-go-full.cover`，再按 `git diff --unified=0 5b5ffe89 -- '*.go'` 统计 changed executable lines。
- Web remediation diff line coverage：**98.46% (192/195)**；diff branch coverage：**84.72% (122/144)**。覆盖数据由 V8/Istanbul 生成于 `/tmp/t1324-web-coverage/coverage-final.json`，再按 `git diff --unified=0 5b5ffe89` 统计 changed executable lines/branches。
- Go 1.25 原生 coverprofile 不输出 branch metric；对应语义分支由 tests 显式覆盖：G0/Gn、4 类 broken lineage、stale parent、stale version、same-key replay、different-payload idempotency conflict、node-decision/edge in-flight rollback。
- 未使用 `coverage:ignore`。

## 异常路径

- [x] active parent 缺失、跨 Plan、cycle、snapshot identity mismatch fail closed
- [x] stale `parent_generation_id`
- [x] stale `base_version`
- [x] reused idempotency key with different payload
- [x] in-flight touched node causes transaction-wide rollback
- [x] empty reason/evidence/idempotency and incomplete diff rejected before mutation
- [x] malformed UI diff rejected before HTTP request

## 命令证据

| 命令 | 结果 |
|---|---|
| `go test ./internal/projectmanager/service ./internal/webconsole/api` | ✅ pass |
| `go test ./...` | ✅ pass（102 packages） |
| `cd web && pnpm exec vitest run src/pages/PlanDetail.test.tsx` | ✅ pass（107 tests） |
| `cd web && pnpm test` | ✅ pass（186 files / 1,718 tests） |
| `make lint` | ✅ pass（go vet、gofmt、project linters、tsc、eslint） |
| `make build` | ✅ pass（Vite production bundle + Go binaries） |
| `git diff --check` | ✅ clean |

首次并行执行全量 Go/Web tests 时，`internal/workerdaemon/TestSupervisorSession_DetachSurvives` 在高负载下超时；该测试单独重跑 2.14s 通过，随后独占资源完整重跑 `go test ./...` 通过。该路径与本改动无代码交集。

## 出口标准核对

- [x] #1–#12 全部通过
- [x] remediation diff line coverage ≥ 90%
- [x] 所列异常路径全部覆盖
- [x] 全量 Go/Web tests、lint、build 全绿
- [x] generation read/write 路径无 `Stage.generation` active 推导、无 `EditPlanTopology` Evolution write

## 结论

✅ 通过。产品可观测性与 Evolution 写入已统一到 persisted immutable `PlanGeneration` ledger/API 合同。
