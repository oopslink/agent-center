# T1435 双 Profile / Team RAM Access 冻结合同

| 字段 | 值 |
|---|---|
| 状态 | Frozen executable contract |
| 日期 | 2026-08-20 |
| 审计基线 | `origin/main@05de26039c389be3bcb4faed4f759d972db80e0b` |
| 范围 | AI Runtime Profile 删除线、Access Profile 保留线、Team Role -> RAM Role、Access IA、截图验收 |
| 前置文档 | `docs/design/features/ai-runtime-configuration.md`、`docs/design/features/unified-permission-contract.md`、`docs/design/features/2026-08-17-access-management-redesign.md` |

## 1. 双 Profile 冻结边界

### 1.1 AI Runtime Profile 是已删除对象

AI Runtime Profile 指旧 `ai_runtime_profiles` 表、`ai_runtime_catalogs.default_profile_id`、导入导出的 `runtime.profiles` / `runtime.default_profile_key`，以及“Agent / Team / Org default 通过 Runtime Profile 间接选择 CLI+Model+Parameters”的设计。

冻结规则：

- 生产 Schema 不允许存在 `ai_runtime_profiles` 表。
- 生产 Schema 不允许 `ai_runtime_catalogs.default_profile_id` 承载非空绑定。
- AI Runtime import 必须拒绝 `runtime.profiles` 和 `runtime.default_profile_key`。
- Agent desired runtime、executor candidates、Team Role runtime config 必须直接保存 CLI / Model / parameters / concurrency，不得恢复 Runtime Profile 绑定。
- Runtime Profile 相关文档命中只能作为历史、测试拒绝、迁移 down 或清退说明存在。

执行点：

- `internal/persistence/migrations/0126_remove_ai_runtime_profiles.up.sql` 先检查 `ai_runtime_profiles` 行数和非空 `default_profile_id`，不为 0 则迁移失败；然后 drop 表和列。
- `internal/webconsole/api/handlers_ai_runtime.go::rejectRetiredRuntimeProfileFields` 对导入文档硬拒绝 retired 字段。
- `web/src/lib/runtimeSelector.ts` 只消费 Catalog 的 CLI / Model 与现有 Agent runtime 字段。

### 1.2 Access Profile 是当前保留对象

Access Profile 指 Access 管理域内的可版本化权限包：`access_profiles` + `access_profile_versions`。它被 UI 作为 RAM Role catalog 展示，并通过 `0136_access_profile_ram_role_contract` 镜像为 `authorization_roles` system/custom rows，供 Team Role -> RAM Role 映射解析。

冻结规则：

- Access Profile 不得因 AI Runtime Profile 清退而删除。
- Access Profile 的 ID 在 Team RAM mapping API 中就是可提交的 RAM Role ID。
- 内置 `team-basic` / `team-contributor` / `team-curator` 必须同时存在于 `access_profiles`、`access_profile_versions`、`authorization_roles`、`authorization_role_permissions`。
- org-owned Access Profile 可以新增版本和 disable；system built-ins 不允许通过 UI 发布新版本或 disable。
- Access Profile 只定义权限包，不直接绑定 subject；subject 授权仍通过 Team Role 映射或 explicit role assignment。

执行点：

- `internal/persistence/migrations/0132_access_profile_versions.up.sql` 创建 Access Profile 版本表并 seed built-ins。
- `internal/persistence/migrations/0136_access_profile_ram_role_contract.up.sql` 将 built-in Access Profile 镜像成 system authorization roles。
- `internal/webconsole/api/handlers_access.go` 暴露 `/access/profiles` CRUD/version/disable。
- `web/src/pages/Access.tsx` Profiles tab 管理 Access Profile，Roles & mappings tab 把 Access Profile 当 RAM Role catalog。

## 2. Team Role -> RAM Role 契约

### 2.1 领域语言

