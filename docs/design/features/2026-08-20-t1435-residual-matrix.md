# T1435 全量残留 / 迁移矩阵

| 字段 | 值 |
|---|---|
| 状态 | Audit matrix / deletion readiness（S1 remediation） |
| 日期 | 2026-08-20 |
| 审计基线 | 候选提交 `9da26a8e`；执行时以回读的 `origin/main` 为准 |
| 配套合同 | `docs/design/features/2026-08-20-t1435-dual-profile-ram-access-contract.md` |

## 1. 分域判定

| 术语 | 是否删除 | 允许残留位置 | 禁止混入 |
|---|---|---|---|
| AI Runtime Profile | 是 | 历史 migration/down、import retired guard、历史报告 | RAM Role、身份资料 profile |
| Access Profile | 是 | 历史 migration、限时迁移 adapter/断言、本合同与审计报告 | 产品导航/API/DTO/模板/生产消费者 |
| RAM Role | 否 | 独立 authorization schema/API/UI/template | 任何 `Profile` 命名或旧表 fallback |
| 身份资料 profile | 否，且不在范围 | identity/account/Agent metadata | runtime selector、permission bundle、RAM mapping |

## 2. Access Profile 删除与迁移矩阵

下表全部是删除目标；“兼容”只允许按合同 W0-W3 限时存在，不能作为保留理由。

| 残留 | 当前生产者/消费者 | 目标迁移 | 删除门槛 | 终态验证 |
|---|---|---|---|---|
| `access_profiles` | 0132 seed；profile handlers；RoleBuilder/Access catalog | 每行无损映射到 `authorization_roles`；disabled -> revoked；collision 非等价即阻断 | W3 终态快照、count/hash/effective 等价通过 | active schema 无该表；历史 migration 命中白名单 |
| `access_profile_versions` | create/version API；detail/history | 每行迁到 `authorization_role_versions`；latest 展开到 role permissions | source/history count 相等、latest permission 集合相等 | active schema 无该表；RAM history 可回读 |
| 0132 built-in profile seed | migration chain | 三 built-in 只 seed RAM current/history/permissions | 新安装和升级安装结果一致 | built-in ID 各一份，无 profile mirror |
| `0136_access_profile_ram_role_contract` bridge | 把 profile 镜像成 auth role | 被 RAM-only consolidation migration 取代；已发布 migration 文件保留历史，不再作为生产同步器 | fresh/upgrade migration tests 通过 | 无双向/重复 seed |
| `/access/profiles*` routes/handlers | Web profile CRUD/version/disable | 新 RAM Role API；旧 API W2 只 410，W3 删除 | 新客户端零旧调用、服务访问日志零调用 | server route 表无旧 route |
| `AccessProfile*` DTO/types/hooks | handler + React Query | `RAMRole*` DTO/types/hooks，字段语义以 RAM current/history 为准 | 类型与 contract tests 更新 | 生产 bundle/source 无命中 |
| Access `Profiles` tab/view | human admin | Roles & mappings 内的 RAM Role detail/create/history/revoke | A12-A14 新证据通过 | tab 只有 Roles & mappings / Subject access |
| RoleBuilder `useAccessProfiles` | Team template/create/edit | `useRAMRoles`; selection 保存 `ram_role_keys` 或 mapping IDs | resolver RAM-only 且 cross-org/revoked 测试通过 | RoleBuilder 不读 profile API |
| fixtures/i18n/MSW mocks | tests/dev | RAM Role fixture/文案/routes | 前端定向测试更新 | 非历史源无旧词 |
| `access_profile_*` template/import fields | 外部模板（若存在） | 拒绝并提示使用 `ram_role_keys`；不得自动猜测 | compatibility release notes/guard tests | import contract 明确拒绝 |

### 2.1 逐表数据核对

| 核对项 | 权威断言 |
|---|---|
| profile rows | 每个 source ID 有且仅有一个等价 RAM Role；org/kind/name/description/timestamps/revocation/version 映射正确 |
| version rows | 每个 `(profile_id, version)` 有且仅有一个 `(role_id, version)`；JSON 规范化后权限集合相同；risk 保存为 snapshot |
| latest current | 每个 active role 的 current permissions 等于 source latest version，resource kind 来自 permission registry |
| mappings | 每个 `ram_role_id` 指向 active、同 org 可见的 RAM Role；ID 不变，无需批量改 FK |
| direct bindings | role ID 可解析；revoked role 不产生 effective grant；无 cross-org 泄漏 |
| templates | `ram_role_keys` 只按 RAM Role name 解析；同 org 优先 system；歧义/unknown 失败 |

### 2.2 兼容 / 回滚证据

| 阶段 | 必交一手证据 |
|---|---|
| W0 | 快照 ID、schema version、逐表 counts、排序 SHA-256、orphan/collision/unknown-permission 报告、恢复演练结果 |
| W1 | dual-write 事务测试；旧读 vs RAM shadow 的 DTO/effective/scope/hash 零差异窗口 |
| W2 | RAM-only 生产消费者清单；旧写冻结；旧 API 410；cache invalidation、audit、mapping/direct coexist probes |
| W3 | drop 前终态快照；旧 route/UI/type/mock/table 删除；全仓白名单扫描 |
| rollback | 停写时点、W2 增量导出、恢复点一致性、重放结果、数据库回读 counts/checksums、effective/explain probes |

