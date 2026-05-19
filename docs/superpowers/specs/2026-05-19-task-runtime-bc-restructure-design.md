# TaskRuntime BC 战术重组设计 — Spec

> **brainstorming 产物，工作期文档。实施完成后删除（见 § 7）。**
>
> 决策永久档案 → ADR-0019；设计正本 → `docs/design/architecture/tactical/task-runtime/*.md`。
> 本 spec 仅用于固化 brainstorming session 的讨论结论 + 驱动接下来的 implementation。

最后更新：2026-05-19。

---

## § 1. 背景

原任务（ddd-blueprint § 3.1 P3 第 1-2 项）：Scheduling BC 两份战术文档（`01-task-model.md` + `02-input-required.md`）补 § X.1-X.6 战术设计小节。

brainstorming 讨论中发现：

1. **现 01-task-model 的"主题切"骨架**（dispatch / kill / workspace / retry / timeout）实际是把 BC1 Scheduling + BC4 Execution 的内容混编而成
2. **BC1↔BC4 之间存在 6+ 处协议↔实现的人工分割**：
   - DispatchEnvelope schema + ACK/NACK 协议（BC1）vs worker 端 11 步处理 + env 注入 + shim spawn（BC4）
   - Kill 两阶段协议（BC1）vs SIGTERM/5s grace/SIGKILL 进程级机制（BC4）
   - Workspace mode VO（BC1）vs worktree 物理创建 + 24h GC（BC4）
   - Artifact 归属（事实上跟 Execution 同 BC，但被塞在 01-task-model § 10）
   - JSONL 解析 + milestone 判定（BC4）混在 01 § 3.7 进度上报
   - Reconcile 协议（BC1）vs worker 端 SIGTERM 本地僵尸（BC4）
3. **UL 重合度极高**：`Task` / `TaskExecution` / `dispatch` / `kill` / `workspace_mode` / `artifact` 全是 BC1 + BC4 共用词；BC1↔BC4 是同一组业务对象的"协议视角"vs"运行时视角"，不是两个不同领域

依据 DDD "**按 UL 划 BC**" 原则，物理 split（state 权威在 center / 执行在 worker）≠ 概念 split。BC1 + BC4 应合并。

---

## § 2. 决策清单（brainstorming 沉淀）

| ID | 决策 | 选项 |
|---|---|---|
| **D1** | TaskExecution 在 DDD 上的分类 | **独立 Aggregate Root**（非 Task 内部 Entity）—— 选项 β |
| **D2** | Artifact 节归属 | 跟 TaskExecution 同 BC（合并后自然归 TaskRuntime） |
| **D3** | 蓝图 P3 模板 | **升级**：从"加一节 X.1-X.6"升级为"按聚合骨架重组 + § X.1-X.6 wrap" |
| **D4** | BC 划分 | **合并 BC1 Scheduling + BC4 Execution → 新 BC TaskRuntime**；BC 数 8→7 |
| **D5** | TaskRuntime 物理文件组织 | **按聚合多文件**：00-overview + 01-task + 02-task-execution + 03-input-request |
| **D6** | Commit 拆分 | **两 commit**：strategic（含 ADR-0019）+ tactical（内容重组 + Workforce/01 carve） |
| **D7** | spec 落地位置 | `docs/superpowers/specs/` skill 默认；BC 合并决策另立 ADR-0019 永久档案；本 spec 实施后删 |

---

## § 3. Commit 1 改动详表（Strategic + ADR + 蓝图 + 目录搬家）

### 3.1 立 ADR-0019

**文件**：`docs/design/decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md`

**内容要点**：
- **Status**：Accepted, 2026-05-19
- **Context**：原 BC 划分 + 6+ 处协议↔实现人工分割 + UL 重合证据
- **Decision**：合并 BC1 + BC4 → TaskRuntime；BC 数 8→7
- **Rationale**：DDD 按 UL 划 BC；物理 split ≠ 概念 split；("协议" vs "实现") 不是合法 BC 切分线
- **Consequences**：
  - Task / TaskExecution / InputRequest 聚合 + 运行时实现细节（shim / JSONL / artifact / workspace 物理）在同 BC
  - Workforce BC 留纯 Worker / Project / Mapping / Proposal
  - 后续 7 BC 文档按蓝图升级后的 P3 模板组织
- **相关 ADR**：列已存在 0010 / 0011 / 0014 / 0015 / 0017 / 0018 措辞要审

### 3.2 `strategic/03-bounded-contexts.md`

