# TaskRuntime BC 重组 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 BC1 Scheduling + BC4 Execution 合并为新 BC TaskRuntime，并按聚合骨架重组战术文档；更新所有跨文档引用；最终清理 brainstorming spec。

**Architecture:** 3 个 commit：(1) Strategic + ADR-0019 + 蓝图模板升级 + 全仓措辞 / 路径审 + 目录搬家（文件内容不动）；(2) Tactical 内容重组（4 个新文件 + 删旧 + workforce/01 carve）；(3) git rm spec 清理。

**Tech Stack:** Markdown 文档；git 版本控制；grep 验证。

**Source of truth：**
- 决策依据 & 内容映射：`docs/superpowers/specs/2026-05-19-task-runtime-bc-restructure-design.md`
- 升级模板措辞 / 新文件骨架：spec § 3.4 / § 4.1-4.4
- Carve 清单（workforce/01）：spec § 4.6

---

## 整体约定

**TDD 等价物**：docs 没有传统单测，"verify" = 一组 grep / 文件存在 / 链接 resolve 检查。每个 task 末尾跑该 task 的 verify，整 commit 末尾跑全仓 verify。

**Commit 边界**：
- Commit 1 完成所有结构性改动（路径 / 措辞 / 蓝图）；**文件内容仍是老 01-task-model + 02-input-required 原文** —— 只是搬到 `task-runtime/` 目录
- Commit 2 完成所有内容性改动（新 4 文件 / 删旧 / workforce/01 carve）；commit 后 grep 老术语应为 0（除 ADR-0019 context 段）
- Commit 3 删除 spec 文件 + 目录

**不跳 hook、不 amend、不 force push**。

---

## Phase 1 — Strategic 改动（= Commit 1）

### Task 1: 全仓引用全量盘点 + change scope 确认

**Files:** 只读盘点

- [ ] **Step 1.1: grep 全仓"Scheduling BC" / "Execution BC" 文字术语**

```bash
grep -rn "Scheduling BC\|Execution BC\|scheduling 上下文\|execution 上下文" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/ \
  --include="*.md" 2>/dev/null
```

把命中行盘到 scratch（按文件分组）；用于 Task 6 / 7 的 audit checklist。

- [ ] **Step 1.2: grep 全仓老路径引用**

```bash
grep -rn "tactical/scheduling\|scheduling/01-task-model\|scheduling/02-input-required\|workforce/01-worker-model" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/ \
  --include="*.md" 2>/dev/null
```

盘到 scratch。注意：workforce/01-worker-model 的引用要分两批 —— 引用 BC4 内容的（Commit 1 改路径或留 anchor）vs 引用 BC3 内容的（不动）。

- [ ] **Step 1.3: grep 老 anchor § 13 invariants 引用**

```bash
grep -rn "01-task-model.md#\|task-model § 13\|01-task-model § 13" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/ \
  --include="*.md" 2>/dev/null
```

- [ ] **Step 1.4: 记录命中清单**

期望命中清单（基于 brainstorming 期间的 grep）：
- 9 个 ADR 文件：0007, 0008, 0010, 0011, 0012, 0013, 0016, 0017, 0018
- 7 个 tactical 文件：agent-harness/01, agent-harness/02, bridge/01, cognition/01, conversation/01, observability/01, workforce/01
- strategic/03-bounded-contexts.md
- ddd-blueprint.md
- architecture/README.md
- requirements/01-functional.md
- roadmap.md
- drafts/session-checkpoint-2026-05-16.md

把每个文件的具体行号列出存到 scratch，供 Task 6 / 7 逐个改时核对。

> 不 commit；这是盘点 task，结果保留在 scratch 内存中或 TODO 列表。

---

### Task 2: 立 ADR-0019

**Files:**
- Create: `docs/design/decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md`

- [ ] **Step 2.1: 读 ADR-0018 作格式模板**

```bash
cat /Users/oopslink/works/codes/oopslink/agent-center/docs/design/decisions/0018-detached-agent-via-per-execution-shim.md
```

记下：metadata block 格式、Status / Context / Decision / Consequences 各 § 风格、行间距、"超链接"用法。

- [ ] **Step 2.2: 读 decisions/README.md 验 ADR 模板**

```bash
cat /Users/oopslink/works/codes/oopslink/agent-center/docs/design/decisions/README.md
```

按 README 模板填字段。

- [ ] **Step 2.3: 写 ADR-0019 文件**

内容大纲（基于 spec § 3.1）：

