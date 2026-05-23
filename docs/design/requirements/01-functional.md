# 功能需求

> 🆕 **v2 更新**（[ADR-0031](../decisions/0031-v2-drop-bridge-vendor-integration.md) Drop Bridge + [ADR-0037](../decisions/0037-web-console-as-main-user-ui.md) W1 + [ADR-0038](../decisions/0038-cli-ux-enhancement.md) W2）：v1 中 F1 / F6 / F14 / F16 / F18 等含飞书 vendor 描述，v2 已撤回；用户主入口改为 **Web Console + CLI**。

| ID | 需求 |
|---|---|
| F1 | 通过 **Web Console / CLI** 接收任务请求（v2: vendor 撤回，per [ADR-0031](../decisions/0031-v2-drop-bridge-vendor-integration.md)）|
| F2 | Supervisor 理解请求、规划任务、派单给合适的 Worker + Agent 类型 |
| F3 | Worker 在隔离的 worktree 内执行 Agent CLI，流式回传事件 / 心跳 |
| F4 | Worker 在任务结束时上传完整日志归档 |
| F5 | Worker / Supervisor / User 三方都可开 Issue（议题），归属某个 project |
| F6 | Issue 支持多轮讨论：Conversation Message 时间线承载（kind=issue Conversation；v2 [ADR-0039](../decisions/0039-conversation-business-model-v2-unified.md)）；用户 / Supervisor / Worker agent 参与 |
| F7 | Issue 结论由用户拍板（v1 不让 Supervisor 自动收敛）；结论可产生 0 / 1 / N 个 Task；产生的 Task 带 `from_issue_id` |
| F8 | Supervisor 跨事件保留 working memory（按 scope 持久化） |
| F9 | Supervisor 每日定时 review，向用户推送摘要（v2: 通过 Web Console / DM Conversation）|
| F10 | 用户可在 VPS 本机 CLI 查询 / 操作状态（`agent-center query / promote-suggestion / ...`） |
| F11 | Worker 通过 `agent-center join <center-endpoint> --token=<bootstrap-token>` 一行命令接入，凭一次性 bootstrap token 兑换长期 session token；token 是独立 Entity（TTL 30min，支持 reissue / revoke）。详见 [ADR-0023](../decisions/0023-worker-enroll-lightweight.md)。 |
| F12 | 任意任务可查询完整时间线（事件 / tool call / 状态 / 产物） |
| F13 | Supervisor 每次调用可查询：触发原因、注入上下文、prompt、输出、tool call、决策 |
| F14 | 任意任务可实时 tail 日志（CLI `conversation tail -f` per [ADR-0038](../decisions/0038-cli-ux-enhancement.md) + Web Console SSE per [ADR-0037](../decisions/0037-web-console-as-main-user-ui.md)）|
| F15 | 提供聚合指标查询（成败率、吞吐、Issue 产生 Task 转化率等） |
| F16 | 提供 fleet view：跨 worker 实时查询当前活跃 agent + 各自活动（CLI `ps` + Web Console fleet 页）|
| F17 | Agent 进行中可通过 CLI `agent-center request-input` 主动请示；CLI 阻塞等中心回应 |
| F18 | Supervisor 收到 InputRequest 默认 escalate 给用户，附自己倾向的答案；用户在 **Web Console 卡片 UI / CLI `input-request respond`** 回复（v2 撤回飞书后唯一入口）|
| F19 | InputRequest 超时（默认 24h）则任务失败，可配置 |
| F20 | _（已废弃 / 合并到 [架构 / Workforce BC](../architecture/tactical/workforce/00-overview.md)）_ |
| F21 | 提供本地 **Web Console UI**（嵌入 `agent-center server` 进程），仅本机绑定（loopback），用户通过 SSH 隧道访问。**v2 升级为全量用户 UI**（聊天 / 看 channel / 回 InputRequest / 发起任务 / 派生 Issue/Task / fleet view / trace 等）；技术栈 Go API + SPA + SSE，per [ADR-0037](../decisions/0037-web-console-as-main-user-ui.md)。SSH 隧道由用户自理（不在本项目范围）|
| F22 | Task 支持父子层级：每个 Task 有可选的 `parent_task_id`，可查询子任务列表，`inspect task` 展示子任务。v1 **不做**父子状态联动（取消 / 完成不自动传播），由 supervisor 显式控制。完整 DAG 依赖见 [roadmap.md](../roadmap.md) v3。 |
| F23 | Worker 自动扫描 `discovery.scan_paths`（来自 center 下发的 Worker config，可通过 `agent-center worker config set ...` 修改）发现候选项目 → 作为 `WorkerProjectProposal` 上传 → **Web Console / CLI 给用户确认** → 升级为 `WorkerProjectMapping`。详见 [架构 / WorkerProjectProposal 聚合](../architecture/tactical/workforce/03-worker-project-proposal.md) 与 [ADR-0008](../decisions/0008-worker-project-mapping-via-discovery-proposal.md) + [ADR-0023](../decisions/0023-worker-enroll-lightweight.md)。 |
| F24 | `AgentInstance` 作为 agent 一等公民：用户用 `agent-center agent create --name=<n> --worker=<id> --agent-cli=<cli>` 显式创建并起名；TaskExecution 通过 `agent_instance_id` 强引用；agent 持久 home_dir 沉淀 instructions / mcp / skill；状态机 `idle / active / sleeping / archived` 跟 Worker 状态联动；1:N（一个 agent 可并行多个 TaskExecution，受 Worker concurrency + AgentInstance.max_concurrent cap）。详见 [架构 / AgentInstance 聚合](../architecture/tactical/workforce/04-agent-instance.md) 与 [ADR-0024](../decisions/0024-agent-instance-first-class.md)。 |
| F25 | **用户密钥中心化管理**：`agent-center secret create / list / rotate / revoke / usage` —— 用户密钥（MCP env vars / 云凭据等）由 SecretManagement BC 集中存储；DB 仅存 AES-GCM 加密的 ciphertext + nonce，master key 来自配置文件不入 DB；CLI 不打印明文；引用语法 `secret:<name>`。详见 [架构 / SecretManagement BC](../architecture/tactical/secret-management/00-overview.md) 与 [ADR-0026](../decisions/0026-user-secret-management-bc.md)。 |
| F26 | **MCP per-agent 注入**：用户为 AgentInstance 配置 `mcp_config`（MCP 标准 schema），可在其中用 `secret:<name>` 引用用户密钥；worker daemon spawn agent 前 just-in-time 解析 secret 写本机 `mcp_config.runtime.json`（mode 0600，execution 后清理），按 agent adapter 翻译给具体 CLI 注入 MCP servers。详见 [ADR-0027](../decisions/0027-mcp-per-agent-injection.md)。 |
| F27 | **Skill file mount（lite）**：用户在 AgentInstance home_dir（worker: `~/.agent-center-worker/agents/<id>/skills/`，built-in supervisor: `~/.agent-center/agents/supervisor/skills/`）自配 skill 文件（Anthropic Skills 标准 SKILL.md）；worker daemon / center 进程 spawn agent 前按 adapter 用 `--skill-path` 或 symlink `~/.claude/skills` 挂载；内置 skill（worker-agent.md / supervisor.md）随 binary 部署。v2 **不** 引入中心化 skill 管理。详见 [ADR-0028](../decisions/0028-skill-file-mount-lite.md)。 |
| F28 | **Supervisor 是 built-in AgentInstance**：center 首次启动时自动创建 `(name='supervisor', is_builtin=true, worker_id=NULL)` AgentInstance；CLI `agent show supervisor / agent config set supervisor / agent list` 跟 worker AgentInstance 一致；`agent archive supervisor` 拒绝。SupervisorInvocation 持 `agent_instance_id` 强引用。详见 [ADR-0029](../decisions/0029-supervisor-as-builtin-agent-instance.md)。 |
| F29 | **AgentAdapter 矩阵扩展**：v2 实装 `claude-code` + `codex` + `opencode` 三个 adapter；Adapter Go interface 扩展 `Probe / SupportedFeatures / BuildMCPConfigArg / BuildSkillMountSetup` 方法承载 G1/G4/G5 per-CLI 翻译职责；Worker.capabilities 加 `supports_mcp / supports_skills / supports_session` feature flags；派单失败 `feature_unsupported` 在 dispatch 校验链早期拦截。详见 [ADR-0030](../decisions/0030-agentadapter-matrix-expansion.md)。 |
