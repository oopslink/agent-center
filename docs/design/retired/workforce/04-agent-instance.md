> 🗑️ **TaskRuntime BC 已退役（v2.7 #131 carve-out）。** 下文对 TaskRuntime 的引用（per-execution 运行时归属、DispatchService 派单校验、`TaskExecution.agent_instance_id` + DispatchEnvelope 等）为**历史记录、非当前架构**：派单 / 执行语义已由 pm.Task + agent-work-item 模型承接。链接指向 `docs/design/retired/`。AgentInstance 实体 / 枚举 / ManagementService 本身在 v2.7 保留。当前架构见 sites/designs/v2.7/；按新模型重写见 task #144-b。

# AgentInstance 聚合（独立 AR）

> **DDD 战术层** · BC: Workforce · 聚合: AgentInstance（独立 AR）
>
> 设计依据：[ADR-0024 AgentInstance 一等公民化](../../../decisions/0024-agent-instance-first-class.md)

AgentInstance 把「agent」从 v1 的隐式概念（仅 `TaskExecution.agent_kind` 类型枚举）升级为**持久身份 + 配置 + 状态机**的一等公民。

> **RETIRED / historical：本聚合不管「怎么干活」** —— per-execution 运行时 / shim / workspace 物理 / Agent CLI 子进程 / JSONL 解析 / Artifact / kill 进程级机制都归 [TaskRuntime BC](../../../retired/task-runtime/02-task-execution.md)。AgentInstance 是逻辑身份层，**不是进程**。

---

## § 1. AgentInstance 状态机

```mermaid
stateDiagram-v2
    [*] --> idle: created
    idle --> active: 第一个 execution 进入<br/>submitted/working
    active --> idle: 最后一个 active execution<br/>进入终态
    idle --> sleeping: 所属 worker → offline
    active --> sleeping: 所属 worker → offline
    sleeping --> idle: 所属 worker → online<br/>(reconcile 完成)
    idle --> archived: 用户软删
    archived --> [*]
    note right of active
        active = current_executions_count ≥ 1<br/>(数量由 query 拿，不存 state)
    end note
    note right of sleeping
        非终态；worker 重连后自动回 idle
    end note
    note right of archived
        terminal；不可逆<br/>必须先 idle 才能 archive
    end note
```

---

## § 2. 模型

```
agent_instance (
  id                ULID         -- 主键
  name              str          -- 全局唯一（v2 单租户），用户起名 / built-in 由系统保留
  agent_cli         enum         -- claude-code | codex | opencode | ...
  worker_id         FK → workers (NULLABLE when is_builtin=true，否则不可变；ADR-0029)
  config            JSON         -- { instructions_ref?, mcp_config?, ... }
                                 -- G1 约定 instructions（home_dir/instructions.md）
                                 -- G4 约定 mcp_config（MCP 标准 schema + SecretRef，详 § 4）
                                 -- G5 约定 skills 走文件目录 home_dir/skills/（不进 config JSON）
  max_concurrent    int?         -- null = 无 cap；=1 → 严格 1:1
  state             enum         -- idle | active | sleeping | archived
  is_builtin        bool         -- v2 加 (ADR-0029)：TRUE = 系统 provisioned（如 supervisor）；archive 拒绝
  created_at        ISO8601 TEXT
  archived_at       ISO8601 TEXT, nullable
  version           int          -- 乐观锁
)

UNIQUE INDEX agent_instance_name_uq (name)
INDEX agent_instance_worker_state_idx (worker_id, state)

CHECK (
  (is_builtin = false AND worker_id IS NOT NULL) OR
  (is_builtin = true  AND worker_id IS NULL)
)
```

### 2.1 Built-in AgentInstance（v2 新增）

> 详见 [ADR-0029 Supervisor as Built-in AgentInstance](../../../decisions/0029-supervisor-as-builtin-agent-instance.md)

v2 默认有一个 built-in：

