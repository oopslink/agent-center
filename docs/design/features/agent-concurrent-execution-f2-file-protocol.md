# F2 实现说明 — 监工↔Executor 文件交换协议与工作区隔离

> 父设计：[agent-concurrent-execution.md](agent-concurrent-execution.md)（§6.D / §7 / §9 / §12）。
> 本文档记录 F2（cycle v2.17.0 plan-9c606650 / 任务 T510）的**实现层**落点，对应包
> `internal/workerdaemon/executor`。F1（进程模型/拓扑）、F3（模型路由）、F5（完成信号/
> watchdog/崩溃恢复的监工侧编排）消费本包，不在本范围内实现。

## 1. 范围

只做**worker 宿主机上的文件协议**：协议值对象、其磁盘生命周期、containment 围栏、
git worktree 备/拆。**不**连任何中心 DB / AppService —— 这里没有领域状态，只有两个进程
约定的 durable 文件契约。定位等同 `internal/workerdaemon/taskexec`（per-execution 目录
承载本机状态，ADR-0018）与 `sessioninstance`。

## 2. 目录布局（design §7 wire contract）

```
<agent_root>/executors/<executor_id>/
  input.json      # 监工写、executor 读：goal + 聚合 context + 分配 model + 来源引用
  workspace/      # executor 的 git worktree（隔离；executor 只在此动文件）
  progress.jsonl  # executor append 流式进度（监工 tail 转播 chat）
  output.json     # executor 写：最终结果 / 错误详情
  status          # 完成信号 + 详情（JSON：state/model/started_at/last_progress_at/error/summary）
```

`<agent_root>` = 既有 per-agent home（`<AgentHomeBase>/agents/<agent_id>`，见
`agent_controller.go:agentPaths`）。`executors/` 与既有 `tasks/` `plans/` `memory/` 平级。

**命名说明（规约 §12）**：隔离目录的*概念*是 Worktree；磁盘叶子名为 `workspace/`，因为
该字符串是 design §7 固定的 wire contract，监工与（独立 spawn 的）executor 必须逐字一致。
代码/注释一律以 worktree 称呼该概念。

## 3. 协议值对象（`protocol.go`）

| 类型 | 写方 | 读方 | 关键不变量（`Validate()`）|
|---|---|---|---|
| `Input` | 监工 | executor | executor_id 合法段；goal.title / model / created_at 必填（§5 监工 spawn 前必已定 model）|
| `Output` | executor | 监工 | success/error 对称：失败必带 `ErrorDetail`，成功必不带 |
| `Status` | executor | 监工 | `State` ∈ {running,done,failed}，未知值显式报错（§17）；failed 必带 error，其它态必不带 |
| `ProgressEntry` | executor | 监工 | at + message 必填 |
| `ErrorDetail` | — | — | kind（机器可读 reason）+ message（人类可读）双字段（§16）|

`State` 是 running/terminal 的权威；`LastProgressAt` 喂监工 watchdog（F5）。

## 4. I/O 操作（`exchange.go`）

`FileExchange{layout, clk}`，注入 clock 保证测试确定性（§14.x）。

- **监工侧**：`Provision`（建 executor 目录，**不**建 workspace——留给 worktree add）、
  `WriteInput`、`ReadOutput`、`ReadStatus`、`ReadProgress`、`Scan`（崩溃恢复扫目录，design §12）、
  `Remove`（containment-guarded 清理）。
- **executor 侧**：`ReadInput`、`AppendProgress`（O_APPEND 追加，崩溃留可读前缀）、
  `WriteStatus`、`WriteOutput`、`ContainedPath`（每次文件访问的围栏 seam）。

读写规则：

- 写：`temp + rename + O_NOFOLLOW` 原子写（沿用 taskexec/sessioninstance），崩溃不留 torn 文件。
- 读缺失：`ReadOutput/ReadStatus/ReadInput` 返回 `os.ErrNotExist`（`errors.Is` 可判），让监工区分
  「还没写」与「损坏」；`ReadProgress` 缺失返回空切片。
- 读损坏 / 非法记录：显式报错，绝不静默零值或跳过（§17）；progress 坏行带 1-based 行号。
- `Scan` 对每个目录 best-effort：input 不可读的目录仍以 `Input=nil` 上报，让监工决定清理，
  而非凭空消失（§17 未知/部分态须 surface）。

## 5. Containment 围栏（`containment.go`，design §6.D）

`ContainedPath` 把 user path 解析进 executor 的 workspace 并保证不逃逸，**逐字复用**
daemon `file_transfer.go` 的策略：`filepath.Abs` → `EvalSymlinks` → 分隔符感知的
`pathWithinRoot`（`/rootEvil` 不算在 `/root` 内）。防 `..` 穿越 / workspace 外绝对路径 /
符号链接逃逸（`mustExist=true` 解整条路径、`false` 解父目录）。逃逸返回 sentinel
`ErrPathEscapesWorkspace`。`resolveWithin` 是目录级变体，供 `Remove` 在 RemoveAll 前再确认
目标落在 `executors/` 内（defense-in-depth）。

## 6. Worktree 备/拆（`worktree.go`，design §6.D）

