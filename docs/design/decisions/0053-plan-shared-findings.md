# 0053. Plan Shared Findings（DeLM「共享的、经校验的上下文」落地）

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-06-14 |
| Delivered | v2.10 P1；详 [v2.10-plan-shared-findings.md](../v2.10-plan-shared-findings.md) |
| Related | [ADR-0047 Built-in claimable pool] / v2.9 Plan Orchestration / [ADR-0012 Memory file-based](0012-memory-file-based.md) / [ADR-0001 no-MCP](0001-no-mcp.md)（已被 v2.9 MCP tool 面实际取代） |
| Source | arXiv:2606.10662 *Decentralized Multi-Agent Systems with Shared Context* (DeLM, Stanford 2026-06) |

## Context

DeLM（Decentralized Language Models with Shared Context）指出：多 agent 系统真正的瓶颈不是「并行执行」，而是缺少一条**共享的、入库前经校验的上下文**通道，让一个 agent 的发现 / 失败 / 约束变成其它 agent **可复用的问题状态**。它的三大件是：并行 agent + 任务队列 + 共享校验上下文。

agent-center 现状已具备其中两件半：

- **任务队列 + 异步 claim**：Work Board（Backlog · built-in pool · Plans）+ claimable + `get_my_work`（ADR-0047）。
- **并行 agent**：Worker + AgentInstance（ADR-0024）。
- **依赖感知队列**：Plan = Task DAG（v2.9）。
- **去中心机械编排**：`PlanOrchestratorProjector` 是事件驱动纯函数，不是会合并/转述的 LLM 主 agent —— 比 DeLM 批判的中心化 MAS 更去中心。

**缺口**：当一个 Plan DAG 节点就绪被派单时，agent 只拿到自己任务的 `title + description`（[plan_lifecycle.go:251](../../../internal/projectmanager/service/plan_lifecycle.go) 的 `content`），**拿不到上游 / 兄弟节点学到了什么**；任务结果只是 status 翻转，发现 / 失败 / 约束不沉淀、不共享、不跨 agent 传播。Memory（ADR-0012）是 per-agent 私有的，学习不传播。

## Decision

在 **ProjectManager BC** 内新增聚合根 **PlanFinding**（统一语言：**Finding（plan 作用域）**）——一条紧凑、归属到来源 Task、plan 作用域的知识 gist。agent 干活时把发现 record 回 Plan 的共享上下文；派单下游节点时，把已记录的 findings **注入到 @mention 派单内容**里；任意 agent 可用 `list_findings` 读取。

### 锁定的分叉决策

1. **BC 归属 = ProjectManager（不是 Cognition/Memory）。** Finding 是 **plan 作用域**的、在派单时（pm orchestrator）被读取的、由 Plan 生命周期 cascade 管理的状态 → 内聚在 pm BC，单 BC 拥有 `pm_plan_findings` 表（§9.z）。Memory（ADR-0012）保持 per-agent 私有不变；本决策不动它。这是新增 AR，不重画 BC 边界。

2. **入库校验（admission）v1 = 证据归属，而非 LLM 校验。** `RecordFinding` 要求 **author == 来源 Task 的 assignee**（你只能为你实际执行的 Task 记录 finding），且 author 是 project member、project 未 archived。这是 DeLM「verified before admission」的轻量版：finding 绑定到作者真正干过的 Task。**完整 LLM-verifier（拿 gist 对账证据）推迟**（见 § Consequences / roadmap）。

3. **Finding 不可变（append-only gist）。** 没有 Update：要改就 retract + 重记。匹配「gist 是一次性凝练的结论」语义，并让并发无写冲突。提供 Delete（retract）+ cascade。

4. **注入点 = 结构化 Plan 的 @mention 派单内容**（`dispatchReadyNodes`）。built-in pull pool 无 @mention，其 agent 通过 `list_findings` 主动读（pull 语义一致）。

5. **命名 = Finding（plan 作用域）**，统一语言里显式区分于 Issue 讨论里的 `agent_finding` IssueComment kind（不同子域：issue 议事 vs plan 共享上下文）。kind 枚举 `fact / failure / constraint / patch_summary` 直接取自 DeLM。

### 模型

```
Plan (existing)
 └── 0..N PlanFinding        # plan 作用域共享上下文
      { id, plan_id, task_id(来源), project_id, author_ref, kind, content(gist), created_at, version=1 }
```

- `kind ∈ {fact, failure, constraint, patch_summary}`（DeLM tag）。
- `content` 是紧凑 gist：构造期强制 ≤ 4000 字符（保持「compact」，且远低于 [§8 BlobStore](../../rules/conventions.md) 10KB 阈值 → 留在 DB TEXT，不入 BlobStore）。
- 不可变：无 `updated_at`，`version` 恒为 1（保留以对齐 AR round-trip 范式）。

