# 架构层

回答"概念怎么组织、组件怎么交互"。具体表结构 / CLI 签名归 [implementation/](../implementation/)；"为什么这么定"归 [decisions/](../decisions/)。

> **⭐ 首读两件套**（DDD 战略层）：
> 1. [**领域愿景 / Domain Vision**](00-domain-vision.md) —— 系统的根本目的 + 关键策略立场 + 核心边界
> 2. [**子域分类 / Subdomain Classification**](00-subdomain-classification.md) —— 8 个 BC 的 Core / Supporting / Generic 标注 + 投入策略
>
> 两者均是领域视角，区别于下面 # 00 系统总览（工程视角）。

## 文档清单

| # | 主题 | 状态 |
|---|---|---|
| 00 | [系统总览（拓扑 / 组件）](00-system-overview.md) | Draft |
| 01 | [限界上下文 & Ubiquitous Language](01-bounded-contexts.md) | Draft |
| 02 | [Task 模型 & 状态机](02-task-model.md) | Draft |
| 03 | [Issue 讨论模型](03-issue-discussion.md) | Draft |
| 04 | [InputRequired 协议](04-input-required.md) | Draft |
| 05 | [可观测性 & Fleet View](05-observability.md) | Draft |
| 06 | [Supervisor 运行模型](06-supervisor-model.md) | Draft |
| 07 | [Worker 执行模型](07-worker-model.md) | TBD |
| 08 | [Prompt 组装](08-prompt-assembly.md) | Draft |
| 09 | [飞书集成](09-feishu-integration.md) | TBD |
| 10 | [Skill + CLI 工具链](10-skill-cli-tooling.md) | Draft |
| 11 | [Web Console](11-web-console.md) | Draft |
| 12 | [Conversation 统一会话层](12-conversation.md) | Draft |

**约定：** 文档内容遵循 [文档管理规则](../../rules/documentation.md)。架构层不写具体表 schema / CLI 签名。
