# 0029. Supervisor as Built-in AgentInstance

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-22 |
| Related | **Amends** [ADR-0024 AgentInstance 一等公民化](0024-agent-instance-first-class.md)；跟 [ADR-0003 Supervisor not brain](../0003-supervisor-not-brain.md) / [ADR-0013 Supervisor Invocation Concurrency](../0013-supervisor-invocation-concurrency.md) / [ADR-0028 Skill File Mount](0028-skill-file-mount-lite.md) 协同 |

## Context

[ADR-0024](0024-agent-instance-first-class.md) (G1) 把 worker agent 升级为 first-class `AgentInstance` AR。但 Supervisor 没在 AgentInstance 模型内 —— 它继续作为 Cognition BC 的「概念」由 `SupervisorInvocation` 表示，跟 worker agent 在模型层**完全分开**。

讨论中（2026-05-22 #agent-center）用户提出：

> 我觉得没有必要将 supervisor 和其他 agent 区别开 ，supervisor 是 builtin 的 AgentInstance 仅创建流程不一样 其他应该是一样的。

观察起来确实如此：

- Supervisor 是 agent（[ADR-0003](../0003-supervisor-not-brain.md)）
- 它有 skill（bundled `supervisor.md`）、有 prompt（事件触发时拼装）、有 memory（[ADR-0012](../0012-memory-file-based.md)）—— 都是 agent 该有的东西
- 跟 worker agent 真正的差异只在「创建流程」（system auto-provisioned）和「执行机器」（center 而非 worker）

如果不统一，[ADR-0028 G5 skill mount](0028-skill-file-mount-lite.md) 对 supervisor 需要单独走机制；[ADR-0027 G4 MCP per-agent](0027-mcp-per-agent-injection.md) 在 supervisor 上失效；用户「装 skill 给 supervisor」要走另一条路。

## Decision

**Supervisor = Built-in AgentInstance**。除创建 + 执行机器外，全部对齐 [ADR-0024](0024-agent-instance-first-class.md) 现有模型。

### 1. AgentInstance schema 加 `is_builtin` 字段

```diff
 agent_instance (
   id                ULID
   name              str          -- 全局唯一
   agent_cli         enum
-  worker_id         FK → workers (不可变)
+  worker_id         FK → workers (NULLABLE when is_builtin=true)
   config            JSON
   max_concurrent    int?
   state             enum         -- idle | active | sleeping | archived
+  is_builtin        bool         -- TRUE for system-provisioned (e.g., supervisor)
   created_at        timestamp
   archived_at       timestamp?
   version           int
 )

 CHECK (
   (is_builtin = false AND worker_id IS NOT NULL) OR
   (is_builtin = true  AND worker_id IS NULL)
 )
```

> `worker_id` 设 NULLABLE 而非新建一个「center」 special Worker 行 —— 简化模型，不污染 Workforce.workers 表语义（Worker = 用户开发机 daemon）。

### 2. Built-in supervisor AgentInstance auto-provisioning

Center 首次启动 / migration up 时，检查 `agent_instances` 是否存在 `name='supervisor' AND is_builtin=true`；不存在则插入：

```
INSERT INTO agent_instances (id, name, agent_cli, worker_id, config, max_concurrent, state, is_builtin)
VALUES (
  ULID(),
  'supervisor',
  'claude-code',  -- v2 默认；未来可通过部署配置改
  NULL,
  '{}',
  NULL,
  'idle',
  TRUE
)
```

CLI / API 角度可见，但 archive 路径拒绝（详 § 5）。

### 3. home_dir 路径分支

| Actor | home_dir |
|---|---|
| Worker AgentInstance (`is_builtin=false`) | `~/.agent-center-worker/agents/<id>/`（worker 机）|
| Built-in supervisor AgentInstance (`is_builtin=true`, `name='supervisor'`) | `~/.agent-center/agents/<name>/` = `~/.agent-center/agents/supervisor/`（center 机）|

用 `name` 而非 `id` 作 built-in 路径锚点：路径稳定 + 可读 + 部署脚本可预测。

### 4. State machine 复用

`idle / active / sleeping / archived` 完全一样：