## 3. AI Runtime Profile 残留

| 残留 | 分类 | 处理/删除条件 | 验证 |
|---|---|---|---|
| 0116 创建旧表/列 | 已发布历史 migration | 不改写；由 0126 抵消 | migration chain test |
| `0126_remove_ai_runtime_profiles.up/down.sql` | 生产删除线/rollback-only | 保留为防回归与升级路径 | schema 无旧表/绑定列 |
| import retired-field guard/tests | 生产拒绝 | 外部旧 schema 明确废弃后才可删除 guard | retired field -> 400 |
| selector/export tests | 防回归 | 以等价直接 CLI/Model 测试替换后才删 | export 无 retired fields |
| 历史 docs/plans/reports | 只读审计 | 不当作生产合同/消费者 | 明确历史上下文 |

AI Runtime Profile 删除不为 Access Profile 提供保留例外；二者分别执行各自删除线。

## 4. RAM Role / Team Mapping 保留矩阵

| 残留 | 分类 | 生产者 | 消费者 | 不变量/验证 |
|---|---|---|---|---|
| `authorization_roles` | RAM current | RAM API/migration | catalog、resolver、auth effective | active name 唯一；system/current-org scope；revoked fail closed |
| `authorization_role_permissions` | RAM current permissions | RAM version transaction | auth effective/explain | registry-valid；集合与 latest history 相等 |
| `authorization_role_versions` | RAM history | create/version/migration | detail/audit/rollback | append-only CAS；完整承接每个旧 version |
| `team_roles.ram_role_keys` | portable template declaration | team template/create/update | RAM-only resolver | 不得读 profile 表；unknown/ambiguous/revoked 失败 |
| mapping/version/audit 三表 | production mapping | CAS replace/team persist | auth effective、Access/Team UI | full replace；version +1；previous/next/actor 完整 |
| Team RAM handlers/service/repo | application chain | Access/Team/tools | authoritative tables | invalid/cross-org 422；stale 409；删除立即 fail closed |
| authorization effective/cache | production consumer | mapping/direct/legacy | Check/Explain/ListEffective | multi-project scope；direct union；version/invalidation 完整 |

## 5. Direct Binding / Shadow / Audit 残留

| 面 | 保留对象 | 验收重点 |
|---|---|---|
| direct binding | assignments、two-phase revoke previews | Team RAM 与 direct 并集；撤销单一来源不抹另一来源；cross-org opaque 404 |
| shadow | enforcement comparison + W1 migration comparison | 默认 authorization shadow；W1 RAM read 零差异；legacy rollback 不伪造成功 |
| cache | effective cache/version/invalidation | role/version/permission、mapping/version、membership/project/direct 变化全部失效 |
| audit | authorization、mapping、RAM Role audit | actor、subject/resource、previous/next、request ID 可追溯 |
| rollback | W0/W3 snapshots + incremental audit replay | mapping/direct/role 必须同一恢复点；从 DB 权威源回读 |

## 6. Access IA / A1-A16 证据索引

不存在 Profiles surface。A12-A14 验证 Roles & mappings 内的 RAM Role 工作流。

| Surface | States | 权威数据 | Screenshot IDs |
|---|---|---|---|
| permission gate/loading | forbidden/loading | effective/explain/overview | A1, A2 |
| RAM Roles + Team mapping | catalog/preview/saved/conflict | RAM APIs + Team RAM APIs | A3-A6 |
| Subject access | source chain/direct coexist | overview/effective/explain | A7, A8 |
| Batch grant/revoke | preview/derived blocked/direct confirmed | batch + revoke preview/confirm | A9-A11 |
| RAM Role in Roles & mappings | detail/history/create/version conflict | `/access/ram-roles*` | A12-A14 |
| Team RoleBuilder | RAM selection/server readback | Team RAM APIs/resolver | A15 |
| AI Runtime retired guard | import reject/export omission | AI Runtime import/export | A16 |

截图文件名与逐项可见内容以配套合同第 6 节为唯一清单；任何含 `Profiles` tab、Access Profile 文案或旧 API 数据的 A12-A14 均判失败。

## 7. 全仓审计与定向验证

```bash
git fetch origin main
git rev-parse origin/main

rg -n "access_profiles|access_profile_versions|access/profiles|AccessProfile|access profile|Access Profile|Profiles tab" internal web sites README* docs
rg -n "authorization_role_versions|authorization_roles|authorization_role_permissions|ram_role_ids|ram_role_keys|RAM Role" internal web docs
rg -n "EnforcementShadow|shadow|effectiveCache|invalidateEffectiveCache|authorization_audit|team_role_ram_role_audit" internal docs
rg -n "runtime\\.profiles|default_profile_key|ai_runtime_profiles|AI Runtime Profile" internal web docs

go test ./internal/persistence ./internal/authorization ./internal/team/service ./internal/webconsole/api
(cd web && pnpm test -- --run src/pages/Access.test.tsx src/pages/TeamDetail.test.tsx src/pages/AiRuntime.test.tsx)
```

旧词扫描不是“零命中即通过”：每个命中必须归类为历史 migration、限时迁移/retired guard 或待删生产残留；生产 API/UI/template/consumer 命中一律阻断 W3。