| 名称 | 权威含义 | 权威结构 |
|---|---|---|
| Team Role | Team 内的功能角色，携带 runtime config 和声明式需求；本身不授予权限 | `team_roles`、`team.RoleConfig` |
| RAM Role | 权限包，持有 `authorization_role_permissions` | `authorization_roles`、`access_profiles` 投影 |
| Mapping | 一个 Team Role 绑定 0..N 个 RAM Role | `team_role_ram_role_mappings` |
| Mapping version | CAS 写入版本 | `team_role_ram_role_versions` |
| Mapping audit | 每次替换的前后角色集、actor、版本 | `team_role_ram_role_audit_events` |

硬规则：

- Team Role 的 `cli`、`model`、`capability_tags`、`max_concurrency` 是 runtime config，不是 access grant。
- Team Role 的 `ram_role_keys` 是模板/create/update 可移植输入，按 Access Profile / authorization role name 解析。
- Team Role RAM mapping 的写 API 使用 `ram_role_ids`，并要求 `expected_version`。
- 任何 mapping 替换必须是全量 replace，不允许 patch append。
- 角色 ID 去重、排序、空值清理由 service 层统一完成。

### 2.2 数据与迁移

| 表 | 生产者 | 消费者 | 不变量 |
|---|---|---|---|
| `team_role_ram_role_mappings` | `team.Service.ReplaceRAMRoleMapping`、Team create/update 的 RAM key resolver | `authorization.Service.addTeamRAMEffective`、Web Access/Team pages | FK 到 declared Team Role；FK 到 active authorization role；按 `(team_id, team_role, ram_role_id)` 唯一 |
| `team_role_ram_role_versions` | mapping replace / Team role persist | Web PUT CAS、cache version hash | version > 0；每次 replace +1 |
| `team_role_ram_role_audit_events` | mapping replace | audit/report 验证 | 保存 previous/next role ids 与 previous/next version |
| `access_profiles` | Access Profile API / migration seed | `/access/profiles`、RoleBuilder、Access page | active name 在 org 内唯一；system built-ins org_id 为空 |
| `authorization_roles` | unified authorization migration、Access role API、0136 built-in mirror | `validateRAMRoles`、`addTeamRAMEffective`、custom direct binding | revoked role 不可被 mapping 使用 |

### 2.3 API 合同

| API | 作用 | 鉴权 | 请求 | 响应/错误 |
|---|---|---|---|---|
| `GET /api/orgs/{slug}/teams/{id}/roles/{role}/ram-roles` | 读取 mapping | org member + team in org | none | `TeamRAMRoleMapping`，missing role -> `404 team_role_not_found` |
| `POST /api/orgs/{slug}/teams/{id}/roles/{role}/ram-roles/preview` | 预览 replace 影响 | org member + team in org | `{ram_role_ids}` | `affected_members`、`affected_project_ids`、added/removed role ids |
| `PUT /api/orgs/{slug}/teams/{id}/roles/{role}/ram-roles` | CAS 全量替换 | `team.runtime_config.manage` on team | `{ram_role_ids, expected_version}` | `200` new mapping；stale -> `409 version_conflict`；cross-org/dangling role -> `422 invalid_ram_role` |
| `GET /api/orgs/{slug}/access/profiles` | RAM Role catalog | org member | none | active latest profile list |
| `POST /api/orgs/{slug}/access/profiles` | 创建 org-owned Access Profile | owner/admin | `{name, description, permissions}` | `201` detail；unknown permission -> `422 invalid_access_profile` |
| `POST /api/orgs/{slug}/access/profiles/{id}/versions` | 发布新版本 | owner/admin；system built-in forbidden | `{expected_latest_version, permissions}` | CAS stale -> `409 version_conflict` |
| `POST /api/orgs/{slug}/access/profiles/{id}/disable` | disable org-owned profile | owner/admin；system built-in forbidden | none | `204` |

### 2.4 Scope 与多 Project

`authorization.Service.addTeamRAMEffective` 是 Team RAM 生效的唯一授权消费者。

Scope 规则：

