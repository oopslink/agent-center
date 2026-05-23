# Phase 8: Foundation（v2 基础设施）

> DDD Workforce BC 扩 + 新 SecretManagement BC · 依赖 v1 Phase 1-6 · 解锁 Phase 9 / 10
> 纪律：按里程碑顺序 / 模块完备不半成品 / TDD + 单测 ≥ 90% + 集成 + e2e + 测试报告

覆盖 ADR：[ADR-0023 Worker Enroll 轻量化](../design/decisions/drafts/0023-worker-enroll-lightweight.md) · [ADR-0024 AgentInstance 一等公民化](../design/decisions/drafts/0024-agent-instance-first-class.md) · [ADR-0026 SecretManagement BC](../design/decisions/drafts/0026-user-secret-management-bc.md) · [ADR-0029 Supervisor as Built-in AgentInstance](../design/decisions/drafts/0029-supervisor-as-builtin-agent-instance.md)

## § 0. 目标

为 v2 后续 phase 提供地基：

- **Worker enroll 简化为一行命令**（用户视角：`agent-center join <center-url> --token=...` 接入）
- **AgentInstance 升一等公民**（用户视角：用户能 `agent create` 命名 + 持久配置 + skill / instructions / mcp_config home dir）
- **Built-in Supervisor 跟普通 AgentInstance 统一**（运维视角：`agent list` 列出 supervisor 跟 worker agent 同界面）
- **用户密钥中心化管理**（用户视角：`secret create / rotate / revoke` —— MCP token / 云凭据等不再散落配置文件；AES-GCM 加密 + master_key 文件）

**DDD 意义**：Workforce BC 大幅扩；新建 BC8 SecretManagement (Supporting Domain)；为 Phase 9 Agent runtime / Phase 10 Conversation v2 准备稳定的 actor / config / secret 模型。

---

## § 1. DDD 工件清单

### 1.1 Aggregate Roots

| AR | BC | 来源 ADR | 备注 |
|---|---|---|---|
| **AgentInstance**（新）| Workforce | 0024 + 0029 | id ULID / name 全局唯一 / agent_cli / worker_id (NULLABLE iff is_builtin) / config JSON / max_concurrent? / state idle/active/sleeping/archived / is_builtin / version |
| **UserSecret**（新）| SecretManagement (BC8) | 0026 | id ULID / name 全局唯一 / kind enum / value_ciphertext + nonce / state active/revoked / 各 timestamps |
| **Worker**（扩字段）| Workforce | 0023 | 加 concurrency / discovery / capabilities 字段（v1 Worker AR 仍然，本 phase schema migrate）|

### 1.2 Entities（子从属）

| Entity | 从属 | 来源 ADR | 备注 |
|---|---|---|---|
| **BootstrapToken**（新）| Worker | 0023 | id ULID / worker_id / value_hash / status active/used/expired/revoked / TTL / 各 timestamps |

### 1.3 Value Objects

| VO | 用在 | 来源 ADR |
|---|---|---|
| **SessionToken** | Worker enroll 认证 | 0023（语义保留；落 worker 本地 credential 文件）|
| **WorkerConfig** | Worker AR 行为配置 | 0023（concurrency + discovery + capabilities-enabled）|
| **Capability** | Worker.capabilities 元素 | 0023 + 0030（含 supports_mcp / supports_skills / supports_session feature flags —— 但本 phase 仅 supports_session；其余 flag 在 P9）|
| **AgentInstanceConfig** | AgentInstance.config | 0024（含 instructions_ref? / mcp_config? / skills? 等留口；P9 填）|
| **AgentInstanceState** | AgentInstance.state | 0024（idle / active / sleeping / archived）|
| **SecretKind** | UserSecret.kind | 0026（mcp / cloud_credential / repo_deploy_key / other）|
| **SecretRef** | mcp_config 等字段内嵌引用语法 | 0026（`secret:<name>`；本 phase 仅引入 VO 类型 + 解析 service；具体使用方 P9）|
| **EncryptedValue** | UserSecret value_ciphertext + value_nonce | 0026（AES-GCM 加密产物）|
| **MasterKey** | center 启动加载 | 0026（不入 DB）|

