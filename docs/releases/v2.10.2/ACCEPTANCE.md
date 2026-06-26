# v2.10.2 验收方案 / Acceptance Plan

**范围**：v2.10.0/v2.10.1 之后的 UI/UX 收尾批次（owner DM+截图驱动）。十个模块见 T151（拆分为模块验收 task：T152 Plan&WorkBoard+org_ref+resume、T154 Lists/Nav/Agent 面板、其余 M1/M3 移动 + 检索 + SSE）。归 v2.10.2 验收 plan `plan-f9e31a7e`。
**run-real 实例**：自起 sandbox（`bin/agent-center install test-instance --with-agent`），build **v2.10.2-27184d7b**。`v2102acc`（首轮，后期 DB 退化见 §备注）+ `v2102b`（复跑实例，干净）。
**证据目录**：`workspace/v2102-acceptance/`（文件名见各条「证据」）。
**判据**：端到端用户旅程（进入→操作→结果可见/可用）、明暗双模、verify-not-trust。

> 状态图例：✅PASS ❌FAIL ⚠️观察/边界 ⬜待验

---

## Tester2 — run-real 逐模块

### M-Plan：Plan UI（T152 #1｜T132/T134/T142/T147）
| # | 验收标准 | 状态 | 证据 |
|---|---|---|---|
|P.1|Plan 详情三 tab「Chat / DAG / Task List」**无中文括注**（T132）|✅|真 UI tablist=["Chat","DAG","Task List"]，无 "(对话)/(推进计划)/(任务列表)" · `s4_plan_dag_tab_1440_light.png`|
|P.2|去 Plan 详情「← execution view…」说明文字（T134）|✅|tabpanel 无该说明；仅保留 "Node status is derived…" 派生说明 · `s4_plan_dag_tab_1440_light.png`|
|P.3|Workspace 导航与页面标题「Plan」→「Plans」（T142）|✅|左导航项 "Plans"；列表页 h1 "Plans" · `s4_plan_dag_1440_light.png`|
|P.4|Plan Task List assignee **单下拉**（头像+名字合并，T147）|✅|单 button「Reassign …」=Unassigned，展开 listbox(Unassigned/Owner/Sandbox Agent 带头像)，非两控件 · `s4_plan_assignee_dropdown_1440_light.png`|
|P.5|明暗双模|✅|dark bg rgb(15,23,42) · `s4_plan_dag_tab_1440_dark.png`|

### M-OrgRef：#hash 全清显示 org_ref（T152 #2｜T126）
| # | 验收标准 | 状态 | 证据 |
|---|---|---|---|
|O.1|grep 守卫脚本生效：全仓无 `#<id-tail>` 短哈希编码|✅|`make lint-no-idtail-hash` → "clean (no #<id-tail> short-hash id encoding)" RC=0|
|O.2|DAG 节点显示 org_ref（T 号）非 #hash|✅|DAG 节点 T7/T8/T9（v2102acc）、运行态节点 T2/T3（v2102b）· `s4_plan_dag_tab_1440_light.png` / `s152_workboard_running_lock_light.png`|
|O.3|Plan Task List / 列表显示 org_ref|✅|Task List T7-9；Plans 列表 P1/P2；Work Board 节点 T2/T3|
|O.4|纵向 stepper（移动）org_ref|⬜|移动端 DAG 纵向 stepper 截图待补（归 M3 移动模块）|

### M-Board：Work Board（T152 #3｜T121/T144）
| # | 验收标准 | 状态 | 证据 |
|---|---|---|---|
|B.1|plan 列头去「Open ›」，点 plan 名进 plan 详情（T144）|✅|列头为 plan 名链接（蓝色 "SSE Hardening"/"Draft Target"）→ `/projects/{id}/plans/{planId}`，无 "Open ›" · `s152_workboard_1440_light.png`|
|B.2|拖拽 task **跨 plan 移动**（draft→draft，T121）|✅|真 HTML5 DnD：T7 由 P1 拖入 P2 → API 复核 P1=[T8,T9]、P2=[T1,T7]（move 成功）|
|B.3|**running plan 锁定**：列不可拖入、卡不可拖出|✅|运行态 "SSE Hardening" 列 `data-droppable=false`、4 卡 `draggable=false`、含 "This plan is running…re-plan" 提示 + 🔒 图标；draft 列仍 `data-droppable=true` · `s152_workboard_running_lock_light.png`|
|B.4|Backlog→plan / plan→Backlog 校验（draft-only）|✅|decideDrop + data-droppable 上游校验；draft 列接收、running/pool 拒收（结构验证）|

