# UI/UX Review Report — agent-center

> Date: 2026-06-26
> Scope: `web/src/` (React SPA) + `sites/` (static pages)
> Target: PC + Mobile 双端 Web 浏览器
> Stack: React 19 + Tailwind 3 + Headless UI + TanStack Query

---

## Executive Summary

审查覆盖 ~80 个 React 组件、12 个 Modal、6 个 Page 和 8 个静态站点页面。项目已建立良好的基础设施：

**做得好的方面：**
- 完整的 CSS 变量设计 token 体系（light/dark 双模式 ~120 个 token）
- a11y 测试守卫（禁止 `focus:outline-none`、raw `text-red-*`、emoji icons）
- 全局 `*:focus-visible` ring + `prefers-reduced-motion` 尊重
- 代码高亮 WCAG AA 对比度验证（每个 token 标注 ratio）
- iOS Safari auto-zoom 修复（移动端 input 16px floor）
- FOUC-free 暗色主题切换（main.tsx 预加载）

**需要改进的方面：**
- 移动端触控目标普遍偏小（大量 < 44px）
- Modal 表单 a11y 缺陷（label 关联、focus trap、aria-labelledby）
- 静态站点可访问性基础薄弱
- 响应式断点在复杂布局（agents 表格、heatmap）缺少移动替代方案

---

## Issue Categories

### P0 — CRITICAL (Must Fix)

| # | Issue | Files | Impact |
|---|-------|-------|--------|
| 1 | **20+ 表单 input 缺少 label/htmlFor 关联** | AgentCreateModal, ChannelCreateModal, IssueCreateModal, BoardTaskCreateModal, SecretCreateModal, DMStartModal, MemberInviteModal, AgentConfigEditModal | Screen reader 用户无法识别输入框用途，违反 WCAG 2.1 §1.3.1 |
| 2 | **MemberInviteModal 缺少 focus trap** | MemberInviteModal.tsx:89-97 | 唯一未调用 `useModalA11y` 的 modal；键盘焦点可逃逸到背景，违反 WCAG 2.1 Level A |
| 3 | **4 个 Modal 缺少 aria-labelledby** | AgentCreateModal:67, IssueCreateModal:46, BoardTaskCreateModal:81, AddWorkerModal:102 | Dialog 有 `role="dialog"` 但未关联标题，screen reader 无法宣告弹窗名称 |
| 4 | **移动端触控目标普遍 < 44x44px** | 跨多个文件，详见下表 | 违反 WCAG 2.5.5 + Apple/Google HIG 最低 44pt 要求 |

#### P0-4 触控目标详细清单

| Component | Element | Current Size | File:Line |
|-----------|---------|-------------|-----------|
| AppLayout | Sidebar subitem toggle (chevron) | `p-1` ≈ 16px | AppLayout.tsx:507 |
| AppLayout | DM delete button | `h-6 w-6` = 24px | AppLayout.tsx:549 |
| AppLayout | Sign out button | `py-1.5 + h-4 w-4` ≈ 28px | AppLayout.tsx:1178 |
| AppLayout | Org settings gear | `h-7 w-7` = 28px | AppLayout.tsx:848 |
| ConversationSidebar | Tab buttons | `py-1.5` ≈ 28-32px | ConversationSidebar.tsx:93 |
| ConversationSidebar | Collapse button | `h-7 w-7` = 28px | ConversationSidebar.tsx:115 |
| CommandPalette | List items | `py-2` ≈ 32px | CommandPalette.tsx:157 |
| Projects | Quick-action icon buttons | `h-7 w-7` = 28px | Projects.tsx:273,284 |
| Projects | Kebab menu button | `h-7 w-7` = 28px | Projects.tsx:298 |
| All Modals | Close "X" button | `py-1.5` ≈ 24-28px | 6 files |
| MemberInviteModal | Cancel/Confirm buttons | `py-1 text-xs` ≈ 28px | MemberInviteModal.tsx:181,189 |
| IssueEditModal | Tag remove "x" | bare text, no padding | IssueEditModal.tsx:214-222 |
| EntityMultiSelect | Chip remove "x" | no size constraint | EntityMultiSelect.tsx:142 |

---

### P1 — HIGH (Should Fix)

