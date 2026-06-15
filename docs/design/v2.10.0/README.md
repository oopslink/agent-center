# v2.10.0 · 三栏式桌面 UI/UX 重构 — Mockups

Plan: `plan-42594411`(project agent-center2)。每个任务对照本目录对应 mockup 实现 —— **优先看 `.html`(用真 design token 写,可直接照抄结构/间距/配色/类名);`.png` 是渲染图**。共享样式 `mods.css`。

布局 = 三栏(按需四栏):col① 顶层模块图标栏(Workspace/Conversations/Members/System) / col② 二级 / col③ 内容 / col④ 按需上下文。落现有 IA(去 Overview)。配色/字体取自 `web/src/index.css` + `tailwind.config.js`。

## 任务 → mockup 对照

| 任务 | org_ref | mockup 文件 | 看哪部分 |
|---|---|---|---|
| T1 App Shell 三栏骨架 | T63 | `shell-conversations-tasks.{html,png}` | 整体三栏骨架(图标栏/二级/内容/按需四栏);例1/2/3 |
| T2 Conversations | T64 | `shell-conversations-tasks.{html,png}` | 例1:col② Channels+DMs / col③ 消息流+composer / col④ 参与者 |
| T3 Tasks/Issues 全局列表 | T65 | `shell-conversations-tasks.{html,png}` | 例2:col③ 跨项目表格+筛选 / col④ 元数据 |
| T4 Projects + 项目详情 | T66 | `projects.{html,png}` | Projects 列表 + 项目子导航(col②)+ tab 内容(col③) |
| T5 Project Work Board | T67 | `workboard.{html,png}` | 项目内 plans 看板:Backlog+每Plan列+New Plan |
| T6 Plan(列表+详情) | T68 | `plan.{html,png}` | 全局 plan 列表 → 详情 Chat/DAG/Task列表 三 tab |
| T7 Members | T69 | `members.{html,png}` | col② Humans/Agents / Agent 详情四 tab+生命周期 / Humans 表 |
| T8 System | T70 | `system.{html,png}` | Environment(stats+worker+三段)+ Settings |

> 注:`shell-conversations-tasks` 一张图含 T1/T2/T3 三例(骨架 + Conversations + Tasks),实现各自任务时看对应例。Conversations/Tasks 的更细对照也可参考骨架内同款排布。
>
> 本地预览:`open shell-conversations-tasks.html`(或任意 .html)。移动端适配为后续阶段,本目录暂为桌面稿。
