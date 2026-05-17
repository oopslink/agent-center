# Worker 执行模型

Worker daemon 是用户开发机上的常驻进程，负责接派单、起 agent 子进程、中转 agent ↔ CLI 调用、回传状态与产物。

## 角色定位

- 不存权威状态，状态权威在 Center（Task / TaskExecution 状态以 [02-task-model.md](02-task-model.md) 为准）
- 不做决策，决策权在 Supervisor / 用户
- 只负责"把活干完并如实汇报"

> 术语：本章统一用 **TaskExecution** 指"一次任务执行"。AgentSession 已下线，见 [ADR-0010](../decisions/0010-task-execution-two-layer-model.md)。

## Workspace 模式

每次 TaskExecution 必有 CWD。两种模式独立设计，由 `task.requires_worktree` 决定（详细判断维度见 [02-task-model.md § 6](02-task-model.md)）：

| 模式 | `requires_worktree` | CWD | 隔离 |
|---|---|---|---|
| **worktree** | `true`（默认） | `base_path + ".wt/task-<execution_id>"`（per-execution git worktree） | git worktree 隔离 |
| **direct** | `false` | `base_path` 自身 | 无 |

### Worktree 模式

**Worktree 是临时的、动态的，不在配置 / mapping 里登记，只通过事件流实时上报**。

派单时 worker 做：

```
worker daemon 收到 execution E-7 (workspace_mode=worktree):
  base_path     = <mapping.base_path>
  worktree_root = base_path + ".wt"           ← 约定推导, 不存
  worktree_path = worktree_root + "/task-E-7"
  
  cd base_path
  git worktree add -b task/E-7 worktree_path main
  emit worktree.created { execution_id=E-7, path=worktree_path, branch="task/E-7", at=now }
  
  cwd = worktree_path
  spawn agent → 干活
  ...
  任务结束:
    - 上报产物（diff、log、生成的文件清单）
    - 默认保留 worktree 24h 方便人复查
  
  24h 后 GC:
    git worktree remove worktree_path
    rm -rf worktree_path
    emit worktree.released { execution_id=E-7, at=now }
```

**Worktree 的呈现**：通过 `events` 表 + TaskExecution 投影实时维护"活跃 worktree"列表。`agent-center ps` 能看到每个 execution 当前的 worktree 路径；不需要单独的"worktree 表"。

**worktree_root 的处理**：约定 = `base_path + ".wt"`，不在 mapping 表里存字段。极少数项目需要自定义（base_path 是 read-only 挂载等）才需要 override —— v1 不做这个开关。

### Direct 模式

CWD 直接是 `base_path`（项目根目录），不创建 worktree、不新开 branch。

派单时 worker 做：

```
worker daemon 收到 execution E-9 (workspace_mode=direct):
  base_path = <mapping.base_path>
  cwd       = base_path                       ← 直接用, 不创 worktree
  
  emit task_execution.working { execution_id=E-9, workspace_mode='direct', cwd=base_path }
  (不 emit worktree.created)
  
  spawn agent → 干活
  
  任务结束:
    无 worktree 可 GC（base_path 是用户的工作目录, 不动）
    若有产物 → BlobStore (agent 不该改 base_path 文件, 按约定)
```

约束：

- Agent 能读 CLAUDE.md / AGENTS.md / 项目所有文件（[ADR-0005](../decisions/0005-project-charter-stays-in-project-repo.md)）
- Agent **按约定**不修改 base_path 下任何文件；不强 enforcement（v1 不做 readonly mount，推 [roadmap](../roadmap.md)）
- 所有产物走 `agent-center report-artifact` / `agent-center blob put`
- Worker-agent.md skill 在 direct 模式下注入额外提示"你在用户项目根目录，请勿修改文件"

**并发**：多个 direct 模式 execution **共享 base_path** 作为 CWD，无锁。Direct 模式假定 agent 只读、副作用最小，多并发只读无冲突。Worktree 模式 task + Direct 模式 task 在同一 worker 上 CWD 不冲突（前者在 `.wt`，后者在 `base_path`）。

Workspace 不能解决的事：端口冲突、依赖 cache、外部服务 —— 这些 v1 不在 worker 层兜底（项目层用 `concurrency_hint` 配置降级，但 v1 不做 B3）。

## 并发模型

```yaml
# worker.yaml
concurrency:
  per_agent_type: 2     # 默认：同一 agent CLI 最多并跑 2 个
```

- v1 不做 per-project 限制（worktree 已隔离文件）
- per-worker 全局总并发 = sum(per_agent_type)

