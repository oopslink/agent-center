# I166 generation 显示与状态语义修复验收

2026-09-17；基线 main 1fb4a702。对应测试计划：docs/plans/i166-generation-test-plan.md。

## 结果

- 历史快照优先采用明确任务状态：completed、discarded、failed、running、paused、blocked；dispatch_records 不再覆盖失败/撤销。撤销卡片显示独立状态与 Historical 标签。
- revision 决定稳定颜色。时间轴、R 标签、节点边框/色条同色；承接虚线采用目标代颜色。所选代新增节点着色，前代保留节点浅底实框，历史/撤销节点虚框。正文 opacity=1。
- 当前版本与创建时快照分别说明，历史快照显示时间；已结束计划旧控制诊断默认收起，并显示诊断时间。
- Overview 与 100% 定位所选代均可操作。浏览器观测 viewport scale 分别为 0.821212 和 1；agent 名字点击继续打开详情浮层。
- 浅色/深色切换无页面错误。深色 R1/R2/R3 色条分别为 rgb(139,178,233)、rgb(233,139,151)、rgb(139,233,155)，R2 承接线与 R2 色条一致。

## 验证

- 聚焦状态/页面测试：117 项通过。
- 全量前端测试：201 个文件、1,945 项通过；make lint、make build 均退出 0。
- 浏览器使用隔离 fetch/EventSource fixture，未请求生产 center；截图是回归样例，不代表已上线。
- 无部署动作；生产生效需后续正常部署。

## 复现与证据

`i166-generations/fixture.tsx` 为完整隔离 fixture。复制到 web/i166-review.tsx，使用临时 HTML entry 导入它，以 Vite 启动；项目 noVNC 依赖要求 optimizeDeps.esbuildOptions.target=es2022。浏览器打开 entry，选择 DAG。

截图：`i166-generations/current.png`（当前有效图）、`history.png`（含历史）、`snapshot.png`（历史快照）、`dark.png`（深色）、`readable.png`（100%定位）。依次切换历史开关、R2/R3、两种缩放，再点击 agent 名字检查详情浮层。
