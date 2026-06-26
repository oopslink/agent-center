# All Pages UI/UX Remediation Plan

> Date: 2026-06-26
> Scope: 全部 31 个页面 + AppLayout shell
> Target: PC + Mobile 双端

---

## Page Index

| # | Page | Category | Priority | Key Issues |
|---|------|----------|----------|------------|
| 1 | [AppLayout (Shell)](#1-applayout-shell) | Shell | P0 | 7 处触控目标 < 44px, drawer 缺 aria-label |
| 2 | [Agents](#2-agents) | List | P1 | 无移动端卡片视图, checkbox 16px, 按钮 < 44px |
| 3 | [AgentDetail](#3-agentdetail) | Detail | P2 | Activity section 缺 role, tab 模式良好 |
| 4 | [Projects](#4-projects) | List | P2 | 快捷操作按钮 28px, 移动端 kebab 偏小 |
| 5 | [ProjectDetail](#5-projectdetail) | Detail | P1 | 表格无移动端视图, 分页链接偏小 |
| 6 | [TaskDetail](#6-taskdetail) | Detail | P0 | 标题占高过多, 双 bar 需合并, min-h 硬编码 |
| 7 | [IssueDetail](#7-issuedetail) | Detail | P0 | 同 TaskDetail + sidebar 对齐问题 |
| 8 | [PlanDetail](#8-plandetail) | Detail | P1 | Tab/toggle 28px, Actions dropdown 项 < 44px, header 信息密度 |
| 9 | [Channels](#9-channels) | List | P2 | Archive 按钮 30px, 创建日期移动端隐藏无替代 |
| 10 | [ChannelDetail](#10-channeldetail) | Detail | P2 | 已有 MobileTabs (好), composer 32px |
| 11 | [DMs](#11-dms) | List | P2 | Tab/delete 按钮 36px |
| 12 | [DMDetail](#12-dmdetail) | Detail | P2 | 已有 MobileTabs (好), composer 32px |
| 13 | [MembersAgents](#13-membersagents) | List | OK | 已有 44px 卡片 (好) |
| 14 | [MembersHumans](#14-membershumans) | List | OK | 已有 44px 卡片 (好) |
| 15 | [Secrets](#15-secrets) | List | P1 | 无移动端视图, Revoke 32px |
| 16 | [Environment](#16-environment) | Settings | P2 | Worker 操作按钮 36px |
| 17 | [Reminders](#17-reminders) | List | P2 | 操作 icon 14px, filter chip 28px |
| 18 | [OrgPlans](#18-orgplans) | List | P2 | 链接按钮 36px, 移动端卡片已有 44px (好) |
| 19 | [OrgWorkItems](#19-orgworkitems) | List | OK | 委托 OrgWorkItemsView |
| 20 | [ProjectPlans](#20-projectplans) | List | P2 | FAB 56px (好), add-to-plan 30px |
| 21 | [Unread](#21-unread) | List | P2 | Filter chip 28px |
| 22 | [Settings](#22-settings) | Settings | OK | 无显著问题 |
| 23 | [OrganizationSettings](#23-organizationsettings) | Settings | P2 | 表格内按钮 36px |
| 24 | [Me](#24-me) | Settings | P2 | Input/button 32-36px |
| 25 | [Signin](#25-signin) | Auth | P2 | Input/button 36px |
| 26 | [Signup](#26-signup) | Auth | P2 | 同 Signin + passcode hint 缺 aria-describedby |
| 27 | [MemberNew](#27-membernew) | Auth | P2 | Input/button 32px |
| 28 | [InvitationAccept](#28-invitationaccept) | Auth | P3 | 按钮 36px |
| 29 | [UserDetail](#29-userdetail) | Detail | P2 | Tab 无 44px, definition list 移动端溢出 |
| 30 | [WorkerDetail](#30-workerdetail) | Detail | P2 | Tab 无 44px |
| 31 | [Version / NotFound](#31-version--notfound) | Misc | OK | 无交互元素 |

---

## Per-Page Remediation

### 1. AppLayout (Shell)

**Role:** 全局导航壳，所有页面的容器。移动端 sidebar → drawer。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| Hamburger button | :183 | `h-8 w-8` (32px) | `h-11 w-11 md:h-8 md:w-8` |
| Sidebar group toggle | :425 | `px-1` (~28px) | `py-2.5 md:py-1` |
| Subitem toggle chevron | :507 | `p-1` (~20px) | `p-2.5 md:p-1` |
| DM delete button | :549 | `h-6 w-6` (24px) | `h-11 w-11 md:h-6 md:w-6` |
| Org switcher | :714 | `py-1.5` (36px) | `py-2.5 md:py-1.5` |
| Search (collapsed) | :759 | `h-9` (36px) | `h-11 md:h-9` |
| Theme toggle (collapsed) | :1231 | `h-9` (36px) | `h-11 md:h-9` |
| Drawer missing aria-label | :626 | — | `aria-label="Mobile navigation"` |

---

### 2. Agents

**Role:** Agent 列表。桌面端表格 + 批量操作。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| 无移动端替代 | :175-338 | `min-w-[60rem]` 横滚表格 | 参考 MembersAgents: `hidden md:block` (table) + `md:hidden` (card) |
| Checkbox 触控 | :188, 242 | `h-4 w-4` (16px) | 外包 44px touch area: `p-3 md:p-0` wrapper |
| Delete 按钮 | :328 | `px-2 py-1` (~36px) | `py-2 md:py-1` |
| Batch Start/Stop | :437-440 | `px-2.5 py-1` (~36px) | `py-2 md:py-1` |
| Table 缺 `<caption>` | :177 | — | `<caption class="sr-only">Agent list</caption>` |

---

### 3. AgentDetail

**Role:** Agent 详情 + 活动流 + 诊断。已有较好的移动端适配。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| Activity section 缺 role | :368 | `<section>` | `role="region" aria-label="Activity"` |
| Tab 触控模式 | :322-339 | 父级 `[&>button]:min-h-[44px]` | 已做 ✓ (好模式，推广到其他页) |

---

### 4. Projects

**Role:** 项目列表。移动端已有 kebab 菜单。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| 快捷操作 icon | :273, 284 | `h-7 w-7` (28px) | 仅桌面展示，移动端走 kebab — OK |
| Kebab 按钮 | :298 | `h-7 w-7` (28px) | `min-h-[44px] min-w-[44px] md:h-7 md:w-7` |
| Menu items | :321 | `px-2 py-1.5` (~36px) | `py-2.5 md:py-1.5` |

---

### 5. ProjectDetail

**Role:** 项目详情 + tasks/issues/plans tabs + transfers 表格。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| 表格无移动端视图 | :414-451, :835 | Desktop table only | 添加 `md:hidden` card view |
| 分页链接 | :451 | 小文本链接 | 增加 padding `py-2 px-3` |
| Add member 按钮 | :560 | `px-2 py-1` (~36px) | `py-2 md:py-1` |

---

### 6. TaskDetail

**Role:** Task 详情 + 嵌入 chat。移动端核心页面。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| 标题区占高过多 | :114-143 | breadcrumb + multi-line title + badge ~120px | back arrow + 单行 ellipsis ~30px |
| 双 bar 需合并 | :149-204 | MobileWorkItemBar + Conv banner 各一行 | 合并为 unified bar (ID+status+assignee+Actions▾+⤢) |
| Chat 高度硬编码 | :201 | `min-h-[60vh]` | `flex-1` |
| Composer 按钮 | (MessageComposer) | `h-8 w-8` (32px) | `h-11 w-11` (44px) on `pointer:coarse` |
| Loading 纯文本 | :58-63 | `<section>Loading task…</section>` | `<Skeleton>` + `role="status"` |
| Error 缺 role | :68 | `<p class="text-danger">` | `role="alert"` |

---

### 7. IssueDetail

**Role:** Issue 详情 + 嵌入 chat。结构同 TaskDetail。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| 同 TaskDetail 所有问题 | :77-208 | — | 同上 |
| Sidebar 对齐 (DT-1) | (desktop) | EmbeddedConversationSidebar `w-9` 竖条 | 方案 A: toggle 内移至 conv banner |

---

### 8. PlanDetail

**Role:** Plan 执行视图 + DAG + Chat tabs。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| Tab 按钮 | :168-176 | `px-3 py-1` (~28px) | `min-h-[44px] md:min-h-0` |
| Maximize toggle | :189 | `h-7 w-7` (28px) | `h-10 w-10 md:h-7 md:w-7` |
| DAG compact toggle | :204 | `h-7 w-7` (28px) | `h-10 w-10 md:h-7 md:w-7` |
| Actions dropdown 内项 | :341-397 | `py-1.5 text-xs` (~28px) | 移动端 `min-h-[44px] w-full` |
| Header 移动端密度 | :312-315 | 全部 flex-wrap | 提取 compact bar (类似 MobileWorkItemBar) |

---

### 9. Channels

**Role:** Channel 列表。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| Archive 按钮 | :138 | `py-1 text-xs` (~30px) | `py-2 md:py-1` |
| 创建日期移动端隐藏 | :119 | `hidden sm:inline` 无替代 | 考虑显示相对时间 "3d ago" |

---

### 10. ChannelDetail

**Role:** Channel 详情 + 消息。已有 ConversationMobileTabs (好)。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| Mobile tab 高度 | (ConversationMobileTabs:72) | `py-1.5` (~24px) | `min-h-[44px]` |
| Composer 按钮 | (MessageComposer:447,465) | `h-8 w-8` (32px) | `h-11 w-11` on `pointer:coarse` |

---

### 11. DMs

**Role:** DM 列表 + All/Agent/Human tabs。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| Tab 按钮 | :85, 96 | `px-3 py-1` (~36px) | `min-h-[44px] md:min-h-0` |
| Delete 按钮 | :163 | `px-2 py-1` (~36px) | `py-2 md:py-1` |

---

### 12. DMDetail

**Role:** DM 详情 + 消息。已有 ConversationMobileTabs (好)。

同 ChannelDetail 的 tab 高度 + composer 问题。

---

### 13. MembersAgents

**Role:** Agent 成员列表。**已做好移动端适配** ✓

- 移动端卡片视图 `md:hidden` ✓
- DM 按钮 `min-h-[44px] min-w-[44px]` ✓
- Card link `min-h-[44px]` ✓

**推广此模式到 Agents / Secrets / ProjectDetail 等缺少移动端视图的列表页。**

---

### 14. MembersHumans

同 MembersAgents，**已做好** ✓

---

### 15. Secrets

**Role:** Secrets 管理列表。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| 无移动端视图 | :68-125 | Desktop table only | 添加 `md:hidden` card view |
| Revoke 按钮 | :115 | `px-3 py-1 text-xs` (~32px) | `py-2 md:py-1` |

---

### 16. Environment

**Role:** Worker 管理 + 系统环境。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| Worker 操作按钮 | :972, 980, 1052 | `px-2 py-1 text-xs` (~36px) | `py-2 md:py-1` |

---

### 17. Reminders

**Role:** 定时提醒列表 + CRUD。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| 操作 icon 按钮 | :272-298 | `h-3.5 w-3.5` (14px) 无 padding | 外包 `p-2.5` wrapper 达 44px |
| Filter chips | :185-187 | `px-2.5 py-0.5` (~28px) | `py-1.5 md:py-0.5` |
| New 按钮 | :159 | `px-3 py-1.5` (~36px) | `py-2 md:py-1.5` |

---

### 18. OrgPlans

**Role:** Org 级 Plan 列表 + 搜索。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| 移动端卡片 | :264 | `min-h-[44px]` ✓ | 已做好 |
| Summary 链接按钮 | :364 | `px-2.5 py-1 text-xs` (~36px) | `py-2 md:py-1` |

---

### 19. OrgWorkItems

委托 `OrgWorkItemsView` 组件。**无额外问题。**

---

### 20. ProjectPlans

**Role:** 项目级 Plans + backlog。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| FAB | :193 | `h-14 w-14` (56px) ✓ | 已做好 |
| Add-to-plan 按钮 | :565 | `px-1 py-1 text-[0.6875rem]` (~30px) | `py-2 md:py-1` |
| Menu items | :665 | `px-2 py-1.5` (~36px) | `py-2.5 md:py-1.5` |
| Pause/resume icons | :272 | `h-3.5 w-3.5` (14px) | 同 Reminders: 外包 padding wrapper |

---

### 21. Unread

**Role:** 未读消息列表。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| Filter chips | :151 (FilterChip:37) | `px-3 py-1` (~36px) | `min-h-[44px] md:min-h-0` |

---

### 22. Settings

**无显著问题。** ✓

---

### 23. OrganizationSettings

**Role:** 组织设置 + 成员管理。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| Role select | :179 | `px-2 py-1` (~36px) | `py-2 md:py-1` |
| Disable/Drop 按钮 | :192-196 | `text-xs` 无 min-height | `min-h-[44px] md:min-h-0` |
| Copy 按钮 | :262 | `px-3 text-sm` (~36px) | `py-2 md:py-1` |

---

### 24. Me

**Role:** 个人设置。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| Input fields | :108 | `py-1.5` (~32px) | `py-2 md:py-1.5` |
| Submit / Sign out | :140, 153 | `py-1.5` (~36px) | `py-2 md:py-1.5` |

---

### 25. Signin

**Role:** 登录页。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| Input fields | :56, 72 | `py-2` (~36px) | `py-2.5 md:py-2` |
| Submit button | :77 | `py-2` (~36px) | `py-2.5 md:py-2` |

---

### 26. Signup

**Role:** 注册页。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| Input/button | :61-64, 228 | `py-2` (~36px) | `py-2.5 md:py-2` |
| Passcode hint | :200-203 | hint 未关联 input | `aria-describedby="passcode-hint"` |

---

### 27. MemberNew

**Role:** 创建成员表单。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| Input/select/button | :115, 180, 189, 195 | `py-1.5` (~32px) | `py-2 md:py-1.5` |

---

### 28. InvitationAccept

**Role:** 邀请接受页。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| Accept 按钮 | :25 | `py-2` (~36px) | `py-2.5 md:py-2` |

---

### 29. UserDetail

**Role:** 用户详情 + tabs。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| Tab 按钮无 44px | :122 | 无 min-height | `[&>button]:min-h-[44px] md:[&>button]:min-h-0` |
| Definition list | :161 | `grid-cols-[8rem_1fr]` | `grid-cols-1 md:grid-cols-[8rem_1fr]` 防窄屏溢出 |

---

### 30. WorkerDetail

**Role:** Worker 详情 + tabs。

| Issue | Location | Current | Fix |
|-------|----------|---------|-----|
| Tab 按钮无 44px | :112 | 无 min-height | 同 UserDetail |

---

### 31. Version / NotFound

纯展示页，**无交互元素需修复。** ✓

---

## Cross-Cutting Patterns

### Pattern A: Global Touch Target (index.css)

```css
@media (pointer: coarse) {
  button,
  [role="tab"],
  [role="menuitem"],
  select,
  .touch-target {
    min-height: 44px;
    min-width: 44px;
  }
  input:not([type='checkbox']):not([type='radio']),
  textarea {
    min-height: 44px;
  }
}
```

此规则覆盖 ~60% 的触控目标问题，剩余需个别调整。

### Pattern B: Mobile Card View (参考 MembersAgents)

```
desktop: <table class="hidden md:table">...</table>
mobile:  <ul class="md:hidden space-y-2">
           <li class="min-h-[44px] rounded border ...">card</li>
         </ul>
```

需要此模式的页面：Agents, Secrets, ProjectDetail (transfers), Channels (archive list)。

### Pattern C: Merged Context Bar (TaskDetail/IssueDetail mobile)

```
┌─────────────────────────────────────────────┐
│ [ID] [BADGE] [status] dur [avatar] assignee │
│                          Actions ▾   [⤢]    │
└─────────────────────────────────────────────┘
```

合并 MobileWorkItemBar + Conversation Banner，省 ~110px。

### Pattern D: Loading State (参考 PlanDetail)

```tsx
// Bad:  <section>Loading task…</section>
// Good: <section><Skeleton width="16rem" height="1.75rem" /><Skeleton height="8rem" /></section>
```

需要升级的页面：TaskDetail, IssueDetail, ChannelDetail, DMDetail。

---

## Implementation Roadmap

| Sprint | Scope | Pages | Files |
|--------|-------|-------|-------|
| **S1** | Global touch target CSS + AppLayout shell | AppLayout | 2 |
| **S1** | TaskDetail/IssueDetail merged bar + 标题压缩 | TaskDetail, IssueDetail, MobileWorkItemBar, WorkItemConversation | 4 |
| **S1** | Sidebar 对齐 (DT-1 方案 A) | EmbeddedConversationSidebar, WorkItemConversation | 2 |
| **S1** | Composer 44px + Mobile tab 44px | MessageComposer, ConversationMobileTabs | 2 |
| **S2** | Agents 移动端卡片视图 | Agents | 1 |
| **S2** | Secrets 移动端卡片视图 | Secrets | 1 |
| **S2** | PlanDetail tab/toggle/actions 44px + header compact | PlanDetail | 1 |
| **S2** | Loading skeleton 统一 | TaskDetail, IssueDetail, ChannelDetail, DMDetail | 4 |
| **S2** | Thread sidebar 移动端全屏 | ThreadSidebar | 1 |
| **S3** | Auth 页 input/button 44px | Signin, Signup, MemberNew, InvitationAccept | 4 |
| **S3** | Settings 页按钮 44px | OrganizationSettings, Me, Environment | 3 |
| **S3** | List 页 filter/action 44px | DMs, Channels, Reminders, Unread, OrgPlans, ProjectPlans | 6 |
| **S3** | Detail 页 tab 44px + layout | UserDetail, WorkerDetail, ProjectDetail | 3 |
| **S3** | Error states role="alert" | 全部 detail 页 | 6 |
