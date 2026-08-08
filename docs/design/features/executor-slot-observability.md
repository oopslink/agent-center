# Executor Slot 编号与并发可观测性

> 决策依据：[ADR-0056](../decisions/0056-stable-executor-slots.md)。
> 本文是功能/契约设计；具体代码改动按 §9 实施切片推进。

## 1. 目标与非目标

目标：让操作者在 Agent Detail、Activity 侧面板和 Agent 列表中稳定识别一个 Agent 的
固定并发槽位，快速判断占用率、排队量、每个槽正在执行的任务和健康状态。

非目标：

- 不替换 `exec-{ULID}`，不改 executor 目录、git branch、日志、peek、recovery 或 delivery audit 主键。
- 不把 Slot 配成绑定 CLI/model 的静态机器；模型仍按每次 Run 路由。
- 不新建长期 Slot 历史表；历史属于 Execution Run / Agent Activity，实时状态属于 live snapshot。

## 2. 当前实现基线

现有链路可直接扩展：

1. `executor.Pool.active map[executorID]*Handle` 执行 per-agent 并发闸门，但只计数，没有 Slot identity。
2. `ExecutorEngine.SnapshotConcurrency()` 合并 `Pool.Handles()` 与 adopted orphans，生成
   `concurrency.ExecutorSnapshot`。
3. agent-runtime `/concurrency` 经 worker daemon heartbeat 上报 Center
   `concurrency.LiveStateStore`。
4. `GET /api/agents/{id}/concurrency` 拼接 profile cap、ProjectManager queued/running 与 live snapshot。
5. Web `useAgentConcurrency` 每 3 秒轮询；Agent Tasks 已显示 `active/cap` 与三态 freshness。
6. Agent Activity 已发出 `executor.start/progress/stop`，目前仅展示 execution id 尾部。

Slot 状态必须扩展这条唯一链路，不能另造 Center DB 状态源。

## 3. 统一语言与不变量

- **Executor Slot**：Agent 内的有限运行时资源；值对象 identity 为 `(agent_id, slot_index)`。
- **Execution Run**：一次实际 fork；唯一 identity 为 `execution_id=exec-{ULID}`。
- **Slot Assignment**：一个 Run 在活跃生命周期内对一个 Slot 的独占绑定。

不变量：

1. `0 <= slot_index < runtime_effective_cap`。
2. 同一 Agent 同一时刻，一个 Slot 最多绑定一个 Run；一个活跃 Run恰好绑定一个 Slot。
3. launch reservation、starting、running、finishing、orphan 都占槽；仅最终 `Release` 释放。
4. restart/adopt 后恢复原 Slot；Slot 不因进程重启或 snapshot 顺序变化而跳号。
5. Slot 可被后续 Run 复用；历史记录始终按 execution id 隔离。

## 4. Runtime 设计

### 4.1 Pool 双索引

```go
type SlotAssignment struct {
    SlotIndex  int
    ExecutorID string
    Handle     *Handle // nil = launch reservation / adopted orphan
}

byExecutor map[string]*SlotAssignment
bySlot     []*SlotAssignment
configuredMax int
admissionMax  int
```

全部状态变更在 Pool mutex 内：

- `Launch` 分配最小空闲 Slot并创建 reservation，再执行锁外 provision/spawn；任一步失败释放同一 Slot。
- `Release(executorID)` 同时清理两个索引，幂等。
- `Adopt(executorID, preferredSlot)` 恢复持久化编号。原 Slot 被不同 Run 占用属于恢复不变量冲突：
  不静默换号，记录明确错误并把冲突 Run 置为 recovery-required，防止两个进程冒充同一 Slot。
- `Assignments()` 返回包含 reservation/handle-less orphan 的复制快照；观测不再只依赖 `Handles()`。

### 4.2 Durable Slot Assignment

在 spawn 前确定 Slot，并在两个本地协议中增加可选字段：

```go
SlotIndex *int `json:"slot_index,omitempty"`
```

- `executor.Input`：在 `WriteInput` 前已携带 Slot，snapshot 与 Activity 可读取。
- `executor.Record`：spawn 后随 pid 写入，restart recovery 优先读取；缺失时回退 Input。

