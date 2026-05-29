# 0050. Environment BC：Worker + AgentController + worker-initiated control（v2.7）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-29 |
| Delivered | v2.7 design phase；详 [v2.7-domain-refactor-plan § 2.5 / § 3.3 / § 5 D](../../plans/v2.7-domain-refactor-plan.md) |
| Supersedes | amends [ADR-0023 Worker Enroll 轻量化](0023-worker-enroll-lightweight.md)（Worker 升为 Environment BC 内 AR + worker-initiated 控制流） |
| Related | [ADR-0049 Agent BC](0049-agent-bc-no-agentrun.md)（Agent 绑定 Worker） / [ADR-0051 ClaudeCode 契约](0051-claudecode-headless-contract.md) / [ADR-0048 File URI/BlobStore](0048-file-uri-and-blobstore.md)（FileTransfer 归属此 BC） |

## Context

v2.7 需要一个**运行环境** BC 承载：部署在机器上的常驻 Worker、对本地 Agent 进程/会话的控制、中心与 Worker 的通信，以及文件传输机制。Worker 可能在 NAT/防火墙后，控制通道必须 worker 主动发起。

## Decision

### 1. 立 Environment 为独立 BC

承载 Worker、AgentController、本地进程/会话控制、worker-initiated 中心连接、FileTransfer。

### 2. 聚合 / 流（plan § 3.3）

| AR / Stream | 职责 |
|---|---|
| Worker | 部署在机器上的常驻进程 |
| AgentRuntimeSession | Agent 进程/会话的内部 runtime 记录（按需，不对外暴露为产品 AR） |
| BlobStore | 横向内容存储；blob 身份 = ULID（详 [ADR-0048](0048-file-uri-and-blobstore.md)） |
| FileReference | 放置记录 `{scope, scope_id} → file_uri` |
| FileTransferSession | 上传/下载会话与传输态 |
| WorkerControlEvent | 中心↔Worker 命令/ack/状态流 |

### 3. 拓扑与基数

- 一台机器可跑多个 Worker；一个 Worker 控制多个 Agent（**Worker:Agent = 1:N**）。
- Agent.runtime.worker_id 必填且不可变（[ADR-0049](0049-agent-bc-no-agentrun.md)）。
- Worker.status 与 Agent.lifecycle 分离；Agent.availability 由两者派生，Worker 维度优先级最高（plan § 10 OQ2）。

### 4. 控制通道（plan § 10 OQ3）

- **Worker 主动发起**；命令建模为**有序可重放命令日志**，stream 优先 + poll 兜底（**同一日志**）。
- **v1 实现顺序（D1，决定 2026-05-30）**：先落 **poll-baseline**（worker 主动 poll，骑现有 admin API bearer/TLS：`connect` / `commands?after=ackOffset` / `ack`），它就是 ADR 的"兜底、同一命令日志"，完整满足 #102 验收核心（重连按 offset 续传 / ack 推进 / 破坏命令不重复）且端到端最好测；**SSE「stream 优先」低延迟推送列为后续优化**（slice ②b / D-later，跑同一日志、零返工）。控制流低频（生命周期命令 + ack/status；agent 高频输出走观测流），poll 间隔延迟可接受。
- ack = 按 offset 累积确认；每条命令带 **idempotency_key**。**两层去重**：日志层（已 ack 的重连不再下发，D1 保证）+ 执行层（worker 命令处理器按 idempotency_key 去重，防"执行后 ack 前崩溃→重拉→二次执行"，属 D2）。
- AgentController（原 AgentLauncher 改名）负责：start/stop/restart/reset 进程、stream-json stdin/stdout 控制、状态/心跳映射。
- StopAgent 操作态不自动 blocked（业务态需显式，详 [ADR-0049](0049-agent-bc-no-agentrun.md)）。

### 5. Worker 内部结构

ControlLoop、AgentController、MCPHost、FileTransferClient、LocalStore。

- **MCPHost 只暴露 AppService 工具，不暴露直接数据库访问**（plan § 2.5 / § 10 OQ4）。

### 6. FileTransfer

- FileTransferSession：创建上传/下载会话、完成/取消/过期、本地文件系统后端。
- 上传完成后 `ac://files/{ulid}` 可读；引用计数 GC（7 天宽限）归此 BC 落地（详 [ADR-0048](0048-file-uri-and-blobstore.md)）。
- 大文件分块机制需单独细化（D 阶段）。

## Consequences

### 正面

- Worker 主动连 = 穿透 NAT/防火墙；offset+幂等键 = 重连不重复破坏。
- 1:N + 状态分层让单机多 Agent、Worker 掉线时 Agent 统一 unavailable 表达清晰。

### 负面 / 待跟进

- worker-initiated 长连的重连/背压/心跳超时阈值需工程细化。
- FileTransfer 分块/断点续传 v2.7 D 阶段才完整；A0 只占接缝。
- Worker 上云 / Worker 迁移 / failover 列 roadmap。

## Alternatives Considered

### A. 中心主动连 Worker

- ❌ Worker 多在 NAT 后，中心无法主动建连。否决，改 worker-initiated。

### B. 先做 poll

- ❌ 实时控制体验差（延迟或空转）。否决，stream 优先 + poll 兜底（同一命令日志）。

### C. 逐命令 ack

- ❌ 比 offset 累积确认重；幂等键已负责防重复执行。否决。

### D. Worker MCP 直连数据库

- ❌ 破坏 AppService 唯一入口与域隔离。否决。

## References

- [v2.7-domain-refactor-plan § 2.5](../../plans/v2.7-domain-refactor-plan.md) / [§ 3.3](../../plans/v2.7-domain-refactor-plan.md) / [§ 4.5](../../plans/v2.7-domain-refactor-plan.md) / [§ 5 D](../../plans/v2.7-domain-refactor-plan.md) / [§ 10 OQ2/OQ3/OQ4](../../plans/v2.7-domain-refactor-plan.md)
- [ADR-0049](0049-agent-bc-no-agentrun.md) / [ADR-0051](0051-claudecode-headless-contract.md) / [ADR-0048](0048-file-uri-and-blobstore.md)
- 来源：2026-05-27 ～ 2026-05-29 DM 设计讨论（@oopslink ↔ @AgentCenterPD）
