# 架构层

回答"概念怎么组织、组件怎么交互"。具体表结构 / CLI 签名归 [implementation/](../implementation/)；"为什么这么定"归 [decisions/](../decisions/)。

按 DDD 分层组织。`tactical/` 的子目录有两种合法形态：**BC 子目录**（领域自治）与 **主题子目录**（跨 BC 的工程主题）。

```
architecture/
├── strategic/        # DDD 战略层（域愿景 / 子域分类 / 限界上下文 / 系统总览）
└── tactical/         # DDD 战术层
    ├── workforce/         # BC3: 工作池
    ├── cognition/         # BC4: 认知 / Supervisor
    ├── observability/     # BC5: 观测
    ├── conversation/      # BC6: 会话
    ├── secret-management/ # BC8: 用户密钥管理（v2 新增；ADR-0026）
    ├── agent-harness/     # 主题: agent 运行时支撑（prompt 组装 + skill/CLI 工具）
    └── presentation/      # 主题: 表现层 / 前端（Web Console、未来 Mobile / Desktop）
```

每份文档头部应有 layer label，便于读者一眼定位归属（如 `> DDD 战略层` / `> DDD 战术层 | BC: TaskRuntime` / `> DDD 战术层 · 主题: agent-harness`）。

---

## 战略层（首读）

DDD 战略设计：从整体看系统怎么切。

| # | 主题 | 状态 |
|---|---|---|
| 00 | [领域愿景 / Domain Vision](strategic/00-domain-vision.md) ⭐ | Accepted |
| 01 | [子域分类 / Subdomain Classification](strategic/01-subdomain-classification.md) ⭐ | Accepted |
| 02 | [系统总览（拓扑 / 组件）](strategic/02-system-overview.md) | Draft |
| 03 | [限界上下文 & Ubiquitous Language](strategic/03-bounded-contexts.md) | Draft |

> ⭐ 首读两件套（领域视角，区别于 # 02 系统总览的工程视角）。

---

## 战术层（按 BC）

DDD 战术设计：每个 BC 内部的聚合 / 实体 / VO / Invariants / Domain Service / Factory / Repository。

### TaskRuntime（任务运行时）— BC1 — RETIRED (v2.7 #131)

> ⚠️ **RETIRED (v2.7 #131 carve-out)**：BC 已从代码库删除（code + tables removed）。职责迁移至 ProjectManager BC（pm.Task）+ Agent work-items。以下链接指向归档战术设计文档。

| # | 主题 | 状态 |
|---|---|---|
| 00 | [BC 入口 / Domain Service / Factory / Repo / 跨聚合](../retired/task-runtime/00-overview.md) | RETIRED |
| 01 | [Task 聚合](../retired/task-runtime/01-task.md) | RETIRED |
| 02 | [TaskExecution 聚合（含 worker 运行时 / Artifact）](../retired/task-runtime/02-task-execution.md) | RETIRED |
| 03 | [InputRequest 聚合](../retired/task-runtime/03-input-request.md) | RETIRED |

### Discussion（讨论）— BC2 — RETIRED (v2.7 #131)

> ⚠️ **RETIRED (v2.7 #131 carve-out)**：BC 已从代码库删除（code + tables removed）。职责迁移至 ProjectManager BC（pm.Issue）+ Agent work-items。以下链接指向归档战术设计文档。

| # | 主题 | 状态 |
|---|---|---|
| 00 | [Discussion BC Overview](../retired/discussion/00-overview.md) | RETIRED |

### Workforce（工作池）— BC3

> 原 BC4 Execution 运行时内容已 carve 到 BC1 TaskRuntime（[ADR-0019](../decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)）；本 BC 留 Worker / Project / Mapping / Proposal 元数据。

| # | 主题 | 状态 |
|---|---|---|
| 00 | [Workforce BC Overview](tactical/workforce/00-overview.md) | Draft |
| 01 | [Worker 聚合 + BootstrapToken + WorkerProjectMapping 子从属](tactical/workforce/01-worker.md) | Draft |
| 02 | [Project 聚合](tactical/workforce/02-project.md) | Draft |
| 03 | [WorkerProjectProposal 聚合](tactical/workforce/03-worker-project-proposal.md) | Draft |
| 04 | [AgentInstance 聚合](../retired/workforce/04-agent-instance.md) — 已归档（replaced by Agent BC `internal/agent/`）| Retired |

### Cognition（认知 / Supervisor）— BC4

| # | 主题 | 状态 |
|---|---|---|
| 00 | [Cognition BC Overview](tactical/cognition/00-overview.md) | Draft |
| 01 | [SupervisorInvocation 聚合](../retired/cognition/01-supervisor-invocation.md) — 已归档 per ADR-0044 | Retired |
| 02 | [Memory 聚合](tactical/cognition/02-memory.md) | Draft |

### Observability（观测）— BC5

| # | 主题 | 状态 |
|---|---|---|
| 00 | [Observability BC Overview](tactical/observability/00-overview.md) | Draft |

### Conversation（会话）— BC6

| # | 主题 | 状态 |
|---|---|---|
| 00 | [Conversation BC Overview](tactical/conversation/00-overview.md) | Draft |
| 01 | [Conversation 聚合（+ Message）](tactical/conversation/01-conversation.md) | Draft |
| 02 | [Identity 聚合（+ ChannelBinding）](tactical/conversation/02-identity.md) | Draft |

### Bridge（渠道桥接）— BC7 — RETIRED

> **v2 已撤回**（per [ADR-0031](../decisions/0031-v2-drop-bridge-vendor-integration.md)）；vendor 接入 v3+ 重新设计（[roadmap.md](../roadmap.md) v3+ AgentImage / Bridge 重设计条）。

### SecretManagement（用户密钥管理）— BC8

> v2 新增（Supporting Domain）。仅管理用户密钥（MCP env vars 等）；系统内部凭证不动。

| # | 主题 | 状态 |
|---|---|---|
| 00 | [SecretManagement BC Overview](tactical/secret-management/00-overview.md) | Draft |
| 01 | [UserSecret 聚合](tactical/secret-management/01-user-secret.md) | Draft |

## 战术层（按主题）

跨 BC 的工程主题，不专属于任何单一 BC。

### Agent Harness（agent 运行时支撑）

Agent（supervisor / worker agent）的 prompt 上下文与能力暴露机制 —— 跨 Cognition / TaskRuntime / Workforce 多 BC 共用。

| # | 主题 | 状态 |
|---|---|---|
| 01 | [Prompt 组装](tactical/agent-harness/01-prompt-assembly.md) | Draft |
| 02 | [Skill + CLI 工具链](tactical/agent-harness/02-skill-cli-tooling.md) | Draft |

### Presentation（表现层 / 前端）

用户 surface 层，呈现 Observability / TaskRuntime / Discussion 等多 BC 的数据，本身不持领域聚合。

| # | 主题 | 状态 |
|---|---|---|
| 01 | [Web Console](tactical/presentation/01-web-console.md) | Draft |

---

**约定：** 文档内容遵循 [文档管理规则](../../rules/documentation.md)。架构层不写具体表 schema / CLI 签名（归 [implementation/](../implementation/)）；"为什么这么定" 归 [decisions/](../decisions/)。
