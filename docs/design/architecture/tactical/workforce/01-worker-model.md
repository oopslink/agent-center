# Worker 模型

> **DDD 战术层** · BC: Workforce
>
> Worker daemon 是用户开发机上的常驻进程，负责注册到 center、管理 worker_project_mapping、上报心跳。
>
> **本文档限定 BC3 Workforce 内容**：Worker 注册 / heartbeat / WorkerProjectMapping / WorkerProjectProposal / Project 元数据 / worker daemon 进程自身（非 per-execution）的生命周期。
>
> **per-execution 运行时**（shim 模型 / Workspace 物理创建 / Agent CLI 子进程 / JSONL 解析 / per-execution 目录 / Reconcile worker 端 / kill 进程级机制 / Artifact / dispatch ACK 协议的 worker 端实施）已迁出到 [task-runtime/02-task-execution.md § 9-12](../task-runtime/02-task-execution.md)（按 [ADR-0019](../../../decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md) 合并）。

---

## § 1. 角色定位

- 不存权威状态，状态权威在 Center（Task / TaskExecution 状态以 [task-runtime/01-task.md](../task-runtime/01-task.md) 和 [task-runtime/02-task-execution.md](../task-runtime/02-task-execution.md) 为准）
- 不做决策，决策权在 Supervisor / 用户
- BC3 职责限定：**"谁能干活"的元数据管理** —— Worker 注册 / 在线状态 / 项目映射 / 候选发现
- **"怎么干活"**（执行 dispatch / 起 agent / 管 workspace / 上报 progress / Artifact）= TaskRuntime BC，详见 [task-runtime/02-task-execution.md](../task-runtime/02-task-execution.md)

---

## § 2. 注册与认证

- Worker 启动时凭 `worker.yaml` 里的 **bootstrap token** 连回 center
- Center 校验通过后给一个长期 **session token**
- Bootstrap token 通过 `agent-center worker enroll`（在 center 同机）签发

事件：
- `worker.enrolled` —— bootstrap → session 兑换成功
- `worker.online` —— 长连接建立 + reconcile 完成
- `worker.offline` —— 长连接断开 / 心跳超时
- `worker.heartbeat` —— 周期心跳

---

## § 3. Worker.yaml 形态

```yaml
worker:
  id: mac-mini-1
  bootstrap_token: ...
  center_endpoint: ...

concurrency:
  per_agent_type: 2     # 默认：同一 agent CLI 最多并跑 2 个 execution

discovery:
  scan_paths:                    # 扫这些路径找 git repo 作为候选项目
    - /Users/oopslink/code
    - /Users/oopslink/works
  exclude:                       # 排除 glob
    - "**/node_modules/**"
    - "**/vendor/**"
    - "**/.cache/**"
  scan_interval: 1h              # 周期扫；首次 enroll 后立刻扫一次

agent_cli:                       # 该 worker 支持的 agent CLI adapter
  - claude-code                  # v1 必须
  # - codex                      # 计划支持
  # - opencode                   # 计划支持
```

**注意**：

- **worker.yaml 不再列具体项目** —— 哪些项目能跑 = 哪些项目通过自动发现 + 用户确认成为了 `WorkerProjectMapping`
- `concurrency.per_agent_type` 是 worker 容量配置；每条 `task_execution` 的运行时调度归 [task-runtime/02-task-execution.md](../task-runtime/02-task-execution.md)
- `agent_cli` 列表是 worker 声明的能力（worker enroll 时上报 capabilities）；center 派单时按 `task.agent_cli` 字段匹配支持该 CLI 的 worker；具体 adapter 实现归 task-runtime BC（worker 端运行时章节）

---

## § 4. WorkerProjectMapping 创建与维护

### 4.1 设计原则

- **自动发现 + 用户确认**：worker 主动扫描候选；用户点 ✅ 才生效（避免随便建出无用 mapping）
- **流程对齐 Issue / InputRequest 模式**：候选作为 Proposal 进入系统，飞书卡片让用户决策
- **Worktree 是动态的**（详见 [task-runtime/02-task-execution § 8](../task-runtime/02-task-execution.md)）；mapping 表只存稳定的 `base_path`

