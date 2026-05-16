# Supervisor 运行模型

Supervisor 是中心的"调度官 agent"。它是 LLM 驱动的真 agent（通过 spawn claude code 实现），不是规则路由器。

## 运行形态：C+D 合体

事件驱动外壳（C） + 工具调用做动作（D）。

- 每次事件触发：spawn 一个短生命周期的 `claude` 进程（带 supervisor.md skill + 注入相关 working memory）
- Supervisor 通过 Bash 工具调用 `agent-center <subcommand>` 完成读 / 写
- 进程结束，状态全在 DB（events 表 + working memory 表）
- 下次事件来又是一个新进程

参见 [ADR-0002 不用 LLM SDK 走 CLI agent](../decisions/0002-no-llm-sdk-use-cli-agents.md)。

## 触发事件白名单

```
✅ 飞书新消息
✅ Worker 接到任务（submitted → working 转换确认）
✅ Worker 心跳 / 进度里程碑
✅ Worker 提交 Issue（agent open-issue）
✅ Worker input-required
✅ Worker completed
✅ Worker failed
✅ Worker canceled
✅ Issue 新增 comment
✅ 任务长时间无更新（挂死预警，每个任务给个 timeout）
✅ 周期性 review（每天 9 点主动汇报）
⛔ 原始日志流（行级）—— 这是产物，不是事件
```

**防抖机制**：每个聚合根维持 30 秒（可调）的 coalescing window，同源事件合并成一个 batch 喂给 supervisor。不为省 token，是为决策上下文连贯。

## Working Memory

Supervisor 是 event-driven 的，每次都是新 LLM 进程，**没有内置记忆**。

引入 `agent_memory` 表 + scope（global / project / task / issue / suggestion / worker / supervisor）作为持久脑：

- Supervisor 启动前：按事件相关 scope 拉最近 N 条 memory → 拼到 prompt 顶部
- Supervisor 通过 CLI `agent-center record-decision --scope=... --content=...` 主动写入新记忆
- 简单关系表 + scope 关键字过滤；**不用向量检索**

schema 见 [implementation/02-persistence-schema.md](../implementation/02-persistence-schema.md) TBD。

## Supervisor 工具集（通过 Bash 调用 CLI）

| 命令 | 用途 |
|---|---|
| `agent-center dispatch ...` | 派单到某 worker 某 agent 类型 |
| `agent-center query tasks\|workers\|projects\|issues\|sessions ...` | 查询当前状态 |
| `agent-center inspect ...` | 详细查看某实体 |
| `agent-center issue open / comment / conclude / close` | Issue 操作（开 / 评论 / 收敛 / 关闭） |
| `agent-center memory list / add / prune` | 自身记忆操作 |
| `agent-center conversation add-message` | 往指定 Conversation 写一条 Message（内部存储；Bridge 自动外发） |
| `agent-center issue comment` | 往指定 Issue 写一条 IssueComment（结构化；Bridge 自动外发到 bound thread） |
| `agent-center escalate-input-request` | 把 InputRequest 升级到飞书 |
| `agent-center record-decision` | 写 working memory |

完整命令签名 → [implementation/03-cli-subcommands.md](../implementation/03-cli-subcommands.md) TBD。

## 部署位置

Supervisor 跟 server 同机（VPS）。假设 VPS 上的 claude code 已可用（OAuth 已完成 / API key 已配，本项目不管登录流程）。

> *本节其余内容待 §5 讨论补全：周期 review 的具体 prompt 设计、Working Memory 的 ttl 策略、Supervisor 错误处理等。*
