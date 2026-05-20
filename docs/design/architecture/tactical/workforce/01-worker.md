# Worker 聚合（+ WorkerProjectMapping 子从属）

> **DDD 战术层** · BC: Workforce · 聚合: Worker（AR）+ WorkerProjectMapping（Entity，子从属）

Worker 是用户开发机上的常驻守护进程，注册到 center 后维持长连接、上报心跳、扫候选项目；每个 worker 在自己机器上能跑哪些 project = 哪些 WorkerProjectMapping。

> **本聚合不管"怎么干活"** —— per-execution 运行时 / shim / workspace 物理 / Agent CLI 子进程 / JSONL 解析 / Artifact / kill 进程级 / reconcile 端响应都归 [TaskRuntime BC](../task-runtime/02-task-execution.md)（[ADR-0019](../../../decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md) carve）。

---

## § 1. Worker 状态机

```
                  (offline)
                     │
                     │ worker enroll + 长连建立 + reconcile 完成
                     ▼
                  online
                     │
                     │ 长连断开 / heartbeat 静默 60s
                     ▼
                  offline
                     │
                     │ 长连重建 + reconcile 完成
                     ▼
                  online
                     ...
```

- 初始：worker 进程启动 → 凭 `worker.yaml.bootstrap_token` 调 `WorkerEnrollService`（[00-overview § 3.1](00-overview.md)）兑换 session_token → 建立长连接 → reconcile → `worker.online`
- `online ↔ offline` 反复迁移，**非终态**
- offline 期间该 worker 上 active executions → TimeoutScanner 标 `failed(worker_lost)`（[task-runtime § 3.3](../task-runtime/00-overview.md)）

---

## § 2. 注册与认证

```bash
# Center 同机一次性签发 bootstrap token：
agent-center worker enroll --worker-id=mac-mini-1
# → 终端打印 bootstrap token（短期，一次性）

# 用户复制到 worker 机器的 worker.yaml：
# 然后启动 worker daemon：
agent-center worker run --config=worker.yaml
# → worker 用 bootstrap token 兑换 session_token → emit worker.enrolled
# → 建立 WebSocket 长连接到 center
# → 调 reconcile 服务（详见 task-runtime/00-overview § 3.2）
# → emit worker.online
```

**事件**：

| 事件 | 触发 | payload |
|---|---|---|
| `worker.enrolled` | bootstrap → session 兑换成功 | worker_id, capabilities |
| `worker.online` | 长连接建立 + reconcile 完成 | worker_id, online_at |
| `worker.offline` | 长连接断开 / heartbeat 静默 | worker_id, offline_at, reason+message（[conventions § 16](../../../../rules/conventions.md)）|
| `worker.heartbeat` | 周期心跳 | worker_id, working_seconds_accumulated（容量信号）|

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

- **worker.yaml 不列具体项目** —— 哪些项目能跑 = 通过自动发现 + 用户确认成为的 `WorkerProjectMapping`
- `concurrency.per_agent_type` 是 worker 容量配置；每条 task_execution 运行时调度归 [TaskRuntime](../task-runtime/02-task-execution.md)
- `agent_cli` 是 worker 声明的能力（enroll 时上报 capabilities）；center 派单时按 `task.agent_cli` 字段匹配支持该 CLI 的 worker；具体 adapter 实现归 TaskRuntime worker 端运行时

---

## § 4. WorkerProjectMapping（Entity，子从属）

### 4.1 模型

```
worker_project_mapping (
  id                      ULID/UUID
  worker_id               FK → workers (强引用，不可变)
  project_id              FK → projects (强引用，不可变)
  base_path               TEXT  -- 主 checkout, 稳定；worktree_root 按约定 = base_path + ".wt"
  source_proposal_id      FK → worker_project_proposals (血缘)
  status                  active | invalidated
  invalidate_reason       nullable: path_missing | not_git_repo | manual_remove
  invalidate_message      nullable TEXT (reason+message 双字段, conventions § 16)
  added_at                ISO8601 TEXT
  invalidated_at          ISO8601 TEXT, nullable
)
```

**worktree_root 不存** —— 按约定 = `base_path + ".wt"`（详见 [task-runtime/02-task-execution § 8 workspace](../task-runtime/02-task-execution.md)）。

### 4.2 创建路径

唯一路径：**Proposal 走 accept** → 同事务建 Mapping。详见 [03-worker-project-proposal.md § 4](03-worker-project-proposal.md) + [ADR-0008](../../../decisions/0008-worker-project-mapping-via-discovery-proposal.md)。

v1 不支持手动 CLI `worker mapping add`（运行时 add/remove 推迟到 [roadmap](../../../roadmap.md)）。

### 4.3 Invalidation

Worker 周期 scan 阶段对比既有 mapping 表：