使用 `*int`，因为 `0` 是合法编号。旧 Run 两处都无字段时，按
`(spawned_at, executor_id)` 稳定排序后分配最小空闲槽，并原子回写 Record；禁止依赖目录或 map 遍历顺序。

### 4.3 动态 cap

当前 `UpdateExecutorConfig` 只刷新 model router，Pool 的 `max` 在构建后固定。落地必须新增
`Pool.Resize(max)`，并由 live reconcile 同时更新 routing config 与 Pool cap：

- 扩容：立即追加 Slot，允许新 admission。
- 缩容：设置 `configuredMax=M`；若 `slot_index >= M` 仍活跃，则进入 draining，
  `admissionMax=M` 且不再向高位分配，不迁移或终止 Run。高位槽释放后物理数组收敛。
- `configuredMax<=0` 仍按既有 effective cap 规则归一化。
- 并发 `Launch/Release/Resize/Snapshot` 必须通过同一 mutex 并跑 race 门禁。

## 5. Snapshot 与 API 契约

扩展 worker snapshot：

```go
type ExecutorSnapshot struct {
    SlotIndex *int `json:"slot_index,omitempty"`
    // existing fields unchanged
}

type AgentSnapshot struct {
    AdmissionCap  int                `json:"admission_cap,omitempty"`
    SlotCount     int                `json:"slot_count,omitempty"`
    ConfigVersion int                `json:"config_version,omitempty"`
    Active        int                `json:"active"`
    Executors     []ExecutorSnapshot `json:"executors"`
}
```

`admission_cap` 是新 Run 的当前准入上限；`slot_count` 是当前可寻址 Slot 数（缩容 draining
期间可能大于 admission cap）；`config_version` 可直接复用现有 ResumeAgent/reconcile 的
Agent version，用于识别配置尚未到达 runtime。它们共同防止 Center 用新 profile cap 给仍
运行旧配置的 runtime 虚构 Idle Slot。Center 响应新增：

```json
{
  "configured_cap": 4,
  "admission_cap": 4,
  "slot_count": 4,
  "active": 2,
  "queued": 1,
  "slot_stable": true,
  "slots": [
    {"slot_index":0,"state":"running","executor_id":"exec-...","task_id":"task-..."},
    {"slot_index":1,"state":"idle"},
    {"slot_index":2,"state":"orphan","executor_id":"exec-..."},
    {"slot_index":3,"state":"idle"}
  ],
  "reachable": true,
  "has_snapshot": true,
  "stale": false,
  "snapshot_age_ms": 1200
}
```

规则：

- 只有 fresh snapshot 才把未占用位置断言为 `idle`。
- 缩容 draining 时按 `slot_count` 保留高位 Slot，并显示 `target {configured_cap} · draining`；
  摘要不把 `active > configured_cap` 误报为 over-admission。
- expired snapshot 展示 last-known Slot，整体标 stale；worker offline 展示 Offline，不把未知状态写成 Idle。
- 从未收到 snapshot 时不生成虚假的 Slot assignment，只显示 Center task load fallback `~running/cap`。
- 保留现有 `cap` 与 `executors` 至少一个兼容版本。
- 旧 worker 未上报 `slot_index` 时返回 `slot_stable=false`；UI 显示 active/cap，但不按数组顺序伪造 `#N`。
- executors/slots 按 `slot_index` 升序，保证 wire 与 UI 稳定。

## 6. Activity 契约

现有 `executor.start/progress/stop` AgentActivityEvent payload 增加：

```json
{"slot_index":0,"executor_id":"exec-..."}
```

- payload 固化事件发生时的 Slot；历史页面不反查当前 Slot。
- `interaction_ref` 与 progress grouping 继续使用 `executor:{execution_id}`，不能改用 Slot。
- start 必须在获得 Slot 后发送；stop 在 Release 前发送，确保同一 Run 的所有事件编号一致。
- 这是既有 Agent Activity 事件的字段扩展，不新增 Domain Event。

默认预览：`Executor #0 · T1274 · Running 8m`；展开显示完整
`Executor #0 · exec-409d0782...` 与现有诊断字段。

