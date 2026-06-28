# W2 实现说明 — Writeback 连中心回写

> 父设计：[agent-concurrent-execution.md](agent-concurrent-execution.md)（§3 唯一写入者 /
> §9 完成信号 / §11.2 step g 统一回写）。阶段二 issue-b8687f2a「让并发真正在生产跑」。
> 本文档记录 W2（cycle v2.18.0 plan-4751e59e / 任务 T541）：把 W1 留的 `executor.Monitor`
> Writeback seam 接成**真实中心回写**——executor 完成后由监工（唯一写入者）把结果回写中心。

## 1. 范围（W2）

- **真 Writeback**：实现 `executor.Writeback`，在 `Monitor.Finalize` 拆目录**之前**回写中心：
  - **Succeeded → `complete_task`**（agent-tool 在同一中心事务里把 summary 发到 task 会话 **并** 完成 task）；
  - **Failed/Crashed → `block_task`**（同一事务发 reason 到 task 会话 **并** block，§9「按失败处理」；crash 在文案里标 retryable）；
  - 无 task 但有来源 chat 的工作 → `post_message` 兜底转播。
- **唯一写入者**：W2 的 `CenterWriteback` 带互斥锁——daemon 每个 executor 各自 goroutine reap，锁让并发完成**串行化**，回写不交叉（design §3）。
- **只经 transport**：全程走 `CenterClient` 端口 → daemon `*AdminClient.CallAgentTool`（agent-tools 端点，worker bearer + agent_id），**不直碰中心存储**（规约 §0.4）。

**含 W1 覆盖回填**：本分支 cherry-pick `7f381bd7`（3 个 _test.go + W1 doc「覆盖率」节，**纯 test/doc**，与 W2 生产代码零交集），把 W1 `orchestrator` 包覆盖 70.1%→97% 随 W2 一并回主干（PD 裁定方案 2）。生产代码 `21b352dd↔7f381bd7` 零差异（dev1/dev2 双核）。

## 2. 落点

| 文件 | 职责 |
|---|---|
| `internal/workerdaemon/orchestrator/writeback.go` | `CenterWriteback`（实现 `executor.Writeback`）+ `CenterClient` 端口 + `MemoryWriter` 可选 seam。读 executor `input.json` 取来源 refs（Finalize 在拆目录前回写，故 input 仍在）；按 Completion.Kind 路由；互斥串行；未知 kind / 无来源 / 读 input 失败均显式报错（§17）|
| `internal/workerdaemon/center_client.go` | `agentToolCaller` 端口 + `centerClientAdapter`：把 `CenterClient` 桥到 `*AdminClient.CallAgentTool`（complete_task / block_task / post_message 的 body 形状），`newCenterClient(nil)→nil`（优雅降级）|
| `internal/workerdaemon/agent_controller.go` | `AgentControllerConfig.ToolCaller`（可选 seam）|
| `internal/workerdaemon/run.go` | 生产接线 `ToolCaller: client`（*AdminClient）|
| `internal/workerdaemon/concurrent_exec.go` | `buildExecutorEngine`：ToolCaller 非空 → 建 `CenterWriteback` 传给 `Monitor`；否则 nil Writeback（W1 行为，优雅降级）|

## 3. 调用链（§11.2 step f→g）

```
drainExecutor → Monitor.AwaitCompletion(handle)
  → harvest(exit + output.json + status) → Classify（双信号 §9）
  → Monitor.Finalize(Completion):
      1. Writeback.Report(c)   ← W2：唯一中心写
           lock → ReadInput(execID) 取 Source.TaskRef/ChatIDs/Goal
           Succeeded → CenterClient.CompleteTask(agentID, taskRef, summary)
           Failed/Crashed → CenterClient.BlockTask(agentID, taskRef, reason, "obstacle")
           无 taskRef 有 chat → PostMessage 兜底；都无 → 显式报错（不静默丢 §17）
      2. Pool.Release（腾并发槽）
      3. 终态拆 worktree + 目录；crash（retryable）保留目录待重跑
  （Report 失败 → Finalize 返回 err → 目录保留，结果不丢）
```

来源路由口径：W1 当前只把 `TaskRef` 串进 `input.json`（ChatID/IssueRef 是 W1 的 deferred plumbing），故 W2 主路径＝**complete/block 来源 task**——而 complete_task/block_task 本身把 summary/reason **原子发到该 task 的会话**，因此「结果出现在来源 chat」与「task 正确 complete/block」由**一次原子调用**同时满足（对齐规约 §0.4 same-tx）。

## 4. 规约自检（conventions §15）

- **§0.4 AppService 唯一入口**：回写只经 `CenterClient`→`*AdminClient.CallAgentTool`（agent-tools transport），daemon 不直读写中心存储。executor 仍不连中心。
- **§2 可观测性**：complete_task/block_task 会在中心 emit 既有领域事件 + 在 task 会话留可见消息（结果/原因可见）；W2 不新增事件类型，复用既有 agent-tool 端点的事件面。
- **§3 唯一写入者**：`CenterWriteback` 互斥锁串行化并发完成回写——根除并行覆盖。
- **§16 reason+message**：失败回写 reason 同时带机器可读 kind（`[runner_failed]` 等）与人类可读 message。
- **§17 错误不吞**：每个回写错误 return（Finalize 据此保留目录）；未知 Completion.Kind / 无来源 / 读 input 失败显式报错，不静默丢结果。
- **§4 零 LLM SDK / §10 单二进制 / §9 无 SQL·FK**：无新依赖、无新 binary、无 schema 变更。

