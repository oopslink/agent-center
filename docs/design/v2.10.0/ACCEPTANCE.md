# v2.10.0 验收方案 / Acceptance Plan（细化版）

**范围**：三栏式桌面 UI/UX 重构（App Shell + Conversations / Workspace / Members / System 全模块）+ 附件·消息·授权修复批。
**不在本轮**：移动端适配（后续阶段）。
**基线对照**：本目录 `*.html` mockup（真 design token 写）；token 源 `web/src/index.css` + `tailwind.config.js`。
**计划**：`plan-42594411`(T63–T72) + 独立项 T60/T62/T73/T74/T75/c780999a。

## 0. 门禁与原则
1. 三道门：① **§-1 自动门**（PD，每模块分支 + 集成树）② **Tester1 data/API + 授权**（硬门）③ **Tester2 run-real 逐模块对照 mockup**。
2. 🚨 **授权硬门（§2）任一不过 → 不 tag、不 promote**（red line）。
3. 每条目（编号 N.M.K，可单独签）有：**验收标准（可观测 PASS 条件）/ 证据 / 负责人 / 状态**（⬜待验 ✅PASS ❌FAIL ⚠️观察）。
4. 验证视口：桌面 **1280 / 1440 / 1920** 三档（无横向溢出）；明暗双模每条都要过。
5. 集成终验在 `origin/v2.10.0` 最终全合树上跑；记录 hash。
6. 🖼 **证据强制（owner 2026-06-14）**：每条签 ✅ 必须附**关键步骤证据** —— Tester2 §3 每模块 / 每关键交互**截图**（明暗各一）+ 关键流程**录屏**；Tester1 §2 授权用例附**请求/响应码**（403/404）。**无证据不签 ✅。** 证据贴 #v2.10.0 频道 + 在各条「证据」列链接，PD 汇总进 release-docs。

---

## 1. §-1 自动门（PD）
| # | 验收项 | 验收标准 | 证据 | 状态 |
|---|---|---|---|---|
|1.1|build|`go build ./...` + 前端 `vite build` exit 0，无 warning 当 error|日志|⬜|
|1.2|tsc|`tsc -b` 0 error（含 React import gate）|日志|⬜|
|1.3|eslint|eslint 0 error 0 warning|日志|⬜|
|1.4|no-raw-colors|`lint-no-raw-colors-spa` clean（全走 token，无 #hex/rgb 硬编码）|日志|⬜|
|1.5|go vet|`go vet ./...` clean|日志|⬜|
|1.6|go test|`go test ./...` 全包 ok（跑前 `export TMPDIR=/tmp/s` 避 agentsupervisor unix-socket 假阳）|日志|⬜|
|1.7|vitest|`npm test` 全 pass，0 fail，0 unhandled|汇总|⬜|
|1.8|范围|每模块分支各跑 + **集成树（含 T64+T73）再跑一次**，记每个 hash|hash 列表|⬜|
|1.9|覆盖|新增交互/组件均有单测（注册表、col②/④、linkify、附件区、scope handlers）|覆盖抽查|⬜|

