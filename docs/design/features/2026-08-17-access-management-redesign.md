# Access Management Redesign API / Security / Migration Contract

| 字段 | 值 |
|---|---|
| 状态 | Frozen contract / implementation protocol source |
| 日期 | 2026-08-17 |
| 任务 | T1355 |
| 前置契约 | [统一权限契约与现状冻结](unified-permission-contract.md)、[ADR-0058](../decisions/0058-unified-permission-contract.md) |
| 实现协议 | [07-access-redesign-protocol](../implementation/07-access-redesign-protocol.md) |
| 范围 | Authorization / RBAC / Access；Web / MCP / admin internal service；Graph / Explain / Effective / Batch |

## 1. 冻结结论

本文件冻结 Access Redesign 的 API、安全与迁移契约。后续实现、验收、回滚讨论必须以本文件为唯一方案源；
不得用聊天结论、临时任务描述或局部代码实现替代本文件。

本次只冻结契约，不实现业务代码。冻结范围：

- `SubjectRef` 语法和身份命名空间。
- Graph / Explain / Effective 的可见性、脱敏和跨入口投影一致性。
- 多层分页 completeness 判定。
- Web / MCP / internal projection parity。
- Preview / Apply / Revoke 的 CAS、TTL、idempotency 语义。
- Delegation 三层授权语义。
- P0 / P1 / P2 rollout 与 rollback 门槛。

## 2. 现状证据盘点

本节记录当前生产代码路径和符号，作为迁移边界。没有列出的入口不得被默认视为已纳入统一权限链。

