# Agent 并发执行任务 — 监工 / Executor 设计

> **一个 agent 同时跑多个难度不一的任务，并按难度选模型。**
>
> 核心结论：在 agent（身份）之下引入两类执行体——**常驻「监工」(orchestrator)**
> 与**按需「executor」**。监工是唯一连 mcp / 看所有 chat / 写中心的「大脑」，
> executor 是纯计算、不连中心、靠**文件交换**与监工通信的隔离工人。并发的串行
> 锁粒度由 agent 下沉到 executor，从而实现「一个 agent 并行多 task」。
>
> 来源：#并发讨论 channel 与 @oopslink 的逐点对齐（2026-06-28）。
> 参考但**不照搬**：`codingclaw-agent-parallel-execution-design.md`（模型不同，
> 见 §11）。状态：**设计草案，待 owner 最终拍板后落实现层**。
> 源码工作区细化方案见：[Agent Runtime Repo Workspaces](agent-runtime-repo-workspaces.md)。

---

## 1. Why — 现状约束

当前 agent-center 的层次是 **worker → agent → task** 三层，且：

- **串行锁在 agent 粒度**：`start_task` 撞 `agent_busy` —— 派给同一 agent 的 task
  必须排队，一个 agent 同时只能跑一个 task。
- **mcp-host 是 per-agent 进程**，`AC_MCP_AGENT_ID` 进程级写死，身份与执行体 1:1。

要做到「一个 agent 并行多 task + 每 task 选模型」，必须改这两点。本设计的本质是：
**把「必须保持顺序一致的最小范围」（串行单元）从 agent 下沉到比 agent 更细的执行
单元，范围内串行保正确、范围外放开并行保吞吐。**

非目标（本设计不解决）：跨机器 dispatch、多 worker 共享 lease 的物理调度（留作未来，§12）。

---

## 2. 概念分层（对齐定稿）

讨论里「agent」一词此前身份 / 执行体合一，导致歧义。本设计显式拆分：

| 概念 | 定位 | 生命周期 | 连 mcp / 中心 | 跑 LLM |
|---|---|---|---|---|
| **worker** | 宿主进程 / 守护（项目既有专有名词） | 常驻 | — | — |
| **agent** | **身份**：owner / 凭据 / profile / capabilities / 默认模型 | 长期 | —（由监工代表） | — |
| **orchestrator（监工 / 智能 session）** | 一个 agent 名下**唯一**的协调者：收发所有 chat、判难度、起 executor、汇总回写 | **常驻**（事件驱动唤醒，空闲不烧 token） | **是（唯一消费者）** | 是（轻量档） |
| **executor** | 做一个具体任务的隔离工人：纯计算 | **按需**启动，做完即退 | **否** | 是（按难度档） |
| **chat（对话）** | 外部 append-only 消息流，一个 agent 同时混在多个里 | —（外部） | — | — |
| **problem（话题 ≈ issue）** | 被讨论的逻辑问题，可被多个 chat 引用、派生多个 executor | — | — | — |

**关键关系：一个 agent = 一个监工 + N 个 executor。** chat ≠ executor、chat ≠ 串行单元
（CodingClaw 是 chat=session 白捡串行键，我们没有这个前提，故须自定义，见 §3 / §8）。

```
worker
 └── agent（身份 / profile）
      └── orchestrator（常驻，唯一连 mcp，看所有 chat）
           ├── executor #1 （按需，隔离，文件通信）
           ├── executor #2
           └── executor #3      ← 并发上限 max_concurrent_tasks（profile，默认 3）
```

---

## 3. 串行单元与并发模型

**串行单元 = 并发控制的锁粒度**：共享同一串行键的工作串行，不同串行键的并行。

- **监工对「中心交互 / 记忆写入」是串行的**——单一协调点，唯一写入者，天然把所有对
  中心与记忆的写串行化，**根除并行 session 互相覆盖记忆的问题**。
- **executor 之间是并行的**——各自独立 context + 独立工作区，互不串味。
- 一个监工同时管 ≤ `max_concurrent_tasks` 个 executor；超额排队，不硬起。

这把锁从 agent 下沉到 executor，正是「一个 agent 并行多 task」的根因。

---

## 4. 进程与连接拓扑

```
        ┌─────────── agent（身份 / 凭据 / profile）───────────┐
        │                                                       │
   mcp-host (per-agent)                                         │
        │ ↑ 唯一消费者                                          │
   ┌────┴──────────────┐   文件交换    ┌──────────────────┐    │
   │   orchestrator     │ ───────────▶ │   executor #i     │    │
   │  （常驻，连 mcp）  │ ◀─────────── │ （按需，无 mcp）  │    │
   └───────────────────┘   status/out  └──────────────────┘    │
        └───────────────────────────────────────────────────────┘
```

- **mcp-host 维持 per-agent**，唯一消费者是监工 → 「一个 agent 配一个 mcp server」
  这个相关 issue 顺带有了正解，不必改成 per-executor。