### M-Resume：resume-paused-node（T152 #4｜T101）
| # | 验收标准 | 状态 | 证据 |
|---|---|---|---|
|R.1|paused 节点（agent 暂停其 work item）UI 显示 paused + 出现 operator Resume 操作|✅|**live 复现**：DM 驱使 real agent `pause_work` → 节点 T3 派生 `paused`（DAG + Task List 双视图显示 paused）+ Task List 出现 operator「Resume」按钮 · `s152_resume_paused_node_dag_light.png` / `s152_resume_paused_tasklist_light.png`|
|R.2|Resume 行为正确（成功 re-dispatch+唤醒）|✅|点「Resume」→ T3 paused→**running**（API 复核）+ 列回到 "Auto-advancing"，Resume 按钮消失，无报错 · `s152_resume_paused_result_light.png`|
|R.3|Resume 失败给**准确提示**而非 generic（T101 核心）|✅|后端 discriminated 码实证：非暂停节点→`node_not_paused`「the plan node has no paused work item to resume」；draft/未运行 plan→`plan_not_running`「the plan is not running, so its nodes can't be resumed」；`agent_busy` 码亦在位。前端 `resumeNodeErrorMessage`（PlanDetail.tsx:1619）逐码映射为准确话术|

### M-Lists：列表点名进详情 / 去 Open（T154 #1-3｜T133/T143/T139）
| # | 验收标准 | 状态 | 证据 |
|---|---|---|---|
|L.1|Members Agents 列表：无 Open 按钮，点 agent 名进详情（T133）|✅|桌面：name=link→`/agents/{id}`，行仅 Delete，无 Open · `s5_members_agents_1440_light.png` / `s5_agent_detail_1440_light.png`。移动 390：无横向溢出、name link 在、0 Open · `s154_members_agents_390_light.png`|
|L.2|System·Env Worker AGENTS 列表：去「Open →」、点名进详情、对齐（T143）|✅|桌面：Workers>Agents name=link→`/agents/{id}`，无 Open→ · `s5s7_environment_1440_light.png`。移动 390：无溢出、name link 在、0「Open →/›」· `s154_environment_390_light.png`|
|L.3|Projects 卡片快捷按钮直达 编辑/Work Board/Task/Issue/Codebase，路由正确（T139）|✅|桌面：Edit / Work Board→`/plans` / Tasks→`?tab=tasks` / Issues→`?tab=issues` / Codebase→`?tab=repos`，title→项目详情 · `s154_projects_cards_light.png`。移动 390：卡片精简为 title+stats+⋮ kebab（快捷收入菜单），无溢出 · `s154_projects_cards_390_light.png`|

### M-AgentPanel：Agent 活动侧栏 Open DM（T154 #4｜T136）
| # | 验收标准 | 状态 | 证据 |
|---|---|---|---|
|A.1|Agent 活动侧栏（SenderDetailSidebar）header 有**仅图标 Open DM 按钮**（kind=agent）|✅|点 agent 名开侧栏，header 见 speech-bubble「Open DM」按钮（aria-label/title=Open DM）+ X · `s6_senderdetail_dm_btn_1440_light.png`|
|A.2|点击 Open DM **打开/跳转**与该 agent 的 1:1 DM|❌→✅ **(T159 修复, rs_)**|原 FAIL：点击 POST `/conversations`→200 但**不 navigate**（根因 onClose 同步卸载 AgentDmButton→React Query 丢 per-call onSuccess 的 navigate）。**T159 修复**（dev/v2102-t159-opendm-navigate `b80eb14b`）：navigate 改走 `mutateAsync().then()` 普通 promise 链（不绑观察者，卸载后仍执行）。**Tester2 复验 PASS**：fix 分支自建实例 t159v run-real，点 Open DM → URL `/tasks/{id}`→**`/dms/dm-d2b74eaf`**，DM 打开（2 次复跑一致）· `rs_t159_opendm_navigated_light.png`。**注：fix 尚未合入 v2.10.2，最终 ship 待 IntegrationDev 合并后生效**||

---

