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
| **聚合管理** | Worker（AR + 子从属 WorkerProjectMapping）/ WorkerProjectProposal（独立 AR）/ Project（独立 AR）|
| **Worker 生命周期** | 注册（bootstrap token → session token 兑换） / 心跳 / 在线状态 / heartbeat timeout |
| **自动发现 + 用户确认** | Worker 周期扫 scan_paths 找候选项目 → 写 Proposal → 飞书卡片 → 用户拍板 → 升级为 Mapping |
| **Project 元数据** | project_id / name / kind / default_agent_cli 等；自动从 accepted Proposal 创建 |
| **不持有的状态** | Task / TaskExecution / per-execution 运行时（已 carve 到 TaskRuntime BC）|

### 0.2 UL 切片

来自 [strategic/03-bounded-contexts § 1](../../strategic/03-bounded-contexts.md) 标 Workforce 上下文的术语：

- `Worker`（聚合根，用户开发机守护进程）
- `WorkerProjectMapping`（实体，子从属于 Worker；已生效的稳定映射）
- `WorkerProjectProposal`（聚合根，独立；自动发现的候选）
- `Project`（聚合根，独立；任务归属的逻辑容器）
- 行为动词：`Enroll`（注册）/ `Adopt`（用户接纳一条 Proposal）/ `Invalidate`（base_path 失效）
- 状态机词汇：Worker `online` / `offline`；WorkerProjectProposal `pending` / `accepted` / `ignored` / `superseded`

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
| **Worker** | [01-worker.md](01-worker.md) | 2 态（online / offline）+ 内部状态 enrolled | `worker_id`（用户在 worker.yaml 配，全局唯一）；身份不变 |
| **WorkerProjectProposal** | [03-worker-project-proposal.md](03-worker-project-proposal.md) | 4 态（pending / accepted / ignored / superseded）| ULID/UUID；身份不变；按 `(worker_id, candidate_path)` 唯一 |
| **Project** | [02-project.md](02-project.md) | 无状态机（v1；project add / update / remove 是 CRUD）| `project_id`（slug，全局唯一）；身份不变 |

### 1.2 Entity（子从属）

| 实体 | 从属 | 位置 |
|---|---|---|
| **WorkerProjectMapping** | Worker（独立表 `worker_project_mappings`，归属 worker） | [01-worker.md § 4](01-worker.md) |

### 1.3 Value Objects（按使用聚合分组）

| VO | 用在哪 | 描述 |
|---|---|---|
| **BootstrapToken** | Worker enroll | `agent-center worker enroll` 签发；短期；用一次（兑换 session token）|
| **SessionToken** | Worker 长连接认证 | 长期；workerclient 持有；session 失效需要重新 enroll |
| **WorkerYAML** | Worker 配置 | 包含 worker_id / bootstrap_token / center_endpoint / concurrency / discovery / agent_cli 等；详见 [01-worker.md § 3](01-worker.md) |
| **CandidateMetadata** | Proposal | `{git_remote_url, commit_count, recent_activity_at, detected_language}` 等启发式发现的项目元数据 |
| **ProjectKind** | Project / Proposal | `coding` / `writing` / `investing` / `null`（启发式）|
| **InvalidateReason** | mapping invalidated | `path_missing` / `not_git_repo` / `manual_remove`（v2）等 |

---

## § 2. Invariants 索引（X.2）

每个聚合自己维护 invariants 节，本 § 仅做索引：

- **Worker Invariants** → [01-worker.md § 7](01-worker.md)
- **WorkerProjectMapping Invariants** → [01-worker.md § 4.5](01-worker.md)（作为 Worker 子从属）
- **WorkerProjectProposal Invariants** → [03-worker-project-proposal.md § 6](03-worker-project-proposal.md)
- **Project Invariants** → [02-project.md § 5](02-project.md)

**跨聚合的不变量**：

1. **WorkerProjectMapping 必须有 Worker + Project 都存在**：app 层校验 worker_id / project_id 均有效
2. **Proposal accepted → 同事务创建（或复用）Project + 创建 Mapping**（[ADR-0008](../../../decisions/0008-worker-project-mapping-via-discovery-proposal.md)）
3. **一个 (worker_id, project_id) 只有一条 active Mapping**（重新 accept 时旧 mapping 标 invalidated，新 mapping 取代）
4. **Project.project_id 全局唯一**（slug 形式，不允许同名）

---

## § 3. Domain Services（X.3）

### 3.1 WorkerEnrollService

**职责**：bootstrap token → session token 兑换 + Worker 初始化 + reconcile 触发。

