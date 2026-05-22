# Workforce BC — DDD 战术设计 Overview

> **DDD 战术层** · BC: Workforce
>
> "谁能干活"的元数据管理：Worker 注册 / 在线状态 / WorkerProjectMapping / WorkerProjectProposal（自动发现 + 用户确认）+ Project 元数据。
>
> **本 BC 不管"怎么干活"**：dispatch / shim / workspace 物理创建 / Agent CLI 子进程 / JSONL 解析 / Artifact / kill 进程级机制 / reconcile worker 端 → 全部归 [TaskRuntime BC](../task-runtime/00-overview.md)（[ADR-0019](../../../decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md) carve 后的边界）。

---

## § 0. BC 一览

### 0.1 职责

| 维度 | 内容 |
|---|---|
| **聚合管理** | Worker（AR + 子从属 BootstrapToken + WorkerProjectMapping）/ AgentInstance（独立 AR，[ADR-0024](../../../decisions/drafts/0024-agent-instance-first-class.md)）/ WorkerProjectProposal（独立 AR）/ Project（独立 AR）|
| **Worker 生命周期** | enroll token 签发 → `agent-center join` 兑换 session token / 心跳 / 在线状态 / heartbeat timeout（[ADR-0023](../../../decisions/drafts/0023-worker-enroll-lightweight.md)）|
| **AgentInstance 生命周期** | 用户 `agent create` → state machine `idle ↔ active → sleeping → archived`；跟 Worker 状态联动；持久 home_dir 沉淀 agent-level instructions（[ADR-0024](../../../decisions/drafts/0024-agent-instance-first-class.md)）|
| **行为配置主权** | concurrency / discovery / capabilities 在 center DB 主导；worker 端通过 reconcile 拉取 + `worker.config.updated` 长连推送同步 |
| **自动发现 + 用户确认** | Worker 周期扫 scan_paths（来自 center 下发的 WorkerConfig）找候选项目 → 写 Proposal → 飞书卡片 → 用户拍板 → 升级为 Mapping |
| **Project 元数据** | project_id / name / kind / default_agent_cli 等；自动从 accepted Proposal 创建 |
| **不持有的状态** | Task / TaskExecution / per-execution 运行时（已 carve 到 TaskRuntime BC）|

### 0.2 UL 切片

来自 [strategic/03-bounded-contexts § 1](../../strategic/03-bounded-contexts.md) 标 Workforce 上下文的术语：

- `Worker`（聚合根，用户开发机守护进程）
- `BootstrapToken`（实体，子从属于 Worker；enroll 凭证，stateful 状态机）
- `WorkerProjectMapping`（实体，子从属于 Worker；已生效的稳定映射）
- `AgentInstance`（聚合根，独立；agent 一等公民身份，绑到 worker，含 config + 状态机；[ADR-0024](../../../decisions/drafts/0024-agent-instance-first-class.md)）
- `WorkerProjectProposal`（聚合根，独立；自动发现的候选）
- `Project`（聚合根，独立；任务归属的逻辑容器）
- 行为动词：`Issue` / `Reissue` / `Revoke`（token 签发 / 重发 / 撤销）/ `Join`（worker 一行接入）/ `Enroll`（仓库 / 服务层的 token-to-session 兑换动词）/ `Create` / `Archive`（AgentInstance 软删）/ `Activate` / `Idle` / `Sleeping` / `Awaken`（AgentInstance 状态转移）/ `Adopt`（用户接纳一条 Proposal）/ `Invalidate`（base_path 失效）
- 状态机词汇：Worker `online` / `offline`；BootstrapToken `active` / `used` / `expired` / `revoked`；AgentInstance `idle` / `active` / `sleeping` / `archived`；WorkerProjectProposal `pending` / `accepted` / `ignored` / `superseded`

### 0.3 Context Map 位置

[strategic/03-bounded-contexts § 3](../../strategic/03-bounded-contexts.md)：

- **Workforce ↔ TaskRuntime**：**Shared Kernel**（Task / TaskExecution 引用 `worker_id` + `project_id`；TaskExecution dispatch 时 center 查 WorkerProjectMapping 取 base_path）
- **Workforce ↔ Discussion**：Shared Kernel（Issue 引用 `project_id`）
- **Bridge → Workforce**：Customer-Supplier（用户在飞书点 Proposal 卡片按钮 → Bridge 翻译为 `accept-proposal` / `ignore-proposal` API 调用）
- **Cognition → Workforce**："User" via tools（Supervisor 看到 proposal 写 supervisor_summary 提醒用户决策；不替用户做 accept/ignore）
- **Observability ← Workforce**：Open Host（订阅 `worker.*` / `project.*` / `worker_project_proposal.*` / `worker_project_mapping.*` 事件）

---

## § 1. 聚合清单（X.1）

### 1.1 Aggregate Roots

