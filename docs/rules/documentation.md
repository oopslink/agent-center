# 文档管理规则

本规则约束 agent-center 项目所有设计 / 决策 / 需求文档的组织、写法和维护方式。

## 1. 目录与分层

`docs/` 按四类内容组织：

| 类别 | 目录 | 说明 |
|---|---|---|
| 项目规约 | `docs/rules/` | conventions / 文档管理 / 测试规约 / 发布流程 / 验收规范 / 设计系统 |
| 系统设计 | `docs/design/` | 需求 / 架构 / 实现 / ADR / 功能设计 / 部署 / 迁移 / 运维 |
| 发布产物 | `docs/releases/` | 按版本归档的 release notes / 验收报告 / 截图证据 |
| 专项计划 | `docs/plans/` | 实现计划 / 审计 / 测试报告（已完成的归 `archived/`）|

设计文档（`docs/design/`）按职责分层：

| 层 | 目录 | 回答 |
|---|---|---|
| 需求 | `docs/design/requirements/` | 做什么 / 不做什么 |
| 架构 | `docs/design/architecture/` | 概念怎么组织、组件怎么交互 |
| 实现 | `docs/design/implementation/` | 代码侧具体怎么落（表 schema / CLI / 配置）|
| ADR | `docs/design/decisions/` | 为什么这么定，备选方案为何不采 |
| 功能设计 | `docs/design/features/` | 版本迭代的功能设计（UX 研究、接口契约等）|

**不允许混层**：一个文档不要同时写需求 + 架构 + 实现。各自有自己的语言、读者、变更频率。

## 2. 哪个层写什么

| 层 | 允许出现 | 不该出现 |
|---|---|---|
| requirements | 业务目标、用户、F/NF 需求清单、out-of-scope、假设 | 技术选型、表结构、代码 |
| architecture | 限界上下文、聚合、领域事件、组件交互、流程图、状态机 | 具体表结构、CLI 签名、代码 |
| implementation | 表 schema、配置示例、CLI 完整签名、目录布局 | 业务理由、宽泛动机 |
| decisions | Context / Decision / Consequences / Alternatives 四段 | 实现细节、整套设计稿 |

## 3. 不和其它目录混

- 设计文档不放 `docs/superpowers/`（那是 brainstorming / skill 工件目录）
- 设计文档不放仓库根（README.md / CLAUDE.md 除外）
- 临时草稿放 `docs/design/drafts/` 或本地
- 发布产物（验收报告、截图证据）放 `docs/releases/`，不放 `docs/design/`
- 专项计划放 `docs/plans/`，不放 `docs/design/`

## 4. 范围决策的两种区分

**不允许把"做不做"在文档里混在一起**。必须区分：

### 4a. 出范围（明确不做）

**边界决策。** 这件事永远不在本系统的职责里，或跟核心定位冲突。

- 存放：`requirements/03-out-of-scope.md`
- 表述风格："X 由 Y 系统负责"、"X 跟我们 [某定位] 冲突"
- 一旦写下，重新讨论需要新证据，不轻易翻案

### 4b. 推迟（本阶段不做）

**节奏决策。** 这件事**早晚要做**，只是 v1 不做。

- 存放：每个 architecture/* 或 implementation/* 文档末尾的"未来扩展 / Future Work"小节，或独立的 `docs/design/roadmap.md`
- 表述风格："v2 再做"、"等真的撞到再说"、"留扩展点 XX"
- 反映优先级和成本判断，会随实际情况调整

混淆这两类会让"是否要做"和"什么时候做"被一锅端，未来再做决策成本飙升。

## 5. ADR 格式

ADR (Architecture Decision Record) 是 `decisions/` 下的单个文件，命名 `NNNN-kebab-case-title.md`，编号严格递增、永不复用。

每个 ADR 必含：

```markdown
# NNNN. <Title>

| Field | Value |
|---|---|
| Status | Proposed / Accepted / Superseded by ADR-NNNN |
| Date | YYYY-MM-DD |

## Context
（为什么遇到这个决定，背景约束、动机）

## Decision
（决定是什么，越具体越好）

## Consequences
（采用后的后果 —— 正面、负面、需要跟进的事）

## Alternatives Considered
（评估过的其它方案，简要说明为何不采）
```

何时写 ADR：

- 架构或实现的"分叉路口"决定（多选一）
- 推翻先前决定
- 跨多个文档影响的约定

日常实现细节、命名 bikeshed 不必写 ADR。

## 6. 工作流

| 原则 | 说明 |
|---|---|
| **先文档后讨论** | 任何设计 / 决策提议，先落到对应文档（创建或编辑文件），然后基于文件讨论。不在对话框里口头确认设计 |
| **决策必有 ADR** | 架构或实现的分叉决定必须落到 ADR。未来回顾时不用翻聊天记录 |
| **变更要留痕** | 推翻的决定 → 原 ADR 标 `Superseded by ADR-NNNN`，写新 ADR 覆盖。**不删旧 ADR** |
| **小步提交** | 每次文档变更尽量是一个完整的小步（一节 / 一个 ADR），便于回顾 |
| **范围判定走 §4** | 决定"不做"时，先问自己是出范围还是推迟，分别归位 |

## 7. 命名与排序

- 一个目录下的多个文档用 `NN-kebab-name.md`，`NN` 是两位数字前缀，决定阅读顺序（例：`00-overview.md`, `01-functional.md`）
- ADR 用 `NNNN-kebab-name.md`，四位数字，**全局唯一递增**
- **不**用日期前缀（让顺序反映依赖 / 重要性，而非创建时间）
- README.md 在每个目录中作为该目录的索引页

## 8. 中英文

设计文档主要用中文。专有名词、API、CLI 签名、表名等使用英文（保持原貌）。ADR 标题用 kebab-case 英文（如 `0001-no-mcp.md`）。