### 1.4 Repositories

```go
// 已有（v1）：WorkerRepository / WorkerProjectMappingRepository / WorkerProjectProposalRepository / ProjectRepository

// 本 phase 扩 / 加：

type WorkerRepository interface {
    // v1 已有 + 加 v2 方法：
    UpdateConfig(ctx, id, fields WorkerConfigFields, version int) error
    UpdateCapabilities(ctx, id, detected []Capability) error
}

type BootstrapTokenRepository interface {
    FindByID(ctx, id) (*BootstrapToken, error)
    FindByValueHash(ctx, hash) (*BootstrapToken, error)
    FindByWorkerID(ctx, workerID, statuses ...) ([]*BootstrapToken, error)
    FindActiveByWorkerForUpdate(ctx, workerID) (*BootstrapToken, error)
    Save(ctx, t) error
    UpdateStatus(ctx, id, from, to, at) error
    FindExpired(ctx, before) ([]*BootstrapToken, error)
}

type AgentInstanceRepository interface {
    FindByID(ctx, id) (*AgentInstance, error)
    FindByName(ctx, name) (*AgentInstance, error)
    FindByWorkerID(ctx, workerID, states ...) ([]*AgentInstance, error)
    FindAll(ctx, filter) ([]*AgentInstance, error)
    Save(ctx, a) error
    UpdateState(ctx, id, from, to, version) error
    UpdateConfig(ctx, id, fields AgentInstanceConfigFields, version) error
    CountActiveExecutions(ctx, id) (int, error)
    BulkUpdateStateByWorker(ctx, workerID, from, to) (count int, err)
}

// 新 BC8 SecretManagement:

type UserSecretRepository interface {
    FindByID(ctx, id) (*UserSecret, error)
    FindByName(ctx, name) (*UserSecret, error)
    FindAll(ctx, filter) ([]*UserSecret, error)
    Save(ctx, s) error
    UpdateState(ctx, id, from, to, version) error
    UpdateValue(ctx, id, ciphertext, nonce, version) error
    UpdateLastUsedAt(ctx, id, at) error
}
```

### 1.5 Domain Services

| Service | 在哪 | 来源 ADR | 职责 |
|---|---|---|---|
| **WorkerEnrollService**（重写）| Workforce | 0023 | `ExchangeRequest { token_value, worker_id } → ExchangeResponse { session_token }`；token 校验 + Worker 入库（首次）+ emit 事件 |
| **BootstrapTokenService**（新）| Workforce | 0023 | issue / reissue（FOR UPDATE 锁）/ revoke / expire-scan；明文仅 issue 返回 |
| **WorkerConfigService**（新）| Workforce | 0023 | SetConfig + 通过 worker 长连接 push `worker.config.updated` |
| **AgentInstanceManagementService**（新）| Workforce | 0024 + 0029 | create（builtin / non-builtin 区分）/ update config / archive（builtin 拒）|
| **AgentInstanceLifecycleService**（新）| Workforce | 0024 | 订阅 task_execution.* / worker.* 事件，自动 state 转移（active/idle/sleeping/awakened）|
| **UserSecretService**（新）| SecretManagement | 0026 | Create / Rotate / Revoke；AES-GCM 加密发生在 service 内 |
| **SecretResolutionService**（新）| SecretManagement | 0026 | worker daemon caller；FOR UPDATE 查 + worker_id 一致性校验 + AES-GCM 解密；emit user_secret.accessed |

### 1.6 Application Services（CLI handler 层）