```
1. Worker 扫 scan_paths 找到所有 .git 目录 (按 exclude glob 过滤)
2. Worker 拿到 center 上的现有 mapping 列表 (经 RPC)
3. 对每条既有 mapping，检查 base_path:
   - base_path 不存在 → emit worker_project_mapping.invalidated (reason=path_missing)
   - base_path 存在但不是 git repo → emit worker_project_mapping.invalidated (reason=not_git_repo)
   - 正常 → 不动
4. Center 收事件 → mapping.status=invalidated（不实际删，保留血缘）
   → 飞书提示用户："Worker X 上 project Y 的路径失效了，是否重新映射？"
   → 不自动迁移（避免用户改路径正在测试时被系统错误处理）
```

### 4.4 同一 project 被多 worker 发现

Worker A 已 accepted `agent-center → /Users/.../code/agent-center`。
Worker B 扫到自己本地 `/home/.../code/agent-center`，suggested_project_id 也是 `agent-center`。

Center 检测到 project 已存在 → 仍然推飞书：

```
Worker home-server 也发现 agent-center 项目:
  📁 /home/oopslink/code/agent-center

是否在该 worker 上也启用?
[✅ 启用 (默认)] [❌ 不启用]
```

默认选项是 ✅ —— 一键即可，避免无意义的二次确认。详见 [03-worker-project-proposal.md § 4](03-worker-project-proposal.md)。

### 4.5 WorkerProjectMapping Invariants

1. **worker_id / project_id 不可变**：创建时填，永不改；改"项目主路径"= 走新 Proposal 流程
2. **base_path 不可变**：路径改变 → 走 invalidate → 新 Proposal
3. **同一 (worker_id, project_id) 至多 1 条 active mapping**：重新 accept 时旧 mapping 标 invalidated，新 mapping 取代
4. **terminal 状态 invalidated 不可逆**（要重新 active 需建新 mapping）
5. **invalidated 必带 reason + message**（[conventions § 16](../../../../rules/conventions.md)）

---

## § 5. 心跳与超时

- Worker 长连接建立后周期 emit `worker.heartbeat`（含 `working_seconds_accumulated` 增量等容量信号）
- Center 端 `worker_heartbeat_timeout`（默认 60s）：心跳静默超时 → worker → offline
- offline 后 worker 上所有 active execution → `failed(worker_lost)`（详见 [task-runtime/02-task-execution § timeout](../task-runtime/02-task-execution.md)）
- Worker 重连流程（含 reconcile worker 端 active/stale/unknown 处理）归 [task-runtime/00-overview § 3.2](../task-runtime/00-overview.md)

---

## § 6. CLI

| 命令 | 用途 | 同机要求 |
|---|---|---|
| `agent-center worker enroll --worker-id=...` | 签发 bootstrap token（输出到终端，用户拷到 worker.yaml） | center 同机 |
| `agent-center worker run --config=worker.yaml` | 启动 worker daemon（兑换 session + 建长连）| worker 机 |
| `agent-center worker list [--status=...]` | 列所有 worker（+ status + last heartbeat） | center 同机 / 远程（v2）|
| `agent-center worker status <worker_id>` | 看单个 worker 详情 | center 同机 |
| `agent-center worker proposal list [--worker_id=...] [--status=pending]` | 列 proposal | center 同机 |
| `agent-center worker proposal unignore <proposal_id>` | 把先前 ignored 的提议重置为 pending | center 同机 |

完整 CLI 见 [agent-harness/02-skill-cli-tooling.md](../agent-harness/02-skill-cli-tooling.md)。

---

## § 7. Worker Invariants

1. **worker_id 不可变**：用户在 worker.yaml 配置时定，永不改
2. **bootstrap token 一次性**：用一次（兑换 session_token）即失效
3. **session token 跟 worker_id 1:1**：重新 enroll 触发新 session_token + 旧 token 立即失效
4. **online / offline 反复迁移**：非终态；可重连
5. **offline 时该 worker 不接新派单**：DispatchService 单活校验阶段会拒绝（[task-runtime/00-overview § 3.1](../task-runtime/00-overview.md)）
6. **heartbeat 静默 > 60s → 自动 offline**：TimeoutScanner 触发（[task-runtime/00-overview § 3.3](../task-runtime/00-overview.md)）

---

## § 8. References

- [ADR-0008 WorkerProjectMapping discovery proposal](../../../decisions/0008-worker-project-mapping-via-discovery-proposal.md)
- [ADR-0019 BC 合并](../../../decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)
- [00-overview.md](00-overview.md) — BC 入口（含 Domain Services / 跨 BC）
- [03-worker-project-proposal.md](03-worker-project-proposal.md) — Proposal 状态机 + 发现流程
- [02-project.md](02-project.md) — Project AR
- [task-runtime/02-task-execution.md § 9-12](../task-runtime/02-task-execution.md) — worker 端 per-execution 运行时（已 carve）
- [conventions § 13 安全](../../../../rules/conventions.md)（bootstrap / session token）
