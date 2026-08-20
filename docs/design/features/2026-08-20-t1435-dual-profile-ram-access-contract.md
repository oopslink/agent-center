# T1435 Profile 分域 / Team RAM Access 冻结合同

| 字段 | 值 |
|---|---|
| 状态 | Frozen executable contract（S1 remediation） |
| 日期 | 2026-08-20 |
| 审计基线 | 候选提交 `9da26a8e`；执行前必须回读并记录实际 `origin/main` |
| 范围 | AI Runtime Profile 删除线、Access Profile 产品删除线、独立 RAM Role、Team Role -> RAM Role、Access IA、迁移/回滚、A1-A16 截图验收 |
| 配套矩阵 | `docs/design/features/2026-08-20-t1435-residual-matrix.md` |

## 1. Profile 分域边界

`profile` 不是一个可跨域复用的产品实体。以下三个词必须保持隔离：

| 名称 | 含义 | 本合同结论 |
|---|---|---|
| AI Runtime Profile | 旧 CLI / Model / parameters 间接选择器 | 已删除；禁止恢复表、字段、API 或 UI |
| Access Profile | 旧 Access 权限包产品、Profiles tab、`/access/profiles*` | 删除；不得重命名 UI 文案后继续保留同一产品/API |
| 身份资料 profile | 用户/Agent 的头像、名称、简介等 identity metadata | 不在 T1435 范围；不得与授权或 runtime 配置联表/互转 |

### 1.1 AI Runtime Profile 删除线

- 生产 Schema 不允许存在 `ai_runtime_profiles`，`ai_runtime_catalogs.default_profile_id` 不得承载绑定。
- import 必须拒绝 `runtime.profiles`、`runtime.default_profile_key`；export 不得输出它们。
- Agent desired runtime、executor candidates 与 Team Role runtime config 直接保存 CLI、Model、parameters、concurrency。
- `0126_remove_ai_runtime_profiles` 及 retired-field 拒绝测试是永久防回归合同，不属于 Access/RAM 迁移。

### 1.2 Access Profile 产品删除线

目标态不存在 Access Profile 产品概念。以下均为必须删除项，而不是保留面：

- 数据：`access_profiles`、`access_profile_versions` 的活跃读写与 seed；兼容窗口结束后删除旧表。
- API：`GET/POST /api/orgs/{slug}/access/profiles`、detail、`versions`、`disable`；切换后不得由别名或 rewrite 继续服务。
- UI：Access 的 `Profiles` tab、profile list/detail/create/version/disable 表单、`AccessProfile*` 类型/hooks/i18n/mocks。
- 模板：不得出现 `access_profile_id(s)`、`access_profile_key(s)` 或名为 profile 的权限包模板。现有 `ram_role_keys` 是 RAM Role 稳定名称引用，可以保留。
- 生产消费者：RoleBuilder、Team detail、mapping preview/save、authorization effective 只能读 RAM Role repository；不得读旧表或调用旧 profile API。

删除旧产品不删除权限含义。权限包以独立 **RAM Role** 模型承接，其权威当前态为 `authorization_roles` + `authorization_role_permissions`，历史态为 `authorization_role_versions`。RAM Role 不是任何一种 profile。

## 2. 独立 RAM Role 合同

### 2.1 权威模型

| 实体 | 权威结构 | 不变量 |
|---|---|---|
| RAM Role current | `authorization_roles` | `id` 稳定；system role 的 `org_id=''`；org role 的 `org_id` 必须匹配；active name 唯一；`revoked_at` 后不可新绑定 |
| RAM Role permissions | `authorization_role_permissions` | 只引用已注册 permission；`(role_id, permission_key, resource_kind)` 唯一 |
| RAM Role history | `authorization_role_versions` | `(role_id, version)` 唯一；保存规范化 permissions JSON、risk snapshot、actor、timestamp；只追加 |
| Team Role mapping | `team_role_ram_role_mappings` + versions + audit | Team Role 绑定 0..N RAM Role；CAS 全量 replace；前后集合可审计 |

`authorization_role_versions` 的冻结列为：`role_id TEXT`、`version INTEGER`、`permissions_json TEXT`、`risk_snapshot TEXT`、`created_by TEXT`、`created_at TEXT`，主键 `(role_id, version)`。`risk_snapshot` 只保存当时风险展示值；实际风险始终由 permission registry 推导。