```
agent-center join <center-endpoint> --token=<t> --worker-id=<id>
agent-center worker token issue --worker-id=<id>
agent-center worker token reissue --worker-id=<id>
agent-center worker token revoke --token-id=<id>
agent-center worker token list --worker-id=<id>
agent-center worker config set <id> <key>=<value>
agent-center worker capability enable / disable <id> <agent_cli>
agent-center worker remove <id>

agent-center agent create --name=<n> --worker=<id> --agent-cli=<cli> [--max-concurrent=<n>]
agent-center agent list [--worker=<id>] [--state=<s>] [--builtin=<bool>]
agent-center agent show <name>
agent-center agent config set <name> <key>=<value>
agent-center agent archive <name>

agent-center secret create --name=<n> --kind=<k>
agent-center secret list [--kind=<k>] [--state=<s>]
agent-center secret rotate <name>
agent-center secret revoke <name> [--reason=<r>]
agent-center secret usage <name>
```

### 1.7 Domain Events

| 事件 | 来源 ADR | payload 关键字段 |
|---|---|---|
| `worker.bootstrap_token.issued / used / expired / reissued / revoked` | 0023 | token_id, worker_id, ... |
| `worker.enrolled`（语义微调）| 0023 | worker_id |
| `worker.config.updated` | 0023 | worker_id, changed_fields, by |
| `worker.capability.detected` | 0023 | worker_id, capabilities |
| `agent_instance.created / config_updated / activated / idle / sleeping / awakened / archived` | 0024 + 0029 | id, ... |
| `user_secret.created / rotated / revoked / accessed / access_denied` | 0026 | id (or name), by, ... |

### 1.8 Context Map 关系

- **本 phase 内新 BC8 SecretManagement** ← **Workforce**（Customer-Supplier）：worker daemon 调 SecretResolutionService.resolve 拿明文（per AgentInstance.id）
- **Workforce ↔ TaskRuntime**（Shared Kernel；扩）：TaskExecution.agent_instance_id 字段引用（Phase 9 落 dispatch envelope；本 phase schema 加字段）
- **Workforce ↔ Cognition**（Shared Kernel；扩）：SupervisorInvocation 加 agent_instance_id ref → built-in supervisor AgentInstance

---

## § 2. 上游依赖（来自 v1 Phase 1-6）

| 上游工件 | 用在本 phase 哪步 |
|---|---|
| Worker AR + WorkerRepository (v1 Phase 1) | 扩字段 + 加 method（本 phase 3.1）|
| TaskExecution AR (v1 Phase 2) | schema 加 `agent_instance_id` 字段（本 phase 3.5 schema migration 但实际 dispatch 链路 P9 走通）|
| SupervisorInvocation AR (v1 Phase 6) | schema 加 `agent_instance_id` ref（本 phase 3.6）|
| events 表 + EventSink (v1 Phase 1) | 所有新事件 emit 走 v1 已有总线 |
| SQLite + migration framework (v1 Phase 1) | 加 v2 migration 文件（本 phase 3.0）|

## § 3. 工作项分解

### 3.0 DB Schema Migration（先做）

| Migration 文件 | 内容 |
|---|---|
| `0010_v2_worker_config_fields.up.sql` | Worker 表加 concurrency_json / discovery_json / capabilities_json 列 |
| `0011_v2_bootstrap_tokens.up.sql` | 新 bootstrap_tokens 表 |
| `0012_v2_agent_instances.up.sql` | 新 agent_instances 表 (含 is_builtin + CHECK constraint) |
| `0013_v2_task_executions_agent_instance.up.sql` | task_executions 加 agent_instance_id 列（替代 agent_cli；本 phase 仅加列，dispatch 路径 P9 改）|
| `0014_v2_supervisor_invocations_agent_instance.up.sql` | supervisor_invocations 加 agent_instance_id 列 |
| `0015_v2_user_secrets.up.sql` | 新 user_secrets 表（BC8）|
| 对应 `*.down.sql` | reverse migrations |

每个 .up.sql 配 .down.sql；migration 跟 v1 数据库不考虑向后兼容（user 已表态），直接 schema 改。

**DoD**：migration up 跟 down 都跑过 + 单测 + 集成测验证表结构。

### 3.1 Worker AR 扩字段 + WorkerRepository v2 methods

