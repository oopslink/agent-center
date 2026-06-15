# v2.10.1 验收方案 / Acceptance Plan（细化版）

**范围**：移动端体验优化（把 v2.10.0 三栏桌面适配到 <768 移动）+ claimability 规则（开放认领 + 类守护授权硬门 + 持有上限 N=3）+ 桌面/Plan UI 增强（Thread/参与者侧栏拖拽、Channel 三 tab、Plan archived 只读、col① 侧栏 rail）+ 本周期追加项（Plan P 号 + linkify、org_ref #id-tail bug、SenderDetailSidebar、关联 plan 跳转、入站附件 wake 透传、SSE 跳变修复）。
**不在本轮**：plan unarchive（owner 拍：仅可查看）；移动端新功能（仅适配既有 8 模块）。
**基线对照**：本目录 `*.html` mockup（`v2.10.1-mobile.html` / `workboard-mobile.html` / `desk-thread-resize.html` / `desk-participant-resize.html` / `desk-channel-tabs.html` / `desk-plan-archived.html` / `desk-sidebar-rail.html`，真 design token 写；token 源 `mods.css`，与 v2.10.0 同源）。
**计划**：plan-9674fa13（移动 M1–M7 + claimability T83 + 桌面增强 T95/T96/T97/T98）+ 追加项 task-f628b23d/60bc3a1a/96eb3d70/e09d04f7/8ac7ad77/b867de44/574245cd。INT task-5a501b96 · ACC task-b2dbbd40。

## 0. 门禁与原则
1. 三道门：① **§-1 自动门**（PD，每模块分支 + 集成树）② **Tester1 data/API + 授权**（硬门）③ **Tester2 run-real 逐模块对照 mockup**。
2. 🚨 **授权硬门（§2）任一不过 → 不 tag、不 promote**（red line）。具体红线 = T83 claimability 用例 2.1.1/2.1.3/2.1.4（backlog 不可领 / 非成员 opaque / 并发 CAS）+ 入站附件越权 **2.2.3**（非参与方 fail-closed）。
3. 每条目（编号 N.M.K，可单独签）有：**验收标准（可观测 PASS 条件）/ 证据 / 负责人 / 状态**（⬜待验 ✅PASS ❌FAIL ⚠️观察）。
4. 验证视口：移动 **<768（主测 375 / 390 / 430）** + 桌面 **≥md（768 / 1024 / 1280 / 1440 / 1920）**（无横向溢出）；明暗双模每条都要过。
   - **测试方式（owner 2026-06-15）：端到端，移动端 + 电脑端两套都要测。移动端用 Chrome DevTools 设备模拟（Device Mode）**——预设如 iPhone SE(375) / iPhone 12(390) / Pixel(430)，开触控模拟；桌面用真实窗口尺寸。每条 §3 run-real 在对应端真实运行实例上走完整端到端用户旅程并截图/录屏。
5. 集成终验在 `origin/v2.10.1` 最终全合树上跑；记录 hash。
6. 🖼 **证据强制（owner 2026-06-14 / 规范 2026-06-15）**：每条签 ✅ 必须附**关键步骤证据** —— Tester2 §3 每模块 / 每关键交互**截图**（明暗各一）+ 关键流程**录屏**；Tester1 §2 授权用例附**请求/响应码**（403/404/200）。**无证据不签 ✅。** 证据以**附件**形式贴 #v2.10.1 频道 + @PD（file_uri 进 PD inbox），并在各条「证据」列链接；**严禁只给 workspace 路径**；PD 汇总进 release-docs（内嵌截图导 PDF）。
7. 🧭 **功能验收 = 端到端用户旅程（end-user 视角，owner 2026-06-14）**：每个特性按一条完整 user journey 真跑（真浏览器 / 真实例，必要处真 agent computed-truth），从用户进入 → 操作 → **结果可见/可用**；不止组件/单屏渲染。判据 = 用户能真正走通该流程且结果如预期/mockup（verify-not-trust，看真实结果不靠组件测代过）。
8. 📐 移动判据：`<md` 切移动布局（底 Tab 导航 A）、`≥md` 维持 v2.10.0 三栏；断点切换无布局崩坏、无横向滚动；触控目标 ≥44px。

---