内置 `team-basic`、`team-contributor`、`team-curator` 只在 RAM Role 表中各存在一份。禁止再向旧 profile 表镜像，也禁止双写两套领域对象。

### 2.2 领域与模板语言

| 名称 | 权威含义 |
|---|---|
| Team Role | Team 内功能角色，携带 runtime config/声明式需求；本身不授予权限 |
| RAM Role | 独立权限包，持有 RAM permissions；不是 profile |
| `ram_role_keys` | template/create/update 的可移植 RAM Role 名称；只解析 `authorization_roles` |
| `ram_role_ids` | mapping preview/PUT 的稳定 ID；只校验 active `authorization_roles` |

`ram_role_keys` 的 resolver 必须优先匹配同 org role，再匹配 system role；歧义、dangling、cross-org 或 revoked 均失败，不得 fallback 到 `access_profiles`。

### 2.3 API 合同

| API | 作用 | 鉴权 | 关键行为 |
|---|---|---|---|
| `GET /api/orgs/{slug}/access/ram-roles` | RAM Role catalog | org member | 返回 active system + current-org roles，不含 profile DTO/术语 |
| `GET /api/orgs/{slug}/access/ram-roles/{id}` | current + version history | org member | cross-org opaque 404 |
| `POST /api/orgs/{slug}/access/ram-roles` | 创建 org RAM Role | owner/admin | 校验 permission registry；写 current、permissions、version 1 与 audit |
| `POST /api/orgs/{slug}/access/ram-roles/{id}/versions` | CAS 更新权限集合 | owner/admin；system forbidden | `expected_version` stale -> 409；事务更新 current/version/history/audit |
| `POST /api/orgs/{slug}/access/ram-roles/{id}/revoke` | revoke org RAM Role | owner/admin；system forbidden | 先 preview 已有 mapping/direct binding；确认后 fail closed，不能称 disable profile |
| Team RAM GET/preview/PUT | 读取、预览、CAS 全量替换 mapping | member/read；manage/write | 请求使用 `ram_role_ids`；invalid role -> 422；stale -> 409 |

旧 `/access/profiles*` 在兼容窗口的 W0/W1 只允许旧版本服务继续工作；新版本不得新增调用。W2 切换时路由移除并返回明确的 `410 access_profile_retired`（不做写入、不代理 RAM API），W3 后完全删除路由。410 只是短期客户端诊断，不是兼容 API。

## 3. `access_profile*` -> RAM Role 逐表迁移

迁移必须可重跑、事务化、遇冲突即停止；禁止用 `INSERT OR IGNORE` 隐藏不一致。

### 3.1 回滚前快照（W0）

在任何双写/回填前，生成同一数据库事务视图下的只读快照：

1. 导出 `access_profiles`、`access_profile_versions`、`authorization_roles`、`authorization_role_permissions`、`team_role_ram_role_mappings`、mapping versions/audit、`authorization_role_assignments`。
2. 记录每表 row count、主键排序后的 SHA-256、schema version、候选 SHA、UTC timestamp。
3. 单独记录 orphan version、重复 active org/name、unknown permission、profile/role 同 ID 非等价冲突；任一非零则阻断。
4. 快照写入受控备份位置并做恢复演练；应用只记录 snapshot ID/checksum，不在日志输出权限数据。

### 3.2 逐表转换（W1）