- **工件**：`internal/workforce/worker.go` + `internal/workforce/worker_repository.go`
- **依赖**：3.0 migration up
- **输出**：Worker AR 字段含 concurrency / discovery / capabilities；`UpdateConfig` + `UpdateCapabilities` 方法
- **步骤**：
  1. 写 fail test for `UpdateConfig`（涵盖正常 + 乐观锁冲突）
  2. 实现 method
  3. 写 fail test for `UpdateCapabilities`（capability 数组 upsert）
  4. 实现
  5. 集成测验证从 DB read 回的 Worker 字段
- **DoD**：单测 100% method coverage；集成测 read/write 一致

### 3.2 BootstrapToken Entity + BootstrapTokenRepository + Service

- **工件**：`internal/workforce/bootstrap_token.go` / `_repository.go` / `_service.go`
- **依赖**：3.0
- **步骤**：
  1. 写 fail tests for state machine transitions (active/used/expired/revoked) + reissue 拒绝 used + 并发 reissue 用 FOR UPDATE
  2. 实现 Entity + Repository
  3. 实现 BootstrapTokenService.Issue / Reissue / Revoke / ScanExpired
  4. AES-N hash for value 存 DB；明文仅 Issue 返回
  5. emit `worker.bootstrap_token.*` 事件
  6. 集成测：跨事务 reissue 验证 active count constraint；扫 expired
- **DoD**：所有状态转移单测覆盖；reissue 并发 race condition test（goroutine）；明文不入 DB（grep 测）

### 3.3 WorkerEnrollService 重写（exchange-based）

- **工件**：`internal/workforce/worker_enroll_service.go`
- **依赖**：3.1 + 3.2
- **步骤**：
  1. 写 fail test for ExchangeRequest 校验链（token active / token expired / worker_id mismatch / 等）
  2. 实现 ExchangeService.Exchange
  3. emit `worker.bootstrap_token.used` + `worker.enrolled`
  4. 集成测：完整 issue → join 流程
- **DoD**：6 个失败路径 + 1 成功路径单测；集成测端到端 ok

### 3.4 WorkerConfigService（含长连接 push）

- **工件**：`internal/workforce/worker_config_service.go` + worker daemon 长连接客户端补
- **依赖**：3.1
- **步骤**：
  1. 实现 SetConfig + 写 Worker.config 字段
  2. 长连接 push `worker.config.updated` event 到 in-line worker
  3. worker 端 handler 重拉 config（in-memory cache 覆盖）
  4. 集成测：模拟 worker 长连接收 event
- **DoD**：center push + worker receive + cache update 全链路通

### 3.5 AgentInstance AR + Repository + ManagementService + LifecycleService

- **工件**：`internal/workforce/agent_instance.go` 等系列
- **依赖**：3.0 (table) + 3.4 (capabilities for dispatch validation)
- **步骤**：
  1. 实现 AR + Repository（含 `BulkUpdateStateByWorker` 批量）
  2. 实现 ManagementService.Create / UpdateConfig / Archive（含 is_builtin 校验：created 时不开放给 CLI；archive built-in 拒）
  3. 实现 LifecycleService 订阅 task_execution.* / worker.* 事件做 state 转移
  4. 实现 Auto-provision built-in supervisor（center 启动时检查 + INSERT 不存在则建）
  5. CLI handlers（agent create / list / show / config-set / archive）
  6. 集成测：worker offline → sleeping → online → idle 转移完整
- **DoD**：8 状态转移单测；built-in auto-provision idempotent；archive built-in 返回 ErrIsBuiltinAgentInstance；CLI help 跟 ADR-0024 § 10 对齐

### 3.6 SupervisorInvocation schema 加 agent_instance_id

- **工件**：`internal/cognition/supervisor_invocation.go` （v1 已有，加字段）
- **依赖**：3.5（built-in supervisor AgentInstance 必须存在）
- **步骤**：
  1. SupervisorInvocation struct 加 agent_instance_id 字段
  2. InvocationFactory 自动填入 built-in supervisor id
  3. 集成测：每条 invocation 引用 valid agent_instance
- **DoD**：所有新 invocation 行 agent_instance_id 非 NULL；旧 invocation 行 migration 时 backfill to built-in supervisor id

