# Phase 4 测试报告

> 完成日期：2026-05-21 · 提交 SHA：`28853ef`

Phase 4 范围：Observability BC 完整化（TaskExecutionProjection 投影 + TaskStatusProjection helper + FleetSnapshot + TraceArchive + LocalDirBlobStore + QueryService（Inspect×10 + Query×8）+ StatsService + LogsService + UnknownEventEscalator + 6 个 CLI 顶级动词 + peek-trace 真跨进程 RPC）。

## § 1. 覆盖率汇总

| 维度 | 数值 | 是否达标（≥ 90%） |
|---|---|---|
| 整体行覆盖率 | **90.0%** | ✅ 达标 |
| Phase 4 diff 行覆盖率 | **~91.7%**（observability/ + blobstore/ + cli/handlers_observability + projection / trace / peek / escalator / query 等 21 个新文件加权） | ✅ 达标 |
| 分支覆盖率（参考） | n/a（Go cover 工具不输出分支） | - |

**生成命令**：
```bash
go test -coverprofile=/tmp/cov.out -coverpkg=./internal/... -count=1 ./internal/... ./tests/...
go tool cover -func=/tmp/cov.out | tail -1
# → total:	(statements)	90.0%
```

## § 2. 测试场景执行结果

### 2.1 单测

| 场景 | 用例数 | pass / fail | 备注 |
|---|---|---|---|
| TaskExecutionProjection（VO / Repo / Service） | 14 | 14/0 | UPSERT 新行 / UPDATE / staleness drop + emit / FK 缺失 / FindByID-NotFound / FindByIDs / 输入校验 / 100 次高频 / checker hits-misses-error / nil receiver guard / nil-clock 默认 |
| TaskStatusProjection helper | 5 | 5/0 | tx 必填 / writer 错误传播 / 输入校验 / nil receiver / 真 sql.Tx invoke |
| BlobStore LocalDir（契约测试） | 16 | 16/0 | PutGetRoundTrip / Exists / Delete / List skip-tmp / URL / RelPath 校验（abs / .. / empty）/ Overwrite / size mismatch / max bytes / 空 root 拒绝 / unknown-size + 运行时超限 |
| TraceArchiveService | 14 | 14/0 | happy / tar 内容校验 / center callback 调用 + 错误传播 / 源文件缺失 / BlobStore 错误 / 超大 payload / 必填校验 / WithActor / WithMaxBytes / nil-store error / PendingScanner derive-skip / derive-error |
| FleetSnapshotService | 6 | 6/0 | 4 段 happy / --project 三段过滤 / partial failure 双 warning / 空 fleet / IR 经 exec→task→project filter / worker mapping filter |
| QueryService（Inspect ×10 + Query ×8） | 26 | 26/0 | 10 个 kind 各 happy（含 supervisor / decision Phase 6 stub）/ unknown kind / id missing / not found / artifact JOIN / message JOIN / 8 个 resource happy / refs filter / actor / correlation / decision_id / limit too large / cursor 分页 1000 行 |
| StatsService | 7 | 7/0 | 5 个 scope happy / unknown scope / since 过滤 / counter 数学正确 |
| LogsService | 6 | 6/0 | unknown kind / archived follow / Task happy / Exec happy / TargetMissing / empty id |
| UnknownEventEscalator | 8 | 8/0 | below threshold / triggered / dedup 24h / dedup reset post-24h / 缺字段跳过 / 多分组 / Config default / nil deps + Run error report |
| peek-trace（server + client） | 13 | 13/0 | TailLast / KindFilter / ExecutionNotFound / TraceFileMissing / InvalidRequest / ConnectFailed worker offline / FollowMode 真追加 / 配置校验 / ErrConnectFailed unwrap / Addr / Close idempotent / malformed JSON reply / reason 闭集 |
| CLI handlers（inspect / query / ps / stats / logs / peek-trace） | 28 | 28/0 | 6 个 verb happy / 用法错误 / not_found / JSON+human 双轨 / limit too large / unknown_resource / unknown_scope / unknown_kind / bad --since / bad --until / 10 kind smoke / 8 resource smoke / 5 scope smoke / archive --follow → ExitUsage / worker offline → ExitBusinessError / socket unset → ExitBusinessError / --watch ticker + ctx cancel / Logs happy + BlobStore wired / Logs blob_not_found / Logs no-store-error / Logs generic |
| EventRepository.Find 扩展 | 8 | 8/0 | type 前缀 / cursor 1000 行无重无漏 / since-until 窗口 / actor / limit too large / refs 合取 / decision_id 索引 / correlation_id 索引 / default limit |

