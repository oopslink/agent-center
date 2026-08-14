# 0058. Unified Permission Contract

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-08-14 |

## Context

agent-center 的权限事实分散在多个 bounded context 和 transport：

- Web 以 JWT session 进入，组织级资源主要由 `requireOrgMember`、role 检查和各 handler 的
  fetch-then-check 保护。
- MCP/Agent tools 以 worker bearer token 进入，先由 `requireAgentOnWorker` 证明 worker 只能代表
  绑定在自己身上的 Agent，再交给 ProjectManager、Conversation、Files、Team Memory 等资源级检查。
- Internal admin HTTP 使用 `admin_tokens.scopes_json`，但除 `RequireScope` 路径外，还有 worker owner
  check、agent binding check 和 operator-global read 路径。
- 数据层同时存在 Org `members`、Project `pm_project_members`、Team `team_members`、Conversation
  `participants`、File `scope/scope_id`，以及 Worker/Agent/Task/Team role runtime capability。

这些结构各自合理，但没有统一语言时容易把 membership、access、runtime capability 混为一谈。例如
Team role 的 `capability_tags` 可能被误认为授权，MCP `my_capabilities` 可能被误认为 grant，
worker `task:*` scope 可能被误认为可以绕过 resource-level checks。

## Decision

冻结统一权限契约：

1. 将事实分为三类：`access`、`membership`、`runtime capability`。只有 access decision 可以 allow/deny；
   membership 只能作为派生证据；runtime capability 永不授予 access。
2. 使用 `SubjectRef`、`ResourceScope`、`PermissionKey` 作为统一判定输入。产品权限使用点分
   `PermissionKey`，内部 bearer scope 保留现有冒号格式并通过 registry 映射。
3. 第一阶段不复制旧 membership 表、不引入全量 RBAC 表。Authorizer 从现有权威结构派生 effective
   permissions，并由测试证明与旧 helper 行为一致。
4. 现有 helper 不立即删除：`requireOrgMember`、`pmRequireProjectInOrg`、`RequireScope`、
   `requireAgentOnWorker`、file reachability resolver 先委托统一 Authorizer，再逐步替换散落判断。
5. Team role / Agent tag / Worker capability / Task required capability 明确归入 runtime capability；
   任何把它们变成授权来源的需求必须另写 ADR 和迁移规格。

完整矩阵、命名规范、角色/作用域矩阵、API 协议和数据迁移协议见
[统一权限契约与现状冻结](../features/unified-permission-contract.md)。

## Consequences

正面：

- Web、MCP、internal service 可以共享同一套权限语言，降低跨入口行为漂移。
- 迁移可增量执行：旧表仍是权威，统一 Authorizer 先作为派生层和审计层落地。
- 安全边界更明确：worker token、agent binding、project/team membership、runtime capability 不再互相替代。
- 未来增加自定义 grant 时有清晰表结构和迁移门槛，不会把所有 membership 机械复制为 RBAC。

代价：

- 短期会出现 wrapper：旧 helper 名称继续存在，但内部判定逐步委托 Authorizer。
- 需要为每个 legacy source 建派生测试和 effective-permission 回读测试。
- Team CRUD 当前由所有 Org member 管理的行为会被显式冻结为 compatibility grant；若产品想收紧，
  需要单独决策和迁移。

## Alternatives Considered

1. **一次性新增 RBAC grants 表并回填所有关系**：拒绝。会复制 `members`、`pm_project_members`、
   `team_members` 等 BC 权威关系，制造双写和一致性问题。
2. **继续保留各入口自定义判断，不统一命名**：拒绝。无法可靠审计 Web/MCP/internal 的等价权限，
   也无法防止 runtime capability 被误用为授权。
3. **直接把 role/capability tag 当 permission**：拒绝。Org/Project role 是 membership 属性；
   Agent/Team/Worker capability 是运行配置，不能跨语义层授予 access。
4. **立即收紧 Team CRUD 到 owner/admin**：拒绝作为本 ADR 内容。现状代码允许 Org member 管理 Team；
   本次任务是冻结和迁移契约，不做未验收的产品收紧。