| 聚合 | 文件 | 状态机 | 身份 / 不变性 |
|---|---|---|---|
| **Worker** | [01-worker.md](01-worker.md) | 2 态（online / offline）+ 内部状态 enrolled | `worker_id`（用户在 `agent-center join` 时指定，全局唯一）；身份不变 |
| **AgentInstance** | [04-agent-instance.md](04-agent-instance.md) | 4 态（idle / active / sleeping / archived）| ULID `id`；`name` 全局唯一；`worker_id` v2 不可变（[ADR-0024](../../../decisions/drafts/0024-agent-instance-first-class.md)）|
| **WorkerProjectProposal** | [03-worker-project-proposal.md](03-worker-project-proposal.md) | 4 态（pending / accepted / ignored / superseded）| ULID/UUID；身份不变；按 `(worker_id, candidate_path)` 唯一 |
| **Project** | [02-project.md](02-project.md) | 无状态机（v1；project add / update / remove 是 CRUD）| `project_id`（slug，全局唯一）；身份不变 |

### 1.2 Entity（子从属）

| 实体 | 从属 | 位置 |
|---|---|---|
| **BootstrapToken** | Worker（独立表 `bootstrap_tokens`） | [01-worker.md § 3](01-worker.md)（[ADR-0023](../../../decisions/drafts/0023-worker-enroll-lightweight.md)）|
| **WorkerProjectMapping** | Worker（独立表 `worker_project_mappings`） | [01-worker.md § 5](01-worker.md) |

### 1.3 Value Objects（按使用聚合分组）

| VO | 用在哪 | 描述 |
|---|---|---|
| **SessionToken** | Worker 长连接认证 | 长期；worker 端落 `~/.agent-center/credentials` (mode 0600)；session 失效需要重新 enroll（[ADR-0023](../../../decisions/drafts/0023-worker-enroll-lightweight.md)）|
| **WorkerConfig** | center → worker 同步 | `{ concurrency, discovery, capabilities_enabled }`；reconcile response 返回 + `worker.config.updated` 长连推送（[ADR-0023](../../../decisions/drafts/0023-worker-enroll-lightweight.md)）|
| **Capability** | Worker.capabilities 数组元素 | v2 形态：`{ agent_cli, detected, enabled, version?, supports_mcp, supports_skills, supports_session }`；探测来源 + 用户开关 + adapter feature 上报（[ADR-0023](../../../decisions/drafts/0023-worker-enroll-lightweight.md) + [ADR-0030](../../../decisions/drafts/0030-agentadapter-matrix-expansion.md)）|
| **AgentInstanceConfig** | AgentInstance.config 字段（JSON）| `{ instructions_ref?, mcp_config?, skills?, ... }`；G1 约定 instructions；G4 约定 mcp_config（MCP 标准 schema + SecretRef，[ADR-0027](../../../decisions/drafts/0027-mcp-per-agent-injection.md)）；G5 约定 skills（后续）|
| **CandidateMetadata** | Proposal | `{git_remote_url, commit_count, recent_activity_at, detected_language}` 等启发式发现的项目元数据 |
| **ProjectKind** | Project / Proposal | `coding` / `writing` / `investing` / `null`（启发式）|
| **InvalidateReason** | mapping invalidated | `path_missing` / `not_git_repo` / `manual_remove` 等 |

---

## § 2. Invariants 索引（X.2）

每个聚合自己维护 invariants 节，本 § 仅做索引：

- **Worker Invariants** → [01-worker.md § 8](01-worker.md)
- **BootstrapToken Invariants** → [01-worker.md § 3.4](01-worker.md)（作为 Worker 子从属）
- **WorkerProjectMapping Invariants** → [01-worker.md § 5.5](01-worker.md)（作为 Worker 子从属）
- **AgentInstance Invariants** → [04-agent-instance.md § 7](04-agent-instance.md)
- **WorkerProjectProposal Invariants** → [03-worker-project-proposal.md § 6](03-worker-project-proposal.md)
- **Project Invariants** → [02-project.md § 5](02-project.md)

**跨聚合的不变量**：

1. **WorkerProjectMapping 必须有 Worker + Project 都存在**：app 层校验 worker_id / project_id 均有效
2. **Proposal accepted → 同事务创建（或复用）Project + 创建 Mapping**（[ADR-0008](../../../decisions/0008-worker-project-mapping-via-discovery-proposal.md)）
3. **一个 (worker_id, project_id) 只有一条 active Mapping**（重新 accept 时旧 mapping 标 invalidated，新 mapping 取代）
4. **Project.project_id 全局唯一**（slug 形式，不允许同名）
5. **AgentInstance.state 跟 Worker.status 联动**：worker → offline 时该 worker 上所有 idle/active 的 AgentInstance → sleeping；worker → online 时 sleeping 的 → idle（[ADR-0024](../../../decisions/drafts/0024-agent-instance-first-class.md)）
6. **AgentInstance.agent_cli 必须 ∈ worker.capabilities[detected ∧ enabled]**：dispatch 阶段校验；create 阶段同样校验