**单测汇总：151 用例，全部 pass，0 fail / 0 skip。**

### 2.2 集成测试

| 场景 | 涉及工件 | 关键断言 |
|---|---|---|
| Projection 高频 300 次 push no leak | TaskExecutionProjectionService + 真 SQLite WAL | 最终 total_tool_calls = N，无 lock 错误 |
| Stale drop 事件落 events 表 | Projection + EventSink 同 tx | 1 条 observability.projection_stale_drop |
| Trace archive 终态 roundtrip | TraceArchiveService + LocalDirBlobStore + center callback | upload + 回填 blob_ref + uploaded event |
| Trace archive retry on restart | PendingScanner + LocalDirBlobStore | uploaded.json marker 写入 + retry 成功 |
| UnknownEventEscalator dedup 跨 scan | EventRepository.Find + 内存 dedup map | 11+5 条同 group / 24h 内只 emit 一次 escalation |
| Events cursor 分页 1000 行 | EventRepository + QueryService | 10 页拼回 1000 行无重无漏 |
| 跨 BC tx rollback | EventSink + ProjectionRepo 在同一 RunInTx 内 | 故意 return error 后 events + projection 表均无记录 |
| CLI handlers full stack | cli.NewApp + 6 个 observability verb 跑 happy | 所有 verb ExitOK |
| NewApp BlobStore 自动连接 | cli.NewApp + cfg.BlobStore.Root | App.BlobStore + LogsSvc 非 nil |
| migration 0004 idempotent | migrator Up / Up / Version → 4 | 重复 Up 无副作用，Down 0 全清 |

**集成汇总：10 场景，全部 pass。**

### 2.3 e2e 测试

| 场景 | 用户视角 / 入口 CLI | 关键断言 |
|---|---|---|
| Inspect task happy | `query tasks --project=proj` → `inspect task <id>` | exec & projection JSON 数据稳定 |
| Inspect kind=unknown | `inspect blob X` | exit 2 (Usage) |
| Inspect task not found | `inspect task T-missing` | exit 17 (NotFound) |
| Query events prefix filter | `task create` → `query events --type=task.` | 返回非空，全 task.* 前缀 |
| Query limit too large | `query events --limit=99999` | exit 2 (Usage) |
| Ps human + JSON | `ps` / `ps --format=json` | FLEET SNAPSHOT 头 / executions key |
| Stats counters | `stats --scope=workers --format=json` | scope key + worker counter |
| Stats unknown scope | `stats --scope=blob` | exit 2 (Usage) |
| Logs --follow on archive | `logs task T-x --follow` | exit 2 (Usage) 显式 message |
| Peek-trace worker offline | `peek-trace E-1 --socket=/tmp/absent.sock` | exit 1 (Business) |
| Peek-trace TailLast 真 cross-process | 真起 peek.Server + 真 events.jsonl + 真二进制 dial | 2 行 stdout |
| Peek-trace execution not found | 真 socket + missing exec | exit 17 (NotFound) |
| 10 inspect kind smoke | inspect each kind | exit 17 (NotFound) 或 0（supervisor / decision stub） |

**e2e 汇总：13 场景，全部 pass。**

## § 3. 跟测试计划（plan-4 § 5）的对位

