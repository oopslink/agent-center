# 移动端 Members 模块设计

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-07-16 |
| Scope | Members 模块：Agents 列表 / AgentDetail（Profile/Activity/Runtime/Tasks/Analytics 5 个 tab + 生命周期控制）/ MembersHumans 列表 / UserDetail |
| Depends on | [mobile-redesign-nav-framework.md](mobile-redesign-nav-framework.md)、[mobile-redesign-conversations.md](mobile-redesign-conversations.md)（"发消息"入口复用已定案的 DM 交互） |
| Mockup | [assets/mobile-redesign-members-mockup.html](../assets/mobile-redesign-members-mockup.html) |

## 1. 背景

第四批交付物，覆盖底部 Tab「Team」。设计前完整审计了 PC 端实现，有两个关键发现：

1. **`MembersMobile.tsx` 组件不存在**——只有一个孤立的测试文件 `MembersMobile.test.tsx`，实际测的是 `MembersSegmentControl` + `MembersAgents`/`MembersHumans`。仓库里唯一真实存在的 Members 模块移动端组件是 `MembersSegmentControl.tsx`（顶部 Humans/Agents 分段控件）。
2. **现有的移动端卡片已经默默丢了大量功能**：`Agents.tsx` 和 `MembersHumans.tsx` 里各自的 `md:hidden` 卡片列表，相比桌面表格分别丢了角色/负载/待处理/CLI-Model/最近活动/成员状态/工作者引用/批量操作，以及成员状态/邮箱/创建时间/最近登录/角色变更/禁用-启用操作。这正是"独立设计不等于功能缩水"这条项目原则要防的典型案例，本批把这些字段和操作找回来。

AgentDetail 是本产品信息量最大的详情页（5 个 tab，含完整生命周期状态机），UserDetail 相比之下是纯只读的薄页面——这个差异本身就是产品定位（agent-oriented）的体现，不需要强行拉平。

## 2. 页面清单与信息架构

| 视图 | PC 路由 | 类型 | 说明 |
|---|---|---|---|
| Agents | `/agents` | 列表 | Team Tab 之一 |
| AgentDetail | `/agents/:id` | 详情 | 5 个 tab：Profile/Activity/Runtime/Tasks/Analytics |
| MembersHumans | `/members/humans` | 列表 | Team Tab 默认页 |
| UserDetail | `/users/:userId` | 详情（只读） | |

顶部沿用 PC 端已有的 `MembersSegmentControl` 交互形状——Humans/Agents 分段控件，但视觉重新绘制以贴合本次设计语言（保留交互形状，不照搬样式代码）。

## 3. 视觉设计

### 3.1 Agents 列表

卡片补齐现有移动卡片缺失的字段，但不是把桌面 12 列表格原样塞进卡片，而是按"识别 agent 状态最需要看什么"重新分层：

1. 生命周期徽章（Running/Stopped/Error）+ 可用性徽章（Available）
2. 负载/待处理 chip（负载高时变色警示）
3. 角色 pill（Owner/Admin/Member）
4. 配置摘要（CLI/Model）+ 最近活动时间

Membership Status 为"—"（standalone/未加入组织的 agent）时不渲染成假的"Disabled"，与 PC 端行为一致。

### 3.2 AgentDetail

**头部生命周期控制**：横向可滑动的胶囊按钮行，按钮集合随当前生命周期状态动态变化（这套状态机在 PC 端本身就比较复杂，mockup 只画出 running 态的示例：发消息/Stop/Restart；完整的 状态→可见按钮 映射表留给 spec 附录/实现阶段用状态表逐条定义）。

**5 个 tab**（`tabstrip` 横向可滑动）：

- **Profile**：基本信息（Computer 在线状态/创建时间/创建者）、运行配置 tag（CLI/Model/Reasoning）、Auto-assignable 开关状态、并发上限、环境变量（只显示键名，不显示值，与 PC 端一致）、**Installed Skills**（按层级分组，`shadowed` 的技能保留删除线+"已被覆盖"标记，不拍平成一个列表）。
- **Activity**：事件流，分组折叠（Checking runs 折叠），手动刷新 + 加载更早分页，与上一批 ChannelDetail 的"加载更早消息"模式一致（同一套滚动分页交互语言，不重新发明）。
- **Runtime**：只读文件树浏览，做了**简化但不阉割**的处理——保留敏感文件屏蔽提示、Worker 离线时的"运行时不可用"整 tab 降级态；桌面版的 memory 目录 Content/History 双子 tab 简化为单个"History"入口标记，具体历史查看交互留实现阶段。
- **Tasks**：Current Task 卡片 + 所属 Plan 行（进度）+ 实时并发槽位组件（占用/上限进度条 + 排队数 + "实时"新鲜度标签）+ 只读任务列表（状态汇总统计 + 逐行状态）。
- **Analytics**：保留概览数字（本月任务数/花费）+ 活动热力图 + Top 花费任务；桌面版按模型/按项目切换视图的细分留后续（Deferred，不是删除）。

