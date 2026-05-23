> ⚠ **v1-era doc** — pending rewrite in Phase 10 / 11 (see `docs/plans/phase-10-conversation-v2.md` and `phase-11-user-entry.md`). v2 撤回了 Bridge BC + 飞书集成 (per [ADR-0031](../../../decisions/0031-v2-drop-bridge-vendor-integration.md))；本文中 Bridge / vendor / 飞书 / 已删 ADR 引用是 v1 残留，待 P10/P11 重写。

# Skill + CLI 工具链

> **DDD 战术层** · 主题: agent-harness

agent-center 给 agent（supervisor 与 worker-agent）暴露能力的方式：**skill 文档教 agent 怎么用 + CLI 命令作为实际工具**。**不引入 MCP**（理由见 [ADR-0001](../../../decisions/0001-no-mcp.md)）。

## Skill 文件

| Skill | 谁的 | 用途 | 注入方式 |
|---|---|---|---|
| `supervisor.md` | Supervisor agent | 说明角色、决策模式、可用命令、输出约定 | spawn supervisor 前注入到 prompt 顶部 |
| `worker-agent.md` | Worker 内 agent | 说明你在执行 task、约束、如何调 request-input / report-progress / open-issue / read-task-context 等 | spawn worker agent 前由 worker daemon 注入到 prompt（见 [08-prompt-assembly.md](01-prompt-assembly.md)） |

两个文件由 binary 携带分发（embed in Go binary 或 `${install_dir}/skills/` 下）。v1 直接拼到 prompt 顶部，不依赖各 agent CLI 的原生 skill 机制。

## CLI 子命令大类

### 操作命令（人 / supervisor 都用）

| 命令 | 用途 |
|---|---|
| `dispatch` | 派单 |
| `query tasks\|workers\|projects\|issues\|sessions` | 查询 |
| `inspect task\|supervisor\|session\|issue` | 详细查看 |
| `ps [--watch]` | Fleet view |
| `issue open / comment / conclude / close` | Issue 操作 |
| `memory list / add / prune` | Supervisor 记忆 |
| `stats` | 聚合指标 |

### Supervisor 专用

| 命令 | 用途 |
|---|---|
| `conversation add-message` | 往指定 Conversation 写入一条 Message（内部存储；~~Bridge 自动外发~~ — v2 删 Bridge per [ADR-0031](../../../decisions/0031-v2-drop-bridge-vendor-integration.md)） |
| `issue comment` | 往指定 Issue 写入一条 IssueComment（结构化；~~如 Issue 有 bound card，Bridge 自动外发到 bound thread~~ — v2 删 Bridge per [ADR-0031](../../../decisions/0031-v2-drop-bridge-vendor-integration.md)） |
| `issue bind-conversation` | 懒创建路径触发：把没有 Conversation 的 Issue 关联到一个 Conversation（[ADR-0021](../../../decisions/0039-conversation-business-model-v2-unified.md)；删除原 unbind/rebind 系列） |
| `issue link-conversation` | 手动把某 conversation 关联到 issue（写入 `issue.related_conversation_ids`） |
| `escalate-input-request` | ~~InputRequest 升级到飞书~~ → v2: InputRequest 升级路径待 P11 重设计 (vendor 集成撤回 per [ADR-0031](../../../decisions/0031-v2-drop-bridge-vendor-integration.md)) |
| `record-decision` | 写 working memory |

### Worker 内 agent 专用（通过 worker daemon 中转）

| 命令 | 行为 |
|---|---|
| `request-input` | **阻塞**，等中心回应；stdout 输出 JSON answer |
| `report-progress` | 上报里程碑 / 百分比 |
| `open-issue` | 非阻塞，立即返回 issue id |
| `read-task-context` | 拉本任务的 prompt / 约束 / parent task / project 元信息 |

### 服务模式 / 设置

| 命令 | 用途 |
|---|---|
| `server` | VPS 上的常驻 |
| `supervisor` | （内部）事件触发的一次性 supervisor 进程 |
| `worker --config=...` | Worker daemon |
| ~~`feishu setup`~~ | ~~一键创建应用流程~~ (v2 删 vendor 集成 per [ADR-0031](../../../decisions/0031-v2-drop-bridge-vendor-integration.md)) |
| `worker enroll` | 签发 worker bootstrap token |
| `worker proposal list [--worker=...] [--status=...]` | 列 WorkerProjectProposal |
| `worker proposal unignore <id>` | 取消之前的"忽略"，让下次扫到再次提议 |
| `project add / update / remove` | 项目管理（手动；通常由 proposal accept 自动创建） |

完整 CLI 签名 → [implementation/03-cli-subcommands.md](../../../implementation/03-cli-subcommands.md)。

## CLI 自适应上下文

同一命令在不同上下文走不同 transport：

```
agent-center query tasks ...
  ├─ server 同机直接跑    → 走本地 unix socket 到 server
  ├─ worker 内 agent 跑   → env 检测到 WORKER_SOCK → 走 worker daemon → 转 server
  └─ 未来远程跑           → env 中 AGENT_CENTER_REMOTE=tcp://... + token → 走远程 RPC
```

用户 / agent 写命令是一样的，**transport 自动**。

## Worker 内"阻塞式 CLI"实现

agent 调 `agent-center request-input` 时，CLI 与 worker daemon 之间走 unix socket，daemon 持 pending map 等中心回应。详见 [03-input-request.md](../task-runtime/03-input-request.md) 流程图。
