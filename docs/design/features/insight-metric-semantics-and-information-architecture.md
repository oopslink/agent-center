# Insight 指标语义与信息架构整改

| 字段 | 值 |
|---|---|
| 状态 | Proposed（供开发与 Owner 真实页面验收） |
| 日期 | 2026-08-29 |
| 关联 Issue | `issue-ea949e06` |
| 审计基线 | `origin/main@1c216dd586462a230e9e9bccdd2502e73a5c4d1e` |
| 指标契约 | [Insight Phase 1 contract](insight-phase-1-contract.md)（继续冻结，不修改公式） |

本文只整改 Insight Phase 1 的产品信息架构、展示语义和文案，并定义实现所需的加法式 API 字段。本文不改变 24 小时窗口、事实来源、join key、完成/失败定义、分位数算法、slot utilization 或 coverage 公式。

## 1. 整改目标与产品问题

Insight Overview 必须让组织 Owner 在首次打开页面时可靠回答两个问题：

1. 过去 24 小时执行了多少次、失败多少、排队和执行通常/长尾耗时多少？
2. 容量利用率是否可判断；若不可判断，是因为没有 slot 观测、观测覆盖不足，还是数据已过期？

Overview 是组织级聚合决策页；TaskExecution list 是带筛选上下文的事实明细页；TaskExecution detail 是单次执行尝试的解释页。三者不能再共用一个页面内的临时展开区。

Owner 真实页面复核给出的五类问题全部作为本设计的阻断验收项：

- coverage 约 `0.1%` 时，页面仍把 `slot_utilization=0` 显示成代表过去 24 小时的 `0%`；
- `outcome`、`quality` 等内部枚举直接暴露；
- 毫秒值只被格式化为 `ms/s`，长耗时与滚动时间窗难读；
- Overview、TaskExecution list/detail 的入口、层级和筛选上下文混杂；
- Agent/Project 排名缺少排序口径、分位数样本数和分母说明。

## 2. 现状、API 与真实生产链审计

### 2.1 当前页面与路由

| 观察 | 代码证据 | 影响 |
|---|---|---|
| 侧栏仅有 `Insight > Overview` | `web/src/AppLayout.tsx` | TaskExecution list 没有稳定入口 |
| Overview 路由存在；只有单条 execution detail 路由 | `web/src/App.tsx` | list 不是独立、可分享、可返回的页面 |
| `Execution details` 与 Agent/Project 点击均在 Overview 底部展开临时表格 | `web/src/pages/InsightOverview.tsx` | 汇总与明细争夺同一视觉层级；筛选不进 URL |
| Overview 与 detail 使用同一个表格组件；detail 仍是一行横向表 | `web/src/pages/InsightOverview.tsx` | 单次执行缺少时间线和解释结构 |
| 所有产品文案硬编码为英文，未使用已有 `insights` i18n namespace | `web/src/pages/InsightOverview.tsx`、`web/src/i18n/locales/{en,zh}/insights.json` | 中文环境仍暴露英文和内部词 |
| `formatMs` 大于一秒后永远只显示秒 | `web/src/pages/InsightOverview.tsx` | 分钟/小时级耗时不可扫读 |
| 排名表显示 p50/p95 数值，但不显示各自 `samples` | `web/src/pages/InsightOverview.tsx` | 小样本看起来与大样本同样可靠 |

### 2.2 权威事实链与真实数据形状

本设计只消费当前生产链已有事实：

```text
worker heartbeat
  -> concurrency.AgentSnapshot
  -> agent_concurrency_observations (SQLite, durable)
  -> slot_interval_fact (DuckDB projection)
  -> slot_utilization + slot_coverage_ratio

agent.fork_executor command
  -> worker_control_events
  -> queue_interval_fact
  -> queued_at / command_status / status_reason / status_detail

executor.start / executor.stop / executor.recovery_quiet_finalized
  -> agent_activity_events
  -> execution_fact
  -> started_at / finished_at / outcome / failure_reason / recovered / quality

pm_tasks + pm_projects + agents
  -> snapshot dimensions
  -> task/project/organization/agent display fields
```

落点与约束：

- heartbeat 在 `internal/admin/api/workforce.go` 先调用 `InsightObservations.Append`，再更新 `LiveState`；持久表由 migration `0145_insight_concurrency_observations` 定义。
- projector/API 在 `internal/insight/service.go` 与 `helpers.go`；Web handler 在 `internal/webconsole/api/handlers_insights.go`。
- Overview 现有 DTO 真实返回 `window/as_of/refreshed_at/freshness/summary/agents/projects/diagnostics`；前端类型在 `web/src/api/insights.ts`。
- `summary.queue_wait_ms` 与 `summary.execution_duration_ms` 均为 `{p50,p95,samples}`；空样本为 `null/null/0`，不是零耗时。
- `outcome` 的生产值为 `succeeded|failed|crashed`；恢复路径额外投影 `quiet_finalized`。`quality` 当前可生成 `valid|invalid_time_order`。
- 预启动 command 以兼容展示 ID `command:<command_id>` 进入 execution list，可能只有 `queued_at`，没有 `started_at/finished_at/outcome`。
- `executor.stop` payload 已有机器字段 `reason` 和人类字段 `detail`，但当前 Insight 只投影 `failure_reason`；`detail` 被丢弃。
- `worker_control_events` 的生产状态集合是 `pending|started|rejected|failed|expired`，并有配套的 `status_reason/status_detail`。当前 `queue_interval_fact` 只保留前两者而丢失人类可读的 `status_detail`，execution API 则三者均未返回，UI 无法可靠解释排队、拒绝、失败或过期命令。
- `execution_fact` 已有 `cli/model`，本轮产品目标不需要在 Overview 排名中展示它们。

