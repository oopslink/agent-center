# agent-center

Agent center —— DDD-driven 设计 + Go 实现。

## v2 状态（Phase 11 ship'd）

- **单 binary**：`agent-center` 含 server / supervisor / worker / 所有 CLI 子命令 + React Web Console SPA（go:embed）
- **Web Console**：loopback bind 默认 `127.0.0.1:7100`，浏览器开同一 port 即用；远程通过 SSH 隧道 `ssh -L 7100:127.0.0.1:7100`
- **CLI 全套**：`agent-center help` 列分组命令树；channel / dm / issue / task / agent / secret / input-request / fleet 全 CRUD

```bash
# 完整构建
make build

# 跑 server（启用 web console）
cat > /tmp/server.yml <<EOF
server:
  listen_addr: 127.0.0.1:7099
  sqlite_path: ~/.agent-center/center.db
identity:
  default_user: hayang
web_console:
  enabled: true
  listen_addr: 127.0.0.1:7100
EOF
./bin/agent-center server --config /tmp/server.yml
# 浏览器开 http://127.0.0.1:7100/

# 主 CLI 操作
./bin/agent-center help                          # 命令树
./bin/agent-center channel create --name=alpha   # 建 channel
./bin/agent-center conversation tail <conv-id> -f
./bin/agent-center input-request list
```

## 设计文档

设计在 [`docs/`](docs/) 下，按 DDD 分层（战略 / 战术 / 决策 ADR / 实现）。先看：

- [DDD 蓝图（推进 plan & status）](docs/design/ddd-blueprint.md)
- [战略层入口（领域愿景）](docs/design/architecture/strategic/00-domain-vision.md)
- [Web Console 战术文档](docs/design/architecture/tactical/presentation/01-web-console.md) + [SPA 架构](docs/design/architecture/tactical/presentation/02-spa-architecture.md)
- [部署](docs/design/implementation/06-deployment.md)
- [CLI 子命令完整签名](docs/design/implementation/03-cli-subcommands.md)
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
