# 测试报告 — T1248 Agent 单入口纵向运行链

## 计划项执行结果

| # | 用例 | 状态 | 证据 |
|---|---|---|---|
| 1 | selection round-trip / fail closed | PASS | `TestAgentRuntimeSelectionRoundTripAndValidation` |
| 2 | resolver / shadow | PASS | `TestExecutionFreezerUsesPersistedAgentSelection`；`TestShadowExecutionFreezerDoesNotWriteSnapshot` |
| 3 | adapter argv | PASS | `TestRuntimeRunnerBuilders_MapKnownParametersAndRejectUnknown` |
| 4 | task lifecycle byte stability | PASS | `TestRuntimeSnapshotProductionLifecycleIsByteStable` |
| 5 | worker fork consumes frozen Snapshot | PASS | `TestBuildWorkItem/frozen_runtime_snapshot...`；admission/fork tests |
| 6 | legacy / F3 compatibility | PASS | Snapshot 缺失仍走原 router；ADR-0056 与既有 task override tests |
| 7 | supervisor_inline Runtime 一致性 | PASS | `TestExecutionFreezerInlineCompatibility`；`TestNotifyWork_SupervisorInline_RuntimeSnapshotGate` |
| 8 | build / vet / test / race | 有环境保留 | build、vet PASS；目标单元/集成 PASS；全量与 race 在宿主机的 git/SQLite 子进程上触发 10m timeout，未出现 DATA RACE |

## 关键原始结果

- 新增/相关 unit inventory：131 个 `--- PASS`，0 FAIL。
- integration：
  - lifecycle byte stability PASS；
  - frozen Snapshot -> WorkItem PASS；
  - admission 后 fork PASS；
  - already-running continuation PASS。
- 变异验证：临时移除 `runtimeSnapshotCLI` 生产接线后，
  `TestBuildWorkItem/frozen_runtime_snapshot...` 明确 FAIL；恢复后 PASS。
- `go build ./...`：PASS。
- `go vet ./...`：PASS。
- `go test ./...` 首跑：`internal/environment/service` 在 migration SQLite
  写事务等待中 10m timeout；同一失败测试单独 `-count=3` 为 3/3 PASS。
- `go test -p 2 ./...`：宿主机极慢时 `internal/admin/api` 与
  `internal/agentruntime` 达到 10m package timeout；相关改动测试单独通过。
- loopback 最终树再次运行 `go test ./...`：`internal/admin/api`、
  `internal/cognition/memory`、`internal/cognition/memory/centergit`、
  `internal/environment/service` 分别在 HTTP/真实 git/SQLite fixture 上达到
  package 10m timeout；无 AI Runtime 断言失败。该结果仍不满足“全量绿”，未降格出口。
- `make test-race`：未报告 DATA RACE；真实 git unreachable/push/cache 测试
  在同一宿主机达到 10m timeout。

## Gate loopback（2026-07-30）

- 采用 Gate 裁定 A：常驻 supervisor 不做逐 task 重启；`supervisor_inline` 的冻结
  CLI/model 必须与 Agent 当前 selection 和 resident session 实际 CLI/model 一致。
- 中心 `StartTask` 在 Snapshot 冻结后、Task 状态迁移前执行 selection 兼容性门；
  runtime `NotifyWork` 在创建 task dir / 注入 resident session 前执行实际 session 兼容性门。
- CLI/model 不一致会逐字段报告 `resident`/`snapshot`，建议改用 `executor_fork`
  或选择匹配常驻 supervisor 的 Profile；拒绝测试同时断言零注入、零任务占用。
- 受影响四包 `go vet` PASS；`airuntime` 与 `agentruntime` 全包 PASS；
  `projectmanager/service` 全包在高负载宿主运行 377s 后人工中止，随后本改动
  真入口集成用例独立 PASS。未把这条记录冒充全仓门禁已绿。
- fresh-binary deployed smoke：
  - flag ON：`AC_AI_RUNTIME_AGENT_EXECUTION=1 ...v22-deployed-pipeline.spec.ts` PASS，
    真 server/worker/agent-tools 跑完 claim/start/block/unblock/reassign，Snapshot
    在 Catalog 改动及 continuation 前后字节一致；
  - flag OFF：相同真实链 PASS，并断言 execution 全程不写 Snapshot；
  - 首轮未显式开 flag 时暴露 spec 把 OFF 当 ON 的红证据（Snapshot 为空）；
    spec 现按实际 flag 分支断言。高负载宿主两次未在旧 70s 内写 bootstrap token，
    stderr 为空；将有界启动等待提高至 180s 后 ON 在 2.1m 内完成，未放宽业务断言。

## 结论

功能级与改动面门禁通过；全仓重 IO 门受当前宿主机 git/SQLite 延迟影响，
应由独立 Gate 在干净隔离实例重跑 deployed-binary smoke 与全量 race 后给出
Stage 2 的最终 pass/reject。Dev 分支不合并 main。