heartbeat 的真实 JSON 形状来自 `concurrency.AgentSnapshot`，而不是 UI 自定义采样：顶层为 `admission_cap/slot_count/config_version/integrity/integrity_error/active/executors/slots`；每个 slot 为 `slot_index/executor_id/task_id/cli/model/state/started_at/pid/last_progress_at/current_activity`。Insight 只按冻结契约消费 admission、integrity、slot index/state 与 interval 时间，不用 `active` 反推 24 小时利用率。

### 2.3 已发现的 contract / implementation drift

以下不是新数据口径，而是当前代码相对已冻结 contract 的偏差；实现整改必须显式关闭，不能由 UI 掩盖：

| 偏差 | 当前代码 | 冻结/本设计要求 | 处置 |
|---|---|---|---|
| 排名排序计数 | `leaderboard()` 用窗口内所有 `finished_at` 行 `COUNT(*)` 排序 | 只按 `completed_executions` 的 terminal outcome set 排序 | backend query 对齐 terminal set，并加未知 outcome fixture |
| coverage 分母 | `slotUtilization()` 以窗口内 distinct observed slot 数 × 24h 作为分母 | 配置的 admissible capacity 随时间积分 | 以 admission-cap interval 计算；补 cap 变化与未观测 slot 的数值 fixture |
| Detail 窗口 | `Execution()` 只限制 org + execution ID，不限制 24h | Phase 1 detail 必须处于响应所示滚动 24h 上下文 | 用与 list 相同的 `COALESCE(finished,started,queued)` 半开窗口；窗外返回 404 |
| 503 envelope | handler 直接通用 `writeError` | rebuilding/unavailable 应返回可解析的 window/freshness envelope | 增加稳定 error DTO 与 handler contract test |
| reason/message | activity `detail`、queue `status_detail` 未进入 Insight DTO | 内部 reason 与人类 message 成对贯通 | 加法投影 nullable message；不补造历史文案 |

修正 coverage 分母是回到冻结公式，不是另立指标；数值修正必须在 backend contract test 中完成。本轮 UI 无权据页面值反推、平滑或修正 coverage。

### 2.4 当前统计口径（保持不变）

| 指标 | 当前权威口径 | 窗口归属 | 空分母 |
|---|---|---|---|
| Completed executions | 窗口内终止的 execution attempts；重试分别计数 | `finished_at` | `0` |
| Failed executions | `failed|crashed|quiet_finalized` | `finished_at` | `0` |
| Failure rate | failed / completed | 同上 | `null` |
| Queue wait | `started_at - queued_at`，仅真实 start 且时间顺序有效 | `started_at` | p50/p95 `null`，samples `0` |
| Execution duration | `finished_at - started_at`，仅终止且真实 start、时间顺序有效 | `finished_at` | p50/p95 `null`，samples `0` |
| P50/P95 | DuckDB `quantile_cont`，最终四舍五入到毫秒 | 见各指标 | `null` |
| Slot utilization | known admissible slot time 内 occupied / available | interval 与窗口相交 | `available_slot_ms=0` 时 `null` |
| Slot coverage | available / 窗口内配置的 admissible capacity 积分 | interval 与窗口相交 | 无可计算容量时 `null` |
| 排名顺序 | completed executions 降序，稳定 ID 升序，最多 20 行 | terminal `finished_at` | 空数组 |

“Task 已完成数”“按 Task 去重”“当前实时 slot”“日历今天”均不是上述指标的别名，文案不得混用。

## 3. 信息架构与下钻契约

### 3.1 页面层级

```text
Insight
├── Overview                         /organizations/:slug/insights/overview
└── Task executions                 /organizations/:slug/insights/executions
    └── TaskExecution detail        /organizations/:slug/insights/executions/:executionId
```

侧栏在本轮新增 `Task executions` 二级入口。它不是另一个指标模块，而是 Overview 指标的事实解释面。

### 3.2 入口、上下文与 URL

| 起点 | 操作 | 目标 | 必须保留的上下文 |
|---|---|---|---|
| Overview header | `查看全部执行记录` | Task executions list | `window=24h` |
| Agent 表一行 | 点击名称/`查看执行记录` | Task executions list | `window=24h&agent_ref=<exact canonical ref>` |
| Project 表一行 | 点击名称/`查看执行记录` | Task executions list | `window=24h&project_id=<exact id>` |
| List 筛选摘要 | 删除筛选 chip | 同一 list | 删除对应 query；保留其余 query |
| List execution row | 点击主要行/`查看详情` | TaskExecution detail | detail URL；navigation state 保存来源 list URL |
| Detail breadcrumb | `Task executions` | 来源 list | 优先回到完整来源 query；无来源时回到 `?window=24h` |
| Detail breadcrumb | `Insight Overview` | Overview | 无明细筛选 |