| 维度 | 内容 |
|---|---|
| 入参 | `EnrollRequest { worker_yaml, bootstrap_token, capabilities }` |
| 出参 | `EnrollResponse { session_token, expires_at }` + Worker 入库 + emit `worker.enrolled` |
| 跨聚合 | 写 Worker；不写 WorkerProjectMapping（mapping 走 Proposal 路径）|
| 触发后续 | reconcile（先看 worker 已有 active executions） + 第一次 scan_paths 扫描 |

详见 [01-worker.md § 2](01-worker.md)。

### 3.2 ProposalDiscoveryService

**职责**：Worker 端周期扫 scan_paths 找候选 git repo + 跟 Center 同步 proposal 状态去重。

| 维度 | 内容 |
|---|---|
| 入参 | Worker scan_interval 触发 + `scan_paths` / `exclude` 配置 |
| 出参 | N 个 candidate path → 跟 Center 查重（pending/accepted/ignored 跳过）→ 新 proposal 入库 + emit `worker_project_proposal.proposed` |
| 跨聚合 | 写 WorkerProjectProposal（新建）+ 查既有 mapping / proposal 状态 |
| 启发式 | suggested_project_id = dir name；suggested_kind = 按 `go.mod` / `package.json` / 文件后缀推断 |

详见 [03-worker-project-proposal.md § 3 发现流程](03-worker-project-proposal.md) + [ADR-0008](../../../decisions/0008-worker-project-mapping-via-discovery-proposal.md)。

### 3.3 ProposalReviewService

**职责**：用户在飞书 / Web Console / CLI 对 Proposal 做 accept / ignore / accept-with-edit 决策；同事务 spawn Project（如不存在）+ Mapping。

| 维度 | 内容 |
|---|---|
| 入参 | `ReviewProposalCommand { proposal_id, decision: accept | accept-with-edit | ignore, override_project_id?, override_kind? }` |
| 出参 | Proposal 状态终结 + （accept 时）新 Project + 新 WorkerProjectMapping |
| 跨聚合 | 单事务：写 Proposal 状态 + 可能写 Project（不存在则建）+ 写 Mapping + emit `worker_project_proposal.accepted/ignored` + `project.created`（如新建）+ `worker_project_mapping.added` |
| 命名冲突 | accept 时 suggested_project_id 已存在 → 飞书卡片标红让用户改 |
| 用户拒绝 | ignore：proposal.status=ignored；worker 下次扫不再提；可通过 `worker proposal unignore <id>` 重置 |

详见 [03-worker-project-proposal.md § 4-5](03-worker-project-proposal.md)。

### 3.4 MappingInvalidationService

**职责**：Worker 扫到 mapping base_path 已不存在 / 不再是 git repo 时，触发 mapping 标 invalidated。

| 维度 | 内容 |
|---|---|
| 触发 | Worker scan 阶段对比既有 mapping 表 → 缺失 base_path → emit `worker_project_mapping.invalidated` |
| Center 行为 | mapping.status = invalidated（不删，保留血缘）；飞书提示用户决定是否重新映射；不自动迁移 |
| 跨聚合 | 写 Mapping 状态 + emit 事件 |

详见 [01-worker.md § 4.4 边界情况](01-worker.md)。

---

## § 4. Factories（X.4）

### 4.1 WorkerFactory

**唯一 caller**：WorkerEnrollService（§ 3.1）。Worker enroll 时建 Worker AR。

入参：`worker.yaml` 配置（含 `worker_id` / `capabilities` / `concurrency` / `discovery` 等）。

### 4.2 ProposalFactory

**唯一 caller**：ProposalDiscoveryService（§ 3.2）。Worker 端 scan 发现新 candidate 时，跨网络通知 Center 建 Proposal。

### 4.3 ProjectFactory

**两个 caller**：
- ProposalReviewService（§ 3.3）：accept Proposal 时若 project 不存在则同事务建
- CLI `agent-center project add`（手动，v1 罕见路径）

入参：`{ project_id, name, kind, default_agent_cli?, default_workspace_mode? }`。

### 4.4 MappingFactory

**两个 caller**：
- ProposalReviewService（§ 3.3）：accept 时同事务建
- ~~CLI `worker mapping add`~~（v1 不实现，[roadmap](../../../roadmap.md)）

入参：`{ worker_id, project_id, base_path, source_proposal_id }`。

---

## § 5. Repositories（X.5）

**接口签名 TBD**，schema 见 [implementation/02-persistence-schema.md](../../../implementation/) (TBD)。

| Repository | 主表 | 主要操作 |
|---|---|---|
| **WorkerRepository** | `workers` | findById / findByStatus / save / updateStatus / updateLastHeartbeatAt |
| **WorkerProjectMappingRepository**（or sub-repo of Worker）| `worker_project_mappings` | findByWorkerId / findByProjectId / findByWorkerAndProject / save / updateStatus（invalidated）|
| **WorkerProjectProposalRepository** | `worker_project_proposals` | findById / findByWorkerId / findPending / findByCandidatePath / save / updateStatus |
| **ProjectRepository** | `projects` | findById / findAll / save / updateName / updateKind |