### T166 逐任务验收·B（修复后 AFTER 效果图，run-real 合树 c89469d6 / 实例 t166）→ 9/9 PASS
PDF 报告：`ac://files/01KV5ZHMSEHBG1FXSJ2Z6HNA6B`（贴 #agent-center-v2.10.2 @PD）。
| 任务 | verdict | 证据(AFTER) |
|---|---|---|
|T129 移动 Chat Channel↔DM 段控切换|✅|`after_T129_mobile_chat_channels.png` / `after_T129_mobile_chat_dms.png`|
|T131 项目列表检索对齐全局（仅 project 固定）|✅|`after_T131_project_tasks_filters.png`|
|T135 SSE 域名+CF+本地代理 稳定 open 不闪烁|✅（域名+CF 真环境需 owner 部署复跑）|no-transform+X-Accel-Buffering:no+retry:3000+连上即心跳+15s 心跳；本地 15s & 经本地反代 13s 全 Connected 无闪烁 · `after_T135_sse_connected_stable.png` / `after_T135_sse_through_local_proxy.png`|
|T136+T159 Open-DM 点击跳转进 DM|✅|`after_T136T159_opendm_navigated.png`|
|T140 Worker Activity 工作项 T<n>+title 链不 404|✅|`after_T140T141_worker_activity.png`|
|T141 Activity agent 名字+点击进详情|✅|`after_T140T141_worker_activity.png`|
|T148 Composer 按键在底+小号+自增高≤4行+不溢出|✅|`after_channel_sidebar_composer.png` / `after_T148_composer_autogrow.png`|
|T149 移动消息区无横向滚动|✅|`after_T149_mobile_msg_no_hscroll.png`|
|channel 侧栏拖拽调宽+Participants tab 去 header|✅|`after_channel_sidebar_resized_participants.png`|

## 待验（本轮 Tester2 尚未覆盖 / 归其他拆分 task）
- 移动端 M1/M3：Chat 侧栏 Channel↔DM 段控、Task/Issue 详情移动版、消息区无横向滚动、Composer（按键底+小号+自增高≤4 行）、纵向 stepper org_ref（M-OrgRef O.4）。
- 项目内 Task/Issue 列表检索对齐全局（仅 project 固定）。
- Channel 侧栏拖拽调宽 + Participants tab（已在 v2102acc 验 ✅，归 M-Chat 复签：`s3_channel_resized_1440_light.png` / `s3_channel_detail_1440_dark.png`）。
- Worker Activity feed：工作项 T<n>+title 链不 404 + agent 名点击进详情。
- SSE connect：域名 + Cloudflare + 本地代理稳定 open / 断网重连（域名/CF 需 owner 部署环境，本地仅能验本地代理）。

## 备注 / 非阻塞观察
1. **v2102acc 实例后期 DB 退化**：长时间 + 高并发访问后，center err log 刷屏 `webconsole fanout/outbox pump: interrupted (9)`（SQLITE_INTERRUPT），引发间歇 401「invalid session」与登录失败。**全新实例 v2102b 0 次复现、signin 正常** → 判定为该实例 DB 竞争退化（测试侧高频并发所致），**非 v2.10.2 产品缺陷**；但提示后续可关注 SSE outbox pump 在 ctx 取消下的告警噪音。

---

## 签字表
| 角色 | 范围 | 状态 | 日期 | 实例/hash |
|---|---|---|---|---|
|Tester2|T152（Plan UI/org_ref/Work Board/resume）+ T154（Lists/Nav/Agent 面板）|⚠️ **GO（1 FAIL 回 dev）**：T152 全 ✅（Plan UI / org_ref / Work Board 拖拽+running 锁+T144 / **resume-paused live 复现+Resume 正确+准确提示**）；T154 中 Members/System-Env Agents 列表 ✅、Projects 卡片 ✅、**T136 Agent 侧栏 Open DM 不跳转 ❌（回 dev）**|2026-06-15|run-real v2102b（27184d7b）|
|Tester2 复验|T161：复验 T159 修复（Open DM 点击跳转）|✅ **PASS / GO**：fix 分支 `b80eb14b` 自建实例 t159v run-real，点 Open DM 真 navigate 进 DM（2 次复跑），原 FAIL 已修。**ship 待 fix 合入 v2.10.2**|2026-06-15|run-real **b80eb14b**（dev/v2102-t159-opendm-navigate）|
|Owner|tag/promote 决策|⬜ 待定| | |
