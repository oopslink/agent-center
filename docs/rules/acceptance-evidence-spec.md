# 验收证据与报告规范 / Acceptance Evidence & Report Spec

适用**所有版本**验收(owner 2026-06-15 立;起因:v2.10.0 验收报告两次缺端到端关键路径截图)。

## 1. 证据强制
每个验收条目签 ✅ 必须附证据,无证据不签;❌ 回责任 dev 修 → 复跑 → 复签。

## 2. 证据类型
- **Tester1(data/API + 授权)**:每条附**请求/响应码**(403/404/200)+ 测试名/断言行;授权红线条目附截图或日志。
- **Tester2(run-real 功能)**:**端到端用户旅程截图** —— 每模块按完整 user journey(进入 → 操作 → **结果可见/可用**)逐步截图,**明暗双模各一**;关键流程录屏。**非孤立组件截图。**

## 3. 端到端关键路径(必须有图,逐条)
- 发消息 → 拖/粘附件 → 参与者/agent 下载(gated 200)
- 建 task → 入 Plan → Start → DAG 节点态推进 / 进度刷新
- 消息 linkify(task-<id> / T<number>)点击跳详情
- system/调度通知作者显示 System
- 附件越权(非参与/非成员 → 403/404 fail-closed)
- 每模块 col①(底Tab/图标)/ col② / col③ / col④(sheet) 切换

## 4. 证据交付（关键——避免再次缺图）
- 证据图必须以**附件**形式贴到 `#<版本>` 频道,并 **@PD**(使 file_uri 进 PD inbox 可下载);**严禁只给 workspace 路径/目录引用** —— PD 拿不到文件就无法嵌入报告。
- 命名:`s<段号>_<描述>`(如 `s31_appshell`),复签 `rs_`,明暗 `_light` / `_dark`。
- ⚡ **agent 间频道 @ 不唤醒对方**(代码 loop-break:只有 human/任务派发唤醒 agent)。故 PD 拉测试方证据走 **task 指派**(work_available 唤起);测试方完成后主动把附件贴频道 + @PD。

## 5. 验收报告规范
- 报告必须**内嵌所有端到端关键路径截图**(inline `<img>`,不是路径引用)。
- 🚫 **全嵌齐才出报告,不出半成品**;缺任一关键路径图 → 不发,先补图。
- 报告内容 = ACCEPTANCE.md(验收项 + 标准 + §6 签字表)+ 内嵌证据图(§2 授权 / §3 端到端逐路径 / 复签)→ 导出 PDF。
- 生成法:md → HTML(`python3 -m markdown`,extensions=tables,fenced_code)+ 内嵌 `<img>` → Chrome `--headless --print-to-pdf`。
- 流程:dev done → PD §-1 → Tester 验收(证据附件 @PD)→ PD 汇总内嵌报告(PDF)→ owner tag/promote。

> 教训(2026-06-15):缺图根因 = 证据只给路径没传附件 + PD 没等齐就发。本规范堵这两个口。