```markdown
# ADR-0019 — BC1 Scheduling + BC4 Execution 合并为 TaskRuntime

- **Status**: Accepted
- **Date**: 2026-05-19
- **Deciders**: oopslink
- **Supersedes / Related**: 0010 / 0011 / 0014 / 0015 / 0017 / 0018（措辞审）

## Context

[根据 spec § 1 + § 2 重述]
- 原始 BC 划分（strategic/03-bounded-contexts § 2）：BC1 Scheduling = 协议
  + 状态机；BC4 Execution = worker 侧运行时
- P3 推进 Scheduling 战术文档时发现 6 处协议↔实现的人工分割：
  - DispatchEnvelope schema（BC1）vs worker 端 11 步处理 + env 注入 + shim
    spawn（BC4）
  - Kill 两阶段协议（BC1）vs SIGTERM/5s grace/SIGKILL 进程级机制（BC4）
  - Workspace mode VO（BC1）vs worktree 物理创建 + 24h GC（BC4）
  - Artifact 归属 BC4 但被错塞在 01-task-model § 10
  - JSONL 解析 + milestone 判定（BC4）混在 01 § 3.7 进度上报
  - Reconcile 协议（BC1）vs worker 端 SIGTERM 本地僵尸（BC4）
- UL 重合度：Task / TaskExecution / dispatch / kill / workspace_mode / 
  artifact 全是 BC1+BC4 共用词；它们是同一组业务对象的"协议视角"vs
  "运行时视角"

## Decision

合并 BC1 Scheduling + BC4 Execution → 新 BC **TaskRuntime**。BC 总数 8 → 7。

新 BC 职责：Task / TaskExecution / InputRequest 聚合的全生命周期 + dispatch
协议 + 派单可靠性 + kill 两阶段 + 任务依赖 + InputRequest + worker 侧运行时
（shim / workspace 物理 / JSONL / artifact）。

## Rationale

1. DDD "按 UL 划 BC" 原则：物理 split（state 权威在 center / 实际执行在
   worker）≠ 概念 split。一个 BC 完全可以多物理位置分布式实现
2. "协议" 跟 "实现" 不是合法 BC 切分线 —— 它们是同一域的两个表述层次
3. UL 证据：BC1+BC4 共用词高密度，且事实上 01-task-model 已经混编
4. 后续维护成本：每改一处 dispatch / kill / workspace 细节都得想 "BC1 还
   是 BC4"，认知负担大于收益

## Consequences

**Strategic**：
- strategic/03-bounded-contexts § 2 BC 列表 8→7（BC1+BC4 合）
- strategic/01-subdomain-classification 总数 + 表回填；TaskRuntime 仍 Core
- ddd-blueprint § 1.1 / § 1.2 / § 3.1 P3 表 + 模板升级
- BC2-BC7 顺移（或保持现编号，等本 ADR 落实时定）

**Tactical**：
- 新 dir：`tactical/task-runtime/`（00-overview + 01-task + 02-task-execution
  + 03-input-request）
- 老 `tactical/scheduling/` 删
- `tactical/workforce/01-worker-model.md` carve（BC4 内容迁出，留 BC3 真内容）

**ADR 术语**：
- 0010 / 0011 / 0014 / 0015 / 0017 / 0018 + 部分其它 ADR 内引用的 "Scheduling
  BC" / "Execution BC" → "TaskRuntime BC"。**只改措辞，不动决策**

**蓝图 P3 模板升级**（spec § 3.4 完整草案）：
- 从"每个 BC 加一节 § X.1-X.6"升级为"按聚合骨架重组 + § X.1-X.6 wrap"
- 多文件按聚合切（多聚合 BC）/ 单文件（单聚合 BC）/ 仅 overview（无业务聚合
  BC）

## Alternatives Considered

- **(A) 保持 3 BC 现状 + 逐处加 BC4 → BC1 指针**：把 6 处人工分割固化为
  长期技术债，每改一处仍要决策 BC 归属 → 驳回
- **(C) 合并 Scheduling + Workforce + Execution 三 BC**：Workforce 的
  enrollment / mapping / discovery 跟 task 执行域共流弱；过度合并 → 驳回
- **(D) 解散 BC4 拆给 BC1 + BC3**：Artifact / JSONL 这种 per-execution 产物
  塞 Workforce 别扭；边界仍不爽 → 驳回

## References

- spec：`docs/superpowers/specs/2026-05-19-task-runtime-bc-restructure-design.md`
- conventions § 0 DDD
- ADR-0010 / 0011 / 0014 / 0015 / 0017 / 0018（措辞审；不动决策）
```

按上面大纲完整写出 markdown 文件。**不留 TBD 项**。

- [ ] **Step 2.4: Verify ADR-0019 文件存在 + 结构完整**

```bash
test -f /Users/oopslink/works/codes/oopslink/agent-center/docs/design/decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md && \
echo "ADR-0019 exists"
grep -c "^## " /Users/oopslink/works/codes/oopslink/agent-center/docs/design/decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md
```

Expected: 文件存在；`grep -c` ≥ 5（Context / Decision / Rationale / Consequences / Alternatives Considered / References）

> 暂不 commit；Phase 1 末统一 commit。

---

### Task 3: 改 strategic/03-bounded-contexts.md

**Files:**
- Modify: `docs/design/architecture/strategic/03-bounded-contexts.md`

- [ ] **Step 3.1: 读现 § 1 UL 表**

```bash
sed -n '15,35p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/strategic/03-bounded-contexts.md
```

记录 BC1 / BC4 在 UL 表里的行号。

- [ ] **Step 3.2: 改 § 1 UL 表**

- BC1 列名 "Scheduling" → "TaskRuntime"
- BC4 行的 UL 词（TaskExecution worker 侧视图 / Artifact / 等）并入 BC1 行
- BC4 行整体删除

> 用 Edit 工具按命中具体术语 + 行号一一改。

- [ ] **Step 3.3: 改 § 2 BC 列表**

读 § 2 中 BC1 段（line 146 附近）+ BC4 段（line 200 附近）：

```bash
sed -n '140,170p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/strategic/03-bounded-contexts.md
sed -n '200,215p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/strategic/03-bounded-contexts.md
```

- 删除 BC4 整段（约 line 200-215）
- 重写 BC1 段为合并后版本：
  - 标题 `### BC1: TaskRuntime（任务运行时）`
  - 职责：Task / TaskExecution / InputRequest 全生命周期 + 派单可靠性 + 单
    任务依赖 + InputRequest + worker 侧运行时（workspace / shim / JSONL /
    artifact）
  - 核心聚合：3 个 AR（Task / TaskExecution / InputRequest）+ Artifact 子实体
  - 核心事件：合并 BC1 + BC4 现有事件清单（去重）
  - 核心操作：合并
  - 详细设计：`tactical/task-runtime/00-overview.md`
- BC 编号：后续 BC2-BC7（Discussion / Workforce / Cognition / Observability /
  Conversation / Bridge）保留原编号（即原 BC2 仍是 BC2，原 BC3 仍是 BC3，原 BC5
  顺序变 BC4? 否，保留 BC5 编号但去掉中间一项）。**做法**：删除 BC4 段，BC5
  开始的所有 BC 编号 -1。Discussion 现是 BC2 仍是 BC2；Workforce 现是 BC3 仍是
  BC3；Execution 现是 BC4 被删；Cognition 现是 BC5 改 BC4；Observability 现是
  BC6 改 BC5；Conversation 现是 BC7 改 BC6；Bridge 现是 BC8 改 BC7

