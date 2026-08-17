# 统一权限契约与现状冻结

| 字段 | 值 |
|---|---|
| 状态 | Frozen contract / migration spec |
| 日期 | 2026-08-14 |
| 关联 ADR | [ADR-0058](../decisions/0058-unified-permission-contract.md) |
| 范围 | Org / Project / Team / Human / Agent；Web / MCP / admin internal service |

2026-08-17 的 Access Redesign API、安全与迁移冻结源见
[Access Management Redesign API / Security / Migration Contract](2026-08-17-access-management-redesign.md)。
本文件仍是统一权限基础契约；涉及 Graph/Explain 可见性、分页 completeness、projection parity、
preview/apply idempotency TTL、delegation rollout 的实现验收以 2026-08-17 冻结源为准。

## 1. 结论摘要

当前系统同时存在三类容易混用的事实：

1. **access**：一次请求能否执行动作。现状入口包括 Web JWT + `requireOrgMember`、
   admin bearer `RequireScope`、worker token owner 检查、`requireAgentOnWorker`、以及各
   AppService 的 project/team/file/conversation 检查。
2. **membership**：主体和资源的关系。现状表包括 `members`、`pm_project_members`、
   `team_members`、`conversations.participants`、`file_references.scope/scope_id`。
3. **runtime capability**：执行能力或调度约束。现状结构包括 Worker `Capability`、Agent
   `capability_tags` / `allowed_executors` / `max_concurrent_tasks`、Team `team_roles`
   中的 `cli/model/capability_tags/max_concurrency`、Task `required_capabilities` 和
   AI Runtime Catalog。

冻结原则：

- `access` 才能回答 allow/deny；`membership` 只能作为派生 access 的证据；`runtime capability`
  永远不能直接授予 access。
- 角色、成员关系、worker token scope、MCP tool catalog 必须收敛为同一套 `PermissionKey`
  + `ResourceScope` 判定输入。
- 迁移第一阶段不重写生产表。所有权限从现有结构派生，保证旧行为可回读、可审计、可回滚。

## 2. 证据边界

本规格的结论来自以下生产代码与真实结构，不以概念模型替代现状：