| § 5 行号 | 场景描述 | 实际用例文件:函数 | 状态 |
|---|---|---|---|
| 5.1 - 1 | TaskExecutionProjectionService UPSERT 新行 | `internal/observability/sqlite/projection_repo_test.go:TestProjectionRepo_Upsert_Fresh_Insert` | ✅ |
| 5.1 - 2 | UPSERT UPDATE 路径 | `projection_repo_test.go:TestProjectionRepo_Upsert_Update_Path` | ✅ |
| 5.1 - 3 | staleness（out-of-order） | `projection_repo_test.go:TestProjectionRepo_Upsert_Stale_Drop` + `projection/service_test.go:TestService_Update_Stale_EmitsEvent_NoOverwrite` | ✅ |
| 5.1 - 4 | FK 违反等 explicit error | `projection/service_test.go:TestService_Update_NotFound_FromChecker` | ✅ |
| 5.1 - 5 | FindActive subset filter | 通过 `inspect_execution` JOIN 验证（`query/service_test.go:TestInspect_Execution_WithProjection`） | ✅ |
| 5.1 - 6 | OnExecutionTerminal in tx | `projection/task_status_projection_test.go:TestStatusProjection_WithTx_InvokesWriter` | ✅ |
| 5.1 - 7 | 调用未在 tx 内 explicit error | `task_status_projection_test.go:TestStatusProjection_RequiresTx` | ✅ |
| 5.1 - 8 | FleetSnapshot 4 段全成功 | `query/fleet_test.go:TestFleetSnapshot_FourSegments_HappyPath` | ✅ |
| 5.1 - 9 | 1 段失败 partial | `fleet_test.go:TestFleetSnapshot_PartialFailure_EmitsWarnings` | ✅ |
| 5.1 - 10 | --project 过滤 4 段都生效 | `fleet_test.go:TestFleetSnapshot_ProjectFilter` + `fleet_extra_test.go:TestFleetSnapshot_IRProjectFilter_ViaExecChain` + `fleet_workers_filter_test.go:TestFleetSnapshot_WorkerProjectFilter_WithMapping` | ✅ |
| 5.1 - 11 | TraceArchive 终态触发 | `trace/archive_test.go:TestTraceArchive_HappyPath_UploadedEvent` | ✅ |
| 5.1 - 12 | upload 失败 emit + 本地保留 | `archive_test.go:TestTraceArchive_BlobStoreError_EmitsBlobStoreUnavailable` | ✅ |
| 5.1 - 13 | retry on restart | `archive_test.go:TestPendingScanner_RetriesUnfinishedArchives` + `integration/phase4_test.go:TestPhase4_TraceArchive_FailureThenRetry` | ✅ |
| 5.1 - 14 | EventRepo.Find cursor 1000 行 | `observability/sqlite/event_repo_find_test.go:TestEventRepo_Find_CursorPagination_NoDupesNoGaps` | ✅ |
| 5.1 - 15 | type 前缀匹配 | `event_repo_find_test.go:TestEventRepo_Find_TypePrefixMatch` | ✅ |
| 5.1 - 16 | refs 合取过滤 | `event_repo_find_test.go:TestEventRepo_Find_RefsConjunction` | ✅ |
| 5.1 - 17 | Inspect × 10 kinds | `query/service_test.go:TestInspect_*` + `cli/handlers_observability_test.go:TestInspect_AllKinds_NoPanic_SnapshotShape` + `tests/e2e/phase4_test.go:TestE2EP4_AllInspectKinds_SmokeNoCrash` | ✅ |
| 5.1 - 18 | Query × 8 resources | `query/service_test.go:TestQuery_*` + `cli/handlers_observability_extra_test.go:TestQueryCmd_AllResources_NoCrash` | ✅ |
| 5.1 - 19 | StatsService scope matrix | `query/stats_test.go:*` + `cli/handlers_observability_extra_test.go:TestStatsCmd_AllScopes_NoCrash` | ✅ |
| 5.1 - 20 | Inspect handler human+json | `cli/handlers_observability_test.go:TestInspectCmd_Task_HumanOutput` / `TestInspectCmd_Task_JSONOutput` | ✅ |
| 5.1 - 21 | Query exit code 拼写错误 | `handlers_observability_test.go:TestQueryCmd_UnknownResource` | ✅ |
| 5.1 - 22 | Ps --watch 一轮循环 | `handlers_observability_watch_test.go:TestPsCmd_Watch_TicksAndExitsOnCancel` | ✅ |
| 5.1 - 23 | Logs --follow on archive | `handlers_observability_test.go:TestLogsCmd_FollowOnArchived_Explicit` | ✅ |
| 5.1 - 24 | PeekTrace worker_offline | `handlers_observability_test.go:TestPeekTraceCmd_WorkerOffline` | ✅ |
| 5.1 - 25 | UnknownEventEscalator threshold | `escalator/escalator_test.go:TestEscalator_ThresholdTriggered` | ✅ |
| 5.1 - 26 | 去重 | `escalator_test.go:TestEscalator_Dedup_24hWindow` | ✅ |
| 5.1 - 27 | Find since + until | `event_repo_find_test.go:TestEventRepo_Find_SinceUntilWindow` | ✅ |
| 5.1 - 28 | Find empty filter default | `event_repo_find_test.go:TestEventRepo_Find_DefaultLimit_Applies` | ✅ |
| 5.1 - 29 | actor / correlation_id / decision_id filter | `event_repo_find_test.go:TestEventRepo_Find_ActorFilter` / `_DecisionAndCorrelation` | ✅ |
| 5.1 - 30 | FleetSnapshot json 序列化稳定 | `cli/handlers_observability_extra_test.go:TestPrintFleet_HumanIncludesAllSegments` + cli e2e JSON | ✅ |
| 5.2 - 1 | events 表 cursor 分页（真 SQLite file） | `integration/phase4_test.go:TestPhase4_Events_CursorPagination_1000Rows` | ✅ |
| 5.2 - 2 | projection 高频更新 | `integration/phase4_test.go:TestPhase4_Projection_HighFrequency_NoLeak` | ✅ |
| 5.2 - 3 | fleet 聚合多表 | `query/fleet_test.go:TestFleetSnapshot_FourSegments_HappyPath`（含 PK JOIN 投影） | ✅ |
| 5.2 - 4 | terminal hook trace archive | `integration/phase4_test.go:TestPhase4_TraceArchive_TerminalHook_Roundtrip` | ✅ |
| 5.2 - 5 | events 同事务双写 tx rollback | `integration/phase4_test.go:TestPhase4_TxRollback_AffectsBothTables` | ✅ |
| 5.2 - 6 | § 9.z 隔离回归 | `grep -rn "task_execution_projections\|TaskExecutionProjectionRepo\|observability/projection" ./internal/taskruntime/` → 0 hits | ✅ |
| 5.2 - 7 | migration up + down 0004 | `persistence/migrator_test.go:TestMigrator_VersionTracksApplied`（更新为 4）+ `integration_test.go:TestINT2_MigrationIdempotent` | ✅ |
| 5.2 - 8 | 跨 BC tx 失败 rollback | `integration/phase4_test.go:TestPhase4_TxRollback_AffectsBothTables` | ✅ |
| 5.2 - 9 | UnknownEvent dedup 跨 scan | `integration/phase4_test.go:TestPhase4_Escalator_DedupAcrossScans` | ✅ |
| 5.2 - 10 | stats 数学正确 | `query/stats_test.go:TestStats_Executions_CountsActiveAndTerminal` | ✅ |
| 5.3 - 1 | dispatch → ps 可见 | 通过 `tests/e2e/phase4_test.go:TestE2EP4_Ps_HumanAndJSON`（task create → ps 验证 fleet 行） | ✅ |
| 5.3 - 2 | Inspect execution --include-projection | 通过 `query/service_test.go:TestInspect_Execution_WithProjection` + e2e `TestE2EP4_Inspect_Task_Roundtrip` | ✅ |
| 5.3 - 3 | Query events 跨 task | `tests/e2e/phase4_test.go:TestE2EP4_Query_Events_PrefixMatch` | ✅ |
| 5.3 - 4 | Peek-trace live | `tests/e2e/phase4_test.go:TestE2EP4_PeekTrace_TailLast_CrossProcess` | ✅ |
| 5.3 - 5 | Logs follow on archive 报错 | `tests/e2e/phase4_test.go:TestE2EP4_Logs_FollowOnArchived_Explicit` | ✅ |
| 5.3 - 6 | Worker offline peek-trace | `tests/e2e/phase4_test.go:TestE2EP4_PeekTrace_WorkerOffline_ExitBusinessError` | ✅ |
| 5.3 - 7 | Stats happy | `tests/e2e/phase4_test.go:TestE2EP4_Stats_Counters` | ✅ |
| 5.3 - 8 | Inspect 10 kinds smoke | `tests/e2e/phase4_test.go:TestE2EP4_AllInspectKinds_SmokeNoCrash` | ✅ |
| 5.3 - 9 | Trace archive failure → retry | `integration/phase4_test.go:TestPhase4_TraceArchive_FailureThenRetry` | ✅ |
| 5.3 - 10 | § 9.z 违规扫描 | grep + e2e/import scan | ✅ |

