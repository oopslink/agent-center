# 系统总览

## 高层拓扑

```
                        ┌─────────────────────────────────┐
                        │     飞书开放平台                  │
                        └────────┬────────────────────────┘
                                 │ WebSocket 长连接
                                 │ (Center 作为 client 主动建立)
                                 ↓
   ┌──────────────────────────────────────────────────────────────┐
   │  agent-center server  (VPS 常驻 / 单一二进制 / 多模式)         │
   │                                                              │
   │   ├─ feishu module           接事件 / 发卡片                  │
   │   ├─ worker gRPC endpoint    与 worker 长连接                 │
   │   ├─ supervisor launcher     事件触发 spawn supervisor 进程    │
   │   ├─ admin CLI (unix sock)   人 / supervisor 调用工具         │
   │   ├─ SQLite (events / tasks / issues / ...)                  │
   │   └─ BlobStore (LocalDir → S3 future)                        │
   └──────────┬──────────────────────────────┬────────────────────┘
              │ gRPC 长连接（worker enroll）  │ spawn 进程
              ↓                              ↓
   ┌─────────────────────┐         ┌─────────────────────┐
   │ Worker daemon       │         │ Supervisor agent    │
   │ (你的开发机)         │         │ (claude code 实例)   │
   │                     │         │                     │
   │   ├─ task executor  │         │   skill: supervisor │
   │   ├─ agent adapter  │         │   工具: Bash → CLI  │
   │   ├─ local sock     │         └─────────────────────┘
   │   │  (agent ↔ CLI) │
   │   └─ worktree mgr   │
   │       │              │
   │       ↓ spawn        │
   │  ┌──────────────┐    │
   │  │ Worker agent │    │
   │  │ (claude code)│    │
   │  │              │    │
   │  │ skill:       │    │
   │  │   worker-agt │    │
   │  │ 工具:        │    │
   │  │   Bash → CLI │    │
   │  │              │    │
   │  │ cwd=worktree │    │
   │  │ 读 CLAUDE.md │    │
   │  └──────────────┘    │
   └─────────────────────┘
```

## 主要角色

| 角色 | 部署 | 职责 |
|---|---|---|
| **Center server** | VPS 常驻 | 状态权威、事件流总线、飞书入口、worker 长连接端、ADR 的事实承载 |
| **Supervisor agent** | VPS 同机短生命周期进程 | LLM 驱动的调度官，事件触发 spawn 一次。**它也是 agent**（claude code 进程），区别是工具集偏向调度 / 决策 |
| **Worker daemon** | 用户的开发机 | 接派单、起 agent 子进程、维持 agent ↔ CLI 工具的本地 socket、收集 trace / 日志、上报状态 |
| **Worker agent** | 用户的开发机短生命周期进程 | 真正干活的 agent（claude code 或其它 CLI）。spawn 进 worktree，干活 |

## 主要数据流

| 流向 | 内容 |
|---|---|
| 飞书 → Center | DM / @bot 消息、card action 回调 |
| Center → 飞书 | 普通消息、交互卡片、Issue 讨论回复 |
| Worker daemon → Center | 状态事件、心跳、agent trace 流、suggestion / open-issue、input-request、任务结束日志归档上传 |
| Center → Worker daemon | 派单、input-response、控制指令（取消 / 暂停） |
| Worker agent → Worker daemon | CLI 调用（request-input / report-progress / open-issue / read-task-context / ...） |
| Worker daemon → Worker agent | CLI 调用的响应（包括 input-response 解阻塞） |
| Supervisor → Center | 通过 CLI 工具调用（dispatch / query / issue comment / conversation add-message / record-decision / ...） |

各上下文的事件类型与聚合关系见 [01-bounded-contexts.md](01-bounded-contexts.md)。

## 单一二进制 / 多模式

`agent-center` 是一个 Go binary，按子命令选择运行模式：

- `agent-center server` — VPS 上的常驻
- `agent-center supervisor` —（内部）事件触发的一次性 supervisor 子进程
- `agent-center worker --config=...` — 开发机上的 worker daemon
- `agent-center <operation>` — CLI 操作命令（人用 / agent 用）

完整 CLI 表见 [implementation/03-cli-subcommands.md](../implementation/03-cli-subcommands.md)（TBD）。

## 部署形态

- VPS 一台：跑 Center + Supervisor。
- 用户开发机 N 台：每台跑一个 Worker daemon。
- 飞书：作为 IO 通道，不存状态。
- 项目仓库：在 Worker 本地已 clone（agent-center 不管理 git）。

## 网络方向

- 飞书 ↔ Center：**Center 主动出站**建立 WebSocket（无需 VPS 暴露入站到飞书）
- Worker → Center：**Worker 主动出站**建立 gRPC 长连接
- 用户操作：SSH 到 VPS 跑 CLI（v1 不开远程 admin RPC）

故 VPS 仅需对 Worker 暴露 gRPC 端口（一个），不需要 HTTP webhook、不需要域名。
