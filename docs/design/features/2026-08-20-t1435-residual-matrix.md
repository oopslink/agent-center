# T1435 全量残留矩阵

| 字段 | 值 |
|---|---|
| 状态 | Audit matrix / deletion readiness |
| 日期 | 2026-08-20 |
| 审计基线 | `origin/main@05de26039c389be3bcb4faed4f759d972db80e0b` |
| 配套合同 | `docs/design/features/2026-08-20-t1435-dual-profile-ram-access-contract.md` |

## 1. AI Runtime Profile 残留

删除目标只覆盖 AI Runtime Profile，不覆盖 Agent product profile、Worker Profile tab、Access Profile。

| 残留 | 分类 | 生产者 | 消费者 | 数据迁移 | 删除条件 | 验证命令 |
|---|---|---|---|---|---|---|
| `internal/persistence/migrations/0116_ai_runtime_catalog.up.sql` 创建 `ai_runtime_profiles` 与 `default_profile_id` | 历史迁移源 | migration 0116 | migration chain / tests | 0126 清退 | 不能删除已发布迁移；只允许被 0126 抵消 | `rg -n "ai_runtime_profiles|default_profile_id" internal/persistence/migrations` |
| `internal/persistence/migrations/0126_remove_ai_runtime_profiles.up.sql` | 生产删除线 | migration 0126 | migrator | guard 要求旧表空、default 空，然后 drop | 永久保留，作为删除合同 | `go test ./internal/persistence -run TestMigration0116` |
| `0126_remove_ai_runtime_profiles.down.sql` | rollback-only | migrator down | rollback | 还原旧表/列但不回填生产数据 | 只有废弃 rollback 支持时可删 | `rg -n "0126_remove_ai_runtime_profiles" internal/persistence` |
| `internal/webconsole/api/handlers_ai_runtime.go::rejectRetiredRuntimeProfileFields` | 生产拒绝 | AI Runtime import decode | import preview/apply | 无数据写入；拒绝 retired 字段 | AI Runtime import schema 升级且外部合同废弃 retired guard 后 | `go test ./internal/webconsole/api -run TestAIRuntime` |
| `internal/webconsole/api/handlers_ai_runtime_test.go` retired field cases | 测试 | test fixture | API guard | 无 | guard 删除时同步删 | `rg -n "default_profile_key|runtime\\.profiles" internal/webconsole/api` |
| `web/src/pages/AiRuntime.test.tsx` export 不含 `default_profile_key` | 前端测试 | UI export request | AI Runtime page | 无 | AI Runtime export UI 重写时用等价测试替代 | `cd web && pnpm test -- --run src/pages/AiRuntime.test.tsx` |
| `web/src/lib/runtimeSelector.ts` 注释与测试 | 生产防回归 | selector | Agent/Runtime selectors | 无 | selector 不再服务 legacy Agent runtime 时 | `cd web && pnpm test -- --run src/lib/runtimeSelector.test.ts` |
| `docs/design/features/ai-runtime-configuration.md` | 文档历史/当前合同 | design doc | implementer/acceptance | 无 | 新版设计完全替代且迁移窗口结束 | `rg -n "Runtime Profile|default_profile_key|profiles" docs/design/features/ai-runtime-configuration.md` |
| `docs/plans/2026-07-22-ai-runtime-configuration-implementation.md` | 历史计划 | archived plan | audit context | 无 | 只读历史，不清理 | `rg -n "Profile" docs/plans/2026-07-22-ai-runtime-configuration-implementation.md` |
| `docs/plans/reports/t1315-runtime-delivery-integration.md` | 历史报告 | integration report | audit context | 无 | 只读历史，不清理 | `rg -n "AI Runtime Profile" docs/plans/reports` |
| `internal/cli/handlers_migrate_v1_to_v2.go` 注释 | 迁移顺序说明 | CLI migrate | migration operator | 无 | 注释失真时更新 | `rg -n "retires AI Runtime Profile" internal/cli` |

删除验收：

- `sqlite_master` 中无 `ai_runtime_profiles`。
- `ai_runtime_catalogs.default_profile_id` 不存在或仅在 down rollback。
- Import retired field 返回 400。
- Runtime selector 不读取 Runtime Profile 字段。

## 2. Access Profile 残留

这些不是待删残留，是 Access/RAM 合同的保留面。