| 领域 | 数据结构证据 | 鉴权入口证据 | 当前结论 |
|---|---|---|---|
| Identity / Org | `internal/persistence/migrations/0033_identity_bc.up.sql` 的 `identities`、`organizations`、`members(role owner/admin/member,status joined/disabled)`、`invitations(role_to_grant)`；`internal/identity/types.go` 的 `MemberRole.AtLeast` | `internal/webconsole/api/handlers.go::requireOrgMember`；`handlers_org.go`；`handlers_members.go`；`handlers_invitations.go` | Org membership 是 Web 组织访问根。Owner/Admin/Member 是 access 派生来源，不是通用 permission 字符串。 |
| Project | `internal/persistence/migrations/0041_v27_projectmanager.up.sql` 的 `pm_projects`、`pm_project_members(role member/owner)`；`internal/projectmanager/types.go::ProjectMemberRole` 注释说明 v1 role 不区分权限 | `internal/projectmanager/service/service.go::requireProjectMember`；`appservices.go::AddProjectMember/RemoveProjectMember`；`internal/webconsole/api/handlers_pm.go::pmRequireProjectInOrg` | Project membership 是读写门槛；owner 仅多出移除成员等少量 owner-only 动作。 |
| Team | `internal/persistence/migrations/0107_v229_teams.up.sql` 的 `teams`、`team_roles`、`team_members`、`team_projects`；`0113_team_member_multi_role.up.sql` 多角色迁移；`0124_team_memory_policy.up.sql` | `internal/team/service/service.go` 无 actor 参数；`internal/webconsole/api/handlers_teams.go::teamGuardMember` 仅走 Org member；`teammemory/team_policy_auth.go` 对 Team Memory 单独授权 | Team membership 是 roster / memory / git 的关系，不等于 Org 权限。Team role 的 CLI/model/tags/concurrency 是 runtime config。 |
| Human / Agent ref | `internal/agent/agent.go::agentActor` 使用 `agent:<identity_member_id>`；`internal/team/types.go::MemberRef` 要求 `agent:` / `user:`；`internal/projectmanager/types.go::IdentityRef` 同样要求 `system|user:<id>|agent:<id>` | `internal/admin/api/agent_tools.go::requireAgentOnWorker`；`internal/mcphost/server.go` 固定注入 `agent_id` | Agent 有 runtime entity id 与 business identity-member id 两个命名空间；业务权限必须用 actor ref，worker 只证明运行载体。 |
| Web | `web/src/api/auth.ts`、`web/src/OrgContext.tsx`、`web/src/api/client.ts` | 后端 `authMiddleware` + `requireOrgMember`；Web `role` 只用于导航/禁用按钮 | 前端 role / memory permission 是展示投影，不是权威授权。 |
| MCP / Agent tools | `internal/mcphost/agent_facing_set.go::AgentFacingToolNames`；`internal/mcphost/tools.go` 注入 `agent_id` | `requireAgentOnWorker`；`agent_tools_passthrough.go::requireTaskAccess`；`agent_tools_write.go::requireOwnTask`；`agent_tools_files.go::agentOwnDomainScopes` | Tool 是否可见不是权限来源；每个请求仍需 worker-bound agent + 资源级检查。 |
| Internal service | `internal/persistence/migrations/0028_admin_tokens.up.sql` 的 `admin_tokens.scopes_json`；`internal/admintoken/types.go` 的 `Scope` 初始集合 | `internal/admin/api/auth.go::RequireScope`；`secret.go`；`blob.go`；`workforce.go`；`worker_report_capabilities.go` | Admin bearer scope 是内部 service access；很多 worker/agent 路由还依赖 owner check，需纳入统一判定。 |
| Runtime capability | `internal/workforce/types.go::Capability`；`internal/agent/agent.go::Profile.EffectiveConcurrencyCap`、`capabilityTags` 注释；`internal/projectmanager/task.go::RequiredCapabilities`；`0116_ai_runtime_catalog.up.sql` | `auto_assign_reconciler.go::capabilityGatePasses`；`worker_report_capabilities.go` worker owner check；`RuntimeSelectors.tsx` 注释 | Capability 参与调度、匹配、兼容性和并发，不授予 read/write/manage。 |

## 3. 统一语言

### 3.1 Access

`AccessDecision` 是一次请求的最终判定：

```ts
type AccessDecision = {
  allowed: boolean
  subject_ref: SubjectRef
  permission: PermissionKey
  resource: ResourceScope
  source: "org_role" | "project_member" | "team_member" | "team_memory_policy" |
          "conversation_participant" | "file_scope" | "admin_token_scope" |
          "worker_owner" | "agent_worker_binding" | "system"
  reason: string
  evidence_ref: string
}
```

`evidence_ref` 必须能回到现有结构，例如 `members:<id>`、`pm_project_members:<id>`、
`team_members:<team_id>/<member_ref>/<role>`、`admin_tokens:<id>` 或
`file_references:<scope>/<scope_id>`。

### 3.2 Membership

`Membership` 是关系，不是动作：

```ts
type Membership = {
  subject_ref: "user:<identity_id>" | "agent:<identity_member_id>"
  resource: { kind: "org" | "project" | "team" | "conversation"; id: string; org_id: string }
  role?: string
  status?: "joined" | "disabled" | "left"
  source_table: string
}
```

规则：

- `members.status=joined` 是 Org access 派生的前提；disabled member 不派生普通 access。
- `pm_project_members` 派生 `project.read` / `project.write`；当前 `role=owner` 只派生 owner-only member removal。
- `team_members` 派生 team membership、team memory agent proposal/read、team git rw；不派生 Org admin。
- `conversations.participants` 派生 conversation participant access；left participant 不派生 access。

### 3.3 Runtime Capability

`RuntimeCapability` 描述“能不能运行/适配/被调度”，不描述“能不能访问”：

```ts
type RuntimeCapability = {
  owner: "worker:<id>" | "agent:<identity_member_id>" | "team:<id>" | "task:<id>" | "runtime_catalog:<org_id>"
  key: string
  value: unknown
  source: string
}
```

硬规则：

