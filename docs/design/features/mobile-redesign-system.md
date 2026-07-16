# 移动端 System 模块设计

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-07-16 |
| Scope | System 模块：Environment（Fleet 总览）/ WorkerDetail（Profile/Agents/Management/Activity 4 个 tab）/ Secrets / Reminders |
| Depends on | [mobile-redesign-nav-framework.md](mobile-redesign-nav-framework.md)、[mobile-redesign-conversations.md](mobile-redesign-conversations.md)（Reminder 详情内容复用消息 markdown+linkify 渲染管线） |
| Mockup | [assets/mobile-redesign-system-mockup.html](../assets/mobile-redesign-system-mockup.html) |

## 1. 背景

第六批交付物，覆盖底部 Tab「Sys」（Reminders 同时也有自己的独立底部 Tab「Remind」，两者是 PC 端 col① rail 下的并列项，不是父子关系）。

审计确认了本模块几个安全/约束性质的设计决策，必须原样保留：

1. **Worker Force Delete 是全系统最强的安全确认流程**（GitHub 风格输入名称二次确认），比普通的 Remove Worker 更强一级。
2. **Secrets 遵循 ADR-0026 §5**：创建后永不提供查看/编辑，只能撤销后重新创建；提交成功后明文值立即从前端状态清除。
3. **Reminder 的 Cancel 操作在 PC 端目前没有二次确认**（与同样是终态操作的 Delete 不一致）——这是现状而非本设计的推荐做法，移动端是否补上确认留待决策（见 §4）。

## 2. 页面清单与信息架构

| 视图 | PC 路由 | 类型 |
|---|---|---|
| Environment | `/environment` | Fleet 总览列表 |
| WorkerDetail | `/workers/:id` | 详情，4 个 tab |
| Secrets | `/secrets` | 列表 |
| Reminders | `/reminders` | 列表 |

System 顶层 `tabstrip`：Environment / Secrets / Reminders / Settings 四项——Reminders 在这里保留一个平级入口，即便它也有独立的底部 Tab，因为 PC 端两者本来就是同一 col① rail 下的并列项，不强行只留一个入口。

## 3. 视觉设计

### 3.1 Environment

- 顶部 4 格统计磁贴（在线数/Agent 运行中/任务/待处理）。
- Worker 卡片：状态点+文案（非纯颜色）、活跃任务数、心跳时间、CLI 探测徽章（区分"探测到"与"可执行"——只有 claude-code 真正可执行，其它 CLI 目前只是发现态，这个语义差异要保留，不能都渲染成同一个"已启用"绿标）、绑定 Agent 列表（超过 3 个自动折叠）。
- **离线 Worker** 显示专门的安装引导卡片：说明文案 + "显示安装口令"入口 + "重新生成"提示。**手机上复制一段命令去粘贴到目标机器不现实**，需要专门设计交付方式（见 §4，本批未定案）。
- Activity 四 tab 汇总流（All/Work Items/Issues/Transfers）沿用 PC 端已有的"移动端卡片/桌面表格"双布局模式——这是 Transfers 面板已经验证过的既有响应式模式，本批直接采纳，不重新发明。

### 3.2 WorkerDetail

4 个 tab：Profile / Agents / Management / Activity（Activity 目前 PC 端也只是占位 stub，移动端同步留空）。

- **Profile**：worker_id、注册时间、心跳 + Worker 自报字段（主机名/OS/架构/构建版本），每个字段独立处理"尚未上报"占位，不是整页统一显示"加载中"。
- **Management**（本模块风险最高的 tab）：
  - 重命名（内联编辑，仅此一处入口，Environment 列表不提供内联改名，与 PC 端"单一收敛点"的既有决策一致）。
  - 安装口令显示/重新生成（仅离线 Worker 可用，在线时置灰说明原因）。
  - **移除 Worker**：danger 按钮，确认文案动态附加"N 个 Agent 绑定将变为不可用"的警告（仅在有绑定时出现）。
  - **强制删除**：**完整保留输入 Worker 名称才能启用删除按钮**的二次确认机制——这是全模块最强的安全设计，触屏误触概率比鼠标点击更高，反而更需要这层保护，绝不能因为"移动端要快"而降级成普通 Yes/No。

### 3.3 Secrets

- 列表卡片补全现有移动卡片缺失的字段：kind、state（Active/Revoked）、创建时间——PC 端桌面表格有这些，移动卡片之前只显示名称，本批找回。
- 页面顶部保留"不可查看/编辑"的免责声明文案。
- **创建流程严格保留"永不明文回显"约束**：值输入框在展示态用圆点遮罩，提交成功后立即清空、只显示名称确认创建成功——mockup 里特意写了一句说明，防止实现阶段手滑加一个"眼睛图标"显式违反 ADR-0026。
- Revoke 是唯一的现存 secret 操作，走标准 danger ConfirmModal（非输入名称级别，与 Force Delete 强度不同——因为 Revoke 虽终态但不会立即产生级联影响，比删除 Worker 风险低一级）。

