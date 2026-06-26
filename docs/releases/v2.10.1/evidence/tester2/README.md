# Tester2 §3/§4 run-real 证据（v2.10.1）

实例：`v2.10.1-da6fde8`(v2101rs) + `df84b08`(v2101s3，前端与 da6fde8 字节一致：df84b08→da6fde8 仅 docs + T119/T124 后端)。真浏览器 agent-browser；移动 Chrome Device Mode 375/390/430；明暗双模。

| 文件 | 对应条目 |
|---|---|
| s31_shell_390_light/dark | §3.1 底 Tab 移动壳（明暗） |
| s42_breakpoint_767_mobile / 768_desktop | §3.1.2/§4.2 断点 768 翻转 |
| s32_conversations_390_light/dark | §3.2 Conversations 移动 |
| s33_tasks_cardflow_390_light | §3.3 卡片流(org_ref+状态+负责) |
| s34_plan_detail_3tab / s34_plan_dag_stepper_390 / da6_m4_plan_dag_stepper_390 | §3.4 三 tab + DAG 纵向 stepper |
| s35_workboard_portrait_390 / landscape_844 | §3.5 竖滑 / 横屏多列 |
| s36_members_agents_390 / s36_agent_detail_4tab_390 | §3.6 Humans/Agents + Agent 详情 4 tab |
| s37_system_390 / s37_system_tablist_390 | §3.7 Env/Settings + Activity tablist |
| s313_task_senderdetail / s313_issue_senderdetail | §3.13 SenderDetailSidebar(task+issue) |
| s314_task_plan_link | §3.14 Task→Plan 关联链接 |
| s315_linkify_code_not_converted / da6_s315_… | §3.15 双向 linkify + 代码块不转 |
| rs_s311_archived_list_light/dark / rs_s311_archived_detail_readonly | §3.11 复签(T124/da6fde8)：archived 列表入口 + 只读详情 |

结论：Tester2 §3(1–7,13–15)+§4 = ✅ GO；§3.11 经 T124 复签 ✅。非阻塞观察：§3.1.1 Tab 文案 Work/Chat、§4.9 移动触控 <44px、§4.11 大列表未压测。