记 BC 重编号后的对照：

| 原 | 新 |
|---|---|
| BC1 Scheduling | BC1 TaskRuntime（合 BC4） |
| BC2 Discussion | BC2 Discussion |
| BC3 Workforce | BC3 Workforce |
| BC4 Execution | (并入 BC1) |
| BC5 Cognition | BC4 Cognition |
| BC6 Observability | BC5 Observability |
| BC7 Conversation | BC6 Conversation |
| BC8 Bridge | BC7 Bridge |

> 注：编号顺移影响所有 "BC5" / "BC6" / "BC7" / "BC8" 字面引用。grep 一遍校。

- [ ] **Step 3.4: 改 § 3 Context Map 图**

读 § 3 现 context map 图（line 290-340 附近）：

```bash
sed -n '290,360p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/strategic/03-bounded-contexts.md
```

- 删除图中 BC1↔BC4 边
- 把 BC1 节点改名 TaskRuntime
- 把 BC4 节点删除；图里 BC4↔其它 BC 边改连到 BC1 TaskRuntime
- BC5-BC8 节点的编号顺移

- [ ] **Step 3.5: 改 § 3.1 上下游表**

读 § 3.1 上下游表（line 350+）：

```bash
sed -n '350,420p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/strategic/03-bounded-contexts.md
```

- 删除 BC1↔BC4 行
- BC4↔其它 BC 行合并入 BC1↔其它 BC 行（去重；保留更准确的描述）
- BC 名 Scheduling → TaskRuntime；BC 编号 BC5+ -1

- [ ] **Step 3.6: 改 § 3.3 (或末尾) BC 列表 / 事件总览**

```bash
grep -n "^### BC\|^| BC[0-9]\|Scheduling\|Execution" /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/strategic/03-bounded-contexts.md
```

按命中逐处校：BC 名 / BC 编号 / 表头列。

- [ ] **Step 3.7: Verify**

```bash
grep -n "Scheduling BC\|Execution BC\|BC4 Execution\|### BC4:" /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/strategic/03-bounded-contexts.md
```

Expected: 0 命中（这文件不应残留 BC4 / Scheduling BC 字样）。

```bash
grep -c "^### BC[0-9]:" /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/strategic/03-bounded-contexts.md
```

Expected: 7（BC1-BC7）。

---

### Task 4: 改 strategic/01-subdomain-classification.md

**Files:**
- Modify: `docs/design/architecture/strategic/01-subdomain-classification.md`

- [ ] **Step 4.1: 读现内容**

```bash
cat /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/strategic/01-subdomain-classification.md | head -100
```

- [ ] **Step 4.2: 改总数 + 分类表**

- 总数 "8 BC" → "7 BC"
- 分类表删 BC4 Execution 行；BC1 行名改 TaskRuntime
- BC5-BC8 行编号顺移（如 BC5 Cognition → BC4 Cognition）
- 分类计数（Core 3 / Supporting-Essential 3 / Supporting-Peripheral 2 / 
  Generic 0）回填：
  - 原 Core 3：Cognition / Scheduling / Discussion
  - 原 Supporting-Essential 3：Workforce / Execution / Conversation
  - 合并后：BC4 Execution 从 Supporting-Essential 撤；BC1 TaskRuntime 仍 Core
  - 新数：Core 3（TaskRuntime / Cognition / Discussion） / 
    Supporting-Essential 2（Workforce / Conversation） / 
    Supporting-Peripheral 2（Observability / Bridge） / Generic 0
  - 总计 7 ✓

- [ ] **Step 4.3: Verify**

```bash
grep -n "^| BC[0-9]\|8 BC\|Execution\|Scheduling" /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/strategic/01-subdomain-classification.md
```

Expected: 无 "8 BC" 字样；无 "Execution" 单独成 BC 名；BC1 名为 TaskRuntime。

---

### Task 5: 改 ddd-blueprint.md（5 个区域）

**Files:**
- Modify: `docs/design/ddd-blueprint.md`

- [ ] **Step 5.1: 改 § 1.1 战略表**

```bash
sed -n '17,30p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/ddd-blueprint.md
```

- "8 个 BC" → "7 个 BC"
- 命名表：删 Scheduling + Execution 行；加 TaskRuntime 行；BC2-BC7 编号顺移

- [ ] **Step 5.2: 改 § 1.2 战术表**

```bash
sed -n '28,50p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/ddd-blueprint.md
```

各行的"位置 / 说明"列：把所有 `tactical/scheduling/...` / `tactical/workforce/...
BC4 部分` 引用改为 `tactical/task-runtime/...`。

- [ ] **Step 5.3: 改 § 2.1 ADR 表**

```bash
sed -n '57,72p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/ddd-blueprint.md
```

新增一行：

```
| [0019](decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md) | BC1 + BC4 合并为 TaskRuntime | 战略级 BC 边界调整 |
```

- [ ] **Step 5.4: 改 § 3.1 P3 段落（核心：模板升级）**

```bash
sed -n '99,135p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/ddd-blueprint.md
```

按 spec § 3.4 草案完整替换 P3 段落（"目的" + "模板" + "覆盖范围"表 + "跨聚合
视角" + "依赖" + "影响范围"）。新模板措辞要素：