- `team` resource：只在 `resource.id == team_id` 时生效。
- `project` resource：Team 必须通过 `team_projects` 关联该 project。
- `task` / `issue` / `plan` resource：必须带 `project_id`，且该 project 必须关联 Team。
- `conversation` resource：必须解析 owner 为 project/task/issue/plan conversation，且 `project_id` 关联 Team；`project.read` / `task.read` / `issue.read` / `plan.read` 可派生 `conversation.read`。
- 同一个 Team 关联多个 Project 时，mapping 同时在所有 linked projects 生效；preview 必须列出全部 `affected_project_ids`。
- 取消 project link、移除 Team member、删除 mapping 必须立即 fail closed。

### 2.5 Direct Binding 与并存

显式 direct binding 是 `authorization_role_assignments`，来源为 `custom_role`。它与 Team RAM 并存但互不覆盖：

- effective permissions 是 legacy/domain source、Team RAM、custom role direct assignment 的并集。
- direct binding 只能由授权服务 `ApplyBatch` 写入，必须有 idempotency key。
- revoke direct binding 只能走两阶段 preview/confirm；直接 revoke endpoint 必须拒绝。
- revoke 一个 direct binding 后，如果 Team RAM 仍授予同 permission，则 effective permission 仍 allowed，但 evidence/source 必须显示真实来源。
- Team RAM mapping 删除后，如果 direct binding 仍授予同 permission，则不能误判为 denied。

### 2.6 Cache、Shadow、Audit、Rollback

| 切面 | 合同 |
|---|---|
| Cache | `effectiveVersion` 必须纳入 `authorization_role_assignments`、`authorization_roles`、`authorization_role_permissions`、`team_members`、`team_projects`、`team_role_ram_role_mappings`、`team_role_ram_role_versions`、`team_memory_policy_curators`。授权写入后必须 `invalidateEffectiveCache()`。 |
| Shadow | 默认 enforcement mode 是 `shadow`；Team RAM 必须进入 legacy/equivalent 双侧比较，避免 enforce 模式误删有效 mapping grant。 |
| Audit | direct grant/revoke 写 `authorization_audit_events`；Team RAM replace 写 `team_role_ram_role_audit_events`。审计 payload 不得省略 actor、subject/resource 或 previous/next。 |
| Rollback | 授权模式可回退到 `legacy`；DB rollback 可 drop 0132/0133/0136 表，但会丢失 Access Profile 与 Team RAM mapping 数据。产品 rollback 前必须导出 mapping/profile 快照。 |
| Migration | 0129 建统一授权核心；0130 建 revoke preview；0132 建 Access Profile；0133 建 Team RAM mapping；0136 镜像 built-in Access Profile 为 system authorization roles。不得把 AI Runtime Profile 0126 rollback 与 Access Profile 0132 rollback 混用。 |

## 3. Access IA 冻结

Access 的一屏 IA：

1. Header：页面标题、`Batch grant` 操作；无 `org.member.role.manage` 时展示 forbidden 解释，并禁用所有写控件。
2. Summary：Allowed / High risk / Expiring / No access / Not applicable。
3. Filters：Search、Resource、Risk、Status。
4. Tabs：
   - `Roles & mappings`：默认页，显示 RAM Roles catalog 与 Team Role mappings。
   - `Subject access`：按 subject 展开 source chain、decision table、direct/other bindings。
   - `Profiles`：管理 Access Profile 和版本历史。
5. Side panels：
   - 在 `Subject access` 显示 Role management 与 Grant revoke。
   - 全局 Batch grant drawer 从 header 打开。

Access 不是 AI Runtime 页面，也不是 Team settings 的替代页：

- Runtime CLI/Model/Catalog 仍在 AI Runtime 页面。
- Team Role runtime config 仍在 Team detail / RoleBuilder。
- Access 只管理权限包、mapping、direct grants/revoke 和 explain。

## 4. 逐页逐状态截图验收矩阵