| 领域 | 权威结构 / 符号 | 入口 / 符号 | 冻结判断 |
|---|---|---|---|
| Authorization 核心类型 | `internal/authorization/types.go::SubjectRef`、`ResourceScope`、`PermissionKey`、`CheckRequest`、`AccessDecision`、`ExplainResult`、`BatchRequest` | `internal/authorization/service.go::Check`、`Explain`、`ListEffective`、`PreviewBatch`、`ApplyBatch`、`RevokeBatch` | 统一判定输入保持 `SubjectRef + PermissionKey + ResourceScope`；Explain / Effective 是投影，不能成为写授权来源。 |
| Permission registry | `internal/authorization/registry.go::Definitions`、`PermissionForBearerScope`、`BearerScopeAllows` | admin bearer scope 到 `PermissionKey` 的映射 | 内部 bearer scope 保留冒号格式，只能经 registry 映射为点分 `PermissionKey`。 |
| RBAC 自定义表 | `internal/persistence/migrations/0129_unified_authorization.up.sql` 的 `authorization_roles`、`authorization_role_permissions`、`authorization_role_assignments`、`authorization_idempotency_keys`、`authorization_audit_events` | `internal/authorization/store.go` 的 role / assignment / audit / idempotency 写入 | 自定义 grant 是增量层；旧 membership 表仍是权威事实，禁止批量复制 membership 成 grants。 |
| Legacy membership | `0033_identity_bc.up.sql::members`、`0041_v27_projectmanager.up.sql::pm_project_members`、`0107_v229_teams.up.sql::team_members`、`0124_team_memory_policy.up.sql::team_memory_policies`、`conversations.participants`、`file_references` | `internal/authorization/service.go::addLegacyEffective` | membership 只作为 effective permission 派生证据，不是通用 permission 字符串。 |
| SubjectRef 验证 | `internal/authorization/types.go::SubjectRef.Validate`、`UserSubject`、`AgentSubject`、`WorkerSubject` | `internal/admin/api/agent_tools_write.go::agentActor` | 业务 Agent subject 必须是 `agent:<identity_member_id>`；worker 是 transport / binding subject，不替代业务身份。 |
| Web org gate | `internal/webconsole/api/handlers.go::requireOrgMember` | `Authorizer.Check(org.read, ResourceScope{Kind:"org"})` 后保留 legacy member 回读 | Web 组织访问根必须经统一 Authorizer 或 legacy compatibility guard，禁止仅信任前端 role。 |
| Web project gate | `internal/webconsole/api/handlers_pm.go::pmRequireProjectInOrg` | `Authorizer.Check(project.read, ResourceScope{Kind:"project"})` | Project 入口必须解析 org/project 关系；跨 org 返回 opaque 404。 |
| Web permission API | `internal/webconsole/api/server.go` 的 `/permissions/definitions`、`/check`、`/explain`、`/effective`、`/audit`、`/batch/preview`、`/batch/apply`、`/batch/revoke` | `internal/webconsole/api/handlers_permissions.go` | 所有 permission API 均先经 `requireOrgMember`；跨 subject 查询只允许 owner/admin。 |
| Web plan graph | `internal/webconsole/api/server.go` 的 `GET /api/orgs/{slug}/projects/{project_id}/plans/{plan_id}/graph` | `internal/webconsole/api/handlers_pm_plans.go::pmGetPlanGraphHandler`、`pmPlanGraphMap` | Web Graph 是 plan 投影；只能暴露安全字段，不能暴露 raw orchestration metadata / internal command detail。 |
| Admin bearer | `internal/admin/api/auth.go::RequireScope` | `adminBearerSubject`、`bearerResource`、`matchedBearerScope` | 内部 scope 判定必须进入 Authorizer；worker owner / admin owner 只是 subject 解析证据。 |
| Agent binding | `internal/admin/api/agent_tools.go::requireAgentOnWorker` | `Authorizer.Check(agent.operate.self, ResourceScope{Kind:"agent"})` | Worker token 只能证明该 worker 可代表绑定 Agent；不能授予资源 read/write。 |
| Admin route surface | `internal/admin/api/server.go::routes` | `/admin/workforce/*`、`/admin/environment/*`、`/admin/agent-tools/*`、`/admin/git/*`、secret/blob/admin-token 等 | internal route 必须保留 bearer + resource-level check；agent tools 不得因 token scope 跳过业务资源授权。 |
| MCP catalog | `internal/mcphost/agent_facing_set.go::AgentFacingToolNames` | `internal/mcphost/server_test.go`、`agent_tool_admin_route_guard_test.go` | agent-facing tool set 是显式白名单；存在工厂函数不等于可见工具。Graph raw tools 不在 canonical set 时不得暴露给模型。 |
| MCP injection | `internal/mcphost/server.go` | 固定 `Config.AgentID` 注入 admin call | MCP model 不可提供 `agent_id`；admin 侧仍以 `requireAgentOnWorker` 回查。 |
| Pagination | `agent_tools_history.go::listMessagesHandler`、`agent_tools_passthrough.go::listTasksHandler`、`agent_tools_issues.go::listIssuesHandler`、`agent_tools_plans.go::listPlansHandler`、`agent_tools_read_models.go` | `has_more`、`next_before_message_id`、`total`、`page_size`、`offset` | 分页 response 必须说明是否完整；多层聚合不得隐藏内部 cap。 |
| Redaction | `internal/admin/api/agent_tools_read_models.go::redactAuditNote`、`secrets_redacted` | task audit、execution、runtime effective config | secret / token / password / bearer / auth 类信息一律脱敏；Explain/Graph 不得新增旁路。 |
| Idempotency / CAS | `internal/authorization/store.go::beginIdempotency`、`completeIdempotency`、`revokeAssignment` | `internal/authorization/service.go::ApplyBatch`、`RevokeBatch` | Apply/Revoke 必须要求 idempotency key；revoke assignment 使用 CAS 单赢家语义。 |

## 3. SubjectRef 冻结

`SubjectRef` 是 access 判定的唯一主体引用语法：

| 形式 | 语义 | 可授权动作 |
|---|---|---|
| `system` | 系统内部主体 | 仅限明确标记的系统流程；不得由 Web/MCP 客户端传入。 |
| `user:<identity_id>` | 人类用户 identity | Web session、owner/admin/member、项目成员、团队成员等业务授权。 |
| `agent:<identity_member_id>` | Agent 的业务成员身份 | MCP/agent tool 的业务资源授权。不是 `agents.id`。 |
| `worker:<worker_id>` | Worker runtime subject | bearer scope、worker owner、agent binding 证明；不能替代 `agent:<identity_member_id>` 访问业务资源。 |

