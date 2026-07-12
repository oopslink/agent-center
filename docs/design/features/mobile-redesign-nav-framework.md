# 移动端全局导航框架设计

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-07-12 |
| Scope | 移动端（`md:hidden` 断点）全局导航壳层——顶部栏 / 底部 Tab / 二级导航 / 辅助面板。不含任何具体业务页面内容布局 |

## 1. 背景

这是"完整调研 PC 端页面，独立设计移动端页面"这一大项目的第一批交付物。PC 端共约 35 个独立视图（27 个顶层路由 + OrganizationSettings 5 个子 section + 3 个认证页），全部重做工作量巨大，故按模块拆分批次：

1. **本批：全局导航框架**（本文档）
2. 下一批：Workspace 会话类页面（Unread / Channels / ChannelDetail / DMs / DMDetail）
3. 后续批次：项目/工作项类、成员/设置类，依次排期

本文档是"全新独立设计"——不复用现有移动端实现（`MobileTabBar` / `BottomSheet` / mobile top bar 等）的具体代码或交互细节作为设计输入，只以 PC 端信息架构（col①模块 rail、col②二级导航、col③内容、col④context panel、右上角 attention 铃铛）作为要覆盖的功能范围。

## 2. PC 端信息架构（覆盖范围基准）

PC 端 `AppLayout` 是四栏结构：

- **col① 模块 rail**：5 个模块图标（Workspace / Conversations / Members / Reminders / System），常驻左侧。
- **col② 二级导航**：当前模块的子项列表（如 Workspace 下的 Projects/Issues/Tasks/Plan/Repos/Templates），可折叠。
- **col③ 内容区**：当前路由页面。
- **col④ Context Panel**：详情类页面（Task/Project/Issue/Plan Detail）的按需辅助信息侧栏。
- **右上角 attention 铃铛**：跨页面常驻的"需要你处理"告警入口（stuck 任务 + 未读 @mention）。

移动端设计必须覆盖以上 5 类信息，但表现形式独立设计，不要求 1:1 复刻交互。

## 3. 设计方案

### 3.1 结构总览

采用**方向 A：底部 Tab + 二级导航抽屉**，辅助面板（Context Panel、Attention）统一改为底部弹出面板（BottomSheet 语义），与二级导航抽屉共用同一套交互语言，避免多种弹层样式并存。

```
┌─────────────────────────┐
│ ☰ <模块名>      🔔 ●3 👤 │  ← 顶部栏（固定）
├─────────────────────────┤
│                          │
│      内容区（col③）      │  ← 当前路由页面
│                          │
├─────────────────────────┤
│ Work Chat Team Remind Sys│  ← 底部 Tab（固定，5 项，对应 col①）
└─────────────────────────┘
```

### 3.2 顶部栏

- **左侧**：当前模块名 + ☰ 图标，点击打开**二级导航 BottomSheet**（列出该模块的子项，如 Projects/Issues/Tasks/Plan）。
  - 详情类页面（Task/Project/Issue/Plan/Channel/DM Detail 等）左侧变为 **‹ 返回**，不显示 ☰（详情页没有"二级导航"概念，返回上一级即可）。
- **右侧**：🔔 attention 徽标（未读数角标）+ 👤 账户入口。点击 🔔 打开 **Attention BottomSheet**；点击 👤 打开账户 BottomSheet（组织切换、个人资料、主题、登出）。

### 3.3 底部 Tab

固定 5 项，与 PC col① rail 一一对应：Work / Chat / Team / Remind / Sys。每项图标 + 短标签，当前模块高亮。Conversations 模块角标沿用未读/@mention 计数规则。

### 3.4 二级导航 BottomSheet

- 触发：点击顶部栏 ☰。
- 内容：当前模块的子项列表（对应 PC col② 的 `NavSection` 结构），列表项支持展开子列表（如 Channels 下的具体频道）。
- 交互：下拉手势关闭 + 点击遮罩关闭，最大高度 70vh。

### 3.5 Context Panel BottomSheet

- 不再是常驻侧栏，而是详情页内的"更多信息"入口按钮触发的 BottomSheet。
- 内容与 PC col④ 一致（负责人、状态、截止时间等元信息），具体字段设计留给各详情页的 mockup 批次。
- 最大高度 45–70vh，视内容量而定，由各页面 mockup 阶段确定。

### 3.6 Attention BottomSheet

- 触发：顶部栏 🔔。
- 内容与 PC 端 `AttentionPanel` 语义一致：列出 stuck 任务 + 未读 @mention，点击跳转对应详情页并关闭面板。

## 4. 交互规则（跨页面通用）

1. 所有 BottomSheet 支持下拉手势关闭、点击遮罩关闭，最大高度不超过 70vh，避免完全遮住返回路径。
2. 底部 Tab 始终可见（不随 BottomSheet 展开而隐藏，BottomSheet 悬浮其上）。
3. 顶部栏高度固定，不随内容滚动隐藏（除非某具体页面 mockup 阶段另有说明，如沉浸式阅读页）。
4. 详情页返回按钮遵循"返回上一级列表"语义，不做浏览器历史强绑定。

## 5. Out of Scope（本文档不覆盖）

- 具体业务页面（Projects 列表、Task 详情内容区、消息流等）的移动端布局——留给后续各模块批次的 mockup。
- Context Panel 具体字段级 UI——同上。
- 是否复用现有 `MobileTabBar`/`BottomSheet` 组件代码——这是实现阶段（writing-plans）决策，本文档只定交互与信息架构。

## 6. 未来扩展

- 详情页 Context Panel 触发按钮的具体位置/样式，随各业务模块 mockup 逐步细化。
- 若某模块二级导航子项过多（如 Workspace 下 6 项 + 频道/项目子列表），后续批次可能需要在 BottomSheet 内引入搜索/筛选，本文档不预先设计。
