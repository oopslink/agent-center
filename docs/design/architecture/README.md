# 架构层

回答"概念怎么组织、组件怎么交互"。具体表结构 / CLI 签名归 [implementation/](../implementation/)；"为什么这么定"归 [decisions/](../decisions/)。

按 DDD 分层组织：

```
architecture/
├── strategic/        # DDD 战略层（域愿景 / 子域分类 / 限界上下文 / 系统总览）
└── tactical/         # DDD 战术层（按 BC 分子目录 + 非 BC 跨切）
    ├── scheduling/        # BC1: 调度
    ├── discussion/        # BC2: 讨论
    ├── workforce/         # BC3: 工作池（含 Execution 运行时）
    ├── cognition/         # BC5: 认知 / Supervisor
    ├── observability/     # BC6: 观测
    ├── conversation/      # BC7: 会话
    ├── bridge/            # BC8: 渠道桥接
    └── _cross-cutting/    # 跨 BC / 非 BC（Prompt / Skill / Web Console）
```

每份文档头部应有 layer label，便于读者一眼定位归属（如 `> DDD 战略层` / `> DDD 战术层 | BC: Scheduling`）。

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

### Scheduling（调度）— BC1

| # | 主题 | 状态 |
|---|---|---|
| 01 | [Task 模型 & 状态机](tactical/scheduling/01-task-model.md) | Draft |
| 02 | [InputRequest 协议](tactical/scheduling/02-input-required.md) | Draft |

### Discussion（讨论）— BC2

| # | 主题 | 状态 |
|---|---|---|
| 01 | [Issue 讨论模型](tactical/discussion/01-issue-discussion.md) | Draft |

### Workforce（工作池 + Execution 运行时）— BC3 + BC4

| # | 主题 | 状态 |
|---|---|---|
| 01 | [Worker 执行模型](tactical/workforce/01-worker-model.md) | TBD |

> Execution BC 现阶段跟 Workforce 共享文档；未来拆出时建 `tactical/execution/` 子目录。

### Cognition（认知 / Supervisor）— BC5

| # | 主题 | 状态 |
|---|---|---|
| 01 | [Supervisor 运行模型](tactical/cognition/01-supervisor-model.md) | Draft |

### Observability（观测）— BC6

| # | 主题 | 状态 |
|---|---|---|
| 01 | [可观测性 & Fleet View](tactical/observability/01-observability.md) | Draft |

### Conversation（会话）— BC7

| # | 主题 | 状态 |
|---|---|---|
| 01 | [Conversation 统一会话层](tactical/conversation/01-conversation.md) | Draft |

### Bridge（渠道桥接）— BC8

| # | 主题 | 状态 |
|---|---|---|
| 01 | [飞书集成](tactical/bridge/01-feishu-integration.md) | TBD |

### 跨切 / 非 BC

| # | 主题 | 状态 |
|---|---|---|
| 01 | [Prompt 组装](tactical/_cross-cutting/01-prompt-assembly.md) | Draft |
| 02 | [Skill + CLI 工具链](tactical/_cross-cutting/02-skill-cli-tooling.md) | Draft |
| 03 | [Web Console](tactical/_cross-cutting/03-web-console.md) | Draft |

---

**约定：** 文档内容遵循 [文档管理规则](../../rules/documentation.md)。架构层不写具体表 schema / CLI 签名（归 [implementation/](../implementation/)）；"为什么这么定" 归 [decisions/](../decisions/)。
