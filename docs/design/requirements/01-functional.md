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
| F11 | Worker 启动时凭 bootstrap token 注册，获得长期 session token |
| F12 | 任意任务可查询完整时间线（事件 / tool call / 状态 / 产物） |
| F13 | Supervisor 每次调用可查询：触发原因、注入上下文、prompt、输出、tool call、决策 |
| F14 | 任意任务可实时 tail 日志（CLI + 飞书） |
| F15 | 提供聚合指标查询（成败率、吞吐、Issue 产生 Task 转化率等） |
| F16 | 提供 fleet view：跨 worker 实时查询当前活跃 agent + 各自活动（CLI `ps` + 飞书 query） |
| F17 | Agent 进行中可通过 CLI `agent-center request-input` 主动请示；CLI 阻塞等中心回应 |
| F18 | Supervisor 收到 InputRequest 默认 escalate 到飞书，附自己倾向的答案 |
| F19 | InputRequest 超时（默认 24h）则任务失败，可配置 |
| F20 | _（已废弃 / 合并到 [架构 / 项目本地约定](../architecture/tactical/workforce/01-worker-model.md)）_ |
| F21 | 提供本地 **Web Console UI**（嵌入 `agent-center server` 进程），仅本机绑定（loopback），用户通过 SSH 隧道访问。v1 覆盖 task / issue 的查看与管理 + agent 可观测性（fleet view / 单任务 trace 概览）。SSH 隧道由用户自理（不在本项目范围）。详见 [架构 / Web Console](../architecture/tactical/_cross-cutting/03-web-console.md)。 |
| F22 | Task 支持父子层级：每个 Task 有可选的 `parent_task_id`，可查询子任务列表，`inspect task` 展示子任务。v1 **不做**父子状态联动（取消 / 完成不自动传播），由 supervisor 显式控制。完整 DAG 依赖见 [roadmap.md](../roadmap.md) v3。 |
| F23 | Worker 自动扫描 `discovery.scan_paths`（worker.yaml 配置）发现候选项目 → 作为 `WorkerProjectProposal` 上传 → 飞书卡片让用户确认 → 升级为 `WorkerProjectMapping`。详见 [架构 / Worker 模型 § WorkerProjectMapping 创建与维护](../architecture/tactical/workforce/01-worker-model.md#workerprojectmapping-创建与维护) 与 [ADR-0008](../decisions/0008-worker-project-mapping-via-discovery-proposal.md)。 |