### 4.2 数据模型概念

```
WorkerProjectProposal  (提议, 短期)
  id, worker_id,
  candidate_path,             -- /Users/oopslink/code/agent-center
  suggested_project_id,       -- 'agent-center' (worker 的猜测，常用 dir name)
  suggested_kind,             -- 'coding' / 'writing' / null (按 go.mod / package.json / 后缀启发式猜)
  candidate_metadata,         -- JSON: git_remote_url / commit_count / recent_activity_at / detected_language
  status,                     -- pending | accepted | ignored | superseded
  proposed_at, reviewed_at, reviewed_by,
  resulting_mapping_id        -- 若 accepted, 指向生成的 mapping

WorkerProjectMapping  (已生效, 稳定)
  worker_id,
  project_id,
  base_path,                  -- 主 checkout, 稳定
  source_proposal_id,         -- 血缘到 proposal
  added_at
  -- worktree_root: 不存, 约定 = base_path + ".wt"
```

具体 schema 见 [implementation/02-persistence-schema.md](../../../implementation/02-persistence-schema.md)（TBD）。

### 4.3 发现流程

```
1. Worker 周期扫 scan_paths (启动后 + 每 scan_interval):
   找出所有 .git 目录
   按 exclude glob 过滤

2. 对每个候选, worker 先查 center:
   "(worker_id, candidate_path) 见过吗?"
   - accepted: 跳过 (mapping 已有)
   - ignored : 跳过 (用户已拒绝)
   - pending : 跳过 (等用户审)
   - 未见过 : 走下一步

3. Worker emit WorkerProposedProjectMapping 事件:
   含 suggested_project_id (默认 = dir name)
   含 suggested_kind (启发式: go.mod → coding, manuscript/ → writing, ...)
   含 candidate_metadata (git remote, commit 统计等)

4. Center 入库 worker_project_proposals(status=pending)
   触发 supervisor 唤醒

5. Supervisor 决定如何呈现 (v1: 直接推飞书卡片):
   多条 proposal 可批量打包成一张卡片, 也可逐条

6. 飞书卡片:
   🔍 Worker mac-mini-1 发现候选项目:
       📁 /Users/oopslink/code/agent-center  (Go, 2.1k commits, github.com/.../agent-center)
       建议 project_id: agent-center
       建议 kind: coding
   [✅ 加入] [✏️ 改后加入] [❌ 忽略]

7. 用户点击:
   ✅ 加入:
     - 若 project 不存在: 自动创建 Project (用 suggested_project_id + suggested_kind)
     - 创建 WorkerProjectMapping(base_path=candidate_path, source_proposal_id=...)
     - proposal.status=accepted

   ✏️ 改后加入:
     - 弹卡片让用户编辑 project_id / name / kind / default_agent_cli
     - 提交后同 ✅

   ❌ 忽略:
     - proposal.status=ignored
     - worker 下次扫不再提
```

### 4.4 边界情况

#### 路径消失（mapping 中 base_path 不再有 .git）

Worker 扫到原 mapping 的 base_path 已不存在 / 不再是 git repo → emit `worker_project_mapping.invalidated` 事件。

Center 行为：

- 将该 mapping 标 `invalidated`（不实际删，保留血缘）
- 飞书提示用户："Worker X 上 project Y 的路径失效了（base_path 已不在），是否重新映射？"
- 不自动迁移（避免用户改路径正在测试时被系统错误处理）

#### 同一 project 被多 worker 发现

Worker A 已 accepted `agent-center → /Users/.../code/agent-center`。
Worker B 扫到自己本地 `/home/.../code/agent-center`，suggested_project_id 也是 `agent-center`。

Center 检测到 project 已存在 → 仍然推飞书：

```
Worker home-server 也发现 agent-center 项目:
  📁 /home/oopslink/code/agent-center

是否在该 worker 上也启用?
[✅ 启用 (默认)] [❌ 不启用]
```

默认选项是 ✅ —— 一键即可，避免无意义的二次确认。

#### 用户后悔忽略