```markdown
#### P3. 按聚合骨架重组每个 BC 战术文档 + 补 § X.1-X.6 wrap

**目的**：DDD 战术设计落到每个 BC 自治讲，按聚合（不按主题）组织内容。

**文件组织**：

每个 BC 在 `tactical/{bc}/` 下组织：

- `00-overview.md`：BC 入口；承载 § X.1 聚合清单 + § X.2 Invariants 索引 +
  § X.3 Domain Service + § X.4 Factory + § X.5 Repository + § X.6 跨聚合
  引用 + 跨 BC 交互 + Out-of-Scope + References
- `0N-{aggregate}.md`：每聚合一份；内容组织为
  聚合状态机 → 字段 → lifecycle ops → Invariants

**按 BC 体量规则**：
- 多聚合 BC（≥2 聚合）：00-overview + 0N-{aggregate}.md 多文件
- 单聚合 BC：00-overview.md 内合并聚合详情（不另起 01-）
- 无业务聚合 BC（如 Bridge）：仅 00-overview.md，说明"无聚合 / 仅 ACL 翻译职责"

**组织反例（明令禁止）**：
- 不按主题切（dispatch / kill / workspace / retry / timeout） —— 这是 BC1+BC4
  leaky 的根源（详见 ADR-0019）

**覆盖范围**（按推进顺序）：

| # | BC | 文件 | 聚合数 / 文件数 | 现状 |
|---|---|---|---|---|
| 1 | TaskRuntime | `tactical/task-runtime/00-overview.md` + `01-task.md` + `02-task-execution.md` + `03-input-request.md` | 3 / 4 | ✅ ADR-0019 落实后完成 |
| 2 | Discussion | `tactical/discussion/00-overview.md` + `01-issue.md` | 1-2 / 1-2 | 待重组 |
| ... | ... | ... | ... | ... |
| 7 | Bridge | `tactical/bridge/00-overview.md` | 0 / 1 | 仅 overview（无业务聚合） |
```

完整 P3 表内容沿用 spec § 3.4 + 调整 BC 顺序按上面的 1-7。

- [ ] **Step 5.5: 改 § 5 引用**

```bash
sed -n '180,195p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/ddd-blueprint.md
```

- "Invariants 系统化样板"行：从 `tactical/scheduling/01-task-model § 13` 改为
  `tactical/task-runtime/02-task-execution § 13`

- [ ] **Step 5.6: 改 § 9 最后更新日期**

文档头部"最后更新"字段更新为 2026-05-19。

- [ ] **Step 5.7: Verify**

```bash
grep -n "Scheduling BC\|Execution BC\|8 个 BC\|tactical/scheduling" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/design/ddd-blueprint.md
```

Expected: 0 命中。

```bash
grep -c "ADR-0019\|0019-bc-scheduling-execution-merged-to-task-runtime" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/design/ddd-blueprint.md
```

Expected: ≥ 2（§ 2.1 ADR 表 + § 3.1 P3 模板的 "详见 ADR-0019" 引用）

---

### Task 6: 9 个 ADR 措辞审

**Files (审计 + 改路径 + 改术语；不改决策内容):**
- Modify: `docs/design/decisions/0007-conversation-as-unified-session.md`
- Modify: `docs/design/decisions/0008-worker-project-mapping-via-discovery-proposal.md`
- Modify: `docs/design/decisions/0010-task-execution-two-layer-model.md`
- Modify: `docs/design/decisions/0011-dispatch-reliability-protocol.md`
- Modify: `docs/design/decisions/0012-memory-file-based.md`
- Modify: `docs/design/decisions/0013-supervisor-invocation-concurrency.md`
- Modify: `docs/design/decisions/0016-task-progress-via-bound-thread.md`
- Modify: `docs/design/decisions/0017-task-as-conversation.md`
- Modify: `docs/design/decisions/0018-detached-agent-via-per-execution-shim.md`

- [ ] **Step 6.1: 逐 ADR 替换术语**

每个 ADR 文件：

```bash
grep -n "Scheduling BC\|Execution BC\|BC1 Scheduling\|BC4 Execution\|tactical/scheduling\|tactical/workforce/01-worker-model" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/design/decisions/<ADR_FILE>
```

按命中：
- "Scheduling BC" / "Execution BC" → "TaskRuntime BC"
- "BC1 Scheduling" / "BC4 Execution" → "BC1 TaskRuntime"（或省去编号）
- 路径 `tactical/scheduling/01-task-model.md` → `tactical/task-runtime/02-task-execution.md`（注：内容散到 01-task / 02-task-execution 两文件；具体根据被引内容定。例如：引"Task 状态机" → 01-task；引"TaskExecution 状态机" → 02-task-execution；引"InputRequest" → 03-input-request）
- 路径 `tactical/scheduling/02-input-required.md` → `tactical/task-runtime/03-input-request.md`
- 路径 `tactical/workforce/01-worker-model.md`（引 BC4 内容时）→ `tactical/task-runtime/02-task-execution.md`
- 路径 `tactical/workforce/01-worker-model.md`（引 BC3 内容时）保留不动

> **注**：Commit 1 阶段新内容还没写，所以引用 `02-task-execution § 5` 这种锚点暂时是死的；Commit 2 写完才 resolve。这是预期 —— Commit 1 仅做"前后路径切换"+ 术语统一，anchor 准确性留 Commit 2 收敛。

- [ ] **Step 6.2: Verify 9 个 ADR**

```bash
grep -rn "Scheduling BC\|Execution BC\|BC1 Scheduling\|BC4 Execution" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/design/decisions/ \
  --include="*.md"
```

Expected: 仅 0019.md 命中（解释合并历史的 Context 段）。

```bash
grep -rn "tactical/scheduling" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/design/decisions/ \
  --include="*.md"
```

Expected: 0 命中（含 0019）。

---

### Task 7: 其它文档措辞审

**Files:**
- Modify: `docs/design/architecture/README.md`
- Modify: `docs/design/architecture/tactical/agent-harness/01-prompt-assembly.md`
- Modify: `docs/design/architecture/tactical/agent-harness/02-skill-cli-tooling.md`
- Modify: `docs/design/architecture/tactical/bridge/01-feishu-integration.md`
- Modify: `docs/design/architecture/tactical/cognition/01-supervisor-model.md`
- Modify: `docs/design/architecture/tactical/conversation/01-conversation.md`
- Modify: `docs/design/architecture/tactical/observability/01-observability.md`
- Modify: `docs/design/architecture/tactical/workforce/01-worker-model.md`（仅审引用；BC4 内容 carve 留 Task 15）
- Modify: `docs/design/requirements/01-functional.md`
- Modify: `docs/design/roadmap.md`
- Modify: `docs/drafts/session-checkpoint-2026-05-16.md`