## 1. §-1 自动门（PD）
| # | 验收项 | 验收标准 | 证据 | 状态 |
|---|---|---|---|---|
|1.1|build|`make build`（go build ./... + 前端 vite build）exit 0，无 warning 当 error|日志|⬜|
|1.2|tsc|`make lint` 内 `tsc -b` 0 error（含 React import gate）|日志|⬜|
|1.3|eslint|`make lint` 内 eslint 0 error 0 warning|日志|⬜|
|1.4|no-raw-colors|`lint-no-raw-colors-spa` clean（全走 token，无 #hex/rgb 硬编码；移动新样式同样过）|日志|⬜|
|1.5|go vet|`make lint` 内 `go vet ./...` clean|日志|⬜|
|1.6|scripts|`make lint` 内 scripts 校验 clean|日志|⬜|
|1.7|go test|`make test`（go test ./...）全包 ok；**已知假阳**：workerdaemon `TestSupervisorSession_DetachSurvives`（supervisor socket / TMPDIR env）→ **clean-env rerun**（`export TMPDIR=/tmp/s` 后单跑该用例确认绿）|日志 + clean-env rerun|⬜|
|1.8|vitest|`make test` 内 `npm test` 全 pass，0 fail，0 unhandled|汇总|⬜|
|1.9|范围|**每模块分支各跑** + **集成树（含全部移动 M1–M7 + T83 + T95/T96/T97/T98 + 追加项）再跑一次**，记每个 hash|hash 列表|⬜|
|1.10|覆盖|新增交互/组件均有单测（claimability 谓词 + CAS + 上限计数、ResizablePanel、Channel tabs、archived 筛选、mobile shell/底 Tab、P 号 linkify、org_ref label、wake file_uri 透传、SSE no-transform）|覆盖抽查|⬜|

## 2. Tester1 — data/API + 授权（硬门）
> 授权红线条目不过 **不 tag、不 promote**。每条附**请求/响应码** + 测试名/断言行；红线条目附截图或日志。

### 2.1 claimability 类守护（T83 · task-2c899f57）— red line
> 折入 `T83-claimability-spec.md` §4 全部 5（+1）用例，逐条带证据。守护在**服务层**而非仅 UI；越权 opaque（403/404 不泄露存在性）。

| # | 用例 | 验收标准（期望） | 证据 | 状态 |
|---|---|---|---|---|
|2.1.1|backlog 任务（planID==""）|不在任何人 claimable；直接 `start_work` 被拒|请求/码|⬜|
|2.1.2|池内未指派任务 + project 成员 agent|可见、可领；领后 `assignee=该 agent`、`status=running`（原子 open→running + 落 assignee）|请求/码 + 前后态|⬜|
|2.1.3|池内未指派任务 + **非成员** agent|不可见；`start_work` **403/404 opaque**（不泄露存在性）|请求/码|⬜|
|2.1.4|两 agent **并发**领同一池任务|仅一个成功；另一个 `already_claimed`（CAS/乐观锁，version 校验）；**无双 assignee**|请求/码 ×2 + 终态|⬜|
|2.1.5|结构化 plan 节点|仍只被其指派 agent 领（`assignee!=""` 保持）；他人不可领|请求/码|⬜|
|2.1.6|agent 已持有 **N=3** 个已领池任务，再领第 4 个|被拒 `pool_claim_limit_reached`；完成在手一个后可再领；结构化 plan 节点**不受此限**|请求/码 + 计数|⬜|
|2.1.7|可见性派生一致|`get_my_work.claimable_tasks` = 自己被指派 dispatched ∪ 所在 project 开放池任务；`get_task.claimable` 与上同步；backlog 永不出现|抽查 payload|⬜|
|2.1.8|N 可配置|改 org/系统级配置项 N → 上限随之生效（默认 N=3）|配置 + 复测|⬜|

### 2.2 入站附件授权（T103 · task-8ac7ad77）
> T74 半修遗留：wake 投递剥附件 + 推游标导致 agent 看不到入站附件。本轮 wake payload 须带 `file_uri`。

