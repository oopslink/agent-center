# Session Checkpoint — 2026-05-16

> 本文件是 brainstorming session 切换的接力棒。新 session 启动后**先读这个**。
> 不属于正式设计文档（按 [conventions § 3](../rules/conventions.md) 放 `drafts/`）。

---

## 1. 新 session 启动协议

按顺序读以下文件，然后回到本 checkpoint 继续：

1. [`CLAUDE.md`](../../CLAUDE.md)（项目入口）
2. [`docs/rules/conventions.md`](../rules/conventions.md)（**MUST-READ** 项目规约 + 自检清单）
3. [`docs/rules/documentation.md`](../rules/documentation.md)（文档管理规则 + ADR 格式 + 出范围 vs 推迟）
4. [`docs/design/README.md`](../design/README.md)（设计文档总入口）
5. [`docs/design/architecture/README.md`](../design/architecture/README.md)（架构层 12 个章节状态）
6. [`docs/design/architecture/01-bounded-contexts.md`](../design/architecture/01-bounded-contexts.md)（**通用语言 + 8 个 BC 地图**，重点）
7. [`docs/design/decisions/README.md`](../design/decisions/README.md)（9 个 ADR 索引）
8. **本 checkpoint**（确认当前进度 + 下一步）

---

## 2. 当前进度（已完成的设计）

### Requirements 层（4 文档全完整）

- ✅ [`00-overview.md`](../design/requirements/00-overview.md) — 目标 / 边界 / CRUD 重定义
- ✅ [`01-functional.md`](../design/requirements/01-functional.md) — F1-F22 + F23（worker proposal）
- ✅ [`02-non-functional.md`](../design/requirements/02-non-functional.md) — NF1-NF16
- ✅ [`03-out-of-scope.md`](../design/requirements/03-out-of-scope.md) — 4 条边界决策 + ADR 索引
- ✅ [`04-assumptions.md`](../design/requirements/04-assumptions.md) — A1-A5

### Architecture 层（12 章，状态对照）

| # | 文件 | 状态 |
|---|---|---|
| 00 | system-overview | Draft（拓扑图） |
| 01 | bounded-contexts | **Draft（完整：8 BC + UL + Context Map + 命名）** |
| 02 | **task-model** | **TBD —— 这是下一步要讨论的（§ 3）** |
| 03 | issue-discussion | Draft（已对齐 Bridge 模型 + bound card 机制） |
| 04 | input-required | Draft |
| 05 | observability | Draft |
| 06 | supervisor-model | TBD-partial（基础有，剩余细节待补） |
| 07 | worker-model | TBD-partial（已有 WorkerProjectMapping 自动发现机制） |
| 08 | prompt-assembly | Draft |
| 09 | feishu-integration | Draft（已改为 FeishuBridge 实现） |
| 10 | skill-cli-tooling | Draft |
| 11 | web-console | Draft（占位） |
| 12 | conversation | Draft（独立 BC + Bridge 模型） |

### Implementation 层

- ✅ [`01-blob-store.md`](../design/implementation/01-blob-store.md) — Draft
- 待写：02-persistence-schema / 03-cli-subcommands / 04-configuration / 05-agent-adapters / 06-deployment

### Decisions（ADR，9 条）

| # | 标题 | 状态 |
|---|---|---|
| 0001 | 不引入 MCP | Accepted |
| 0002 | 不用 LLM SDK，走 CLI agent | Accepted |
| 0003 | Supervisor 不是 Brain | Accepted |
| 0004 | Issue 取代 Suggestion | Accepted |
| 0005 | 项目宪章留在项目仓库 | Accepted |
| 0006 | 大文件走 BlobStore | Accepted |
| 0007 | 引入 Conversation 层 | Accepted (Refined by 0009) |
| 0008 | WorkerProjectMapping 自动发现 + 用户确认 | Accepted |
| 0009 | Issue 与 Conversation 解耦 + 外部集成走 Bridge | Accepted |

### Rules

- ✅ [`conventions.md`](../rules/conventions.md) — 14 条跨切原则 + § 15 自检清单（含 § 14 测试规约外链 + § 9.x DB 减少 JOIN + § 9.y Bridge 模式）
- ✅ [`documentation.md`](../rules/documentation.md) — 文档组织 / ADR 格式 / 出范围 vs 推迟
- 缺：testing.md（[CLAUDE.md](../../CLAUDE.md) 与 conventions.md 已引用但文件未写，**新 session 不在本次接力范围**）

### Roadmap

- ✅ [`roadmap.md`](../design/roadmap.md) — 11 项推迟功能按 v2 / v3 / 长期 分组

---

## 3. 当前讨论中的话题

**§ 3 Task 模型 + A2A 状态机细化** —— 目标文件 [`architecture/02-task-model.md`](../design/architecture/02-task-model.md)（目前是 TBD 占位）

### 已经定的（散在各处）