### 可观测性（§2）

| Domain Event | 何时 | payload |
|---|---|---|
| `pm.plan_finding.recorded` | RecordFinding 成功（同 tx） | finding_id, plan_id, task_id, project_id, organization_id, author_ref, kind |
| `pm.plan_finding.retracted` | RetractFinding 成功（同 tx） | finding_id, plan_id, project_id, retracted_by |

两者皆为纯动作事件（无「为什么」）→ 不需要 `reason+message`（§16，对齐 `task.dependency_added`）。

### Agent 能力面（§3，沿用 v2.9 MCP tool 面）

- `record_finding(plan_id, task_id, kind, content)` → `{finding_id}`
- `list_findings(plan_id)` → `{findings:[{finding_id, task_id, author_ref, kind, content, created_at}]}`

经 admin endpoint（`requireAgentOnWorker` 守门）→ `PMService.RecordFinding/ListPlanFindings`，与 `create_plan` 等完全同构。**读路径同样要求 project membership**（`ListPlanFindings` actor-aware，见下 § Post-review hardening #2）：worker-bound 不足以读任意 plan 的 findings。

## Consequences

### 正面
- 一个 agent 的发现 / 失败 / 约束变成 plan 内**可复用的共享状态**，下游/重跑 agent 不再失忆重来（DeLM 机制一）。
- 约束以原文保留、不被中央转述软化（DeLM 机制二）。
- 注入的是紧凑 gist 而非整条 trace，成本可控（DeLM 机制三）。
- additive：不动机械 orchestrator、不动 DAG 模型；findings repo 在 Service 上 OPTIONAL（nil-safe），旧构造不受影响。

### 推迟（roadmap，非边界）
- **LLM-verifier admission gate**：spawn 一个 verifier agent 拿 gist 对账证据后才 admit（DeLM 完整「verified before admission」）。v1 用证据归属代替。
- **分层 unfold（gist→summary→raw）**：复用 ADR-0035 carry-over「引用不复制」展开到来源 thread。
- **built-in pool 派单注入**：当前 pull pool 靠 `list_findings` 主动读。
- **task 失败自动落 FAIL gist**：v1 由 agent 显式 `record_finding`。
- **Web Console 查看 findings UI**：v1 仅后端 + agent tool；FE 入口推迟。

### 负面 / 待跟进
- 注入内容变长：派单 @mention 体积随 findings 增长 → 注入做 **bounded read**（repo `CountByPlan` + `ListLatestByPlan(cap=20)`，不把全量 load 进派单 tx）+ 标注 `latest N of M`，不静默截断（§17）。
- 证据归属校验不等于内容真伪校验：错误 finding 仍可能进库（待 LLM-verifier）。v1 靠 retract 兜底。

### Post-review hardening（2026-06-15，GPT-5.5 xhigh 独立 review）
1. **`findingOneLine` 按 rune 截断**（原按字节，会切断中文等多字节字符 → 非法 UTF-8）。**真 bug**。
2. **`ListPlanFindings` actor-aware**：load plan + `requireProjectMember` + 未知 plan → `ErrPlanNotFound`（原为无授权 plain read，worker-bound 即可读任意 plan → 跨 project 信息泄露）。
3. **`RecordFinding` 加 `task.ProjectID()==plan.ProjectID()`** defense-in-depth（no-FK 下挡 inconsistent task 行）。
4. **派单注入 bounded read**（见上）。
5. **repo unique 冲突 → `ErrPlanFindingExists`（409）**，不再误映射成 `ErrPlanFindingNotFound`（404）。

## Alternatives Considered

- **A. 放进 Cognition/Memory BC 做「共享 memory scope」**：✅ 概念贴「shared memory」；❌ Memory 是 per-agent 文件仓（ADR-0012），plan 作用域 + 派单读取 + DAG cascade 都在 pm，跨 BC 取数破坏内聚。否决。
- **B. 复用 Conversation 当共享上下文（往 plan conversation 发消息）**：✅ 零新表；❌ conversation 是给人讨论 + 派单 @mention 的原始消息流，不是 curated 知识基底；混入会污染 wake 语义、无 kind/无 cap、无证据归属。否决（DeLM 明确：raw message buffer ≠ curated shared state）。
- **C. 直接做完整 LLM-verifier**：✅ 最忠于论文；❌ 需 spawn verifier agent、成本/延迟大，v1 过重。先证据归属，verifier 进 roadmap。
- **D. Finding 可变（带 Update）**：❌ gist 应是凝练结论；可变带来并发写冲突 + 版本语义复杂。选不可变 + retract。
