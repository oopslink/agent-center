# 移动端 Workspace 核心页面设计

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-07-16 |
| Scope | Workspace 模块核心：Projects 列表 / ProjectDetail（表格详情，含 Issues/Tasks/Plans/Repos/Members 五个 tab）/ IssueDetail / TaskDetail / PlanDetail / Work Board（ProjectPlans，看板） |
| Depends on | [mobile-redesign-nav-framework.md](mobile-redesign-nav-framework.md)、[mobile-redesign-conversations.md](mobile-redesign-conversations.md)（工作项聊天区共享 `WorkItemConversation`，复用 Conversations 模块已定案的 Follow/Maximize/Threads/Files 规则） |
| Mockup | [assets/mobile-redesign-workspace-core-mockup.html](../assets/mobile-redesign-workspace-core-mockup.html) |

## 1. 背景

第三批交付物，覆盖底部 Tab「Work」（不是 Chat——Work 对应 Projects/Issues/Tasks/Plan/Repos/Templates，Chat 对应上一批的 Conversations，两者是并列模块，命名已在批次一/二混淆过，这里再次明确）。

设计前对 PC 端实现做了两轮功能审计：

1. Projects/ProjectDetail/IssueDetail/TaskDetail + `boardDrop.ts`/`useBoardTouchDrag.ts`/`WorkItemConversation`/`WorkItemFilterBar`/`AgentContextPanel`
2. 审计中发现一个结构性事实：**PC 端真正的看板不在 `ProjectDetail.tsx`**（那是一个纯表格的 tabbed 详情页），**而是独立页面 `ProjectPlans.tsx`**（路由 `/projects/:id/plans`，UI 上叫"Work Board"）。设计过程中一度把看板单独拆批，后按用户要求并回本批，一并出稿。

## 2. 页面清单与信息架构

| 视图 | PC 路由 | 类型 | 说明 |
|---|---|---|---|
| Projects | `/projects` | 列表 | Work Tab 默认页 |
| ProjectDetail | `/projects/:id` | 详情（表格） | 5 个 tab：Issues/Tasks/Plans/Repos/Members，**不含看板** |
| Work Board | `/projects/:id/plans` | 看板 | 真正的拖拽看板，从 ProjectDetail 头部"Work Board"链接或 Projects 卡片快捷操作进入 |
| PlanDetail | `/projects/:id/plans/:planId` | 详情（DAG） | 单个结构化 Plan 的执行详情，从 Work Board 的 Plan 列或 Plans tab 进入 |
| IssueDetail | `/projects/:projectId/issues/:id` | 详情 | |
| TaskDetail | `/projects/:projectId/tasks/:id` | 详情 | |

## 3. 视觉设计

### 3.1 Projects 列表

单一卡片样式：标题 + 状态徽章 + 描述 + 数量 chip（Tasks/Issues/Plans），右下角**唯一**的"⋯"入口 → 底部弹层列出全部快捷操作（Edit/Work Board/Issues/Tasks/Plans/Codebase）。

**修正记录**：初版 mockup 把"内联图标行"和"kebab 菜单"两种操作样式混在同一屏里（一张卡展示完整图标行，另一张只有 kebab），观感杂乱，已统一为唯一样式——所有卡片都是"信息 + 单一 ⋯ 入口"，不再区分展示态。这比照搬 PC 端"≥md 内联图标行、<md 收进 kebab"的响应式断点更彻底：移动端不需要保留桌面的内联态。

已归档 Projects 收进折叠分组（`disclosure`），懒加载，与上一批 Channels 的"已归档"分组同一视觉语言。

### 3.2 ProjectDetail

- 顶部统计磁贴（4 个数字块：Issues/Tasks/Plans/Repos），磁贴本身可点，等价于 PC 端"点数字跳 tab"。
- 下方 `tabstrip` 横向可滑动，承接 Issues/Tasks/Plans/Repos/Members 五个 tab，**不裁剪任何一个**——尤其 Plans tab 是**只读**（PC 端的约束：建 Plan 只能在 Work Board 做，ProjectDetail 里的 Plans tab 只能查看），移动端沿用同一约束，不额外加"新建 Plan"按钮。
- Members tab 严格复刻 PC 端的 owner 门禁：只有 `role==='owner'` 的当前用户才能看到"添加成员"入口和每行的移除操作，非 owner 视角这些控件整体不渲染（不是置灰，是不存在）。
- Repos tab：主仓库星标（影响 merge-check 行为，非装饰）、provider 徽章、默认分支、解除引用。