- **executor 不连中心、不持凭据、不碰 mcp**，只在自己的工作目录内读写文件。
- **结果由监工读 output 后发到来源 chat**（executor 不直接发，因为只有监工连 mcp）。

---

## 5. 模型选择优先级链（定稿）

```
task.model 设了      → 直接用，不问监工（硬覆盖，优先级最高）
        没设         → 监工用 LLM 读 goal 判难度 → 从 profile.allowed_models 选档
                        判不出 → profile.default_executor_model 兜底
                                 未配 default → 用 profile.orchestrator_model 作最后兜底（T743），
                                 使只配了 orchestrator_model 的 profile 也能 fork 执行器而非报错
```

- 难度判断**用 LLM 推理**（监工本就是会推理的模型），不写死启发式规则。
- **监工自身模型**取自 `profile.orchestrator_model`（路由 / 判难度 / 汇总，用便宜快档）。
- **executor 模型**由监工按上面优先级链选，**可选清单在 profile 配**。

配套 profile / task 字段见 §10。

---

## 6. Session（executor）context 五层拼装

监工在创建 executor 时拼装其初始 context。按「继承自 agent（共享只读）」vs
「per-executor（每次独立）」分清，这条线就是并行安全的关键：

| 层 | 来源 | 共享性 | 内容 |
|---|---|---|---|
| **A 身份层** | agent / profile | 共享、**只读** | system prompt / persona、display_name / agent_ref、project / capabilities、凭据与 runtime 参数、默认模型 |
| **B 任务层** | task | per-executor | goal（title / desc / issue spec）、`task.model`（硬覆盖落点）、解阻后的 blocked_comment |
| **C 记忆层** | CLAUDE.md | **读共享、写隔离** | **读**：祖先链 narrow→broad（task→issue→project→global）；**写**：必须 scope 到本 executor 最具体层，禁止并行回写 project/global |
| **D 工作区** | per-executor | **物理隔离（前置条件）** | 独立 git **worktree**、`AC_MCP_AGENT_ROOT` 路径围栏；executor 只在 workspace 内动文件 |
| **E 协作 / 续跑** | 监工聚合 | per-executor、可选 | 以 **problem 为中心**聚合的跨 chat 相关历史、resume 包 |

**并行真正的工程难点不在「起多个 executor」，而在 C 的写隔离 + D 的工作区隔离。**
本设计把 C 写隔离收敛到「监工是唯一写入者」（executor 根本不写中心 / 记忆，只回传
结果由监工写），把 D 收敛到「每 executor 一个 worktree」。

注意 E 不能简单「取来源 chat 历史」——同一 problem 的讨论散在多个 chat，必须以
problem 为中心聚合（见 §8）。

---

## 7. 监工 ↔ executor 文件交换协议

每个 executor 一个工作目录：

```
<agent_root>/executors/<executor_id>/
  input.json      # 监工写：goal + 聚合 context + 分配 model + 来源 chat/issue 引用
  workspace/      # 独立 git worktree（隔离，executor 只在此动文件）
  progress.jsonl  # executor 流式写进度（监工读来在 chat 转播状态）
  output.json     # executor 写：最终结果 / 错误详情
  status          # 完成信号 + 详细信息（schema 见 §9）
```

另有一个**独立的路由记录文件**（§8）用于一致性路由。

生命周期：

```
监工:  判难度 → 选模型 → 以 problem 为中心聚合 context → 写 input.json + 备 worktree
       → spawn executor 进程（指向该目录，不给 mcp / 凭据）
executor: 读 input.json → 跑（不连中心）→ 写 output.json + status=done → exit 0
监工:  检测退出 / done → 读 exit + status + output.json
       → 统一回写（仅监工能做）：发结果到来源 chat / 写记忆 / 更新中心 task
       → 清理或保留 worktree → 看队列拉起下一个
```

---

## 8. 一致性路由（problem 映射）

问题：同一 agent 在多个 chat 里聊**同一个问题**，监工如何判「这条新消息属于哪个在跑
的 problem」？

**定稿做法：在创建 executor 上下文时，把路由归属记录进一个独立的 json 文件**（problem
→ executor / chat / issue 的映射）。监工据此把后续相关消息归并到既有 problem，而不是
每次新起。以 issue / task 显式绑定为主，LLM 语义归并仅作辅助提示。

```
<agent_root>/routing.json   # 独立文件
{
  "problems": [
    {
      "problem_id": "...",
      "issue_ref": "issue-...",        // 显式绑定优先
      "chat_ids": ["channel-...", "channel-..."],
      "executor_ids": ["..."],
      "created_at": "..."
    }
  ]
}
```

这同时让监工能动态决定「这俩 chat 是同一问题 → 合并喂给一个 executor」还是「拆成俩
executor」，不必提前二选一。

---

## 9. 完成信号与 watchdog

**完成信号要两个都看**（定稿）：