- `agents.capability_tags` 只用于 auto-assign 和 human/PD 分派标签，不可映射为 `team.memory.review`。
- `team_roles.capability_tags`、`cli`、`model`、`max_concurrency` 是 role runtime config，不是 role permission。
- `workers.capabilities_json` 和 `workforce.Capability{Detected,Enabled,SupportsMCP,...}` 只表示 worker 可执行环境。
- `pm_tasks.required_capabilities` 是 task 对 assignee 的要求，不能让携带该 tag 的 agent 读取任意 project。

## 4. 命名规范

### 4.1 SubjectRef

| 类型 | 格式 | 来源 |
|---|---|---|
| Human | `user:<identity_id>` | `identities.kind=user` |
| Agent business identity | `agent:<identity_member_id>` | `agents.identity_member_id`，由 `agentActor` 生成 |
| Worker internal client | `worker:<worker_id>` | `admin_tokens.owner` |
| System | `system` | 事件、迁移、内置任务 |

运行时 Agent entity id 只用于 worker binding、runtime home、git agent repo owner 等执行域。Project、
Team、Conversation、Message、Task assignee 等业务域必须使用 `agent:<identity_member_id>`。

### 4.2 ResourceScope

```ts
type ResourceScope =
  | { kind: "org"; id: string }
  | { kind: "project"; id: string; org_id: string }
  | { kind: "team"; id: string; org_id: string }
  | { kind: "task" | "issue" | "plan"; id: string; project_id: string; org_id: string }
  | { kind: "conversation"; id: string; org_id: string; owner_ref?: string }
  | { kind: "file"; uri: string; refs: Array<{ scope: string; scope_id: string }> }
  | { kind: "agent"; id: string; org_id: string; identity_member_id?: string }
  | { kind: "worker"; id: string; org_id?: string }
  | { kind: "admin_token"; id: string }
```

跨 scope 资源读取必须先解析 parent scope，再做判定；不能仅凭 path id 判定。

### 4.3 PermissionKey

产品权限使用小写点分键：

```text
<domain>.<action> | <domain>.<resource>.<action>[.<qualifier>]
```

动作词固定为：

```text
read, list, create, update, delete, manage, assign, start, complete,
block, attach, upload, download, resolve, report, review, promote, reject,
export, put, pull, heartbeat
```

内部 bearer scope 保留现有冒号语法，称为 `BearerScope`：

```text
<domain>:<action> | <domain>:* | *
```

`BearerScope` 是传输凭证权限，不替代资源级 `PermissionKey`。例如 `task:*` 允许 worker 调用 task
内部路由，但 agent tool 仍要过 `requireAgentOnWorker` 和 project/task access。

## 5. 角色与作用域矩阵

### 5.1 Org role

| PermissionKey | Scope | owner | admin | member | 现状证据 |
|---|---|---:|---:|---:|---|
| `org.read` | org | 是 | 是 | 是 | `requireOrgMember` |
| `org.settings.manage` | org | 是 | 否 | 否 | `handlers_org.go::updateOrgHandler` |
| `org.lifecycle.manage` | org | 是 | 否 | 否 | `handlers_org.go::deleteOrgHandler/orgEnableDisable` |
| `org.member.list` | org | 是 | 是 | 是 | `handlers_members.go` list 走 `resolveCallerAndOrg` |
| `org.member.create.human` | org | 是 | 是，不能授 owner | 否 | `handlers_members.go::addHumanMemberHandler` |
| `org.member.create.agent` | org | 是 | 是，不能授 owner | 否 | `handlers_members.go::addAgentMemberHandler` |
| `org.member.role.manage` | org | 是 | 否 | 否 | `handlers_members.go` role change owner-only |
| `org.invitation.manage` | org | 是 | 是，不能授 owner | 否 | `handlers_invitations.go` |
| `org.analytics.read` | org | 是 | 是 | 否 | `handlers_analytics.go` |
| `coderepo.workspace.manage` | org | 是 | 是 | 否 | `handlers_coderepo.go::requireOrgAdmin` |
| `ai_runtime.catalog.manage` | org | 是 | 是 | 否 | `handlers_ai_runtime.go::aiRuntimeDeps(admin=true)` |
| `team.memory.review` | team | 是 | 是 | 否 | `teammemory/team_policy_auth.go::CanReview` |

Disabled org 例外：`requireOrgMember` 对 disabled org 只允许 owner 进入，non-owner 返回
`org_disabled`。因此 disabled 状态是 org access 的额外 gate，不是 role 变化。