冻结规则：

- 禁止裸 id、email、display name、`member:<id>`、`team:<id>`、`service:<id>` 作为 `SubjectRef`。
- `agent:<...>` 默认指 `identity_member_id`；只有运行时绑定、worker enrollment、admin diagnostics 可使用 `agents.id`，
  且字段名必须显式为 `agent_id`，不得进入 `SubjectRef`。
- Web permission API 默认 subject 是当前 user；查询另一个 subject 必须先通过同 org owner/admin 判定。
- MCP 请求的 `agent_id` 由 mcphost 固定注入，模型输入不得覆盖；admin 侧必须回查 worker-agent binding。
- `SubjectRef.Validate` 是落地验收锚点；任何新增 subject namespace 必须先更新本文件、ADR 或替代冻结文档。

## 4. ResourceScope / PermissionKey 冻结

`ResourceScope` 和 `PermissionKey` 继承 [统一权限契约与现状冻结](unified-permission-contract.md) 的命名规则：

- `PermissionKey` 使用点分产品权限，例如 `org.read`、`project.read`、`task.internal.report`。
- admin bearer scope 使用现有冒号格式，例如 `task:*`、`secret:resolve`，只能经 registry 映射进入 Authorizer。
- `ResourceScope` 必须携带足够 parent scope，以便从 child resource 回溯 org/project/team。
- `ResourceScope.Kind` 不得用 transport 名称表达，例如禁止 `mcp_tool`、`web_page` 作为业务资源类型。
- 资源不存在、跨 org、无权访问在外部响应中统一为 opaque 404 或 deny；Explain 内部证据也必须脱敏。

## 5. Graph / Explain / Effective 可见性与脱敏

### 5.1 Graph

Graph API 分三类：

| Graph 类型 | 当前入口 | 可见性冻结 |
|---|---|---|
| Web plan graph projection | `GET /api/orgs/{slug}/projects/{project_id}/plans/{plan_id}/graph` | 必须先通过 org/project/plan 解析和 `plan.read` 等价授权；仅返回 plan 用户投影。 |
| MCP agent-facing graph | `AgentFacingToolNames` | 默认不得暴露 raw orchestration graph 工具；若未来暴露，必须在 canonical set、admin route guard、resource-level check 三处同时显式登记。 |
| Internal orchestration graph | `internal/admin/api/agent_tools_orchestration.go` 的 graph handlers | 只能作为 internal/admin API；若被 agent-facing 调用，必须将 `graph_id -> plan_id -> project_id -> org_id` 解析后执行 `plan.read` / `plan.write`。 |

Graph response 允许字段：

- node: `id`、`category`、`control_kind`、`title`、`status`、`task_id`、`task_status`、`assignee_ref`、`org_ref`。
- edge: `from`、`to`、`kind`。
- graph envelope: `has_graph`、`nodes`、`edges`、`summary` 类用户投影字段。

Graph response 禁止字段：

- raw orchestration metadata、internal command id、worker token、executor path、process env、hidden system note。
- 未脱敏的 actor credential、secret name/value、bearer/admin token scope 原文。
- 跨 org target id、未授权 plan/project/task id。

`ErrPlanHasNoGraph` 可映射为 `has_graph:false`；跨 org、资源不存在、无授权不得借此暴露 id 是否存在。

### 5.2 Explain

Explain 是诊断投影，不是授权来源。冻结规则：

- 默认只能 explain 当前 subject。
- owner/admin 可 explain 同 org 内另一个 `user:` 或 `agent:` subject。
- `worker:` subject 的 explain 仅限 internal diagnostics；Web 不应暴露 worker runtime 细节。
- Explain 可以返回 `source`、`reason`、脱敏 `evidence_ref` 和允许/拒绝的 high-level 原因。
- Explain 不得返回 secret、token、password、bearer、auth detail、admin token hash、raw participants JSON。
- Cross-org / unknown resource 的 external response 不得回显请求中的 target id；内部日志可记录完整 id，但必须遵守审计访问控制。

### 5.3 Effective

Effective permissions 是展示和测试投影：

