> 📌 **v2.7 update (2026-06-26)** — v2.7 引入 ProjectManager BC 取代 TaskRuntime + Discussion；Identity BC 独立（ADR-0040）；CLI 收窄为 deployment-only ~9 命令（v2.7 #162）；Supervisor 暂停主动开发（ADR-0044）但代码骨架保留。用户主入口 = Web Console（ADR-0037）。

# 系统总览

> **DDD 战略层**

## 高层拓扑

```
   ┌───────────────────────────────────────────────────────────────────┐
   │  agent-center server  (VPS 常驻 / 单一二进制 / 多模式)              │
   │                                                                   │
   │   ├─ Web Console (SPA React + Go API + SSE)  用户主入口 (ADR-0037)│
   │   │    webconsole/api + webconsole/spa + webconsole/sse           │
   │   ├─ Admin API (admin/api + admin/clienttransport)               │
   │   ├─ worker gRPC endpoint    与 worker 长连接                     │
   │   ├─ supervisor launcher     事件触发 spawn supervisor 进程       │
   │   │    (ADR-0044: 暂停主动开发，代码骨架保留)                      │
   │   ├─ MCP Host (mcphost)      MCP server 托管                     │
   │   │                                                               │
   │   │  ┌─ 领域层 ─────────────────────────────────────────────┐     │
   │   │  │ projectmanager  — Project/Issue/Task/Plan/PlanFinding│     │
   │   │  │ cognition       — SupervisorInvocation/Memory/       │     │
   │   │  │                   Reminder/WakeGuard                 │     │
   │   │  │ identity        — Identity/Organization/Member/      │     │
   │   │  │                   Invitation                         │     │
   │   │  │ agent           — AgentInstance lifecycle             │     │
   │   │  │ workforce       — Worker/WorkerProjectProposal       │     │
   │   │  │ conversation    — Conversation/Message/Channel       │     │
   │   │  │ environment     — Environment/controlstream          │     │
   │   │  │ observability   — Event store + projections          │     │
   │   │  │ secretmgmt      — UserSecret (AES-GCM)              │     │
   │   │  │ files           — File upload/management             │     │
   │   │  └──────────────────────────────────────────────────────┘     │
   │   │                                                               │
   │   ├─ SQLite (persistence/migrations) + outbox                    │
   │   └─ BlobStore (blobstore — file-based, S3 future)               │
   └──────────┬──────────────────────────────┬────────────────────────┘
              │ gRPC 长连接（worker enroll）  │ spawn 进程
              ↓                              ↓
   ┌─────────────────────┐         ┌─────────────────────┐
   │ Worker daemon       │         │ Supervisor agent    │
   │ (workerdaemon)      │         │ (agentsupervisor/   │
   │ (你的开发机)         │         │  supervisormanager) │
   │                     │         │ (claude code 实例)   │
   │   ├─ agent adapter  │         │                     │
   │   │  (agentadapter/ │         │   skill: supervisor │
   │   │   claudecode/   │         │   工具: Bash → CLI  │
   │   │   codex/        │         └─────────────────────┘
   │   │   opencode/)    │
   │   ├─ environment    │
   │   │  controlstream  │
   │   ├─ local sock     │
   │   │  (agent ↔ CLI) │
   │   └─ worktree mgr   │
   │       │              │
   │       ↓ spawn        │
   │  ┌──────────────┐    │
   │  │ Worker agent │    │
   │  │ (claude code/│    │
   │  │  codex/      │    │
   │  │  opencode)   │    │
   │  │              │    │
   │  │ cwd=worktree │    │
   │  │ 读 CLAUDE.md │    │
   │  └──────────────┘    │
   └─────────────────────┘
```

## 主要角色