### 3.3 Reset 弹层

完整保留 PC 端的安全设计：scope 单选（Memory / Workspace / 全部）+ 二次确认勾选框，勾选框未选中时提交按钮保持置灰不可点——不简化成一个"确认"按钮。

### 3.4 MembersHumans 列表

卡片补回 Joined/Disabled 状态徽章（与 PC 端桌面表格的着色逻辑一致）和"⋯"行操作入口（变更角色/禁用/重新启用，与当前登录用户自己那一行不显示菜单——`isSelf` 门禁与 PC 端一致）。

### 3.5 UserDetail

维持只读、单薄的设计——PC 端本来就没有在这个页面放编辑/角色/禁用操作（那些都在 MembersHumans 的行菜单里），移动端不额外发明。自己查看自己时额外有 Account tab（改密码/登出），与 PC 端一致。

## 4. 功能覆盖清单

| 功能 | PC 端来源 | 移动端处理 |
|---|---|---|
| Agents 列表字段补全（角色/负载/待处理/CLI-Model/最近活动） | `Agents.tsx` 桌面表格 | Covered |
| Agents 批量多选 + 批量生命周期操作 | `BatchToolbar` | Deferred — 现有移动卡片完全没有，是否需要对应的"长按多选"模式待显式决策，不默默丢弃 |
| Agent 生命周期控制（Start/Stop/Restart/Reset/Archive/Force-delete） | AgentDetail 头部按钮组 | Covered（Start/Stop/Restart/Reset 已画）/ Deferred（Archive 确认弹层文案、Force-delete 的输入名称二次确认未画，但要求同等安全强度） |
| Agent 创建弹层 | `AgentCreateModal` | Deferred — 入口留后续，表单字段留实现阶段 |
| Agent 配置编辑弹层（并发/env vars/executor 白名单等） | `AgentConfigEditModal` | Deferred — 同上 |
| Installed Skills（分层 + shadowed 标记） | AgentProfile | Covered |
| Activity 分组折叠 + 分页 | AgentDetail Activity tab | Covered |
| Runtime 文件浏览（含敏感文件屏蔽、离线降级） | AgentRuntime | Covered（简化版）— memory 目录 Content/History 双 tab 的具体历史交互留实现阶段 |
| Current Task + Owning Plan | `AgentContextPanel` | Covered |
| 并发槽位实时组件（5 种模式：live/offline/expired/disabled/nodata） | `ConcurrencySlots` | Covered（画了 live 态）/ Deferred（其余 4 种模式的具体视觉留实现阶段，但确认要保留状态区分，不拍平成单一"忙/闲"） |
| Analytics（概览/热力图/Top花费/按模型-项目切换） | `AgentAnalyticsPanel` | Covered（概览/热力图/Top花费）/ Deferred（按模型/项目切换视图） |
| MembersHumans 状态徽章 + 行操作（变更角色/禁用/启用） | `MembersHumans.tsx` 桌面表格 | Covered |
| MembersHumans 无邀请入口（邀请在 OrganizationSettings/MemberNew） | 现状确认 | N/A — 确认后移动端不额外加"邀请"按钮 |
| UserDetail 只读 + 自查看时的 Account tab | `UserDetail.tsx` | Covered |
| SenderDetailSidebar（消息里点头像的精简预览） | 共享组件 | N/A（不属于本批页面，但需注意它不是 AgentDetail 的替代品，两者都保留） |
| SenderDetailSidebar 里 agent 专属的"创建提醒"（扫描限流重置时间预填） | 跨模块联动 | Deferred — 留给 Reminders 批次一并设计 |

## 5. 与其它批次的关系

- AgentDetail 头部"发消息"按钮复用 Conversations 模块已定案的 DM 交互（打开/复用与该 agent 的 1:1 会话）。
- Activity tab 的分组折叠 + "加载更早"分页复用上一批 ChannelDetail 消息流的滚动分页交互语言，不重新设计一套。

## 6. Out of Scope（本文档不覆盖）

- Agent 创建/配置编辑弹层的具体表单字段布局。
- Archive/Force-delete 确认弹层的具体文案与交互细节。
- MembersHumans 的邀请流程（OrganizationSettings/MemberNew，属于 Settings 模块，留给后续批次）。
- SenderDetailSidebar 本身的移动端处理（它出现在 Conversations/Workspace 模块里，不是 Members 模块的页面）。

## 7. 未来扩展

- Agents 列表批量操作是否需要移动端等价物（长按多选等），需要在实现阶段前明确决策。
- Runtime tab 的 memory git History 子视图、Analytics 的按模型/项目切换，本批只定了"要保留、要简化"的方向，具体交互留后续细化。