| 项 | 内容 |
|---|---|
| 任务唯一权威 | 只能 center 创建（[conventions § 1](../rules/conventions.md)） |
| 归属 | 任务永远绑一个 project；无散单 |
| 状态机骨架 | submitted / working / input_required / completed / failed / canceled（借鉴 A2A） |
| 子任务层级 | 加 `parent_task_id`；v1 不做父子状态联动（F22） |
| 来源血缘 | `from_issue_id` 关联到产生此 task 的 Issue |
| 删除 | 不允许；只能 cancel / fail / archive |
| 隔离 | 每个 task 一个 worktree（动态） |
| 派单组装 | Worker daemon 拼 prompt；Supervisor 发 envelope |
| 观测 | 全程进 events 表 + 实时投影 |
| 日志归档 | 任务结束打包到 BlobStore |

### 待决策清单（Q1-Q10）

**Q1. A2A 状态机的精确语义**

每个状态的精确含义 + 转换条件 + 谁触发：

```
submitted → working          : 谁触发?（worker ACK? supervisor 派完即转?）
working   → input_required   : worker 调 request-input 时
input_required → working     : InputResponse 回来后；谁负责标？
working   → completed/failed : agent 退出时由 worker 报；如何区分成败？
任何状态  → canceled         : 谁能 cancel?
```

**Q2. AgentSession 数量**

一个 Task 跑过程中可有几个 AgentSession？
- A. 严格 1:1 ← **我推 A**（v1 简洁）
- B. 1:N（task 可重试 / 续跑）

**Q3. 重试策略**

`failed` 后默认怎么办？
- A. 不自动重试，等用户 / supervisor 决定 ← **我推 A**
- B. 有限自动重试
- C. 配置化

**Q4. Timeout**

Task 跑多久算"挂死"？
- A. 无 timeout
- B. 全局 default（6h）+ 任务级 override
- C. 跟 worker heartbeat 关联
- **我推 B + C 组合**

**Q5. Cancel 语义**

谁能 cancel？agent 怎么处理？
- 谁：user / supervisor / parent task（v1 不做 cascade）
- agent 处理：SIGTERM → 5s → SIGKILL；worktree 保留
- 已产出部分是否上传

**Q6. 优先级 / 排队**

Task 有 priority 吗？
- A. 无 priority，FIFO ← **我推 A**
- B. 数字 priority
- C. 标签优先级

**Q7. Artifact 模型**

Task 产物如何表达？
- A. 自由 JSON 字段 `task.artifacts_json` ← **我推 A**
- B. 结构化表 `task_artifacts`
- C. 只靠 events 推断

**Q8. Task ↔ Worktree 可选性**

每个 task 都必须有 worktree 吗？
- 编程 task → 有
- "总结昨天发生啥" → 不需要 worktree？
- v1 可选两种：必有 OR 可选（`task.requires_worktree=false`）

**Q9. Dispatch envelope 完整字段**

派单时给 worker 的完整 envelope schema 细节。

**Q10. Cross-BC 交互细节**

- Discussion BC 怎么"spawn tasks"？一次性批量还是逐个？
- Cognition BC 用什么工具 query task 状态？
- Observability 怎么从 events 还原 task 时间线？

### 建议讨论顺序

1. Q1（状态机精确语义）—— 一切的基础
2. Q2（AgentSession 数量）—— 紧密耦合
3. Q5（Cancel 语义）—— 状态机一部分
4. Q3（重试）—— 跟 fail 相关
5. Q4（Timeout）—— 跟 working 相关
6. Q6 / Q7 / Q8 / Q9 / Q10 —— 其它

---

## 4. 上一个 session 刚结束的状态

- 用户拍板**保留方案 X**（Discussion 与 Conversation 两个 BC 分开，不合并）
- 询问了 BC = Bounded Context，已解答
- 提议讨论 § 3 Task 模型，列了 Q1-Q10
- 然后切 session

**下一步：** 新 session 接力，从 Q1 开始 work through。

---

## 5. 新 session 工作流提示

- **doc-first**：所有设计变更先写文件再讨论，参见 [conventions § 5](../rules/conventions.md)
- **每个分叉决定写 ADR**：新决定续 ADR-0010+，参见 [decisions/](../design/decisions/)
- **范围决策两分**：出范围 → [03-out-of-scope.md](../design/requirements/03-out-of-scope.md)；推迟 → [roadmap.md](../design/roadmap.md)
- **当前命名约束**：见 [conventions § 12](../rules/conventions.md)（Supervisor / Issue / LarkCard / Bridge / 等术语已稳定，新 session 沿用）
- **每章节进度调整 architecture/README.md status**：TBD → Draft 等
- **完成 task-model 之后**继续 supervisor / worker / persistence schema / 等

---

## 6. 接力的开场白模板（建议给新 session 用）

> 我接手 agent-center 的 brainstorming session。已经按 `docs/drafts/session-checkpoint-2026-05-16.md` § 1 读了 CLAUDE.md / conventions.md / documentation.md / design/README.md / architecture/README.md / 01-bounded-contexts.md / decisions/README.md。
>
> 当前推进到 § 3 Task 模型，待讨论 Q1-Q10。从 Q1（A2A 状态机精确语义）开始：你的方案是 XXX，理由 XXX。

或：

> 接力 agent-center。读完 checkpoint，从 Q1 开始 — 你认为 `submitted → working` 的触发应该由谁来标？