### 3.3 IssueDetail / TaskDetail

默认**聊天优先**：状态横幅 + 聊天区占满屏幕，描述/标签/项目链接/派生任务/审计时间线折叠进顶栏 ⓘ 触发的底部信息抽屉。这是对 PC 桌面版"默认展开描述 + 常驻侧栏"的独立重设计，但抽屉内的信息颗粒度与桌面侧栏一一对应，一条不少（见 §4 功能覆盖）。

TaskDetail 额外有：

- **Assignee** 标识（agent=紫色圆点+机器人图标 / human=青色圆点+首字母），点击 agent 头像跳转 Agent 活动侧栏（复用 Conversations 模块已定案的"点击头像开侧栏"交互）。
- **Stuck 横幅**（`status==='running' && blocked_reason` 时出现）——琥珀色高优先级横幅，常驻在状态栏下方**不折叠进抽屉**，因为这是要求用户立即介入的信号，折叠会导致错过。
- Composer 占位文案明确写"会唤醒对应 Agent"——发消息是真实的 agent 控制动作，不是普通评论区，这是本产品的核心差异化语义，移动端必须保留这个提示强度。

Discarded（终态）的 Issue/Task 隐藏 Edit 入口——移动端复刻这个条件渲染，不是"一律显示编辑按钮"。

### 3.4 PlanDetail（DAG → 竖向 stepper）

PC 端 DAG 图形化画布在移动端独立重设计为**竖向 stepper**：节点按依赖顺序竖排，连线 + 状态点（绿=完成 / 紫色光晕=进行中 / 灰=等待前置任务），每个节点带 Task ID、标题、assignee、状态文案（含 Stuck 标注）。

DAG 的图形化编辑（拖拽连接依赖边）保留为桌面端能力；移动端不做画布编辑，draft 计划的依赖边增删走 Task 列表 tab 内的表单式编辑（本文档只定"走表单"这个方向，具体表单字段留待实现阶段）。

### 3.5 Work Board（ProjectPlans）

PC 端是横向多列 kanban + 鼠标拖拽；已有的移动端妥协方案是"横向滚动 + scroll-snap + 长按拖拽 + 竖屏旋转提示"——本质是把桌面横向结构硬塞进小屏。本批**独立重设计**：

- **单列竖排**：顶部列选择 chip（Backlog / Assignment Pool / 每个结构化 Plan / ＋新建 Plan）横向可滑动，一次只看一列，卡片竖排比横向多列窄条更好读更好点。
- **移动卡片不用拖拽**：卡片上的"移动"图标 → 底部弹层列出全部目的列，锁定的列（运行中/已完成/已归档的结构化 Plan）在弹层里直接标"已锁定"且置灰不可点——**错误提前到操作前**，而不是允许点击后再报错。这个弹层本质是独立重设计版的 PC 端"Add to plan"键盘可达下拉菜单（PC 端本来就有这个非拖拽的无障碍替代路径），移动端和桌面无障碍路径殊途同归，都不依赖鼠标/触控的精确拖拽。
- **Claimable / Starved** 用色块 pill 强区分（绿色 Claimable=已派发等待认领，红色 Starved=没有满足 `required_capabilities` 的在线 agent）——纯 agent 编排概念，无人类 PM 工具对应物，移动端沿用 PC 端颜色语义。
- 底部悬浮 FAB "＋新建 Task"（对应 PC 端"+ New Task"，选目的地：Backlog/Pool/某 draft Plan，表单留待实现阶段）。

## 4. 功能覆盖清单

延续上一批的三态标注（Covered / Deferred / N/A）。

