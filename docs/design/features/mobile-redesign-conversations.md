# 移动端 Conversations 模块页面设计

| Field | Value |
|---|---|
| Status | Implemented（批次二，2026-07-17）——详情页部分见 §8 实现记录 |
| Date | 2026-07-12 |
| Scope | Conversations 模块的 5 个视图：Unread / Channels / DMs（列表）+ ChannelDetail / DMDetail（详情），及其挂载的 Thread 详情、长按消息操作条 |
| Depends on | [mobile-redesign-nav-framework.md](mobile-redesign-nav-framework.md)（底部 Tab + 二级导航抽屉 + Context Panel/Attention 底部弹层） |
| Mockup | [assets/mobile-redesign-conversations-mockup.html](../assets/mobile-redesign-conversations-mockup.html) |

## 1. 背景

这是"完整调研 PC 端页面，独立设计移动端页面"项目的第二批交付物，落在全局导航框架之后。PC 端 Conversations 模块归属底部 Tab 的 **Chat**（不是 Workspace／Work——设计过程中一度混淆两者，Workspace 对应 Projects/Issues/Tasks/Plan，Conversations 对应 Channels/DMs，两者是并列的独立模块）。

设计前对 PC 端 5 个页面 + 共享消息组件（`ConversationView`/`MessageList`/`MessageComposer`/`ConversationSidebar`/`ConversationMobileTabs`）做了完整功能审计，见 §4 功能覆盖清单——移动端独立设计不代表可以随意精简功能，审计的目的是保证"独立设计"不等于"功能缩水"。

## 2. 页面清单与信息架构

| 视图 | PC 路由 | 类型 | 挂载导航框架的位置 |
|---|---|---|---|
| Unread | `/unread` | 列表 | Chat Tab → 二级分段「Unread」（默认） |
| Channels | `/channels` | 列表 | Chat Tab → 二级分段「Channels」 |
| DMs | `/dms` | 列表 | Chat Tab → 二级分段「DMs」→ 三级 subtabs（我的/Agent互聊/系统） |
| ChannelDetail | `/channels/:id` | 详情 | 从 Channels 行进入，顶栏"‹ 返回" |
| DMDetail | `/dms/:id` | 详情 | 从 DMs 行进入，顶栏"‹ 返回" |
| Thread 详情 | （无独立路由，PC 端是 col④ 侧栏） | 详情的子详情 | 从消息的 thread pill 或 ChannelDetail 的 Threads 子分段进入 |

**列表页三段导航**：Unread/Channels/DMs 用页内常驻的横向分段控件（`segtabs`）切换，不依赖点击顶栏 ☰ 才能看到——这是对全局导航框架"二级导航默认走抽屉"规则的一个**有意例外**：Conversations 三个来源之间切换是高频操作，做成常驻分段比多一次抽屉展开更符合使用频率。

**DMs 三级 subtabs**：DMs 内部对应 PC 端 `dm_type` 字段有三类会话——默认（我的）/ `agent_agent_dm`（Agent 互聊）/ `system_dm`（系统），移动端用 DMs 分段下的第二层 `subtabs` 承接，不合并成一个平铺列表，因为三类的操作权限不同（Agent 互聊对人类只读旁观，无法发言）。

## 3. 视觉设计

### 3.1 设计 token

沿用项目实际 CSS 变量（`web/src/index.css` 亮色主题）：品牌紫 `--color-brand: #7C3AED`、暖白背景 `--color-bg-base: #FFFCFC`、成功绿 `--color-success: #5CB198`、危险红 `--color-danger: #D44040`。深色主题变量对称存在，本文档未逐一给出深色 mockup，实现阶段直接换算对应的 dark token。

### 3.2 图标规范

全部使用线性描边 SVG（`stroke-width: 1.8`），不使用 emoji 字符——emoji 在不同系统/字体下渲染不一致，且与本项目其它页面的图标风格（细描边 SVG，见 `AppLayout.tsx` 的 `FolderIcon`/`ChatIcon` 等）不符。

### 3.3 列表行

- 头像：38px（列表）/ 26px（消息气泡）/ 24px（顶栏），色板取品牌色系轮换（紫/绿/棕）区分不同人，不用随机色避免视觉噪音。
- Unread 行：来源标签（Channel/DM/Task 三色）+ 加粗发送人名 + 单行预览；@ 提及用红色角标区分于普通未读数字角标（紫色）。
- DMs 行：头像右下角挂在线状态点——绿=在线、灰=离线、紫=Agent（Agent 视为常驻在线，颜色与人类的在线绿区分）。

### 3.4 消息流

