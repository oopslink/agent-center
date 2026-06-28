# F3 实现说明 — 模型路由与 profile 配置

> 父设计：[agent-concurrent-execution.md](agent-concurrent-execution.md)（§5 模型选择优先级链 / §10 配置项）。
> 本文档记录 F3（cycle v2.17.0 plan-9c606650 / 任务 T515）的**实现层**落点。
> F3 交付两件事：**(1)** 路由决策单元 `internal/workerdaemon/modelrouter`（监工据此为每个
> executor 选模型）；**(2)** 它读取的持久化配置字段（agent profile + task）。监工主循环
> （真正调用本路由 + spawn executor）属 F1/F5，不在本范围内实现——本包是被它消费的纯单元。

## 1. 范围

- **路由决策**：实现 design §5 定死的优先级链，产出「这个 executor 用哪个模型」+ provenance。
- **配置字段**：在 agent profile 与 task 上新增 §10 字段，使路由有真实配置可读。
- **不做**：监工主循环 / spawn 编排 / executor 进程消费（F1/F5）；难度判断的真实 LLM 实现
  （本包只定义 `DifficultyJudge` port，监工把它接到自己的推理模型）。

## 2. 优先级链（design §5，权威）

```
task.model 设了   → 直接用（硬覆盖，最高优先级；不问 LLM）
        没设       → 监工 LLM 读 goal 判难度 → 从 profile.allowed_models 选一个
        判不出     → profile.default_executor_model 兜底
```

- 监工自身模型 = `profile.orchestrator_model`（便宜快档，用于路由/判难度/汇总）。
- 难度**用 LLM 推理判**，不写死启发式（§5）——故本包**无任何模型分档规则**，只依赖
  `DifficultyJudge` port；分档由监工的推理模型在 port 内完成。

## 3. 路由单元（`modelrouter` 包）

`Router{judge DifficultyJudge}`，无状态、可在并发 spawn 间共享。

| 类型 | 作用 |
|---|---|
| `Config` | profile 的路由字段（`OrchestratorModel` / `AllowedModels[]` / `DefaultExecutorModel`），以**基本类型**传入，使本包不 import agent BC（无环、解耦）|
| `DifficultyJudge` (port) | LLM 判难度：`Judge(ctx, JudgeRequest{Goal, AllowedModels}) (Judgment{Model}, error)`；返回 `ErrInconclusive`/任意 error = 判不出 → 兜底 |
| `Decision` | `Model` + `Source`(`task_override`/`llm_judged`/`default_fallback`) + `JudgeError`（兜底时记录判难度为何失败，**不吞**，供 observability）|

核心方法：

- `ResolveExecutorModel(ctx, taskModel, goal, cfg) (Decision, error)` —— 跑优先级链：
  1. `taskModel` 非空 → `SourceTaskOverride`，**短路不调 LLM**。
  2. 有 judge 且 `AllowedModels` 非空 → 调 judge；判出且**命中** allowed → `SourceJudged`。
  3. 否则 `DefaultExecutorModel` 非空 → `SourceDefault`（`JudgeError` 带判难度失败原因）。
  4. 都没有 → `ErrNoExecutorModel`（`errors.Join` 兜底原因，仍 errors.Is 可判）。
- `OrchestratorModel(cfg) (string, error)` —— 取监工自身模型，未配 → `ErrNoOrchestratorModel`。

**护栏**：judge 选了不在 `AllowedModels` 内的模型（LLM 幻觉）→ 记 `ErrJudgeOutOfRange`、
拒用、回兜底，绝不 spawn 未授权模型。

**监工接线（F5 一行适配，不在本包）**：监工读 `agent.Profile` → 填 `modelrouter.Config`
（`OrchestratorModel/AllowedModels/DefaultExecutorModel`）→ `ResolveExecutorModel` 得 `Decision.Model`
→ 写入 `executor.Input.Model` → `pool.Launch`。`Input.Validate()` 已要求 `Model` 非空，与本链
「spawn 前必有模型」闭合。

## 4. 配置字段（design §10）