| 残留 | 分类 | 生产者 | 消费者 | 数据迁移 | 删除条件 | 验证命令 |
|---|---|---|---|---|---|---|
| `access_profiles` | 保留生产表 | Access Profile API、0132 seed | `/access/profiles`、RoleBuilder、Access Roles catalog | 0132 创建并 seed built-ins | 不得因 AI Runtime Profile 清理删除；只有 Access Profile 产品废弃才删 | `rg -n "CREATE TABLE IF NOT EXISTS access_profiles" internal/persistence/migrations` |
| `access_profile_versions` | 保留生产表 | create/version API、0132 seed | profile detail/list | 0132 创建，latest join | 同上 | `go test ./internal/webconsole/api -run TestAccessProfilesPersistVersionsAndCAS` |
| `team-basic/team-contributor/team-curator` in Access Profile | built-in RAM catalog | 0132 | Access page, Team role builder | 0132 seed | 需要替代 built-in RAM catalog 后才能迁移 | `go test ./internal/team/service -run TestBuiltInAccessProfileRAMRoleContract` |
| `0136_access_profile_ram_role_contract.up.sql` | bridge migration | migration | `validateRAMRoles` / `addTeamRAMEffective` | 镜像 built-ins 到 authorization roles | 只有 Access Profile 与 auth roles 完全统一存储后可收敛 | `rg -n "access profile ids as RAM role ids" internal/persistence/migrations` |
| `internal/webconsole/api/handlers_access.go` profile handlers | 生产 API | Web Access page | Access Profile store | create/version/disable | Access IA 移除 Profiles tab 后 | `rg -n "accessProfile.*Handler|accessListProfiles|accessCreateProfile" internal/webconsole/api/handlers_access.go` |
| `web/src/api/access.ts` AccessProfile types/hooks | 前端 API | React Query hooks | Access page / RoleBuilder | 无 | API 删除后 | `rg -n "AccessProfile|useAccessProfiles" web/src` |
| `web/src/pages/Access.tsx::AccessProfilesView` | 产品 UI | Access page | human admins | 无 | Profiles tab 由新 IA 替代后 | `cd web && pnpm test -- --run src/pages/Access.test.tsx` |
| `web/src/components/teams/RoleBuilder.tsx` | Team create/edit selector | Access profile query | Team detail/create | ram_role_keys 可移植保存 | Team Role mapping UI 全部迁到 Access 后可降级为只读 | `rg -n "useAccessProfiles|ram_role_keys|ram_role_ids" web/src/components/teams web/src/pages/TeamDetail.tsx` |
| `web/src/mocks/*` Access/Profile handlers | dev/test mock | MSW | UI tests/manual dev | mock data only | 生产 API 删除后 | `rg -n "access/profiles|ramRole" web/src/mocks` |

保留验收：

- `/api/orgs/{slug}/access/profiles` 返回 built-ins。
- 创建 org-owned profile 后可发布新版本；stale version 返回 409。
- system built-ins 不允许 UI version/disable。

## 3. Team Role -> RAM Role 残留

| 残留 | 分类 | 生产者 | 消费者 | 数据迁移 | 删除条件 | 验证命令 |
|---|---|---|---|---|---|---|
| `team_roles.ram_role_keys` / `RoleConfig.RAMRoleKeys` | portable declaration | Team template/create/update/API/tool | resolver -> mapping tables；UI display | 0131/0133 后写入映射 | 如果模板合同改为只存 role IDs，需迁移所有 template payload | `rg -n "RAMRoleKeys|ram_role_keys" internal/team internal/webconsole/api web/src` |
| `team_role_ram_role_mappings` | production mapping | service replace / repo persist | authorization effective, Access page, Team detail | 0133 创建 | 只有 Team RAM 授权模型废弃后 | `rg -n "team_role_ram_role_mappings" internal` |
| `team_role_ram_role_versions` | CAS/cache | service replace / repo persist | PUT expected_version、effective cache hash | 0133 创建 | mapping 表删除时一起删除 | `rg -n "team_role_ram_role_versions" internal` |
| `team_role_ram_role_audit_events` | audit | service replace / repo persist | acceptance/audit | 0133 创建 | audit 留存期结束且 mapping 废弃 | `rg -n "team_role_ram_role_audit_events" internal` |
| `internal/team/service/ram_roles.go` | application service | Web/API/tool workflows | Team Service callers | 无 | 被同等 service 替代 | `go test ./internal/team/service -run RAMRole` |
| `internal/team/sqlite/repo.go` RAM persist helpers | repository writer | create/update team roles | Team service | 无 | service 不再 persist from role keys | `rg -n "ram_role" internal/team/sqlite/repo.go` |
| `internal/webconsole/api/handlers_team_ram_roles.go` | Web API | Access/Team pages | Team service | 无 | mapping writes removed from product | `go test ./internal/webconsole/api -run TeamRAMRole` |
| `internal/authorization/service.go::addTeamRAMEffective` | authorization consumer | Team RAM mapping | Check/Explain/ListEffective | effective cache includes mapping/version | Only after replacement consumer ships | `go test ./internal/authorization -run 'TeamRAM|Shadow|Effective'` |
| `web/src/pages/Access.tsx` mapping UI | product UI | Access page | Team RAM APIs | 无 | New Access graph UI replaces it | `cd web && pnpm test -- --run src/pages/Access.test.tsx` |
| `web/src/pages/TeamDetail.tsx` mapping edit | product UI | Team detail | Team RAM APIs | 无 | Team detail becomes runtime-only and Access owns mapping | `cd web && pnpm test -- --run src/pages/TeamDetail.test.tsx` |
| `internal/admin/api/agent_tools_team.go` `ram_role_keys` | MCP/team tooling | agent team tools | Team service/template | keys resolve to mapping | tool schema version drops RAM keys | `go test ./internal/admin/api -run Team` |
| `docs/acceptance/t1413-s3-ram-write-auth.md` | acceptance reference | test plan | reviewer | 无 | superseded by new acceptance report | `rg -n "RAM" docs/acceptance` |