- § 1 UL 表：BC1 列名 "Scheduling" → "TaskRuntime"；BC4 行的 UL 词合并入 BC1 行
- § 2 BC 列表：删 BC4 段，BC1 段重写为合并后版本（职责覆盖 Task / Execution / InputRequest / 运行时）
- § 3 Context Map：去除 BC1↔BC4 边
- § 3.1 上下游表：去 BC1↔BC4 行；BC1 名改 TaskRuntime；其它 BC 跟 BC1+BC4 的关系并入到跟 TaskRuntime 的单一关系

### 3.3 `strategic/01-subdomain-classification.md`

- 总 BC 数 8 → 7
- BC1 TaskRuntime 标 **Core**（BC1+BC4 都是 Core）
- 投入策略 / Essential 等字段同步更新

### 3.4 `ddd-blueprint.md`

#### § 1.1 战略表
- BC 数 "8 个 BC" → "7 个 BC"
- 命名表：删 Scheduling + Execution 两行；加 TaskRuntime 一行

#### § 1.2 战术表
- 各行的"位置/说明"列里凡是引用 `tactical/scheduling/...` / `tactical/workforce/... BC4` 的 path → 改 `tactical/task-runtime/...`

#### § 3.1 P3 模板升级（核心改动）

新 P3 模板措辞（草案）：

```
P3. 按聚合骨架重组每个 BC 战术文档 + 补 § X.1-X.6 wrap

每个 BC 在 tactical/{bc}/ 下组织 § X.1-§ X.6 战术设计内容：

文件组织：
- 00-overview.md：BC 入口；承载 § X.1 聚合清单 + § X.2 Invariants 索引 +
  § X.3 Domain Service + § X.4 Factory + § X.5 Repository + § X.6 跨聚合
  引用 + 跨 BC 交互 + Out-of-Scope + References
- 0N-{aggregate}.md：每聚合一份；内容组织为
  聚合状态机 → 字段 → lifecycle ops → Invariants

按 BC 体量规则：
- 多聚合 BC（≥2 聚合）：00-overview + 0N-{aggregate}.md 多文件
- 单聚合 BC：00-overview.md 内合并聚合详情（不另起 01-）
- 无业务聚合 BC（如 Bridge）：仅 00-overview.md，说明"无聚合 / 仅 ACL 翻译职责"

组织反例（明令禁止）：
- 不按主题切（dispatch / kill / workspace / retry / timeout）—— 这是 BC1+BC4 leaky 的根源
```

#### § 3.1 P3 表
- 表头加列"聚合数 / 文件数"
- 删 BC1（Scheduling）+ BC4（Workforce+Execution 现合并行的 Execution 部分）
- 加 BC1 TaskRuntime（聚合数 3 / 文件数 4）
- Workforce 行的覆盖范围说明改为"BC3 Workforce only（worker / project / mapping / proposal）"

#### § 2.1 ADR 表
- 加 ADR-0019 一行：「BC1 + BC4 合并为 TaskRuntime」

#### § 5 引用
- "Invariants 系统化样板" 从 `tactical/scheduling/01-task-model § 13` 改成 `tactical/task-runtime/02-task-execution § 13`（按 § 4.3 骨架，Execution 聚合的 Invariants 节固定 § 13）

### 3.5 ADR 措辞审

| ADR | 涉及术语 |
|---|---|
| 0010 | "Scheduling BC" / "Execution BC" → "TaskRuntime BC" |
| 0011 | "Scheduling BC" → "TaskRuntime BC" |
| 0014 | "Scheduling" / "Execution" → "TaskRuntime" |
| 0015 | "Execution BC" → "TaskRuntime BC" |
| 0017 | "Scheduling BC" → "TaskRuntime BC" |
| 0018 | "Execution BC" → "TaskRuntime BC" |

**不动决策内容**；只统一 BC 名字术语。

### 3.6 目录搬家（不动内容）

```bash
mkdir -p docs/design/architecture/tactical/task-runtime/
git mv docs/design/architecture/tactical/scheduling/01-task-model.md \
       docs/design/architecture/tactical/task-runtime/01-task-model.md
git mv docs/design/architecture/tactical/scheduling/02-input-required.md \
       docs/design/architecture/tactical/task-runtime/02-input-required.md
rmdir docs/design/architecture/tactical/scheduling/
```

文件**内容此 commit 不改** —— 留 Commit 2。

---

## § 4. Commit 2 改动详表（Tactical 内容重组）

### 4.1 新建 `tactical/task-runtime/00-overview.md`

骨架：

