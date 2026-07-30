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
| 7 | build / vet / test / race | 有环境保留 | build、vet PASS；目标单元/集成 PASS；全量与 race 在宿主机的 git/SQLite 子进程上触发 10m timeout，未出现 DATA RACE |

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
- `make test-race`：未报告 DATA RACE；真实 git unreachable/push/cache 测试
  在同一宿主机达到 10m timeout。

## 结论

功能级与改动面门禁通过；全仓重 IO 门受当前宿主机 git/SQLite 延迟影响，
应由独立 Gate 在干净隔离实例重跑 deployed-binary smoke 与全量 race 后给出
Stage 2 的最终 pass/reject。Dev 分支不合并 main。