## Agent Adapter

每种 agent CLI 一个 adapter，封装该 CLI 的：

- 怎么起 headless / structured 模式（如 `claude --output-format stream-json`）
- 怎么传 `--session-id`
- 怎么传 system prompt
- JSONL 输出怎么解析

v1 必须支持的 adapter：`claude-code`。
计划支持：`codex`、`opencode`。

## Worker 内 Agent CLI 中转

Worker daemon 在本机暴露一个 unix socket，agent 子进程通过 `agent-center xxx` CLI 命令与之通信：

```
worker daemon: listens on /var/run/agent-center-worker-<id>.sock

worker daemon spawns agent，env:
  AGENT_CENTER_EXECUTION_ID=<uuid>
  AGENT_CENTER_TASK_ID=<uuid>
  AGENT_CENTER_PROJECT_ID=<string>
  AGENT_CENTER_WORKSPACE_MODE="worktree" | "direct"
  AGENT_CENTER_CWD=<resolved path>
  AGENT_CENTER_WORKER_SOCK=/var/run/agent-center-worker-<id>.sock
  AGENT_CENTER_PRIORITY="high" | "medium" | "low"
  AGENT_CENTER_ETA_AT=<ISO 8601> 或 ""

agent: 执行 agent-center request-input "..."
  CLI 子命令 → 连 sock → 发 RPC → 阻塞 / 立即返回
  worker daemon → 转发到 center / 等回应
```

> `AGENT_CENTER_AGENT_SESSION_ID` 已废弃；用 `AGENT_CENTER_EXECUTION_ID` 取代（[ADR-0010](../decisions/0010-task-execution-two-layer-model.md)）。

参见 [10-skill-cli-tooling.md](10-skill-cli-tooling.md) 与 [04-input-required.md](04-input-required.md)。

## 注册与认证

- Worker 启动时凭 `worker.yaml` 里的 **bootstrap token** 连回 center
- Center 校验通过后给一个长期 **session token**
- Bootstrap token 通过 `agent-center worker enroll`（在 center 同机）签发

## Worker.yaml 形态

```yaml
worker:
  id: mac-mini-1
  bootstrap_token: ...
  center_endpoint: ...

concurrency:
  per_agent_type: 2

discovery:
  scan_paths:                    # 扫这些路径找 git repo 作为候选项目
    - /Users/oopslink/code
    - /Users/oopslink/works
  exclude:                       # 排除 glob
    - "**/node_modules/**"
    - "**/vendor/**"
    - "**/.cache/**"
  scan_interval: 1h              # 周期扫；首次 enroll 后立刻扫一次
```

**注意：worker.yaml 不再列具体项目**。哪些项目能跑 = 哪些项目通过自动发现 + 用户确认成为了 `WorkerProjectMapping`。

## WorkerProjectMapping 创建与维护

### 设计原则

- **自动发现 + 用户确认**：worker 主动扫描候选；用户点 ✅ 才生效（避免随便建出无用 mapping）
- **流程对齐 Issue / InputRequest 模式**：候选作为 Proposal 进入系统，飞书卡片让用户决策
- **Worktree 是动态的**（见上一节）；mapping 表只存稳定的 `base_path`

### 数据模型概念

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

具体 schema 见 [implementation/02-persistence-schema.md](../implementation/02-persistence-schema.md)（TBD）。

### 发现流程

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

### 边界情况处理

#### 路径消失（mapping 中 base_path 不再有 .git）

Worker 扫到原 mapping 的 base_path 已不存在 / 不再是 git repo → emit `WorkerProjectMappingInvalidated` 事件。

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

### 不做的事（v1）

- ❌ 跨 worker 自动"广播"已 accepted 的项目到其它 worker（除非该 worker 也自己扫到）
- ❌ 自动跟随路径移动（用户从 `/code/foo` 搬到 `/works/foo` → 必须重新接受 proposal）
- ❌ 提议合并 / 去重（每条候选独立提议）
- ❌ CLI 手动管理 mapping（运行时 add/remove 命令推迟到 [roadmap](../roadmap.md)）

## 派单可靠性协议

详细决策见 [ADR-0011](../decisions/0011-dispatch-reliability-protocol.md)。worker 端要点：

### Dispatch ACK

Center 发 `DispatchEnvelope` → worker **必须 ACK / NACK**。Center 端 30s 没收到 ACK → 视为失败（execution → `failed(reason='dispatch_no_ack', ...)`），不重发。

