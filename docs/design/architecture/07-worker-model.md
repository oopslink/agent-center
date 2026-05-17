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

## Shim 模型与 per-execution 目录

详细决策见 [ADR-0018](../decisions/0018-detached-agent-via-per-execution-shim.md)。本节给架构层视图。

### 为什么不直接把 agent 作为 daemon 子进程

Worker daemon 升级是常态（v1 估计每周 1-2 次：bug fix / 协议迭代 / 新 agent CLI adapter）。如果 agent 是 daemon 的子进程，daemon 重启 = stdout pipe 断 + unix socket 断 → agent 短时间内 SIGPIPE / RPC 失败而终结 → 长任务（小时级）每次升级都重跑，浪费严重。

所以 agent 设计为 daemon 的 **detached 进程**：daemon 跟 agent 之间隔一个 shim 进程，shim 真正持有 agent 子进程 + IO 通道；daemon 跟 shim 通过本地 RPC 通信。

### 拓扑

```
daemon ──spawn (setsid)──> shim ──fork+exec──> agent CLI
   │                          │                     │
   │  daemon.sock            │ shim.sock          │
   │ (center 出口)           │ (agent → shim)     │
   ▼                          ▼                    ▼
center                      events.jsonl       agent.log
                            status.json
```

daemon 死时：

- shim 不死（脱离 daemon process group）
- agent 不死（parent=shim，IO=shim 持有的文件，全活）
- 事件继续 append 进 events.jsonl
- daemon 起来后 shim 主动重连 + catchup

### Per-execution 目录布局

每个 TaskExecution 在 worker 本机有独立目录：

```
~/.agent-center-worker/exec/<execution_id>/
  envelope.json        # daemon 派单时写, 不可变快照
  status.json          # shim 写, daemon 读
  events.jsonl         # shim append-only
  agent.log            # agent stdout/stderr
  shim.sock            # shim 暴露的本地 socket
```

`status.json` 含字段（概念，schema 归实现层）：

```
{
  shim_pid, shim_start_time,
  agent_pid, agent_start_time,
  phase,                 // preparing | running | done
  exit_code?,
  last_acked_seq         // daemon 已 ACK 的最大事件 seq
}
```

写入语义：

- `envelope.json` —— daemon 派单时 `temp+rename` 落地
- `status.json` —— shim 每次 phase 变更 `temp+rename` 原子写
- `events.jsonl` —— shim O_APPEND 顺序追加，自带递增 seq
- `agent.log` —— shim 起 agent 时把 fd 重定向到此文件

### Shim 是 `agent-center worker shim` 子命令

不引入独立 binary。daemon 通过 `os/exec` 拉起：

```
agent-center worker shim \
  --execution-id=<uuid> \
  --shim-token=<nonce> \
  --cmd=claude-code \
  -- <agent args...>
```

配合 `SysProcAttr{Setsid: true}` 让 shim 脱离 daemon 的 session / process group。Daemon 死后 shim 与 agent 都活。

### Shim → daemon RPC

Shim 主动连 daemon 主 socket，单一长连接。主要消息：

| 方向 | 消息 | 时机 |
|---|---|---|
| shim → daemon | `ShimHello { execution_id, shim_token, shim_pid, shim_start_time, agent_pid, agent_start_time, last_acked_seq }` | shim 启动 / 重连 |
| daemon → shim | `Catchup { from_seq }` | 收 Hello 后告诉 shim 从哪个 seq 开始重发 |
| shim → daemon | `PhaseChanged { execution_id, phase, exit_code? }` | phase 切换 |
| shim → daemon | `Event { execution_id, seq, event }` | 每个 JSONL 解析后的事件 |
| daemon → shim | `EventAck { execution_id, seq }` | 让 shim 更新 status.json.last_acked_seq |
| shim → daemon | `RPCForward { execution_id, rpc_name, payload }` | agent 调 `agent-center request-input` 等阻塞 RPC |
| daemon → shim | `RPCResponse { ..., payload }` | center 回来后转给 shim 转给 agent |
| shim → daemon | `ShimGoodbye { execution_id, finalized_at }` | 所有 event 已 ACK + 准备退出 |
| daemon → shim | `GoodbyeAck` | 收尾确认（agent.log 已上传等） |

连接断（daemon 升级 / 网络抖动）时：