规则：

- `window=24h` 是可见但不可更改的固定上下文，不显示伪下拉框。
- `agent_ref`、`project_id` 必须原样传给现有 exact-match API；不得将 display name 当筛选键。
- 首轮不新增 outcome、quality、日期范围等后端筛选。页面可以展示这些字段，但不能做“仅筛当前已加载 50 条”的假筛选。
- cursor 分页必须进入 URL（`cursor`）；点击下一页、刷新、复制链接和浏览器返回都应保持筛选与页位。
- Overview 和 list 各自调用 API 并持有自己的 `as_of`。从 Overview 下钻后，list 明示其自己的精确窗口；不声称与先前请求逐毫秒相同。

## 4. 页面线框与组件结构

### 4.1 Overview

```text
Insight / Overview                         [查看全部执行记录]
过去 24 小时（滚动）
8月28日 16:30 – 8月29日 16:30（本地时间） · 更新于 16:30:08 · 数据最新

[已结束的执行 128] [失败率 7.8% · 10/128]
[容量利用率 数据不足 · 观测覆盖 0.1%]
[排队等待 典型 12秒 · 慢端 2分18秒 · 96 个样本]
[执行耗时 典型 8分04秒 · 慢端 41分12秒 · 121 个样本]

! 容量观测不足：仅覆盖过去 24 小时可用容量的 0.1%，暂不展示利用率。

按智能体查看（按已结束执行次数排序）
智能体 | 已结束 | 失败 | 排队等待 P50 / P95（样本） | 执行耗时 P50 / P95（样本） | 操作

按项目查看（按已结束执行次数排序）
项目   | 已结束 | 失败 | 排队等待 P50 / P95（样本） | 执行耗时 P50 / P95（样本） | 操作

数据说明：一次重试算一次 execution attempt；P50/P95 仅使用具有有效起止时间的样本。
```

组件建议：

```text
InsightOverviewPage
├── InsightPageHeader
├── InsightWindowContext
│   └── FreshnessBadge
├── InsightStateBanner
├── InsightSummaryGrid
│   ├── CountMetricCard
│   ├── RateMetricCard
│   ├── CoverageAwareUtilizationCard
│   └── PercentileMetricCard × 2
├── InsightCoverageNotice
├── InsightDimensionTable (agent)
├── InsightDimensionTable (project)
└── InsightMethodNote / InsightDiagnosticsNotice
```

### 4.2 Task executions list

```text
Insight / Task executions
过去 24 小时（滚动） · 8月28日 16:31 – 8月29日 16:31 · 数据最新
筛选：[智能体 Builder ×] [项目 Launch ×]                    [清除筛选]

执行记录（每行是一次 execution attempt）
状态 | Task / Project | Agent | 排队时间 | 开始时间 | 结束时间 | 排队等待 | 执行耗时 | 数据提示
失败   Ship UI / Launch Builder  15:00:00 15:00:01 15:05:01 1秒       5分钟     —

显示 50 条                                     [上一页] [下一页]
```

列表列不再显示 `quality` 原始值；正常数据不占用一列。只有异常时在“数据提示”显示 `时间数据异常` badge。窄屏时保留状态、Task、Agent、执行耗时四个主字段，其余进入展开区。

组件建议：

```text
InsightExecutionsPage
├── InsightPageHeader
├── InsightWindowContext
├── InsightFilterSummary
├── InsightStateBanner
├── TaskExecutionTable
│   ├── ExecutionStatusBadge
│   ├── HumanDuration
│   └── DataQualityBadge (exception only)
└── CursorPagination
```

### 4.3 TaskExecution detail

```text
Insight Overview / Task executions / exec-…                         [刷新]
TaskExecution                           [失败] [由系统恢复]
Ship UI · Launch · Builder

执行时间线
进入队列 15:00:00 ── 1秒 ── 开始 15:00:01 ── 5分钟 ── 结束 15:05:01

结果
执行失败
执行进程返回非零状态。（若 failure_message 可用）

关联信息
Task: Ship UI (task-…)        Project: Launch (proj-…)
Agent: Builder (agent:…)      Worker: worker-…
Execution ID: exec-…          Command ID: cmd-…

数据完整性（仅异常时展开）
时间顺序无效；该记录未参与排队/执行耗时分位数。
```

detail 必须使用定义列表/时间线，不得复用 list 的一行表格。ID 保留为技术关联信息，不能用 Task 状态替换 TaskExecution 状态。

## 5. 展示语义

### 5.1 coverage、未知与真实零值

`slot_utilization` 只代表“有观测覆盖的 known admissible slot time”，不是天然代表整整 24 小时。coverage 是利用率可解释性的前置条件。

以下阈值是 **UI 置信展示策略**，不是新指标，也不改变 API 数值：

| API 值 | 展示状态 | 卡片主值 | 辅助文案 | 是否显示数值利用率 |
|---|---|---|---|---|
| coverage `null` | unknown | `无法判断` | `没有可计算的容量基线` | 否 |
| coverage = `0` | unknown | `无法判断` | `过去 24 小时没有有效 slot 观测` | 否 |
| `0 < coverage < 0.5` | insufficient | `数据不足` | `观测覆盖 x% · 暂不展示利用率` | 否 |
| `0.5 ≤ coverage < 0.9` 且 utilization 非 null | partial | `x%（部分观测）` | `观测覆盖 y% · 结果可能不代表完整 24 小时` | 是，必须带限定词 |
| coverage `≥ 0.9` 且 utilization 非 null | representative | `x%` | `观测覆盖 y%` | 是 |
| coverage 非 null、utilization `null` | unknown | `无法判断` | `没有有效的可用 slot 时长` | 否 |

