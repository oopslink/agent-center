# 0024. AgentInstance 一等公民化

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-22 |
| Related | v2 议题 G1（[v2-kickoff-2026-05-22 § Group 2](../../drafts/v2-kickoff-2026-05-22.md)）；与 [ADR-0008](../0008-worker-project-mapping-via-discovery-proposal.md)（mapping 维度不动）+ [ADR-0023](0023-worker-enroll-lightweight.md)（Worker 配置 + capabilities 模型）协同；TaskExecution 字段重命名见 [task-runtime/02-task-execution.md § 5](../../architecture/tactical/task-runtime/02-task-execution.md) |

## Context

v1 里「agent」是**隐式概念**：

- agent 不是任何 BC 的 AR / Entity / VO
- 仅以 `TaskExecution.agent_kind = claude-code | codex | opencode` 表达**类型**
- 每个 TaskExecution 都拉起一个独立的 agent CLI 子进程（per-execution shim，[ADR-0018](../0018-detached-agent-via-per-execution-shim.md)），execution 结束 → 进程退出 → 销毁 worktree
- **没有「我的 agent 叫 `coder-mbp`、它在 Worker `home-mbp` 上、配了某套 instructions」这种持久关系**

[竞品报告 § 3.7](../../../research/competitive-analysis-2026-05-21.md) 已分析：Slock 把 agent 做成 first-class identity（绑到 (server, computer)），用户可命名、配置、动态创建。agent-center 的 v1 隐式模型让以下场景表达不出来：

- 「让叫 `coder-mbp` 的那个 agent 接这个 task」——当前只能说「派给 Worker X 的 claude-code」
- 「我的 `reviewer-mbp` 配了一套审 PR 的 instructions」——当前 instructions 仅 per-task 通过 envelope 注入，无 agent-level 沉淀
- 「同时让 `coder-mbp` 并行干 3 个特性」——当前 dispatch 看不到「agent」这一维，只看 worker × CLI

v2 thesis「Agent 模型 的优化和重构」需要把 agent 升级为聚合实体，作为 G2 (`agent:create`) / G4 (MCP per-agent) / G5 (Skill file-mount) 的共同基石。

## Decision

新增 **`AgentInstance`** 作为 Workforce BC 的**独立 AR**，作为 agent 一等公民的实体表达。

### 1. AgentInstance AR 字段

```
agent_instance (
  id                ULID         -- 主键
  name              str          -- 全局唯一（v2 单租户），用户起名（如 "coder-mbp"）
  agent_cli         enum         -- claude-code | codex | opencode | ...（v1 沿用枚举）
  worker_id         FK → workers (v2 不可变；E2 后跨节点迁移再开）
  config            JSON         -- { instructions_ref?, mcp_config?, skills?, ... }
                                 -- G1 仅约定 instructions（home_dir/instructions.md）；G4 加 mcp_config；G5 加 skills；其余字段对应候选未来填
  max_concurrent    int?         -- null = 无 cap，受 Worker cap 兜底；=1 退化为 1:1 严格模式
  state             enum         -- idle | active | sleeping | archived
  created_at        timestamp
  archived_at       timestamp?
  version           int          -- 乐观锁
)

UNIQUE INDEX (name)
INDEX (worker_id, state)
```

### 2. 计算字段（不存 DB）

- `home_dir` = `~/.agent-center-worker/agents/<id>/`（约定推导，跟 `exec_base_dir` 同级）
- `current_executions_count` = `SELECT count(*) FROM task_executions WHERE agent_instance_id=? AND status IN ('submitted','working','input_required')`

### 3. 状态机

```
              created
                ↓
              idle ←──→ active   (active = current_executions_count ≥ 1)
                ↓
              sleeping            (worker → offline 时同步进；worker → online 时自动回 idle)
                ↓
              archived            (terminal；用户软删；不可逆)
```

**状态转移触发**：

| 转移 | 触发 |
|---|---|
| `created → idle` | 创建后默认 |
| `idle → active` | 第一个 TaskExecution 进入 submitted/working |
| `active → idle` | 最后一个 active TaskExecution 进入终态 |
| `idle/active → sleeping` | 所属 worker → offline |
| `sleeping → idle` | 所属 worker → online（自动；reconcile 完成后） |
| `idle → archived` | 用户软删（只允许从 idle；active / sleeping 时禁止） |

### 4. 并发模型（1:N + 两层 cap）

| Cap | 落处 | 默认 |
|---|---|---|
| Worker 层 | `Worker.concurrency.per_agent_type` | 2（单 CLI 类型最大并跑） |
| AgentInstance 层 | `AgentInstance.max_concurrent` | null = 不另设上限 |

Dispatch 时校验链：

