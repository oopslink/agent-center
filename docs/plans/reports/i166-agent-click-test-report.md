# I166 DAG agent 点击测试报告

对应 [测试计划](../i166-agent-click-test-plan.md)。基线 `63758ee0`。

根因：React Flow 在 selectable=false / draggable=false 且无节点层事件处理时，为 wrapper 写入 pointer-events:none。子卡片此前未覆盖这个继承值，真实鼠标命中画布。旧 fireEvent 单测绕过浏览器 hit testing，不能发现此问题。

修复：卡片显式 pointer-events:auto，并用 nopan 保留内部交互；不启用节点拖动或选择。卡片键盘事件仅处理自身焦点，避免吞掉子按钮的 Enter/Space，或误开任务页。

| # | 结果 | 证据 |
|---|---|---|
| 1 | PASS | 原版 elementFromPoint 命中 react-flow__pane，buttonReceivesClick=false；修复后同位置命中 agent 按钮，buttonReceivesClick=true。真实鼠标点击打开侧栏，显示 agent-center-dev1，请求 /api/agents/agent-center-dev1 及其 activity/concurrency/tasks。 |
| 2 | PASS | 浏览器分别 Enter/Space 打开侧栏，tab list 始终只有原标签；新增单测断言子按钮键盘事件未被取消且 window.open 未触发，卡片本身 Enter 仍开任务。标题链接可命中。 |
| 3 | PASS | 节点 draggable/selectable 数均为0，背景拖动仍改变 viewport transform。 |
| 4 | PASS | PlanDetail 114 tests；全量200 files / 1942 tests；make lint、make build通过。 |

截图：[agent-click.png](../../releases/i166-readability/agent-click.png)。沿用并补全隔离前端 fixture 的 agent 详情、成员解析、活动和任务响应；不访问 center HTTP/DB，不代表生产上线。
