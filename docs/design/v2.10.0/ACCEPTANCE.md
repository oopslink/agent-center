# v2.10.0 验收方案 / Acceptance Plan

**范围**:三栏式桌面 UI/UX 重构(App Shell + Conversations / Workspace / Members / System 全模块)+ 附件·消息·授权修复批。
**不在本轮**:移动端适配(后续阶段,导航 A/B/C 待定)。
**基线对照**:本目录 `*.html` mockup(真 design token 写);配色/字体取自 `web/src/index.css` + `tailwind.config.js`。
**计划**:`plan-42594411`(T63–T72)+ 独立项 T60/T73/T74/T75/T62/c780999a。

## 0. 门禁与原则
1. 三道门:① **§-1 自动门**(PD,每模块分支 + 集成树)② **Tester1 data/API + 授权**(硬门)③ **Tester2 run-real 逐模块对照 mockup**。
2. 🚨 **授权硬门(§2.1–2.3)任一不过 → 不 tag、不 promote**(red line)。
3. 每项有明确**验收标准**(PASS 条件)+ **证据要求** + **负责人**;Tester 逐项签字(状态:⬜待验 / ✅PASS / ❌FAIL)。
4. 集成终验在 `origin/v2.10.0`(含 T64)上跑;当前 HEAD `87a52f9`(6 模块已合,缺 T64)。

---

## 1. §-1 自动门(PD)
| # | 验收项 | 验收标准 | 证据 | 状态 |
|---|---|---|---|---|
|1.1|make build|`go build` + 前端 `vite build` exit 0|构建日志|⬜|
|1.2|make lint|`tsc -b` + eslint + **no-raw-colors** + go vet 全 clean|lint 日志|⬜|
|1.3|go test|`go test ./...` 全包 ok(跑前 `export TMPDIR=/tmp/s` 短路径避 agentsupervisor unix-socket 假阳)|test 日志|⬜|
|1.4|vitest|`npm test` 全 pass(0 fail)|vitest 汇总|⬜|
|1.5|范围|每模块分支各跑 + **集成树(含 T64)再跑一次**|各 hash 日志|⬜|

## 2. Tester1 — data/API + 授权(硬门)
| # | 验收项 | 验收标准 | 证据 | 状态 |
|---|---|---|---|---|
|2.1|**文件越权 class-guard**(T60/T44)|**非参与会话**对 attach/download → **403/404 fail-closed**(无任何 200 泄漏);参与者正常 200;覆盖 channel + DM、human + agent 两侧|每用例请求/响应码|⬜|
|2.2|**任务附件授权**(T73)|非授权身份对 task-scope attach/download → 403/404;项目成员/被指派 agent → 正常|请求/响应|⬜|
|2.3|**入站附件透传**(T74)|human 在 DM/channel 发带附件消息 → agent `get_my_unread` 能拿到 file_uri+元数据 → `download_file` 成功;非参与会话取该 uri → 403/404|收件 payload + download 结果|⬜|
|2.4|消息 linkify 数据(T62/c780999a)|`task-<id>` 与 `T<number>`(org_ref)都解析到正确 task;解析不到则不误转|API/解析用例|⬜|
|2.5|路由/接口契约|模块嵌套路由全部可达;各列表/详情/plan/workboard 接口返回正确 schema;无 4xx/5xx 回归|接口抽查|⬜|

## 3. Tester2 — run-real 逐模块(对照 mockup,每项截图;关键交互录屏)
> 通用标准:布局结构与对应 mockup 一致;明暗双模均正常;无横向溢出;交互可用。

### 3.1 App Shell 三栏骨架(T63)· mockup `shell-conversations-tasks.html`
| 验收标准 | 状态 |
|---|---|
|col① 图标栏 = Workspace/Conversations/Members/System,点击切顶层;col② 二级随①切换;col③ 内容;col④ 仅需要的视图出现|⬜|
|Overview/Home 已移除;路由按模块嵌套;侧栏折叠可用(非 hover-only);⌘K/⌘B 等快捷键正常|⬜|

### 3.2 Conversations(T64)· `shell-conversations-tasks.html`
| 验收标准 | 状态 |
|---|---|
|col② Channels + DMs 列表 + 未读角标;col③ 消息流 + composer;col④ Channel→参与者、DM→对方资料|⬜|
|composer:拖拽上传 / ⌘V 粘贴截图 / 附件托盘(缩略图·文件 chip·进度)/ 类型大小校验|⬜|