| 字段 | 值 |
|---|---|
| `name` | `supervisor`（保留名）|
| `is_builtin` | TRUE |
| `worker_id` | NULL（不在任何 Worker 上跑，跑在 center 进程）|
| `agent_cli` | `claude-code`（默认；可通过部署配置）|
| 进程触发 | Cognition wake scheduler（事件驱动），不是 TaskExecution dispatch |
| `archived` 转移 | **禁止**（CLI / API 层拒绝）|

→ 用户通过 `agent show supervisor` 查看；可 `agent config set supervisor mcp_config=...` 等同 worker agent；唯一拒绝的是 `agent archive supervisor`。

### 计算字段（不存 DB）

| 字段 | 计算 |
|---|---|
| `home_dir`（worker AgentInstance）| `~/.agent-center-worker/agents/<id>/`（worker 机本地约定路径）|
| `home_dir`（built-in supervisor，[ADR-0029](../../../decisions/0029-supervisor-as-builtin-agent-instance.md)）| `~/.agent-center/agents/<name>/`（center 机；用 `name` 而非 `id` 保证稳定可读）|
| `current_executions_count`（worker）| `SELECT count(*) FROM task_executions WHERE agent_instance_id=? AND status IN ('submitted','working','input_required')` |
| `current_executions_count`（built-in supervisor）| `SELECT count(*) FROM supervisor_invocations WHERE agent_instance_id=? AND exit_status IS NULL`（活跃 invocations）|

---

## § 3. Home Directory（持久工作目录）

### 3.1 路径约定

Worker AgentInstance（worker 机）：

```
~/.agent-center-worker/agents/<agent_instance_id>/
   ├─ instructions.md          # G1: agent-level system prompt 片段
   ├─ mcp_config.json          # G4: MCP 标准 schema + SecretRef（无明文）
   ├─ mcp_config.runtime.json  # G4: 派单时由 daemon just-in-time 生成（含明文，mode 0600）；execution 后 unlink
   ├─ skills/                  # G5 (ADR-0028): 用户自配 skill 文件，按 Anthropic Skills 标准
   │    └─ <skill-name>/SKILL.md
   └─ notes/                   # 用户随意；agent 进程在 execution 期间只读
```

Built-in supervisor AgentInstance（center 机，[ADR-0029](../../../decisions/0029-supervisor-as-builtin-agent-instance.md)）：

```
~/.agent-center/agents/supervisor/
   ├─ instructions.md          # 用户给 supervisor 写的个性化指令（叠加 bundled supervisor.md）
   ├─ mcp_config.json          # supervisor 也可配外部 MCP server（G4 同样适用）
   ├─ mcp_config.runtime.json  # center 进程解析 SecretRef 后短暂落盘
   ├─ skills/                  # 用户给 supervisor 装的额外 skill（叠加 bundled supervisor.md）
   │    └─ <skill-name>/SKILL.md
   └─ notes/
```

> Worker daemon 跟 center supervisor 进程都按同样的 home_dir 结构装载 skill / instructions / mcp_config。

详见 [ADR-0027 MCP per-agent 注入](../../../decisions/0027-mcp-per-agent-injection.md) + [ADR-0026 SecretManagement BC](../../../decisions/0026-user-secret-management-bc.md) + [ADR-0028 Skill File Mount](../../../decisions/0028-skill-file-mount-lite.md)。

### 3.2 写权限约束

- **execution 期间**：agent 进程对 home_dir **只读**
- **between-execution**：worker daemon 或用户可写（更新 instructions、装 skill）
- 多 execution 并发跑同一 agent 时不可能同时变更 home，避免文件 race

### 3.3 跟 worktree 的关系

worktree（[task-runtime/02-task-execution § 8](../../../retired/task-runtime/02-task-execution.md)）是 per-execution 临时沙箱；home_dir 是持久。worker daemon 在 prompt-assembly 阶段把 `home_dir/instructions.md` 内容**叠加进 prompt 层次**（详见 [agent-harness/01-prompt-assembly.md](../agent-harness/01-prompt-assembly.md)），而不是把 home 文件挂到 worktree 内。两个目录互不污染。

---

## § 4. 并发模型（1:N + 两层 cap）

### 4.1 并发上限