| # | Issue | Files | Detail |
|---|-------|-------|--------|
| 5 | **Agents 表格缺少移动端替代方案** | Agents.tsx:175-338 | `min-w-[60rem]` 横向滚动表格在移动端体验极差；应提供卡片视图（MembersAgents 已做，可参考） |
| 6 | **Error messages 缺少 aria-describedby** | 8+ modal 文件 | 错误提示显示在表单下方但未通过 `aria-describedby` 关联到对应 input，screen reader 不宣告 |
| 7 | **Required 字段指示不一致** | ChannelCreateModal, SecretCreateModal, DMStartModal | 部分 modal 有红色星号标注必填，部分完全无标注 |
| 8 | **ConversationView loading 状态为纯文本** | ConversationView.tsx:59-62 | "Loading messages..." 无骨架屏、无 `role="status"`；错误状态也缺少 `role="alert"` |
| 9 | **静态站点缺少 skip-to-content link** | 所有 sites/ HTML 页面 | 键盘用户需 Tab 多次才能到达主内容区 |
| 10 | **静态站点 `--generic` 色彩对比度不足** | redesign.css:89 | `.chip.generic` 紫色 `#9333ea` on `#f3e8ff` ≈ 3.2:1，不满足 WCAG AA 4.5:1 |
| 11 | **静态站点交互元素缺少 :focus-visible** | redesign.css:74, site.css:171 | Nav links、buttons 无键盘焦点指示器 |
| 12 | **Roadmap 页 JS-only 渲染无降级** | roadmap/index.html:90-154 | 所有 epic/feature 内容由 JS 生成；JS 失败则页面完全空白 |
| 13 | **Table 缺少 `<caption>` 和 `aria-label`** | Agents.tsx:177, AgentTasks.tsx:147, MembersAgents.tsx:143 | 3 个表格无语义标题描述 |

---

### P2 — MEDIUM (Should Plan)

| # | Issue | Detail |
|---|-------|--------|
| 14 | **AppLayout z-index 无统一规范** | z-30 (mobile header), z-40 (drawer), z-50 (OrgDropdown + CreateOrgModal) — 多组件共用 z-50 可能冲突 |
| 15 | **Sidebar truncated labels 缺少 title 属性** | ConversationSidebar.tsx:97 — 截断文本 screen reader 不宣告完整内容 |
| 16 | **ConversationSidebar embedded 模式无移动端响应** | ConversationSidebar.tsx:230 — `w-72` 固定宽度在移动端占满全屏 |
| 17 | **AgentHeatmap 无键盘导航** | AgentHeatmap.tsx:236 — `role="grid"` 但无 arrow key 导航、无 loading 骨架 |
| 18 | **ConfirmModal 无 Enter 键提交** | ConfirmModal.tsx:60-81 — 纯 `onClick` button，非 `<form>` 包裹 |
| 19 | **Disabled 按钮样式不统一** | ConfirmModal `disabled:opacity-50` vs 其他 `disabled:cursor-not-allowed disabled:bg-bg-subtle` |
| 20 | **Close "X" 应为 SVG icon** | 6 个 modal 使用文本 "X" 而非 SVG icon，与 icon-only 最佳实践不符 |
| 21 | **静态站点 font-size < 16px** | roadmap:39 (`11.5px`), dev docs:62 (`11px`), manual:60 (`12px`) — 移动端可读性差 |
| 22 | **静态站点 max-width 1180px 行字数过多** | redesign.css:48 — 正文行超 90 字符，超出 65-75 字符最佳范围 |
| 23 | **静态站点缺少 Open Graph meta tags** | 所有 sites/ 页面无 og:title/og:description，社交分享无预览 |
| 24 | **EntitySelect/EntityMultiSelect search input 缺少 label** | EntitySelect.tsx:169, EntityMultiSelect.tsx:183 — 仅有 placeholder，无 `<label>` 或 `aria-label` |

---

### P3 — LOW (Nice to Have)

| # | Issue | Detail |
|---|-------|-------|
| 25 | Icon sizing 不一致 — `h-3.5` vs `h-4` | AppLayout.tsx:1108 OrgIcon |
| 26 | 过渡动画 duration 未显式指定（依赖 Tailwind 默认） | 多文件 `motion-safe:transition-colors` |
| 27 | `heading` fontFamily 与 `sans` 完全相同（冗余定义） | tailwind.config.js:136-151 |
| 28 | Manual 页 search input 无 `<label>` | manual/v2.15.0/index.html:107 |
| 29 | 静态站点 hero h1 `line-height: 1.16` 过紧 | site.css:216 — 建议 1.3+ |
| 30 | MemberInviteModal backdrop `bg-black/40` 与其他 modal `bg-black/50` 不一致 | MemberInviteModal.tsx:99 |