| # | 验收标准 | 证据 | 状态 |
|---|---|---|---|
|2.2.1|human→agent DM/channel 发图 → agent **唤醒消息内联**拿到 `file_uri`（+ filename/mime/size），非被剥|收件 payload|⬜|
|2.2.2|agent 对该 uri `download_file` → **200**（自身参与会话）|结果 + 码|⬜|
|2.2.3|**非参与方** agent 取该附件 → **403/404 fail-closed**（无 200，不泄露存在性）|请求/码|⬜|
|2.2.4|游标推进正确：附件透传后 unread 游标不丢消息、不重复唤醒|游标/序列抽查|⬜|

### 2.3 路由 / 接口契约 sanity
| # | 验收标准 | 证据 | 状态 |
|---|---|---|---|
|2.3.1|移动布局复用同一组接口/路由（无新增端点歧义），桌面/移动同源数据一致|路由抽查|⬜|
|2.3.2|claimability 改动后 `get_my_work` / `get_task` / plan 节点 DTO schema 不破，旧字段兼容|接口抽查|⬜|
|2.3.3|archived plan 只读详情接口可达；start/advance 等写操作对 archived 拒绝|接口抽查|⬜|
|2.3.4|Plan P 号序列、org_ref label/href、关联 plan 跳转字段返回正确|接口抽查|⬜|
|2.3.5|无 4xx/5xx 回归（与 v2.10.0 比）|对比|⬜|

## 3. Tester2 — run-real 逐模块（**端到端用户旅程**；对照 mockup；每条截图，关键交互录屏）
> 功能特性须从终端用户视角走完整旅程(进入→操作→结果可见/可用),非孤立组件检查。每模块下列项串成可走通的 journey。
> 通用：结构与 mockup 一致；**明暗双模**均过；移动 `<768`（主测 375/390/430）无横向溢出、≥md 桌面不破；交互可用；触控 ≥44px；中文/英文混排不破版。

### 3.1 Mobile Shell（M1 · task-aab6eb82）· `v2.10.1-mobile.html`（导航壳/底 Tab 帧）
|#|验收标准|状态|
|---|---|---|
|3.1.1|`<768` 切移动布局：**底部 Tab 导航**含 Workspace/Conversations/Members/System（col① 四模块），图标/标签/顺序正确|⬜|
|3.1.2|点底 Tab 切顶层模块，active 高亮正确；`≥md` 自动回 v2.10.0 三栏（断点切换无崩坏）|⬜|
|3.1.3|移动层级流：col②整屏列表 → col③整屏详情 → col④底部 sheet（导航 A）|⬜|
|3.1.4|后退/返回手势/返回按钮回上一层级正确；深链接可达|⬜|

### 3.2 Conversations 移动（M2 · task-4d5bcc79）· `v2.10.1-mobile.html`（Conversations 帧）
|#|验收标准|状态|
|---|---|---|
|3.2.1|整屏会话列表（Channels/DMs），未读角标正确；点入整屏消息流|⬜|
|3.2.2|消息流 own/other 气泡、作者、时间正确；own-bubble 固定色|⬜|
|3.2.3|composer 在移动可用：附件托盘 / 上传进度 / 发送；附件渲染（图片预览/文件 chip）+ gated 下载|⬜|
|3.2.4|col④ 信息（参与者/对方资料）以底部 sheet 呈现|⬜|

### 3.3 Workspace 列表移动（M3 · task-8aecc929）· `v2.10.1-mobile.html`（Tasks/Issues 卡片流帧）
|#|验收标准|状态|
|---|---|---|
|3.3.1|跨项目列表在移动转**卡片流**（非表格），每卡含 ID/标题/项目/状态/负责/更新|⬜|
|3.3.2|筛选（状态/项目/负责）在移动可用；状态 chip 颜色按 token|⬜|
|3.3.3|点卡 → 整屏详情；Edit/元数据以 sheet 呈现|⬜|
|3.3.4|Issues 与 Tasks 同卡片版式、互不串；空态正常|⬜|

### 3.4 Plan 移动（M4 · task-fdff6e8b）· `v2.10.1-mobile.html`（Plan Chat/DAG-stepper/Task 帧）
|#|验收标准|状态|
|---|---|---|
|3.4.1|Plan 详情移动三 tab：Chat / DAG / Task|⬜|
|3.4.2|**DAG → 纵向 stepper**：节点+依赖+状态色竖排呈现，paused 节点如实|⬜|
|3.4.3|Chat tab 会话 + composer 可用；Task tab 节点表与 stepper 同源|⬜|
|3.4.4|Start/Stop/Advance/resume 操作经 sheet 可用；进度刷新不卡旧值|⬜|