- supervisor `idle`：center 进程 idle，无 invocation 在跑
- supervisor `active`：至少 1 个 SupervisorInvocation 在跑
- supervisor `sleeping`：（**新含义**）center 进程 down；下次启动自动 → `idle`
- supervisor `archived`：**built-in 禁止**（详 § 5）

> Worker 联动规则（worker offline → 该 worker 上 agent → sleeping）对 built-in 不适用（worker_id=NULL）。Supervisor 的 sleeping 由 center 进程 lifecycle 决定（非本 ADR 直接关心；通过 center heartbeat / 启动监测落地）。

### 5. Built-in AgentInstance 特殊 Invariants

- **`is_builtin=true` 时 `archived` 转移拒绝** —— `agent archive supervisor` 返回错误「system-provisioned agent cannot be archived」
- **`name` 不可改**（沿用 ADR-0024 invariant）
- **`is_builtin` 字段不可变**（创建后冻结）
- **同 name 不能创建新 built-in**（UNIQUE INDEX (name) 沿用）
- **`worker_id` 不可从 NULL 改为 not-null** 或反之（CHECK constraint 配 is_builtin 保证）

### 6. SupervisorInvocation 加 `agent_instance_id` 强引用

```diff
 supervisor_invocations (
   id                ULID
+  agent_instance_id FK → agent_instances (强引用，指向 built-in supervisor)
   triggered_at      timestamp
   trigger_event_ids JSON
   scope             str
   prompt_blob_ref   BlobRef
   output_blob_ref?  BlobRef
   exit_status       enum
   ...
 )
```

`agent_instance_id` 用来：

- 给 `cognition.supervisor_invocations` ←→ `workforce.agent_instances` 建强关联
- `agent show supervisor` 能反查最近 invocations
- 未来若引入多 supervisor agent（v3+），区分 invocation 归哪个 agent

> v2 只有一个 built-in supervisor，所以 `agent_instance_id` 实际上恒指向同一 row。但 schema 留出多 supervisor 可能。

### 7. CLI 表现

| 命令 | 对 built-in supervisor 的行为 |
|---|---|
| `agent list` | 列出含 `supervisor` 行；标记 `[built-in]` |
| `agent show supervisor` | 显示 config / state / current_executions_count（=活跃 invocations 数）/ home_dir 路径 / attached skills（扫 home_dir/skills/）/ recent invocations |
| `agent config set supervisor mcp_config=<json>` | 允许（supervisor 也能配 MCP） |
| `agent config set supervisor max_concurrent=<n>` | 允许（覆盖默认 [ADR-0013](../0013-supervisor-invocation-concurrency.md) 全局 cap，谨慎） |
| `agent archive supervisor` | **拒绝**：`✗ system-provisioned agent 'supervisor' cannot be archived` |
| `agent create --name=supervisor ...` | **拒绝**：name 冲突 |
| `agent create --name=foo --is-builtin=true ...` | **拒绝**：is_builtin 字段不开放给 CLI，仅系统设置 |

### 8. 跟 Prompt Assembly 的关系

[agent-harness/01-prompt-assembly](../../architecture/tactical/agent-harness/01-prompt-assembly.md) 现有「supervisor side / worker side 两套独立」的描述维持：

- Supervisor prompt：bundled `supervisor.md` + memory ancestor walk + wake event payload + **本 ADR 后新增**：home_dir/skills/<n>/SKILL.md（用户挂的额外 skill）+ home_dir/instructions.md（用户给 supervisor 写的个性化指令）+ home_dir/mcp_config.json（[ADR-0027](0027-mcp-per-agent-injection.md) per-supervisor MCP）
- Worker prompt：bundled `worker-agent.md` + home_dir/instructions.md + home_dir/skills/ + constraints_extra + task_description + ...

→ 「两套独立」是**实际处理流程**不同（事件触发 vs envelope 驱动；center 进程 vs worker daemon），但**用户接口面**统一：每个 AgentInstance（含 built-in supervisor）有同样的 home_dir + skill mount + MCP config 机制。

### 9. v3+ Roadmap

> Multi-supervisor agent（不同 name / 不同 config 的多个 supervisor，分别处理不同 wake scope）；Built-in agent 类型扩展（除 supervisor 外可能加 ingestion / cleanup 等系统 agent）；AgentInstance.is_builtin 给后续 built-in 类型留接口。

## Consequences

**正面**：