---

## § 3. Domain Services（X.3）

### 3.1 BootstrapTokenService

**职责**：enroll token 全生命周期 —— issue / reissue / revoke / expire-scan。

| 维度 | 内容 |
|---|---|
| 入参 | `IssueRequest { worker_id, created_by }` / `ReissueRequest { worker_id, reissued_by }` / `RevokeRequest { token_id, revoked_by, reason }` |
| 出参 | `IssueResponse { token_value, expires_at }`（明文 token 仅此一次返回，DB 只存 hash）|
| 状态机校验 | reissue 拒绝 `used`；reissue 旧 `active` → `revoked` 同事务做 |
| 并发 | `SELECT ... FOR UPDATE` 锁住 `(worker_id, status=active)` 行 |
| 事件 | `worker.bootstrap_token.issued / used / expired / reissued / revoked` |

详见 [01-worker.md § 3](01-worker.md)。

### 3.2 WorkerEnrollService

**职责**：`agent-center join` 时 bootstrap token → session token 兑换 + Worker 初始化 + reconcile 触发。

| 维度 | 内容 |
|---|---|
| 入参 | `ExchangeRequest { token_value, worker_id }`（来自 worker 机 `agent-center join`）|
| 出参 | `ExchangeResponse { session_token, expires_at }` + Worker 入库（如首次） + emit `worker.bootstrap_token.used` + `worker.enrolled` |
| 校验 | token 在 `active` 且未过期；`worker_id` 与 token 绑定的 worker 一致 |
| 失败 | token 不存在 / 已 used / expired / revoked → 拒绝并返回明确错误 |
| 跨聚合 | 写 Worker（如首次）+ 写 BootstrapToken 状态；不写 WorkerProjectMapping（mapping 走 Proposal 路径）|
| 触发后续 | reconcile 第一步拉 WorkerConfig 下发 + worker 端 capabilities 探测上报 |

详见 [01-worker.md § 2](01-worker.md)。

### 3.3 WorkerConfigService

**职责**：center 端读 / 写 Worker 行为配置 + 通过长连接推送变更给在线 worker。

| 维度 | 内容 |
|---|---|
| 入参 | `SetConfigCommand { worker_id, changed_fields, by }` |
| 跨聚合 | 写 Worker（合并 concurrency / discovery / capabilities-enabled 字段）|
| 在线推送 | 通过 worker 长连接 push `worker.config.updated` 事件 → worker 重拉 config（不重启）|
| Offline worker | 不推；重连时 reconcile 自动拉 |
| 事件 | `worker.config.updated` |

详见 [01-worker.md § 4](01-worker.md)。

### 3.4 AgentInstanceManagementService（[ADR-0024](../../../decisions/drafts/0024-agent-instance-first-class.md)）

**职责**：AgentInstance 全生命周期 —— create / update config / archive。

| 维度 | 内容 |
|---|---|
| 入参 | `CreateAgentCommand { name, agent_cli, worker_id, max_concurrent?, config? }` / `UpdateAgentConfigCommand { id, fields, by }` / `ArchiveAgentCommand { id, by }` |
| 校验 | create: name 全局唯一 + `agent_cli` ∈ worker.capabilities[detected ∧ enabled] + worker 存在；archive: state=idle |
| 跨聚合 | 写 AgentInstance；create 时不写 Worker / WorkerProjectMapping |
| 事件 | `agent_instance.created / config_updated / archived` |

详见 [04-agent-instance.md](04-agent-instance.md)。

### 3.5 AgentInstanceLifecycleService（[ADR-0024](../../../decisions/drafts/0024-agent-instance-first-class.md)）

**职责**：响应 TaskExecution 状态 + Worker 状态变化，自动更新 AgentInstance.state。

| 触发 | 行为 |
|---|---|
| TaskExecution 进 submitted/working，所属 AgentInstance state=idle | → state=active；emit `agent_instance.activated` |
| AgentInstance 上最后一个 active TaskExecution 进终态 | → state=idle；emit `agent_instance.idle` |
| Worker → offline | 该 worker 上 idle/active 的 AgentInstance → sleeping；emit `agent_instance.sleeping` |
| Worker → online | 该 worker 上 sleeping 的 AgentInstance → idle；emit `agent_instance.awakened` |

实现：订阅 `task_execution.*` / `worker.online` / `worker.offline` 事件，触发 state 转移。

### 3.6 ProposalDiscoveryService

**职责**：Worker 端周期扫 scan_paths（来自 center 下发的 WorkerConfig.discovery）找候选 git repo + 跟 Center 同步 proposal 状态去重。

