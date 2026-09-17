# I166 DAG agent 点击修复测试计划

1. 真实浏览器：点击前对 agent 按钮中心执行 elementFromPoint，复现命中画布而非按钮；修复后同位置命中按钮，正常鼠标点击打开 agent 侧栏，正确请求该 agent。
2. DAG 卡片内 agent 按钮使用 Enter/Space 打开侧栏，不触发卡片的新标签页动作；标题链接继续可点击。
3. 节点仍不可拖动/选择，背景仍可平移；不通过空 onNodeClick 或开启选择绕过只读约束。
4. PlanDetail/布局组件回归、完整 pnpm test、make lint、make build；真实浏览器截图留存。不访问真实 center HTTP/DB，沿用隔离前端 fixture。