- [ ] **Step 7.1: 逐文件审 + 改**

每个文件：grep → 替换。规则同 Task 6.1。

- [ ] **Step 7.2: Verify 全仓**

```bash
grep -rn "Scheduling BC\|Execution BC" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/ \
  --include="*.md" 2>/dev/null
```

Expected: 仅 0019.md（Context 段）+ spec 文件命中。

---

### Task 8: 目录搬家 `scheduling/` → `task-runtime/`

**Files (git mv):**

- [ ] **Step 8.1: mkdir 新目录**

```bash
mkdir -p /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/
```

- [ ] **Step 8.2: git mv 两份文件**

```bash
cd /Users/oopslink/works/codes/oopslink/agent-center
git mv docs/design/architecture/tactical/scheduling/01-task-model.md \
       docs/design/architecture/tactical/task-runtime/01-task-model.md
git mv docs/design/architecture/tactical/scheduling/02-input-required.md \
       docs/design/architecture/tactical/task-runtime/02-input-required.md
```

- [ ] **Step 8.3: 删空目录**

```bash
rmdir /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/scheduling/
```

- [ ] **Step 8.4: Verify**

```bash
test -f /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/01-task-model.md && \
test -f /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/02-input-required.md && \
test ! -d /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/scheduling/ && \
echo "git mv OK"
```

---

### Task 9: Verify + Commit 1

- [ ] **Step 9.1: 全仓 grep 老术语 / 路径 / BC 编号**

```bash
echo "--- Scheduling BC / Execution BC ---"
grep -rn "Scheduling BC\|Execution BC" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/ \
  --include="*.md" 2>/dev/null

echo "--- 老路径 tactical/scheduling ---"
grep -rn "tactical/scheduling" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/ \
  --include="*.md" 2>/dev/null

echo "--- 老 BC 编号 BC4 Execution / BC5+ ---"
grep -rn "BC4 Execution\|BC5 Cognition\|BC6 Observability\|BC7 Conversation\|BC8 Bridge" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/ \
  --include="*.md" 2>/dev/null

echo "--- 8 个 BC（指总数）---"
grep -rn "8 个 BC\|总共 8 BC" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/ \
  --include="*.md" 2>/dev/null
```

Expected：
- "Scheduling BC / Execution BC"：仅 spec + ADR-0019 Context 段
- 老路径：0 命中
- 老 BC 编号：0 命中（spec 内可能有；spec 不是项目正本，OK）
- "8 个 BC"：0 命中

如果 spec 内有命中，那是历史叙述，不改。

- [ ] **Step 9.2: 验文件结构**

```bash
ls /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/
test -f /Users/oopslink/works/codes/oopslink/agent-center/docs/design/decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md && echo "0019 OK"
test ! -d /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/scheduling/ && echo "scheduling/ deleted OK"
```

- [ ] **Step 9.3: 看 git status / diff stat**

```bash
cd /Users/oopslink/works/codes/oopslink/agent-center
git status
git diff --stat HEAD
```

预期改动文件数：~20（1 个 ADR 新增 + 1 个 strategic/03 + 1 个 strategic/01 + 1 个 blueprint + 9 个 ADR + 7-8 个 tactical + README + requirements + roadmap + 1 个 draft + 2 git mv）。

- [ ] **Step 9.4: Commit 1**

```bash
cd /Users/oopslink/works/codes/oopslink/agent-center
git add -A docs/design/decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md
git add docs/design/architecture/strategic/
git add docs/design/architecture/tactical/
git add docs/design/architecture/README.md
git add docs/design/decisions/
git add docs/design/requirements/
git add docs/design/ddd-blueprint.md
git add docs/design/roadmap.md
git add docs/drafts/
git status
```

确认 staged 清单合理后：