| 维度 | 内容 |
|---|---|
| 入参 | Worker scan_interval 触发 + `scan_paths` / `exclude` 配置（center 下发）|
| 出参 | N 个 candidate path → 跟 Center 查重（pending/accepted/ignored 跳过）→ 新 proposal 入库 + emit `worker_project_proposal.proposed` |
| 跨聚合 | 写 WorkerProjectProposal（新建）+ 查既有 mapping / proposal 状态 |
| 启发式 | suggested_project_id = dir name；suggested_kind = 按 `go.mod` / `package.json` / 文件后缀推断 |

详见 [03-worker-project-proposal.md § 3 发现流程](03-worker-project-proposal.md) + [ADR-0008](../../../decisions/0008-worker-project-mapping-via-discovery-proposal.md)。

### 3.7 ProposalReviewService

**职责**：用户在飞书 / Web Console / CLI 对 Proposal 做 accept / ignore / accept-with-edit 决策；同事务 spawn Project（如不存在）+ Mapping。

| 维度 | 内容 |
|---|---|
| 入参 | `ReviewProposalCommand { proposal_id, decision: accept | accept-with-edit | ignore, override_project_id?, override_kind? }` |
| 出参 | Proposal 状态终结 + （accept 时）新 Project + 新 WorkerProjectMapping |
| 跨聚合 | 单事务：写 Proposal 状态 + 可能写 Project（不存在则建）+ 写 Mapping + emit `worker_project_proposal.accepted/ignored` + `project.created`（如新建）+ `worker_project_mapping.added` |
| 命名冲突 | accept 时 suggested_project_id 已存在 → 飞书卡片标红让用户改 |
| 用户拒绝 | ignore：proposal.status=ignored；worker 下次扫不再提；可通过 `worker proposal unignore <id>` 重置 |

详见 [03-worker-project-proposal.md § 4-5](03-worker-project-proposal.md)。

### 3.8 MappingInvalidationService

**职责**：Worker 扫到 mapping base_path 已不存在 / 不再是 git repo 时，触发 mapping 标 invalidated。

| 维度 | 内容 |
|---|---|
| 触发 | Worker scan 阶段对比既有 mapping 表 → 缺失 base_path → emit `worker_project_mapping.invalidated` |
| Center 行为 | mapping.status = invalidated（不删，保留血缘）；飞书提示用户决定是否重新映射；不自动迁移 |
| 跨聚合 | 写 Mapping 状态 + emit 事件 |

详见 [01-worker.md § 5.3 Invalidation](01-worker.md)。

---

## § 4. Factories（X.4）

### 4.1 BootstrapTokenFactory

**唯一 caller**：BootstrapTokenService（§ 3.1）的 issue / reissue 路径。

入参：`{ worker_id, ttl: 30min, created_by }`。返回明文 token 仅此一次；DB 落 `value_hash`。

### 4.2 WorkerFactory

**唯一 caller**：WorkerEnrollService（§ 3.2）token exchange 时若 Worker AR 还不存在则建立（默认 config = 系统默认值）。

入参：`{ worker_id }`（其余字段由系统默认值填充：`concurrency.per_agent_type=2`、`discovery.scan_interval=1h`、`capabilities=[]`、等首次 reconcile + 探测填）。

### 4.3 AgentInstanceFactory（[ADR-0024](../../../decisions/drafts/0024-agent-instance-first-class.md)）

**唯一 caller**：AgentInstanceManagementService（§ 3.4）的 create 路径。

入参：`{ name, agent_cli, worker_id, max_concurrent?, config? }`。状态初始化为 `idle`。

### 4.4 ProposalFactory

**唯一 caller**：ProposalDiscoveryService（§ 3.6）。Worker 端 scan 发现新 candidate 时，跨网络通知 Center 建 Proposal。

### 4.5 ProjectFactory

**两个 caller**：
- ProposalReviewService（§ 3.7）：accept Proposal 时若 project 不存在则同事务建
- CLI `agent-center project add`（手动，罕见路径）

入参：`{ project_id, name, kind, default_agent_cli?, default_workspace_mode? }`。

### 4.6 MappingFactory

**两个 caller**：
- ProposalReviewService（§ 3.7）：accept 时同事务建
- ~~CLI `worker mapping add`~~（[roadmap](../../../roadmap.md)）

入参：`{ worker_id, project_id, base_path, source_proposal_id }`。

---

## § 5. Repositories（X.5）

接口签名（Go-style，含 `ctx context.Context` 参数；架构层契约，跟实现解耦）：

### 5.1 WorkerRepository