因此 production 反馈中的 `coverage≈0.1% + utilization=0` 必须显示“数据不足 / 观测覆盖 0.1%”，主值区域不得出现 `0%`。只有 coverage 至少 90% 且 API 明确返回 utilization `0`，才可无警告显示真实 `0%`；50%–90% 时可显示“0%（部分观测）”。

其它零值/未知规则：

| 字段 | `0` 的含义 | `null` 的含义 | UI |
|---|---|---|---|
| `completed_executions` | 窗口内确实没有终止 attempts | 不会返回 null | `0` |
| `failed_executions` | 确实无失败 attempts | 不会返回 null | `0` |
| `failure_rate` | 有完成样本，失败数为零 | 没有完成分母 | `0%` / `—（无已结束执行）` |
| percentile p50/p95 | 有效差值真实为 0ms | 没有有效样本 | `0 ms` / `—（0 个有效样本）` |
| timestamps | 不适用 | 事实缺失/尚未发生 | `尚未开始`、`尚未结束`，不得显示 `0` |

### 5.2 TaskExecution 用户状态映射

主状态必须由真实字段按下列优先级确定；前端不得直接打印 raw enum：

| 条件（从上到下） | 中文 | English | 色彩/语义 | 计入 failure rate |
|---|---|---|---|---|
| `outcome=succeeded` | 已完成 | Completed | success | 否 |
| `outcome=failed` | 执行失败 | Failed | danger | 是 |
| `outcome=crashed` | 执行中断 | Interrupted | danger | 是 |
| `outcome=quiet_finalized` | 恢复时结束 | Ended during recovery | danger + recovery hint | 是 |
| `finished_at != null` 且 outcome 未识别/null | 结果未知 | Outcome unavailable | warning | 不在冻结公式的 terminal set 内 |
| `started_at != null` 且 `finished_at=null` | 执行中 | Running | info | 否 |
| `command_status` 表示 rejected/failed/expired 且未 start | 未开始 | Did not start | danger/warning，原因见 message | 否 |
| `queued_at != null` 且未 start | 等待开始 | Waiting to start | neutral | 否 |
| 其它 | 状态未知 | Status unavailable | warning | 否 |

`recovered=true` 是附加 badge“由系统恢复 / Recovered by system”，不能覆盖主 outcome。未知未来枚举统一落入“结果未知”，raw value 只允许出现在折叠的“技术详情/复制诊断”中，不得成为状态文案。

### 5.3 quality 与异常数据

| API `quality` | 用户文案 | 展示位置 | 统计影响 |
|---|---|---|---|
| `valid` | 不显示 badge | 无 | 按冻结公式参与 |
| `invalid_time_order` | `时间数据异常` | list 行尾；detail 展开说明 | 该记录仍可见，但无效区间不参与耗时分位数 |
| 未知值 | `数据需检查` | list/detail warning | 不猜测影响；技术详情保留 raw |

Overview 的 `diagnostics.invalid_facts > 0` 时显示：“有 n 条记录的时间数据异常，已从相关耗时统计中排除。”`late_events > 0` 时在数据说明中显示：“本窗口处理了 n 条延迟到达的观测；指标按业务发生时间归窗。”不得把 diagnostics 当成失败数。

### 5.4 failure reason/message

主文案优先使用真实 `failure_message`。只有 message 缺失时，才使用下面的稳定 reason 映射：

| `failure_reason` | 缺省用户文案 |
|---|---|
| `nonzero_exit` | 执行进程返回错误。 |
| `output_failure` / `status_failed` | 执行器报告失败。 |
| `process_gone` | 执行进程意外退出。 |
| `clean_exit_no_output` / `done_no_output` | 执行结束，但没有生成有效结果。 |
| `stalled` | 执行长时间没有进展，已停止。 |
| `non_delivery` | 执行结果未成功交付。 |
| `evidence_persistence` | 执行证据未能保存。 |
| `repo_source_unavailable` | 仓库源不可用，执行未能开始或完成。 |
| `no_backfill_guard` | 恢复时无法安全确认此前执行结果。 |
| 未知/任意自由文本 | `执行未成功。`；原始值仅在技术详情中展示 |

不得对 raw reason 做简单下划线替空格后当产品文案；那仍会暴露内部分类，并可能泄露不适合主界面的自由文本。

## 6. 时间、分位数、样本数与排名口径

### 6.1 时间窗与时间点

- 固定标题：`过去 24 小时（滚动）` / `Past 24 hours (rolling)`。
- 同行显示 API `window.start` 到 `window.end`，转换为浏览器本地时区，格式为 `M月D日 HH:mm – M月D日 HH:mm`；跨年时补年份。
- 紧邻显示 `本地时间 · UTC+08:00`（按浏览器实际 offset）；hover/title 与复制值保留完整 RFC3339 UTC。
- `as_of` 是本次查询窗口终点；`refreshed_at` 是投影最后成功更新时间。UI 不得混称二者为“数据时间”。
- freshness 文案显示 `数据最新`、`数据延迟`、`正在重建`、`暂不可用`；技术 tooltip 才显示 age/threshold。