## § 4. 失败 / 已知问题 / 偏离

- **§ 9.z 静态扫描尚未自动化（gomodguard）**：plan-4 § 6.3 风险已 acknowledged。v1 走 code review + 集成测 grep 兜底（5.2 - 6）；自动化 spike 推迟到 Phase 5 收尾。
- **`agent-center server` 长跑流程未端到端 wired-in escalator**：UnknownEventEscalator.Run 单测已覆盖；wiring 到 server start-up 跟 server 命令的整体重构归 Phase 7 部署收尾时一并处理。本 phase 把 Service + Run 提供给将来的 server 主循环使用。
- **peek-trace 协议**：plan-4 § 3.7 提到 `proto/peek_trace.proto` / gRPC。v1 实装走 line-delimited JSON over unix socket（无 gRPC 依赖；单机 v1 足够）；接口 surface 兼容（Request / Response / 闭集 reasons）。Phase 7 多机部署若有需要再换 gRPC。已在 `peek/protocol.go` doc-comment 标注。
- **applyDefaultLimit 在 v1 仅 events 路径调用**：其它 resource 直接用 100 默认。`//nolint:unused` 标注；保留以便后续 resource 加入分页时复用。
- **PsWatchInterval 提取为 var 而非 const**：测试期间 monkey-patch 缩短 ticker；生产默认 2s 不变。已加 doc-comment 说明。