```markdown
# TaskRuntime BC

> DDD 战术层 · BC: TaskRuntime
> 任务全生命周期：从 Issue spawn / 用户发起到 dispatch / 实际执行 / 终结

§ 0  BC 一览
     职责 / UL 切片 / Context map 中的位置 / 跟其它 BC 的边界

§ 1  聚合清单（X.1）
     1.1 Aggregate Roots
         - Task                → 详 01-task.md
         - TaskExecution       → 详 02-task-execution.md
         - InputRequest        → 详 03-input-request.md
     1.2 Entities
         - Artifact            (sub of TaskExecution) → 详 02 § X
     1.3 VOs（按使用聚合分组）
         - DispatchEnvelope / DispatchAck / DispatchNack
         - WorkspaceMode / Priority
         - CompletedReason+Message / FailedReason+Message /
           KilledReason+Message (conventions § 16)
         - IssueConcludeSpec

§ 2  Invariants 索引（X.2）
     → 01 § X / 02 § X / 03 § X 各 invariants 节

§ 3  Domain Services（X.3）
     3.1 DispatchService     envelope + ACK/NACK + 单活校验 + retry=新派
     3.2 ReconcileService    worker 重连 active/stale/unknown 三类回填
     3.3 TimeoutScanner      4 类 timeout 统一扫描
     3.4 IssueConcludeSpawn  Discussion 调入；批量 tx spawn
     3.5 KillCoordinator     kill / abandon-precondition / suspend-precondition
                             两阶段决策（emit kill_requested → killed）

§ 4  Factories（X.4）
     4.1 TaskFactory             5 caller：CLI / Issue / Supervisor / Web / Bridge
     4.2 TaskExecutionFactory    DispatchService 内部 use
     4.3 InputRequestFactory     含 conversation_id=null fallback

§ 5  Repositories（X.5）
     接口签名 TBD；schema 见 implementation/02-persistence-schema.md（TBD）

§ 6  跨聚合引用出方向（X.6）
     | 引用方 → 被引方       | 强弱 | 一致性窗口 | ADR |
     |---|---|---|---|
     | Task → Conversation   | 强 / 1:1   | tx 同步 | 0017 |
     | Task → Issue          | 弱 / 血缘  | 无      | -    |
     | Task → parent_task    | 弱 / 不阻塞| 无      | -    |
     | TaskExecution → Worker| 强 / 不可变| tx 同步 | 0010, 0011 |
     | TaskExecution → InputRequest (pending) | 强 | tx 同步 | -  |
     | InputRequest → TaskExecution           | 强 / 不可变 | tx 同步 | - |
     | Artifact → TaskExecution               | 强 / 不可变 | tx 同步 | - |

§ 7  跨 BC 交互
     7.1 Supervisor 唤醒事件白名单（11 条；老 01 § 12.1）
     7.2 Bridge 渲染（Task root card / InputRequest 卡片 / agent_finding milestone）
     7.3 Observability 订阅
     7.4 Customer-Supplier 上下游表（从 strategic 03 § 3.1 摘 TaskRuntime 相关行）

§ 8  Out-of-Scope / Future
     (从老 01 § 14 拷过来 + 老 02 相关 OOS)

§ 9  References
     ADR-0010 / 0011 / 0014 / 0015 / 0017 / 0018 / 0019
     strategic/03-bounded-contexts § 1 (UL) / § 2 (BC TaskRuntime)
     conventions § 0 / § 16
     聚合详情：01-task.md / 02-task-execution.md / 03-input-request.md
```

### 4.2 新建 `tactical/task-runtime/01-task.md`

骨架：

```markdown
# Task 聚合

> TaskRuntime BC · Aggregate Root
> 工作单元身份；4 态状态机

§ 1   概述（"工作单元"语义；身份不变；从属一个 project；可 N 次 dispatch）
§ 2   状态机                         ← 老 01 § 2.1
§ 3   状态语义                        ← 老 01 § 2.2
§ 4   状态迁移                        ← 老 01 § 2.3
§ 5   字段                            ← 老 01 § 2.4
§ 6   可变性                          ← 老 01 § 2.5
§ 7   Conversation 绑定（1:1）       ← 老 01 § 2.6
§ 8   依赖（depends_on_task_ids）    ← 老 01 § 7
§ 9   创建来源（5 入口；详 00 § 4.1 Factory）
§ 10  Invariants（Task 相关 5 条）   ← 老 01 § 13 抽取
§ 11  References
```

### 4.3 新建 `tactical/task-runtime/02-task-execution.md`