## 5. 已知边界 / deferred（W2 诚实披露）

- **记忆回写未接（留 seam）**：design §11.2 提「写记忆（scope 到本 executor 最具体层，不并发覆盖）」。W2 **不实现** daemon 侧记忆写，因为：(1) 记忆是 supervisor 自管的 per-agent git 仓（`internal/cognition/memory`），daemon 直写会越过其 ownership（§0.4 跨域）；(2) scope 所需的 **project id 尚未 plumb** 进 WorkItem/Completion（W1 只串了 TaskRef）。已留 `MemoryWriter` 端口（nil-safe），且 `CenterWriteback` 的互斥锁已为「不并发覆盖」备好——待 scope plumb + 写入方（daemon vs supervisor）定论后由 follow-up 接。**已 @PD 标记此范围决策。**
- **crash 的真重试策略**：Failed 与 Crashed 当前都 `block_task(obstacle)`（crash 文案标 retryable）；真正的自动重新入队（Monitor 已为 crash 保留目录）留 W3/后续。
- **来源 chat plumbing**：ChatID/IssueRef 全程接通依赖 W1 deferred 的结构化 goal 下发；当前以 TaskRef 为主路径（其会话即来源 chat），ChatIDs 兜底已实现待 plumb 后即生效。

## 6. 测试

### W2 自身新代码覆盖（与 W1 回填分列，PD 要求）
| 文件 | 覆盖 |
|---|---|
| `orchestrator/writeback.go` | ~96%（reportFailure/relayToChat 残余为 PostMessage 错误等防御分支）|
| `center_client.go` | **100%** |
| `concurrent_exec.go: buildExecutorEngine`（W2 writeback 分支）| 76.9%（其余残余为 New*-err 防御 + 集成-only glue，沿用 W1/F1/F2 口径）|

### W1 覆盖回填（cherry-pick 7f381bd7，非 W2 产出）
`orchestrator` 包整体 70.1%→**96.3%**（idminter 三函数 100%、engine 错误分支补齐）；executor 包 94.0%。

### 用例清单与结果
| # | 用例 | 文件 | 结果 |
|---|---|---|---|
| 1 | `NewCenterWriteback` 校验（nil client/fx、空 agentID）| writeback_test.go | PASS |
| 2 | Succeeded → complete_task（summary=status.summary）| writeback_test.go | PASS |
| 3 | summary 回退链（status.summary→output.result→goal title）| writeback_test.go | PASS |
| 4 | Failed → block_task(obstacle)，reason 含 kind+message | writeback_test.go | PASS |
| 5 | Crashed → block_task，文案标 retryable | writeback_test.go | PASS |
| 6 | Failed 无 error detail → 兜底文案 | writeback_test.go | PASS |
| 7 | Running → no-op | writeback_test.go | PASS |
| 8 | 无 taskRef 有 chat → post_message（跳过空 chatID；成功+失败两路）| writeback_test.go | PASS |
| 9 | 无 task 无 chat → 显式报错（§17 不丢）| writeback_test.go | PASS |
| 10 | input.json 缺失 → 报错 | writeback_test.go | PASS |
| 11 | 未知 Completion.Kind → 报错 | writeback_test.go | PASS |
| 12 | 中心调用失败 → 传播（目录保留）| writeback_test.go | PASS |
| 13 | MemoryWriter 被调用 + 失败显式报错 | writeback_test.go | PASS |
| 14 | clip 截断/trim | writeback_test.go | PASS |
| 15 | 并发 Report 串行不竞态（sole writer）| writeback_test.go | PASS |
| 16 | centerClientAdapter complete/block/post 的 tool+body 形状；newCenterClient(nil)→nil | center_client_test.go | PASS |
| 17 | buildExecutorEngine 在 ToolCaller 下建出 writeback | concurrent_exec_w2_test.go | PASS |

### 出口标准核对
- [x] executor 完成 → 中心 task 正确 complete（成功）/ block（失败·崩溃），结果原子出现在 task 会话
- [x] 唯一写入者串行，回写不互覆
- [x] 只经 transport（§0.4），不直碰中心存储
- [x] W2 自身新代码行覆盖 ≥90%（writeback.go ~96% / center_client.go 100%）
- [x] `go build ./...` / `go vet ./internal/workerdaemon/...` / `gofmt` 干净；workerdaemon 全量回归绿
- [~] 记忆回写：**显式 deferred + 留 seam + 已标 PD**（理由见 §5）

### 结论
W2 把 W1 的 Writeback seam 接成真实中心回写：executor 完成由监工（唯一写入者、经 transport）原子地 complete/block 来源 task 并把结果发进其会话；并发回写串行不互覆。记忆回写按边界与 plumbing 现状显式 deferred 并留 seam。