| Cap | 落处 | 默认 |
|---|---|---|
| Worker 层 | `Worker.concurrency.per_agent_type` | 2（单 CLI 类型最大并跑） |
| AgentInstance 层 | `AgentInstance.max_concurrent` | null = 不另设上限，受 Worker 层兜底 |

### 4.2 派单校验链

RETIRED / historical：DispatchService（[task-runtime/00-overview § 3.1](../../../retired/task-runtime/00-overview.md)）派单时按顺序校验：

```
dispatch (task → agent_instance_id) {
  1. agent_instance.state ∈ {idle, active} ?
       否 → NACK reason=agent_unavailable
  2. agent_instance.agent_cli ∈ worker.capabilities[detected ∧ enabled] ?
       否 → NACK reason=capability_missing
  3. count(active executions on agent_instance) < min(
         worker.concurrency.per_agent_type,
         agent_instance.max_concurrent ?? ∞
     ) ?
       否 → NACK reason=agent_at_capacity（supervisor 决定排队 or 派给别的 agent）
  全过 → 起新 TaskExecution + DispatchEnvelope 下发
}
```

### 4.3 同 agent 并行 execution 的边界

| 项 | 共享 / 不共享 |
|---|---|
| `home_dir/instructions.md` | ✅ 读共享（execution 期间只读，不存在 race）|
| `home_dir/mcp_config.json` | ✅ 读共享（G4 后定）|
| `home_dir/skills/` | ✅ 读共享（G5 后定）|
| `home_dir/notes/` | ⚠️ 读共享，写仅 between-execution |
| worktree | ❌ 各自独立（per-execution shim 沿用）|
| agent in-process working memory | ❌ 各自独立 |
| TaskExecution trace / log | ❌ 各自独立（一条 trace 一个 execution）|

---

## § 5. AgentInstance 跟 Worker 的状态联动

| Worker 状态变化 | AgentInstance 自动行为 |
|---|---|
| worker → online | 该 worker 上 state=sleeping 的 agent 全部 → idle |
| worker → offline | 该 worker 上 state ∈ {idle, active} 的 agent 全部 → sleeping |

实现：监听 `worker.online` / `worker.offline` 事件，在 Workforce BC 内部触发 AgentInstance state 转移（同事务双写或异步事件驱动，详见 [00-overview § 3.x AgentInstanceLifecycleService](00-overview.md)）。

---

## § 6. CLI

| 命令 | 用途 |
|---|---|
| `agent-center agent create --name=<n> --worker=<id> --agent-cli=<cli> [--max-concurrent=<n>]` | 创建（仅 worker AgentInstance；name 撞 built-in 保留名拒绝）|
| `agent-center agent list [--worker=<id>] [--state=<s>] [--builtin=<bool>]` | 列；built-in 行标 `[built-in]` |
| `agent-center agent show <name>` | 详情（含 current_executions_count + home_dir 路径 + attached skills 扫 home_dir/skills/）|
| `agent-center agent config set <name> <key>=<value>` | 改 config / max_concurrent；built-in 同样允许 |
| `agent-center agent archive <name>` | 软删（要求 state=idle；active 时拒绝；**built-in 直接拒绝**，[ADR-0029](../../../decisions/0029-supervisor-as-builtin-agent-instance.md)）|

> **创建命令的同机要求**：v2 默认 center 同机 / 远程均可（CLI 走 admin endpoint）。G2 `agent:create` 飞书卡片协议是基于此 endpoint 的 UX 包装。

---

## § 7. AgentInstance Invariants

