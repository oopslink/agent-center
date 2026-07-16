# 移动端跨项目汇总页面设计

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-07-16 |
| Scope | Workspace 模块的跨项目视图：OrgWorkItems（`/issues` `/tasks`，同一组件两种 kind）/ OrgPlans（`/plans`）/ OrgRepos（`/repos`，工作区仓库注册表）/ OrgTemplates（`/templates`，纯文本 prompt 模板） |
| Depends on | [mobile-redesign-workspace-core.md](mobile-redesign-workspace-core.md)（复用其 tabstrip / wi-row / 只读摘要弹层等视觉语言） |
| Mockup | [assets/mobile-redesign-org-summary-mockup.html](../assets/mobile-redesign-org-summary-mockup.html) |

## 1. 背景

第五批交付物，仍在底部 Tab「Work」下，但是与第三批（ProjectDetail 内的项目内视图）平级的**跨项目**视图——PC 端顶层路由 `/issues` `/tasks` `/plans` `/repos` `/templates`，对应 col① rail 的 `pathPrefixes`。

审计澄清了两组容易混淆的概念，写这批 spec 前必须先分清楚：

1. **OrgRepos ≠ ProjectDetail 的 Repos tab**。OrgRepos 是工作区级别的仓库**注册表**（创建/编辑/删除仓库元数据：label/provider/url/默认分支/凭证），外加只读的远端查看（Commits/Branches，直接从 git host 拉取，不做本地 clone）。第三批的 ProjectDetail Repos tab 则是"项目引用了注册表里的哪些仓库"（引用/取消引用/设主仓库）。两者是不同的读写面，不要合并成一个页面。
2. **OrgTemplates ≠ Team Templates**。OrgTemplates 是纯文本 `{name, description, content}` 的 prompt 模板 CRUD，**没有"应用/实例化"动作**。仓库里另有一个不相关的 Team Templates 功能（`/teams/templates`，团队组成模板，可导入/导出/实例化）不在本批范围内，也不应被混进本批设计。

## 2. 页面清单与信息架构

| 视图 | PC 路由 | 类型 | 说明 |
|---|---|---|---|
| OrgWorkItems（Issues） | `/issues` | 跨项目列表 | |
| OrgWorkItems（Tasks） | `/tasks` | 跨项目列表 | 同一组件，`kind` 不同 |
| OrgPlans | `/plans` | 跨项目列表 | 只读投影，不能在此建 Plan |
| OrgRepos | `/repos` | 注册表 | 增删改 + 只读远端查看 |
| OrgTemplates | `/templates` | 注册表 | 纯文本模板 CRUD |

Workspace 顶层 `tabstrip` 从第三批的"Projects"单项扩展为 **Projects / Issues / Tasks / Plans / Repos / Templates** 六项，横向可滑动——对应 PC 端 col① rail 的完整 `pathPrefixes` 集合，与第三批 ProjectDetail 内部的 tabstrip 是同一视觉语言但不同语义层级（这里切的是"跨项目视图"，那里切的是"单项目内的维度"）。

## 3. 视觉设计

### 3.1 OrgWorkItems（Issues/Tasks 跨项目列表）

- **项目筛选器**（"全部项目"下拉）是这批独有的——第三批项目内视图的项目维度已固定，没有这个筛选器；这是与第三批唯一的结构性差异，需要单独确认不要在复用组件时误加或误删。
- 行内加了 `proj-tag`（项目名标签），因为跨项目列表里每一行必须能看出属于哪个项目——这是项目内列表不需要的字段。
- **点行 = 只读摘要底部弹层 + "打开完整详情"跳转按钮**，不能就地编辑状态/负责人。这是刻意保留的 PC 端设计（列表本身是只读投影），移动端不要顺手加编辑入口。
- 跨项目创建（顶栏 + 号）先委托一个"选项目"步骤，再落到项目内已有的创建弹层——移动端保留这个两步流程，不假设用户已经在某个项目上下文里。

### 3.2 OrgPlans

- 顶部搜索框（服务端按名称搜索）+ 状态筛选 chip（draft/running/done，archived 默认不显示，需要显式切换才能看到）+ 项目筛选下拉。
- 卡片用 `progress-mini` 迷你进度条替代桌面表格的"进度"列，一眼看出节点完成度。
- **点卡片 = 只读摘要弹层 + "打开 Plan"跳转**（进入第三批已定案的 PlanDetail 竖向 stepper 页面）。
- **没有"新建 Plan"入口**——PC 端本来就是这样，Plan 只能在 Work Board 创建，这里纯粹是查看+筛选+跳转。

### 3.3 OrgRepos