### 5.2 Project role

| PermissionKey | Scope | owner | member | 现状证据 |
|---|---|---:|---:|---|
| `project.read` | project | 是 | 是 | `pmRequireProjectInOrg` + PM member-scoped reads |
| `project.write` | project | 是 | 是 | `projectmanager/service::requireProjectMember` |
| `project.member.add` | project | 是 | 是 | `appservices.go::AddProjectMember` 只要求 actor project member |
| `project.member.remove` | project | 是 | 否 | `appservices.go::RemoveProjectMember` owner-only |
| `project.repo_ref.manage` | project | 是 | 是 | `handlers_coderepo.go` project refs 走 PM service |
| `project.stage.manage` | project | 是 | 否 | `projectmanager/service/plan_stage.go` owner checks |

当前 `ProjectMemberRole` 注释明确 v1 role 不做细粒度权限区分；统一契约只冻结现有差异，不引入
额外 project role 语义。

### 5.3 Team 与 Team Memory

| PermissionKey | Scope | Human owner/admin | Human member | Team agent | Curator agent | 现状证据 |
|---|---|---:|---:|---:|---:|---|
| `team.read` | team | 是 | 是 | 不走 Web；MCP 按 own org/team | 不走 Web；MCP 按 own org/team | `handlers_teams.go::teamGuardMember` |
| `team.write` | team | 是 | 是 | 否 | 否 | Web 现状仅 Org member gate；TeamService 无 actor |
| `team.member.manage` | team | 是 | 是 | 否 | 否 | `handlers_teams.go` + `team.Service.AddMemberRoles` |
| `team.project.link.manage` | team | 是 | 是 | 否 | 否 | `handlers_teams.go` association routes |
| `team.memory.read` | team | 是 | 是 | 是，需当前 team member | 是，需当前 team member | `TeamPolicyAuthorization.CanRead` |
| `team.memory.propose` | team | 是 | 否 | 是，需当前 team member | 是，需当前 team member | `CanPropose` |
| `team.memory.review` | team | 是 | 否 | 否 | 是，需 policy `IsCurator` | `CanReview` |
| `team.git.read`, `team.git.write` | team repo | 否，通过 Web 不直连 | 否 | 是，需 team membership | 是，需 team membership | `centergit.Authorizer` |

迁移兼容：Team CRUD 现状是所有 Org member 可做。Phase 1 继续按 `org.member` 派生 `team.write`、
`team.member.manage`、`team.project.link.manage`，并把这一点登记为 legacy compatibility grant。
任何收紧都必须另起产品决策和数据迁移。

### 5.4 Agent / Worker / Internal

| PermissionKey | Scope | 主体 | 现状证据 |
|---|---|---|---|
| `agent.operate.self` | agent | `worker:<id>` token + body/header agent bound to same worker | `requireAgentOnWorker`、`gitAgentResolver` |
| `worker.capability.report` | worker | `worker:<id>` token，owner 必须等于 body worker_id | `worker_report_capabilities.go` |
| `worker.heartbeat` | worker | worker bearer | `workforce.go::workerHeartbeatHandler` |
| `secret.resolve` | secret | bearer scope `secret:resolve` | `admin/api/secret.go` |
| `blob.put` | blob | bearer scope `blob:put` | `admin/api/blob.go` |
| `admin_token.manage` | admin token | bearer scope `admin:token` 或 `*` | `admin/api/admintoken.go` |

## 6. 旧入口到新契约矩阵