## 2. Tester1 — data/API + 授权（硬门）
### 2.1 文件越权 class-guard（T60 / T44）— red line
| # | 验收标准 | 证据 | 状态 |
|---|---|---|---|
|2.1.1|**非参与**会话 channel attach → 403/404 fail-closed（无 200）|请求/码|⬜|
|2.1.2|**非参与**会话 channel download(`GET /files/{ulid}`) → 403/404|请求/码|⬜|
|2.1.3|**非参与** DM attach/download → 403/404|请求/码|⬜|
|2.1.4|参与者 attach/download → 200 正常|请求/码|⬜|
|2.1.5|agent 侧（agent file-tools，work-item 域)：参与会话正常、非参与 403/404|请求/码|⬜|
|2.1.6|跨会话拿 file_uri 猜测访问 → 不泄露（404 而非 403,不暴露存在性）|请求/码|⬜|
|2.1.7|transfer complete 复检 session initiator(#142)，他人 complete → 拒|请求/码|⬜|

### 2.2 任务/Issue 附件授权（T73）
| # | 验收标准 | 证据 | 状态 |
|---|---|---|---|
|2.2.1|项目成员 task-scope upload→list→download 往返成功|往返记录|⬜|
|2.2.2|issue-scope 同上往返成功|往返记录|⬜|
|2.2.3|**非成员** attach/download → 403|码|⬜|
|2.2.4|目标 task/issue 缺失 或 **跨项目**(不属路由 {pid}) → 404（不泄露存在性）|码|⬜|
|2.2.5|complete 复检 initiator|码|⬜|
|2.2.6|被指派 agent 仍走 agent file-tools（不被本面破坏）|回归|⬜|

### 2.3 入站附件透传（T74）
| # | 验收标准 | 证据 | 状态 |
|---|---|---|---|
|2.3.1|human 在 DM 发带附件消息 → agent `get_my_unread`/brief 带 file_uri + filename/mime/size|收件 payload|⬜|
|2.3.2|agent 对该 uri `download_file` 成功（自身参与会话）|结果|⬜|
|2.3.3|channel @mention 带附件同样透传|payload|⬜|
|2.3.4|非参与会话 uri → download 403/404|码|⬜|

### 2.4 消息 linkify 数据（T62 / c780999a）
| # | 验收标准 | 证据 | 状态 |
|---|---|---|---|
|2.4.1|`task-<id>` 解析到正确 task（org_ref label + href）|用例|⬜|
|2.4.2|`T<number>`(org_ref) 解析到正确 task|用例|⬜|
|2.4.3|解析不到的 ref → 不误转（保留纯文本）|用例|⬜|
|2.4.4|跨项目 task 引用也能解析到详情|用例|⬜|

### 2.5 路由 / 接口契约
| # | 验收标准 | 证据 | 状态 |
|---|---|---|---|
|2.5.1|模块嵌套路由全部可达（Workspace/Conversations/Members/System 及子路由），无死链|路由抽查|⬜|
|2.5.2|旧 Overview/Home 路由已移除或重定向，无 404 暴露|抽查|⬜|
|2.5.3|各 list/detail/plan/workboard 接口返回正确 schema，分页/筛选参数生效|接口抽查|⬜|
|2.5.4|无 4xx/5xx 回归（与 v2.9.2 比）|对比|⬜|

## 3. Tester2 — run-real 逐模块（对照 mockup；每条截图，关键交互录屏）
> 通用：结构与 mockup 一致；**明暗双模**均过；1280/1440/1920 无横向溢出；交互可用；中文/英文混排不破版。

### 3.1 App Shell（T63）· `shell-conversations-tasks.html`
|#|验收标准|状态|
|---|---|---|
|3.1.1|col① 图标栏含 Workspace/Conversations/Members/System 四项，顺序/图标/标签正确|⬜|
|3.1.2|点 col① 图标切换顶层模块，active 高亮正确|⬜|
|3.1.3|col② 随①切换为该模块二级；切回保留/重置合理|⬜|
|3.1.4|col③ 显示选中项内容；col④ 仅在需要的视图出现（会话/任务/plan）|⬜|
|3.1.5|Overview/Home 已移除，无入口|⬜|
|3.1.6|侧栏折叠/展开可用且非 hover-only（点击触发），状态持久化|⬜|
|3.1.7|底部 footer：Live(SSE) 指示 / 明暗切换 / 用户 / 签出 均在且可用|⬜|
|3.1.8|快捷键：⌘K 命令面板、⌘B 折叠、⌘D 主题（及现存 ⌘1..7 若保留）正常|⬜|
|3.1.9|org 切换器可用|⬜|

### 3.2 Conversations（T64）· `shell-conversations-tasks.html`
|#|验收标准|状态|
|---|---|---|
|3.2.1|col② 分 Channels / DMs 两段，各列具体会话，未读角标数正确|⬜|
|3.2.2|选中会话 col③ 渲染消息流（own/other 气泡、作者、时间）|⬜|
|3.2.3|composer：拖拽文件上传|⬜|
|3.2.4|composer：⌘V 粘贴截图上传|⬜|
|3.2.5|composer：附件托盘（图片缩略图 / 文件 chip / 上传进度条 / 移除）|⬜|
|3.2.6|composer：类型 + 大小校验（≤25MB；非法类型提示）|⬜|
|3.2.7|col④ Channel→参与者面板（邀请/移除）；DM→对方资料|⬜|
|3.2.8|消息内附件渲染（图片预览 / 文件 chip）+ 下载经 gated 路径|⬜|
|3.2.9|空会话 / 长消息 / 多附件 呈现正常|⬜|

### 3.3 Workspace · Tasks/Issues 全局列表（T65）· `shell-conversations-tasks.html`
|#|验收标准|状态|
|---|---|---|
|3.3.1|col③ 跨项目表格列：ID/标题/项目/状态/负责/更新|⬜|
|3.3.2|筛选（状态/项目/负责/时间）生效|⬜|
|3.3.3|选中行 col④ 只读元数据栏 + Edit 入口|⬜|
|3.3.4|Issues 与 Tasks 同版式、互不串|⬜|
|3.3.5|空列表 / 大量行（滚动）正常|⬜|
|3.3.6|状态 chip 颜色按 token（running/open/blocked/done）|⬜|

### 3.4 Projects + 项目详情（T66）· `projects.html`
|#|验收标准|状态|
|---|---|---|
|3.4.1|Projects 列表（名称/状态/计数）|⬜|
|3.4.2|选项目 → col② 变项目子导航：Issues/Tasks/Work Board/Members/Code repos|⬜|
|3.4.3|col③ 显示选中 tab 内容；tab 切换正确|⬜|
|3.4.4|archived 项目状态呈现正确（不混入活跃）|⬜|
|3.4.5|Edit / Archive 操作可用|⬜|

### 3.5 Project Work Board（T67）· `workboard.html`
|#|验收标准|状态|
|---|---|---|
|3.5.1|Backlog 列 + 每 Plan 一列 + New Plan 列|⬜|
|3.5.2|任务卡拖拽进 Plan / 跨列编排生效并持久|⬜|
|3.5.3|卡片显示 id/标题/状态/负责|⬜|
|3.5.4|空 Backlog / 无 Plan 空态正常|⬜|

### 3.6 Plan 全局列表 + 详情（T68）· `plan.html`
|#|验收标准|状态|
|---|---|---|
|3.6.1|col③ 全局 plan 列表，可搜索/筛选（名称/状态/项目/进度）|⬜|
|3.6.2|点开 → Plan 详情 = Chat / DAG / Task列表 三 tab|⬜|
|3.6.3|Chat tab：plan 会话 + composer|⬜|
|3.6.4|DAG tab：节点+依赖+状态色；paused 节点如实显示（关联 T75）|⬜|
|3.6.5|Task列表 tab：节点任务表（与 DAG 同源）|⬜|
|3.6.6|col④ plan 概要/参与者；Start/Stop/Advance/resume 操作可用|⬜|
|3.6.7|进度随子节点刷新（不卡旧值）|⬜|

### 3.7 Members（T69）· `members.html`
|#|验收标准|状态|
|---|---|---|
|3.7.1|col② Humans / Agents 两段；Agents 展开列表|⬜|
|3.7.2|Humans col③ 成员表（Name/Role/Invited/Status）+ Add/启停/移除|⬜|
|3.7.3|Agent 详情四 tab：Profile/Activity/Workspace/Work items|⬜|
|3.7.4|生命周期 Start/Stop/Restart（按状态 gating）+ Reset/Archive|⬜|
|3.7.5|在线状态点 online/busy/offline 与 Chat 一致|⬜|
|3.7.6|col④ 当前工作项 + 归属计划；点头像可开 DM|⬜|

### 3.8 System（T70）· `system.html`
|#|验收标准|状态|
|---|---|---|
|3.8.1|col② Environment / Settings|⬜|
|3.8.2|Environment：stats（active/agents/workers）+ worker 卡（含 CLI 安装命令复制）|⬜|
|3.8.3|Environment 三段 tablist：Work items / Issues / Transfers|⬜|
|3.8.4|Settings：版本面板（Version/Branch/Commit/Built）|⬜|
|3.8.5|三栏无 col④（验证「按需第四栏」逻辑正确）|⬜|

### 3.9 消息 linkify（T62 + c780999a）
|#|验收标准|状态|
|---|---|---|
|3.9.1|`task-<id>` 纯文本自动转链接、点击跳详情|⬜|
|3.9.2|`T<number>`(T123) 自动转链接、点击跳详情|⬜|
|3.9.3|**收发双向**：human 发 + agent 发 均生效|⬜|
|3.9.4|代码块（反引号)内/已有链接内 **不转**|⬜|
|3.9.5|不误转普通文本（边界），明暗双模链接色按 token|⬜|