- AgentInstance 模型统一覆盖 worker + supervisor；用户认知一致
- G4 (MCP) / G5 (skill mount) 直接复用到 supervisor，零额外代码路径
- `agent` CLI 命令面给 supervisor 一视同仁（除 archive）
- Cognition BC 通过 `supervisor_invocations.agent_instance_id` 跟 Workforce 建立强关联
- 用户给 supervisor 装 skill 直接走「编辑 home_dir/skills/<n>/SKILL.md」 文件操作，跟 worker agent 同
- 为 v3+ multi-supervisor / 其他 built-in 类型留好接口

**负面 / 待跟进**：

- `worker_id NULLABLE + is_builtin` 引入 CHECK constraint；DB 层多一道校验
- 现有 `SupervisorInvocation` 表加 `agent_instance_id` 列；migration 一次（v2 不考虑向后兼容）
- 部分文档需调整描述（agent-harness/01-prompt-assembly / cognition/00-overview / strategic/03-bounded-contexts UL）
- Supervisor 进入 `agent list` 显示，用户可能误以为它跟 worker agent 完全平等 —— 通过 `[built-in]` 标记 + archive 拒绝错误信息缓解

## Alternatives Considered

### A. Supervisor 保留独立概念（不进 AgentInstance 模型）

- ✅ 边界清楚（Cognition vs Workforce）
- ❌ G4 / G5 等所有 per-agent 机制要单独为 supervisor 重做一遍；代码路径分叉
- ❌ 用户接口面分裂（`agent skill attach` 不对 supervisor 适用）
- 否决：跟用户「不必区别开」明确表态冲突

### B. Center 自己注册成特殊 Worker（`worker_id="center"` 行）

- 让 supervisor `worker_id='center'`，Worker 表多一行
- ✅ AgentInstance.worker_id NOT NULL 不变
- ❌ Worker AR 的 UL 是「用户开发机 daemon」，加 center 行污染语义；Worker 上的 capabilities / discovery / mapping 等机制对 center 无意义
- 否决：`worker_id NULLABLE` 是更小的扰动

### C. is_builtin 不进 AgentInstance，单独「BuiltinAgent」AR

- 新建 `BuiltinAgent` AR，跟 AgentInstance 是兄弟
- ❌ 用户「supervisor 是 builtin 的 AgentInstance」明确说是同一类
- ❌ 模型分裂导致 G4/G5 也要做两遍
- 否决

### D. v2 不做 supervisor 统一，留 v3+

- 维持 Supervisor 跟 worker agent 分离
- ❌ 用户已表态要统一
- ❌ G4/G5 完成时若没统一，supervisor 不能受益
- 否决

## References

### Amends

- [ADR-0024 AgentInstance 一等公民化](0024-agent-instance-first-class.md) — 加 `is_builtin` + `worker_id` NULLABLE + 路径分支 + invariants 扩展

### 相关 ADR

- [ADR-0003 Supervisor not brain](../0003-supervisor-not-brain.md)（Supervisor 是 agent）
- [ADR-0012 Memory file-based](../0012-memory-file-based.md)（supervisor memory 不动；跟 home_dir 是不同概念）
- [ADR-0013 Supervisor Invocation Concurrency](../0013-supervisor-invocation-concurrency.md)（max_concurrent 参考）
- [ADR-0019 BC 合并](../0019-bc-scheduling-execution-merged-to-task-runtime.md)
- [ADR-0027 MCP per-agent 注入](0027-mcp-per-agent-injection.md)（supervisor 也能用）
- [ADR-0028 Skill File Mount](0028-skill-file-mount-lite.md)（supervisor 也用 home_dir/skills/）

### 跨 BC

- [workforce/04-agent-instance.md](../../architecture/tactical/workforce/04-agent-instance.md) — schema 加 is_builtin
- [workforce/00-overview.md](../../architecture/tactical/workforce/00-overview.md) — UL + invariants
- [cognition/00-overview.md](../../architecture/tactical/cognition/00-overview.md) — SupervisorInvocation 加 agent_instance_id
- [agent-harness/01-prompt-assembly](../../architecture/tactical/agent-harness/01-prompt-assembly.md) — supervisor 段添加 home_dir 引用

### 来源

- 2026-05-22 #agent-center 讨论
