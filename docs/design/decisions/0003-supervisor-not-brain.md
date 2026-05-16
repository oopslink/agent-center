# 0003. 调度官命名为 Supervisor 而非 Brain

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-16 |

## Context

中心需要一个 LLM 驱动的"调度官"实体，负责：理解飞书消息、规划任务、派单、处理 Suggestion / Issue、决策升级 InputRequest、做周期 review。

初稿命名为 "Brain"，对应身体里的中枢隐喻。

讨论中发现：

- "Brain" 暗示**单一意识 / 中央控制**，但本系统里它跟 worker agent 是同源的（都是 claude code 实例），区别只是工具集
- 它本身就是 **agent**，跟 worker agent 是"角色不同"而非"种类不同"

## Decision

**改名为 Supervisor。**

- "Supervisor" = 监督者 / 调度官，是一个角色描述，不暗示物种差异
- 跟 worker agent 共享底层（都是 spawn 的 CLI agent 进程），仅工具集不同
- "Brain" 这个词永不在代码 / 文档中出现

## Consequences

正面：

- 心智一致：所有 agent 都是同一类东西，supervisor 是个**角色**
- 命名跟职责对齐，新读者理解成本低
- 不会暗示"它有意识 / 不是 agent"等错觉

负面 / 待跟进：

- 需要在所有文档、代码、CLI 命令里统一替换（已在文档中完成；代码尚未开始）
- "Supervisor" 跟 K8s / 进程监督等其它技术语境的 supervisor 有歧义 —— 文档内部要明确语义

## Alternatives Considered

### A. Brain

- Pro: 形象易懂
- Con: 暗示中央意识、跟 worker agent 不平等，与实际架构不符

### B. Coordinator / Dispatcher / Scheduler

- Pro: 准确描述部分职责
- Con: 偏向 "无脑路由器"，丢失了 "它是个 agent" 的关键属性

### C. Director / Conductor

- Pro: 跟 "做决策的 agent" 含义贴近
- Con: 文化色彩重 / 不够通俗

## 影响范围

- 文档：全部用 Supervisor
- CLI 子命令：`agent-center supervisor`（不是 `brain`）
- 表名：`supervisor_invocations`
- Skill 文件：`supervisor.md`
- 事件 / decision 字段：`supervisor:<invocation-id>` 命名风格