```go
type WorkerRepository interface {
    FindByID(ctx context.Context, id WorkerID) (*Worker, error)
    FindByStatus(ctx context.Context, status WorkerStatus, filter WorkerFilter) ([]*Worker, error)
    FindAll(ctx context.Context, filter WorkerFilter) ([]*Worker, error)
    Save(ctx context.Context, w *Worker) error                                                  // 新建 + 全量更新（含乐观锁 version 列）
    UpdateStatus(ctx context.Context, id WorkerID, from, to WorkerStatus, version int) error    // online/offline CAS 防 heartbeat 竞态
    UpdateLastHeartbeatAt(ctx context.Context, id WorkerID, at time.Time, workingSeconds int) error
    UpdateConfig(ctx context.Context, id WorkerID, fields WorkerConfigFields, version int) error // 改 concurrency / discovery / capabilities-enabled
    UpdateCapabilities(ctx context.Context, id WorkerID, detected []Capability) error           // worker 上报探测结果
}

// WorkerConfigFields 允许由 center 端 SetConfig 触发更新的子集（pointer = nil 不改）
type WorkerConfigFields struct {
    Concurrency         *ConcurrencyConfig
    Discovery           *DiscoveryConfig
    CapabilityEnabledBy map[string]bool   // agent_cli → enabled
}

// Domain errors
var (
    ErrWorkerNotFound        = errors.New("workforce: worker not found")
    ErrWorkerAlreadyExists   = errors.New("workforce: worker already enrolled")
    ErrWorkerOffline         = errors.New("workforce: worker is offline, cannot accept new dispatch")
    ErrWorkerVersionConflict = errors.New("workforce: worker version conflict (optimistic lock)")
)
```

### 5.2 AgentInstanceRepository（[ADR-0024](../../../decisions/drafts/0024-agent-instance-first-class.md)）

```go
type AgentInstanceRepository interface {
    FindByID(ctx context.Context, id AgentInstanceID) (*AgentInstance, error)
    FindByName(ctx context.Context, name string) (*AgentInstance, error)
    FindByWorkerID(ctx context.Context, workerID WorkerID, states ...AgentInstanceState) ([]*AgentInstance, error)
    FindAll(ctx context.Context, filter AgentInstanceFilter) ([]*AgentInstance, error)
    Save(ctx context.Context, a *AgentInstance) error                                              // 新建 + 全量更新（含乐观锁 version 列）
    UpdateState(ctx context.Context, id AgentInstanceID, from, to AgentInstanceState, version int) error  // CAS 防竞态
    UpdateConfig(ctx context.Context, id AgentInstanceID, fields AgentInstanceConfigFields, version int) error
    CountActiveExecutions(ctx context.Context, id AgentInstanceID) (int, error)                    // 派单校验时查
    BulkUpdateStateByWorker(ctx context.Context, workerID WorkerID, from, to AgentInstanceState) (count int, err error)
        // worker offline → sleeping / worker online → idle 批量联动
}

type AgentInstanceConfigFields struct {
    Config         *AgentInstanceConfig
    MaxConcurrent  *int   // pointer-of-pointer 语义实现"显式设 nil"需要单独 helper；这里简化
}

// Domain errors
var (
    ErrAgentInstanceNotFound      = errors.New("workforce: agent instance not found")
    ErrAgentInstanceNameTaken     = errors.New("workforce: agent instance name already taken")
    ErrAgentInstanceVersionConflict = errors.New("workforce: agent instance version conflict (optimistic lock)")
    ErrAgentInstanceInvalidTransition = errors.New("workforce: agent instance state transition invalid")
    ErrAgentInstanceArchivedNotIdle = errors.New("workforce: agent instance must be idle to archive")
    ErrAgentInstanceCapabilityMissing = errors.New("workforce: worker does not have the required agent_cli capability enabled")
)
```

### 5.3 BootstrapTokenRepository（sub-repo of Worker, [ADR-0023](../../../decisions/drafts/0023-worker-enroll-lightweight.md)）

```go
type BootstrapTokenRepository interface {
    FindByID(ctx context.Context, id TokenID) (*BootstrapToken, error)
    FindByValueHash(ctx context.Context, valueHash string) (*BootstrapToken, error)          // exchange 时用 hash 比对查
    FindByWorkerID(ctx context.Context, workerID WorkerID, statuses ...TokenStatus) ([]*BootstrapToken, error)
    FindActiveByWorkerForUpdate(ctx context.Context, workerID WorkerID) (*BootstrapToken, error)  // FOR UPDATE 行锁；reissue 并发用
    Save(ctx context.Context, t *BootstrapToken) error                                       // 新建（issue / reissue 新 token）
    UpdateStatus(ctx context.Context, id TokenID, from, to TokenStatus, at time.Time) error  // status CAS
    FindExpired(ctx context.Context, before time.Time) ([]*BootstrapToken, error)            // 扫描器查 TTL 到期
}

// Domain errors
var (
    ErrTokenNotFound          = errors.New("workforce: bootstrap token not found")
    ErrTokenInvalid           = errors.New("workforce: bootstrap token invalid or expired")
    ErrTokenAlreadyUsed       = errors.New("workforce: bootstrap token already used; reissue not allowed")
    ErrTokenWorkerMismatch    = errors.New("workforce: bootstrap token does not match the supplied worker_id")
    ErrTokenAlreadyActiveExists = errors.New("workforce: another active token exists for this worker; revoke first")
)
```