- shim 继续 append events.jsonl
- agent 的阻塞类 RPC 在 shim 内**继续阻塞**（语义同"用户暂时没回"），不引入 RPC timeout
- shim 周期重连 daemon；daemon 起来后 ShimHello 重新走流程

### Fencing：shim_token + PID start_time

**shim_token**（防伪造）：daemon spawn shim 时生成一次性 nonce 塞 env：

```
AGENT_CENTER_SHIM_TOKEN = <crypto random nonce>
```

shim 在 ShimHello 里回传，daemon 校验匹配才接受连接。防止任意进程冒充 shim 连主 socket。

**PID start_time**（防 PID 复用）：daemon 探活 shim 时不光 `kill -0`，还读进程 start_time 跟 status.json 里存的 `shim_start_time` 比对。读法：

- macOS：`ps -o lstart= -p <pid>`
- Linux：`/proc/<pid>/stat` 第 22 字段

agent_pid 同理（status.json 存 `agent_start_time`）。

### 异常 timeout 表

| 场景 | 行为 |
|---|---|
| daemon spawn shim 后未收到 ShimHello | **60s** 超时 → daemon emit `task_execution.failed(reason='shim_no_hello', message=...)` → kill shim_pid（若还活） |
| daemon 长时间不在时 agent 调阻塞 RPC | shim 阻塞 agent 不报错，daemon 回来后 flush；不引入 RPC 超时 |
| shim ShimGoodbye 后等 daemon ACK | 最长 **24h**（跟 GC 同步）；超时 shim fence-and-forget 退出；daemon 下次启动扫到剩余 events.jsonl 补完投递 |
| shim 进程崩溃 | daemon 周期 `kill -0 + start_time` 探活；shim 死 → SIGTERM agent_pid（若活） → emit `failed(reason='shim_crashed')` |

新的 failed reason 进 [02-task-model.md § 3.6](02-task-model.md)：`shim_no_hello` / `shim_crashed`。

### GC

per-execution 目录跟 worktree 同步 24h 释放。三资源齐释放（worktree / per-execution 目录 / agent.log），调试体验一致：24h 内 `inspect execution E-7` 能完整复盘（envelope / status / events / log / worktree 全在）。

## Worker 内 Agent CLI 中转

Agent CLI 子进程通过 `agent-center xxx` 调用本机 shim，shim 转 daemon，daemon 转 center：

```
agent ───> shim.sock ──RPCForward──> daemon ──> center
           (本地 RPC)               (daemon.sock)
```

Daemon spawn shim 时 env 注入：

```
# daemon → shim
AGENT_CENTER_EXECUTION_ID    = <uuid>
AGENT_CENTER_TASK_ID         = <uuid>
AGENT_CENTER_PROJECT_ID      = <string>
AGENT_CENTER_CONVERSATION_ID = <uuid> 或 ""
AGENT_CENTER_WORKSPACE_MODE  = "worktree" | "direct"
AGENT_CENTER_CWD             = <resolved path>
AGENT_CENTER_PRIORITY        = "high" | "medium" | "low"
AGENT_CENTER_ETA_AT          = <ISO 8601> 或 ""
AGENT_CENTER_SHIM_TOKEN      = <crypto random nonce>   ★ 仅 daemon → shim
```

Shim 起 agent 时透传上述（除 SHIM_TOKEN 外）+ 加：

```
# shim → agent
AGENT_CENTER_WORKER_SOCK = ~/.agent-center-worker/exec/<id>/shim.sock   ★ 指向 shim, 不是 daemon
```

Agent 看到的 `AGENT_CENTER_WORKER_SOCK` 指向 **shim 的本地 socket**（不是 daemon 主 socket）。这是 detached 模型的关键：daemon 升级窗口期间，agent 调 RPC 不受影响 —— shim 一直在听，缓冲请求；daemon 回来后 flush。

阻塞类 RPC（`request-input` 等）：

