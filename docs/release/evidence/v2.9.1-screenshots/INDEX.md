# v2.9.1 验收截图索引（用户视角 · 真实例）

实例:PD 亲手起的 v2.9.1 真实例 @ `fa9cdcd`(真 `bin/agent-center` + 内嵌 Web Console)。
数据走与 Web Console 同一套 `/api` 播种:org `acme`(alice)· project `Phoenix` · channel `#general`(含线程)· `old-incidents`(已归档)· 7 任务(backlog/指派池/结构化 Plan)。
浏览器:Chromium 1440×900@2x。**全程 console error = 0。**
复现脚本:[`tests/e2e/v2/capture-v291.mjs`](../../../tests/e2e/v2/capture-v291.mjs)(一键重跑)。
验收者:AgentCenterPD。

| 截图 | 能力 | 关键断言 |
|---|---|---|
| A1_channel_threads.png | A Thread | 顶层消息显示线程按钮+回复数 chip(2/1);右栏 THREADS 列出 2 线程 |
| A2_thread_sidebar.png | A Thread | ThreadSidebar「Thread · 2 replies」= root + 2 回复 + 回复框 |
| A3_thread_list.png | A Thread | 线程列表:发起人 alice / 预览 / 回复数(1/2) |
| D1_org_tasks.png | B 状态机(ADR-0046) | 状态过滤恰 5 态 open/running/completed/discarded/reopened(无 blocked/verified);org 号 T1–T7 |
| C1_work_board.png | C claimable+内置池(ADR-0047) | 三段:Backlog(not claimable)/ Assignment Pool(Built-in·always running·claimable)/ 结构化 Plan |
| H1_plan_chat_tab.png | H Plan UX | Chat/DAG/Task list 三 tab,默认 Chat |
| H2_plan_dag.png | H Plan UX | DAG:START→T5→T6→T7→END;Task 号;派生节点状态(READY/BLOCKED);+Dep 连线编辑;legend;Compact |
| H3_plan_tasks.png | H Plan UX | Task list tab 渲染 plan 内任务 |
| G1_channels_archived.png | G 频道归档 | general ACTIVE 在活动列表(计数1);old-incidents ARCHIVED 在「Archived/已归档」组(只读) |
| I1_dark_channel.png | I both-mode | 暗色频道/线程,chip/THREADS 可读,console=0 |
| I2_dark_work_board.png | I both-mode | 暗色 Work Board 三段可读 |

> 备注:E(工具/门)/F(恢复/运维)非 UI 表面,不截图,由 §0 门 + §-1 + data/API class-guard 覆盖。看板卡片 Unassigned、@agent 线程内回复(真 LLM)说明见验收报告「诚实备注」。