### 5.4 WorkerProjectMappingRepository（sub-repo of Worker）

```go
type WorkerProjectMappingRepository interface {
    FindByWorkerID(ctx context.Context, workerID WorkerID) ([]*WorkerProjectMapping, error)
    FindByProjectID(ctx context.Context, projectID ProjectID) ([]*WorkerProjectMapping, error)
    FindByWorkerAndProject(ctx context.Context, workerID WorkerID, projectID ProjectID) (*WorkerProjectMapping, error)
    Save(ctx context.Context, m *WorkerProjectMapping) error
    Invalidate(ctx context.Context, id MappingID, reason InvalidateReason, message string) error // status=active → invalidated, 不删；reason 是 § 1.3 VO
}

// Domain errors
var (
    ErrMappingNotFound       = errors.New("workforce: mapping not found")
    ErrMappingAlreadyActive  = errors.New("workforce: (worker_id, project_id) already has active mapping")
    ErrMappingNotActive      = errors.New("workforce: mapping not in active state")
)
```

### 5.5 WorkerProjectProposalRepository

```go
type WorkerProjectProposalRepository interface {
    FindByID(ctx context.Context, id ProposalID) (*WorkerProjectProposal, error)
    FindByWorkerID(ctx context.Context, workerID WorkerID, status ...ProposalStatus) ([]*WorkerProjectProposal, error)
    FindPending(ctx context.Context) ([]*WorkerProjectProposal, error)
    FindByCandidatePath(ctx context.Context, workerID WorkerID, path string) (*WorkerProjectProposal, error) // 去重查询
    Save(ctx context.Context, p *WorkerProjectProposal) error
    UpdateStatus(ctx context.Context, id ProposalID, from, to ProposalStatus, reviewedBy string, resultingMappingID *MappingID) error
}

// Domain errors
var (
    ErrProposalNotFound          = errors.New("workforce: proposal not found")
    ErrProposalAlreadyTerminated = errors.New("workforce: proposal already in terminal state")
    ErrProposalInvalidTransition = errors.New("workforce: invalid proposal status transition")
)
```

### 5.6 ProjectRepository

```go
type ProjectRepository interface {
    FindByID(ctx context.Context, id ProjectID) (*Project, error)
    FindAll(ctx context.Context, filter ProjectFilter) ([]*Project, error)
    Save(ctx context.Context, p *Project) error                                                     // 新建 + 全量更新（含乐观锁 version 列）
    Update(ctx context.Context, id ProjectID, fields ProjectUpdateFields, version int) error       // 更新允许字段；CAS via version
    Delete(ctx context.Context, id ProjectID) error                                                 // 严格删除，必先无 active task / mapping
}

// ProjectUpdateFields 是允许更新的字段集合（pointer 表示 nil = 不改）
type ProjectUpdateFields struct {
    Name            *string
    Kind            *ProjectKind
    DefaultAgentCLI *string
    Description     *string
}

// Domain errors
var (
    ErrProjectNotFound        = errors.New("workforce: project not found")
    ErrProjectAlreadyExists   = errors.New("workforce: project_id already taken")
    ErrProjectVersionConflict = errors.New("workforce: project version conflict (optimistic lock)")
    ErrProjectHasActiveDeps   = errors.New("workforce: project has active task or mapping, cannot delete")
)
```

### 5.7 约定

- 外部只通过 Root.id 引用各 AR（[conventions § 0.3](../../../../rules/conventions.md) AR 守门）
- BootstrapToken / WorkerProjectMapping 都是 Worker 的子从属 Entity，通过 worker_id 关联到 Worker 聚合（强引用）
- AgentInstance 是独立 AR，通过 worker_id 弱关联到 Worker（[ADR-0024](../../../decisions/drafts/0024-agent-instance-first-class.md)）
- Repository 是**领域层抽象接口**；实现层落到 [implementation/02-persistence-schema.md](../../../implementation/)
- Proposal accept 同事务建 Project（如不存在）+ Mapping（[ADR-0008](../../../decisions/0008-worker-project-mapping-via-discovery-proposal.md) + [ADR-0014 § 2](../../../decisions/0014-event-sourcing-level.md)）：跨多个 Repository 调用同事务，由 application service 协调
- BootstrapToken reissue 同事务 revoke 旧 active + insert 新 active（[ADR-0023](../../../decisions/drafts/0023-worker-enroll-lightweight.md)）：跨同一 Repository 的两次写，行锁防并发
- AgentInstance state 跟 Worker.status 联动通过 `BulkUpdateStateByWorker` 批量切换（[ADR-0024](../../../decisions/drafts/0024-agent-instance-first-class.md)）
- Domain errors 用 sentinel error pattern；调用方用 `errors.Is` 判定

