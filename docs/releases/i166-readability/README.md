# I166 画布回归证据

这是隔离的前端 fixture，复现截图中的两行标题、负责人、已替代失败节点、后续承接以及孤立 END。复用实际 PlanDetail、React Flow、ELK 和样式；fetch/EventSource 在入口替换，不访问任何 center 实例。截图证明前端显示行为，不代表生产部署或真实数据验收。

## 复现

在仓库根执行：

```sh
cp docs/releases/i166-readability/fixture.tsx web/i166-review.tsx
cp docs/releases/i166-readability/fixture.html web/i166-review.html
cp docs/releases/i166-readability/vite.config.ts web/i166-vite.config.ts
cd web
pnpm install --frozen-lockfile
pnpm exec vite --config i166-vite.config.ts --host 127.0.0.1 --port 5198
```

浏览器打开 `http://127.0.0.1:5198/i166-review.html`，进入 DAG。该 fixture 的 dev 配置仅为已有 noVNC 依赖的 top-level await 设置 es2022，不改变产品构建配置。

检查：

- 默认 3 个当前节点，结束节点在画布内部，小地图收起；`current-light.png`。
- 展开历史后 4 个节点，失败节点明确标历史、保留原始失败状态；紫色虚线标后续承接，长依赖绕过卡片；`history-light.png`。
- `document.documentElement.classList.add('dark')` 切换深色，打开小地图；`history-dark.png`。
- 390×844 移动端无横向溢出，使用列表展示；`mobile.png`。
- 桌面 `plan-node-assignee` 的底边均不超出所属 `plan-graph-node`；完成卡片 opacity=1。
- 展开/收起历史及改变窗口尺寸后，结束节点仍在画布内部；该项曾在首次验证失败，已改为按最新 ELK 尺寸适配视图。

完成后删除复制到 web 根目录的三个临时文件。
