# v2.10.1 设计契约（mockup = contract，dev 照做）

v2.10.1 = **移动端体验优化**（把 v2.10.0 三栏桌面适配到 <768 移动）+ **claimability 规则** + **4 条桌面/Plan UI 增强**。
所有 mockup 复用 v2.10.0 设计系统 token（`mods.css`，与 v2.10.0 同源）。`≥md` 维持三栏，`<md` 切移动布局。

## 任务 ↔ mockup 文件对照

### 移动适配（导航 A = 底部 Tab，col①四模块；col②整屏列表 → col③整屏详情 → col④底部 sheet）
| 节点 | 任务 | mockup |
|------|------|--------|
| M1 Mobile Shell | task-aab6eb82 | `v2.10.1-mobile.html`（导航壳/底 Tab 帧） |
| M2 Conversations | task-4d5bcc79 | `v2.10.1-mobile.html`（Conversations 帧） |
| M3 Workspace 列表 | task-8aecc929 | `v2.10.1-mobile.html`（Tasks/Issues 卡片流帧） |
| M4 Plan（DAG→纵向 stepper） | task-fdff6e8b | `v2.10.1-mobile.html`（Plan Chat/DAG-stepper/Task 帧） |
| M5 Work Board（竖滑+横屏 landscape） | task-f45880ad | `workboard-mobile.html`（竖屏横滑 vs 横屏对比） |
| M6 Members | task-ef6fc35a | `v2.10.1-mobile.html`（Members 帧） |
| M7 System | task-0b4b275e | `v2.10.1-mobile.html`（System 帧） |

### claimability
| 节点 | 任务 | 说明 |
|------|------|------|
| claimability (T83) | task-2c899f57 | **完整 spec 见 `T83-claimability-spec.md`**。backlog 不可领（现状已满足）；**Assignment Pool 开放认领**（去掉 pool 任务的 assignee 要求）；认领=start_work 原子 assignee+running+CAS；类守护 authz 硬门；**每 agent 持有池任务上限 N=3**（owner 拍定，可配置）。 |

### 桌面 / Plan UI 增强（仅 ≥md 桌面）
| 节点 | 任务 | mockup | dev 要点 |
|------|------|--------|----------|
| T93 Thread 面板拖拽调宽 | task-97c7600a | `desk-thread-resize.html` | Thread 面板（col④）左缘 resize grip，`cursor:col-resize`；min ~320px / **max 75vw**；宽度 localStorage 持久化；主内容随之压缩。 |
| T96 Channel 侧栏 Chat/Threads/Files Tab | task-67fff619 | `desk-channel-tabs.html` | **✅ IA 定稿(owner 2026-06-15)= variant B：Chat/Threads/Files 三 tab 组织整块**（mockup 主画的就是 B）。三 tab 分段头：Chat=消息流 / Threads=thread 列表 / Files=文件列表，同一时刻显示当前 tab。忽略 mockup 里 variant A 注解。 |
| T95 参与者侧栏拖拽调宽 | task-412a6835 | `desk-participant-resize.html` | 参与者侧栏复用 **T93 同一个 ResizablePanel 组件**（grip/col-resize/min320/max75vw/持久化）。 |
| T98 全局 Plan 列表查看 archived | task-4f903bf7 | `desk-plan-archived.html` | Plan 列表 header 加 Active/Archived 分段筛选；Archived 行灰显 + "Archived" 角标；点进去 = **只读详情**（DAG/节点/历史可看，不可改/不可 start）；**本轮不做 unarchive**（owner 拍：仅可查看）。 |

> 注：上表 org_ref 实际为 T95 Thread / T96 Channel / T97 参与者 / T98 Plan-archived（进 plan 后排号；早期标注有偏差，以任务 id 为准）。

### 本周期追加项（owner 2026-06-15 陆续提，均 v2.10.1，挂 INT）
| 任务 | 内容 | mockup/spec | dev |
|------|------|-------------|-----|
| task-f628b23d | Plan 序列号 P&lt;number&gt; + 消息 plan-id/P123 linkify（对称 T 号/T-linkify） | 任务描述 | dev1 |
| task-60bc3a1a | Bug：列表显示 `#b6eb82` 短哈希而非 org_ref；全面消除 `#<id-tail>`（AgentWorkItems:190 等 11 处） | 任务描述（含审计清单） | dev1 |
| task-96eb3d70 | Task/Issue detail 侧栏 agent 名点击 → 弹出活动侧栏（复用 `SenderDetailSidebar`） | 任务描述 | dev1 |
| task-e09d04f7 | Task detail 侧栏展示关联 plan + 点击跳转 plan 详情（接 P 号） | 任务描述 | dev1 |
| task-8ac7ad77 | Bug：入站附件 agent 看不到（wake 投递剥附件+推游标，T74 半修）；wake payload 带 file_uri | 任务描述 | dev2 |
| task-b867de44 | Bug：SSE connecting↔reconnecting 跳变 = Cloudflare 缓冲流；`bus.go:151` 加 `no-transform`（+owner CF 侧关缓冲） | 任务描述 | dev2 |
| task-574245cd | col① 侧栏整合：连接状态图标(WiFi+彩点呼吸+tooltip)+搜索+底部用户面板(Light/Dark 胶囊 Toggle+Sign out 玻璃质感) | `desk-sidebar-rail.html` | dev3 |

> ⚠️ **TaskDetailSidebar/IssueDetailSidebar 同文件多任务**（task-60bc3a1a / task-96eb3d70 / task-e09d04f7 均 dev1）→ 串做避冲突。

## 集成 / 验收
- INT task-5a501b96：IntegrationDev 合并各移动模块 + claimability + 4 条桌面增强进 v2.10.1（增量 --no-ff，dev done → PD §-1 绿 → 合）。
- ACC task-b2dbbd40：验收门 = PD §-1（每模块+集成树）+ Tester1 claimability authz 硬门 + Tester2 run-real 逐模块对照本目录 mockup（移动 8 模块 + 4 条桌面，明暗双模、关键步骤截图）。

## 注记
- 桌面增强 mockup 的 shell chrome 内联样式沿用 v2.10.0 mockup 既有写法（`.desk4`/`.rail`/`.c2`/`.main`/`.ctx` 在各文件 `<style>` 内联），可复用 token 来自 `mods.css`。
- T96 的 IA 已定稿 = **variant B（Chat/Threads/Files 三 tab 组织整块）**，owner 2026-06-15 拍板；mockup `desk-channel-tabs.html` 主视图即 B，其内 variant A 注解作废。