```
Center → Worker: DispatchEnvelope { execution_id, task_id, agent_cli, workspace_mode, ... }
Worker → Center: DispatchAck { execution_id, accepted=true, message?, acked_at }
              或 DispatchNack { execution_id, accepted=false, reason, message, acked_at }
```

Worker NACK 的标准 reason：`worker_at_capacity` / `agent_cli_unsupported` / `mapping_missing` / `worktree_path_busy` / `base_branch_missing` / `envelope_version_unsupported`。

每个 NACK 必须同时填 `reason + message`（[conventions § 16](../../rules/conventions.md)）。

### 本地 ledger（崩溃恢复用）

Worker daemon 在本机维护 sqlite ledger：

- 字段：`execution_id` / `received_at` / `phase ∈ {preparing / running / done}` / `envelope_json`
- 收到 envelope **先写 ledger** 再 ACK（写失败则不 ACK）
- 重启后从 ledger 读未完成派单（preparing / running）→ 走 reconcile 拿权威状态后决定动作

ledger **不是状态权威**（状态权威在 Center），仅本地崩溃恢复用。

幂等查询：

| 本地 ledger | Worker 行为 |
|---|---|
| 未收过 | 落 ledger → ACK → 准备 workspace |
| 已收过 + phase=running | 重发 ACK；**不重起 agent**（防双跑） |
| 已收过 + phase=done | 重发 ACK；不做任何事 |

### Reconcile（重连对账）

Worker enroll 或网络重连后**第一件事**：

```
Worker → Center: Reconcile { worker_id, local_active_executions: [E-7, E-9, ...] }
Center → Worker: ReconcileResponse {
  active:   [...],   # center 也认为还 active, 继续跑
  stale:    [...],   # center 已标 failed/killed/done, worker 杀本地进程
  unknown:  [...]    # 不存在或归属其他 worker
}
```

Worker 后续：

- `active` → 继续上报事件
- `stale` / `unknown` → SIGTERM 本地仍 alive 的 agent；emit `task_execution.killed { reason='reconcile_stale' / 'reconcile_unknown', message=... }`（仅审计；center 状态不改）

Worker reconcile **完成前不接收新 dispatch**。

## 上报内容（worker → center）

- **结构化事件流（实时）**：TaskExecution 状态变化、心跳、agent trace 解析后的事件、open-issue / request-input / report-artifact
- **日志归档（任务结束）**：原始 stdout / stderr 打包压缩，上传到 BlobStore；DB 存相对路径

参见 [05-observability.md](05-observability.md) § O2 / O4。

## Worker 视角的工作流时序

```
1. enroll              → 获得 session token
2. dial center         → 建立长连接, 发 ImAlive(capabilities, projects)
3. Reconcile           → 上报 local_active_executions; 收 ReconcileResponse;
                          kill stale/unknown 进程后继续
4. 长连接 listen        → 收 DispatchEnvelope
5. 收派单:
   a. 落 ledger (落地失败不 ACK; 落地成功 → ACK / NACK)
   b. workspace 准备:
        - workspace_mode=worktree: git worktree add -b task/<execution_id>
        - workspace_mode=direct: cwd = base_path (不建 worktree)
   c. 装载 worker-agent.md skill (按 workspace_mode 注入不同提示)
   d. 组装 final_prompt（见 [08-prompt-assembly.md](08-prompt-assembly.md)）
   e. spawn agent, env 注入（含 AGENT_CENTER_EXECUTION_ID 等）
   f. emit task_execution.working { workspace_mode, cwd, ... }
   g. workspace_mode=worktree 额外 emit worktree.created
   h. 并行: 解析 agent JSONL → emit events / 更新 TaskExecution 投影
   i. agent 退出:
        - exit 0 + 无未 resolve input_request → emit task_execution.completed
        - exit 非 0 / agent 显式 report-failure → emit task_execution.failed(reason+message)
        - 收 task_execution.kill_requested → SIGTERM → 5s grace → SIGKILL → emit task_execution.killed
   j. 收集产物 → BlobStore (大文件) / artifacts 表 (元数据)
   k. 日志归档 → BlobStore; ledger 更新 phase=done
6. worktree 模式: 24h 后 GC worktree, emit worktree.released
7. 心跳 → 周期 emit Heartbeat (含 working_seconds_accumulated 增量)
```

> 失败 reason / token 轮换 / 离线后 task 走向 见 [02-task-model.md § 9 timeout](02-task-model.md) 与 [ADR-0011](../decisions/0011-dispatch-reliability-protocol.md)。
