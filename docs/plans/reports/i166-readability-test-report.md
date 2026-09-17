# I166 画布可读性测试报告

对应 [测试计划](../i166-readability-test-plan.md)。基线 origin/main `e59d7d52`。

| # | 结果 | 证据 |
|---|---|---|
| 1 | PASS | 卡片由84px改为116px，标题两行、负责人固定保留底部空间；真实浏览器逐卡检查负责人底边位于卡片内部。 |
| 2 | PASS | 孤立 END 自动获得展示用虚线，位于叶节点下方；覆盖孤立 START、悬空边、loopback、已有连边不重复。 |
| 3 | PASS | 默认基于 effective=false 隐藏已替代节点，可切换历史并显式标记；follows_task_id 只生成独立的 lineage 展示线。原依赖数组不修改。 |
| 4 | PASS | 完成节点 opacity=1；层间距78→40；小地图默认收起且可切换；最新 ELK 布局直接驱动适配视图，展开历史后 END 仍在可视范围内。 |
| 5 | PASS | 前端全量、lint、build 及浏览器验证，见下。 |

## 验证范围

- 前端单元/组件集成：全量 200 files / 1941 tests。
- 定向测试：PlanDetail、planGraphLayout、InsightCollaboration 149 tests；布局模块 planDagFlow 行覆盖100%、分支90.2%。
- `make lint`：含 go vet、gofmt、规约检查、tsc -b、eslint。
- `make build`：生产 SPA 与 Go binary 构建。
- 真实 Chromium：浅色、深色、小地图切换、历史切换、390px 移动端与窗口缩放。[截图与复现入口](../../releases/i166-readability/README.md)。移动端 document.scrollWidth=innerWidth=390。
- 生产部署 smoke：未执行；本次按 owner 既有要求以合 main 交付，不启动部署修复或重开原计划。

## 限制与附带修复

- 浏览器使用明确的前端 fixture，不将其冒充生产状态或真实 MCP 验收；未访问 center DB/admin/socket。
- 仅在图中已有起终点孤立时补展示线；不改变调度依赖。flat graph 使用 ELK 绕行点，staged graph 保留现有复合容器连线路径。
- 额外发现并修复旧适配视图时序问题：历史节点加入后，旧内部测量结果曾将 END 排在屏外；最新 ELK 边界计算修复后浏览器检查通过。
- 为通过原有 lint，将 InsightCollaboration 8处裸色类替换成对应 semantic status token，无行为变更；其定向测试通过。