### 3.4 Reminders

- 顶部 3 格统计（Active/Paused/下次触发时间）。
- 状态筛选 chip（默认/全部/Active/Paused/已完成）+ 范围筛选（我创建的/我是提醒对象/全部）——PC 端的范围筛选和搜索原本由页面外的侧边栏驱动，该侧边栏在移动端本来就折叠不可达，本批用页内筛选 chip 补上这个入口，不是新增功能而是补齐可达性。
- 卡片行操作（Pause/Resume/Edit/Clone/Delete，按当前状态条件显示，与 PC 端逻辑一致）。
- **触发类型三选一**（Cron 周期 / 单次 / 事件触发）用顶部分段控件承接：
  - Cron：表达式输入 + 预设 chip（每天9点/每小时等）+ 人类可读预览（本地时区）。
  - 单次：日期时间选择器。
  - 事件触发：实体类型（plan/task/issue）+ 实体搜索选择 + 事件名（词表随实体类型变化）+ 可选延迟。
  - "跳过重叠触发"和"结束条件"开关明确标注**仅创建时可设，创建后不可再改**——防止实现阶段误做成随时可编辑。
- Reminder 详情页内容区**保留任务/计划/issue 引用的可点击富文本链接**（复用 Conversations 模块已定案的消息 markdown+linkify 渲染管线，不重新设计一套），并展示发送历史（送达/跳过重叠/失败三态徽章）。

## 4. 功能覆盖清单

| 功能 | PC 端来源 | 移动端处理 |
|---|---|---|
| Environment 统计磁贴 + Worker 卡片（状态/CLI/绑定Agent） | `Environment.tsx` | Covered |
| Worker 安装引导（离线态、显示/重新生成口令） | `AddWorkerModal`/`InstallCommandModal` | Covered（入口）/ Deferred（手机端复制/分享口令的具体交付方式——二维码/系统分享面板/发送到桌面端，需要专门设计） |
| Environment Activity 四 tab 汇总流 | `Environment.tsx` Activity 区块 | Deferred — 沿用已有的卡片/表格双布局模式，具体逐屏未画 |
| WorkerDetail Profile（含逐字段"未上报"占位） | `WorkerProfile` | Covered |
| WorkerDetail Agents tab + 逐行 Restart | `BoundAgents` | Deferred — 入口留后续 |
| WorkerDetail 重命名（唯一入口） | `WorkerManagement` | Covered |
| WorkerDetail 移除 Worker（动态警告文案） | 同上 | Covered |
| WorkerDetail 强制删除（输入名称二次确认） | `ForceDeleteModal` | Covered — 强度不降级 |
| Secrets 列表字段补全（kind/state/创建时间） | `Secrets.tsx` 桌面表格 | Covered |
| Secrets 创建（永不明文回显、无查看/编辑入口） | `SecretCreateModal` + ADR-0026 §5 | Covered |
| Secrets 撤销（ConfirmModal） | 同上 | Covered |
| Reminders 状态/范围筛选 + 搜索 | `Reminders.tsx` + 页外侧边栏 | Covered（状态+范围筛选 chip）/ Deferred（搜索框交互细节） |
| Reminders 行操作（Pause/Resume/Edit/Clone/Delete） | 同上 | Covered |
| **Reminder Cancel 无二次确认**（与 Delete 不一致） | 现状确认 | Deferred — 触屏误触率更高，是否给 Cancel 补轻量确认/撤销条需要显式决策，不能照搬 PC 端"无确认"直接搬到移动端 |
| Reminder 创建/编辑（三种触发类型 + Advanced 仅创建可设） | `ReminderCreateModal` | Covered |
| Reminder 详情（富文本链接内容 + 发送历史） | `ReminderDetailModal` | Covered |
| AddWorkerModal 表单 | 同上 | Deferred — 入口已标，表单细节留后续 |

## 5. 与其它批次的关系

- Reminder 详情内容区复用 Conversations 模块（第二批）已定案的消息 markdown + @mention/引用 linkify 渲染规则，不重新设计。
- Worker 绑定 Agent 行、点击跳转 AgentDetail，复用 Members 模块（第四批）已定案的交互。

## 6. Out of Scope（本文档不覆盖）

- AddWorkerModal、SecretCreateModal、ReminderCreateModal 的具体表单字段布局细节（只定了信息结构，未画完整表单交互）。
- Environment Activity 四 tab 汇总流的逐屏设计。
- WorkerDetail Agents tab 的逐行操作细节。

## 7. 未来扩展

- Worker 安装口令在移动端的交付方式（二维码/系统分享/发送到桌面端）需要单独立项设计，本批只确认了"现状不可行，需要专门方案"这个结论。
- Reminder Cancel 是否需要补二次确认或撤销条，需要在实现阶段前明确决策。