```bash
git commit -m "$(cat <<'EOF'
docs(design): BC 合并 — Scheduling + Execution → TaskRuntime

BC1 Scheduling 跟 BC4 Execution 之间存在 6+ 处协议↔实现的人工分割
（dispatch / kill / workspace / reconcile / artifact / JSONL 解析）。证据是
UL 重合度极高 + 现 01-task-model 实质是两 BC 混编。按 DDD "按 UL 划 BC"
原则合并为新 BC TaskRuntime。

变更：
- ADR-0019 立
- strategic/03 + strategic/01 + ddd-blueprint BC 数 8→7；BC2-BC7 编号顺移
- ddd-blueprint § 3.1 P3 模板升级（聚合骨架 + 多文件 + § X.1-X.6 wrap）
- 9 个 ADR + 7 个 tactical + README + requirements + roadmap + draft 措辞 /
  路径审 — 仅术语，不动决策
- tactical/scheduling/ → tactical/task-runtime/ git mv

实际内容重组留下一 commit。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 9.5: Verify commit OK**

```bash
git log -1 --format="%H %s"
git log -1 --stat
```

---

## Phase 2 — Tactical 内容重组（= Commit 2）

### Task 10: 写 `tactical/task-runtime/00-overview.md`

**Files:**
- Create: `docs/design/architecture/tactical/task-runtime/00-overview.md`

- [ ] **Step 10.1: 读 spec § 4.1 骨架确认 § 编号**

```bash
sed -n '/### 4.1 新建/,/### 4.2/p' \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/superpowers/specs/2026-05-19-task-runtime-bc-restructure-design.md
```

确认骨架：§ 0-§ 9。

- [ ] **Step 10.2: 读老 01-task-model § 12（跨 BC 交互）+ § 14（OOS）**

```bash
sed -n '776,860p' \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/01-task-model.md
```

记录 supervisor 唤醒事件白名单 11 条；OOS 14+ 项。这些是 § 7 / § 8 内容源。

- [ ] **Step 10.3: 写 00-overview.md（一次性完整文件）**

按 spec § 4.1 骨架完整写出。重点 §：

- **§ 0 BC 一览**：3-5 段落叙述 TaskRuntime BC 职责 + 在 context map 中位置
- **§ 1 X.1 聚合清单**：3 子节（AR / Entity / VO）；VO 按使用聚合分组
- **§ 2 X.2 Invariants 索引**：3 行表，每行 → 01/02/03 各自 Invariants 节
  锚点
- **§ 3 X.3 Domain Services**：5 个 service 各一节，每节含：入参 / 出参 /
  协议 / 不变量 / 跨聚合
- **§ 4 X.4 Factories**：3 个 factory 各一节
- **§ 5 X.5 Repositories**：4 个 Repo 接口列；详细 schema 指向
  implementation/02-persistence-schema.md（TBD）
- **§ 6 X.6 跨聚合引用出方向**：表格如 spec § 4.1 骨架
- **§ 7 跨 BC 交互**：4 子节（supervisor 事件白名单 / bridge 渲染 / observability
  订阅 / customer-supplier 表）
- **§ 8 Out-of-Scope**：拷 + 整理自老 01 § 14
- **§ 9 References**：ADR 列表 + strategic / conventions + 同 BC 内 01/02/03 链接

> **不留 TBD**；所有交叉链接使用相对路径写完整。

- [ ] **Step 10.4: Verify**

```bash
test -f /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/00-overview.md
wc -l /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/00-overview.md
grep -c "^## §\|^### " /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/00-overview.md
```

Expected: 文件存在；行数 350-550；§ + ### 总计 ≥ 25。

---

### Task 11: 写 `tactical/task-runtime/01-task.md`

**Files:**
- Create: `docs/design/architecture/tactical/task-runtime/01-task.md`

- [ ] **Step 11.1: 读老 01-task-model § 2 + § 7 + § 13（Task 相关 invariants）**

```bash
sed -n '32,150p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/01-task-model.md
sed -n '558,628p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/01-task-model.md
sed -n '826,840p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/01-task-model.md
```

- [ ] **Step 11.2: 写 01-task.md（按 spec § 4.2 骨架）**

§ 1-§ 11 完整写。内容来源：
- § 2-7 → 老 § 2.1-2.6
- § 8 → 老 § 7
- § 9 → 简短说明 5 入口；详 00 § 4.1
- § 10 → 从老 § 13 抽取与 Task 相关的 5 条 invariant
- § 11 → 引用列表

跨文件链接：
- → `00-overview.md#-3-domain-services`
- → `02-task-execution.md`（"详 02"语境）
- → `03-input-request.md`（"详 03"语境）

- [ ] **Step 11.3: Verify**

```bash
test -f /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/01-task.md
wc -l /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/01-task.md
```

Expected: 文件存在；行数 300-450。

---

### Task 12: 写 `tactical/task-runtime/02-task-execution.md`

**Files:**
- Create: `docs/design/architecture/tactical/task-runtime/02-task-execution.md`

> 最厚的文件（spec 预算 600-700 行）。建议按子任务分批写。

- [ ] **Step 12.1: 读源文件**

```bash
# 老 01-task-model § 3 / § 4.3-4.5 / § 5.2 / § 6 / § 10 / § 13
sed -n '151,275p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/01-task-model.md
sed -n '335,415p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/01-task-model.md
sed -n '428,450p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/01-task-model.md
sed -n '489,555p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/01-task-model.md
sed -n '680,725p' /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/01-task-model.md

# 老 workforce/01-worker-model 的 BC4 内容
cat /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/workforce/01-worker-model.md
```

记 BC4 内容的具体行号 / 段落。

- [ ] **Step 12.2: 写 02-task-execution.md § 1-§ 7（聚合本身）**

按 spec § 4.3 骨架：
- § 1 概述（强调 D1：独立 AR）
- § 2 状态机
- § 3 状态语义
- § 4 状态迁移
- § 5 字段
- § 6 不可变 / append-only
- § 7 失败 / killed reason 枚举

- [ ] **Step 12.3: 写 § 8 Workspace 模式**

老 01 § 6 完整搬入，注明 "workspace_mode 是挂在 TaskExecution 上的 VO 性质字段"。

- [ ] **Step 12.4: 写 § 9 Worker 端运行时（合并 BC4 内容）**

来源：
- 老 01 § 4.3 worker 端处理时序 11 步
- 老 01 § 4.4 env 注入
- workforce/01 中 BC4 部分（shim 模型 / JSONL 解析 / per-execution 目录 / agent
  CLI 子进程 / worktree 物理创建 / direct CWD 解析 / agent CLI 中转）
- 老 01 § 3.7 进度上报 milestone

- [ ] **Step 12.5: 写 § 10 Kill 进程级机制**

老 01 § 5.2 搬入。**注**：协议两阶段（emit kill_requested → killed）属于 
KillCoordinator domain service，详 00 § 3.5；本节只描述 worker 收到 
kill_requested 之后的进程级动作（SIGTERM/5s/SIGKILL/关 socket/取消 IR/emit killed）。

- [ ] **Step 12.6: 写 § 11 Reconcile worker 端**

老 01 § 4.5 worker 端部分。协议层指针 → 00 § 3.2。

- [ ] **Step 12.7: 写 § 12 Artifact 子实体**

老 01 § 10 搬入。本 § 内说明 Artifact 是 TaskExecution 的子 entity（按 X.1）。

- [ ] **Step 12.8: 写 § 13 Invariants（Execution 相关）+ § 14 References**

§ 13：从老 01 § 13 抽出与 TaskExecution 相关的 invariants（约 3 条）。
§ 14：完整 references。