截图必须来自真实导航路径，不允许只开孤儿 URL。每张截图保存到后续验收 commit 的 `docs/releases/<version>-screenshots/access/`，报告内联引用。

| ID | 页面/入口 | 状态 | 必须可见 | 导航路径 | 建议文件名 |
|---|---|---|---|---|---|
| A1 | Access | no permission | forbidden alert、后端 reason、Batch grant disabled/不可达 | Sign in member -> sidebar Access | `access-forbidden.png` |
| A2 | Access | loading | Summary/表格 skeleton，无布局跳动 | Sign in admin -> sidebar Access，延迟 `/access/overview` | `access-loading.png` |
| A3 | Access Roles & mappings | happy | RAM Roles 卡片、Team Role mappings、mapping version、Used by Team Roles | Access -> Roles & mappings | `access-roles-mappings.png` |
| A4 | Access Roles & mappings | preview changed | preview impact 显示 members、+/- roles、projects | 修改 mapping -> Preview impact | `access-mapping-preview.png` |
| A5 | Access Roles & mappings | save success | version +1、draft 清空、query cache 刷新 | Preview -> Save mapping | `access-mapping-saved.png` |
| A6 | Access Roles & mappings | stale conflict | `version_conflict` 错误 inline 显示，不落部分写 | 用旧 expected_version 保存 | `access-mapping-conflict.png` |
| A7 | Access Subject access | source chain | `membership:<team>` -> Team Role -> RAM Role -> scoped effective permissions | Access -> Subject access -> expand subject | `access-subject-chain.png` |
| A8 | Access Subject access | direct coexists | Direct/other bindings 行与 Team RAM source 并存 | 有 direct grant + Team RAM 的 subject | `access-direct-coexist.png` |
| A9 | Access grant drawer | preview | grantable/high risk/unauthorized/not applicable summary | Batch grant -> select subject/permission/resource -> Preview | `access-grant-preview.png` |
| A10 | Access revoke | derived grant | derived grant revoke preview 显示 not_applicable，不能直接 revoke | Subject access -> revoke derived grant | `access-derived-revoke-blocked.png` |
| A11 | Access revoke | direct confirm | preview_id/token 二阶段，confirm 后 grant revoked，audit 可查 | Revoke direct grant -> Confirm | `access-direct-revoke-confirmed.png` |
| A12 | Profiles | list/detail | built-ins、latest version、version history、risk badges | Access -> Profiles | `access-profiles.png` |
| A13 | Profiles | create | create form、permission checklist、created profile selected | Profiles -> Create profile | `access-profile-created.png` |
| A14 | Profiles | version CAS conflict | stale expected_latest_version 显示 conflict，不写新版本 | Profiles -> Publish with stale version | `access-profile-version-conflict.png` |
| A15 | Team detail | role builder mapping | RAM role multi-select、save 后 role `ram_role_keys` 从 server 回读 | Teams -> Team detail -> edit roles | `team-role-ram-builder.png` |
| A16 | AI Runtime | retired profile guard | export/import 不含 profiles/default_profile_key；导入 retired 字段报错 | System -> AI Runtime -> Import preview | `ai-runtime-profile-retired-guard.png` |

## 5. 验证命令

```bash
git fetch origin main
git rev-parse origin/main

rg -n "runtime\\.profiles|default_profile_key|ai_runtime_profiles|AI Runtime Profile|Runtime Profile" internal docs web sites README*
rg -n "access_profiles|access_profile_versions|access/profiles|AccessProfile|access profile|Access profiles" internal docs web sites README*
rg -n "team_role_ram_role|ram_role_ids|ram_role_keys|SourceTeamRoleRAM|RAM Role|RAM roles" internal docs web sites README*

go test ./internal/persistence ./internal/airuntime ./internal/authorization ./internal/team/service ./internal/webconsole/api
(cd web && pnpm test -- --run src/pages/Access.test.tsx src/pages/TeamDetail.test.tsx src/api/access.ts src/api/teams.test.tsx)
```