| 来源 | 目标 | 逐行映射与校验 |
|---|---|---|
| `access_profiles` | `authorization_roles` | `id -> id`，`org_id -> org_id`，空 org -> `kind=system`，非空 -> `kind=custom`，name/description/created_by/created_at/updated_at 原样；`disabled_at -> revoked_at`；`version = max(access_profile_versions.version)`。目标同 ID 已存在时必须逐字段等价，否则阻断 |
| `access_profile_versions` 每一行 | `authorization_role_versions` 每一行 | `profile_id -> role_id`，version/permissions_json/risk/created_by/created_at 原样映射；permissions 先规范化去重排序并验证 registry；不得只迁 latest 而丢历史 |
| 每个 profile 的 latest version | `authorization_role_permissions` | 展开 permissions JSON；从 registry 展开其合法 `resource_kind`；与目标 role 现有集合做集合等价校验后 upsert。空/unknown/非法 JSON 阻断 |
| built-in seed/0136 mirror | RAM Role current/history | 以迁移后 RAM Role 三表为唯一 seed；删除 profile seed 与 bridge mirror，内置 ID 不变，从而无需改写 mapping FK |
| `team_role_ram_role_mappings` | 原表 | role ID 保持不变；逐行验证目标 active RAM Role 与 org scope，不合法则阻断，不静默删除 |
| `team_roles.ram_role_keys` 与模板 payload | 原字段/新模板 | key 保持 RAM Role name；resolver 攅读 `authorization_roles`；旧 profile 命名字段拒绝导入 |

回填后必须证明：source profile count = migrated role count（排除迁移前已等价 role 后按 ID 去重）、source version count = history count、每个 latest permission 集合相等、全部 mapping/direct assignment 可解析、三 built-in 只存在一份权威行。

### 3.3 兼容窗口与切换顺序

| 阶段 | 读 | 写 | 退出门槛 |
|---|---|---|---|
| W0 snapshot | 旧服务照常；新 RAM repo 不接流量 | 旧写 | 快照校验/恢复演练通过 |
| W1 backfill + dual-write | 生产仍旧读；shadow 读 RAM 并逐请求比对 | 临时 adapter 先写 RAM 事务，再写旧表；任一失败整体回滚 | 连续观测窗口内 row/hash、API DTO 语义、effective permission、mapping preview 均零差异 |
| W2 RAM cutover | 所有生产消费者只读 RAM；旧读仅离线 shadow | 只写 RAM；旧 profile 写冻结；旧 API 410 | 缓存版本、shadow、审计与定向测试通过；无新版本客户端访问旧 API |
| W3 cleanup | RAM only | RAM only | 删除旧路由/UI/types/mocks/handlers、旧表与 dual adapter；全仓仅迁移/历史/retired guard 允许命中旧词 |

双写是限时迁移机制，不是目标架构。W2 后不得为了“兼容”从旧表 fallback；否则 RAM revoke 可能被旧数据复活。

### 3.4 回退方案

- W1 失败：回滚当前事务，保留旧服务权威；用 W0 快照恢复被验证的 RAM 目标表，修复后全量重跑。不得在部分 profile 上继续。
- W2 应用回退：先停止写入，导出 W2 增量 RAM role/version/permission/mapping/audit；仅当旧双写仍完整且 checksum 等价时，短时回到 W1 旧读。若不等价，恢复 RAM 版本应用，不允许丢弃增量。
- W2 数据回退：从 W0 快照恢复目标表，再按保存的 W1/W2 audit 顺序重放；mapping 与 direct assignment 必须同一恢复点。授权 enforcement 可临时切 `legacy`，但不等于数据恢复完成。
- W3 drop 前再做一份相同格式终态快照并保留至兼容期结束；drop 后若必须回退，先恢复旧 schema + 快照，再部署旧二进制。禁止只执行 0132/0136 down，因为这会丢版本、mapping 关联或 W2 增量。
- 每次回退后从数据库权威源回读 counts/checksums，并跑 explain/effective probes；日志中的 `ok` 不构成成功证据。

## 4. Team RAM 生效合同

- Team RAM 唯一消费者为 authorization effective service；team scope 只匹配本 Team。
- project 以及 task/issue/plan/conversation 派生 scope 必须落在 `team_projects` 关联中；多 Project 时全部关联项目同时生效并完整出现在 preview。
- 移除 mapping、member 或 project link 必须立即 fail closed。
- direct binding (`authorization_role_assignments`) 与 Team RAM 求并集、互不覆盖；撤销一条来源不得抹掉另一条来源，explain 必须显示真实 source chain。
- mapping replace 使用 CAS 全量 replace、写 previous/next role IDs/version/actor audit；不得 patch append。

## 5. Cache、Shadow、Audit