```
agent: agent-center request-input "..."
  CLI 子命令 → 连 shim.sock → shim 暂存 + 转 daemon → daemon 转 center
  agent 阻塞等响应 (无 RPC timeout, 等同"用户暂时没回")
  daemon 升级期间 shim 持续阻塞 agent; daemon 起来后路径 resume
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

### 本地崩溃恢复（per-execution 目录）

Worker daemon 用 `~/.agent-center-worker/exec/<execution_id>/` 目录承载本机状态（详见上文 [Shim 模型与 per-execution 目录](#shim-模型与-per-execution-目录) + [ADR-0018 § 3 / § 5](../decisions/0018-detached-agent-via-per-execution-shim.md)）。**替代 [ADR-0011](../decisions/0011-dispatch-reliability-protocol.md) 原方案的 sqlite ledger**。

- 收到 envelope **先写 `exec/<id>/envelope.json`** 再 ACK（temp+rename，写失败不 ACK）
- shim 启动后写 `status.json` 维护 phase；事件 append 进 `events.jsonl`
- daemon 重启时扫 `exec/*/status.json` 拿未完成 execution，对每条决定动作（等 shim Hello / 接管 cleanup / kill 僵尸 / 标 failed）

per-execution 目录 **不是状态权威**（状态权威在 Center），仅本地崩溃恢复 + daemon ↔ shim 状态对账用。

幂等查询：

| 本地状态 | Worker 行为 |
|---|---|
| 无 `exec/<id>/` 目录 | 建目录 → 写 envelope.json → ACK → spawn shim |
| 目录存在 + status.json.phase=running | 重发 ACK；**不重起 shim**（防双跑） |
| 目录存在 + status.json.phase=done | 重发 ACK；不做任何事 |

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
3. 本机 reconcile       → 扫 ~/.agent-center-worker/exec/*/status.json
                         逐条决定: 等 shim Hello / 接管 cleanup / kill 僵尸 / 标 failed
4. center reconcile    → 上报 local_active_executions; 收 ReconcileResponse;
                         kill stale/unknown 进程后继续
5. 长连接 listen        → 收 DispatchEnvelope
6. 收派单:
   a. 建 ~/.agent-center-worker/exec/<id>/ 目录, 写 envelope.json (temp+rename)
      失败不 ACK; 成功 → ACK / NACK
   b. workspace 准备:
        - workspace_mode=worktree: git worktree add -b task/<execution_id>
        - workspace_mode=direct: cwd = base_path (不建 worktree)
   c. 装载 worker-agent.md skill (按 workspace_mode 注入不同提示)
   d. 组装 final_prompt (见 [08-prompt-assembly.md](08-prompt-assembly.md))
   e. 生成 shim_token (一次性 nonce)
   f. spawn shim (detached, setsid):
        agent-center worker shim --execution-id=... --shim-token=... --cmd=claude-code -- ...
   g. shim 起 agent 子进程, IO 重定向到 agent.log;
      shim 起 shim.sock 监听本地 RPC;
      shim 写 status.json (phase=running, shim_pid, agent_pid, ...);
      shim 主动连 daemon.sock 发 ShimHello (含 shim_token)
   h. daemon 校验 shim_token → 回 Catchup → 接管事件流
   i. shim 解析 agent JSONL → emit events 给 daemon (durable: events.jsonl);
      agent 调 agent-center xxx → shim 转 daemon 转 center
   j. emit task_execution.working { workspace_mode, cwd, ... }
   k. workspace_mode=worktree 额外 emit worktree.created
   l. agent 退出:
        - exit 0 + 无未 resolve input_request → shim emit task_execution.completed
        - exit 非 0 / agent 显式 report-failure → shim emit task_execution.failed(reason+message)
        - 收 task_execution.kill_requested → shim SIGTERM agent → 5s grace → SIGKILL → emit task_execution.killed
   m. shim 把剩余事件发完 → 等 daemon ACK 全部 seq
   n. shim 发 ShimGoodbye → daemon 读 agent.log 上传 BlobStore → emit task_log.archived
      → daemon 回 GoodbyeAck → shim 退出
   o. artifacts 走 agent-center report-artifact → shim → daemon → center
7. worktree 模式: 24h 后 GC worktree + exec/<id>/ 目录, emit worktree.released
8. 心跳 → 周期 emit Heartbeat (含 working_seconds_accumulated 增量)
```

> daemon 升级期间（步骤 i / j 阶段）：shim 跟 agent 都活，事件继续 append events.jsonl；daemon 回来后 shim 自动重连重发未 ACK 段。详见 [ADR-0018 § 4 / § 5](../decisions/0018-detached-agent-via-per-execution-shim.md)。
>
> 失败 reason / token 轮换 / 离线后 task 走向 见 [02-task-model.md § 9 timeout](02-task-model.md) 与 [ADR-0011](../decisions/0011-dispatch-reliability-protocol.md) / [ADR-0018](../decisions/0018-detached-agent-via-per-execution-shim.md)。