```
dispatch (task → agent_instance_id):
  1. agent_instance.state ∈ {idle, active} ?
  2. agent_instance.agent_cli ∈ worker.capabilities[detected ∧ enabled] ?
  3. count(active executions on agent_instance) < min(
       Worker.concurrency.per_agent_type,
       AgentInstance.max_concurrent ?? ∞
     ) ?
  全过 → 起新 TaskExecution；否则 NACK / 排队
```

### 5. 持久工作目录（home_dir）

约定路径：`~/.agent-center-worker/agents/<agent_instance_id>/`

**G1 范围**仅约定 home_dir 包含：

```
~/.agent-center-worker/agents/<id>/
   ├─ instructions.md     # G1: agent-level system prompt 片段（worker daemon 拼装时叠加）
   └─ notes/              # 用户随意（agent 不该写）
```

`mcp_config.json` / `skills/` 等结构留口给 G4 / G5 时填。

**并发约束**：execution 期间 agent 进程对 home_dir **只读**。写 home 是 between-execution 行为（避免多 execution 并发写 race）。

### 6. TaskExecution schema 变更（直接替换，无后向兼容）

| 字段 | v1 | v2 |
|---|---|---|
| `agent_kind` / `agent_cli`（旧）| 存 enum 表达类型 | ❌ 删字段 |
| `agent_instance_id` | ❌ | ✅ 强引用 AgentInstance（不可变；派单契约）|

派单时 `agent_cli` 通过 `JOIN agent_instances ON task_executions.agent_instance_id = agent_instances.id` 拿。

DispatchEnvelope 同步改：`agent_cli` → `agent_instance_id`；worker 端 prompt-assembly 拿到 instance_id 后查本机 home_dir/instructions.md 叠加进 prompt 层次（[agent-harness/01-prompt-assembly.md](../../architecture/tactical/agent-harness/01-prompt-assembly.md) 新增一层）。

### 7. 跟 Project / WorkerProjectMapping 关系（不动）

- WorkerProjectMapping 仍是 `(worker_id, project_id) → base_path`，**不加 agent 维度**
- agent 跟 project **不直接关联**；dispatch 时 supervisor 选 (agent_instance, task) 组合，task 自带 project_id
- 理由（用户原话）：**「project 和 agent 是两个维度的实体」**；mapping 是文件系统位置，跟 agent 逻辑身份无关

未来如果出现「per-agent project 允许列表」需求，再加 `agent_project_access (agent_instance_id, project_id)` 关联表；**G1 不预留字段**。

### 8. 新增事件

| 事件 | 触发 | payload 关键字段 |
|---|---|---|
| `agent_instance.created` | 新建（CLI 或 G2 `agent:create` 协议）| id, name, agent_cli, worker_id, config |
| `agent_instance.config_updated` | 改 config / max_concurrent | id, changed_fields, by |
| `agent_instance.activated` | idle → active（第一个 execution 进入）| id |
| `agent_instance.idle` | active → idle（最后一个 execution 完）| id |
| `agent_instance.sleeping` | worker offline → 同步进 sleeping | id, worker_id |
| `agent_instance.awakened` | worker online → 同步出 sleeping | id, worker_id |
| `agent_instance.archived` | 用户软删（terminal）| id, archived_by |

`worker.online` / `worker.offline` 事件除原有逻辑外，新增触发 supervisor 处理「所属 AgentInstance 跟着进 / 出 sleeping」。

### 9. AgentInstance Invariants

1. **name 全局唯一**（v2 单租户；UNIQUE INDEX 强制）
2. **worker_id 不可变**（v2 范围；跨节点迁移留给 E2 / v3）
3. **archived 终态不可逆**
4. **state 跟 worker 联动**：worker offline → agent sleeping；worker online → agent 回 idle
5. **home_dir 在 execution 期间对 agent 进程只读**（应用层约束；agent 想沉淀经验走 supervisor memory [ADR-0012] 或 task artifact 渠道）
6. **archived 时禁止 active**：active / sleeping 状态不能直接进 archived，需先 idle

### 10. CLI

| 命令 | 用途 |
|---|---|
| `agent-center agent create --name=<n> --worker=<id> --agent-cli=<cli> [--max-concurrent=<n>]` | 创建（也是 G2 `agent:create` 协议的实现底）|
| `agent-center agent list [--worker=<id>] [--state=<s>]` | 列 |
| `agent-center agent show <name>` | 详情（含 current_executions_count）|
| `agent-center agent config set <name> <key>=<value>` | 改 config / max_concurrent |
| `agent-center agent archive <name>` | 软删（要求 state=idle）|

## Consequences

**正面**：

- agent 真正成为一等公民，supervisor 派单语义从「派给 Worker X 的 claude-code」升为「派给 agent `coder-mbp`」
- `coder-mbp` 跨 task 持久身份 + agent-level instructions 沉淀
- 用户场景「让一个 agent 并行干 3 件事」直接可表达（1:N + 两层 cap）
- G2 `agent:create` 协议有 AgentInstance.created 事件 + create CLI 作为协议底
- G4 MCP per-agent 注入有 `AgentInstance.config.mcp_config` 字段挂点
- G5 Skill file-mount 有 home_dir/skills 挂点