- 不能被 handler 当作 allow/deny 缓存或写授权依据。
- 必须标记 permission 的 `source` 和 `delegatable`。
- 对同一资源的 Web / MCP / internal effective projection 必须共享 Authorizer 结果，不得独立拼装。
- 如果投影来自分页或多层聚合，必须携带 completeness 信息；无 completeness 时只能作为 partial debug。

## 6. 多层分页 Completeness 冻结

分页响应必须明确说明客户端看到的是完整集合还是一页。

| 分页模式 | 必备字段 | 完整性语义 |
|---|---|---|
| Offset pagination | `items` 或业务集合、`total`、`page_size`、`offset`、`has_more` | `total` 必须是过滤后的 pre-page count；`has_more = offset + len(items) < total`。 |
| Keyset pagination | `items` 或业务集合、`has_more`、`next_*_cursor` | 当 `has_more=true` 时必须返回可继续读取的 cursor；当 `has_more=false` 时本方向完整。 |
| Tail / recent window | `items`、`has_more`、`next_before_*` | 响应只能声称 recent window 完整，不能声称整个历史完整。 |
| Multi-layer aggregation | top-level `complete` 或每个 child 的 `complete/has_more/truncated` | 任一 child 被 page cap、timeout、permission filter、post-filter raw limit 截断时，top-level 必须不是 complete。 |

冻结规则：

- API 不得在内部先取固定 N 条再过滤，却仍返回 `complete=true` 或省略 completeness。
- audit / history / graph / effective 等诊断 API 若跨多个来源聚合，必须暴露每个来源的截断状态或整体
  `complete:false`。
- 当前 `permissions/audit` 以 raw limit 过滤 org audit 的形态在契约上只能视为 partial；进入 P2 前必须改为稳定
  cursor 或显式返回 incomplete。
- MCP tool 描述中的 page cap 必须和 admin response envelope 一致；测试要同时锁住 schema、cap 和 `has_more`。

## 7. Projection Parity 冻结

Projection parity 指同一授权事实在 Web、MCP、internal/debug 中的字段、过滤、脱敏、分页语义一致。

冻结规则：

- Graph、Explain、Effective、Audit、Task/Issue/Plan list projection 必须以 AppService / Authorizer / read model 为单一来源；
  Web/MCP handler 只做 transport envelope 和 subject 解析。
- 任一 projection 新增字段时，必须更新：
  1. Web response mapper 或 OpenAPI/handler contract。
  2. MCP tool schema / description。
  3. internal/admin response contract。
  4. parity test 或 acceptance matrix。
- MCP canonical set 与 registered tools 必须双向一致；存在 handler 或 factory 不等于 agent-facing。
- 不允许 Web 暴露比 MCP 更多的敏感 detail，也不允许 MCP 暴露 Web 不可见的 raw orchestration detail。
- `effective` 和 `explain` 不能绕过 `Check`；它们只能复用 `Explain/ListEffective` 生成投影。

## 8. Preview / Apply / Revoke：CAS、TTL、Idempotency

### 8.1 Preview

Preview 是 side-effect-free dry run：

- 不写 `authorization_role_assignments`、`authorization_audit_events`、`authorization_idempotency_keys`。
- 返回每个 operation 的 `would_*` / `allowed` / `reason` / validation error。
- Preview 成功不保证 Apply 成功；Apply 必须重新执行授权、CAS 和 idempotency。
- Preview response 不得包含可用于绕过 Apply 的 capability token 或 hidden grant id。

### 8.2 Apply / Revoke Idempotency

Apply / Revoke 必须要求 caller 提供 idempotency key。冻结语义：

| 条件 | 结果 |
|---|---|
| 首次 `key + actor + operation + request_digest` | 记录 pending，执行事务，完成后保存 response digest / response JSON。 |
| 相同 key、actor、operation、digest，已完成 | replay 上次 response，HTTP 语义等价 200。 |
| 相同 key、actor、operation，不同 digest | 409 idempotency conflict。 |
| 相同 key、actor、operation、digest，仍 pending | 409 conflict；不得并发执行第二份写。 |
| key 过期后重复提交 | 作为新请求处理；不得 replay 过期响应。 |

