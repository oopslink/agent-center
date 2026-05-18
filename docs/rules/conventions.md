# 项目规约 (Project Conventions)

> **所有 agent-center 设计 / 开发 / 测试前必读。** 任何新功能 / 新决定都要按本文档列出的原则做自检。提交前过一遍 [§ 15 自检清单](#-15-新功能--新设计自检清单)。

本文档列出**跨切原则**（cross-cutting principles）—— 不属于某个特定模块，但每个模块都必须遵守。模块特定的细节归 [docs/design/](../design/)。

## § 0. 设计方法论：DDD + 统一语言

**agent-center 使用 [DDD（Domain-Driven Design）](https://en.wikipedia.org/wiki/Domain-driven_design)作为项目设计与实现方法论**。所有设计文档、ADR、代码注释、提交信息、issue 讨论、code review 评论都必须使用 DDD 统一语言（Ubiquitous Language）。

### § 0.1 统一语言（Ubiquitous Language）

**项目通用语言以 [`docs/design/architecture/01-bounded-contexts.md` § 1](../design/architecture/strategic/03-bounded-contexts.md#-1-通用语言ubiquitous-language) 为权威**。包含：

- 核心实体 / 聚合根 / 值对象的术语（Task / TaskExecution / Issue / IssueComment / Worker / Conversation / Message / Identity / ChannelBinding / SupervisorInvocation / Memory / Artifact / InputRequest / ...）
- 行为动词（Dispatch / Conclude / Spawn / Escalate / Enroll / Adopt / Withdraw / Open / Cancel / Add-message / Comment / Deliver / ...）
- 状态机词汇（Task: open / suspended / done / abandoned；TaskExecution: submitted / working / input_required / completed / failed / killed；等）
- 易混淆术语对照（Supervisor 不叫 Brain；Issue 不叫 Suggestion；TaskExecution 不叫 AgentSession 等）

**违反命名一致性见 [§ 12](#-12-命名一致)**：代码包 / 文档 / CLI / event_type 字符串 / 数据库表名都用同一组词。

### § 0.2 DDD 术语自检

讨论 / 文档 / 代码里出现的所有领域概念，必须能在以下范畴里被归类：

| 范畴 | 含义 | 举例 |
|---|---|---|
| **Bounded Context** | 限界上下文 | Scheduling / Discussion / Workforce / Execution / Cognition / Observability / Conversation / Bridge（8 个，详见 [01 § 2](../design/architecture/strategic/03-bounded-contexts.md#-2-限界上下文bounded-contexts)）|
| **Aggregate / Aggregate Root** | 聚合 / 聚合根 | Task（根）+ TaskExecution（实体，从属）；Issue（根）+ IssueComment（实体，从属）；Conversation（根）+ Message（实体，从属）；... |
| **Entity** | 实体（有 identity，跨时间可变） | 各聚合内的从属实体；详见 01 § 1.1 |
| **Value Object** | 值对象（由属性定义、不可变） | ChannelBinding / DispatchEnvelope / Reason+Message 对 / FailedReason taxonomy / IssueConcludeSpec / 等（v1 散落，正在系统化）|
| **Domain Event** | 领域事件 | `task.created` / `issue.concluded` / `conversation.message_added` / ... 详见 [05-observability.md](../design/architecture/tactical/observability/01-observability.md) |
| **Domain Service** | 跨聚合的纯领域逻辑 | Supervisor 决策 / Dispatch 调度 / Issue conclude → spawn tasks |
| **Repository** | 聚合的持久化访问 | 见 [implementation/02-persistence-schema.md](../design/implementation/) (TBD) |
| **Factory** | 复杂聚合的创建逻辑 | Issue conclude batch spawn N tasks / Task create / TaskExecution create |
| **Anti-Corruption Layer (ACL)** | 跟外部系统的翻译层 | Bridge（每 vendor 一个）/ Agent CLI Adapter / BlobStore Adapter（[01 § 3.2](../design/architecture/strategic/03-bounded-contexts.md#32-anti-corruption-layers)）|
| **Shared Kernel / Customer-Supplier** | 上下文映射模式 | 见 [01 § 3.1](../design/architecture/strategic/03-bounded-contexts.md#31-上下游关系一览) |

### § 0.3 自检清单

- 我引入的新概念，**有没有恰当归类到上述某个 DDD 范畴**？没有的话，是真新概念还是该归到某个已有类别？
- 我用的术语，是不是 [01 § 1.1 通用语言表](../design/architecture/strategic/03-bounded-contexts.md#-1-通用语言ubiquitous-language)里的同一个词？还是无意中造了同义词（Brain ≠ Supervisor / Session ≠ TaskExecution / Card ≠ Message 等）？
- 跨聚合写操作，符合 [ADR-0014 § 2](../design/decisions/0014-event-sourcing-level.md) 的同事务原则吗？跨聚合**强引用**（如 task.conversation_id）vs **弱关联**（如 issue.related_conversation_ids）的选择有依据吗？
- 我引入的值对象是不是有"由属性定义、不可变、可替换"特征？是的话明示为 VO，不要散成"几个字段"

### § 0.4 不接受的措辞

- "事件" → 必须区分 **Domain Event**（领域事件，进 events 表）vs **AgentTraceEvent**（agent JSONL 内的事件，不进 events 表，见 [ADR-0015](../design/decisions/0015-agent-trace-not-in-events-table.md)）
- "对象" / "数据" / "记录" → 模糊；明示 Entity / Value Object / Aggregate / Projection / Event
- "调用 / RPC / 接口" → 跨聚合或跨 BC 时明示是 **Customer-Supplier** / **Shared Kernel** / **ACL** 哪种模式
- "中央 / 大脑 / 总管" → Supervisor，**不是** Brain（[ADR-0003](../design/decisions/0003-supervisor-not-brain.md)）

## § 1. 单一来源 / 无野任务

**Center 是任务的唯一权威**。Task 只能由 supervisor 或用户创建。Worker / Agent **不允许造任务**，它们想要新工作必须开 [Issue](../design/architecture/tactical/discussion/01-issue-discussion.md) 走讨论。

**自检：** 我引入的新动作 / 接口，会不会让某个 worker / agent 绕过中心创建任务？会的话，要么改为开 Issue，要么本设计有问题。

## § 2. 可观测性优先 (Observability First)

可观测性是一级需求。**任何新功能的设计阶段必须同时设计其可观测能力。** "先做功能、事后补观测"不被接受。

**自检（设计阶段必答）：**

| 问题 | 必须回答 |
|---|---|
| 这个功能产生哪些 **domain event**？字段是什么？ | 列清单 |
| 这些事件如何落到 `events` 表？是否会重放？ | 说明 |
| 是否引入新的 **trace 维度**（agent 视角 / supervisor 视角 / worker 视角）？ | 是 / 否 + 说明 |
| `inspect` / `ps` / `stats` 三个查询接口是否要扩展？ | 是 / 否 + 列变更 |
| 异常路径如何被发现？（fail 事件？timeout？外部告警？） | 说明 |
| 用户从飞书侧能问到这个功能的状态吗？ | 是 / 否 |

详情见 [架构层 / 可观测性](../design/architecture/tactical/observability/01-observability.md)。

### § 2.x 观测层是统一且 opinionated 的

Agent-center 的观测层（`events` 表 / fleet view / `inspect` 命令格式 / domain event 类型）是**统一**的，不允许 per-project 自定义。

- **项目特有的度量**应留在**项目自身**（项目仓库的 README / metric 文件 / 自有可观测工具链）
- **Agent-center 提供规范 / 扩展点**（如标准 event 类型、agent 可发的 metric event），项目按规范对接
- **不允许**为某个项目特殊需求侵入 agent-center 观测层代码

这条原则确保 fleet view 跨项目可比，inspect / stats 命令保持统一接口。

## § 3. AI Native

工具暴露给 agent 走 **skill + CLI**，**不引入 MCP** —— 见 [ADR-0001](../design/decisions/0001-no-mcp.md)。

新增 agent 可用能力的标准流程：

1. 在对应 skill 文件（`supervisor.md` / `worker-agent.md`）里描述：何时该用、参数语义、返回格式
2. 在 `agent-center` 二进制加 CLI 子命令
3. CLI 检测运行上下文（server 同机 / worker 内 / 远程）自动选 transport

**自检：** 新能力是否同时具备 skill 文档 + CLI 子命令？skill 是否清楚地讲明"agent 在什么场景应该调它"？

## § 4. 零 LLM SDK 依赖

**严禁 `import` 任何 LLM 厂商 SDK**（anthropic / openai / gemini 等）。LLM 能力一律通过 spawn agent CLI 实现 —— 见 [ADR-0002](../design/decisions/0002-no-llm-sdk-use-cli-agents.md)。

**自检：** 代码改动是否引入了任何 LLM SDK 依赖？是的话立即改。

## § 5. 文档先行 / 决策必有 ADR

任何设计 / 决策 / 接口提议 **先到文档**，再讨论；不在聊天 / Issue / PR 描述里口头确认设计。

架构 / 实现的"分叉路口"决定必须写 ADR。推翻先前决定 → 写新 ADR 并把旧 ADR 标 `Superseded by ADR-NNNN`，**不删旧 ADR**。

具体流程见 [文档管理规则](documentation.md)。

**自检：** 我做了哪些"多选一"决定？这些决定有 ADR 吗？

## § 6. 范围决策两分（出范围 vs 推迟）

**永远不在职责里** 的（边界决策）放 [`requirements/03-out-of-scope.md`](../design/requirements/03-out-of-scope.md)。
**v1 不做但早晚要做** 的（节奏决策）放 [`roadmap.md`](../design/roadmap.md) 或对应 architecture 文档的"未来扩展"小节。

混在一起是违反规约。详见 [文档管理规则 § 4](documentation.md#4-范围决策的两种区分)。

**自检：** 我新增的"不做"条目，归属边界还是节奏？写对位置了吗？

## § 7. 项目本地约定不进 agent-center

项目自有的 `CLAUDE.md` / `AGENTS.md` / `README.md` 等约定文件**留在项目仓库**，由 agent CLI 在 worktree cwd 自然加载。agent-center **不复制、不缓存、不注入** —— 见 [ADR-0005](../design/decisions/0005-project-charter-stays-in-project-repo.md)。

**自检：** 我引入的新功能是否在 agent-center 里维护了"项目级文档/规则"？如果是，是否能改为"读项目本地文件"或"通过 supervisor working memory 的 project scope"？

## § 8. 大文件走 BlobStore

DB **不存** markdown / 日志 / trace 这类大内容。统一通过 BlobStore 抽象，DB 只存**相对路径** —— 见 [ADR-0006](../design/decisions/0006-blob-store-for-large-content.md) 与 [implementation/01-blob-store.md](../design/implementation/01-blob-store.md)。

**自检：** 我新增的字段，内容会超过 ~10KB 吗？会的话走 BlobStore。

## § 9. 持久化层 dialect-agnostic

v1 默认嵌入式 **SQLite**（单文件、零运维）。未来可能切 **Postgres**（多 center / 高并发 / 强约束需求），写代码时**贴 SQL 共同子集**，不踩任何一家方言独占的特性。理由见 [NF3](../design/requirements/02-non-functional.md)。

> Worker 端不使用 SQLite，改用 per-execution 目录承载本机状态，见 [ADR-0018](../design/decisions/0018-detached-agent-via-per-execution-shim.md)。

### § 9.0 禁忌清单 + 替代方案

| 维度 | 禁用 | 替代 |
|---|---|---|
| 自增主键 | SQLite `AUTOINCREMENT` / PG `SERIAL` / `IDENTITY` | 应用层生成 **ULID / UUID** 字符串当主键 |
| Boolean | PG `BOOLEAN` | `INTEGER` 0/1 |
| 时间戳 | PG `TIMESTAMPTZ` / SQLite 内置 datetime | `TEXT` 存 ISO8601（UTC，`2026-05-17T10:00:00Z`） |
| JSON | PG `JSONB` + GIN 索引 / SQLite `json_extract` 索引 | `TEXT` 存 JSON 字符串，应用层 marshal；**不在 SQL 里查 JSON 内容** |
| 行级锁 | PG `SELECT ... FOR UPDATE` / SQLite `BEGIN IMMEDIATE` | **乐观锁**：version 列 + `UPDATE ... WHERE version=? `，CAS 失败重试 |
| 队列消费原语 | PG `SKIP LOCKED` | events 表 + 应用层游标 / 状态机 |
| 表特性 | SQLite `WITHOUT ROWID` / 动态类型 ; PG 数组 / enum / 范围类型 / `LATERAL` / 触发器 / 物化视图 | 普通表 + 普通列；enum 用 `TEXT` + 应用层校验 |
| Upsert | — | 两边都支持 `INSERT ... ON CONFLICT(...) DO UPDATE`，可用 |
| 全文搜索 | SQLite FTS5 / PG `tsvector` | v1 不上 FTS；需要时单独 ADR 评估 |

### § 9.1 Schema migration

- 只做 **additive migration**（加列 / 加表 / 加索引），不依赖 `ALTER TABLE` 复杂能力（SQLite 受限）
- 删列 / 改类型 / 改约束 → 新表 + copy + rename 的两步迁移
- 一份 migration SQL 必须**同时在 SQLite 和 PG 上跑通**；类型映射差异在 migration 工具层抽象，业务 query 不分叉

### § 9.2 CI 双引擎跑

测试套件（含 repository / migration / 任何写过 SQL 的代码）必须**同时在 SQLite 和 PG 上跑**。CI 默认双 job 并行，缺一不可。否则方言泄漏会悄无声息地积累，到真要切 PG 那天爆炸。

### § 9.3 不依赖 dialect 的存储抽象边界

dialect-agnostic 只承诺"**SQL 引擎之间**可切"，**不**承诺可换到 KV / 文件 / 文档库。需要换存储种类的场景（如 Memory file repo）请独立 ADR 论证，不要塞进 repository 层抽象。

**自检：**
- 我的 schema 改动在 SQLite 和 Postgres 上都能跑吗？
- CI 是不是真的两个引擎都跑了？
- 我有没有用 § 9.0 禁忌清单里的特性？用了的话替代方案是什么？

## § 9.x DB schema 尽量减少 JOIN 操作

简单的 list-of-id references → 用**主表上的 JSON 数组字段**（如 `issue.related_conversation_ids`）。

复杂 N:N（含富元数据 / 频繁双向查询 / 关系本身有领域含义）→ 才用**独立实体子聚合**（不是纯 join 表）。

避免：仅为关联两个实体而存在的"纯 join 表"（如 `issue_conversation_join (issue_id, conversation_id)` 无任何额外字段）。

**自检：** 我新增的关联是不是只是 list-of-id？是的话用 JSON 字段；不是的话有没有真正的领域含义？

## § 9.y 外部集成走 Bridge 模式，不在领域模块内调 vendor SDK

领域模块（Issue / Conversation / Task / 等）是**系统内部模块**，**不直接调用** 飞书 / DingTalk / 任何 vendor 的 SDK 或 API。

外部集成通过独立的 **Bridge 程序**实现：

- **Outbound（系统 → vendor）**：Bridge 订阅领域事件 → 推到 vendor
- **Inbound（vendor → 系统）**：Bridge 收 vendor 回调 → 写到领域模块的 API
- 每个 vendor 一个 Bridge 实现（FeishuBridge / DingTalkBridge / ...）

这样领域模块零 vendor 依赖，可独立测试、可独立运行（无 Bridge 时系统仍跑，只是没人能从 vendor 侧 IO）。

**自检：** 我的领域模块代码里 import 了 vendor SDK 吗？把它移到 bridge 包里。

## § 10. 单一二进制 / 多模式

所有功能聚合到 `agent-center` 一个 binary，按子命令区分模式（`server` / `supervisor` / `worker` / 各 CLI 命令）。**不引入额外可执行文件**。

**自检：** 我的功能是否需要新增一个独立 binary？能否改为新的子命令？

## § 11. Issue / InputRequest / 状态事件 三者分清

不同交互场景走不同渠道，不要混用：

| 场景 | 渠道 |
|---|---|
| 议题、要讨论、可能后续产生多个 Task | **Issue**（非阻塞，多轮）|
| 任务执行中卡住要拍板，等不动 | **InputRequest**（同步阻塞）|
| Worker / Task 状态机推进、心跳、产物上报 | **Domain Event**（异步事件流）|

**自检：** 我设计的新交互，走的是哪条渠道？为什么不是另两条？

## § 12. 命名一致

| 概念 | 用 | 不用 |
|---|---|---|
| 调度官 agent | **Supervisor** | ~~Brain~~（见 [ADR-0003](../design/decisions/0003-supervisor-not-brain.md)）|
| 待讨论的事 | **Issue** | ~~Suggestion / Proposal~~（见 [ADR-0004](../design/decisions/0004-issue-not-suggestion.md)）|
| 工作单元 | **Task** | ~~Job / Work~~ |
| 用户开发机的守护进程 | **Worker daemon** | ~~Agent / Worker（指 host 本身）~~ |
| Worker 上跑的 CLI agent 实例 | **Agent** 或 **Worker agent** | ~~Worker~~（避免歧义）|
| 持久脑 | **(supervisor) memory** | ~~knowledge / brain~~ |
| 任务隔离的目录 | **Worktree** | ~~workspace / sandbox~~ |
| 大文件存储抽象 | **BlobStore** | ~~ObjectStore / FileStore~~ |
| 飞书交互卡片 | **LarkCard**（代码 / CLI / 字段名）| ~~Card~~（裸 Card 在多上下文里有歧义） |

代码、文档、CLI、表名、event_type 字符串等**一律遵守**。

**自检：** 我用的命名跟此表一致吗？

## § 13. 安全 / 隐私（v1 简化）

| 项 | 当前要求 |
|---|---|
| Worker ↔ Center 认证 | bootstrap token + session token |
| Feishu ↔ Center 认证 | SDK 内部凭 App ID + App Secret（一键创建签发） |
| Agent ↔ Worker daemon 通信 | 本机 unix socket，同机进程信任 |
| Admin CLI | 本机 unix socket / 同机用户身份；远程访问留口未实现 |
| 用户数据（飞书消息内容、agent 输出） | 仅在 agent-center 内流转，不外发；BlobStore 内容遵从存储后端的访问控制 |

**自检：** 我的功能是否引入新的外部通信？凭据怎么管？数据是否会泄露到不该到的地方？

## § 14. 测试

测试规约展开在独立文档 [`testing.md`](testing.md)。本节只保留**纲领**与**自检**，细则去 testing.md。

核心硬约束：

- **单元测试行覆盖率 ≥ 90%**（整体 + diff），CI 阻断 < 90% 的合并
- **每次测试必须有测试计划 + 测试报告**，条目编号 1:1 对齐，**逐条确认**通过 / 失败 / 跳过 + 理由
- **关键路径 e2e**：`worker enroll → dispatch → 任务结束`、`feishu 入口 → supervisor → task → 回流`
- **BlobStore 契约测试**：local 实现 / S3 mock 实现走同一份契约
- 设计 / 实现侧的可测性硬约束见下面 [§ 14.x](#-14x-设计与实现必须可测)

**自检：** 我的改动是否同时附了测试计划 + 测试报告？单元行覆盖率（整体 + diff）是否 ≥ 90%？关键路径有 e2e 吗？

### § 14.x 设计与实现必须可测

测试不是事后补的工序，**设计阶段与实现阶段都必须把"可测试"作为硬约束**。任何让测试只能靠"真连外部服务 / sleep / 真实超时巧合"才能跑通的设计，都视为设计缺陷。

**设计阶段必答：**

| 问题 | 必须回答 |
|---|---|
| 这个功能的外部依赖（DB / blob store / 飞书 SDK / 时钟 / 随机数 / 子进程 / 网络）有哪些？ | 列清单 |
| 每个外部依赖能否在测试中**替换为可控实现**（interface / port / 注入）？ | 是 / 否 + 说明 |
| 每类**异常路径**（timeout / 5xx / 网络中断 / 状态不一致 / 部分失败）能否**显式 mock 注入**？ | 是 / 否 + 说明 |
| 时间 / 随机 / 并发等非确定性来源如何注入？ | 说明 |
| 关键事件 / 副作用如何被测试**断言**到（事件流 / 显式同步原语，不靠 sleep）？ | 说明 |

**实现要求：**

- 业务逻辑**不直接持有具体依赖**；通过构造函数 / 参数 / 接口注入
- 凡是有 IO / 时钟 / 随机的位置，必须留出注入点
- "只能在生产环境复现的异常"是反模式 —— 必须把触发条件抽象到接口，让测试能主动制造
- 不允许给测试加 `sleep` 之类的等待 —— 改用可注入时钟或显式同步

**自检：** 我的接口能不能在不连真实外部服务的情况下覆盖所有正常+异常路径？如果不能，先改设计再写实现。

## § 15. 新功能 / 新设计自检清单

提交设计文档 / PR 前，逐条过：

- [ ] 单一来源（§1）：没有引入"绕过 center 造任务"的路径
- [ ] 可观测性（§2）：列出新 events、说明 trace 影响、扩展 inspect / ps / stats 的项
- [ ] AI Native（§3）：新 agent 能力同时有 skill + CLI，不走 MCP
- [ ] 零 LLM SDK（§4）：没新增 LLM 厂商 SDK 依赖
- [ ] 文档先行（§5）：设计落到 `docs/design/` 对应层；分叉决定有 ADR
- [ ] DDD 统一语言（§0）：新引入的术语 / 概念已归类到 DDD 范畴（Aggregate / Entity / VO / Domain Event / ...）；用词跟 [01 § 1.1](../design/architecture/strategic/03-bounded-contexts.md#-1-通用语言ubiquitous-language) 一致，没造同义词
- [ ] 范围决策（§6）：新增的"不做"分到出范围 / 推迟正确位置
- [ ] 项目本地约定（§7）：不在 agent-center 里管项目自有的规则文档
- [ ] BlobStore（§8）：大字段走 BlobStore
- [ ] dialect-agnostic（§9）：SQL 跨 SQLite / PG 兼容
- [ ] 单一二进制（§10）：没新增独立 binary
- [ ] 渠道选对（§11）：明确走 Issue / InputRequest / Event 哪一条
- [ ] 命名（§12）：遵循术语表
- [ ] 安全（§13）：凭据 / 数据流转无新风险
- [ ] 测试（§14）：单元行覆盖 ≥ 90%（整体 + diff）；测试计划 + 报告齐全且条目 1:1 对齐；关键路径 e2e；详情见 [testing.md](testing.md)
- [ ] 可测性（§14.x）：外部依赖可注入、异常路径可 mock、无 sleep / 真连外部服务
- [ ] reason + message 双字段（§16）：所有带 `reason` 的事件 / 字段同时携带 `message`

任何一项 ❌ → 退回修改。

## § 16. 错误 / 状态信息双字段（reason + message）

凡是携带 `reason` 枚举的**事件 payload / RPC 响应 / 状态字段**，必须**同时**携带 `message` 字段。

| 字段 | 用途 |
|---|---|
| `reason` | **机器可读**枚举值，supervisor / center 据此路由决策（如 `worker_lost` / `dispatch_no_ack` / `worktree_path_busy`）|
| `message` | **人类可读**详细信息，落 inspect / 飞书卡片 / log（如 `"last heartbeat 2026-05-16T10:23 UTC"`） |

**适用场景**：

- `task_execution.failed { reason, message }`
- `task_execution.killed { reason, message }`
- `task_execution.kill_requested { reason, message }`
- `input_request.timed_out { reason, message }`
- `input_request.canceled { reason, message }`
- `DispatchNack { reason, message }`
- `worker.offline { reason, message }`
- 任何带"为什么"的状态终结 / 异常

**不需要**：

- 纯动作事件（如 `task.dependency_added` —— 没有"为什么"）
- 正常状态机迁移（如 `task_execution.working`）

**自检：** 我新加的字段 / 事件里有 `reason` 吗？是的话配 `message` 了吗？