`agent-center worker proposal unignore <proposal_id>` 把先前 ignored 的提议重置为 pending，下次 worker 扫到会再次提议（或 center 立即重新触发该提议的 supervisor flow）。

#### Project 自动创建的命名冲突

User 想 `accept` 一个 `suggested_project_id=foo` 的 proposal，但 center 里已有别的 project 叫 `foo`。

行为：飞书卡片标红，让用户改 project_id 后再提交（不允许同名）。

### 4.5 不做的事（v1）

- ❌ 跨 worker 自动"广播"已 accepted 的项目到其它 worker（除非该 worker 也自己扫到）
- ❌ 自动跟随路径移动（用户从 `/code/foo` 搬到 `/works/foo` → 必须重新接受 proposal）
- ❌ 提议合并 / 去重（每条候选独立提议）
- ❌ CLI 手动管理 mapping（运行时 add/remove 命令推迟到 [roadmap](../../../roadmap.md)）

详见 [ADR-0008](../../../decisions/0008-worker-project-mapping-via-discovery-proposal.md)。

---

## § 5. 心跳与上线

- Worker 长连接建立后周期 emit `worker.heartbeat`（含 `working_seconds_accumulated` 增量等容量信号）
- Center 端 `worker_heartbeat_timeout`（默认 60s）：心跳静默超时 → worker → offline；上面所有 active execution → failed(`worker_lost`)；详见 [task-runtime/02-task-execution § 7.3 timeout](../task-runtime/02-task-execution.md)
- Worker 重连流程（含 reconcile worker 端 active/stale/unknown 处理）归 [task-runtime/02-task-execution § 11](../task-runtime/02-task-execution.md)

---

## § 6. 跟 TaskRuntime BC 的接口

Workforce BC 跟 TaskRuntime BC 是 Shared Kernel 关系（[strategic/03-bounded-contexts § 3.1](../../strategic/03-bounded-contexts.md)）：

- Task / TaskExecution 引用 `worker_id`（属 Workforce BC）+ `project_id`（属 Workforce BC）
- TaskExecution dispatch 时 center 查 WorkerProjectMapping 决定 worker pick + base_path
- Worker daemon 启动 / 重连 → 走 ReconcileService 协议（详见 [task-runtime/00-overview § 3.2](../task-runtime/00-overview.md)）

> 详细 worker 端 dispatch 处理时序 / shim / Agent CLI 子进程 / JSONL 解析 / Artifact 上报 / kill 进程级机制 / reconcile 端响应 → [task-runtime/02-task-execution.md § 9-12](../task-runtime/02-task-execution.md)。

---

## § 7. References

### 相关 ADR

- [ADR-0008 WorkerProjectMapping discovery proposal](../../../decisions/0008-worker-project-mapping-via-discovery-proposal.md)
- [ADR-0010 Task / TaskExecution 两层模型](../../../decisions/0010-task-execution-two-layer-model.md)
- [ADR-0011 派单可靠性协议](../../../decisions/0011-dispatch-reliability-protocol.md)
- [ADR-0018 Detached agent + per-execution shim](../../../decisions/0018-detached-agent-via-per-execution-shim.md)
- [ADR-0019 BC 合并](../../../decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)

### 战略层

- [strategic/03-bounded-contexts § BC3 Workforce](../../strategic/03-bounded-contexts.md)
- [strategic/01-subdomain-classification](../../strategic/01-subdomain-classification.md)（Workforce: Supporting-Essential）

### 跨 BC

- [task-runtime/00-overview.md](../task-runtime/00-overview.md) — TaskRuntime BC 入口（含 ReconcileService / DispatchService 协议视图）
- [task-runtime/02-task-execution.md § 9-12](../task-runtime/02-task-execution.md) — worker 端运行时（Worker 上的 per-execution 运行时实施细节，已从本文件迁出）
- [agent-harness/01-prompt-assembly.md](../agent-harness/01-prompt-assembly.md) / [agent-harness/02-skill-cli-tooling.md](../agent-harness/02-skill-cli-tooling.md)

### 配置与实现

- [conventions § 1 无野任务](../../../../rules/conventions.md)
- [implementation/02-persistence-schema.md](../../../implementation/02-persistence-schema.md)（TBD）