---

## § 6. 跨聚合引用出方向（X.6）

| 引用方 → 被引方 | 强弱 | 一致性窗口 | 触发场景 | ADR |
|---|---|---|---|---|
| **WorkerProjectMapping → Worker**（`mapping.worker_id`） | 强 / 不可变 | tx 同步（创建时填）| ProposalReviewService accept | - |
| **WorkerProjectMapping → Project**（`mapping.project_id`） | 强 / 不可变 | tx 同步（创建时填）| ProposalReviewService accept | - |
| **WorkerProjectMapping → Proposal**（`mapping.source_proposal_id`） | 弱 / 血缘 | tx 同步（创建时填）| ProposalReviewService accept | - |
| **WorkerProjectProposal → Worker**（`proposal.worker_id`） | 强 / 不可变 | tx 同步 | ProposalDiscoveryService | - |
| **WorkerProjectProposal → 既建 Mapping**（`proposal.resulting_mapping_id`） | 弱 / 反向血缘 | tx 同步（accept 时回填） | ProposalReviewService | - |
| **AgentInstance → Worker**（`agent_instance.worker_id`） | 强 / v2 不可变 | tx 同步（create 时填）；删 Worker 前置要求所有 agent archived | AgentInstanceManagementService create | [ADR-0024](../../../decisions/drafts/0024-agent-instance-first-class.md) |
| **TaskRuntime ← Workforce**（`task.project_id` / `task_execution.worker_id` / `task_execution.project_id` / `task_execution.agent_instance_id`）| 强 / 不可变 | tx 同步（TaskFactory 创建时填） | Task / TaskExecution 创建 | [ADR-0024](../../../decisions/drafts/0024-agent-instance-first-class.md) |
| **Discussion ← Workforce**（`issue.project_id`） | 强 / 不可变 | tx 同步 | Issue 创建 | - |

**跨聚合一致性策略汇总**：

- **Proposal accept 流程**：单事务内写 Proposal 状态 + 可能建 Project + 建 WorkerProjectMapping（[ADR-0008](../../../decisions/0008-worker-project-mapping-via-discovery-proposal.md) + [ADR-0014 § 2](../../../decisions/0014-event-sourcing-level.md)）
- **Mapping invalidate**：单聚合改 status（不删）+ emit 事件

---

## § 7. 跨 BC 交互

### 7.1 Supervisor 唤醒事件白名单

Workforce emit 的事件中触发 supervisor 唤醒的子集：

| 事件 | 典型 supervisor 决策 |
|---|---|
| `worker.online` | 重评估堆积 open task（适合的 task 可以派给这个 worker）|
| `worker.offline` | 处理 worker_lost 的 active execution（详见 [TaskRuntime § 3.3](../task-runtime/00-overview.md) TimeoutScanner）|
| `worker.bootstrap_token.expired` | 通知用户 enroll token 过期（可在卡片提供 reissue 操作入口）|
| `worker.capability.detected` | 一般不直接动作；用于 fleet view 更新 |
| `agent_instance.created` | 通知用户「agent X 已就绪」；可触发引导 |
| `agent_instance.awakened` | 重评估堆积 open task 可派给这个 agent |
| `agent_instance.archived` | 清理跟该 agent 关联的待派单意图 |
| `worker_project_proposal.proposed` | 推飞书卡片让用户决策 accept / ignore / 改后 accept |
| `worker_project_mapping.invalidated` | 飞书提示用户决定是否重新映射 |
| `project.created` / `project.removed` | 一般不直接动作（除非有等候该 project 的 open task）|

详见 [cognition/00-overview.md](../cognition/00-overview.md)。

### 7.2 Bridge 渲染（outbound）

| 事件 | Bridge 渲染动作 |
|---|---|
| `worker_project_proposal.proposed` | 通过 supervisor flow 推飞书卡片：项目候选 + [✅ 加入] [✏️ 改后加入] [❌ 忽略] 按钮 |
| `worker_project_mapping.invalidated` | 推飞书提示（system kind Message）|
| 卡片按钮 → `card.action.trigger` inbound | Bridge 翻译为 `accept-proposal` / `ignore-proposal` API 调用 |

详见 [bridge/01-feishu-integration.md](../bridge/01-feishu-integration.md)。

### 7.3 Observability 订阅

Observability BC 订阅 Workforce 全部事件做投影 / fleet view / inspect。详见 [observability/00-overview.md § 7.5 事件总览](../observability/00-overview.md)。

### 7.4 Customer-Supplier 上下游汇总