### 3.10 系统通知作者（T75）
|#|验收标准|状态|
|---|---|---|
|3.10.1|plan 会话自动派发/系统通知作者显示 **System**（稳定 name+avatar），非「(deleted)」|⬜|
|3.10.2|明暗双模下作者样式正常|⬜|

## 4. 细节与跨模块（每屏过）
|#|验收项|验收标准|状态|
|---|---|---|---|
|4.1|明暗双模|每模块每屏 light/dark 均正确；own-bubble 固定 #D1E3FF+深字；无硬编码色|⬜|
|4.2|空态|每列表/面板空态有占位文案，不空白崩|⬜|
|4.3|加载态|首屏/切换有 loading（skeleton/spinner），无闪烁布局跳动|⬜|
|4.4|错误态|接口失败有错误提示 + 重试入口，不白屏|⬜|
|4.5|长内容|长标题/长名/长 URL 截断省略，不溢出三栏|⬜|
|4.6|响应式|1280/1440/1920 三档无横向滚动；col 宽度合理|⬜|
|4.7|导航一致|col①/col② 选中态跨切换一致；浏览器前进/后退正确;深链接可达|⬜|
|4.8|焦点/键盘|主要交互键盘可达、焦点环可见(基本 a11y)|⬜|
|4.9|未读/角标/状态|未读数、状态 chip、生命周期 badge 准确|⬜|
|4.10|i18n|中英文混排不破版；mockup 英文 UI 与实际文案口径一致|⬜|
|4.11|性能基本|大列表（>100 行）滚动不卡死；切模块无明显白屏|⬜|