**agent profile（`internal/agent/agent.go` `Profile`，沿用 T236 Reasoning/Mode/Provider 同款流转：
domain → sqlite 列 → migration → service/appservice → webconsole api → controller/projector）：**

| 字段 | 类型 | 说明 | 默认 |
|---|---|---|---|
| `orchestrator_model` | string（nullable TEXT 列）| 监工自身模型 | 空=center 默认 |
| `default_executor_model` | string（nullable TEXT 列）| 判不出兜底 | 空=center 默认 |
| `max_concurrent_tasks` | int（INTEGER 列 DEFAULT 3）| 名下最大并发 executor 数；`EffectiveMaxConcurrentTasks()` 把 ≤0 归一为 3 | **3** |
| `allowed_models` | []string（JSON TEXT 列，同 EnvVars/skills/tags）| executor 可选清单 | 空 |

**task（`internal/projectmanager/task.go`）：**

| 字段 | 类型 | 说明 |
|---|---|---|
| `model` | string（nullable TEXT 列）| 设了 = 硬覆盖，最高优先级；空=不覆盖 |

迁移：`internal/persistence/migrator.go` 追加 `ALTER TABLE` 增列（agents ×4、tasks ×1）。

## 5. 规约自检（conventions §15）

- §4 零 LLM SDK：判难度经 `DifficultyJudge` port，本包**无** LLM SDK / 无新依赖。
- §5 单点写：本包纯决策（无中心状态写入）；字段读写沿用既有 agent/task AppService。
- §12 命名：orchestrator / executor 一致；`Source` 取值即 §5 三档。
- §16 reason+message：`Decision.JudgeError` 携判难度失败原因；sentinel 错误齐备。
- §17 错误不吞：judge 失败/判不出/越界**不静默**——回兜底但把原因挂上 `Decision.JudgeError`；
  彻底无解返 `ErrNoExecutorModel`（join 原因）。

## 6. 测试计划 / 报告

### modelrouter（`router_test.go`，覆盖率 95.5%）

| # | 用例 | 结果 |
|---|---|---|
| 1 | task.model 硬覆盖（trim）+ judge **零调用**（短路）| PASS |
| 2 | 无 task.model → judge 选 allowed → `llm_judged`；断言 judge 收到 goal+allowed | PASS |
| 3 | judge 返回 `ErrInconclusive` → 兜底 + `JudgeError` 可判 | PASS |
| 4 | judge 返回任意 error → 兜底 + `JudgeError` 可判 | PASS |
| 5 | judge 选越界模型 → 拒用、兜底、`ErrJudgeOutOfRange` | PASS |
| 6 | 无 allowed_models → 跳过 judge（零调用）→ 兜底 | PASS |
| 7 | judge 为 nil → 兜底 | PASS |
| 8 | 全无解（无覆盖/判不出/无兜底）→ `ErrNoExecutorModel` 且 join 判难度原因 | PASS |
| 9 | task.model 在空 Config 下仍生效 | PASS |
| 10 | `OrchestratorModel`：取值 / 未配 → `ErrNoOrchestratorModel` | PASS |

### 配置字段
- agent profile 四字段 sqlite round-trip（含 `max_concurrent_tasks` 默认 3、`allowed_models` 空/非空）。
- task.model round-trip。
- `EffectiveMaxConcurrentTasks()` 归一。
（详见各包 `_test.go`；见 §7 出口核对。）

## 7. 出口标准核对

- [x] 优先级链四条路径各命中正确模型（用例 1/2/3+4/6+7 → override / judged / 兜底）
- [x] task.model 优先级压过 LLM 路由（用例 1：judge 零调用）
- [x] 判不出走兜底（用例 3/4/5/6/7）
- [x] §10 字段落库（profile ×4 + task ×1，含默认与 JSON 列 round-trip）
- [x] `go build ./...` / `go vet` / `gofmt`（go1.25.11）干净
- [x] 异常路径（判不出/越界/全无解）显式覆盖、错误不吞

## 8. 结论

F3 验收达成：优先级链四路径正确，task.model 硬覆盖压过 LLM 路由，判不出走兜底；§10 配置
字段在 agent profile 与 task 上落库可配。监工主循环消费本路由属 F1/F5，已在 §3 给出接线点。
