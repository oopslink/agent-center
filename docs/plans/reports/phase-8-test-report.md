> ⚠ **v2 P8 Foundation 阶段测试报告**

# Phase 8 测试报告

> 完成日期：2026-05-23 · 提交范围：`eafec87..6d39ee8` (7 commits)

## § 1. 覆盖率汇总

| 维度 | 数值 | 是否达标（≥ 90%） |
|---|---|---|
| **P8 新增包整体行覆盖率** | **83.0%** | ⚠ 部分包未达 |
| Workforce / SecretManagement 新增包平均 | 79.6% | ⚠ |
| Cognition (P8 仅加 agent_instance_id 字段) | 98.7% | ✅ |

**按包细分**:

| 包 | 行覆盖率 | 评估 |
|---|---|---|
| `internal/workforce` | 81.5% | 主要漏覆盖：`buildCapabilityList` 一些 dedup 分支 / `Worker.CapabilitiesJSON` empty path |
| `internal/workforce/service` | 83.0% | 漏覆盖：少量 tx 错误回滚路径 |
| `internal/workforce/sqlite` | 77.7% | 漏覆盖：scan helper 错误路径 / `cassDiagnose` 罕见分支 |
| `internal/secretmgmt` | 82.6% | 漏覆盖：`LoadMasterKey` 部分错误路径 |
| `internal/secretmgmt/service` | 77.6% | 漏覆盖：tx 失败回滚 / `Zero plaintext on error` 路径 |
| `internal/secretmgmt/sqlite` | 74.8% | 漏覆盖：`diagnoseUserSecretUpdate` 不同分支 / scan 错误路径 |
| `internal/persistence` (migrator + v2 migrations) | 88.2% | 漏覆盖：少量 tx 失败路径 |

### 未达 90% 项目的处置

P8 plan DoD 要求「单测行覆盖率 ≥ 90%」。当前 P8 新增包平均 83%。差距 主要来自:

1. **SQLite repo 的 scan 错误路径**（74-78%）：scan 函数中 `time.Parse` 错误 / sql 错误的边界路径——需要构造异常 DB 行（事务失败 / 故意脏数据）触发，工程成本高。**建议**：作为 P8 follow-up 补；或在 P12 e2e 阶段统一捕获。
2. **tx 失败回滚分支**（service 77-83%）：依赖 mock 持久层注入故障；当前 setup 用真实 SQLite。**建议**：加 ErroringRepo mock，专测 Service 层错误传播。

**P8 当前状态**：所有 happy path + 主要 error path（version conflict / not found / 状态不变量 / 加密失败 / 明文不泄露 等）已覆盖；剩余 gap 是 marginal error path。

## § 2. 测试场景执行结果

### 2.1 单测（per 工作项）

| 工件 | 用例数 | pass / fail | 备注 |
|---|---|---|---|
| `Worker AR` (v2 扩字段 + Capabilities/Concurrency/Discovery getters) | 17 | 17/0 | TestWorker_* in worker_test.go |
| `WorkerRepository.UpdateConfig + UpdateCapabilities + ReplaceCapabilities` | 8 | 8/0 | concurrency only / discovery only / version conflict / not found / no fields / replace verbatim |
| `BootstrapToken AR` (state machine + hash + 4 transitions) | 9 | 9/0 | TestBootstrapToken_* in bootstrap_token_test.go |
| `BootstrapTokenRepository` (Save / Find variants / UpdateStatus / FindExpired) | 11 | 11/0 | 含 unique constraint disambiguation by SQLite column name |
| `BootstrapTokenService` (Issue + Reissue + Revoke + ScanExpired) | 11 | 11/0 | 含 concurrent race 测试 |
| `WorkerEnrollService.Exchange` (v2 exchange-based) | 10 | 10/0 | 6 失败路径 + 1 happy + 3 边界 |
| `WorkerConfigService` (SetConfig + SetCapabilityEnabled) | 9 | 9/0 | concurrency/discovery/both/no-fields/version/not-found/bad-actor + toggle |
| `AgentInstance AR` (4 状态 + builtin invariants) | 14 | 14/0 | TestAgentInstance_* in agent_instance_test.go |
| `AgentInstanceRepository` (Save/Find/UpdateState/Archive/Bulk + 4 分支) | 10 | 10/0 | Archive built-in 拒 + state 非 idle 拒 + version conflict |
| `AgentInstanceManagementService` (Create/UpdateConfig/Archive/EnsureBuiltin) | 8 | 8/0 | dup name / builtin archive 拒 / EnsureBuiltin idempotent |
| `AgentInstanceLifecycleService` (4 state transitions) | 4 | 4/0 | OnExecutionStarted/Ended/WorkerOffline/WorkerOnline |
| `SupervisorInvocation.AgentInstanceID` (新字段 propagate) | 3 | 3/0 | Spawn + Rehydrate + repo roundtrip |
| `UserSecret AR` (Rotate/Revoke + validations) | 11 | 11/0 | TestUserSecret_* in user_secret_test.go |
| `MasterKey + Crypto` (AES-GCM + load file) | 10 | 10/0 | tamper + wrong-key + nonce + perms + file missing |
| `UserSecretRepository` (Save/Find/UpdateValue/UpdateState + revoked guard) | 9 | 9/0 | name unique + version conflict + revoked rejected |
| `UserSecretService` (Create + Rotate + Revoke; **no plaintext leak** assertions) | 6 | 6/0 | 验证 events payload 不含明文（substring scan） |
| `SecretResolutionService` (Resolve + denial path) | 5 | 5/0 | revoked → access_denied event in separate tx + plaintext zero on error |
| `SecretRef VO` (Parse + IsSecretRefValue + 8 invalid 形式) | 6 | 6/0 | scheme/name 边界 / unsupported scheme 拒 |

