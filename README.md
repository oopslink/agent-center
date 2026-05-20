# agent-center

Agent center —— DDD-driven 设计文档 + （未来）Go 实现。

## 设计文档

设计在 [`docs/`](docs/) 下，按 DDD 分层（战略 / 战术 / 决策 ADR / 实现）。先看：

- [DDD 蓝图（推进 plan & status）](docs/design/ddd-blueprint.md)
- [战略层入口（领域愿景）](docs/design/architecture/strategic/00-domain-vision.md)
- [项目规约 / 跨切原则](docs/rules/conventions.md)

## 本地查看可视化文档站点

仓内 [`sites/`](sites/) 是 VitePress 静态站点脚手架，源 markdown 直接来自 [`docs/`](docs/)。

```bash
cd sites/
npm install           # 首次安装依赖

# 开发模式（推荐日常用）
npm run dev           # → http://localhost:5173，markdown 改动热重载

# 构建静态产物 + 本地预览
npm run build         # → sites/.vitepress/dist/ 纯静态文件
npm run preview       # → http://localhost:4173 离线浏览构建产物
```

**离线分发**：拷走 `sites/.vitepress/dist/` 整个目录到任何 http server 即可（nginx / `python -m http.server` / `npx serve` 均可）。

**自动更新**：站点跟 `docs/` 同源 —— 你改 markdown 后 `npm run dev` 即时刷新，或 `npm run build` 重新生成静态产物。

详细架构 / 依赖见 [`sites/package.json`](sites/package.json) 与 [`sites/.vitepress/config.ts`](sites/.vitepress/config.ts)。