### 3.7 SecretManagement BC8（UserSecret AR + 全套）

- **工件**：`internal/secretmgmt/user_secret.go` 系列；新包
- **依赖**：3.0 (table) + master_key_file 配置
- **步骤**：
  1. AR + Repository + Service（Create / Rotate / Revoke）
  2. AES-GCM 加密：crypto/aes + crypto/cipher；master key 从 `secret_management.master_key_file` 加载（启动时 once）
  3. SecretResolutionService（worker daemon 调）：FOR UPDATE 查 + worker_id 一致性 + 解密
  4. CLI handlers（secret create + 交互 stdin / pinentry / list / rotate / revoke / usage）
  5. CLI 不打印明文；create 后仅显示「Reference as: secret:<name>」
  6. emit `user_secret.*` 事件（payload 不含明文）
  7. 集成测：create → resolve → 验证返回明文等输入；rotate → 新值生效；revoke → resolve 拒
- **DoD**：明文不入 DB（grep 测 ciphertext 列 = base64/binary 不可读）；resolve 跨 actor 校验（错 worker_id resolve 失败）；events payload 不含明文（assert）；CLI 不打印明文（test stdout）

### 3.8 SecretRef VO + 引用语法解析（基础）

- **工件**：`internal/secretmgmt/secret_ref.go`
- **依赖**：3.7
- **步骤**：
  1. SecretRef VO 类型 + Parse(string) (SecretRef, error)（识别 `secret:<name>` pattern）
  2. 单测：valid / invalid format
- **DoD**：Parser 单测 100%；本 phase 不实装 mcp_config 解析（在 P9 G4）

### 3.9 conventions / config / docs sync

- `docs/design/implementation/04-configuration.md` 已 ADR-0023/0026 update；本 phase 验证准确
- `docs/design/architecture/tactical/workforce/` 文档已 ADR 阶段 update；本 phase 验证
- `docs/design/architecture/tactical/secret-management/` 已 ADR 阶段建；本 phase 验证

---

## § 4. Definition of Done

- [ ] § 3.0 所有 migration up / down 跑过且单测验证 schema
- [ ] § 3.1 - 3.8 所有工件单测 + 集成测通过
- [ ] 单测行覆盖率 ≥ 90%（diff + 整体；以 `go test -cover` 为准）
- [ ] 所有新事件实际进 events 表（集成测验证）
- [ ] CLI 命令 `--help` 跟 ADR 设计对齐
- [ ] master_key 不入 DB / event / trace（grep 测）
- [ ] AgentInstance built-in auto-provision idempotent（启动多次只一行）
- [ ] e2e: `agent-center worker token issue` → `agent-center join` 一行接入 → `agent-center agent create` → `agent-center secret create` → `agent-center secret rotate` 全链路通
- [ ] `go vet` / `go test ./...` / lint 全过
- [ ] 测试报告 `docs/plans/reports/phase-8-test-report.md` 归档（§ 5 各场景 1:1 对位）
- [ ] § 6 风险项处理或显式 defer

## § 5. 测试计划

### 5.1 单测场景

| 工件 | 测试场景 | 关键断言 |
|---|---|---|
| BootstrapToken state machine | 6 状态转移路径 + reissue used 拒绝 + 并发 reissue race | 状态 CAS + ErrTokenAlreadyUsed |
| WorkerEnrollService.Exchange | token active/expired/revoked/used + worker_id mismatch | ErrTokenInvalid / ErrTokenWorkerMismatch |
| WorkerConfigService.SetConfig | 单字段改 / 多字段改 / 乐观锁冲突 | Worker.config 字段更新 + version bump |
| AgentInstanceManagementService.Create | name 冲突 / worker 不存在 / agent_cli not in capabilities | ErrAgentInstanceNameTaken / ErrCapabilityMissing |
| AgentInstanceManagementService.Archive | built-in 拒 + state=active 拒 + state=idle 接受 | ErrBuiltinNotArchivable / ErrNotIdle |
| AgentInstanceLifecycleService | idle→active / active→idle / sleeping 联动 | state 转移 emit event |
| UserSecretService.Create | name 唯一 + AES-GCM 加密 + 明文不入 DB | ciphertext.length > 0 / plaintext_lookup_returns_nil |
| SecretResolutionService.Resolve | active resolve / revoked 拒 / worker_id 不匹配拒 | ErrSecretAccessDenied |
| SecretRef.Parse | valid `secret:foo` + invalid `bar:foo` / 空 | 返回 ref or err |