- [ ] **Step 12.9: Verify**

```bash
test -f /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/02-task-execution.md
wc -l /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/02-task-execution.md
grep -c "^## §\|^### " /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/02-task-execution.md
```

Expected: 文件存在；行数 500-750；§ + ### 总计 ≥ 30。

---

### Task 13: 写 `tactical/task-runtime/03-input-request.md`

**Files:**
- Create: `docs/design/architecture/tactical/task-runtime/03-input-request.md`

- [ ] **Step 13.1: 读老 02-input-required.md**

```bash
cat /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/02-input-required.md
```

- [ ] **Step 13.2: 写 03-input-request.md（按 spec § 4.4 骨架）**

§ 1-§ 10 完整写。重点：
- § 5 完整流程：从老 02 的 9 步压成 sequence 图 + 5-7 行步骤说明
- § 8 conversation_id=null fallback 单独一节（之前散在"关键概念"）

- [ ] **Step 13.3: Verify**

```bash
test -f /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/03-input-request.md
wc -l /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/03-input-request.md
```

Expected: 文件存在；行数 250-400。

---

### Task 14: 删除老文件

**Files:**
- Delete: `docs/design/architecture/tactical/task-runtime/01-task-model.md`
- Delete: `docs/design/architecture/tactical/task-runtime/02-input-required.md`

- [ ] **Step 14.1: 删两份老文件**

```bash
cd /Users/oopslink/works/codes/oopslink/agent-center
git rm docs/design/architecture/tactical/task-runtime/01-task-model.md
git rm docs/design/architecture/tactical/task-runtime/02-input-required.md
```

- [ ] **Step 14.2: Verify**

```bash
test ! -f /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/01-task-model.md && \
test ! -f /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/02-input-required.md && \
echo "old files removed OK"

ls /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/
```

Expected: 仅 00-overview.md / 01-task.md / 02-task-execution.md / 03-input-request.md

---

### Task 15: `workforce/01-worker-model.md` carve

**Files:**
- Modify: `docs/design/architecture/tactical/workforce/01-worker-model.md`

- [ ] **Step 15.1: 读现 workforce/01 完整内容 + 按 spec § 4.6 分类**

```bash
cat /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/workforce/01-worker-model.md
```

按 spec § 4.6 分类两堆：
- **保留**（BC3）：Worker 注册 / heartbeat / online-offline / WorkerProjectMapping
  / WorkerProjectProposal / Project 元数据 / worker daemon 进程本身的生命周期
- **移出**（BC4 内容）：per-execution 目录 / Shim 模型 / Agent CLI 子进程 / 
  JSONL 解析 / worktree 物理创建 / direct CWD 解析 / agent CLI 中转 / env 注入 /
  24h GC worktree / Reconcile 的 worker 端响应

注：BC4 内容应该在 Task 12 已经写入 02-task-execution.md § 9-11；本任务只做"删除"，不重复迁移。

- [ ] **Step 15.2: 删除 BC4 段落 / 节**

用 Edit 工具逐段删除 BC4 相关 §。**不删 BC3 内容**。

- [ ] **Step 15.3: 加 cross-reference 指针**

在 workforce/01 中合适位置（顶部 banner 或 BC4 内容原所在 § 处）加一句：

```markdown
> Worker 上 per-execution 运行时（shim / workspace / JSONL / artifact）归
> TaskRuntime BC，详见 [`../task-runtime/02-task-execution.md § 9-12`](
> ../task-runtime/02-task-execution.md)（[ADR-0019](
> ../../decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)）。
```

- [ ] **Step 15.4: Verify**

```bash
wc -l /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/workforce/01-worker-model.md
grep -n "Shim\|ShimHello\|per-execution\|JSONL\|worktree add\|agent CLI 子进程" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/workforce/01-worker-model.md
```

Expected：行数显著缩小；BC4 关键词 0 命中（仅"详见..."指针行可能命中"shim"作为指针用语）。

---

### Task 16: 跨文件 anchor 审 + 全仓死链检查

**Files:** 全仓审

- [ ] **Step 16.1: grep 所有指向 01-task-model § XX / 02-input-required § XX 的引用**

```bash
grep -rn "01-task-model\.md#\|01-task-model §\|02-input-required\.md#\|02-input-required §" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/ \
  --include="*.md" 2>/dev/null
```

Expected: 0 命中（Commit 1 改完后老路径应已无指向）。如有命中：
- 改指向新文件位置（按 spec § 3-§ 4 迁移映射判断目标）
- 0019.md context 段可保留（历史叙述）

- [ ] **Step 16.2: grep `task-runtime/0X.md` 新引用**

```bash
grep -rn "task-runtime/00-overview\|task-runtime/01-task\.md\|task-runtime/02-task-execution\|task-runtime/03-input-request" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/ \
  --include="*.md" 2>/dev/null
```

记总数 + 分文件检查是否都 resolve。

- [ ] **Step 16.3: 检查新 4 文件互引完整**

```bash
grep -rn "task-runtime/0" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/ \
  --include="*.md"
```

预期：每个文件都有 → 00-overview 的引用；00-overview 有 → 01/02/03 的引用。

---

### Task 17: Verify + Commit 2

- [ ] **Step 17.1: 整体 verify**

```bash
echo "--- new files ---"
ls /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/
echo "--- old files removed ---"
test ! -f /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/01-task-model.md && echo "OK"
test ! -f /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/task-runtime/02-input-required.md && echo "OK"

echo "--- workforce/01 carved ---"
wc -l /Users/oopslink/works/codes/oopslink/agent-center/docs/design/architecture/tactical/workforce/01-worker-model.md

echo "--- no Scheduling/Execution BC remnants (except ADR-0019 / spec) ---"
grep -rn "Scheduling BC\|Execution BC\|BC1 Scheduling\|BC4 Execution" \
  /Users/oopslink/works/codes/oopslink/agent-center/docs/ \
  --include="*.md" 2>/dev/null
```