删除验收：

- Removing a mapping immediately denies Team RAM-derived project/team/conversation permissions unless another source grants them.
- Project unlink and Team member removal also fail closed.
- Direct custom role assignments continue independently.

## 4. Authorization / Direct Binding / Shadow 残留

| 残留 | 分类 | 生产者 | 消费者 | 数据迁移 | 删除条件 | 验证命令 |
|---|---|---|---|---|---|---|
| `permission_definitions` | registry | 0129 seed / service definitions | Access catalog, auth checks | 0129 | never while unified auth exists | `go test ./internal/authorization -run Definitions` |
| `authorization_roles` | system/custom roles | migrations, role API | direct assignments, RAM mapping | 0129/0134/0136 | never while unified auth exists | `rg -n "authorization_roles" internal/authorization internal/persistence/migrations` |
| `authorization_role_permissions` | permission bundles | migrations, role update | effective permissions | 0129/0134/0136 | same as roles | `rg -n "authorization_role_permissions" internal` |
| `authorization_role_assignments` | direct binding | `ApplyBatch` | `addCustomEffective`, Access grants | 0129 | direct grants feature removed | `go test ./internal/authorization -run Revoke` |
| `authorization_revoke_previews` | two-phase revoke | revoke preview | revoke confirm | 0130 | direct revoke UI/API removed | `go test ./internal/authorization -run RevokePreview` |
| `authorization_audit_events` | audit | ApplyBatch/RevokeBatch | reports/tests | 0129 | unified auth removed | `rg -n "authorization_audit_events" internal` |
| `effectiveCache` | performance/cache correctness | `ListEffective` | `Check/Explain` | no schema | cache implementation replaced | `rg -n "effectiveCache|effectiveVersion|invalidateEffectiveCache" internal/authorization/service.go` |
| `EnforcementShadow` | migration safety | service config | shadow metrics endpoint | no schema | enforce mode stable with zero mismatches window | `go test ./internal/authorization -run Shadow` |
| `/permissions/shadow` | observability | Web API | operators/test | no schema | shadow mode removed | `go test ./internal/webconsole/api -run Shadow` |

验收重点：

- default mode is shadow.
- legacy rollback mode does not record shadow metrics.
- enforce mode fails closed after mapping revoke.
- cross-org revoke is opaque 404 and does not mutate foreign rows.
- last org owner cannot be revoked.

## 5. Access IA / Product Screenshot Matrix

| Surface | States | Data producer | Consumer | Screenshot IDs |
|---|---|---|---|---|
| Access permission gate | loading / no permission / manage allowed | `/permissions/effective`, `/permissions/explain` | `Access.tsx` | A1, A2 |
| Access summary/filter | all / filtered empty / error | `/access/overview` | summary tiles, filters | A2, A3 |
| RAM Roles catalog | built-ins / org-owned / no mapping | `/access/profiles`, Team mapping queries | Roles & mappings tab | A3 |
| Team Role mapping | unchanged / changed preview / saved / conflict / invalid role | Team RAM APIs | Access + TeamDetail | A4, A5, A6, A15 |
| Subject access | allowed / unauthorized / not_applicable / source chain | `/access/overview`, Team members/mappings | Subject access tab | A7, A8 |
| Batch grant | preview / apply success / unauthorized / high risk | `/access/batch/preview`, `/access/batch/apply` | drawer | A9 |
| Revoke | derived not-applicable / direct preview / bad token / confirm / replay | `/access/grants/revoke/*` | GrantRevoke | A10, A11 |
| Profiles | list / detail / create / version / conflict / disable | `/access/profiles/*` | Profiles tab | A12, A13, A14 |
| AI Runtime retired profile | import reject / export omits retired fields | `/ai-runtime/import/*`, export | AI Runtime page | A16 |

## 6. 一手审计命令记录

```bash
git fetch origin main
git rev-parse origin/main
# 05de26039c389be3bcb4faed4f759d972db80e0b

rg -n "runtime\\.profiles|default_profile_key|ai_runtime_profiles|AI Runtime Profile|Runtime Profile" internal docs web sites README*
rg -n "access_profiles|access_profile_versions|access/profiles|AccessProfile|access profile|Access profiles" internal docs web sites README*
rg -n "team_role_ram_role|ram_role_ids|ram_role_keys|SourceTeamRoleRAM|team_role_ram|RAM Role|RAM roles" internal docs web sites README*
rg -n "EnforcementShadow|ShadowComparison|shadow|effectiveCache|invalidateEffectiveCache|authorization_audit|team_role_ram_role_audit" internal/authorization internal/team internal/webconsole/api internal/persistence/migrations docs/design/features/unified-permission-contract.md
```