| 切面 | 冻结规则 |
|---|---|
| Cache | effective version 纳入 roles、role permissions、role versions、assignments、members、projects、mapping/version 与 curator policy；写后 invalidate |
| Shadow | W1 比较旧读与 RAM 读；授权默认 shadow，Team RAM 必须进入 legacy/equivalent 双侧；差异按 role/permission/scope 分类且不含敏感 payload |
| Audit | RAM Role create/version/revoke 与 direct grant/revoke、mapping replace 各写权威 audit；actor、subject/resource、previous/next 不得省略 |
| Rollback | `legacy` 只回退 enforcement；数据回退必须按第 3.4 节快照与重放执行 |

## 6. Access IA 与 A1-A16 截图矩阵

Access 只有两个 tab：`Roles & mappings`（默认）与 `Subject access`。不存在 `Profiles` tab。RAM Role 的创建、编辑、历史与 revoke 均在 Roles & mappings 内完成，并始终使用 RAM Role 文案。

截图必须来自真实导航路径，保存到 `docs/releases/<version>-screenshots/access/` 并在报告内联引用。

| ID | 页面/状态 | 必须可见 | 导航路径 | 建议文件名 |
|---|---|---|---|---|
| A1 | Access no permission | forbidden reason；写控件禁用 | member -> sidebar Access | `access-forbidden.png` |
| A2 | Access loading | summary/table skeleton；无布局跳动 | admin -> Access，延迟 overview | `access-loading.png` |
| A3 | Roles & mappings happy | RAM Role catalog、Team mappings、version、used-by | Access -> Roles & mappings | `access-ram-roles-mappings.png` |
| A4 | mapping preview | members、+/- roles、全部 projects | edit mapping -> Preview | `access-mapping-preview.png` |
| A5 | mapping saved | version +1、draft 清空、server 回读 | Preview -> Save | `access-mapping-saved.png` |
| A6 | mapping conflict | inline 409；无部分写 | stale expected_version save | `access-mapping-conflict.png` |
| A7 | Subject access chain | membership -> Team Role -> RAM Role -> scoped permissions | Subject access -> expand | `access-subject-chain.png` |
| A8 | direct coexists | direct binding 与 Team RAM source 并存 | subject with both sources | `access-direct-coexist.png` |
| A9 | batch grant preview | grantable/high risk/unauthorized/not applicable | Batch grant -> Preview | `access-grant-preview.png` |
| A10 | derived revoke | not_applicable；禁止直接 revoke | Subject access -> revoke derived | `access-derived-revoke-blocked.png` |
| A11 | direct revoke confirm | token 二阶段、revoked、audit 可查 | direct revoke -> Confirm | `access-direct-revoke-confirmed.png` |
| A12 | RAM Role detail/history | current permissions、version history、risk；无 Profile 文案/tab | Roles & mappings -> RAM Role | `access-ram-role-detail.png` |
| A13 | RAM Role create | create form、permission checklist、created role selected | Roles & mappings -> New RAM Role | `access-ram-role-created.png` |
| A14 | RAM Role version conflict | stale expected_version 409；无新版本 | RAM Role -> publish stale edit | `access-ram-role-version-conflict.png` |
| A15 | Team detail mapping | RAM Role multi-select；save 后 `ram_role_keys` server 回读 | Team detail -> edit roles | `team-role-ram-builder.png` |
| A16 | AI Runtime retired guard | export 无 retired fields；import retired fields 报错 | System -> AI Runtime -> Import | `ai-runtime-retired-guard.png` |

## 7. 定向验收

```bash
git fetch origin main
git rev-parse origin/main

# 旧产品命中只能在历史迁移、迁移兼容/删除说明和 retired 断言白名单中。
rg -n "access_profiles|access_profile_versions|access/profiles|AccessProfile|access profile|Access Profile|Profiles tab" internal web sites README* docs
rg -n "authorization_role_versions|authorization_roles|authorization_role_permissions|ram_role_ids|ram_role_keys|RAM Role" internal web docs
rg -n "runtime\\.profiles|default_profile_key|ai_runtime_profiles|AI Runtime Profile" internal web docs

go test ./internal/persistence ./internal/authorization ./internal/team/service ./internal/webconsole/api
(cd web && pnpm test -- --run src/pages/Access.test.tsx src/pages/TeamDetail.test.tsx src/pages/AiRuntime.test.tsx)
```