预期最后一项仅命中 ADR-0019 context + spec 文件。

- [ ] **Step 17.2: Commit 2**

```bash
cd /Users/oopslink/works/codes/oopslink/agent-center
git status
git add docs/design/architecture/tactical/task-runtime/
git add docs/design/architecture/tactical/workforce/
git status
```

```bash
git commit -m "$(cat <<'EOF'
docs(design): TaskRuntime 战术设计落地 — 聚合骨架重组 + § X.1-X.6

按 ADR-0019 升级后的 P3 模板，TaskRuntime BC 文档按聚合切多文件：
- 00-overview.md：BC wrap + Domain Services (5) + Factories (3) + Repos +
  跨聚合引用 + 跨 BC 交互
- 01-task.md：Task 聚合（状态机 + 字段 + Conversation 绑定 + 依赖 + invariants）
- 02-task-execution.md：TaskExecution 聚合（含 worker daemon 运行时；吸收原
  BC4 内容：shim / workspace 物理 / JSONL / artifact / reconcile worker 端 /
  kill 进程级机制）
- 03-input-request.md：InputRequest 聚合（状态机 + 协议 + 三响应路径 + 
  fallback）

同时：
- 旧 task-runtime/01-task-model.md + 02-input-required.md 删（Commit 1 搬过来
  的老文件）
- workforce/01-worker-model.md carve：剥 BC4 内容；保留 BC3 真内容（Worker /
  Project / Mapping / Proposal / daemon 自身生命周期）

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 17.3: Verify commit OK**

```bash
git log -2 --format="%H %s"
git log -1 --stat
```

---

## Phase 3 — 清理（= Commit 3）

### Task 18: 删除 spec + dir 清理

**Files:**
- Delete: `docs/superpowers/specs/2026-05-19-task-runtime-bc-restructure-design.md`
- Delete: `docs/superpowers/plans/2026-05-19-task-runtime-bc-restructure.md`
- Delete: `docs/superpowers/specs/`（若空）
- Delete: `docs/superpowers/plans/`（若空）
- Delete: `docs/superpowers/`（若空）

- [ ] **Step 18.1: git rm spec + plan**

```bash
cd /Users/oopslink/works/codes/oopslink/agent-center
git rm docs/superpowers/specs/2026-05-19-task-runtime-bc-restructure-design.md
git rm docs/superpowers/plans/2026-05-19-task-runtime-bc-restructure.md
```

- [ ] **Step 18.2: 清空目录**

```bash
ls /Users/oopslink/works/codes/oopslink/agent-center/docs/superpowers/specs/ 2>/dev/null && rmdir /Users/oopslink/works/codes/oopslink/agent-center/docs/superpowers/specs/
ls /Users/oopslink/works/codes/oopslink/agent-center/docs/superpowers/plans/ 2>/dev/null && rmdir /Users/oopslink/works/codes/oopslink/agent-center/docs/superpowers/plans/
ls /Users/oopslink/works/codes/oopslink/agent-center/docs/superpowers/ 2>/dev/null && rmdir /Users/oopslink/works/codes/oopslink/agent-center/docs/superpowers/
```

> 注：plan 文件也一并 rm —— 它跟 spec 都是工作期产物。若用户想保留 plan 作为审计，可在 Step 18.1 跳过 plan 删除（**视用户偏好**）。

- [ ] **Step 18.3: Verify**

```bash
test ! -d /Users/oopslink/works/codes/oopslink/agent-center/docs/superpowers/ && echo "superpowers/ cleaned up"
```

- [ ] **Step 18.4: Commit 3**

```bash
cd /Users/oopslink/works/codes/oopslink/agent-center
git status
git add -A docs/superpowers/  # capture deletions
git status
git commit -m "$(cat <<'EOF'
chore(spec): 删除 TaskRuntime BC 重组 brainstorming spec + plan（实施完成）

TaskRuntime BC 重组 brainstorming（spec d12a44c）+ 实施 plan 已通过两个
docs(design) commit 落地：
- BC 合并 + strategic + 蓝图 + 9 ADR 措辞审
- TaskRuntime 战术设计落地 + workforce/01 carve

Spec / plan 是工作期产物，按既定流程实施完即清理；BC 合并决策永久档案 →
ADR-0019。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 18.5: 最终 git log 检查**

```bash
git log -4 --format="%H %s"
```

Expected 4 个 commit：spec 立 → Commit 1 strategic → Commit 2 tactical → Commit 3 清理。

---

## 自审验证

- [ ] **覆盖度**：spec § 1-§ 7 各节有无对应 task？
  - § 1 背景 / § 2 决策 → ADR-0019 + commit messages 承载
  - § 3 Commit 1 详表 → Phase 1（Task 1-9）✓
  - § 4 Commit 2 详表 → Phase 2（Task 10-17）✓
  - § 5 验证清单 → 嵌入 Step verify ✓
  - § 6 commit message 模板 → Task 9.4 / 17.2 / 18.4 ✓
  - § 7 清理 → Phase 3（Task 18）✓
- [ ] **路径准确**：所有 file path 用绝对路径；Edit / Read / Write / Bash 都 OK
- [ ] **Verify 命令**：每个 task 末尾有 grep / test / wc 验证
- [ ] **不留 TBD**：plan 文档自身不留 TBD；新文件骨架内的 TBD（implementation/persistence-schema）是预期保留 ✓

---

## References

- spec：`docs/superpowers/specs/2026-05-19-task-runtime-bc-restructure-design.md`
- ddd-blueprint.md § 3.1 P3
- conventions § 0 DDD / § 16 reason+message
- 旧文档：`tactical/scheduling/01-task-model.md` / `02-input-required.md`（Commit 1 后位于 task-runtime/）
- 旧文档：`tactical/workforce/01-worker-model.md`（BC4 部分；Commit 2 carve）