### 6.2 duration 格式（毫秒仍为 API 权威单位）

| 范围 | 显示 | 示例 |
|---|---|---|
| `< 1s` | 整数 ms | `250 ms` |
| `1s .. < 60s` | 最多 1 位小数 s；整秒不补 `.0` | `1.2 秒`、`12 秒` |
| `1m .. < 60m` | `M分 SS秒`，秒为 0 时省略 | `8分 04秒`、`12分` |
| `1h .. < 24h` | `H小时 MM分` | `2小时 03分` |
| `≥24h` | `D天 H小时` | `1天 4小时` |

负值不格式化为时长，显示“时间数据异常”。`null` 按字段上下文显示 `—`/“尚未开始”，不得变成 `0 ms`。

### 6.3 分位数文案

- 卡片主标签使用 `典型值（P50）`；次标签使用 `慢端（P95）`。P50/P95 必须保留，避免把统计量伪装成平均值。
- tooltip：`P50：一半有效样本不超过此耗时。P95：95% 的有效样本不超过此耗时。使用连续分位数计算。`
- Queue samples 文案：`基于 n 次具有真实入队和开始时间的执行`。
- Duration samples 文案：`基于 n 次具有真实开始和结束时间的执行`。
- `samples=0` 时主值 `—`，辅助文案 `没有有效样本`；`samples=1` 时仍展示 API P50/P95，但明确 `仅 1 个样本`，不加趋势判断。

### 6.4 排名不是性能评分

两张表分别命名为“按智能体查看”“按项目查看”，副标题固定为“按已结束执行次数降序；同数时按 ID 排序；最多显示 20 项”。不得使用“最佳”“最快”“质量排名”。

每一行必须显示：

- `completed_executions`：`n 次已结束执行`；说明一次重试单独计数；
- failure：`failed/completed（rate）`，如 `2/10（20%）`；分母为零显示 `—（0 次已结束）`；
- queue P50/P95 + `n 个样本`；
- duration P50/P95 + `n 个样本`。

表的排序仍完全使用后端冻结顺序，不能在浏览器按 failure rate 或 p95 重排后仍称相同榜单。

## 7. 逐项文案（zh-CN / en）

| key/位置 | 中文 | English |
|---|---|---|
| 页面标题 | Insight 概览 | Insight overview |
| 窗口 | 过去 24 小时（滚动） | Past 24 hours (rolling) |
| all execution CTA | 查看全部执行记录 | View all executions |
| completed card | 已结束的执行 | Completed executions |
| completed hint | 每次执行尝试分别计数，包括重试 | Each execution attempt is counted, including retries |
| failed rate | 失败率 | Failure rate |
| failure formula | 失败执行 / 已结束执行 | Failed / completed executions |
| utilization | 容量利用率 | Slot utilization |
| coverage | 观测覆盖 | Observation coverage |
| low coverage | 数据不足，暂不展示利用率 | Insufficient data; utilization is hidden |
| partial coverage | 部分观测，可能不代表完整 24 小时 | Partial observation; may not represent the full 24 hours |
| queue | 排队等待 | Queue wait |
| duration | 执行耗时 | Execution duration |
| p50 | 典型值（P50） | Typical (P50) |
| p95 | 慢端（P95） | Slow tail (P95) |
| agent table | 按智能体查看 | By agent |
| project table | 按项目查看 | By project |
| list title | Task executions | Task executions |
| list explanation | 每行代表一次执行尝试 | Each row is one execution attempt |
| no executions | 过去 24 小时没有执行记录 | No executions in the past 24 hours |
| no matching | 当前筛选下没有执行记录 | No executions match these filters |
| stale title | 数据有延迟 | Data is delayed |
| stale body | 指标仍可查看，但可能缺少最近发生的执行。最后更新于 {time}。 | Metrics remain available but may omit recent executions. Last updated {time}. |
| rebuilding | Insight 正在重建 | Insight is rebuilding |
| unavailable | Insight 暂不可用 | Insight is unavailable |
| unauthorized | 你没有查看组织 Insight 的权限 | You do not have permission to view organization Insight |
| detail missing | 当前组织中找不到这条 TaskExecution | This TaskExecution was not found in the current organization |

所有新增页面文案进入 `insights` namespace，页面组件不得继续硬编码双语文本。

## 8. API → UI 字段映射与加法字段

### 8.1 Overview（API 不变）