### 3.5 Work Board 移动（M5 · task-f45880ad）· `workboard-mobile.html`（竖屏横滑 vs 横屏对比）
|#|验收标准|状态|
|---|---|---|
|3.5.1|**竖屏**：列（Backlog/各 Plan/New Plan）**横向滑动**浏览，卡显 id/标题/状态/负责|⬜|
|3.5.2|**横屏 landscape**：呈现对照 mockup 的横屏布局（更多列可见）|⬜|
|3.5.3|竖↔横旋转切换无布局崩坏；空 Backlog/无 Plan 空态正常|⬜|

### 3.6 Members 移动（M6 · task-ef6fc35a）· `v2.10.1-mobile.html`（Members 帧）
|#|验收标准|状态|
|---|---|---|
|3.6.1|整屏 Humans/Agents 列表；在线点 online/busy/offline 与 Chat 一致|⬜|
|3.6.2|Agent 详情移动呈现（Profile/Activity/Workspace/Work items）；生命周期操作经 sheet|⬜|
|3.6.3|点头像可开 DM；当前工作项 + 归属计划以 sheet 呈现|⬜|

### 3.7 System 移动（M7 · task-0b4b275e）· `v2.10.1-mobile.html`（System 帧）
|#|验收标准|状态|
|---|---|---|
|3.7.1|Environment/Settings 在移动整屏呈现；stats + worker 卡（CLI 安装命令复制）可用|⬜|
|3.7.2|Work items/Issues/Transfers tablist 在移动可切换|⬜|
|3.7.3|Settings 版本面板（Version/Branch/Commit/Built）正确|⬜|

### 3.8 Thread 面板拖拽调宽（T95 · task-97c7600a）· `desk-thread-resize.html`（仅 ≥md）
|#|验收标准|状态|
|---|---|---|
|3.8.1|Thread 面板（col④）左缘有 resize grip，hover `cursor:col-resize`|⬜|
|3.8.2|拖拽改宽：min ~320px / **max 75vw**，主内容随之压缩不溢出|⬜|
|3.8.3|宽度 **localStorage 持久化**（刷新后保留）|⬜|
|3.8.4|明暗双模 grip/面板样式正常|⬜|

### 3.9 参与者侧栏拖拽调宽（T97 · task-412a6835）· `desk-participant-resize.html`（仅 ≥md）
|#|验收标准|状态|
|---|---|---|
|3.9.1|参与者侧栏复用**同一 ResizablePanel**：grip / col-resize / min320 / max75vw / 持久化|⬜|
|3.9.2|拖拽生效且持久；主内容压缩正常|⬜|
|3.9.3|与 T95 Thread 拖拽行为一致（同组件）|⬜|

### 3.10 Channel Chat/Threads/Files 三 tab（T96 · task-67fff619 · variant B）· `desk-channel-tabs.html`（仅 ≥md）
|#|验收标准|状态|
|---|---|---|
|3.10.1|Channel 侧栏分段头三 tab：**Chat / Threads / Files**（IA 定稿 variant B）|⬜|
|3.10.2|Chat=消息流 / Threads=thread 列表 / Files=文件列表；同一时刻显示当前 tab|⬜|
|3.10.3|tab 切换正确、选中态正确；忽略 mockup 内 variant A 注解|⬜|
|3.10.4|Files tab 文件列表点击经 gated 路径下载|⬜|

### 3.11 全局 Plan 列表 Active/Archived（T98 · task-4f903bf7）· `desk-plan-archived.html`（仅 ≥md）
|#|验收标准|状态|
|---|---|---|
|3.11.1|Plan 列表 header 有 **Active/Archived 分段筛选**，切换生效|⬜|
|3.11.2|Archived 行**灰显 + "Archived" 角标**；不混入 Active|⬜|
|3.11.3|点 archived → **只读详情**（DAG/节点/历史可看）；**不可改 / 不可 start**（写操作禁用或拒绝）|⬜|
|3.11.4|本轮不做 unarchive（无 unarchive 入口）|⬜|

