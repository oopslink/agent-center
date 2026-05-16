# 0004. Issue 取代 Suggestion

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-16 |

## Context

最初设计里，agent / supervisor 可以提交 **Suggestion**（建议）—— 一个"提议要不要做某事"的实体，由用户决定采纳 / 拒绝。

讨论中发现：

- agent-center 的项目类型多元（编程 / 写作 / 投研），不只是代码
- "Task = agent 干活的通用单位" 是干净的抽象
- 但 "Suggestion" 隐含**一锤子提议 → 拍板**，现实里很多事**要多轮讨论才有结论**
- 跟 GitHub Issue / PR 的概念映射：Suggestion ≈ Issue，Task ≈ branch/PR；GitHub 没把 "提建议"叫 Suggestion 是有道理的

## Decision

**用 Issue 取代 Suggestion。**

| 概念 | 定义 |
|---|---|
| **Task** | Agent 干活的最小单位。状态机驱动 |
| **Issue** | 一件**待讨论**的事。可能由 worker / supervisor / 用户提起。讨论 → 结论 → 产生 0 / 1 / N 个 Task |

支持多轮讨论：

- 来源：user / worker (agent) / supervisor 都可开
- 生命周期：open → under_discussion → concluded / closed_no_action / withdrawn
- 第一条非 opener 的 comment 进入进入 under_discussion
- 用户拍板 conclude，可选 spawn 0/1/N 个 Task
- Task 带 `from_issue_id` 保留血缘

不与 GitHub Issue 实体耦合（只是命名借鉴），保持 agent-center 独立可用于非 GitHub 项目。

## Consequences

正面：

- **抓住"讨论"的本质**：现实中很多事要多轮、有反复；Suggestion 模型表达不出
- **统一来源**：worker / supervisor / 用户开 Issue 用同一个抽象，agent-center 整个事件流更干净
- **跟 GitHub Issue 概念对齐**：用户已有直觉，认知成本低
- **不放弃边界**：Issue 不是 GitHub Issue，不强制 GitHub 集成；只是命名借鉴

负面 / 待跟进：

- 多轮讨论的状态机比 Suggestion 复杂，需要 Issue + IssueComment 两张表
- 飞书 thread 要跟 issue_comment 双向同步
- "Issue" 一词读者会想到 GitHub Issue，需要在文档明确区分

## Alternatives Considered

### A. 保留 Suggestion 一锤子模型

- Pro: 简单，单实体单决策
- Con: 真实工作场景中"一锤子"决策少，"讨论 → 共识"更常见

### B. 改名为 Proposal / Ticket / Topic

- Pro: 不跟 GitHub Issue 撞
- Con: Issue 一词已是行业通用语；Proposal 偏向投票表决场景；Ticket 更工单化

### C. 完整 GitHub 化（Task = PR，Issue = GitHub Issue）

- Pro: 不重复造
- Con: 强 GitHub 依赖，非 GitHub 项目无法接入；详见 [概述](../requirements/00-overview.md) 的"项目多领域"段
