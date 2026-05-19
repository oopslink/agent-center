# 0018. Detached agent execution via per-execution shim

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-17 |
| Refines | [ADR-0011](0011-dispatch-reliability-protocol.md)（worker 侧状态承载机制） |

## Context

[ADR-0011](0011-dispatch-reliability-protocol.md) 设定 worker daemon `fork+exec` agent CLI 作为子进程，并配 sqlite ledger 兜底崩溃恢复。落地时识别出几个相互联动的问题：

- **Worker daemon 升级是常态、且必然打断子进程模型下的 agent**：v1 估计每周 1-2 次 daemon 升级（bug fix / 协议迭代 / 新 agent CLI adapter）。子进程模型下 daemon 死 = agent 的 stdout pipe 断 + unix socket 断 → agent 短时间内 SIGPIPE / RPC 失败而终结。8 小时长任务跑到 7 小时被升级打断 → 全部白做
- **ADR-0011 ledger 兜的场景跟问题不对位**：ledger 设计是为"daemon 崩溃恢复"，但在子进程模型下 daemon 崩 ≈ agent 死，ledger 实际能救回的只是"ACK 后到 spawn 前"的毫秒级窗口；它解决不了"daemon 升级打断长任务"这个真正的痛点
- **支持 in-place 重派不等于解决问题**：[ADR-0010](0010-task-execution-two-layer-model.md) 已经定 retry = 新 execution_id，所以"被打断 → supervisor 重派"在协议上成立；但没有 task 级 checkpoint，重派 = 重跑，对长任务而言体验不可接受
- **Sqlite ledger 跟 worker 单机简单性不匹配**：在调试 / 用户自行运维场景下，sqlite 文件 / schema migration / 锁 比 per-execution 目录更难看。它的查询能力本就用不上（状态权威在 Center）

需要把"agent 跟 daemon 解耦"升格为一级架构原则：daemon 重启不能影响在跑的 agent。

## Decision

### 1. Agent 进程从 daemon 子进程变为 detached 进程，引入 shim 模型

参考 containerd-shim 的解法：每个 TaskExecution 配一个轻量 shim 进程，shim 真正持有 agent CLI 子进程，daemon 跟 shim 通过本地 RPC 通信。

```
daemon → spawn shim (detached, setsid) → shim → fork+exec agent CLI

daemon 死时:
  - shim 不死（脱离 daemon process group）
  - agent 不死（parent=shim, IO=shim 持有的文件, 全活）
  - 事件继续 append 进本地文件
  - daemon 起来后 shim 主动重连 + catchup

daemon 正常时:
  - shim 主动连 daemon 上报事件 + 转发 agent RPC
  - daemon 是单一对 center 的出口
```

### 2. Shim 是 `agent-center worker shim` 子命令，不引入独立 binary

Daemon 通过 `os/exec` 拉起：

```
agent-center worker shim \
  --execution-id=<uuid> \
  --shim-token=<nonce> \
  --cmd=claude-code \
  -- <agent args...>
```

配合 `SysProcAttr{Setsid: true}` 让 shim 脱离 daemon 的 session / process group。

理由：

- Go 单 binary 部署，shim 跟 daemon 共一个 binary 无部署成本
- 大量代码复用：事件类型、RPC client、JSONL parser、blob client wrapper
- 风格跟 git / kubectl 一致

### 3. Per-execution 目录替代 sqlite ledger

每个 TaskExecution 在 worker 本机有独立目录：

```
~/.agent-center-worker/exec/<execution_id>/
  envelope.json        # daemon 派单时写，不可变快照
  status.json          # shim 写，daemon 读：{shim_pid, shim_start_time, agent_pid, agent_start_time,
                       #                       phase ∈ {preparing/running/done}, exit_code?, last_acked_seq}
  events.jsonl         # shim append-only：每个事件一行，自带递增 seq
  agent.log            # agent raw stdout/stderr（调试 + 归档）
  shim.sock            # shim 暴露的本地 socket，agent CLI 通过此连 shim
```

替代 [ADR-0011 § Decision 3](0011-dispatch-reliability-protocol.md) 的 sqlite ledger。优势：