| API 字段 | UI 消费 | 格式/规则 |
|---|---|---|
| `window.kind/duration` | 窗口标题 | 固定 rolling / 24h；不做选择器 |
| `window.start/end` | 精确范围 | 本地可读时间 + UTC tooltip |
| `as_of` | 请求窗口终点 | 技术说明，不冒充刷新时间 |
| `refreshed_at` | `更新于` | 本地时间 |
| `freshness.state` | badge/banner | 映射为用户文案 |
| `freshness.age_ms/threshold_ms` | freshness tooltip | HumanDuration；不在 badge 打印 `x/y` |
| `completed_executions` | count 卡/排名 | 整数；attempt 口径 |
| `failed_executions` | count 辅助/排名分子 | 整数 |
| `failure_rate` | rate 卡/排名 | null/zero 分开 |
| `slot_utilization` | utilization 卡 | 先经过 coverage 展示矩阵 |
| `slot_coverage_ratio` | coverage 状态与文案 | 1 位小数百分比；极小非零不得四舍五入成 `0%`，`<0.1%` 显示 `<0.1%` |
| `queue_wait_ms.*` | queue 卡/排名 | HumanDuration + samples |
| `execution_duration_ms.*` | duration 卡/排名 | HumanDuration + samples |
| `agents[]` | agent 表 | 后端顺序；exact `agent_ref` 下钻 |
| `projects[]` | project 表 | 后端顺序；exact `project_id` 下钻 |
| `diagnostics.*` | 数据说明 notice | 仅非零时突出 |

### 8.2 Execution（复用现有 + 必需加法）

| API 字段 | 状态 | UI |
|---|---|---|
| `execution_id/command_id` | 现有 | 主键与技术关联信息；`command:*` 不伪装真实 executor ID |
| `task_id/task_ref/task_title` | 现有 | Task 展示名/ID；当前 `task_ref` 实际等于 `task_id` |
| `agent_ref/agent_name` | 现有 | Agent 展示名/筛选键 |
| `project_id/project_name` | 现有 | Project 展示名/筛选键 |
| `worker_id` | 现有 | 技术关联信息 |
| `outcome` | 现有 | 按状态矩阵映射；不直出 |
| `failure_reason` | 现有 | 缺 message 时 fallback 映射；raw 只进技术详情 |
| `queued_at/started_at/finished_at` | 现有 | list 时间点/detail 时间线 |
| `queue_wait_ms/duration_ms` | 现有 | HumanDuration；不重新计算 |
| `recovered` | 现有 | 附加恢复 badge |
| `quality` | 现有 | 仅异常 badge/说明 |
| `command_status` | **加法必需** | 从 `queue_interval_fact.command_status` 返回，解释未 start 行 |
| `status_reason` | **加法必需** | 从 `queue_interval_fact.status_reason` 返回；优先配套 message |
| `status_message` | **加法必需** | 复用 `worker_control_events.status_detail` 并投影到 queue fact；人类可读的未启动解释 |
| `failure_message` | **加法必需** | 复用 `executor.stop/recovery` payload 已有 `detail`；人类可读失败解释 |

加法字段不得由 Web handler 拼接或前端推断。`internal/insight/service.go` 的 projector/schema/DTO 是其归属；DuckDB schema version 应按现有 rebuild 机制升级。若历史 retained payload 没有 `detail`，`failure_message=null`，UI 使用 reason fallback；不得生成假的 message。

当前 503 handler 使用通用 `writeError`，未保证冻结契约所述 freshness envelope。实现阶段必须先补 handler contract test：`rebuilding/unavailable` 响应带 `window/refreshed_at/freshness`，否则页面只能显示通用错误，不能伪造刷新状态。

## 9. 页面状态矩阵

| 场景 | Overview | List | Detail | 禁止行为 |
|---|---|---|---|---|
| loading | 骨架/“正在加载” | 表骨架 | detail 骨架 | 先渲染零值 |
| fresh + representative coverage | 正常指标 | 正常表 | 正常 detail | — |
| fresh + insufficient coverage | 隐藏利用率，coverage warning | 不受影响 | 不受影响 | 把 unknown 显示为 0% |
| stale | 保留指标 + 顶部 warning | 保留列表 + warning | 保留 detail + warning | 只显示小 badge、无解释 |
| empty overview | count=0，其余按 null 语义；排名空态 | — | — | 将所有卡显示 0 |
| empty filtered list | — | 显示筛选 chips + matching empty | — | 显示“系统无数据” |
| rebuilding | 不渲染旧指标；重建状态 + 重试 | 同 | 同 | silent stale zeros |
| unavailable | 不渲染指标；错误状态 + 重试 | 同 | 同 | 从错误 message 猜 freshness |
| 401/403 | 权限说明 | 权限说明 | 权限说明 | 显示数据外壳造成泄漏 |
| 404 detail | — | — | 当前 org 找不到 + 返回 list | 称“系统中不存在” |
| invalid time order | diagnostics notice | 行异常 badge | 解释排除规则 | clamp 到 0 |
| unknown enum | 不影响聚合文案 | `状态未知/数据需检查` | 用户态 + 折叠 raw | 直接打印 raw enum |

## 10. 验收标准

### A. 信息架构与上下文

- [ ] 侧栏含 Overview 与 Task executions；list 使用独立 route，不在 Overview 内展开。
- [ ] Overview header、Agent 行、Project 行分别下钻到 list，并在 URL 保留 exact `window/agent_ref/project_id`。
- [ ] list 刷新、复制 URL、下一页和浏览器返回后，筛选/游标语义不丢失。
- [ ] list 行打开 detail；detail 可回到原始带筛选 list，也可回 Overview。

### B. coverage、未知与零