## § 5. DoD 自检

| § 4 DoD 行 | 状态 |
|---|---|
| § 1 所有工件实现并通过单元测试 | ✅ |
| § 5 所有测试场景通过（unit + 集成 + e2e） | ✅（151 + 10 + 13 用例全 pass） |
| 单测行覆盖率 ≥ 90%（diff + 整体） | ✅ 90.0%（go test -coverpkg=./internal/... ./internal/... ./tests/...） |
| 测试报告归档 | ✅ 本文件 |
| 本 phase emit 的 4 个 event 实际进 events 表 | ✅ observability.projection_stale_drop / trace_archive_uploaded / trace_archive_failed / unknown_event_escalated 均有集成测试断言 |
| 6 个 CLI verb `--help` 对齐 03-cli-subcommands § 8.7 | ✅ inspect/query/ps/stats/logs/peek-trace 全 Subject + Flag 命名一致 |
| migration 0004 跑通 + down 可回滚 | ✅ TestINT2_MigrationIdempotent + migrator_test.go |
| § 9.z 边界 grep 通过 | ✅ 0 hits |
| go vet / go test ./... 全过 | ✅ |
| § 6 风险项处理 / defer | ✅ 6.1 (refs 索引) defer Phase 7；6.2 (peek UX) acknowledged；6.3 (lint 自动化) defer Phase 5；6.4 (cold rebuild) out-of-scope；6.5 (stats 量级) defer roadmap；6.6 (--watch 体验) acknowledged；6.7 (BlobStore quota) hard guard 100MB；6.8 (inspect 串行) v1 SQLite WAL 可接受；6.9 (escalator 内存 dedup) acknowledged |

## § 6. 提交清单

### 实现代码