| 功能 | PC 端来源 | 移动端处理 |
|---|---|---|
| Projects 快捷操作（Edit/Board/Issues/Tasks/Plans/Codebase） | `Projects.tsx` per-card shortcuts | Covered — 统一收进单一"⋯"底部弹层 |
| 已归档 Projects（懒加载折叠） | `useArchivedProjects` | Covered |
| Project 编辑（含 auto-assign 主开关） | `ProjectEditModal` | Deferred — 入口已留，表单字段留实现阶段 |
| 创建 Project/Issue/Task | 对应 Create 弹层 | Deferred — 入口已留，表单字段留实现阶段 |
| ProjectDetail 统计磁贴 + Recent Activity | 客户端合并 issues/tasks/plans top-3 | Covered（磁贴）/ Deferred（Recent Activity 列表本身未逐条画出） |
| WorkItemFilterBar（状态多选/assignee/创建-更新时间范围） | `WorkItemFilterBar` | Deferred — 入口（筛选+计数徽标）已留，弹层内容留实现阶段 |
| Plans tab 只读约束 | ProjectDetail Plans tab | Covered — 明确不加"新建 Plan"入口 |
| Repos tab（主仓库星标/provider/解除引用） | `CodeReposPanel` | Covered |
| Members tab（owner 门禁的邀请/移除） | `MembersPanel` | Covered |
| Issue/Task 默认聊天优先 + 信息抽屉 | 对照 PC 桌面默认展开侧栏 | Covered |
| Discarded 隐藏 Edit 入口 | Issue/TaskDetail 条件渲染 | Covered |
| TaskDetail Stuck 横幅（`blocked_reason`） | ADR-0046 | Covered |
| Assignee agent/human 视觉区分 + 点击 agent 跳侧栏 | `AssigneeBadge` | Covered |
| required_capabilities（能力标签） | Task 编辑字段 | Covered（只读展示于信息抽屉）/ Deferred（编辑表单） |
| Priority 字段 | Task 表格列 | N/A — schema 里目前恒为空，移动端不新增编辑入口 |
| 派生任务 / 关联 Plan 只读链接 | Issue/TaskDetail sidebar | Covered |
| 审计时间线（Object Audit Timeline） | `ObjectAuditTimeline`，桌面端 `<md hidden` | Covered — 移动端补上了展示（PC 端反而在移动断点下完全没有），信息抽屉里列出事件流 |
| PlanDetail DAG → 竖向 stepper（只读展示） | `PlanStepper` 现有实现的独立重设计 | Covered |
| PlanDetail DAG 依赖边增删（draft 专属） | 桌面画布编辑 | Deferred — 确认移动端走表单式编辑，具体表单留后续 |
| Work Board 横向多列 → 单列竖排 + 列选择 chip | `ProjectPlans.tsx` | Covered |
| Work Board 拖拽 → 显式"移动到"底部弹层 | 独立重设计，对照 PC 端"Add to plan"无障碍菜单 | Covered |
| 长按拖拽（`useBoardTouchDrag.ts`） | 现有移动端实现 | Deferred — 本设计默认不复用（弹层是主交互），是否作为同列排序等场景的可选增强，需要实现阶段显式决策 |
| Claimable / Starved 徽章 | Assignment Pool 卡片 | Covered |
| Plan 列锁定（运行中/已完成/已归档） | 板级 drop 校验 | Covered — 弹层里对应选项直接置灰，不是点击后报错 |
| "新建 Plan" / "新建 Task" 表单 | `BoardTaskCreateModal` 等 | Deferred — 入口已留（列 chip 末尾/FAB），表单留实现阶段 |
| Follow/unfollow、Maximize、Threads/Files 子面板 | `WorkItemConversation`（与 Conversations 模块共享） | Covered — 复用上一批已定案的规则，不重新设计 |
| 消息反应/表情、编辑、删除、置顶、已读回执、输入中提示、会话内搜索 | （PC 端均不存在，同上一批结论） | N/A |

## 5. 与全局导航框架 / Conversations 模块的关系

- 顶栏 ⓘ → Context Panel 底部弹层，在 Issue/TaskDetail 语境下承载"信息抽屉"（描述/标签/审计时间线等），复用导航框架已定义的弹层语义，只是内容换成工作项元信息而非频道信息。
- Issue/TaskDetail 内嵌的 `WorkItemConversation` 完整复用 Conversations 模块批次已定案的规则（长按消息操作条、日期分隔线、Follow 星标、Maximize、Threads/Files 子分段），不重新设计一套。

## 6. Out of Scope（本文档不覆盖）

- Project/Issue/Task 各类创建与编辑弹层的具体表单字段布局。
- WorkItemFilterBar 筛选弹层的具体交互（状态多选、assignee 单选、日期范围选择器在移动端的控件形态）。
- PlanDetail 依赖边增删的表单细节。
- Work Board 的"新建 Plan”/“新建 Task”表单细节。
- Templates 页面（Workspace 模块内还有一个 `templates` 路由，未在本批调研范围内，留给后续批次）。

## 7. 未来扩展

- 长按拖拽是否在 Work Board 保留为"移动到"弹层之外的可选增强（例如同列内排序），需要实现阶段显式决策，不能默默复用也不能默默丢弃。
- 三处头像堆叠实现（沿用上一批发现的重复问题，此处新增 AssigneeBadge 与之类似）建议在实现阶段统一收敛成一个共享组件。