- 按天插入日期分隔线（今天/昨天/具体日期）。
- 他人消息浅色卡片靠左、自己消息品牌紫靠右。
- 消息操作（复制/引用/开 Thread）走**长按触发**的悬浮操作条，贴靠在被长按消息旁，不做桌面 hover 那种常驻工具条；被操作消息加高亮描边表明当前操作对象。
- 有讨论串的消息下方挂"N 条回复" thread pill，点击进入 Thread 详情（复用 Threads 子分段的展示逻辑）。Thread 详情页根消息钉在顶部浅底卡片区分于回复列表，顶栏显示所属频道。
- 顶部"加载更早的消息"（对应 PC 端游标分页）；滚动到底部之外时右下角悬浮"跳到最新"圆按钮；新消息到达时额外弹出"↓ N 条新消息"胶囊——两者互斥，对应 PC 端两种不同触发条件（一般性滚出可视区 vs 有新消息到达时滚出可视区）。

### 3.5 详情页头部

- ‹ 返回 + 标题；Follow/Unfollow 星标图标（对应 PC 端 `FollowToggle`）；ⓘ 触发 Context Panel 底部弹层（导航框架已定义）。
- DMDetail 额外有"更多"溢出菜单（当前仅一项：复制链接，与 PC 端一致，不新增功能）。
- ChannelDetail 顶部有 Chat/Threads/Files/People 四个子分段；DMDetail 只有 Chat/Threads/Files 三个（DM 成员固定，无 Participants 面板）。

## 4. 功能覆盖清单

审计 PC 端全部实现后确认的功能点，逐项标注移动端处理方式。**Covered** = 本批 mockup 已可视化；**Deferred** = 确认保留但留给实现阶段细化，不在本 spec 展开视觉稿；**N/A** = PC 端本来就不存在，移动端不臆造。

| 功能 | PC 端来源 | 移动端处理 |
|---|---|---|
| 全部标记已读 | `useMarkAllConversationsRead` | Covered — Unread 页顶部按钮 |
| 单条标记已读 | （不存在） | N/A — PC 端也没有，不新增 |
| All/Mentions/Unread 筛选 | `Unread.tsx` filter state | Covered — 筛选 chip + 实时计数 |
| 归档频道（单行操作） | `useArchiveConversation` | Covered — 行内独立图标按钮，非整行点击 |
| 已归档频道列表 | `useArchivedChannels`（懒加载） | Covered — 折叠分组，懒加载，只读 |
| 删除 DM（单行操作+二次确认） | `useDeleteConversation` + `ConfirmModal` | Covered — 行内独立图标按钮；二次确认弹窗沿用导航框架的底部弹层语义 |
| DMs 三类 subtabs（我的/Agent互聊/系统） | `dm_type` 过滤 | Covered |
| Follow/Unfollow | `FollowToggle` | Covered — 详情页顶栏星标 |
| 溢出菜单：复制链接 | DMDetail `<details>` | Covered |
| Quote/引用回复 | `QuoteProvider` | Covered — 长按操作条 |
| Thread（独立于 Quote） | `ThreadButton` + `ThreadSidebar` | Covered — thread pill + 独立 Thread 详情页 |
| 消息复制 | `MessageCopyButton` | Covered — 长按操作条 |
| 附件上传（多选/拖拽/粘贴） | `MessageComposer` | Covered（合并为一个"添加附件"入口，移动端没有拖拽/粘贴场景，多选覆盖原有三种桌面入口） |
| @/# 提及自动补全（含 @all 广播） | Composer mention picker | Covered（composer 占位文案提示，交互细节留实现阶段） |
| 加载更早消息（游标分页） | `ConversationView` scroll pagination | Covered |
| 跳到最新 / 新消息提示 | `ConversationView` scroll state | Covered |
| SSE 实时更新 | `useSSEConversationSubscribe` | Deferred — 无独立视觉，行为不变，接入方式留实现阶段 |
| 已读游标自动推进 | `ConversationView` mount/新消息 effect | Deferred — 无独立视觉，行为不变 |
| Participants 面板（owner 专属邀请/移除） | `ConversationSidebar` People 分段 | Deferred — 仅 owner 可见的操作入口，留待专门设计一次面板细节 |
| 系统消息/agent 失败通知折叠 | `MessageList` 系统行 | Deferred — 需要单独定义紧凑折叠态样式 |
| Maximize/restore 聊天框切换 | 现有 `useConversationMaximize`（mobile-only） | Deferred — 待决策：若新导航框架下详情页本身已全屏，此开关可能不再需要，需实现阶段显式定案，不默默保留也不默默丢弃 |
| 消息反应/表情、编辑、删除、置顶、已读回执、输入中提示、会话内搜索 | （PC 端均不存在） | N/A — 逐一确认过 PC 端代码里没有这些功能，移动端不新增 |