| 旧入口 / 结构 | 代码证据 | 当前类别 | 新 PermissionKey / ResourceScope | 迁移方式 |
|---|---|---|---|---|
| Web session cookie `ac_session` | `handlers_auth.go::authMiddleware` | access bootstrap | `org.read` 等后续权限的 authenticated subject | 不迁表；Authorizer 从 session identity 取 `SubjectRef=user:<id>`。 |
| Org membership row | `0033_identity_bc.up.sql::members`；`identity.Member` | membership | `org.read`、role 派生的 org permissions，scope=`org` | Phase 1 派生；保留 `role/status` 表。 |
| `requireOrgMember` | `handlers.go::requireOrgMember` | access gate | `org.read` + disabled-org owner exception | 先包装为 `Authorizer.Check(org.read)`；旧 helper 保留并委托。 |
| Org owner-only API | `handlers_org.go` | access | `org.settings.manage`、`org.lifecycle.manage` | 从 `members.role=owner` 派生。 |
| Human/Agent member add/role/disable | `handlers_members.go` | access + membership write | `org.member.create.*`、`org.member.role.manage`、`org.member.disable` | 从 Org role 派生；MemberService 保持 last-owner invariant。 |
| Invitation role grant | `handlers_invitations.go`；`invitations.role_to_grant` | access + future membership | `org.invitation.manage` | 派生 owner/admin；admin 仍不能 grant owner。 |
| Project membership row | `0041_v27_projectmanager.up.sql::pm_project_members` | membership | `project.read`、`project.write`、`project.member.add`、`project.member.remove`，scope=`project` | 派生；不把 role 扩大为通用 RBAC。 |
| PM AppService write gate | `projectmanager/service::requireProjectMember` | access gate | `project.write` | Authorizer 适配 PM service；错误继续映射 `ErrNotMember`。 |
| Web project org check | `handlers_pm.go::pmRequireProjectInOrg` | access + scope check | `project.read` with parent `org` | 先 `org.read`，再项目 org match，最后 PM membership gate。 |
| Org-wide issue/task aggregation | `handlers_pm_org.go` | org access surface | `org.work_items.read`，scope=`org` | 保持 Org member 可读；不得误改为 project-member-only。 |
| Team rows | `0107_v229_teams.up.sql` | resource + membership | `team.read`、`team.write`、`team.member.manage`、`team.project.link.manage` | Phase 1 继续从 Org member 派生 Web team manage；`team_members` 仅用于 roster/memory/git。 |
| Team role config | `team_roles(cli,model,capability_tags,max_concurrency)`；`team/types.go::RoleConfig` | runtime capability | `team.runtime_config.manage` 仅表示谁能编辑配置；配置本身无 access grant | 不把 `capability_tags` 迁为 permission；只迁 UI 文案和 authorizer 输入。 |
| Team member row | `team_members(member_ref,member_kind,role)` | membership + runtime roster | `team.membership.current`、`team.memory.read`、`team.memory.propose`、`team.memory.review`、`team.git.read`、`team.git.write` 派生证据 | 保留 PK `(team_id,member_ref,role)`；agent one-team 约束不变。 |
| Team Memory policy | `team_memory_policies`、`team_memory_policy_curators` | access policy | `team.memory.propose`、`team.memory.read`、`team.memory.review` | 由 `TeamPolicyAuthorization` 派生；curator 必须是当前 agent team member。 |
| Conversation participants JSON | `conversations.participants`、`ParticipantElement` | membership | `conversation.read`、`conversation.post`，scope=`conversation` | 派生 active participant；task/issue/plan conversation 可叠加 project-member read/post。 |
| File references | `file_references(scope,scope_id)`；`files.Service.Reachable` | reachability membership | `file.upload`、`file.download`、`file.attach`，scope=`file` + live placement refs | 不把 `ScopeProject` 等当全局权限；caller scopes 由 human/agent adapter 解析。 |
| Agent file own-domain | `agent_tools_files.go::agentOwnDomainScopes` | access derivation | `file.download`、`file.attach` | 继续从 assigned tasks、active conversations、project-member task/issue/plan scopes 派生。 |
| Admin token scopes JSON | `admin_tokens.scopes_json`、`AuthContext.HasScope` | bearer access | `BearerScope` -> PermissionKey adapter | 保留 `*` 与 exact match；新增 registry 映射 `secret:resolve -> secret.resolve` 等。 |
| Worker long-term token scopes | `workforce.go::workerLongTermTokenScopes` | bearer access | `worker.heartbeat`、`worker.capability.report`、`dispatch.pull`、`task.internal.report` | 加入 owner check + bearer scope 双重判定；先兼容已有 token scope 集。 |
| Worker capability upload | `worker_report_capabilities.go` | access + runtime fact write | `worker.capability.report`，scope=`worker` | 保留 owner=`worker:<id>` 等值检查；写入 `RuntimeCapability` 证据。 |
| `requireAgentOnWorker` | `agent_tools.go` | access gate | `agent.operate.self`，scope=`agent` | Authorizer 统一 worker owner + agent worker binding；所有 MCP/admin agent routes 必须调用。 |
| MCP task/issue tools | `agent_tools_passthrough.go`、`agent_tools_issues.go` | access + project membership | `task.read`、`task.write`、`issue.read`、`issue.write` | 先 `agent.operate.self`，再 PM project membership / creator / assignee gate。 |
| MCP own-work writes | `agent_tools_write.go::requireOwnTask` | access | `task.complete.self`、`task.block.self` | 保持 Task.Assignee == `agentActor(a)`。 |
| MCP profile capability lists | `agent_tools_profile.go::projectMemberCapabilities` / `orgAgentCapabilities` | descriptive projection | 无直接 permission grant | 输出可保留，但必须标为 derived display；不能作为 allow source。 |
| MCP model catalog legacy writes | `agent_tools_model_catalog.go` | access legacy exception | `model_catalog.manage`，scope=`org` | 现状是 own-org operating agent 可写；迁移登记为 legacy grant，AI Runtime 后续应改为 owner/admin 或显式 automation grant。 |
| Web AI Runtime catalog | `handlers_ai_runtime.go::aiRuntimeDeps` | access | `ai_runtime.catalog.read`、`ai_runtime.catalog.export`、`ai_runtime.catalog.manage` | member 可读/export；owner/admin 可 import/apply/write。 |
| Workspace code repo | `handlers_coderepo.go::requireOrgAdmin` | access | `coderepo.workspace.manage`、`coderepo.workspace.read` | owner/admin manage；member read；project refs 走 project membership。 |
| MCP repo info | `agent_tools_coderepo.go` | access + project membership | `coderepo.project_ref.read` | `agent.operate.self` + project member；credentials 永不返回。 |
| Center git smart HTTP | `git_backend.go`、`centergit.Authorizer` | access | `git.global.read`、`git.agent.read.self`、`git.agent.write.self`、`team.git.read`、`team.git.write` | 保持 global read-only、agent repo owner-only、team repo team-member-only。 |

