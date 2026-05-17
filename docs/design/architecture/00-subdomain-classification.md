# agent-center 子域分类（Subdomain Classification）

> 以 [00-domain-vision](00-domain-vision.md) 为锚，对 [01-bounded-contexts § 2](01-bounded-contexts.md#-2-限界上下文bounded-contexts) 的 8 个 BC 各打 DDD subdomain 标签——定**投入策略**与**系统必要度**。

最后更新：2026-05-17。

---

## § 1. 两个维度

DDD 把领域切成子域，按 **投入策略** 分三类（**主分类**）：

| 类别 | 投入策略 | 判别灵魂 |
|---|---|---|
| **Core 核心** | 投最强能力 / 必须自研 / 最聪明设计 | 失去其独特设计，[Vision thesis](00-domain-vision.md#-1-核心价值thesis) 或核心 [strategic stance](00-domain-vision.md#-2-关键策略立场strategic-stances) 直接失败 |
| **Supporting 支撑** | 可自研但克制简化 | 必要但非差异化——失去 thesis 还在；业界无现成方案可整体复用 |
| **Generic 通用** | 优先复用现成方案（库 / 服务 / SaaS） | 业界已有成熟方案；自研只是因为没空挑现成的 |

**Sub-label**（项目内 nuance）—— 在 Supporting 内进一步区分 **系统必要度**：

| Sub-label | 含义 |
|---|---|
| **Essential** | 失去后系统**直接不 work**（骨架级，supervisor 无法运作 / agent 无法跑 / 用户无法看进度） |
| **Peripheral** | 失去后系统**降级可继续运行**（基础调度 + 干活 + 决策 仍成立） |

> 主分类回答"是否差异化 / 投入策略"；sub-label 回答"系统必要度"。两个维度**独立**。

---

## § 2. 标注表

| BC | 主分类 | Sub-label | 判别理由（锚 Vision） |
|---|---|---|---|
| Cognition（认知 / 监督者）| **Core** | — | Thesis 直接载体——失去 LLM 决策，[thesis (A)](00-domain-vision.md#-1-核心价值thesis) 失败。对应 [S1 调度逻辑 prompt 化](00-domain-vision.md#s1-调度逻辑-prompt-化) |
| Scheduling（调度）| **Core** | — | 多 agent 协同的中央——失去任务调度仲裁，thesis 中"看着 N 个 agent"失去具体形态。对应 [S4 Center 单一权威](00-domain-vision.md#s4-center-单一权威) |
| Discussion（讨论）| **Core** | — | Supervisor ↔ user 高阶决策协商的主战场——失去 Issue 决策审计，supervisor 决策成黑盒。对应 [S3 对话即决策审计](00-domain-vision.md#s3-对话即决策审计) |
| Workforce（工作池）| Supporting | **Essential** | 系统骨架——失去 worker 注册 / project mapping，supervisor 不知有谁可派单。Thesis 角度：thesis 还在（用户手填 worker 列表理论可行）；系统角度：直接不 work |
| Execution（执行运行时）| Supporting | **Essential** | 系统骨架——失去 task execution / runtime，agent 跑不起来。Thesis 角度：thesis 还在（用户手起 agent 进程理论可行）；系统角度：直接不 work |
| Conversation（会话）| Supporting | **Essential** | 系统骨架——失去 conversation 抽象，task 进度无承载、IM 消息无锚。Thesis 角度：thesis 还在；系统角度：用户层无法运作 |
| Observability（观测）| Supporting | Peripheral | 降级可运行——失去 fleet view / trace 投影，可 grep events 表 / 看 DB 兜底。可观测性能力降级但 Core 三件套（认知 / 调度 / 讨论）依然成立 |
| Bridge（渠道桥接层）| Supporting | Peripheral | 降级可运行——失去飞书 bridge，SSH CLI / Web Console 仍可用。S3 "对话即审计" 退化到 Web 端而非 IM，但 thesis 不靠 IM 唯一渠道 |

**汇总**：

- **Core**: 3 个 —— Cognition / Scheduling / Discussion
- **Supporting – Essential**: 3 个 —— Workforce / Execution / Conversation
- **Supporting – Peripheral**: 2 个 —— Observability / Bridge
- **Generic**: 0 个

---

## § 3. 为什么 0 个 Generic

8 个 BC 全是 agent-center specific 设计，没有"业界现成实现可整体替换"的：

- 业界 generic 组件——BlobStore / DB / gRPC / Feishu SDK / OTel 等——都是**基础设施 / ACL 内部依赖**，不构成 BC
- 每个 BC 的核心模型都是为 agent-center thesis 专门设计：
  - **Task / Execution 两层模型**（[ADR-0010](../decisions/0010-task-execution-two-layer-model.md)）
  - **Issue 协商流**（[03-issue-discussion](03-issue-discussion.md)）
  - **Task ↔ Conversation 1:1**（[ADR-0017](../decisions/0017-task-as-conversation.md)）
  - **Worker enrollment 通过 Discovery Proposal**（[ADR-0008](../decisions/0008-worker-project-mapping-via-discovery-proposal.md)）
  - **Per-execution shim**（[ADR-0018](../decisions/0018-detached-agent-via-per-execution-shim.md)）
  - **Supervisor file-based memory**（[ADR-0012](../decisions/0012-memory-file-based.md)）

→ 这本身是 agent-center 的特征：**核心价值在领域设计**，而非"组装现成组件"。

---

## § 4. 投入策略含义（实际行动差异）

| 分类 | 投入策略 | 应用举例（哪些场景该按这个 priority 操作）|
|---|---|---|
| **Core** | 最强投入 / 最聪明设计 / 最高质量门槛 / 出 bug 视为 P0 / Refactor 投入足 | Supervisor decision prompt 调优 / Task 状态机精细 / Issue 收敛协议 / Dispatch reliability 协议 |
| **Supporting – Essential** | 务实精巧 / 不思辨 / 做好就行 / 出 bug 视为 P1 / 解决方案优先选简单的 | Workforce Discovery Proposal（ADR-0008）/ Per-execution shim（ADR-0018）/ Task ↔ Conversation 1:1（ADR-0017）——都是必要支撑的"做好"，不需要花体力探索"更优雅设计" |
| **Supporting – Peripheral** | 最低投入度 / 够用即可 / 优先复用社区模式 / 出 bug 视为 P2 / refactor 阈值高 | Fleet view 用基础列表（[roadmap v3](../roadmap.md) 推迟时间轴 / 对接 OTel）/ Bridge 只做飞书 adapter，多 vendor 推迟 |
| **Generic** | 直接用库 / 不自研 | — |

**投入优先级**（出 bug / refactor / code review 严苛度）：
> Core > Supporting-Essential > Supporting-Peripheral

**重要 caveat**：Supporting-Essential **不是 Core 降级版**，是**不同性质**的投入。"必要"≠"差异化"——Workforce/Execution/Conversation 必须可靠运转（务实精巧），但**不该花体力在它们的"创新设计"上**——那精力应投在 Core 三件套。

---

## § 5. 触发重审的条件

本标注**不是一锤定音**。下列情况触发重审：

- **Vision 改动**：thesis 或 strategic stance 调整 → Core 集可能变
- **某 BC 的差异化设计显著升级**：例如 v2+ 若 Workforce 引入"agent capability matching"独特设计成为差异化卖点，Workforce 可能升 Core
- **业界出现 generic 方案**：例如未来若有"agent runtime as a service"成熟，Execution 可能降 Generic
- **系统架构变化**：例如多 Center / 多 VPS 架构会让某些 Supporting 升 Essential 或反之

---

## § 6. 相关文档

- [00-domain-vision](00-domain-vision.md) —— 判别锚（thesis + strategic stance）
- [01-bounded-contexts § 2](01-bounded-contexts.md#-2-限界上下文bounded-contexts) —— 8 个 BC 详细定义
- [01-bounded-contexts § 3](01-bounded-contexts.md#-3-上下文映射context-map) —— Context Map（上下游关系 + ACL）
- [ddd-blueprint](../ddd-blueprint.md) —— DDD 推进状态
- [conventions § 0](../../rules/conventions.md#-0-设计方法论ddd--统一语言) —— DDD 是方法论根基
