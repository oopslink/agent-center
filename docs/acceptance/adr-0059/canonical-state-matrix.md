# ADR-0059 canonical 全状态验收合同

| 字段 | 值 |
|---|---|
| 状态 | Frozen executable acceptance contract |
| 权威基线 | `origin/main@7232c04df2bc902353107404d18b4bc3f5bd5712` |
| ADR | `docs/design/decisions/0059-team-role-owns-ram-roles.md` |
| Canonical 源 | `docs/design/assets/adr-0059/*.html` |
| Canonical 尺寸 | `1672 × 941`，device scale factor 1 |

本矩阵裁决产品 surface、状态语义和权威回读，不把 mock 中的示例数量或示例名称
升级为生产 fixture 合同。每行必须从真实导航入口驱动，并保存同尺寸原始截图；仅有
HTTP、日志或执行器自报不构成 UI verdict。

## 1. 全局冻结条件

| ID | 断言 | 失败条件 |
|---|---|---|
| G01 | Access secondary sidebar 恰好两个入口：`RAM Roles`、`Subject access`；无重复页内 tabs | 多/少入口，或内容区再次提供 surface tabs |
| G02 | RAM Role definition 只在 Access / RAM Roles 管理 | RAM Role detail 能修改任一 Team Role 的 RAM Roles |
| G03 | Team Role 的 `RAM Roles` 只在 `Teams → Team → Roles → Team Role` 编辑 | 存在脱离 Team/Team Role 上下文的独立关系管理任务 |
| G04 | 用户文案只使用 `RAM Roles`、`Add RAM Role`、`Remove`、`Save changes` | UI 暴露后端关系表、replace 或 CAS 命名 |
| G05 | `Used by Team Roles` 明示 read-only，仅可跳转 `Open Team Role` | 出现 add/remove/save 控件，或在 Access 内写 Team Role |
| G06 | 403/409/422/blocked 均有明确 reason、无成功假象、保留安全恢复动作 | 空白页、静默回退、局部成功、toast 与权威状态矛盾 |
| G07 | 每次 mutation 后以 GET/read model 回读并绑定 server version | 仅用 optimistic local state 或成功响应作为完成证据 |

## 2. Access / RAM Roles 状态矩阵

Canonical：`access-ram-roles.html`；默认 PNG：
`access-ram-roles-canonical-1672x941.png`。

| ID / state | 前置与动作 | 必须可见 | 权威回读 / 否定断言 |
|---|---|---|---|
| R01 `default` | admin 从 Access 进入默认入口 | catalog、summary、search、risk filter、pagination、选中 detail | GET catalog/detail；无 Team Role 写控件 |
| R02 `loading` | 延迟 catalog/detail 请求 | 稳定尺寸 skeleton；page identity 与 sidebar 保留 | 请求结束只替换内容；无布局跳动、无陈旧数据闪现 |
| R03 `forbidden` | 缺 `authorization.role.read`，服务返回 403 | permission reason、所需 permission、返回动作 | 无 catalog/detail 泄漏；create/edit/delete 不可达 |
| R04 `empty` | org 无 custom RAM Role | empty explanation、system-role 语义、create CTA（若可写） | GET 返回空 custom collection；不伪造示例行 |
| R05 `search` | 输入 `review` | 输入值、result count、匹配行、清除路径 | query 与结果绑定；无匹配时转为 search-empty 而非全局 empty |
| R06 `filter` | 选择 `High risk` | active filter、过滤结果数、可清除 | filter 与分页共同进请求/selector；无隐藏未过滤行计数 |
| R07 `pagination` | 从 page 1 进入 page 2 | `11–20` range、page 2 active、page size | server cursor/page 回读；切 filter 后页码复位并防越界 |
| R08 `detail` | 选择 `Project contributor` | permissions、risk、stable key、current version、history、read-only references | detail GET 与列表 id/version 一致；cross-org 为 opaque 404 |
| R09 `create` | 点击 `New RAM Role` | name/key/description/risk/permission inputs、create/cancel | 创建写 current + version 1 后 GET 选中新 role；unknown permission 422 不写 |
| R10 `edit` | detail 点击 `Edit` | current values、expected version、permission diff、`Publish version` | system role 禁止编辑；成功追加 version，不覆写历史 |
| R11 `version-conflict` | stale expected version 发布 | inline 409、server 最新 version、`Refresh latest`、no-save 文案 | current/history/audit 均无本次部分写；refresh 后重新编辑 |
| R12 `delete-typed` | 无引用 custom role 点击 Delete | exact stable key 输入、不可逆警告、disabled→enabled confirm | key 不匹配不可提交；成功后 catalog/detail GET 均不存在，audit 可查 |
| R13 `reference-blocked` | 被 1+ Team Roles 使用的 role 点击 Delete | 阻断原因、完整 reference count/list、逐项 `Open Team Role` | 不提供强制删除；role/current/history 不变；引用来自权威 read model |

