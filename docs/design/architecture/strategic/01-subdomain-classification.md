> 📌 **v2.7 update applied (2026-06-26)** — v2.7 引入 ProjectManager BC 取代 TaskRuntime + Discussion（v2.7 #131）；Identity BC 从 Conversation 独立（ADR-0040）；Bridge BC v2 撤回（ADR-0031）。当前 active 设计以 ADR + decisions/README 为准。

# agent-center 子域分类（Subdomain Classification）

> **DDD 战略层**

> 以 [00-domain-vision](00-domain-vision.md) 为锚，对 [03-bounded-contexts § 2](03-bounded-contexts.md#-2-限界上下文bounded-contexts) 的各 BC 打 DDD subdomain 标签——定**投入策略**与**系统必要度**。

最后更新：2026-06-26（v2.7：ProjectManager BC 取代 TaskRuntime + Discussion；Identity BC 独立；当前 10 个 live BC）。

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
| **ProjectManager（项目管理）** | **Core** | — | v2.7 核心 BC——统一管理 Project / Issue / Task / Plan / PlanFinding / Subscriber / CodeRepoRef 全生命周期。取代原 TaskRuntime + Discussion。失去项目管理中枢，thesis 中"看着 N 个 agent"失去具体形态、Issue 决策审计成黑盒。对应 [S4 Center 单一权威](00-domain-vision.md#s4-center-单一权威) + [S3 对话即决策审计](00-domain-vision.md#s3-对话即决策审计)。子包：`gatecheck/`（门禁检查）、`mergecheck/`（合并检查）。代码包 `internal/projectmanager` |
| Cognition（认知 / 监督者）| **Core** | — | Thesis 直接载体——失去 LLM 决策，[thesis (A)](00-domain-vision.md#-1-核心价值thesis) 失败。对应 [S1 调度逻辑 prompt 化](00-domain-vision.md#s1-调度逻辑-prompt-化)。子包：`memory/`、`reminder/`、`wakeguard/`。代码包 `internal/cognition` |
| Identity（身份）| **Core** | — | v2.6 从 Conversation BC 独立（ADR-0040）。管理 Identity（user / agent / system）/ Organization / Member / Invitation。失去身份认证体系，用户无法登入、组织无法管理。代码包 `internal/identity` |
| Agent（Agent 实例）| Supporting | **Essential** | AgentInstance 生命周期管理（独立于 Workforce 的 Worker 管理）。失去 agent 身份与状态跟踪，系统无法知道谁在干活。代码包 `internal/agent` |
| Workforce（工作池）| Supporting | **Essential** | 系统骨架——失去 Worker 注册 / WorkerProjectProposal / project mapping，supervisor 不知有谁可派单。代码包 `internal/workforce` |
| Conversation（会话）| Supporting | **Essential** | 系统骨架——失去 Conversation / Message / Channel 抽象，进度无承载。子包：`replyguard/`。代码包 `internal/conversation`（v2.6 Identity 已独立到 `internal/identity`） |
| Environment（环境）| Supporting | **Essential** | 管理 Environment / workspace，worker 端控制流。子包：`controlstream/`。代码包 `internal/environment` |
| Observability（观测）| Supporting | Peripheral | 降级可运行——失去 fleet view / trace 投影，可 grep events 表兜底。子包：`escalator/`、`peek/`、`query/`。代码包 `internal/observability` |
| SecretManagement（密钥管理）| Supporting | Peripheral | 降级可运行——失去密钥中心化管理，可手动配置环境变量兜底。代码包 `internal/secretmgmt` |
| Files（文件管理）| Supporting | Peripheral | 文件上传 / 管理。降级可运行。代码包 `internal/files` |
| ~~TaskRuntime（任务运行时）~~ | ~~**Core**~~ | — | **RETIRED (v2.7 #131)**——职责迁移至 ProjectManager BC（pm.Task）+ Agent work-items。战术设计见 [retired/task-runtime/](../../retired/task-runtime/00-overview.md) |
| ~~Discussion（讨论）~~ | ~~**Core**~~ | — | **RETIRED (v2.7 #131)**——职责迁移至 ProjectManager BC（pm.Issue）+ Agent work-items。战术设计见 [retired/discussion/](../../retired/discussion/00-overview.md) |
| ~~Bridge（渠道桥接层）~~ | ~~Supporting~~ | ~~Peripheral~~ | **RETIRED (v2 per ADR-0031)**——v2 撤回飞书 / vendor 接入 |

**汇总**：

- **Core**: 3 个 —— ProjectManager / Cognition / Identity
- **Supporting – Essential**: 4 个 —— Agent / Workforce / Conversation / Environment
- **Supporting – Peripheral**: 3 个 —— Observability / SecretManagement / Files
- **Generic**: 0 个
- **Retired**: 3 个 —— TaskRuntime / Discussion（v2.7 #131）/ Bridge（v2 ADR-0031）

**总计 10 个 live BC**（另有 3 个 retired BC 作历史记录保留）。

---

## § 3. 为什么 0 个 Generic

10 个 live BC 全是 agent-center specific 设计，没有"业界现成实现可整体替换"的：

- 业界 generic 组件——BlobStore / DB / gRPC / OTel 等——都是**基础设施 / ACL 内部依赖**，不构成 BC
- 每个 BC 的核心模型都是为 agent-center thesis 专门设计：
  - **ProjectManager 统一 Project / Issue / Task / Plan 模型**（v2.7 取代原 TaskRuntime + Discussion）
  - **Plan DAG 调度 + GateCheck / MergeCheck 门禁**（ProjectManager 子能力）
  - **Identity / Organization / Member 身份体系**（ADR-0040 独立 BC）
  - **Worker enrollment 通过 Discovery Proposal**（[ADR-0008](../../decisions/0008-worker-project-mapping-via-discovery-proposal.md)）
  - **Supervisor file-based memory + Reminder + WakeGuard**（[ADR-0012](../../decisions/0012-memory-file-based.md)）
  - **Environment controlstream 工作区管理**
  - **Agent 实例生命周期管理**（独立于 Workforce）

→ 这本身是 agent-center 的特征：**核心价值在领域设计**，而非"组装现成组件"。

---

## § 4. 投入策略含义（实际行动差异）

| 分类 | 投入策略 | 应用举例（哪些场景该按这个 priority 操作）|
|---|---|---|
| **Core** | 最强投入 / 最聪明设计 / 最高质量门槛 / 出 bug 视为 P0 / Refactor 投入足 | ProjectManager 状态机精细 / Plan DAG 调度 / GateCheck 门禁 / Supervisor decision prompt 调优 / Identity 认证体系 |
| **Supporting – Essential** | 务实精巧 / 不思辨 / 做好就行 / 出 bug 视为 P1 / 解决方案优先选简单的 | Workforce Discovery Proposal（ADR-0008）/ Agent 实例管理 / Conversation 消息时间线 / Environment 工作区——都是必要支撑的"做好"，不需要花体力探索"更优雅设计" |
| **Supporting – Peripheral** | 最低投入度 / 够用即可 / 优先复用社区模式 / 出 bug 视为 P2 / refactor 阈值高 | Fleet view 用基础列表 / SecretManagement 加密存储 / Files 文件上传 |
| **Generic** | 直接用库 / 不自研 | — |

**投入优先级**（出 bug / refactor / code review 严苛度）：
> Core > Supporting-Essential > Supporting-Peripheral

**重要 caveat**：Supporting-Essential **不是 Core 降级版**，是**不同性质**的投入。"必要"≠"差异化"——Agent / Workforce / Conversation / Environment 必须可靠运转（务实精巧），但**不该花体力在它们的"创新设计"上**——那精力应投在 Core 三件套（ProjectManager / Cognition / Identity）。

---

## § 5. 触发重审的条件

本标注**不是一锤定音**。下列情况触发重审：

- **Vision 改动**：thesis 或 strategic stance 调整 → Core 集可能变
- **某 BC 的差异化设计显著升级**：例如若 Workforce 引入"agent capability matching"独特设计成为差异化卖点，Workforce 可能升 Core
- **业界出现 generic 方案**：例如未来若有"agent runtime as a service"成熟，部分运行时能力可能降 Generic
- **系统架构变化**：例如多 Center / 多 VPS 架构会让某些 Supporting 升 Essential 或反之

---

## § 6. 相关文档

- [00-domain-vision](00-domain-vision.md) —— 判别锚（thesis + strategic stance）
- [03-bounded-contexts § 2](03-bounded-contexts.md#-2-限界上下文bounded-contexts) —— 10 个 live BC 详细定义（另有 3 个 retired BC）
- [03-bounded-contexts § 3](03-bounded-contexts.md#-3-上下文映射context-map) —— Context Map（上下游关系 + ACL）
- [ddd-blueprint](../../ddd-blueprint.md) —— DDD 推进状态
- [conventions § 0](../../../rules/conventions.md#-0-设计方法论ddd--统一语言) —— DDD 是方法论根基