1. **name 全局唯一**（v2 单租户；UNIQUE INDEX 强制；冲突时 create 返回 ErrAgentInstanceNameTaken）
2. **worker_id 不可变**（v2 范围；改 worker = 走 archive + 新建流程；E2 / v3 加迁移路径）—— built-in 的 worker_id=NULL 同样不可变（[ADR-0029](../../../decisions/0029-supervisor-as-builtin-agent-instance.md)）
3. **archived 终态不可逆**
4. **state 跟 worker 联动**（详 § 5；仅 worker AgentInstance；built-in supervisor 的 sleeping 由 center 进程 lifecycle 决定）
5. **home_dir 在 execution 期间对 agent 进程只读**（应用层约束；agent 想沉淀经验走 supervisor memory [ADR-0012](../../../decisions/0012-memory-file-based.md) 或 task artifact 渠道）
6. **archived 禁止 active / sleeping**：必须先 idle 才能 archive
7. **max_concurrent ≥ 1**（如设值；null 表"不另设"）
8. **is_builtin=true 时 archived 拒绝**（[ADR-0029](../../../decisions/0029-supervisor-as-builtin-agent-instance.md)）：built-in 是系统 provisioned，不允许 archive
9. **is_builtin 字段创建后冻结**：不允许 user 升级 / 降级
10. **CHECK constraint**：`(is_builtin=false AND worker_id IS NOT NULL) OR (is_builtin=true AND worker_id IS NULL)`

---

## § 8. 创建路径（v2 范围）

**Worker AgentInstance（`is_builtin=false`）**：CLI `agent-center agent create --name=<n> --worker=<id> --agent-cli=<cli> [--max-concurrent=<n>]`（远程 endpoint）。

**Built-in AgentInstance（`is_builtin=true`）**：仅由 **system auto-provisioning** 创建（center 启动 / migration up 时检查并 INSERT）。CLI `agent create` 不开放 `is_builtin` 字段；尝试 `--name=supervisor` 等保留名直接拒绝（name 冲突）。

未来 [G2 `agent:create` 协议](../../../drafts/v2-kickoff-2026-05-22.md) 走飞书卡片做 UX 包装，但调用的是同一 endpoint + 同一 Factory。

---

## § 9. 跨聚合关系

| 关系 | 强弱 | 备注 |
|---|---|---|
| `AgentInstance → Worker`（`agent_instance.worker_id`）| 强 / v2 不可变 | tx 同步；删 Worker 前置要求该 worker 上所有 agent 都 archived |
| `TaskExecution → AgentInstance`（`task_execution.agent_instance_id`）| 强 / 不可变 | 派单契约字段；冻结 |
| `AgentInstance ↔ Project` | **无直接关联** | dispatch 时由 supervisor 选 (agent, task with project) 组合 |
| `AgentInstance.agent_cli` 跟 `Worker.capabilities` | 派单时校验 | dispatch 阶段要求 agent_cli ∈ worker.capabilities[detected ∧ enabled] |

---

## § 10. 事件

| 事件 | 触发 | payload 关键字段 |
|---|---|---|
| `agent_instance.created` | 新建 | id, name, agent_cli, worker_id, config |
| `agent_instance.config_updated` | 改 config / max_concurrent | id, changed_fields, by |
| `agent_instance.activated` | idle → active | id |
| `agent_instance.idle` | active → idle | id |
| `agent_instance.sleeping` | worker offline 联动 | id, worker_id |
| `agent_instance.awakened` | worker online 联动 | id, worker_id |
| `agent_instance.archived` | 用户软删 | id, archived_by |

---

## § 11. References

- [ADR-0024 AgentInstance 一等公民化](../../../decisions/0024-agent-instance-first-class.md)
- [ADR-0026 SecretManagement BC](../../../decisions/0026-user-secret-management-bc.md)（config.mcp_config 内嵌 SecretRef）
- [ADR-0027 MCP per-agent 注入](../../../decisions/0027-mcp-per-agent-injection.md)（config.mcp_config schema）
- [00-overview.md](00-overview.md) — BC 入口（含 AgentInstanceRepository / Domain Services）
- [01-worker.md](01-worker.md) — Worker AR（capabilities 字段 + agent 状态联动）
- [task-runtime/02-task-execution.md § 5](../../../retired/task-runtime/02-task-execution.md) — TaskExecution.agent_instance_id 字段 + DispatchEnvelope v2 schema
- [agent-harness/01-prompt-assembly.md](../agent-harness/01-prompt-assembly.md) — prompt 层加 agent-level instructions + MCP 注入流程
- [secret-management/00-overview.md](../secret-management/00-overview.md) — SecretManagement BC
- [竞品报告 § 3.7](../../../../research/competitive-analysis-2026-05-21.md) — Slock Computer + Agent 模型对照