- 卡片：provider 徽章 + label + 描述 + 默认分支 + "被 N 个项目引用"（删除前的软警示信号）。
- 每张卡片可展开"查看远端"（可多张同时展开）：Commits/Branches 两个子 tab。Commits 按日期分组，每条含作者、相对时间、短 SHA（复制按钮）、以及尽力构造的"在代码托管平台中查看"外链；Branches 是分支 chip 列表，默认分支高亮。
- 编辑/删除走行内"⋯"菜单（未在 mockup 逐一画出交互态，参照上一批"⋯"底部弹层的既有模式）。

### 3.4 OrgTemplates

- 卡片：名称 + 描述 + 内置徽章（仅内置模板显示，灰色，不用禁用态图标制造额外视觉噪音）。
- 内置模板**隐藏**编辑/删除入口，只保留"查看"；自定义模板三个操作都在。
- "查看"展开一个 `code-block`（等宽字体、保留换行）显示模板正文——列表 DTO 本身不含 `content`，展开时才异步拉取完整内容，与 PC 端行为一致。

## 4. 功能覆盖清单

| 功能 | PC 端来源 | 移动端处理 |
|---|---|---|
| 跨项目 Issues/Tasks 列表（状态/项目/负责人/日期范围筛选、排序、分页） | `OrgWorkItems.tsx` + `WorkItemFilterBar`（含项目选择器） | Covered（筛选入口+计数）/ Deferred（筛选弹层完整内容，含新增的项目筛选项） |
| 跨项目创建（先选项目再委托项目内创建弹层） | `OrgWorkItemCreateModal` | Deferred — 入口已标，二级"选项目"步骤留后续 |
| 点行只读摘要 + 跳转详情 | 桌面 col④ 面板的移动等价 | Covered |
| OrgPlans 搜索 + 状态筛选（含默认隐藏 archived）+ 项目筛选 + 排序分页 | `OrgPlans.tsx` | Covered |
| OrgPlans 无新建入口（只能在 Work Board 建） | 现状确认 | N/A — 确认后移动端不额外加"新建 Plan"按钮 |
| OrgPlans 点卡片摘要 + "打开 Plan" | 同上 | Covered |
| OrgRepos 仓库注册表 CRUD（label/provider/url/默认分支/凭证） | `RepoFormModal` | Deferred — 入口已标，表单细节留后续；凭证字段"留空=保留原值"的约束需写进实现阶段 |
| OrgRepos 删除时按引用计数分级警示 | `ConfirmModal` 差异化文案 | Deferred — 需要在实现阶段体现"有引用"警示，不能所有删除都用同一句文案 |
| OrgRepos 只读远端查看（Commits 按天分组/复制 SHA/外链、Branches 列表） | `RemoteViewerPanel` | Covered |
| OrgRepos 远端不可用时的降级态 | 现状确认 | Covered（要求："不可用"而非硬报错） |
| OrgRepos 深链自动展开定位（`/repos?repo=`） | 现状确认 | Deferred — 确认保留跳转定位能力，具体动效留实现阶段 |
| OrgTemplates 模板 CRUD（name/description/content） | `TemplateFormModal` | Deferred — 入口已标，表单细节留后续；编辑时 content 异步加载完成前禁止保存的约束需写进实现阶段 |
| OrgTemplates 内置模板隐藏编辑/删除 | 现状确认 | Covered |
| OrgTemplates 无"应用/实例化"动作 | 现状确认（区别于不相关的 Team Templates） | N/A — 确认后移动端不额外发明"应用"按钮 |

## 5. 与第三批（Workspace 核心）的关系

- OrgWorkItems/OrgPlans 的"点行→只读摘要弹层→跳转详情"模式，跳转目标就是第三批已定案的 IssueDetail/TaskDetail/PlanDetail 页面，不重新设计一套详情视觉。
- OrgRepos 的"查看远端"面板与第三批 ProjectDetail 的 Repos tab（仓库引用列表）是两个独立入口，二者可以互相链接（"查看引用它的项目"之类的跳转留实现阶段决定是否需要），但视觉和数据源不合并。

## 6. Out of Scope（本文档不覆盖）

- OrgWorkItems 跨项目筛选弹层、创建流程"选项目"步骤的具体表单。
- OrgRepos 的 Add/Edit Repo 表单细节，删除确认的具体文案分级。
- OrgTemplates 的 Add/Edit 表单细节。
- Team Templates（`/teams/templates`）——与本批的 OrgTemplates 是完全不同的功能，不在本次调研范围内，是否需要单独立项留待后续决定。

## 7. 未来扩展

- OrgRepos 与 ProjectDetail Repos tab 之间是否需要更紧密的跳转联动（例如从仓库卡片直接跳到引用它的某个项目），本批未设计，留待实际使用反馈后再定。