**总单测数**：151 cases；全部 PASS；0 fail；0 flaky。

### 2.2 集成测试

| 场景 | 涉及工件 | 关键断言 |
|---|---|---|
| v2 migration up→down→up 全 cycle | `internal/persistence.Migrator` | TestMigrator_DownReverts / TestMigrator_DownIdempotent / TestMigrator_VersionTracksApplied @ version=12 |
| v2 新表 / 新列存在 | `internal/persistence.Migrator` | TestMigrator_UpCreatesV2Tables: 3 new tables (bootstrap_tokens / agent_instances / user_secrets) + 5 new columns (workers.{concurrency_json, discovery_json, capabilities_json}, task_executions.agent_instance_id, supervisor_invocations.agent_instance_id) + 旧 `capabilities` 列已 drop |
| Tx 边界 + repo CAS race | `WorkerRepo.UpdateConfig / BootstrapTokenRepo.UpdateStatus / AgentInstanceRepo.UpdateState / UserSecretRepo.UpdateValue/UpdateState` | 所有 CAS 路径有 version mismatch + not found 双分支测试 |
| 跨表 tx atomicity (v1 已验证；P8 沿用) | `persistence.RunInTx` | 仍绿（无回归）|

### 2.3 e2e 测试

P8 本身**无新 e2e 用例**——纯基础设施层 + service 层；端到端 v2 workflow 在 P11/P12 e2e suite 中体现（per phase-12 § 3.1 列 8 跨 phase 场景，含「冷启动用户旅程：identity add → worker join → agent create → secret create → channel create → ...」）。

v1 e2e suite 全部 PASS（28 cases，无 regression）：
- TestE2E1_* / TestE2EP2_E* / TestE2EP4_* 等
- TestE2E1_WorkerEnrollListStatus 仍走 v1 Enroll 路径（v2 Exchange 路径有独立 service test）

## § 3. 跟测试计划（phase-8 § 5）的对位

| § 5 行 / DoD 项 | 实际用例文件:函数 | 状态 |
|---|---|---|
| § 4 ☑ § 3.0 migrations up/down | `internal/persistence/migrator_test.go::TestMigrator_*` | ✅ |
| § 4 ☑ § 3.1-3.8 工件单测 + 集成 | 见 § 2.1 表 | ✅ |
| § 4 ☑ **单测行覆盖率 ≥ 90%** | per § 1 表 | ⚠ **83.0% < 90%**（gap 14% / 主要 SQLite scan + tx 错误路径）|
| § 4 ☑ 所有新事件实际进 events 表 | TestBootstrapTokenService_Issue_Happy 验证 events 表 / TestExchange_Happy 验证 3 个事件 / TestAIMgmt_EventChain 验证 created+config_updated+archived / TestUserSecretService_Create_Happy + Resolver_Happy 验证 events table | ✅ |
| § 4 ☑ CLI 命令 `--help` 跟 ADR 对齐 | **defer to P9 CLI handlers**（P8 仅 service 层；CLI 层在 P9 整合到 agent-center CLI）| 🟡 |
| § 4 ☑ master_key 不入 DB / event / trace | TestUserSecretService_Create_Happy: substring scan asserts no "TestPlaintext" in events; ciphertext != plaintext | ✅ |
| § 4 ☑ AgentInstance built-in auto-provision idempotent | TestAIMgmt_EnsureBuiltinSupervisor_Idempotent | ✅ |
| § 4 ☑ e2e workflow `agent-center join` → `agent create` → `secret create` → `secret rotate` | **defer to P11/P12 e2e suite**（无 CLI = 无端到端命令链路；service 层等价测试已覆盖）| 🟡 |
| § 4 ☑ go vet / go test ./... 全过 | `go test ./...` exit 0 / `go vet ./...` no output | ✅ |
| § 4 ☑ 测试报告归档 | 本文档 | ✅ |
| § 4 ☑ § 6 风险项处理或显式 defer | per § 5 风险节 | ✅ |