骨架（最厚，含 worker 端运行时）：

```markdown
# TaskExecution 聚合

> TaskRuntime BC · 独立 Aggregate Root（持 task_id 强引用 Task）
> 一次 dispatch → 结束的运行痕迹；6 态状态机；execution_id = 主身份 + 幂等 + fencing key

§ 1   概述
      "两层模型" 的 Execution 层；独立 AR 而非 Task 内嵌 entity（D1）

§ 2   状态机（6 态 + cancel_requested_at 两阶段）  ← 老 01 § 3.1
§ 3   状态语义                                    ← 老 01 § 3.2
§ 4   状态迁移                                    ← 老 01 § 3.3
§ 5   字段                                        ← 老 01 § 3.4
§ 6   不可变 / append-only                         ← 老 01 § 3.5
§ 7   失败 / killed reason 枚举                    ← 老 01 § 3.6

§ 8   Workspace 模式 / workspace_mode VO          ← 老 01 § 6
      8.1 两种模式 (worktree / direct)
      8.2 决策维度（4 问）
      8.3 Direct 模式约束
      8.4 Workspace 资源事件（worktree.created / worktree.released）
      8.5 修改 workspace_mode

§ 9   Worker 端运行时（合并 BC4 内容 + 老 01 § 4.3-4.4）
      9.1 per-execution 目录 + 幂等                ← 老 01 § 4.3 step 2
      9.2 Worker 端处理时序 11 步                  ← 老 01 § 4.3
      9.3 Env 注入（daemon → shim → agent）        ← 老 01 § 4.4
      9.4 Shim 模型（ADR-0018）                    ← 老 workforce/01 BC4 部分
      9.5 Agent CLI 子进程 + JSONL 解析            ← 老 workforce/01 BC4 部分
      9.6 进度上报 Conversation milestone          ← 老 01 § 3.7

§ 10  Kill 进程级机制                             ← 老 01 § 5.2
      SIGTERM → 5s grace → SIGKILL；
      KillCoordinator 协议层见 00 § 3.5

§ 11  Reconcile worker 端                          ← 老 01 § 4.5 worker 端
      ReconcileService 协议层见 00 § 3.2

§ 12  Artifact 子实体                              ← 老 01 § 10
      12.1 模型 / 字段
      12.2 上报方式
      12.3 不可变 / append-only

§ 13  Invariants（Execution 相关 3 条）            ← 老 01 § 13 抽取
§ 14  References
```

### 4.4 新建 `tactical/task-runtime/03-input-request.md`

骨架：

```markdown
# InputRequest 聚合

> TaskRuntime BC · 独立 Aggregate Root
> Agent 执行中需要外部输入的同步阻塞请求

§ 1   概述（触发场景 + 调用方式）         ← 老 02 § "触发场景" / "调用方式"
§ 2   状态机
§ 3   字段
§ 4   协议（request / respond / cancel / timeout）
§ 5   完整流程（sequence 图 + 5-7 行步骤）  ← 老 02 § "完整流程" 9 步精简
§ 6   三响应路径                            ← 老 02 § "用户响应的三条路径"
      6.1 卡片按钮（card.action）
      6.2 自由文本 @bot（走 supervisor 解析）
      6.3 Slash 命令 /answer
§ 7   超时 / 升级（T1=4h / T2=24h）         ← 老 02 § "超时 / 升级"
§ 8   conversation_id=null Fallback         ← 老 02 § "关键概念" 拆出
§ 9   Invariants
§ 10  References
```

### 4.5 删除（Commit 1 搬来的旧文件）

- `tactical/task-runtime/01-task-model.md`（内容已拆到新 01-task.md + 02-task-execution.md + 00-overview.md）
- `tactical/task-runtime/02-input-required.md`（内容已拆到新 03-input-request.md）

### 4.6 `workforce/01-worker-model.md` carve

**移出（→ task-runtime/02 § 9-11）**：
- per-execution 目录管理
- Shim 模型 / ShimHello 协议（ADR-0018）
- Agent CLI 子进程 spawn / 监管
- JSONL trace 解析
- worktree 物理创建（`git worktree add`）
- direct 模式 CWD 解析
- agent CLI 中转（worker 内 unix socket）
- env 注入（daemon → shim → agent）
- 24h GC worktree
- Reconcile 的 worker 端响应（SIGTERM 本地僵尸）