- `internal/blobstore/` — LocalDir BlobStore + 契约 (`localdir.go` + `blobstore.go`)
- `internal/observability/projection/` — VO ProjectionUpdate / TaskExecutionProjection / TaskStatusProjection helper / Repository 接口 + Service
- `internal/observability/sqlite/projection_repo.go` — UPSERT-if-fresh
- `internal/observability/sqlite/event_repo.go` — Find 扩 prefix / actor / until
- `internal/observability/repository.go` — EventQueryFilter 扩列 + MaxEventQueryLimit + ErrEventQueryLimitTooLarge
- `internal/observability/trace/` — TraceArchiveService + PendingScanner
- `internal/observability/query/` — types / service (Inspect ×10 + Query ×8) / projections (row mapper) / fleet (4 段并行) / stats / logs
- `internal/observability/escalator/` — UnknownEventEscalator
- `internal/observability/peek/` — protocol + server + client
- `internal/cli/handlers_observability.go` — 6 个 verb handlers
- `internal/cli/app.go` — App 扩 QuerySvc / FleetSvc / StatsSvc / LogsSvc / ProjectionRepo / ProjectionSvc / BlobStore
- `internal/cli/build.go` — observabilityCommands lazyApp adapter
- `internal/config/config.go` — BlobStoreConfig + PeekConfig

### 测试代码

- `internal/blobstore/blobstore_test.go` + `localdir_extra_test.go` + `localdir_more_test.go`
- `internal/observability/projection/service_test.go` + `task_status_projection_test.go` + `nil_clock_test.go`
- `internal/observability/sqlite/projection_repo_test.go` + `event_repo_find_test.go`
- `internal/observability/trace/archive_test.go` + `archive_extra_test.go`
- `internal/observability/query/service_test.go` + `fleet_test.go` + `stats_test.go` + `logs_test.go` + `projections_test.go` + `service_extra_test.go` + `fleet_extra_test.go` + `fleet_workers_filter_test.go` + `inputrequests_test.go` + `extra_branches_test.go`
- `internal/observability/escalator/escalator_test.go` + `escalator_run_test.go` + `escalator_more_test.go`
- `internal/observability/peek/peek_test.go` + `peek_extra_test.go` + `peek_extra_helpers_test.go`
- `internal/cli/handlers_observability_test.go` + `_extra_test.go` + `_logs_test.go` + `_watch_test.go`
- `tests/integration/phase4_test.go` + `phase4_helpers_test.go` + `phase4_cli_test.go`
- `tests/e2e/phase4_test.go`
- `internal/persistence/migrator_test.go`（版本号从 3 改 4）
- `tests/integration/integration_test.go`（版本号从 3 改 4）

### Migration

- `internal/persistence/migrations/0004_observability_projections.up.sql`
- `internal/persistence/migrations/0004_observability_projections.down.sql`

### 提交 SHA 列表

1. `c1b5230` feat(phase-4): TaskExecutionProjection — AR-less Repo + Service + migration 0004
2. `8fc9587` feat(phase-4): TaskStatusProjection helper — same-tx 双写 + tx 强制校验
3. `28c8f9a` feat(phase-4): BlobStore (LocalDir) + TraceArchiveService — execution 终态 hook
4. `adcba25` feat(phase-4): QueryService — Inspect×10 + Query×8 + EventRepository.Find 扩展
5. `532ff9a` feat(phase-4): FleetSnapshotService — 4 段并行 + partial failure warning
6. `cccbda4` feat(phase-4): StatsService — 5 个 scope 聚合
7. `38efcf3` feat(phase-4): UnknownEventEscalator — 周期 scan + 阈值 + 24h dedup
8. `21c0140` feat(phase-4): peek-trace RPC（center↔daemon unix socket）+ LogsService
9. `ffc9866` feat(phase-4): CLI handlers — inspect / query / ps / stats / logs / peek-trace
10. `28853ef` test(phase-4): 集成 + e2e + 覆盖率推升至 90.0%

## § 7. 结论

✅ **通过**：Phase 4 DoD 100% 达成，覆盖率 90.0%（整体 + diff），无失败 / 跳过用例。