## 5. 回归（不破现有）
|#|验收项|验收标准|状态|
|---|---|---|---|
|5.1|实时|SSE 未读/在线状态、@mention 唤醒正常|⬜|
|5.2|附件既有链路|v2.9.2 消息附件收发不破|⬜|
|5.3|账户|登录/登出/组织切换正常|⬜|
|5.4|plan 看板|v2.9.2 plan 看板（去截断/T# 上卡/进度刷新）不回归|⬜|
|5.5|权限角色|owner/member/agent 各角色可见性与操作权限不越界|⬜|

## 6. 签字与发布流程
**签字表**

| 角色 | 范围 | 状态 | 日期 | 集成 hash |
|---|---|---|---|---|
|PD|§1 §-1 自动门 + 集成终验|⬜| | |
|Tester1|§2 data/API + 授权硬门|⬜| | |
|Tester2|§3 run-real 逐模块 + §4 细节 + §5 回归|⬜| | |
|Owner|tag / promote 决策|⬜| | |

**流程**：T64+T73 合入 v2.10.0 → 集成终验 §-1（§1）→ Tester1（§2 授权硬门）+ Tester2（§3/§4/§5 逐条签）→ **全绿 + 授权硬门过** → PD 汇总 release-docs（本表签字 + 证据链接）→ **Owner 定 tag v2.10.0 + promote**。授权硬门未过不 promote。

> 任一条 ❌ → 回责任 dev 修 → 重跑该条 + 相关 §-1 → 复签。⚠️观察项不阻塞但需记录跟进。
