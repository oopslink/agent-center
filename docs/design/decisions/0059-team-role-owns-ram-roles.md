# 0059. Team Role owns its RAM Roles

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-08-23 |

## Context

旧 Access 产品合同使用 `Roles & mappings`，并允许把 Team Role 与 RAM Role
的关系做成独立 `Team Role mappings` 页面。这种口径把持久化/API 的实现概念
暴露给用户，也把 Team Role 的一个属性从 Team Role 本身拆了出去。

用户不管理一张“映射表”。用户编辑 Team Role，并选择这个 Team Role 拥有的
RAM Roles。所属 Team 和具体 Team Role 是完成该操作不可缺少的上下文。

## Decision

1. Access 二级侧栏恰好只有 `RAM Roles` 与 `Subject access` 两个入口。
2. Access 不存在 `Roles & mappings` 或 `Team Role mappings` 入口、tab、页面或
   用户工作流。
3. Team Role 在 `Teams → Team → Roles → Team Role` 中直接暴露可编辑的
   `RAM Roles` 列表。
4. RAM Roles 页面管理 RAM Role 定义。它可以只读展示 `Used by Team Roles`
   引用，但不能修改某个 Team Role 的 RAM Roles。
5. `mapping`、`map`、`replace mapping` 仅保留为持久化/API 实现术语。产品文案
   使用 `RAM Roles`、`Add RAM Role`、`Remove`、`Save changes`。
6. 保存 Team Role 的 RAM Roles 仍采用带 version 与 audit history 的完整列表
   CAS replace；并发不变量不升级为产品名词。
7. 产品验收以 `docs/design/assets/adr-0059/` 下三份 repo-native HTML 与其确定性
   PNG 为 canonical；全状态行为以
   `docs/acceptance/adr-0059/canonical-state-matrix.md` 为冻结合同。旧 T1456/T1457/
   T1458 合成截图只保留历史证据身份，不再作为 ADR-0059 产品基线。

## Consequences

- Access 信息架构收敛为两个单一职责页面。
- Team Role 配置在 Team Role 上下文中同时承载 runtime configuration 与
  RAM Roles。
- 现有 `Team Role mappings` 导航和页内 tabs 必须删除。
- 测试与验收证据必须验证上述产品语言和页面归属，同时保留 CAS、audit 与
  effective-access 行为。
- 既有后端表和 endpoint 可继续使用 `mapping`，直到另有独立实现迁移；这些
  名称不得成为 UI 文案。
- Canonical mock 必须由可编辑源重新生成；禁止只改 PNG。默认 viewport 固定为
  `1672 × 941`，状态切换与交互注释随源文件版本化。

## Alternatives Considered

### 在 Access 保留 `Roles & mappings`

拒绝。它把 RAM Role 定义管理与 Team Role 属性合并，并隐藏了理解变更所必需
的 Team 上下文。

### 保留独立 `Team Role mappings` 页面

拒绝。它把 Team Role 属性人为包装成关系管理产品，并与 Team Role 编辑器重复。

### 只重命名独立页面

拒绝。问题是页面归属与用户心智，不只是文案。