## 7. API 协议

### 7.1 Authorizer 输入

所有 Web/MCP/internal adapter 最终构造同一输入：

```ts
type CheckRequest = {
  subject_ref: SubjectRef
  transport: "web" | "mcp" | "admin_http" | "git_http" | "system"
  bearer_scope?: string
  permission: PermissionKey
  resource: ResourceScope
  request_id: string
}
```

判定顺序：

1. 认证：Web session、admin bearer、git header、system actor。
2. 主体绑定：worker token 只能代表自身 worker；agent tool 还必须证明 agent bound to worker。
3. 资源 scope 解析：id -> resource -> parent org/project/team；跨 org unknown 统一 404。
4. permission 派生：从 membership、role、policy、scope refs、bearer scope 派生。
5. runtime gate：在 access 通过后，单独计算 capability/concurrency/schedulability。

### 7.2 错误协议

| 状态 | 语义 | 例子 |
|---|---|---|
| 401 | 未认证或 bearer 无法解析 | no session、missing bearer、git no agent |
| 403 | 已认证但缺 permission 或 owner binding 不匹配 | `scope_forbidden`、`worker_mismatch`、`not_a_project_member` |
| 404 | 资源不存在或跨 scope 不可披露 | cross-org project/repo/team、unseeable proposal |
| 409 | 状态冲突、version/CAS、archived/disabled/write conflict | `team_memory_version_conflict`、archived task |
| 422 | 请求语义有效但跨域业务约束失败 | cross-org assignee |

`permission_denied` 响应应包含 `permission`、`resource.kind`、`reason`，但不能泄漏跨 org 资源 id 是否存在。

### 7.3 新 endpoint 约定

`GET /api/orgs/{slug}/permissions/effective` 用于 Web 展示和测试，不作为业务写入口：

```json
{
  "subject_ref": "user:abc",
  "resource": { "kind": "team", "id": "team-123" },
  "permissions": [
    { "key": "team.read", "source": "org_role", "evidence_ref": "members:mem-1" },
    { "key": "team.memory.review", "source": "org_role", "evidence_ref": "members:mem-1" }
  ]
}
```