### 3.12 col① 侧栏 rail（T105 · task-574245cd）· `desk-sidebar-rail.html`（仅 ≥md）
|#|验收标准|状态|
|---|---|---|
|3.12.1|连接状态图标：**WiFi + 彩点呼吸动画 + tooltip**（状态文案可达）|⬜|
|3.12.2|搜索入口在 rail 内可用|⬜|
|3.12.3|底部用户面板：**Light/Dark 胶囊 Toggle**（玻璃质感）切换主题生效|⬜|
|3.12.4|底部用户面板：**Sign out** 可用|⬜|
|3.12.5|明暗双模 rail 样式/呼吸动画正常|⬜|

### 3.13 SenderDetailSidebar 活动侧栏（T102 · task-96eb3d70）
|#|验收标准|状态|
|---|---|---|
|3.13.1|Task detail 侧栏 **agent 名点击** → 弹出 `SenderDetailSidebar`（活动侧栏）|⬜|
|3.13.2|Issue detail 侧栏同样可点 agent 名弹出|⬜|
|3.13.3|活动侧栏内容正确（复用既有组件，无串数据）|⬜|

### 3.14 Task detail 关联 plan 跳转（T106 · task-e09d04f7）
|#|验收标准|状态|
|---|---|---|
|3.14.1|Task detail 侧栏展示**关联 plan**（P 号 + plan 名）|⬜|
|3.14.2|点击 → **跳转 plan 详情**，落对应 plan|⬜|
|3.14.3|无关联 plan 时不显示空链接/不崩|⬜|

### 3.15 Plan P 号 + 消息 plan-id/P123 linkify（T99 · task-f628b23d）
|#|验收标准|状态|
|---|---|---|
|3.15.1|Plan 显示 **P&lt;number&gt;** 序列号（列表/详情一致）|⬜|
|3.15.2|消息内 `plan-<id>` / `P123` **双向 linkify**（human 发 + agent 发均生效），点击跳 plan 详情|⬜|
|3.15.3|代码块（反引号）内/已有链接内 **不转**；不误转普通文本|⬜|
|3.15.4|明暗双模链接色按 token|⬜|

### 3.16 org_ref #id-tail bug 消除（T100 · task-60bc3a1a）
|#|验收标准|状态|
|---|---|---|
|3.16.1|工作项列表/各列表显示 **T&lt;n&gt;**（org_ref label）而非 `#<id 尾>`|⬜|
|3.16.2|全面无 `#b6eb82` 式短哈希（审计清单 11 处含 `AgentWorkItems:190` 全清）|⬜|
|3.16.3|Issues/Tasks/看板卡/详情各处一致显示 org_ref|⬜|

### 3.17 SSE 状态稳定（T104 · task-b867de44）
|#|验收标准|状态|
|---|---|---|
|3.17.1|SSE 连接**稳定 open**，不再 connecting↔reconnecting 跳变（`bus.go:151` 加 `no-transform` + owner CF 侧关缓冲）|⬜|
|3.17.2|实时未读/在线/@mention 唤醒功能不受影响|⬜|
|3.17.3|长时间挂起观察（>5min）状态指示稳定|⬜|

### 3.18 入站附件端到端（T103 · task-8ac7ad77）· **终端用户视角**（owner 点名重点）
> §2.2 是 Tester1 API/授权层；本节是 Tester2 run-real **真界面端到端用户旅程**，对照 owner 早先实测 bug（发图 agent 看不到）。基线 df84b08 实例。
|#|验收标准|状态|
|---|---|---|
|3.18.1|真界面：human 在聊天框（DM/channel）发**图片/文件** → 目标 agent **被唤醒的消息内联看到**附件（`file_uri` + 文件名/mime/大小），未被剥|⬜|
|3.18.2|该 agent `download_file` 取下 → **成功看到内容**（图片可读/文件正确），端到端闭环（截图：发送端 + agent 收到 + download 结果）|⬜|
|3.18.3|回归对照：owner 早先「发图 agent 看不到」场景，在 df84b08 上**现已能看到**|⬜|

