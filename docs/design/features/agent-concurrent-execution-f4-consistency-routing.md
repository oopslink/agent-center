# F4 实现说明 — 一致性路由（problem 映射）

> 父设计：[agent-concurrent-execution.md](agent-concurrent-execution.md)（§8 / §11.1 step a / §12）。
> 本文档记录 F4（cycle v2.17.0 plan-9c606650 / 任务 T520）的**实现层**落点，对应包
> `internal/workerdaemon/executor` 新增文件 `routing.go`。F1（进程模型）、F2（文件协议）、
> F3（模型路由）、F5（完成信号/watchdog/崩溃恢复编排）与本层正交：监工主循环消费本层，
> 不在本范围内实现。

## 1. 范围与问题

同一 agent 在多个 chat 里聊**同一个问题**时，监工须把后续消息归并到同一执行脉络，
不为同一 problem 重复起 executor；确属新问题则新建。F4 只做**判定与持久化**：

- 一个独立的 durable 文件 `routing.json` 记录 problem → chat/issue/task/executor 映射；
- 一个**纯函数**路由判定，给定一条消息信号，回答「属于哪个在跑 problem，还是新建」。

**不**做：problem_id 生成、LLM 判难度/语义归并、实际 spawn/喂 executor、回写中心——
这些属监工（F1/F3/F5 及监工本体），消费本层。problem_id 由调用方提供，本层从不铸造。

## 2. 磁盘契约（design §8）

```
<agent_root>/routing.json        # 独立文件，与 executors/ 平级（非 per-executor）
{
  "problems": [
    {
      "problem_id": "...",
      "issue_ref": "issue-...",   // 显式绑定——最高优先键
      "task_refs": ["task-..."],  // 显式绑定（见 §3 优先级）
      "chat_ids": ["channel-..."],// 引用此 problem 的 chat
      "executor_ids": ["..."],    // 为此 problem 起过的 executor
      "created_at": "..."
    }
  ]
}
```

`<agent_root>` = 既有 per-agent home，`routing.json` 与 `executors/` `tasks/` `memory/` 平级
（`Layout.RoutingPath()`）。它是监工的 durable 状态：崩溃重启时与 `FileExchange.Scan()`
合用即可重建「哪个 problem 拥有哪些 executor」，不丢、不重复拉起（design §12）。

> `task_refs` 是对 design §8 schema 的忠实补充：设计正文写明「以 issue/**task** 显式绑定
> 为主」，而 schema 示例只画了 `issue_ref`。F2 的 `SourceRefs` 已携带 `TaskRef`，故这里把
> task 显式绑定落为 `task_refs[]`，与 issue 绑定对称。

## 3. 路由判定（`RoutingTable.Route`，纯函数）

优先级链（design §8「以 issue/task 显式绑定为主，LLM 语义归并仅作辅助提示」）：

```
issue_ref ▸ task_ref ▸ chat_id ▸ semantic_hint ▸ 无匹配→新建
```

1. **issue_ref**：中心签发、确定且权威——带已知 issue 的消息归并到该 problem，无视来源 chat。
2. **task_ref**：同上，task 显式绑定。
3. **chat_id**：来源 chat 已映射到某 problem → 该 chat 的后续消息默认延续（连续性）。
4. **semantic_hint**：监工 LLM 猜的 problem_id，**仅当**命名了已存在 problem 才采纳；
   猜了不存在的 id 直接忽略（辅助提示，不喧宾夺主）。
5. 都不中 → `Decision{IsNew:true}`，调用方新建 problem。

`Decision` 带 `Reason`（`issue_ref`/`task_ref`/`chat_id`/`semantic_hint`/空），让监工能
日志/inspect「**为何**归并」，而非把路由当黑盒。`Route` 只读不改。

## 4. API（监工主循环 §11.1 step a 的两支）

`RoutingStore{path, clk}`，注入 clock 保证测试确定性（§14.x）。监工是唯一写者（design §3），
故每个写操作走「load → 改 → 原子 save」，无需跨进程锁。

- `Route(Signal) (Decision, error)`：「这条消息属于哪个 problem？」（只读）。
- `Register(Problem) error`：**否→新建** 分支。`created_at` 为零时按 clock 盖戳；
  problem_id 由调用方给，重复 id 报错。
- `Merge(problemID, Signal, executorIDs...) error`：**是→归并** 分支。把来源 chat、
  消息携带的 issue/task 显式 ref、以及新 spawn 的 executor id 并入既有 problem；幂等
  （重复归并同一 chat/executor 为 no-op）。

表层纯方法（`RoutingTable`）：`Add` / `Find` / `AttachChat` / `AttachExecutor` /
`AttachTaskRef` / `SetIssueRef` / `Validate`。集合字段一律 set-union 去重。

读写规则（沿用 F2）：

- 写：复用 `exchange.go` 的 `writeJSONAtomic`（temp+rename+O_NOFOLLOW），崩溃不留 torn 文件。
- 读缺失：`Load` 把缺文件视为**空表**（agent 尚无在跑 problem，design §12），非错误。
- 读损坏/非法：显式报错，绝不静默零值（§17）——torn/非法表浮出，而不是悄悄丢掉在跑路由。

## 5. 不变量

- `problem_id` 必填、非空、无 null 字节（身份键；但**不**入文件路径，故无需 path-segment 校验）。
- `created_at` 必填（`Register` 盖戳，手写表缺它则 `Validate` 拒绝）。
- 表内 `problem_id` 唯一（重复会令路由歧义）。
- `issue_ref` 单值：重绑到**不同** issue 被拒（拒绝静默覆盖权威绑定），重设同值为 no-op。
- `executor_id` 入表前过 `validateExecutorID`，routing.json 绝不记录指不到真 executor 目录的 id。

## 6. 测试（`routing_test.go`）

- `Route` 优先级表驱动：issue>task>chat>semantic、未知 hint 忽略、空信号/未知 ref→新建。
- 表层 CRUD：Add 去重/拒重、Attach* 幂等 set-union + 校验、SetIssueRef 重绑拒绝。
- Store：缺文件→空表、损坏/非法→报错、Save 校验/拒 nil、Register 盖戳/保留显式 created_at/拒重、
  三个 wrapper（Route/Register/Merge）传播 load 错误。
- 验收 E2E（`TestRouting_CrossChatMerge_AcceptanceFlow`）：同一 problem 跨 chat-1/chat-2
  经同一 issue_ref 归并到单 problem（executor 不重复起）；chat-2 后续无 ref 凭 chat 连续性
  仍归 A；chat-3 带新 issue 正确新建——精确对应任务验收标准。

包行覆盖 ≥90%（新文件 `routing.go` 同档；含 wrapper 的 load-error 传播路径）。