**负面 / 待跟进**：

- 新增聚合 + 表（`agent_instances`）+ Repository；DDD 模型 + ADR 数 + 测试范围都涨
- TaskExecution 字段重命名 (`agent_cli` → `agent_instance_id`) 影响所有相关 SQL / API / 序列化层（按「不考虑向后兼容」原则一次性改完）
- DispatchEnvelope schema bump（envelope_version: v1 → v2）
- prompt-assembly 加一层（agent-level instructions），worker daemon 拼装代码路径变长
- agent state 跟 worker state 的同步要保证一致（worker.offline → 该 worker 上所有 agent → sleeping）
- 用户冷启动认知负担：必须先 `agent create` 才能派 task（v1 直接派给 Worker + CLI 即可）；缓解：CLI 提供「auto-create default agent」便利命令 + 错误提示引导

## Alternatives Considered

### A. AgentInstance 作为 Worker AR 的子从属 Entity

- 不独立 AR，挂在 Worker 下面（类似 WorkerProjectMapping 的位置）
- ✅ 模型简单
- ❌ agent 跟 Worker 强绑；删 Worker = 删 agent；状态机不能独立演化
- ❌ 跟 Slock「first-class identity」思路偏离
- 否决：G1 thesis 要的就是「first-class」

### B. AgentInstance.name 局部唯一（(worker_id, name)）

- 不同 worker 上允许同名 agent
- ✅ 多节点场景下不冲突
- ❌ v2 单租户 / 个人工具无此压力；用户喊「coder-mbp」无歧义性需求
- ❌ supervisor 派单时要 `(worker_id, name)` 双键定位，增加复杂度
- 否决：v2 用全局唯一足够

### C. capability 被 AgentInstance 取代

- 不要 Worker.capabilities 数组，AgentInstance 直接挂在 Worker 下
- ✅ 概念合并少一层
- ❌ 机器能力（CLI 装没装）跟 agent 配置（用什么 CLI + 啥 instructions）是两件事；合并会丢失「这台机器没装 codex」这种基础信息
- 否决：两层共存（Q3 决定）

### D. 严格 1:1（AgentInstance 同时至多 1 个 active execution）

- ✅ 状态机干净；跟 v1 进程级模型最接近
- ❌ 「同一 agent 并行干 N 件事」用例直接表达不出（详见 Q4 case study）
- ❌ 强迫用户用多 agent 实例表达本质是「同一个 agent 多任务」的场景，命名学体验差
- 否决：(ii) 1:N 默认 + 可选 max_concurrent 兼顾灵活性

### E. WorkerProjectMapping 升级为 AgentProjectMapping

- mapping 维度加 agent，废 WorkerProjectMapping
- ✅ 跟 Slock「agent + project」直绑模型对齐
- ❌ base_path 本质是机器文件系统属性，挂 agent 维度概念错位
- ❌ project + agent + worker 三维 ↔ mapping 维度爆炸
- 否决：(a) WPM 不动

## References

### 同 BC

- [workforce/04-agent-instance.md](../../architecture/tactical/workforce/04-agent-instance.md) —— AgentInstance AR 详细模型（新建）
- [workforce/00-overview.md](../../architecture/tactical/workforce/00-overview.md) —— BC 入口（含 Domain Services / 跨 BC）
- [workforce/01-worker.md](../../architecture/tactical/workforce/01-worker.md) —— Worker AR（capabilities 字段；agent 状态联动）

### 跨 BC

- [task-runtime/02-task-execution.md § 5](../../architecture/tactical/task-runtime/02-task-execution.md) —— TaskExecution.agent_instance_id 字段 + DispatchEnvelope v2 schema
- [task-runtime/00-overview.md § 3.1](../../architecture/tactical/task-runtime/00-overview.md) —— DispatchService 校验链调整
- [agent-harness/01-prompt-assembly.md](../../architecture/tactical/agent-harness/01-prompt-assembly.md) —— prompt-assembly 加 agent-level instructions 层

### 相关 ADR

- [ADR-0008](../0008-worker-project-mapping-via-discovery-proposal.md) —— Mapping 维度不变
- [ADR-0011](../0011-dispatch-reliability-protocol.md) —— DispatchEnvelope schema bump
- [ADR-0018](../0018-detached-agent-via-per-execution-shim.md) —— per-execution shim 模型沿用
- [ADR-0023 草案](0023-worker-enroll-lightweight.md) —— Worker 配置 + capabilities 模型

### 来源

- [docs/design/drafts/v2-kickoff-2026-05-22.md § Group 2 G1](../../drafts/v2-kickoff-2026-05-22.md)
- [docs/research/competitive-analysis-2026-05-21.md § 3.7](../../../research/competitive-analysis-2026-05-21.md)