**保留（BC3 Workforce 真内容）**：
- Worker 注册 / enroll
- Worker heartbeat / online-offline 状态
- WorkerProjectMapping（已生效映射）
- WorkerProjectProposal（自动发现 + accept）
- Project 元数据（add / update / remove）
- worker daemon 进程本身的生命周期（非 per-execution 的）

**文件名暂不改**：留给 Workforce BC 自己重组时统一处理。

---

## § 5. 验证清单

### 5.1 spec review（实施前）
- [ ] § 3 / § 4 改动详表无归属错
- [ ] § 4.1-4.4 骨架每个都覆盖 X.1-X.6 wrap 完整
- [ ] § 4.6 carve 没有遗漏 BC4 内容 / 误移 BC3 内容

### 5.2 Commit 1 完成验证
- [ ] ADR-0019 立成功
- [ ] strategic/03 / strategic/01 / ddd-blueprint 改动正确
- [ ] 6 个 ADR 措辞审完成（grep 验证）
- [ ] `git mv` 完成 `scheduling/` → `task-runtime/`
- [ ] 旧 anchor 引用（`task-model § 13` 等）此时仍能 resolve（文件名暂未改）
- [ ] `grep -rn "BC1 Scheduling\|BC4 Execution\|Scheduling BC\|Execution BC" docs/` 命中应仅在 ADR-0019 的 context 段（解释合并历史）

### 5.3 Commit 2 完成验证
- [ ] 4 个新文件存在 + 内容完整 + 字数预算（00 ~400-500 / 01 ~350-450 / 02 ~600-700 / 03 ~300-400）
- [ ] 旧 `01-task-model.md` / `02-input-required.md` 已删
- [ ] workforce/01 carve 完成 + 无重复 + 无遗漏
- [ ] 所有 ADR / blueprint / strategic 内对 `task-model § 13` / `input-required` 等 anchor 引用已更新到新位置
- [ ] 新 4 文件的内部 anchor / 跨文件 anchor 都能 resolve
- [ ] `grep -rn` 整个 docs/ 无 dead anchor

---

## § 6. PR / Commit message 模板

### Commit 1
```
docs(design): BC 合并 — Scheduling + Execution → TaskRuntime

BC1 Scheduling 跟 BC4 Execution 之间存在 6+ 处协议↔实现的人工分割
（dispatch / kill / workspace / reconcile / artifact / JSONL）。证据是 UL
重合度极高 + 现 01-task-model 实质是两 BC 混编。按 DDD "按 UL 划 BC"
原则合并为新 BC TaskRuntime。

变更：
- ADR-0019 立
- strategic/03 + strategic/01 + ddd-blueprint BC 数 8→7
- ddd-blueprint § 3.1 P3 模板升级（聚合骨架 + 多文件 + § X.1-X.6 wrap）
- 6 个 ADR 措辞审 (0010/0011/0014/0015/0017/0018) — 仅术语，不动决策
- tactical/scheduling/ → tactical/task-runtime/ git mv

实际内容重组留下一 commit。
```

### Commit 2
```
docs(design): TaskRuntime 战术设计落地 — 聚合骨架重组 + § X.1-X.6

按 ADR-0019 升级后的 P3 模板，TaskRuntime BC 文档按聚合切多文件：
- 00-overview.md：BC wrap + Domain Services + Factories + Repos + 跨聚合引用
- 01-task.md：Task 聚合
- 02-task-execution.md：TaskExecution 聚合（含 worker daemon 运行时；
  吸收原 BC4 内容）
- 03-input-request.md：InputRequest 聚合

同时：
- 旧 task-runtime/01-task-model.md + 02-input-required.md 删
- workforce/01-worker-model.md carve（剥 BC4 内容；保留 BC3 真内容）
```

---

## § 7. 实施后清理

- [ ] **删除本 spec 文件**：`rm docs/superpowers/specs/2026-05-19-task-runtime-bc-restructure-design.md`
- [ ] 若 `docs/superpowers/specs/` 目录空，`rmdir` —— 不留 dump bucket（memory feedback：`naming-conveys-theme`）
- [ ] 检查 `docs/superpowers/` 整体若空，一并 rmdir

---

## § 8. References

- ddd-blueprint § 3.1 P3
- conventions § 0 DDD / § 16 reason+message
- strategic/03-bounded-contexts
- 旧文档：`tactical/scheduling/01-task-model.md` / `02-input-required.md`
- 旧文档：`tactical/workforce/01-worker-model.md`（BC4 部分）
- 决策 ADR：0010 / 0011 / 0014 / 0015 / 0017 / 0018（措辞审）+ 0019（新立）