## 7. Web 产品落点

### 7.1 Agent Detail

Overview 顶部增加 `Executors 2/4 active · 1 queued` 与 Slot 列表：

- `#0 Running`：task ref、CLI/model、运行时长、最后进展、current activity。
- `#1 Idle`。
- `#2 Orphan / Recovering`：告警样式。
- `#3 Finishing / Starting`：明确过渡态。

### 7.2 Activity 侧面板

顶部复用同一个实时 Slot 组件，下方保留历史 timeline。实时状态来自 `/concurrency`，
历史来自 AgentActivityEvent，视觉相邻但不混成一个来源。

### 7.3 Agent 列表

增加 `active/slot_count` 紧凑指标。fresh snapshot 显示 `2/4`；无 snapshot 时沿用
Center load fallback，显示 `~2/4`；offline/expired 使用已有 freshness 语义。

## 8. 异常与降级

- duplicate Slot、越界 Slot、active 与 assignment 数不一致：runtime fail-loud 并上报 snapshot
  integrity error，Center/UI 显示 degraded，不自行重排掩盖问题。
- heartbeat 丢失：保留 last-known snapshot + age，不删除历史状态。
- launch 在 reservation 后失败：释放 Slot，不发 start 事件。
- process 已 spawn 但 Record 写失败：沿用现有 fail-closed kill；Slot 随 launch 失败释放。
- Center/worker 混合版本：通过 optional fields 与 `slot_stable` 降级，execution 主链不受影响。

## 9. 实施切片

1. **Runtime Slot allocator**：Pool 双索引、Input/Record、Launch/Release/Adopt、稳定 legacy backfill。
2. **Live cap resize**：`Pool.Resize`、reconcile wiring、draining semantics。
3. **Observability contract**：snapshot、heartbeat、Center API、integrity/freshness、mixed-version compatibility。
4. **Activity**：start/progress/stop 贯穿 `slot_index`，历史 grouping 保持 execution id。
5. **Web surfaces**：共享 Slot panel、Agent Detail、Activity sidebar、Agent list。
6. **Independent acceptance**：真实并发、restart adopt、resize、offline/stale、mixed-version 与 UI 回归。

依赖顺序：1 → 2/3；3 → 4/5；1-5 → 6。Runtime 与 Web 不在 API 契约冻结前并发猜字段。

## 10. 验收门禁

- Pool：并发抢槽唯一、最低空闲号、失败释放、幂等 Release、原槽 adopt、冲突 fail-loud、旧记录稳定补号。
- Race：`make test-race` 覆盖 `Launch/Release/Adopt/Resize/Snapshot`。
- Contract：新旧 heartbeat 与 `/concurrency` JSON 兼容；fresh/stale/offline/nodata 不混淆。
- Activity：同一 Run 三类事件 Slot 一致；Slot 复用后历史仍按 execution id 分组。
- Web：所有 Slot 状态、3 秒刷新、fallback、展开完整 execution id、移动端可读。
- E2E：cap=3 并行两任务显示 `#0/#1`；释放 `#0` 后下一 Run 复用 `#0`；worker restart
  后存活 Run 不换号；缩容 draining 不杀在飞 Run。
- 全量：`go test ./...`、`make test-race`、`go vet ./...`、`go build ./...`、
  `cd web && pnpm test && pnpm build && pnpm lint`、`git diff --check`。

## 11. 五轮 Grill 记录

1. **领域与 identity**：否决 `exec-0` 替换 Run ID；明确 Slot 与 Execution Run 分层。
2. **并发与恢复**：补齐 spawn 前持久化、原槽 adopt、duplicate fail-loud、legacy 稳定补号。
3. **配置与容量**：发现当前 live config 只更新 router、不更新 Pool max；补 `Resize + draining`。
4. **观测真实性**：发现 profile cap 与 runtime cap 可能漂移；补 `runtime_cap/config_version`，禁止
   Center/前端伪造 Idle 与临时编号。
5. **兼容与验收**：保留 execution 主链和旧 API 字段；明确 mixed-version、Activity 不串槽、race/E2E 门禁。
