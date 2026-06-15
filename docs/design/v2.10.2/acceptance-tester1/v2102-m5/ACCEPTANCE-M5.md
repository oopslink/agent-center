# v2.10.2 验收·M5 [Tester1] Environment/Worker Activity & SSE & 项目列表检索

**结论：GO ✅（Item1 + Item3 PASS）· Item2 T135 本轮 SKIPPED（per owner 2026-06-15）**

- 分支/commit：v2.10.2 @ 27184d7b（origin frozen tip）
- 实例：test-v2102acc（web console v2.10.2-27184d7b）；登录 Owner v2102acc
- go vet（observability/query + webconsole/api + webconsole/sse）clean

## 逐项结论

### 1. Worker Activity feed（T140/T141）→ PASS
- 工作项显示 `T<n> + title`：fleet 快照对 work-item 行做读时 enrich，加 `task_org_ref`(T<n>) + `task_title` + `project_id`。
- 点击链接进正确任务页不 404：行链接是 **project 作用域** `/projects/{project_id}/tasks/{task_id}`；live `GET …/projects/project-2ee9e524/tasks/task-1a22bfa6 → 200`（旧 bare `task-<id>` 链接会 404，已修）。
- agent 显示**名字** + 点击进 agent 详情（T141）：live tasks 行 `assignee.display_name="Sandbox Agent v2102acc"` + `member_id=agent-4d821b1d`（解析到 agent 详情）。
- 证据：`m5_api_live.txt`、`m5_tests_green.png`（TestFleetSnapshot_WorkItemCarriesTaskTitleAndProject / TestWorkItemRowFromProjection_MapsAllFields + 13 fleet 用例全绿）。

### 2. SSE connect 连接稳定性（T135）→ SKIPPED（本轮，per owner）
- owner 2026-06-15 指示「T135 这次先跳过」。
- 阻塞背景（供后续）：要求"域名+Cloudflare+本地代理"真实链路复现，但唯一稳定 CF 隧道 `agents.oopslink.tech→:7100` 跑 main-350ba02（**不含 T135**）；含 T135 的 v2.10.2 仅 localhost 直连。trycloudflare quick-tunnel 指向 :57009 时本机 mihomo 代理干扰 cloudflared 边缘连接，edge 路由不稳。
- 参考（非本轮判据）：服务端线协议 + 客户端状态机单测全绿——`T135_ImmediateHeartbeatOnConnect`、`T104_PrimesStreamForBufferingProxy`、`HeartbeatIsRealDataMessageWithoutID`、`StreamsEventAndHeartbeat`（连上即心跳 + 首帧 open + 15s 心跳 + buffering-proxy priming）。
- 待 owner 安排 CF 环境后由 run-real 补做。

### 3. 项目内 Task/Issue 列表检索（T131）→ PASS
- 项目内列表与全局列表**同一套检索条件**（仅 project 维度固定）：live 项目 Task 列表接受 `?created_after/created_before/assignee/status`（HTTP 200）。
- 校验对齐全局：坏 RFC3339 时间格式在**全局与项目两侧都 400**（param parity）。
- 终态默认 + 状态筛选（Issue）：handler 单测覆盖。
- 证据：`m5_api_live.txt`、`m5_tests_green.png`（TestPM_ListTasks_ProjectFilters_AssigneeAndTime / TestPM_ListIssues_ProjectFilters_TerminalDefaultAndStatus 全绿）。

## 备注
- UI 截图说明：本实例 web console 用 `Secure` 会话 cookie，headless agent-browser 在 http://127.0.0.1 下不稳定保持会话（curl/in-page fetch 服务端鉴权均 200，非产品缺陷）；故 Item1/3 以 **live API（curl）+ 权威单测** 验收，等价于 UI 渲染的数据源。
- 无 FAIL。Item1/Item3 GO；Item2 本轮 SKIPPED。