- [ ] fixture `slot_coverage_ratio=0.001, slot_utilization=0` 时，主值为“数据不足”，页面主视图不存在代表利用率的 `0%`。
- [ ] coverage null、0、49.9%、50%、89.9%、90% 均有边界测试。
- [ ] `failure_rate=0` 显示 `0%`；`failure_rate=null` 显示 `—` 和无分母说明。
- [ ] p50 `0` 显示 `0 ms`；p50 null + samples 0 显示无有效样本。
- [ ] coverage 极小非零显示 `<0.1%` 或实际一位小数，不能被格式化成 `0%`。

### C. 枚举与解释

- [ ] succeeded/failed/crashed/quiet_finalized、queued/running/did-not-start、unknown outcome 均按矩阵显示。
- [ ] `valid` 不显示；`invalid_time_order` 显示用户文案与统计排除说明；未知 quality 不直出。
- [ ] recovered 作为附加 badge，不覆盖 outcome。
- [ ] 页面主视图不出现 `quiet_finalized`、`invalid_time_order`、snake_case failure reason。
- [ ] API 从真实 payload 返回 `failure_message`，从真实 queue fact 返回 command status/reason/message；缺历史 message 时使用明示 fallback。

### D. 时间、统计与排名

- [ ] 窗口同时显示“滚动 24 小时”、精确本地起止和时区；start/end/refreshed_at 角色不混淆。
- [ ] 250ms、1.2s、8m04s、2h03m、1d4h 的格式逐一测试；null/负值分别测试。
- [ ] 每个 percentile 展示 P50/P95 标签和自己的 samples；无样本不显示零耗时。
- [ ] 排名标题明确按 completed attempts 排序、最多 20 项；每行 failure 显示分子/分母，queue/duration 显示样本数。
- [ ] UI 不重新计算 rate、percentile、duration 或排名；只做百分比/时长/时间点格式化与 coverage 展示分类。

### E. 数据状态与真实链验收

- [ ] loading、empty、filtered empty、stale、rebuilding、unavailable、401/403、detail 404 均有独立组件测试。
- [ ] API contract test 覆盖 503 freshness envelope 与新增加法字段的 null/backfill 行为。
- [ ] 使用真实 SQLite services/events -> projector -> HTTP -> 页面链路，至少验收一组 terminal execution、一组 queued command、一组 invalid timestamp 和一组低 coverage heartbeat。
- [ ] Owner 在真实组织页面复验：能用自己的话回答过去 24 小时完成/失败、典型/长尾耗时，以及容量为何可判断或不可判断。
- [ ] 单一候选 SHA 通过前端全量测试、Go 相关包/全仓测试、build；最终从远端回读候选 ref，并在合并后回读 `origin/main` 可达性。

## 11. 非目标与推迟项

以下是本阶段推迟，不是 Insight 永久不支持：

- 自定义时间范围、日历“今天”、窗口对比和趋势图；
- Fleet 实时 slot 视图、告警、自动容量建议；
- outcome/quality/date/CLI/model 的服务端复合筛选与全文搜索；
- cost/token 指标；既有 `internal/usage` analytics 仍是独立产品面；
- 改变 failure-rate、percentile、coverage/utilization 公式或排行榜排序；
- 对 retained history 之外的数据补造 `failure_message`；
- 将 execution attempt 去重成 Task，或把 Task 状态当 TaskExecution outcome；
- 因本轮 UI 需要而新增第二套分析事实、浏览器端聚合或直读 SQLite。

## 12. 可复用实现、改动位置与最小实施拆分

### 12.1 直接复用

| 现有实现 | 用法 |
|---|---|
| `internal/insight` 冻结公式、事实表与 projector | 原样作为指标权威；仅加法投影 message/status |
| 三个现有 Insight HTTP endpoint | Overview/list/detail 继续使用；list 无需新 endpoint |
| `web/src/api/insights.ts` hooks/query keys | 扩充字段；list page 直接复用 query hook |
| `formatLocalTime` 及现有 design tokens | 时间解析和颜色基础；新增一致 formatter |
| `StatePanel`、`FreshnessBadge` 的状态分支 | 拆成共享 Insight components 并 i18n 化 |
| 现有 org auth `org.analytics.read` | 路由保持同一权限，不新增授权口径 |

### 12.2 需要修改

| 位置 | 最小改动 |
|---|---|
| `internal/insight/types.go` | Execution DTO 加 `command_status/status_reason/status_message/failure_message` |
| `internal/insight/service.go` | 保留 activity `detail` 与 queue `status_detail`；execution query join queue status；schema version/rebuild |
| `internal/webconsole/api/handlers_insights.go` | 保证 503 freshness envelope；保持 org auth/window 验证 |
| `internal/insight/service_test.go`、`handlers_insights_test.go` | 新字段、历史 null、503 envelope、真实枚举/低 coverage contract |
| `web/src/App.tsx`、`AppLayout.tsx` | 新增 list route 与侧栏入口 |
| `web/src/api/insights.ts` | DTO 加法字段；URL/cursor filter helper |
| `web/src/pages/InsightOverview.tsx` | 只保留 Overview；去掉 inline drilldown |
| 新 `web/src/pages/InsightExecutions.tsx` | 独立 list 与 URL filter/cursor |
| `web/src/pages/InsightExecutionDetail.tsx` | 独立 detail 时间线/定义列表 |
| 新 `web/src/components/insight/*`、`web/src/utils/insightFormat.ts` | coverage classifier、duration/ratio/time、status mapping、共享组件 |
| `web/src/i18n/locales/{en,zh}/insights.json` | 本文逐项文案 |
| 页面/API tests | 状态矩阵与边界 fixture |