- **exit code == 0 且 output.json 存在 → 成功**；
- **exit != 0 → 失败**，从 `status.error` 取详情；
- **进程没了但 status 仍 running → 异常崩溃**，监工按失败处理（可重试）。

`status` 文件 schema（executor 写，监工读，含详细信息）：

```json
{
  "state": "running | done | failed",
  "model": "<本次分配>",
  "started_at": "...",
  "last_progress_at": "...",
  "error": { "kind": "...", "message": "..." },   // 仅 failed
  "summary": "一句话结果摘要"                       // 监工转发 chat 时用
}
```

**watchdog**（监工侧）：`last_progress_at` 超时无更新 → 判 stalled → graceful kill →
按失败处理。对齐既有 heartbeat / lease 思路。

---

## 10. 配置项

**agent profile：**

| 字段 | 说明 | 默认 |
|---|---|---|
| `orchestrator_model` | 监工自身模型（便宜快档） | profile 指定 |
| `allowed_models[]` | executor 可选模型清单（监工按难度从中选） | profile 指定 |
| `default_executor_model` | 难度判不出时的兜底（未配时回退 `orchestrator_model`，T743） | profile 指定 |
| `max_concurrent_tasks` | 一个 agent 名下最大并发 executor 数 | **3** |

**task：**

| 字段 | 说明 |
|---|---|
| `model`（可空） | 设了则**硬覆盖**，优先级最高，不问监工 |

---

## 11. 两个主流程

### 11.1 Orchestrator 主循环（常驻、事件驱动）

```
唤醒事件三类：
 (1) chat 新 @消息（经 mcp 收）
 (2) 中心新 task / task 解阻
 (3) executor 退出 / status 变更

收到 (1)(2) 新工作：
  a. 一致性路由：查 routing.json，属于某在跑 problem 吗？
       是 → 归并（喂给该 problem，或排队等其 executor 出来再续）
       否 → 新建 problem 上下文，记入 routing.json
  b. 选模型：task.model 有 → 用；无 → LLM 判难度 → allowed_models 选 → 兜底
  c. 聚合 context（problem 为中心）：跨 chat 相关历史 + 记忆祖先链 + issue spec + resume
  d. 写 input.json + 备 worktree → spawn executor（不给 mcp / 凭据）
  e. 并发约束：超 max_concurrent_tasks 则排队，不硬起

收到 (3) executor 完成：
  f. 读 exit + status + output.json（双信号判定，§9）
  g. 统一回写（仅监工）：发结果到来源 chat / 写记忆 / 更新中心 task（complete / block）
  h. 清理或保留 worktree → 看队列拉起下一个
```

### 11.2 Executor 状态机（纯计算、无中心连接）

```
SPAWNED → 读 input.json → RUNNING（边跑边写 progress.jsonl + 刷 last_progress_at）
        → 成功：写 output.json + status=done → exit 0
        → 失败：status=failed + error → exit != 0
```

---

## 12. 崩溃恢复与单点风险

- **监工是单点**：它崩了，名下 executor 全成孤儿。缓解：**文件即 durable 状态**——监工
  重启后扫 `executors/` 目录 + `routing.json` 即可重建「在管哪些 executor、各自
  goal / 状态」（进程是否活 + output.json / status 是否已出），不丢、不重复拉起。对齐
  CodingClaw 的 reconciler 兜底思路。
- **多 worker（未来）**：若日后要多 worker 共享，per-agent 并发计数不能只放进程内存
  （会被放大 N 倍），须做成 **DB-owned lease + `SKIP LOCKED`**。本期单 worker 不涉及，
  但留此扩展点，避免重蹈 CodingClaw「lease 字段留位没写」的债。

---

## 13. 与 CodingClaw 的对照

| 维度 | CodingClaw | 本设计 |
|---|---|---|
| 串行键 | `singletonKey = sessionId`（session≈chat，白捡） | 自定义：监工对中心串行、executor 间并行 |
| 选模型 | 绑身份的静态值 `profile.defaultModel`（异构靠身份） | **task.model 硬覆盖 → LLM 判难度 → allowed_models → 兜底**（按难度动态，原创增量） |
| fan-out | delegation barrier：父挂起→子建独立 session→汇合唤醒 | 监工 spawn executor，文件交换，监工汇总 |
| 连接 / 凭据 | 中央 server 持唯一 Feishu 连接，daemon 不持凭据 | 监工持唯一 mcp 连接，executor 不持凭据（同构） |
| 防挂死 | watchdog + reconciler | watchdog（§9）+ 重启扫目录重建（§12） |

**最大差异**：CodingClaw 没有「按难度选模型」（它是身份静态值），这正是本设计的原创
增量；且我们没有「chat=session」前提，故须引入 problem 层做一致性路由。

---

## 14. 未来扩展

- 跨机器 dispatch / 多 worker 物理调度（须先做 DB lease + SKIP LOCKED）。
- executor 自升级（默认便宜档卡住 / 失败自动 escalate 重档），作为难度路由的补充。
- progress.jsonl → chat 的实时状态转播粒度与节流策略。