- 一目了然，`ls / cat / tail -f` 直接调试
- 跨 execution 写互不影响，无锁竞争
- 没有 schema migration / 文件锁问题
- shim 跟 daemon 各自访问各自的文件，关注点分离

文件级写入语义：

- `envelope.json` —— daemon 派单时一次性 `temp+rename` 落地
- `status.json` —— shim 每次 phase 变更 `temp+rename` 原子写
- `events.jsonl` —— shim append（O_APPEND，单 writer），fsync 按需
- `agent.log` —— agent 直写（shim 起 agent 时把 fd 重定向到此文件）

### 4. Shim → daemon RPC 协议（shim 主动连）

```
shim 启动:
  连 daemon 主 socket (~/.agent-center-worker/daemon.sock)
  发 ShimHello {
    execution_id, shim_token, 
    shim_pid, shim_start_time,
    agent_pid, agent_start_time,
    last_acked_seq
  }
  daemon 校验 shim_token (防伪造)
  daemon 回 Catchup { from_seq = last_acked_seq + 1 }
  shim 从 events.jsonl seq=Catchup.from_seq 开始重发追平

运行中:
  PhaseChanged  { execution_id, phase, exit_code? }
  Event         { execution_id, seq, event }
  RPCForward    { execution_id, rpc_name, payload }  → 阻塞等响应
  daemon ACK Event 后 shim 更新 status.json.last_acked_seq

连接断 (daemon 升级 / 重启 / 网络抖动):
  shim 继续 append events.jsonl
  agent 的阻塞类 RPC (request-input 等) 在 shim 内继续阻塞（语义同"用户暂时没回"）
  shim 周期重连 daemon

干完活:
  shim wait(agent_pid) → 拿 exit code → 写 status.json (phase=done, exit_code=...)
  shim 把剩余事件发完 → 等 daemon ACK 全部 event
  发 ShimGoodbye
  daemon 回 GoodbyeAck (含已上传 agent.log 等收尾确认)
  shim 退出
```

### 5. Daemon 重启的 Reconcile（本机侧）

```
daemon 启动:
  扫 ~/.agent-center-worker/exec/*/status.json
  for each:
    phase = done → 等 shim 发 Goodbye / 或直接 ACK 剩余事件 → 收尾 cleanup
    phase = running + shim 进程活 (kill -0 + start_time 校验) → 等 shim 自己 ShimHello 上来
    phase = running + shim 进程死 → 标 task_execution.failed(reason='shim_crashed')；
                                    若 agent_pid 还活 → SIGTERM；emit failed
    phase = preparing + shim 进程死 → 同上
  
  本机 reconcile 完后才接收 center 发来的新 dispatch
  
  跟 center 走 Reconcile (ADR-0011 § Decision 4)：把当前 local_active_executions 上报
```

### 6. Fencing：shim_token + PID start_time

**shim_token** 防伪造：daemon spawn shim 时生成一次性 nonce（uuid / crypto random）塞 env：

```
AGENT_CENTER_SHIM_TOKEN = <nonce>
```

shim 在 ShimHello 里回传，daemon 校验匹配才接受连接。防止任意进程冒充 shim 连 daemon 主 socket。

**PID start_time** 防 PID 复用：daemon 探活 shim 时不光 `kill -0`，还读进程 start_time 跟 status.json 里存的 `shim_start_time` 比对。一致才认 PID 还是同一个 shim。读法：

- macOS：`ps -o lstart= -p <pid>`
- Linux：`/proc/<pid>/stat` 第 22 字段

Agent_pid 同理（status.json 存 `agent_start_time`）。

### 7. 异常 timeout 与失败语义