### 12.3 可独立执行的最小节点

每个节点都是可认领的执行契约；不得把节点口头信息当依赖。

#### I1 — Backend contract 收敛与加法式解释字段

- 背景：生产事件已有 `detail`、queue fact 已有 status，但 execution API 丢失；503 未稳定携带 freshness envelope；2.3 还识别出排名、coverage 分母和 detail 窗口 drift。
- 目标：回到冻结公式与窗口契约，并为 list/detail 提供真实的人类解释和未启动状态。
- 技术边界：仅 `internal/insight` 与 Insight Web handlers/tests；不新增事实源，不从 UI 反推字段。
- 依赖：本文与冻结 Phase 1 contract。
- 交付：四个 nullable 加法字段、DuckDB schema version/rebuild、terminal-set 排名、admission-cap interval coverage、detail 24h gate、503 envelope contract tests。
- 独立验收：retained event 有/无 `detail`、queue 各状态、cap 变化/未观测 slot、未知 outcome、窗内/窗外 detail、跨 org、503 rebuilding/unavailable 均通过 Go tests；除纠正已列明 drift 外，其余 summary fixture 数值不变。

#### I2 — 展示语义组件与 i18n

- 背景：coverage、null/zero、枚举、时间与分位数目前分散硬编码。
- 目标：建立纯展示层单一映射源。
- 技术边界：`components/insight`、formatter 与 locale；不发 API、不计算业务指标。
- 依赖：本文；可与 I1 并行，但 DTO 相关状态使用 fixture 接口。
- 交付：coverage classifier、human duration/time、outcome/quality/reason mapping、P50/P95 和 sample 文案。
- 独立验收：本文 B/C/D 的所有边界形成 table-driven Vitest；coverage `0.001 + utilization 0` 不渲染利用率 `0%`。

#### I3 — Overview 重构

- 背景：Overview 内联 list，排名缺少样本/口径。
- 目标：把 Overview 变成组织级解释页。
- 技术边界：只消费现有 Overview API；不改排序/公式。
- 依赖：I2。
- 交付：线框 4.1 的页面、诊断 notice、带样本排名、稳定下钻 URL。
- 独立验收：Overview 组件覆盖 A/B/D/E；无 inline execution table；Owner 五类反馈中 coverage、时间、排名三类可单页复验。

#### I4 — TaskExecution list/detail 分层

- 背景：list 无 route，detail 是一行表，筛选上下文无法分享。
- 目标：建立独立 list、可解释 detail 和完整返回路径。
- 技术边界：使用现有 list/detail endpoints 与 I1 加法字段；只支持已有 exact agent/project filters。
- 依赖：I1、I2。
- 交付：routes/sidebar、线框 4.2/4.3、URL cursor/filter、状态矩阵。
- 独立验收：A/C/E；刷新/复制/back；pending、running、terminal、invalid、unknown fixtures 不显示 raw enum。

#### I5 — 单一候选集成与真实链验收

- 背景：组件绿不等于生产链或 Owner 页面可理解。
- 目标：在一个候选 SHA 上证明事实源到 UI 的闭环。
- 技术边界：不在验收时修改公式；发现偏差退回对应节点。
- 依赖：I1–I4 的确定 SHA。
- 交付：集成候选、命令结果、真实链 fixture/deployed smoke、Owner 复验记录。
- 独立验收：第 10 节全部勾选；远端 ref 回读等于候选 SHA。

#### I6 — 合并与 `origin/main` 可达性终验

- 背景：独立分支交付不是完成态。
- 目标：仅将 I5 验收通过的单一候选合入 main，并验证远端权威状态。
- 技术边界：不得用另一 SHA 替换候选；不得只依据 push 日志宣称完成。
- 依赖：I5 Owner 真实页面复验通过与合并权限。
- 交付：main merge SHA、`git ls-remote`/fetch 后的 `origin/main`、候选为其祖先的证明。
- 独立验收：`git merge-base --is-ancestor <candidate> origin/main` 退出 0，且生产部署/页面指向包含该 SHA 的构建。完成 I6 前，issue 只能保持“整改设计/实现已交付，待 main 终验”，不可关闭。

## 13. 规约自检

- 统一语言：明确区分 Task 与 TaskExecution；列表一行是一条 execution attempt。
- AppService/事实归属：未新增 UI 直读或第二事实源；DuckDB 仍为 disposable analytical read model。
- 可观测性：复用现有 activity/queue/heartbeat 事实；异常通过 diagnostics、freshness、quality 显示。
- 文档先行：本轮没有改变架构边界或冻结公式，因此不新增 ADR；加法字段与 UX 决策在本功能设计中冻结后再实现。
- 安全：继续以 org membership + `org.analytics.read` 隔离；技术详情不得把可能敏感的自由文本 reason 自动放到主界面。
- 范围：第 11 节均为阶段性推迟，不误写为系统永久不支持。
- 生产闭环：最终节点明确要求单一候选、真实链、Owner 页面复验与 `origin/main` 权威回读。