## 5. 与全局导航框架的关系

- Context Panel 触发：ChannelDetail/DMDetail 的 ⓘ 按钮 → 导航框架定义的 Context Panel 底部弹层，内容为频道描述/成员/文件（对应 PC col④ `ConversationSidebar`）。
- 二级导航抽屉（点顶栏 ☰）在 Conversations 模块内**不是主要切换方式**——三个列表页之间靠页内常驻分段控件切换（见 §2），☰ 抽屉仍然存在，作为"从其它模块切回 Conversations 默认视图"的入口，但不是 Unread/Channels/DMs 之间日常切换的路径。

## 6. Out of Scope（本文档不覆盖）

- Participants 面板邀请/移除成员的完整交互细节（弹窗内容、搜索、多选）——留给后续专项设计。
- 系统消息折叠态的具体视觉规格——留给后续专项设计。
- Workspace（Projects/Issues/Tasks/Plan）、Members、Settings 等其它模块的页面——留给后续批次。

## 7. 未来扩展

- ~~Maximize/restore 切换是否保留，需要在进入实现阶段前明确决策（见 §4）。~~ 已决策，见 §8.1。
- 三处头像堆叠实现（ChannelDetail 头部 / Participants 面板 / Channels 列表行）目前 PC 端是三套独立实现，移动端重新设计时建议收敛成一个共享组件，避免三次重复维护成本——这是实现阶段的重构建议，不影响本文档的视觉/交互设计。

## 8. 实现记录（批次二）

列表页（Unread / Channels / DMs）的分段控件、筛选 chip、归档折叠分组、DMs 三类 subtabs 在本批之前已由 T129 / T343 / v2.9.1 落地，本批不重做。本批的实际交付是**详情页**：用新的
`ConversationSurfaceMobile` + `ConversationInfoPanel` 替换掉重做时代的 `ConversationMobileTabs`（后者连同其专属的 `useConversationMaximize` 一起删除，生产调用者为零）。

生产调用者：`pages/ChannelDetail.tsx`（`/channels/:channelId`）、`pages/DMDetail.tsx`（`/dms/:id`）、
`components/WorkItemConversation.tsx`（issue/task/plan 的内嵌聊天，遵循 workspace-core §5 的复用要求）。

### 8.1 决策：移动端 Maximize 切换 —— **丢弃**

§4 与 §7 要求实现阶段对 Maximize/restore 显式定案（"不默默保留也不默默丢弃"）。**决策：丢弃**。
旧的移动端 maximize 只是因为当时聊天框嵌在一个长滚动详情页里、需要"逃出去"才存在；在批次一的导航框架下详情页本身就是全屏surface，maximize 等于把全屏提升为全屏，没有语义。

注意：`WorkItemConversation` **桌面端**横幅上的 maximize 保留 —— 那个场景聊天框确实嵌在长页面里，与本决策无关。

### 8.2 §3.5 与 §5 的重叠：按 mockup 解读为两种信息密度

§3.5（Chat/Threads/Files/People 子分段）与 §5（ⓘ → 频道描述/成员/文件）在文字上读起来重叠。mockup 帧 ④ 与 ⑦ 给出了解读：两者是**同一批信息的两种密度**，不是重复实现——

- **子分段** = 完整可交互面板（`ParticipantsPanel` / `ConversationThreadList` / `SharedFilesPanel`）。
- **ⓘ 底部弹层** = 只读身份卡：标题 + 描述 + 成员预览 + 文件预览（各截断 5 条 + "+N"）。

ⓘ 按钮本身**复用** `ContextPanelMobileButton`（bd895284 为 Issue/Task/PlanDetail 加的共享入口），不另造一个。那个 commit 明确把 Channel/DM 排除在外，理由是"两者在移动端渲染 `ConversationMobileTabs` 而非 `<ContextPanel>`，加 ⓘ 只会打开一个空弹层"——本批补上的正是缺的那块（弹层内容），所以同一个按钮现在在 Channel/DM 上也成立。

顺带修掉一个 bd895284 带进 main 的缺陷：`ContextPanelMobileButton` 读 `shell.contextPanel.openMobileSheet`，但该 key 在 en/zh 都不存在（defaultNS=common），i18next 回退成把 key 原样当文案，ⓘ 的 aria-label 实际是字面量 `shell.contextPanel.openMobileSheet`。原测试只用 test-id 查询，漏掉了。已补 key + 一个断言真实文案的回归测试。

### 8.3 仍然 Deferred（本批未动，与 §4 一致）

Participants 面板的 owner 门禁细节、系统消息折叠态、SSE/已读游标（无独立视觉，行为不变）。
