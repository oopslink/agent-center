# 移动端 Settings 模块设计

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-07-16 |
| Scope | Settings 模块：Settings（`/settings`，系统级配置）/ OrganizationSettings 5 个子 section（profile / humans / agents / invitations / danger） |
| Depends on | [mobile-redesign-nav-framework.md](mobile-redesign-nav-framework.md)、[mobile-redesign-members.md](mobile-redesign-members.md)（agents section 原样复用 Agents.tsx，见 §3.4）、[mobile-redesign-system.md](mobile-redesign-system.md)（Force Delete 输入名称确认的强度基准） |
| Mockup | [assets/mobile-redesign-settings-mockup.html](../assets/mobile-redesign-settings-mockup.html) |

## 1. 背景

第七批（最后一批）交付物。审计澄清了两个容易假设错的点：

1. **`Settings.tsx` 不是用户账号设置**，而是系统级配置页（界面语言 + 唤醒护栏 5 项数值参数），归属 System 模块 col① rail，不要按"我的账号"去设计。
2. **OrganizationSettings 的 "humans" section 不是 `MembersHumans.tsx` 的复用**，而是一套独立、更简化的纯表格实现（无行内菜单/popover）——第四批 Members 模块的移动端工作**不覆盖**这里，需要单独设计。相对地，**"agents" section 是 `Agents.tsx` 的原样复用**（`{section === 'agents' && <Agents />}`），第四批已经覆盖，本批不重复设计。

Danger section（禁用/删除组织）是全系统影响面最大的操作入口，审计发现 PC 端当前的删除确认强度偏弱（单击 ConfirmModal，无输入名称二次确认），本批**没有照抄这个现状**，而是主动把移动端设计提升到与第六批 Worker Force Delete 同等的强度——这是本项目所有批次里唯一一处"独立设计主动纠正 PC 端不足"而非"复刻 PC 端行为"的地方，理由和依据写在 §3.5。

## 2. 页面清单与信息架构

| 视图 | PC 路由 | 类型 |
|---|---|---|
| Settings | `/settings` | 系统级配置表单 |
| OrgSettings · Profile | `/organization-settings/profile` | 表单 |
| OrgSettings · Humans | `/organization-settings/humans` | 列表（独立简化实现） |
| OrgSettings · Agents | `/organization-settings/agents` | 原样复用 Agents 列表 |
| OrgSettings · Invitations | `/organization-settings/invitations` | 列表 |
| OrgSettings · Danger | `/organization-settings/danger` | 危险操作 |

顶部 `tabstrip`：Profile / Humans / Agents / Invitations / Danger 五项横向可滑动，Danger 用红色文字区分视觉权重，提醒用户这是不同性质的一类操作。

## 3. 视觉设计

### 3.1 Settings（系统级配置）

- 界面语言：EN/中文分段切换，即时生效，无需保存按钮。
- 唤醒护栏：5 个数值字段（最大递归深度/循环检测窗口/循环触发阈值/每分钟速率上限/链路 token 预算），逐字段校验（必须 > 0），统一"保存"按钮（校验未通过或请求中禁用）。

### 3.2 OrgSettings · Profile

- 组织名称（可编辑）、Slug/ID（只读展示，不可编辑，与 PC 端一致）、描述（多行文本）。
- Logo 上传保持占位桩状态——**PC 端本身就没有实现这个功能**，移动端不要"顺手"把 PC 端还没做的事情做全，维持同等的未完成状态并明确标注。

### 3.3 OrgSettings · Humans（独立简化实现，非第四批复用）

- 纯卡片列表：头像+姓名、角色选择器（owner/admin/member）、加入时间、Active/Disabled 状态徽章。
- 顶部保留一条说明性 banner，帮助用户理解"这里是组织级角色管理与移除入口，日常查看/私信成员请去 Team 标签下的 Humans 页面"——两个相似页面容易混淆，用文案主动区分，而不是指望用户自己发现差异。
- 行操作：禁用/重新启用（无确认，直接生效，与 PC 端一致）、移除组织（Drop，danger，走确认弹层——本批未画确认弹层本身，强度维持标准 ConfirmModal 级别）。

### 3.4 OrgSettings · Agents（原样复用）

不做独立设计——这个 tab 的内容就是第四批已定案的 Agents 列表页面，逐字复用（批量操作、生命周期控制、创建/删除等全部功能随 Agents 列表一起继承，不需要在本文档重复定义）。

### 3.5 OrgSettings · Invitations

