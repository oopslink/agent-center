# 测试计划 — PlanGeneration 产品可观测性整改

## 范围

- 被测对象：ProjectManager `PlanGeneration` ledger read service、Web Console generation/evolution HTTP API、Plan detail generation history UI。
- 改动摘要：产品读取 persisted immutable `PlanGeneration` ID、`Plan.active_generation_id`、parent lineage、snapshot 与 `PlanGenerationDiff`；写入只调用 `EvolvePlanGeneration`。
- 不在范围：修改 ADR-0055 Stage remediation 语义、引入新 schema、发布/phase close。

## 前置条件

- 环境：临时 SQLite、可控 clock/ID generator、in-process HTTP server、Vitest/MSW。
- 数据：pending Plan 启动生成 G0，再提交 Gn；另建 stale、replay 与 in-flight conflict fixtures。

## 用例清单

| # | 类型 | 目标 | 输入 | 预期 | 异常 mock |
|---|---|---|---|---|---|
| 1 | unit | G0/Gn lineage read | active Gn ID | 按 parent 从 G0 到 Gn 返回 immutable IDs、snapshot、real diff | 无 |
| 2 | unit | generation node ownership | G0 tasks + Gn new/superseded tasks | 每个 task 归属首次出现的 persisted generation ID | 无 |
| 3 | unit | lineage fail closed | wrong-plan parent / cycle / broken parent | typed generation conflict/not-found；不返回伪历史 | repository double |
| 4 | integration | generation HTTP read | started G0 + evolved Gn | 返回 `active_generation_id`、parent IDs、snapshots、ownership | 无 |
| 5 | integration | exact Evolution write | parent ID + base version + complete diff + reason/evidence/key | 调用 `EvolvePlanGeneration`，返回 persisted generation | 无 |
| 6 | integration | stale parent/version | stale parent 或 base version | HTTP 409；ledger/plan version/topology 不变 | 无 |
| 7 | integration | idempotent replay | 相同 key/payload 两次 | 第二次 `duplicate=true`，同 generation ID，无重复 task/dispatch | 无 |
| 8 | integration | in-flight whole-request rejection | diff 同时含合法新增 task 与非法在途变更 | HTTP 409；新增 task、edge、generation 全不落库 | 无 |
| 9 | UI unit | history/snapshot/ownership | G0/Gn API payload | revision badge、active ID/parent lineage、snapshot progress、real diff、历史切换 | MSW |
| 10 | UI unit | Evolution form contract | valid task/node-decision/edge diff | POST parent ID、base version、non-empty reason/evidence/key、complete diff | MSW |
| 11 | UI unit | conflict guidance | 409 stale/in-flight/idempotency | 展示整体拒绝和刷新/重试提示 | MSW 409 |
| 12 | regression | repository/backend/web gates | 全套源码 | Go/Web tests、lint、build 全绿 | 无 |

## 异常路径覆盖

- [ ] active generation parent 缺失 / 跨 Plan / cycle fail closed
- [ ] stale `parent_generation_id`
- [ ] stale `base_version`
- [ ] reused idempotency key with different payload
- [ ] in-flight touched node causes transaction-wide rollback
- [ ] empty reason/evidence/idempotency rejected before domain mutation

## 出口标准

- [ ] #1–#12 全部 pass
- [ ] 新增/修改生产逻辑 diff coverage ≥ 90%
- [ ] `go test ./...` 与 `cd web && pnpm test` 0 failure
- [ ] `make lint` 与 `make build` 0 failure
- [ ] 无 Stage.generation 推导 active generation；无 `EditPlanTopology` Evolution write
- [ ] 测试报告逐项对应本计划