业务 handler 不得根据前端传来的 effective permissions 决策；必须服务端重新 `Check`。

## 8. 数据迁移协议

### 8.1 Phase 0：registry 与只读派生

新增 permission registry，但不新建授权表：

```ts
type PermissionDefinition = {
  key: PermissionKey
  category: "access"
  resource_kinds: string[]
  actions: string[]
  legacy_sources: string[]
}
```

每个 legacy source 必须有测试覆盖：

- Org role -> org permissions。
- Project member -> project permissions。
- Team member/policy -> team memory/git permissions。
- Admin bearer scope -> internal permissions。
- File caller scopes -> file reachability。

### 8.2 Phase 1：adapter 委托

保持旧函数名，内部委托 Authorizer：

- `requireOrgMember` -> `Check(org.read)` + 回传现有 `Identity/Member`。
- `pmRequireProjectInOrg` -> `Check(project.read)` + project org match。
- `RequireScope` -> `Check(<mapped internal permission>)`，保留 `*`。
- `requireAgentOnWorker` -> `Check(agent.operate.self)`。
- `fileReachableForHuman` / `agentOwnDomainScopes` -> `Check(file.*)` 的 scope resolver。

### 8.3 Phase 2：可选持久 grant 表

只有当产品需要自定义 grant/deny 时才新增表；第一阶段不得为了“统一”而复制旧 membership：

```sql
CREATE TABLE permission_grants (
  id TEXT PRIMARY KEY,
  subject_ref TEXT NOT NULL,
  permission_key TEXT NOT NULL,
  resource_kind TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  org_id TEXT,
  source TEXT NOT NULL,
  starts_at TEXT,
  expires_at TEXT,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  revoked_at TEXT,
  version INTEGER NOT NULL DEFAULT 1
);
```

禁止把 `members`、`pm_project_members`、`team_members` 原样复制为 grants；它们仍是各自 BC 的权威
membership。

### 8.4 Legacy scope 映射

| BearerScope | PermissionKey | 资源 scope | 兼容说明 |
|---|---|---|---|
| `*` | all internal permissions | request resource | 保留 superuser，仅 admin/system token 使用。 |
| `admin:token` | `admin_token.manage` | admin token | 现有 `admintoken.go`。 |
| `secret:resolve` | `secret.resolve` | secret ref | 现有 `secret.go`。 |
| `blob:put` | `blob.put` | blob | 现有 `blob.go`。 |
| `dispatch:pull` | `dispatch.pull` | worker queue | worker loop 兼容。 |
| `task:*` | `task.internal.*` | task/work item | worker task report/kill/feedback 兼容；不能跳过 agent/project checks。 |
| `workforce:enroll` | `worker.enroll`、`worker.heartbeat` | worker | worker long-term token 兼容。 |

## 9. 验收与回归

必须增加或保留以下校验：

1. 静态路由校验：所有 `/api/orgs/{slug}/...` handler 走 `requireOrgMember` 或新 Authorizer；所有
   `/admin/agent-tools/...` 走 `requireAgentOnWorker`。
2. 单元测试：Org owner/admin/member permission 派生、disabled org owner exception、project owner/member
   差异、team memory curator、admin bearer wildcard/exact match。
3. MCP 回归：跨 worker agent 403；项目外 agent `create_task/get_issue/list_project_repos` 被拒；own task
   `complete_task/block_task` 仍要求 assignee。
4. Runtime 回归：`capability_tags` 只影响 auto-assign；携带 tag 不能获得 team memory review 或 project access。
5. File 回归：live reference + caller scope 才可下载；soft-deleted reference 不授 access；agent project-member
   conversation attachment 可达但 `ScopeProject` 不被 agent domain 误授。
6. Migration 回读：从旧表派生的 effective permissions 与旧 helper 行为一致；不能只看日志或响应 ok。

## 10. 推进顺序

1. 冻结本规格与 ADR。
2. 建 permission registry、legacy derivation tests、effective permission debug API。
3. 用 Authorizer 包装现有 Web/MCP/admin helper，保持外部行为。
4. 将 handler 中散落的 role/scope 判断替换为 `Check`，保留 domain invariant。
5. 若需要自定义 grant，再引入 `permission_grants`，并逐条给出从产品规则到表迁移的验收。