`request_digest` 必须覆盖 actor、operation 类型、全部 batch operation、resource scope、role/permission assignment 输入。

### 8.3 TTL

Idempotency TTL 冻结为服务端策略：

- Completed replay window：24 小时。
- Pending reclaim window：15 分钟；超过该窗口且事务未完成的 pending key 可被服务端标记为 expired 后重试。
- 服务端可为审计保留过期记录更久，但 replay / conflict 判定必须以 `expires_at` 或等价可回读状态为准。
- 客户端不得提交自定义 TTL。
- 当前 schema 若没有显式 TTL 字段，P0 可以记录差距；P1/P2 不得把 idempotency 迁移标记为完成，直到 TTL 可由数据库或等价持久状态证明。

### 8.4 CAS

CAS 是 assignment 写入和撤销的并发语义：

- Revoke assignment 必须以 `WHERE revoked_at IS NULL` 等等价条件保证只有一个 writer 返回 `revoked`。
- 并发重复 revoke 对同一 assignment 只能有一个 `revoked`，其余为 `unchanged` 或 idempotent replay。
- Upsert role / role permissions / assign role 必须在单事务中写 audit；失败不得留下半写状态。
- Cross-org resource 或 target subject 不得先写后补偿；必须 fail closed。

## 9. Delegation 三层语义

Delegation 不是单一 owner/admin 判断，而是三层同时成立：

| 层 | 判定 | 当前证据锚点 | 冻结规则 |
|---|---|---|---|
| 1. Manage authority | actor 有 `org.member.role.manage` 或等价 system authority | `internal/authorization/service.go::requireManageRBAC` | 可管理 org RBAC 元数据，但不自动代表 actor 可授出所有业务权限。 |
| 2. Delegatable permission | actor 在目标 resource 上已有效持有 role 的每个 permission，且 effective permission `Delegatable=true` | `requireDelegatableRole`、`EffectivePermission.Delegatable` | 非 delegatable 权限不能被转授权；runtime capability 不能转成 delegatable access。 |
| 3. Resource / subject boundary | target resource、target subject、actor 均解析到同一 org / parent scope | `resolveResource`、`subjectOrgID`、cross-org tests | 不能把 project/team/plan 内权限授到其他 org；未知或跨 org 必须 404/deny。 |

Revoke 规则：

- actor 有 manage authority 可 revoke 同 org assignment。
- actor 持有对应 role 的 delegatable 权限可 revoke 自己可管理的 assignment。
- actor 不能通过 revoke response 探测跨 org role/assignment 是否存在。

## 10. Rollout / Rollback

| 阶段 | 目标 | 允许动作 | 禁止动作 | Rollback |
|---|---|---|---|---|
| P0 Contract + Observe | 冻结契约、盘点入口、读路径 shadow / diagnostics | 文档、tests、registry 校验、Explain/Effective 只读、parity snapshot | 改业务授权结果、复制 membership、扩大 Graph 可见性 | 关闭 diagnostics / debug routes；Authorizer wiring 保持可旁路 legacy helper；无数据迁移破坏。 |
| P1 Delegate + Audit | legacy helper 委托 Authorizer，batch write 受控开启 | `requireOrgMember`、`pmRequireProjectInOrg`、`RequireScope`、`requireAgentOnWorker` 委托；Apply/Revoke 强制 idempotency | 在未完成 TTL / completeness / redaction 验收前宣称迁移完成 | feature flag 关闭 custom role writes；helper 回退 legacy 判定；保留 audit 只读。 |
| P2 Enforce + Parity | 统一 Authorizer 成为 Web/MCP/internal 资源授权主链 | Graph/Explain/Effective projection parity、完整分页、TTL-backed idempotency、delegation 三层验收 | 裸 raw graph agent-facing、partial audit 冒充 complete、runtime capability 授权 | 禁用 custom assignments；保留 legacy membership 权威；回滚到 P1 委托/只读模式；禁止 destructive down migration。 |

Rollback 通用规则：