## 3. Teams / Team / Team Role RAM Roles 状态矩阵

Canonical：`team-role.html`；默认 PNG：`team-role-canonical-1672x941.png`。

| ID / state | 前置与动作 | 必须可见 | 权威回读 / 否定断言 |
|---|---|---|---|
| T01 `default` | `Teams → Platform Team → Roles → Developer` | Team breadcrumb、Team Role identity、runtime properties、saved RAM Roles、server version | Team Role GET；页面不是 Access surface，也无独立关系管理标题 |
| T02 `edit` | 点击 `Edit RAM Roles` | active RAM Role complete set、expected Team Role version、cancel/save | revoked/cross-org role 不可选；编辑器不写 RAM Role definition |
| T03 `member-impact` | 增加高风险 `Release operator` 后 preview | current/next delta、18 affected members、3 linked projects、scope names、高风险告警 | preview 无写入；成员/Project 来自 authorization impact service，不靠前端计数 |
| T04 `save-readback` | version 12 保存完整 set | success、version 13、dirty state 清空、server 返回的完整 RAM Roles、recalculated member count | GET Team Role + effective probes；移除/新增立即影响全部 18 members 与 3 projects |
| T05 `conflict` | 另一 actor 先保存 version 13，本地以 v12 保存 | inline 409、no-save、`Refresh latest` | 无部分写/audit 假条目；refresh 重新 GET v13 并要求重做 preview |
| T06 `member-removal-impact` | 从完整 set 移除一个 RAM Role 并 preview/save | removed role、lost permissions、受影响 member/project、fail-closed 提示 | 保存回读后旧权限从 Team source 消失；direct 同名权限仍独立保留 |

## 4. Subject access 状态矩阵

Canonical：`subject-access.html`；默认 PNG：`subject-access-canonical-1672x941.png`。

| ID / state | 前置与动作 | 必须可见 | 权威回读 / 否定断言 |
|---|---|---|---|
| S01 `chain` | 选择 Builder / `project.write` | Subject → Team membership → Team Role → RAM Role → effective permission 完整链与 scope | explain response 每个 source id 可追溯；不把 derived grant 伪装为 direct |
| S02 `direct-coexist` | subject 同时有 Team source 与 direct grant | 两条独立 source、union outcome、各自 expiry/scope | 撤销任一来源不抹另一来源；effective 是集合并集再应用 deny |
| S03 `grant-preview` | batch 输入含 normal/high-risk/unauthorized/not-applicable | 每目标 outcome、scope、risk、可提交数量；明确 preview 尚未写入 | confirm 只提交 eligible set；403/422 target 不产生 assignment/audit |
| S04 `forbidden` | 缺 `authorization.explain`，服务返回 403 | reason、所需 permission、返回动作 | 不泄漏 subject、permission、source chain 或 counts |
| S05 `conflict` | preview token/version 过期，confirm 返回 409 | no-write、`Refresh subject`、重新 preview 要求 | assignments/audit 无部分写；refresh 后 source/effective 重新回读 |
| S06 `revoke` | 对 direct grant 执行 two-phase revoke | direct 标识、subject/resource、影响 preview、confirmation token | confirm 后 assignment/audit 回读；Team-derived source 保留且不可由 direct revoke 删除 |
| S07 `derived-revoke-blocked` | 对 Team-derived source 点击 revoke | `not_applicable`、来源链、`Open Team Role` | 无 revoke token、无 assignment mutation；只能去 Team Role 改属性 |
| S08 `deny-precedence` | direct + inherited allow 与 explicit deny 同时存在 | 全部 allow sources、deny source、最终 Denied outcome、precedence explanation | deny 移除前 effective 必须 fail closed；不能因任一 allow 绕过 deny |

## 5. 证据与 verdict 合同

每个 ID 的验收记录必须包含：候选 SHA、真实导航路径、actor/permission fixture、请求
状态、mutation 前后 server version、权威 GET/explain/audit 摘要、`1672 × 941` 原始 PNG、
PASS/FAIL verdict。T04/T05/S03/S05/S06 还必须保存 mutation request/response 与 mutation
后的权威回读；敏感 payload 脱敏但不可省略 source identity。

以下任一情形直接 FAIL：

1. Access sidebar 不是恰好两个入口；
2. `Used by Team Roles` 可写；
3. RAM Roles 不在 Team Role 页面作为属性编辑；
4. 产品 UI 出现 ADR-0059 已废止的独立关系管理术语；
5. 409 后出现 success、version 前移或部分 mutation；
6. revoke/deny 破坏 direct 与 derived source 的独立性；
7. 截图不是从 repo-native HTML 或真实产品 surface 生成，或尺寸不符。
