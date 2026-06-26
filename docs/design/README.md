# agent-center 设计文档

设计文档按职责分层，遵循 [文档管理规则](../rules/documentation.md)。

## 阅读顺序

1. **[requirements/](requirements/)** — 先看需求层，了解项目目标和范围
2. **[architecture/](architecture/)** — 再看架构层，理解概念和组件关系
3. **[implementation/](implementation/)** — 需要落地细节时看实现层
4. **[decisions/](decisions/)** — 想知道"为什么这么定"时查 ADR

## 各层入口

| 层 | 入口 |
|---|---|
| 需求 | [requirements/00-overview.md](requirements/00-overview.md) |
| 架构 | [architecture/README.md](architecture/README.md) |
| 实现 | [implementation/README.md](implementation/README.md) |
| 决策 | [decisions/README.md](decisions/README.md) |
| 功能设计 | [features/](features/) — 版本迭代的功能设计（UX 研究、接口契约等）|
| 部署方案 | [deployment/](deployment/) — 单机 / 多机部署指南 |
| 迁移方案 | [migration/](migration/) — 跨版本迁移指南 |
| 运维 | [operations/](operations/) — master-key 等运维操作 |
| 路线图 | [roadmap.md](roadmap.md) — 推迟做的功能（节奏决策）|
| DDD 蓝图 | [ddd-blueprint.md](ddd-blueprint.md) — DDD 设计推进的 plan & status（哪些做了 / 没做 / 下一步）|

**范围决策的两类**：
- **出范围**（永远不做）→ [requirements/03-out-of-scope.md](requirements/03-out-of-scope.md)
- **推迟**（v1 不做，但早晚要做）→ [roadmap.md](roadmap.md)

规则见 [conventions § 6](../rules/conventions.md#-6-范围决策两分出范围-vs-推迟)。

其它目录：
- [drafts/](drafts/) — 临时草稿
- [assets/](assets/) — 设计资源（mockup 等）
- [retired/](retired/) — 废弃设计

## 写作规则

见 [docs/rules/documentation.md](../rules/documentation.md)。**先文档后讨论；不允许混层；ADR 编号严格递增**。

## 设计方法论

**项目使用 DDD（Domain-Driven Design）**。所有文档 / 讨论 / 代码必须用 DDD 统一语言。

- 方法论约定：[conventions § 0](../rules/conventions.md#-0-设计方法论ddd--统一语言)
- 通用语言权威表：[architecture/01-bounded-contexts.md § 1](architecture/strategic/03-bounded-contexts.md#-1-通用语言ubiquitous-language)
- DDD 推进 plan & status：[ddd-blueprint.md](ddd-blueprint.md)（讨论 DDD 设计前先过一遍）