### 3.3 Workspace · Tasks/Issues 全局列表(T65)· `shell-conversations-tasks.html`
| 验收标准 | 状态 |
|---|---|
|col③ 跨项目表格(ID/标题/项目/状态/负责/更新)+ 筛选;col④ 选中只读元数据栏;Issues 与 Tasks 同版|⬜|

### 3.4 Projects + 项目详情(T66)· `projects.html`
| 验收标准 | 状态 |
|---|---|
|Projects 列表 → 选项目 col② 变项目子导航(Issues/Tasks/Work Board/Members/Code repos)→ col③ 对应 tab 内容|⬜|

### 3.5 Project Work Board(T67)· `workboard.html`
| 验收标准 | 状态 |
|---|---|
|Backlog + 每 Plan 一列 + New Plan;任务卡可拖拽编排;col② 项目子导航 Work Board 选中|⬜|

### 3.6 Plan(全局列表 + 详情)(T68)· `plan.html`
| 验收标准 | 状态 |
|---|---|
|col③ 全局可搜索/筛选 plan 列表(名称/状态/项目/进度)→ 点开 Plan 详情 = **Chat / DAG / Task 列表** 三 tab;col④ plan 概要/参与者|⬜|

### 3.7 Members(T69)· `members.html`
| 验收标准 | 状态 |
|---|---|
|col② Humans / Agents(Agents 展开列表);col③ Humans 表、Agent 详情四 tab(Profile/Activity/Workspace/Work items)+ Start/Stop/Restart 生命周期;col④ 当前工作项/归属计划|⬜|

### 3.8 System(T70)· `system.html`
| 验收标准 | 状态 |
|---|---|
|col② Environment / Settings;col③ Environment(stats + worker 卡含 CLI + Work items/Issues/Transfers 三段)、Settings 版本面板;三栏无 col④|⬜|

### 3.9 消息 linkify(T62 已修 + c780999a)
| 验收标准 | 状态 |
|---|---|
|`task-<id>` 与 `T<number>`(如 T123)在消息里自动转 task 链接,**收发双向**(human+agent);点击跳任务详情;**代码块内不转**;明暗双模;不误转普通文本|⬜|

### 3.10 系统通知作者(T75)
| 验收标准 | 状态 |
|---|---|
|plan 会话里自动派发/系统通知的作者显示为 **System**(稳定 name+avatar),不再落「(deleted)」|⬜|

## 4. 细节与跨模块(每屏过)
| # | 验收项 | 验收标准 | 状态 |
|---|---|---|---|
|4.1|明暗双模|每模块每屏 light/dark 均正确,无硬编码色(全 token)|⬜|
|4.2|状态态|空态 / 加载态 / 错误态 均有合理呈现|⬜|
|4.3|长内容|长标题/长名截断不溢出|⬜|
|4.4|未读/角标|未读角标、状态 chip 正确|⬜|
|4.5|导航一致|col①/col② 跨模块切换状态一致、可回退|⬜|
|4.6|a11y 基本|键盘可达、焦点可见(基本层面)|⬜|

## 5. 回归(不破现有)
| # | 验收项 | 验收标准 | 状态 |
|---|---|---|---|
|5.1|实时|SSE 未读/在线状态、@mention 唤醒正常|⬜|
|5.2|附件既有链路|消息附件收发(v2.9.2 既有)不破|⬜|
|5.3|账户|登录/组织切换/签出正常|⬜|

## 6. 签字与发布流程
**签字表**

| 角色 | 范围 | 状态 | 日期 | 集成 hash |
|---|---|---|---|---|
|PD|§1 §-1 自动门 + 集成终验|⬜| | |
|Tester1|§2 data/API + 授权硬门|⬜| | |
|Tester2|§3 run-real 逐模块 + §4 细节 + §5 回归|⬜| | |
|Owner|tag / promote 决策|⬜| | |

**流程**:T64 合入 v2.10.0 → 集成终验 §-1(§1)→ Tester1(§2 授权硬门)+ Tester2(§3/§4/§5)→ **全绿 + 授权硬门过** → PD 汇总 release-docs(本表签字 + 证据链接)→ **Owner 定 tag v2.10.0 + promote**。授权硬门未过不 promote。