`WorktreeProvisioner{repoDir, runner}`，git 经 `GitRunner` port（镜像 `mergecheck` /
`cognition/memory`，测试可换 fake）。`Add`（已有 ref）/`AddNewBranch`（`-b` 新分支，常用：
每 executor 一条独立分支）/`Remove`（`worktree remove --force` + `worktree prune`）。git
环境中性化（`GIT_TERMINAL_PROMPT=0` / `GIT_CONFIG_GLOBAL=/dev/null` / …）。每 executor 独立
worktree 路径 ⇒ 两并发 executor 各自 checkout，互不踩（验收点）。

## 7. 规约自检（conventions §15）

- §0.4 AppService：本包是 worker 本机文件状态，**无**中心 DB / 跨进程 AppService 绕过（同
  taskexec/sessioninstance），不触发该规则。
- §2 可观测性：F2 是 executor 本机文件 I/O，executor 不连中心、不发 domain event；其可观测面
  就是 `status`（state/last_progress_at/error）+ `progress.jsonl`。**监工侧**读 output 后回写
  中心 task / emit domain event 属 F1/F5，本包不发事件。
- §9 持久化：纯文件、无 SQL、无 schema migration、无 FK。
- §12 命名：见 §2 命名说明（workspace 是 wire 名，概念称 worktree）。
- §16 reason+message：`ErrorDetail{Kind,Message}` 双字段。
- §17 错误不吞：每个 err 分支 return；未知 state / 坏 JSON / 坏 progress 行显式报错。
- §4 零 LLM SDK / §10 单二进制：无新依赖、无新 binary。

未触发 / 不适用：§1 单一来源（不造 task）、§8 BlobStore（协议文件均为小 JSON，非大内容）、
§9.4 schema 变更（无）、§11 渠道（无新交互渠道）。

## 8. 测试计划 / 报告

### 范围
`internal/workerdaemon/executor` 全部公开 API + containment / worktree。单模块单测（Dev scope，§19）。
真 git 的并发隔离用例属本模块行为验证，git 是确定性本机二进制，缺失则 `t.Skip`。

### 用例清单与结果（计划项 1:1）

| # | 用例 | 文件 | 结果 |
|---|---|---|---|
| 1 | State / ErrorDetail / Input / Output / Status / ProgressEntry 的 Validate（正路 + 各非法分支）| protocol_test.go | PASS |
| 2 | Layout 路径解析 + 非法 id 拒绝 | exchange_test.go | PASS |
| 3 | 全链路 round-trip：Provision→WriteInput→ReadInput→WriteStatus(running)→AppendProgress×2→WriteOutput→WriteStatus(done)→Read{Output,Status,Progress}（design §7 验收）| exchange_test.go:TestFullChain | PASS |
| 4 | 写默认时间戳（StartedAt/LastProgressAt/FinishedAt/progress.at 来自注入 clock）| exchange_test.go | PASS |
| 5 | 非法写被拒（input/status/output/progress）| exchange_test.go | PASS |
| 6 | 读缺失 → ErrNotExist；progress 缺失 → 空 | exchange_test.go | PASS |
| 7 | 读损坏 JSON / 非法 progress 行 → 报错（非 NotExist）| exchange_test.go | PASS |
| 8 | 读取通过 JSON 解析但违反不变量 → Validate 报错（status + output）| exchange/coverage_test.go | PASS |
| 9 | Scan：多 executor 各态、忽略非目录/dotfile/junk、缺失目录→nil、部分态以 nil Input 上报（§12 崩溃恢复）| exchange_test.go | PASS |
| 10 | Remove：删除 + 幂等 + 拒绝非法 id | exchange_test.go | PASS |
| 11 | ContainedPath：内部解析 / 新叶子 / `..` 逃逸→sentinel / 绝对外路径 / 空路径 / 符号链接逃逸→sentinel | exchange_test.go | PASS |
| 12 | resolveWithin / resolveContained / pathWithinRoot / validateExecutorID 边界 | exchange/coverage_test.go | PASS |
| 13 | writeJSONAtomic / 各写操作的 mkdir / marshal 错误分支 | coverage_test.go | PASS |
| 14 | Worktree Add/AddNewBranch/Remove 的 git 参数、校验错误、runner 错误传播（fake runner）| worktree_test.go | PASS |
| 15 | **验收**：两并发 executor 各自 worktree，互不见对方文件、共享 README 各在、各自 output 独立读回、Scan 见两者、删一个不扰另一个（真 git）| worktree_test.go:TestTwoConcurrentExecutorsIsolated | PASS |

### 覆盖率
- 单元行覆盖率：**91.7%**（全新包，diff coverage = 91.7%，≥90% 达标）。
- 未覆盖余项：深层 IO 失败分支（open/write/close/rename 失败、worktree prune 失败）—— 防御性、
  无法确定性触发，已最大化覆盖 mkdir/marshal/校验/逃逸等可达分支。

### 出口标准核对
- [x] 行覆盖率 ≥ 90%（91.7%）
- [x] 全链路 round-trip 通（用例 3）
- [x] 两并发 executor worktree 隔离（用例 15，真 git）
- [x] 异常路径（缺失/损坏/非法/逃逸/git 失败）显式覆盖
- [x] `go build ./...` / `go vet` / `gofmt` 干净

### 结论
F2 验收标准达成：监工写 input → executor 读 → 跑 → 写 output/status → 监工读，全链路通；
两并发 executor 各自 worktree 互不踩。