- 顶栏"+"创建邀请 → 底部弹层：用户标识 + 角色选择。
- 邀请卡片：受邀人、状态徽章（Pending/Accepted）、角色+邀请人、过期时间、只读邀请链接 + 复制按钮、已接受时显示接受时间。
- **Cancel（无确认）与 Delete（有确认）保留 PC 端既有的不对称**——Cancel 只是撤回一个待处理邀请（影响小、可重新邀请），Delete 会清除邀请记录本身，风险级别不同，这个区分本身是合理的产品逻辑，不强行拉平成统一confirm。

### 3.6 OrgSettings · Danger（本批唯一主动加强安全设计的地方）

- **禁用组织**：danger 按钮 + 标准确认弹层（可逆操作——禁用后除 Owner 外的成员无法登录，随时可重新启用），强度维持 PC 端水平即可。
- **审计日志**：保留"即将推出"的占位卡片而非直接隐藏——让用户知道这个能力存在但未上线，比完全消失更不容易被误解成"被砍掉的功能"，与 PC 端现状一致（PC 端本来就是永久 disabled 的占位按钮）。
- **彻底删除组织**（`orgApi.delete`）：**主动强化为输入组织名称才能启用删除按钮**的二次确认机制，与第六批 Worker Force Delete 同一强度模板。理由：
  1. 审计发现这是全系统影响面最大的单个操作——删除组织会级联清除项目/会话/成员/密钥/Agent 记录等全部数据，比删除单个 Worker 影响大得多。
  2. PC 端目前只有单击 ConfirmModal，没有输入确认/二次验证——这是 PC 端现状的一个真实弱点，不是"移动端要遵循的标准"。
  3. 触屏误触概率高于鼠标点击，对这个级别的操作而言，"够用的确认强度"本身就该更高，不分平台。
  4. 这不是"给移动端加戏"，而是识别出 PC 端这里的确认强度与其破坏力不匹配，独立设计时一并修正——这个判断和理由需要在评审时明确共识，如果决定这超出了本次移动端重设计的授权范围，也可以作为一条同步反馈给 PC 端的建议，但移动端本身不应该复刻一个已知偏弱的确认机制。

## 4. 功能覆盖清单

| 功能 | PC 端来源 | 移动端处理 |
|---|---|---|
| 语言切换 | `LanguagePanel` | Covered |
| 唤醒护栏 5 字段表单 | `WakeGuardrailPanel` | Covered |
| Org Profile（名称/slug只读/描述/logo占位） | OrgSettings profile | Covered |
| Org Humans 角色管理 + 禁用/启用/移除 | OrgSettings humans（独立实现） | Covered（列表+行操作入口）/ Deferred（移除确认弹层的具体文案） |
| Org Agents = 原样复用 Agents 列表 | OrgSettings agents | N/A — 复用第四批，无需新设计 |
| 邀请创建/复制链接/取消/删除 | OrgSettings invitations | Covered |
| 邀请 Cancel 与 Delete 的确认不对称 | 现状确认 | N/A — 保留合理的既有区分，不拉平 |
| 禁用/启用组织 | OrgSettings danger | Covered（入口）/ Deferred（确认弹层具体文案，强度维持标准级别） |
| 审计日志占位 | 同上 | Covered（占位态） |
| **彻底删除组织** | 同上 | Covered — **强度主动提升到输入组织名称二次确认**，不是简单复刻 PC 端现状 |
| Danger section 的路由级权限门禁 | 组件本身无显式角色判断 | Deferred — 需要在实现阶段确认非 owner 是否应该在客户端就隐藏整个 tab，而不是让用户点进来才被 API 拒绝 |

## 5. 与其它批次的关系

- Agents section 完全复用第四批 [mobile-redesign-members.md](mobile-redesign-members.md) 的 Agents 列表设计。
- 删除组织的确认强度对齐第六批 [mobile-redesign-system.md](mobile-redesign-system.md) 的 Worker Force Delete 模板（输入名称二次确认）。

## 6. Out of Scope（本文档不覆盖）

- 邀请创建弹层、Humans 移除确认弹层、禁用/启用组织确认弹层的完整文案与交互细节。
- Danger section 的客户端权限门禁具体实现方式。

## 7. 未来扩展

- 是否要把"彻底删除组织"确认强度的加强同步反馈给 PC 端保持两端一致，需要产品/工程侧另行决策，不在本次移动端重设计的授权范围内。
- Org Profile 的 Logo 上传功能本身（PC 端尚未实现），若未来 PC 端补齐，移动端需要同步跟进设计。