---

## Mobile Chat Experience — Deep Dive

> 用户特别关注移动端 Chat 体验，以下为针对 Chat 流程的专项审查。

### 做得好的方面

1. **ConversationMobileTabs (T184)** — 移动端 `< 768px` 将桌面的 col3+col4 布局折叠为 `chat / participants / threads / files` tab bar，正确的响应式策略。Chat tab 保持挂载（SSE/scroll/draft 跨 tab 切换保持），participants/threads/files 懒加载。
2. **气泡宽度自适应** — `max-w-[86%] md:max-w-[75%]` (MessageList.tsx:230-232)，移动端 86% 宽度比桌面 75% 更宽，适合窄屏阅读。代码块 bubble 全宽 + 内部横滚，不会撑破页面。
3. **IME 合成保护** — `composingRef` + `isComposing` (MessageComposer.tsx:59-60,242) 正确处理中文/日文输入，Enter 确认候选字而非发送。
4. **overflow-wrap: anywhere** — `.markdown-body` (index.css:312-315) + `overflow-x-hidden` (MessageList.tsx:481) 双重保护，长 URL / 中英混排不会造成横向页面滚动。
5. **maximize 模式** — WorkItemConversation.tsx:47 在 task/issue 嵌入场景提供全屏聊天覆盖，解决了移动端嵌入 chat 区域过小的问题。
6. **"New messages" pill** — MessageList.tsx:503-511 滚动查历史时出现新消息指示器，点击跳回底部。
7. **Auto-grow textarea** — MessageComposer.tsx:85-102 输入框自增长到 4 行后内部滚动，不会无限撑高。
8. **Drag & paste 附件** — 支持拖拽/粘贴文件，带上传进度条 + retry，体验完整。

### 需要改进的问题

#### MC-1: Composer 发送/附件按钮触控目标偏小 [P1-Mobile]

**File:** MessageComposer.tsx:447,465-468

Send 和 Attach 按钮均为 `h-8 w-8` = 32px，低于移动端 44px 最低标准。代码注释明确提到 "supersedes the M2 44px touch sizing for these two icon controls" (T148 owner-directed)，说明是有意为之——但移动端用户拇指操作时 32px 仍然偏小，尤其 Send 是最高频操作。

**建议:** 在 `@media (pointer: coarse)` 下将 Send/Attach 提升至 `h-11 w-11` (44px)，仅桌面保持紧凑。

#### MC-2: ThreadSidebar 在移动端为 fixed overlay，无宽度限制下限 [P2-Mobile]

**File:** ThreadSidebar.tsx:89-96

ThreadSidebar 使用 `fixed inset-y-0 right-0` + `max-w-[75vw]`。在 375px 屏幕上 `THREAD_MIN_WIDTH = 320px` > 75vw (281px)，`min-width` 与 `max-width` 矛盾：
- `style={{ width: 448 }}` (default) 受 `max-w-[75vw]` 限制到 ~281px
- 但 ResizeHandle 的 `minWidth: 320` 可能让用户拖到超出视口

**建议:** 移动端（`pointer: coarse` 或 `< md`）应全屏展示 thread sidebar (`inset-0`)，不使用 ResizeHandle。

#### MC-3: ConversationView loading/error 无语义角色 [P1]

**File:** ConversationView.tsx:59-67

Loading 用 `<p>` + 纯文本 "Loading messages..."，error 同样。移动端小屏这两个状态视觉占比更大。

**缺失：**
- `role="status"` on loading → screen reader 不宣告加载状态
- `role="alert"` on error → 错误不自动宣告
- 无骨架屏 → 纯文本在移动端显得简陋

#### MC-4: Mobile tab bar 触控目标偏小 [P1-Mobile]

**File:** ConversationMobileTabs.tsx:72-75

Tab 按钮 `px-2.5 py-1.5 text-xs` — 垂直 padding 6px + 12px 字 ≈ 24px 高度，远低于 44px。这是移动端最频繁的导航操作（在 chat/threads/files 间切换）。

**建议:** `py-2.5` 或 `min-h-[44px]` 确保触控友好。

#### MC-5: ThreadButton reply 区域偏小 [P2-Mobile]

**File:** ThreadButton.tsx:32

`px-1.5 py-0.5 text-xs` 使整个按钮高度约 20-24px。移动端用户在消息流中精确点击 thread 回复按钮困难。