### 5.2 集成测试场景

| 场景 | 涉及工件 | 关键断言 |
|---|---|---|
| Worker enroll 全链路 | issue token → join → exchange → enroll | session_token 写本地 credentials；worker 行入 DB |
| BootstrapToken 并发 reissue | 2 goroutine 同时 reissue | 只一个成功；另一个 ErrTokenAlreadyActiveExists |
| Built-in supervisor auto-provision | center 启动多次 | agent_instances 表恒一行 (name='supervisor', is_builtin=true) |
| WorkerConfig 长连接 push | center SetConfig + worker 端 receive | worker 端 cache 更新 |
| Secret resolve worker_id 校验 | worker A 试 resolve worker B 的 agent 的 secret | ErrSecretAccessDenied + emit user_secret.access_denied |
| AgentInstance state worker 联动 | worker offline → 所有 agent → sleeping | BulkUpdateStateByWorker 一次完成 |

### 5.3 e2e 测试场景

| 场景 | 入口 CLI | 关键断言 |
|---|---|---|
| 从零启动到 worker 接入 | `agent-center server start` → `agent-center worker token issue` → `agent-center join` | join 成功；worker.status=online |
| 用户创建 agent | `agent-center agent create --name=coder-mbp --worker=W1 --agent-cli=claude-code` | agent_instances 行 + identity 行 (agent:<id>) 同事务 INSERT |
| 用户创建 secret | `agent-center secret create --name=github-pat --kind=mcp` | stdin 输入；DB 入密文；CLI 不打印明文 |
| Secret rotate + 验证生效 | create → rotate → resolve | resolve 返回新值 |
| Archive built-in supervisor 拒绝 | `agent-center agent archive supervisor` | exit code 1 + 引导错误信息 |

## § 6. 风险 / Spike 项

| 风险 | 缓解 |
|---|---|
| AES-GCM master_key file lost = 所有 secret 不可解 | 文档明确告知（运维责任）；启动时如果 master_key 不存在 fail-fast 报警；备份留运维 |
| Built-in supervisor auto-provision 跟 v1 数据库存在的旧 supervisor_invocation 行不匹配 | Migration `0014_v2_supervisor_invocations_agent_instance.up.sql` 提供 backfill SQL；如果 v1 数据库无 supervisor agent_instance 则 INSERT 一行 + backfill 旧 invocation rows 引用之 |
| 跨 worker reissue race（worker A 跑的同时 reissue token）| FOR UPDATE 保证；测试覆盖（5.2 行 2）|
| 长连接 push `worker.config.updated` worker 不在线 | 不推；offline worker 重连时 reconcile 自动拉新 config（这条 reconcile 跟 P10 task-runtime 改动相关，本 phase 仅 worker 在线 push 路径）|

## § 7. 下游解锁

本 phase 完成 → **Phase 9 启动**（按 Q2 phase 间严格串行原则，per [plans/README § 2.1 顺序](README.md)）。Phase 10 / 11 依次串行（P9 完 → P10 → P11 → P12）。

**提供给下游的接口 surface**（P9 / P10 / P11 都会用）：

- AgentInstance.id (ULID) ← Identity[kind=agent].id reference (P10 用)
- AgentInstance.config.mcp_config 字段（本 phase 仅留口；P9 实装解析 + secret 替换）
- SecretRef Parse 函数（本 phase 提供；P9 在 mcp_config.json 拼装时使用）
- TaskExecution.agent_instance_id 列（本 phase 仅加列；P9 dispatch envelope + worker side 改 join）
- BootstrapToken / Worker config / Auto-provision built-in supervisor 等运行时基础设施