- 不删除 legacy membership 表，不依赖 down migration 恢复业务访问。
- 自定义 assignment 可禁用或忽略，但不能让用户失去 legacy membership 派生访问。
- Rollback 后必须能从数据库和 route tests 回读当前模式；不能以日志或任务状态代替。
- 最终 main 合并由集成节点完成；任务分支只能交付候选 SHA。

## 11. 可执行验收矩阵

| 编号 | 冻结项 | 命令 / 检查 | 通过标准 |
|---|---|---|---|
| A1 | Source of truth exists | `test -f docs/design/features/2026-08-17-access-management-redesign.md` | 文件存在并被 `docs/design/README.md` 索引。 |
| A2 | SubjectRef grammar | `rg -n "type SubjectRef|func \\(s SubjectRef\\) Validate|AgentSubject|WorkerSubject" internal/authorization/types.go` | 只能接受 `system`、`user:`、`agent:`、`worker:`；新增 namespace 必须先改本文件。 |
| A3 | Web permission routes | `rg -n "permissions/(definitions|check|explain|effective|audit|batch/(preview|apply|revoke))" internal/webconsole/api/server.go` | 路由集合与本文件一致，且 handler 先经 org gate。 |
| A4 | Delegate entry points | `rg -n "Authorizer.Check" internal/webconsole/api/handlers.go internal/webconsole/api/handlers_pm.go internal/admin/api/auth.go internal/admin/api/agent_tools.go` | Web org/project、admin bearer、agent binding 均可回读 Authorizer 委托。 |
| A5 | Graph visibility | `rg -n "pmGetPlanGraphHandler|pmPlanGraphMap|AgentFacingToolNames|make.*Graph" internal/webconsole/api internal/mcphost internal/admin/api` | Web graph 是 plan projection；MCP canonical set 未显式列出的 raw graph tool 不可见。 |
| A6 | Redaction anchors | `rg -n "redactAuditNote|secrets_redacted|DeniedBy|evidence_ref" internal/admin/api internal/authorization internal/webconsole/api` | secret/token/auth/bearer 类信息有脱敏锚点；新增 Explain/Graph 字段需同等规则。 |
| A7 | Pagination completeness | `rg -n "has_more|next_before_message_id|total|page_size|offset|complete|truncated" internal/admin/api internal/webconsole/api internal/mcphost` | 每个 paged list 有 pre-page total 或 keyset cursor；多层聚合不得默认 complete。 |
| A8 | Idempotency + CAS | `rg -n "beginIdempotency|completeIdempotency|revokeAssignment|IdempotencyKey|requestHash" internal/authorization` | Apply/Revoke 强制 key；same digest replay、different digest conflict、revoke CAS 单赢家。 |
| A9 | Delegation tests | `go test ./internal/authorization -run 'TestService_.*(Idempotent|Concurrent|CrossOrg|Delegat|Revoke)'` | delegation、cross-org、idempotency、CAS 相关测试通过；缺项时必须补测试后进入 P1/P2。 |
| A10 | Web contract tests | `go test ./internal/webconsole/api -run TestPermissions` | cross-org opaque / subject audit / check / effective 行为通过。 |
| A11 | Admin/MCP parity tests | `go test ./internal/admin/api ./internal/mcphost` | bearer delegate、agent tool route guard、canonical MCP set parity 通过。 |
| A12 | Docs link / format | `git diff --check` plus local markdown-link validation over changed docs | 无 trailing whitespace；相对 markdown 链接可解析。 |

## 12. 未完成差距登记

这些是进入 P1/P2 前必须关闭的差距，不在本任务实现：

- `authorization_idempotency_keys` 当前需要补显式 TTL / expiry 状态，才能满足 24h replay window 和 15m pending reclaim。
- `permissions/audit` 当前需要稳定 cursor 或 explicit incomplete envelope，才能满足多层分页 completeness。
- raw orchestration graph handlers 若未来变成 agent-facing，必须补 `graph_id -> plan/project/org` 授权解析和 parity tests。
- Explain / Graph deny response 需要持续测试，确保 cross-org id、secret、bearer scope detail 不外泄。
- Projection parity 需要在新增字段时同步 Web/MCP/internal schema 和测试，不能只改一个入口。
