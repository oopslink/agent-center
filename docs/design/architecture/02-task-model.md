# Task 模型 & 状态机

> **TBD** —— 本文档将在 [§3 讨论](../../../README.md) 中补全。

## 占位计划

预计将包含：

- Task 作为聚合根的责任
- Task 状态机（借鉴 A2A 协议：submitted / working / input_required / completed / failed / canceled）
- Task 与 Issue 的血缘（`from_issue_id`）
- Task 与 Worktree、AgentSession 的关系
- Task 产物（artifacts）的表达
- Task 的不变量（invariants）：永远绑定一个 project；同一时刻只能在一个 worker 上；branch_name 唯一

## 已确定的状态机要点

- 任务永远归属一个 project（无散单）
- 任务可承接 InputRequired 状态：`working → input_required → working`（resume）或 `→ failed`（超时）
- Task 删除不允许；只能 cancel / fail / archive