| 角色 | 部署 | 职责 |
|---|---|---|
| **Center server** | VPS 常驻 | 状态权威、事件流总线、用户 UI 入口（Web Console SPA + API + SSE，ADR-0037）、Admin API、worker 长连接端、MCP Host、所有领域 BC 的运行宿主 |
| **Supervisor agent** | VPS 同机短生命周期进程 | LLM 驱动的调度官，事件触发 spawn 一次（ADR-0044 暂停主动开发，代码骨架保留）。**它也是 agent**（claude code 进程），区别是工具集偏向调度 / 决策。代码包：`agentsupervisor` / `supervisormanager` |
| **Worker daemon** | 用户的开发机 | 接派单、起 agent 子进程（通过 `agentadapter`：claudecode / codex / opencode）、维持 agent ↔ CLI 工具的本地 socket、收集 trace / 日志、上报状态、管理 environment controlstream。代码包：`workerdaemon` |
| **Worker agent** | 用户的开发机短生命周期进程 | 真正干活的 agent（claude code / codex / opencode）。spawn 进 worktree，干活 |

## 主要数据流

| 流向 | 内容 |
|---|---|
| Browser → Center | Web Console SPA HTTP API 请求（项目管理 / 会话 / agent 管理 / 文件上传等） |
| Center → Browser | SSE 实时推送（事件流、消息更新、状态变化） |
| Worker daemon → Center | 状态事件、心跳、agent trace 流（claudestream 解析）、控制日志、任务结束日志归档上传 |
| Center → Worker daemon | 派单、input-response、控制指令（取消 / 暂停）、environment controlstream |
| Worker agent → Worker daemon | CLI 调用（request-input / report-progress / open-issue / read-task-context / ...） |
| Worker daemon → Worker agent | CLI 调用的响应（包括 input-response 解阻塞） |
| Supervisor → Center | 通过 CLI 工具调用（dispatch / query / conversation send / record-decision / ...）（ADR-0044 暂停，骨架保留） |
| Admin → Center | Admin API（admin/api）：备份（admin/backup）/ 系统管理操作；admintoken 认证 |

各上下文的事件类型与聚合关系见 [03-bounded-contexts.md](03-bounded-contexts.md)。

## 单一二进制 / 多模式

`agent-center` 是一个 Go binary，按子命令选择运行模式：

- `agent-center server` — VPS 上的常驻（含 Web Console + Admin API + gRPC endpoint + MCP Host）
- `agent-center supervisor` —（内部）事件触发的一次性 supervisor 子进程（ADR-0044 暂停，骨架保留）
- `agent-center worker --config=...` — 开发机上的 worker daemon

> **v2.7 CLI 收窄 (#162)**：CLI 已收窄为 deployment-only 约 9 个命令（server / worker / 基础管理），日常操作全部通过 Web Console 完成。代码包 `internal/cli`（cobra 命令集）。

完整 CLI 表见 [implementation/03-cli-subcommands.md](../../implementation/03-cli-subcommands.md)。

## 部署形态

- VPS 一台：跑 Center server（含 Web Console + Admin + Supervisor skeleton）。
- 用户开发机 N 台：每台跑一个 Worker daemon。
- 项目仓库：在 Worker 本地已 clone（agent-center 不管理 git）。

## 网络方向

- Browser → Center：HTTP（Web Console SPA 静态资源 + API + SSE 推送）
- Worker → Center：**Worker 主动出站**建立 gRPC 长连接
- Admin → Center：Admin API（可通过 admin/clienttransport 本地或远程访问，admintoken 认证）

故 VPS 需对外暴露 HTTP 端口（Web Console + Admin API）+ gRPC 端口（Worker 连接）。

## 基础设施包一览

| 包 | 职责 |
|---|---|
| `agentadapter` | CLI agent 适配器（claudecode / codex / opencode 子包） |
| `agentsupervisor` / `supervisormanager` | Supervisor 生命周期管理 |
| `admin` | Admin 操作（api / backup / clienttransport 子包） |
| `admintoken` | Admin token 认证 |
| `blobstore` | 文件 blob 存储 |
| `webconsole` | 嵌入式 React SPA + HTTP API + SSE（api / spa / sse 子包） |
| `cli` | Cobra CLI 命令集（v2.7 deployment-only ~9 命令） |
| `config` / `settings` | 配置管理 |
| `persistence` | SQLite + migrations |
| `workerdaemon` | Worker daemon 生命周期 |
| `outbox` | Transactional outbox 模式 |
| `mcphost` | MCP server 托管 |
| `mention` | @mention 解析 |
| `claudestream` | Claude streaming protocol 解析器 |
| `idgen` / `clock` | ID 生成 / 时钟工具 |
| `usage` | 用量追踪 |