## § 4. 失败 / 已知问题

无失败。已知 follow-up：

- **F1**: P8 新增包行覆盖率 83% 未达 90% DoD（差 7%）；gap 主要在 SQLite scan 错误路径 + tx 失败回滚分支。**处置**：作为 P9 准备工作前的 catch-up 补充，或在 P12 e2e 阶段统一补；**用户决策点**：是否阻塞 P9 启动？
- **F2**: P8 § 3.4 长连接 push (worker daemon-side handler) defer 到 P9：plan 原是 P8 部分，但 daemon RPC 重构基础在 P9；本 phase 只交付 center-side service + event emission。
- **F3**: CLI handlers (agent create / secret create / worker token issue 等) defer 到 P9/P11；P8 只交付 service 层。
- **F4**: SupervisorInvocation.agent_instance_id 字段已加（nullable）但 InvocationFactory caller 还没强制填入；P9 配套 dispatch 重写时完成。

## § 5. DoD 自检

| § 4 DoD 行 | 状态 | 说明 |
|---|---|---|
| § 3.0 所有 migration up / down 跑过且单测验证 schema | ✅ | TestMigrator_UpCreatesV2Tables + DownReverts |
| § 3.1 - 3.8 所有工件单测 + 集成测通过 | ✅ | 151 cases all PASS |
| **单测行覆盖率 ≥ 90%（diff + 整体）** | ⚠ **未达** | P8 包 83%，详 § 1 + § 4 F1 |
| 所有新事件实际进 events 表 | ✅ | 多个 service test 验证 |
| CLI 命令 `--help` 跟 ADR 对齐 | 🟡 defer | CLI handlers P9 集成 |
| master_key 不入 DB / event / trace | ✅ | 多个 plaintext-leak 断言 |
| AgentInstance built-in auto-provision idempotent | ✅ | TestAIMgmt_EnsureBuiltinSupervisor_Idempotent |
| e2e: 完整链路 `worker token issue → join → agent create → secret create → rotate` | 🟡 defer | service 层等价；CLI e2e 在 P11/P12 |
| `go vet` / `go test ./...` / lint 全过 | ✅ | exit 0 全绿 |
| 测试报告归档 | ✅ | 本文档 |
| § 7 风险项处理或显式 defer | ✅ | 详 § 4 F1-F4 |

## § 6. 提交清单

| Commit | 范围 | 测试数 |
|---|---|---|
| `eafec87` § 3.0+3.1 | 6 migrations + Worker AR v2 扩字段 + Repo v2 methods | 19 |
| `c0f6373` § 3.2 | BootstrapToken Entity + Repo + Service | 31 |
| `462f3a3` § 3.3 | WorkerEnrollService.Exchange | 10 |
| `3d6e4dc` § 3.4 | WorkerConfigService | 9 |
| `80ee406` § 3.5 | AgentInstance AR + Repo + Mgmt + Lifecycle services | 32 |
| `f5c6a86` § 3.6 | SupervisorInvocation.AgentInstanceID | 5 |
| `6d39ee8` § 3.7+3.8 | SecretManagement BC8 + SecretRef VO | 45 |

实现代码：约 6,500 行（含测试）跨 30 个新文件
工件：
- 5 新 AR (BootstrapToken / AgentInstance / UserSecret + Worker v2 字段扩展 + SupervisorInvocation v2 字段)
- 5 新 Service (BootstrapTokenService / Exchange (v2 WorkerEnroll) / WorkerConfigService / AgentInstanceManagementService / AgentInstanceLifecycleService / UserSecretService / SecretResolutionService)
- 5 新 Repository (BootstrapToken / AgentInstance / UserSecret + Worker extended + SupervisorInvocation extended)
- 6 v2 migrations (0007-0012)
- 1 新 BC8 (SecretManagement) — first BC added since v1 baseline

## § 7. 下一步

按 plan § 7：**Phase 9 Agent Runtime 扩展** 可启动，前提是 F1 (90% coverage gap) 由用户裁定:
- 选项 A: 接受 83% + 进入 P9
- 选项 B: 先补 SQLite scan 错误路径 / tx 失败回滚测试到 90% 再进 P9（预计 2-3 小时）

P9 上游依赖（来自 P8）全部就绪：
- BootstrapToken Issue/Exchange (worker enroll lightweight) ✅
- AgentInstance AR + Lifecycle (G2/G3/G4/G5 都基于此) ✅
- SecretManagement (G4 MCP secret 注入需要) ✅
- SecretRef VO (mcp_config 解析 in P9) ✅