## 4. 细节与跨模块（每屏过）
|#|验收项|验收标准|状态|
|---|---|---|---|
|4.1|明暗双模|每模块每屏 light/dark **均截图两态**；own-bubble 固定 #D1E3FF+深字；无硬编码色|⬜|
|4.2|响应式断点|移动 `<768`（主测 **375/390/430**）+ 桌面 ≥md（**768/1024/1280/1440/1920**）无横向滚动；断点切换无崩坏|⬜|
|4.3|空态|每列表/卡片流/面板空态有占位文案，不空白崩|⬜|
|4.4|错误态|接口失败有错误提示 + 重试入口，不白屏|⬜|
|4.5|截断|长标题/长名/长 URL/P 号/org_ref 截断省略，不溢出（移动卡片同样过）|⬜|
|4.6|未读/角标/状态|未读数、状态 chip、生命周期 badge、claimable 标记准确|⬜|
|4.7|导航一致|底 Tab / col①/col② 选中态跨切换一致；前进/后退正确；深链接可达；移动层级返回正确|⬜|
|4.8|token 一致|移动新样式 + 桌面增强全走 mods.css token，与 mockup 同源，无 raw color|⬜|
|4.9|a11y|tooltip 可达（rail WiFi/呼吸点）；resize grip 键盘/焦点可达；focus-trap（sheet/sidebar）；**触控 ≥44px**；role/aria 正确|⬜|
|4.10|i18n|中英文混排不破版（移动窄屏同样过）；mockup 英文 UI 与实际文案口径一致|⬜|
|4.11|性能|大列表（>100 行/卡）滚动不卡死；切模块/切断点无明显白屏；横滑/拖拽流畅|⬜|

## 5. 回归（不破现有 v2.10.0）
|#|验收项|验收标准|状态|
|---|---|---|---|
|5.1|桌面三栏 8 模块|v2.10.0 桌面 App Shell + Conversations/Workspace/Projects/WorkBoard/Plan/Members/System ≥md 不回归|⬜|
|5.2|附件链路|v2.10.0 消息/任务/issue 附件收发 + 越权 fail-closed 不破|⬜|
|5.3|linkify 既有|v2.10.0 `task-<id>` / `T<number>` linkify 不回归（新增 P 号 linkify 不冲突）|⬜|
|5.4|paused-resume|plan paused 节点显示 + resume 链路不破|⬜|
|5.5|实时|SSE 未读/在线/@mention 唤醒正常（配合 T104 修复后更稳）|⬜|
|5.6|权限角色|owner/member/agent 各角色可见性与操作权限不越界（含 claimability 新守护）|⬜|

## 6. 签字与发布流程
**签字表**

| 角色 | 范围 | 状态 | 日期 | 集成 hash |
|---|---|---|---|---|
|PD|§1 §-1 自动门（每模块 + 集成树）+ 集成终验|⬜ 待验| | |
|Tester1|§2 data/API + 授权硬门（claimability T83 红线 + 入站附件 T103）|⬜ 待验| | |
|Tester2|§3 run-real 逐模块（移动 M1–M7 + 桌面增强 + 追加项）+ §4 + §5|⬜ 待验| | |
|Owner|tag / promote 决策|⬜ 待定| | |

**流程**：各移动模块 + claimability + 桌面增强 + 追加项 **dev done → PD §-1 绿（每模块分支）→ IntegrationDev 合（INT task-5a501b96，增量 --no-ff）→ 集成终验 §-1（§1，集成树全跑）→ Tester1（§2 授权硬门，带请求/码逐条签）+ Tester2（§3/§4/§5 逐条带证据签）→ 全绿 + 授权硬门过 → PD 汇总 release-docs（本表签字 + 内嵌端到端关键路径截图导 PDF）→ Owner 定 tag v2.10.1 + promote**。**授权硬门（§2.1 红线 2.1.1/2.1.3/2.1.4 + §2.2.3）未过不 promote。**

> 任一条 ❌ → 回责任 dev 修 → 重跑该条 + 相关 §-1 → 复签（复签证据命名 `rs_`）。⚠️观察项不阻塞但需记录跟进。
> ⚠️ TaskDetailSidebar/IssueDetailSidebar 同文件多任务（T100/T102/T106 均 dev1）→ 串做避冲突，验收时交叉复检无回退。