**建议:** 移动端增加 padding 或增大点击热区（可用 `::after` pseudo-element 扩展）。

#### MC-6: MessageCopyButton 在移动端的可发现性 [P3]

**File:** MessageList.tsx:310

Copy 按钮在 header line 中，移动端小屏可能与 sender name / time 挤在一行。无 hover 提示（移动端无 hover），依赖 icon 可辨识性。

#### MC-7: Attachment chip 在移动端偏窄 [P2-Mobile]

**File:** MessageComposer.tsx:374,378

附件列表 `max-w-xs` (320px) + 每个 chip `w-44` (176px)。在 375px 屏幕上 `max-w-xs` = 320px 但 composer 本身有 `p-3` (24px 两侧) → 可用宽度 327px，两个 chip 并排 352px 溢出到第二行是正确的，但单个 chip 176px 在 327px 中不居中、预览图 `h-8 w-8` 也偏小。

**建议:** 移动端 chip 改为 `w-full` 单列布局，预览图适当放大。

#### MC-8: System notification 展开/折叠触控 [P3]

**File:** MessageList.tsx:558-591

SystemNotificationRow toggle button 高度来自 `text-[0.625rem]` (10px) + 图标 `h-3 w-3` = 12px + 内部 padding 很小，触控目标约 20px。移动端难以精确点击。

---

## Recommendations — Mobile-First Priority

### Immediate Actions (Sprint-level)

1. **移动端 Chat 触控目标修复（MC-1, MC-4）：**
   - Send/Attach 按钮在 `@media (pointer: coarse)` 下提升至 44px
   - ConversationMobileTabs tab bar 增加 `min-h-[44px]`
   - ThreadButton 扩大点击热区

2. **统一全局触控目标：** 创建 Tailwind utility `min-h-[44px] min-w-[44px]` 或 `touch-target` 组合类，应用于所有交互元素。移动端 (`@media (pointer: coarse)`) 可额外放大至 48px。

3. **修复 Modal a11y：**
   - 为所有 input 添加 `id` + label `htmlFor` 关联
   - MemberInviteModal 补上 `useModalA11y()` 调用
   - 4 个 modal 补上 `aria-labelledby` 指向 h2 title
   - Error messages 用 `aria-describedby` 关联到对应 input

4. **ConversationView loading 语义化（MC-3）：** 添加 `role="status"` / `role="alert"`，可选骨架屏替代纯文本。

### Short-term (1-2 Sprints)

5. **ThreadSidebar 移动端全屏（MC-2）：** `< md` 断点下 thread sidebar 使用 `inset-0` 全屏展示，隐藏 ResizeHandle，避免 min/max width 矛盾。

6. **移动端附件 chip 优化（MC-7）：** 移动端 composer 附件改为单列 `w-full` 布局，预览图适当放大。

7. **Agents 表格移动端替代：** 参考 MembersAgents 的 `hidden md:block` (table) + `md:hidden` (card) 模式，为 Agents 页添加卡片视图。

8. **静态站点 a11y 基础：** 补充 skip-to-content link、`:focus-visible` ring、修复 generic chip 对比度、添加 `aria-current="page"`。

9. **z-index scale 文档化：** 定义 `z-header: 30`, `z-drawer: 40`, `z-dropdown: 50`, `z-modal: 60`, `z-toast: 70`，避免冲突。

### Long-term

10. **axe-core 集成：** 当前 a11y 测试为静态文本扫描；建议在 e2e 中集成 axe-core 做运行时审计。
11. **静态站点 progressive enhancement：** Roadmap 页提供 `<noscript>` 降级内容。

---

## Metrics

| Category | Issues Found | P0 | P1 | P2 | P3 |
|----------|-------------|----|----|----|----|
| Accessibility (a11y) | 14 | 3 | 5 | 4 | 2 |
| Touch Targets / Mobile | 8 | 1 | 1 | 2 | 0 |
| Mobile Chat (MC-*) | 8 | 0 | 3 | 3 | 2 |
| Dark Mode / Tokens | 3 | 0 | 1 | 1 | 1 |
| Responsive Layout | 4 | 0 | 1 | 2 | 0 |
| Loading / Empty States | 3 | 0 | 1 | 1 | 0 |
| Static Sites | 8 | 0 | 4 | 3 | 2 |
| Visual Consistency | 4 | 0 | 0 | 2 | 2 |
| **Total** | **38** | **4** | **12** | **14** | **6** |
