# 功能需求

| ID | 需求 |
|---|---|
| F1 | 通过飞书（DM / 群 @bot）接收任务请求 |
| F2 | Supervisor 理解请求、规划任务、派单给合适的 Worker + Agent 类型 |
| F3 | Worker 在隔离的 worktree 内执行 Agent CLI，流式回传事件 / 心跳 |
| F4 | Worker 在任务结束时上传完整日志归档 |
| F5 | Worker / Supervisor / User 三方都可开 Issue（议题），归属某个 project |
| F6 | Issue 支持多轮讨论：飞书 thread ↔ issue_comment 同步；用户 / Supervisor 参与 |
| F7 | Issue 结论由用户拍板（v1 不让 Supervisor 自动收敛）；结论可产生 0 / 1 / N 个 Task；产生的 Task 带 `from_issue_id` |
| F8 | Supervisor 跨事件保留 working memory（按 scope 持久化） |
| F9 | Supervisor 每日定时 review，向用户推送摘要卡片 |
| F10 | 用户可在 VPS 本机 CLI 查询 / 操作状态（`agent-center query / promote-suggestion / ...`） |
| F11 | Worker 通过 `agent-center join <center-endpoint> --token=<bootstrap-token>` 一行命令接入，凭一次性 bootstrap token 兑换长期 session token；token 是独立 Entity（TTL 30min，支持 reissue / revoke）。详见 [ADR-0023](../decisions/drafts/0023-worker-enroll-lightweight.md)。 |
| F12 | 任意任务可查询完整时间线（事件 / tool call / 状态 / 产物） |
| F13 | Supervisor 每次调用可查询：触发原因、注入上下文、prompt、输出、tool call、决策 |
| F14 | 任意任务可实时 tail 日志（CLI + 飞书） |
| F15 | 提供聚合指标查询（成败率、吞吐、Issue 产生 Task 转化率等） |
| F16 | 提供 fleet view：跨 worker 实时查询当前活跃 agent + 各自活动（CLI `ps` + 飞书 query） |
| F17 | Agent 进行中可通过 CLI `agent-center request-input` 主动请示；CLI 阻塞等中心回应 |
| F18 | Supervisor 收到 InputRequest 默认 escalate 到飞书，附自己倾向的答案 |
| F19 | InputRequest 超时（默认 24h）则任务失败，可配置 |
| F20 | _（已废弃 / 合并到 [架构 / Workforce BC](../architecture/tactical/workforce/00-overview.md)）_ |
| F21 | 提供本地 **Web Console UI**（嵌入 `agent-center server` 进程），仅本机绑定（loopback），用户通过 SSH 隧道访问。v1 覆盖 task / issue 的查看与管理 + agent 可观测性（fleet view / 单任务 trace 概览）。SSH 隧道由用户自理（不在本项目范围）。详见 [架构 / Web Console](../architecture/tactical/presentation/01-web-console.md)。 |
| F22 | Task 支持父子层级：每个 Task 有可选的 `parent_task_id`，可查询子任务列表，`inspect task` 展示子任务。v1 **不做**父子状态联动（取消 / 完成不自动传播），由 supervisor 显式控制。完整 DAG 依赖见 [roadmap.md](../roadmap.md) v3。 |
| F23 | Worker 自动扫描 `discovery.scan_paths`（来自 center 下发的 Worker config，可通过 `agent-center worker config set ...` 修改）发现候选项目 → 作为 `WorkerProjectProposal` 上传 → 飞书卡片让用户确认 → 升级为 `WorkerProjectMapping`。详见 [架构 / WorkerProjectProposal 聚合](../architecture/tactical/workforce/03-worker-project-proposal.md) 与 [ADR-0008](../decisions/0008-worker-project-mapping-via-discovery-proposal.md) + [ADR-0023](../decisions/drafts/0023-worker-enroll-lightweight.md)。 |
| F24 | `AgentInstance` 作为 agent 一等公民：用户用 `agent-center agent create --name=<n> --worker=<id> --agent-cli=<cli>` 显式创建并起名；TaskExecution 通过 `agent_instance_id` 强引用；agent 持久 home_dir 沉淀 instructions / mcp / skill；状态机 `idle / active / sleeping / archived` 跟 Worker 状态联动；1:N（一个 agent 可并行多个 TaskExecution，受 Worker concurrency + AgentInstance.max_concurrent cap）。详见 [架构 / AgentInstance 聚合](../architecture/tactical/workforce/04-agent-instance.md) 与 [ADR-0024](../decisions/drafts/0024-agent-instance-first-class.md)。 |
| F25 | **用户密钥中心化管理**：`agent-center secret create / list / rotate / revoke / usage` —— 用户密钥（MCP env vars / 云凭据等）由 SecretManagement BC 集中存储；DB 仅存 AES-GCM 加密的 ciphertext + nonce，master key 来自配置文件不入 DB；CLI 不打印明文；引用语法 `secret:<name>`。详见 [架构 / SecretManagement BC](../architecture/tactical/secret-management/00-overview.md) 与 [ADR-0026](../decisions/drafts/0026-user-secret-management-bc.md)。 |
| F26 | **MCP per-agent 注入**：用户为 AgentInstance 配置 `mcp_config`（MCP 标准 schema），可在其中用 `secret:<name>` 引用用户密钥；worker daemon spawn agent 前 just-in-time 解析 secret 写本机 `mcp_config.runtime.json`（mode 0600，execution 后清理），按 agent adapter 翻译给具体 CLI 注入 MCP servers。详见 [ADR-0027](../decisions/drafts/0027-mcp-per-agent-injection.md)。 |