| 上游 → 下游 | 模式 | 内容 |
|---|---|---|
| Workforce ↔ TaskRuntime | Shared Kernel | Task / TaskExecution 引用 worker_id / project_id；TaskExecution dispatch 时 center 查 WorkerProjectMapping 取 base_path |
| Workforce ↔ Discussion | Shared Kernel | Issue 引用 project_id |
| Bridge → Workforce | Customer-Supplier | inbound 时 Bridge 调 accept-proposal / ignore-proposal API |
| Cognition → Workforce | "User" via tools | Supervisor 看 proposal 提醒用户（不直接决策）|
| Observability ← Workforce | Open Host | 全部 `worker.*` / `project.*` / `worker_project_proposal.*` / `worker_project_mapping.*` 事件订阅 |

完整 context map 见 [strategic/03-bounded-contexts § 3](../../strategic/03-bounded-contexts.md)。

---

## § 8. Out-of-Scope / Future Work

| 项 | 归属 |
|---|---|
| 跨 worker 自动广播已 accepted project | [roadmap](../../../roadmap.md)（v1 每个 worker 自己扫到才 propose）|
| 自动跟随路径移动 | [roadmap](../../../roadmap.md)（v1 路径变化必须重新 propose）|
| 提议合并 / 去重 | [roadmap](../../../roadmap.md)（v1 每条候选独立 proposal）|
| CLI 手动管理 mapping（运行时 add / remove）| [roadmap](../../../roadmap.md)（v1 仅 enroll / proposal 路径）|
| Worker 端 mapping 持久缓存（offline 也能查）| [roadmap](../../../roadmap.md) |
| Project 多用户分享 / 权限 | [out-of-scope](../../../requirements/03-out-of-scope.md)（v1 单用户）|
| Worker 端动态 hot-reload `worker.yaml` | [roadmap](../../../roadmap.md)（v1 改 yaml 后必须重启）|

---

## § 9. References

### 相关 ADR

- [ADR-0008 WorkerProjectMapping discovery proposal](../../../decisions/0008-worker-project-mapping-via-discovery-proposal.md)
- [ADR-0014 事件溯源走 L1](../../../decisions/0014-event-sourcing-level.md)（同事务双写原则）
- [ADR-0019 BC 合并](../../../decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)（原 BC4 carve 出去，本 BC 留元数据管理职责）
- [ADR-0023 Worker Enroll 轻量化（草案）](../../../decisions/drafts/0023-worker-enroll-lightweight.md)（一行命令接入 + BootstrapToken Entity + 服务端配置主导）
- [ADR-0024 AgentInstance 一等公民化（草案）](../../../decisions/drafts/0024-agent-instance-first-class.md)（agent 升级为独立 AR + 1:N 并发 + home_dir + state machine）
- [ADR-0026 SecretManagement BC（草案）](../../../decisions/drafts/0026-user-secret-management-bc.md)（AgentInstance.config.mcp_config 内嵌 SecretRef，secret 解析路径）
- [ADR-0027 MCP per-agent 注入（草案）](../../../decisions/drafts/0027-mcp-per-agent-injection.md)（mcp_config schema + 注入流程）

### 战略层

- [strategic/03-bounded-contexts § 1 UL](../../strategic/03-bounded-contexts.md)（Workforce 上下文术语）
- [strategic/03-bounded-contexts § 2 BC3 Workforce](../../strategic/03-bounded-contexts.md)
- [strategic/03-bounded-contexts § 3 Context Map](../../strategic/03-bounded-contexts.md)
- [strategic/01-subdomain-classification](../../strategic/01-subdomain-classification.md)（Workforce: Supporting-Essential）

### 同 BC 内聚合详情

- [01-worker.md](01-worker.md) — Worker 聚合 + BootstrapToken + WorkerProjectMapping 子从属
- [02-project.md](02-project.md) — Project 聚合
- [03-worker-project-proposal.md](03-worker-project-proposal.md) — WorkerProjectProposal 聚合
- [04-agent-instance.md](04-agent-instance.md) — AgentInstance 独立 AR（[ADR-0024](../../../decisions/drafts/0024-agent-instance-first-class.md)）

### 跨 BC 协作文档

- [task-runtime/00-overview.md](../task-runtime/00-overview.md) — TaskRuntime BC 入口（含 ReconcileService / DispatchService 协议视图）
- [task-runtime/02-task-execution.md § 9-12](../task-runtime/02-task-execution.md) — worker 端运行时（per-execution 实施细节）
- [discussion/00-overview.md](../discussion/00-overview.md) — Issue 引用 project_id
- [conversation/00-overview.md](../conversation/00-overview.md) — Identity / ChannelBinding（用户跟 vendor 的关联，跟 Worker 是不同维度）
- [cognition/00-overview.md](../cognition/00-overview.md) — Supervisor 在 proposal 决策中的角色
- [bridge/01-feishu-integration.md](../bridge/01-feishu-integration.md) — Proposal 飞书卡片渲染

### 横切方法论

- [conventions](../../../../rules/conventions.md) § 0 DDD / § 1 无野任务（Worker / Agent 不允许造任务）/ § 9 dialect-agnostic / § 13 安全（bootstrap token / session token）