**约定**：

- 外部只通过 Root.id 引用各 AR
- WorkerProjectMapping 通过 worker_id 关联到 Worker 聚合（强引用）+ project_id 关联到 Project（弱引用）

---

## § 6. 跨聚合引用出方向（X.6）

| 引用方 → 被引方 | 强弱 | 一致性窗口 | 触发场景 | ADR |
|---|---|---|---|---|
| **WorkerProjectMapping → Worker**（`mapping.worker_id`） | 强 / 不可变 | tx 同步（创建时填）| ProposalReviewService accept | - |
| **WorkerProjectMapping → Project**（`mapping.project_id`） | 强 / 不可变 | tx 同步（创建时填）| ProposalReviewService accept | - |
| **WorkerProjectMapping → Proposal**（`mapping.source_proposal_id`） | 弱 / 血缘 | tx 同步（创建时填）| ProposalReviewService accept | - |
| **WorkerProjectProposal → Worker**（`proposal.worker_id`） | 强 / 不可变 | tx 同步 | ProposalDiscoveryService | - |
| **WorkerProjectProposal → 既建 Mapping**（`proposal.resulting_mapping_id`） | 弱 / 反向血缘 | tx 同步（accept 时回填） | ProposalReviewService | - |
| **TaskRuntime ← Workforce**（`task.project_id` / `task_execution.worker_id` / `task_execution.project_id`）| 强 / 不可变 | tx 同步（TaskFactory 创建时填） | Task / TaskExecution 创建 | - |
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
| `worker_project_proposal.proposed` | 推飞书卡片让用户决策 accept / ignore / 改后 accept |
| `worker_project_mapping.invalidated` | 飞书提示用户决定是否重新映射 |
| `project.created` / `project.removed` | 一般不直接动作（除非有等候该 project 的 open task）|

详见 [cognition/01-supervisor-model.md](../cognition/01-supervisor-model.md)。

### 7.2 Bridge 渲染（outbound）

| 事件 | Bridge 渲染动作 |
|---|---|
| `worker_project_proposal.proposed` | 通过 supervisor flow 推飞书卡片：项目候选 + [✅ 加入] [✏️ 改后加入] [❌ 忽略] 按钮 |
| `worker_project_mapping.invalidated` | 推飞书提示（system kind Message）|
| 卡片按钮 → `card.action.trigger` inbound | Bridge 翻译为 `accept-proposal` / `ignore-proposal` API 调用 |

详见 [bridge/01-feishu-integration.md](../bridge/01-feishu-integration.md)。

### 7.3 Observability 订阅

Observability BC 订阅 Workforce 全部事件做投影 / fleet view / inspect。详见 [observability/01-observability.md § 事件总览](../observability/01-observability.md)。

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

### 战略层

- [strategic/03-bounded-contexts § 1 UL](../../strategic/03-bounded-contexts.md)（Workforce 上下文术语）
- [strategic/03-bounded-contexts § 2 BC3 Workforce](../../strategic/03-bounded-contexts.md)
- [strategic/03-bounded-contexts § 3 Context Map](../../strategic/03-bounded-contexts.md)
- [strategic/01-subdomain-classification](../../strategic/01-subdomain-classification.md)（Workforce: Supporting-Essential）

### 同 BC 内聚合详情

- [01-worker.md](01-worker.md) — Worker 聚合 + WorkerProjectMapping 子从属
- [02-project.md](02-project.md) — Project 聚合
- [03-worker-project-proposal.md](03-worker-project-proposal.md) — WorkerProjectProposal 聚合

### 跨 BC 协作文档

- [task-runtime/00-overview.md](../task-runtime/00-overview.md) — TaskRuntime BC 入口（含 ReconcileService / DispatchService 协议视图）
- [task-runtime/02-task-execution.md § 9-12](../task-runtime/02-task-execution.md) — worker 端运行时（per-execution 实施细节）
- [discussion/00-overview.md](../discussion/00-overview.md) — Issue 引用 project_id
- [conversation/01-conversation.md](../conversation/01-conversation.md) — Identity / ChannelBinding（用户跟 vendor 的关联，跟 Worker 是不同维度）
- [cognition/01-supervisor-model.md](../cognition/01-supervisor-model.md) — Supervisor 在 proposal 决策中的角色
- [bridge/01-feishu-integration.md](../bridge/01-feishu-integration.md) — Proposal 飞书卡片渲染

### 横切方法论

- [conventions](../../../../rules/conventions.md) § 0 DDD / § 1 无野任务（Worker / Agent 不允许造任务）/ § 9 dialect-agnostic / § 13 安全（bootstrap token / session token）
