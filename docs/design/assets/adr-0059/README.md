# ADR-0059 canonical mocks

这组 repo-native mock 是 ADR-0059 的唯一产品验收视觉基线。它重绘三个用户可达
surface，而不是修改 T1456/T1457/T1458 的旧合成截图：

| Surface | 可编辑源 | Canonical PNG | 默认状态 |
|---|---|---|---|
| Access / RAM Roles | `access-ram-roles.html` | `access-ram-roles-canonical-1672x941.png` | catalog + detail |
| Teams / Team / Team Role | `team-role.html` | `team-role-canonical-1672x941.png` | saved property readback |
| Access / Subject access | `subject-access.html` | `subject-access-canonical-1672x941.png` | effective source chain |

共享样式在 `canonical.css`。PNG 由 `capture.mjs` 从上述 HTML 确定性生成；禁止在
图像编辑器中单独修改 PNG。

## 尺寸合同

- canonical viewport：`1672 × 941 CSS px`。
- `deviceScaleFactor = 1`，输出必须为 `1672 × 941` PNG。
- 主布局：52 px module rail + 246 px secondary sidebar + fluid content。
- 默认页面在 1672 × 941 内必须同时呈现页面身份、主任务、关键状态与下一步操作；
  不依赖纵向滚动才能理解页面归属。
- 小于 1350 px 时允许内容卡片折行；这不是 canonical 像素对比尺寸。

## 交互注释

直接打开 HTML 时，底部 state picker 可切换验收状态；也可使用 `?state=<state>`。
截图模式追加 `&capture=1`，仅隐藏 mock state picker，不改变产品 UI。

### Access / RAM Roles

- Secondary sidebar 恰好两个入口；当前入口为 `RAM Roles`。
- 行选择更新右侧 detail；`Used by Team Roles` 是只读引用区，只提供
  `Open Team Role` 跳转。
- `New RAM Role` / `Edit` 打开 drawer；编辑发布新 version。
- 删除先做引用检查：无引用走 stable key typed confirmation，有引用只展示阻断与
  Team Role 跳转。
- 状态参数：`default`、`loading`、`forbidden`、`empty`、`search`、`filter`、
  `pagination`、`detail`、`create`、`edit`、`version-conflict`、`delete-typed`、
  `reference-blocked`。

### Teams / Team / Team Role

- 面包屑必须保留 Team 与 Team Role 上下文；`RAM Roles` 与 runtime configuration
  同为 Team Role 属性。
- `Edit RAM Roles` 打开完整集合编辑 drawer；save 前显示成员数、Project scope 与
  permission delta。
- 保存后重新 GET Team Role，页面展示 server version 和完整 RAM Roles；409 不保留
  “已保存”假象，只提供 refresh。
- 状态参数：`default`、`edit`、`member-impact`、`member-removal-impact`、
  `save-readback`、`conflict`。

### Subject access

- Secondary sidebar 恰好两个入口；当前入口为 `Subject access`。
- 选择 subject 和 permission 后显示完整 source chain。direct 与 inherited source
  并存且独立撤销；explicit deny 在 outcome 中优先，但 allow sources 仍可解释。
- Batch grant 必须先 preview；403、409、revoke 都是显式状态。
- 状态参数：`chain`、`direct-coexist`、`grant-preview`、`forbidden`、`conflict`、
  `revoke`、`derived-revoke-blocked`、`deny-precedence`。

## 再生成与核验

```bash
node docs/design/assets/adr-0059/capture.mjs
sips -g pixelWidth -g pixelHeight docs/design/assets/adr-0059/*-canonical-1672x941.png
```

逐状态可执行验收合同见
`docs/acceptance/adr-0059/canonical-state-matrix.md`。