| 场景 | 行为 / timeout |
|---|---|
| daemon spawn shim 后未收到 ShimHello | **60s** 超时 → daemon emit task_execution.failed(reason=`shim_no_hello`, message=...) → kill shim_pid（若还活）|
| daemon 长时间不在（升级 / 离线）时 agent 调阻塞 RPC | shim **阻塞** agent 不报错（语义同"用户没回"），daemon 回来后 flush；不引入 RPC 超时 |
| shim ShimGoodbye 后等 daemon ACK 完事件 | 最长 **24h**（跟 GC 对齐）；超时 shim fence-and-forget 直接退；daemon 下次启动扫到剩余 events.jsonl 补完投递 |
| shim 进程崩溃 | daemon 周期 `kill -0 + start_time` 探活；shim 死 → SIGTERM agent_pid（若活） → emit failed(reason=`shim_crashed`） |

新增的 failed reason 进 [task-runtime/02-task-execution.md § 7 失败 / killed reason 枚举](../architecture/tactical/task-runtime/02-task-execution.md)：`shim_no_hello` / `shim_crashed`。

### 8. Artifact / 日志上传由 daemon 负责

`agent.log` 上传 BlobStore 走 daemon：

```
shim 发完所有事件 + 收到全部 ACK
  → shim 发 ShimGoodbye
  → daemon 从 ~/.agent-center-worker/exec/E-7/agent.log 读取 + 上传 BlobStore
  → daemon emit task_log.archived
  → daemon 回 GoodbyeAck
  → shim 退出
```

理由：

- shim 越轻越好，不引入 BlobStore client 依赖
- agent.log 本地留 24h，daemon 上传失败下次重启扫到能补
- daemon 单点对 center 出口，符合"daemon 是 center 网关"职责切分

`agent-center report-artifact` 走的路径：agent CLI → shim.sock → shim 转 daemon → daemon 转 center（不直接走 daemon.sock，因为 daemon 可能在升级）。

### 9. GC 策略：三资源 24h 同步释放

| 资源 | 保留 | GC 触发 |
|---|---|---|
| `~/.agent-center-worker/exec/<execution_id>/` 整个目录 | **24h** | 跟 worktree 同步 |
| `agent.log` 本地副本 | 24h（在上面目录内） | 同上 |
| Worktree `<base_path>.wt/task-<execution_id>` | 24h | 现有行为，[task-runtime/02-task-execution.md § 8 Workspace](../architecture/tactical/task-runtime/02-task-execution.md)（按 [ADR-0019](0019-bc-scheduling-execution-merged-to-task-runtime.md) 从 workforce/01 迁入）|

三者同步释放，调试一致：24h 内 `inspect execution E-7` 能完整复盘（envelope / status / events / log / worktree 全在）。

## Consequences

正面：

- **Daemon 升级不打断 agent**：核心目标达成。shim 持有 agent + IO + 事件队列，daemon 重启窗口对 agent 完全透明
- **崩溃恢复粒度更细**：daemon 崩 / shim 崩 / agent 崩 三件事独立观测、独立处理
- **本机状态可见**：per-execution 目录用 `ls / cat / tail -f` 就能看，比 sqlite 易调试一个量级
- **协议简化**：daemon 不主动 poll shim；shim 主动上报 + 事件 catchup 单向流；状态变更走 file + RPC 双通道（file 是 durable source，RPC 是 hot path）
- **跟 ADR-0011 协议层兼容**：execution_id 幂等 / center 侧 Reconcile / ACK 协议 全部保留，只是 worker 内部存储机制变了

负面 / 待跟进：

- **多一个进程层（shim）**：每个 active execution 多一个常驻 shim 进程。Shim 本身只做 RPC 转发 + JSONL parse + wait(agent)，内存预算 < 10 MB；v1 单 worker 并发 ≤ 几个不是问题
- **Shim 必须可靠**：shim 自己崩了 agent 还活 → daemon 接管杀掉 + 标 failed（§ 7）；处理路径清晰但代码要写
- **RPC 拓扑多一跳**：agent → shim → daemon → center。本地 socket 路径 ms 级，可接受
- **跨平台 setsid / start_time 读取**：macOS / Linux 系统调用细节差异；adapter 层封装。v1 仅 macOS / Linux
- **status.json 原子写约束**：必须 `temp + rename`；常见模式，工程上无难度
- **不再有"跨 execution 查询"能力**：sqlite ledger 时代可以 `SELECT phase WHERE ...`，现在要 glob 目录。Worker 单机并发 < 10，glob 成本可忽略

## Alternatives Considered

### A. 保留子进程模型 + 用 supervisor 重派应对升级打断

- Pro: 不引入 shim 层；架构最简
- Con: 长任务被打断浪费严重；retry = 重跑（没 task 级 checkpoint），全做白工；用户体验差
- 不选

### B. Detached agent 但无 shim（daemon 直接 `setsid` 起 agent，IO 文件化）

- Pro: 进程层数最少
- Con: daemon 升级窗口无法转发 agent 的阻塞 RPC（request-input 等）；agent 端要么阻塞超时要么自己连 center → 等于把 shim 工作塞 agent CLI 内 → 侵入 agent CLI、违背 [ADR-0002](0002-no-llm-sdk-use-cli-agents.md) "agent CLI 保持简单"
- 不选

### C. Agent 直连 center（绕过 daemon 和 shim）

- Pro: 最解耦
- Con: agent CLI 要内置 center 协议 / 认证 / 心跳；adapter 层暴增；违背 ADR-0002；不可行
- 不选

### D. Shim + per-execution sqlite 数据库

- Pro: 跨 execution 查询能力强
- Con: 比 per-file 复杂；查询能力 v1 不需要（center 才是状态权威）；同 sqlite ledger 的运维负担
- 不选

### E. Daemon 主动 poll 模式（不要 shim 主动连）

- Pro: daemon 端连接管理简单
- Con: daemon 升级窗口 shim 完全静默 → daemon 起来后才能 poll → 升级期间事件投递 latency 高；shim 主动连更对称（"谁有新东西谁说话"）
- 不选

### F. 本方案（shim subcommand + per-execution 目录 + 主动上报 + nonce + start_time fencing）

- Pro: 透明升级、易调试、协议简单、可拓展
- Con: 进程层数 +1
- 选

## 影响范围

- 修订 [ADR-0011](0011-dispatch-reliability-protocol.md)：
  - Status → `Accepted (Refined by ADR-0018)`
  - § Decision 3 "Worker 本地 ledger（持久化）"段改写为指向本 ADR § 3
  - § Consequences 对应负面项更新
- 改写 [task-runtime/02-task-execution.md § 9 worker 端运行时](../architecture/tactical/task-runtime/02-task-execution.md)（按 [ADR-0019](0019-bc-scheduling-execution-merged-to-task-runtime.md) 从 workforce/01 迁入 TaskRuntime BC）：
  - 新增 "Shim 模型" 章节（含 per-execution 目录布局 + RPC 协议 + 异常 timeout 表）
  - 改写 "Worker 内 Agent CLI 中转" 段（去掉子进程描述，改为 shim 中转）
  - 改写 "派单可靠性协议 § 本地 ledger" 段（指向本 ADR）
  - 改写 "Worker 视角的工作流时序"（shim 出场）
  - env 注入清单：daemon → shim 多加 `AGENT_CENTER_SHIM_TOKEN`；shim → agent 不变；agent 看到的 `AGENT_CENTER_WORKER_SOCK` 指向 shim 的本地 socket
- 改写 [task-runtime/02-task-execution.md](../architecture/tactical/task-runtime/02-task-execution.md)：
  - § 7 Failed reason 枚举新增 `shim_no_hello` / `shim_crashed`
  - Worker 端处理时序去掉 "本地 ledger 查 execution_id 幂等" 段，改写为 "per-execution 目录 + shim spawn" 概要 + 指向同文 § 9
- 微调 [05-observability.md](../architecture/tactical/observability/01-observability.md)：
  - § O1 / O2 补一句 "worker 侧事件投递走 shim → daemon → center；shim 端 events.jsonl 是 durable queue（不替代 events 表，状态权威仍在 center）"
- 实现层 [02-persistence-schema.md](../implementation/) (TBD)：
  - 去除 `worker_ledger` 表
  - 新增 per-execution 目录文件 schema（envelope.json / status.json）
- 实现层 [03-cli-subcommands.md](../implementation/) (TBD)：
  - 新增 `agent-center worker shim` 子命令签名
