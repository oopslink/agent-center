# sites/ — agent-center 文档站

> **这是一个零构建的手写静态站。** 没有 `package.json`、没有 VitePress、没有任何构建步骤。
> 所有页面是手写 HTML，共享一份 `assets/site.css` / `assets/site.js`。

## 它是什么

`sites/` 是 agent-center 的**对外门面（showcase）**：把 `docs/` 里权威、详尽的设计/手册内容，
挑出"对外要讲清楚"的部分，用统一的「Engineering Blueprint」设计系统呈现。

- **`docs/` 是权威源（source of truth）**——详尽、随代码演进、给贡献者看。
- **`sites/` 是精选门面**——给第一次了解项目的人看，**不是 `docs/` 的逐字镜像**。

改 `docs/` 时不必同步改 `sites/`；只有当某个 sites 页对应的源发生**对外可见的实质变化**
（命令变了、架构调整了、版本发布了）时，才照下面的对照表更新对应页。

## 本地预览

直接开文件，或起个静态服务（全相对路径，二者皆可）：

```bash
open sites/index.html                  # 直接用浏览器打开
python3 -m http.server -d sites 5173   # 然后访问 http://localhost:5173
```

## 部署

`.github/workflows/pages.yml`：每次 push 到 `main` 且 `sites/**` 有改动时，
把整个 `sites/` 目录当静态文件发布到 **GitHub Pages**（项目子路径 `/agent-center/`）。
**没有构建步骤**——上传即发布。所以站内链接必须全部用**相对路径**。

## 目录结构

```
sites/
├── index.html              首页：四大板块入口 + 开发指引版本卡
├── product/index.html      产品介绍（单页，不版本化）
├── manual/<ver>/index.html 用户手册 · 按版本（当前 v2.15.0）+ 版本切换器
├── dev/<ver>/index.html    开发指引 · DDD 可视化 · 按版本（当前 v2.15.0）
├── roadmap/index.html      路线图（节奏决策：已完成 / 推迟 / 愿景）
├── designs/<ver>/index.html 旧 URL 重定向兜底页（见下）
├── assets/site.css         共享设计系统（蓝图风格 + 暗色 + DDD 语义色 token）
├── assets/site.js          共享渐进增强（scroll-reveal / copy / TOC scrollspy / tabs）
└── .nojekyll               关掉 GitHub Pages 的 Jekyll 处理
```

## 页 ↔ 源 对照表（改 docs 时照这张表检查）

| 站点页 | 主要内容 | 权威源 |
|---|---|---|
| `index.html` | 项目门户、导航 | — |
| `product/index.html` | 是什么 / 核心能力 / 架构概览 | `README.md`、`docs/design/requirements/` |
| `manual/<ver>/index.html` | 安装 / 部署 / CLI / `worker run` / MCP 工具 / Web Console / 约定 | `docs/deployment/`、`docs/operations/`、`internal/cli/`、`internal/webconsole/` |
| `dev/<ver>/index.html` | DDD 架构：限界上下文 / 战术设计 / Event Storming / 核心机制 | `docs/design/architecture/`、`docs/design/decisions/`（ADR）、`internal/*` |
| `roadmap/index.html` | 已完成 / backlog / v3 推迟 / 边界 | `docs/design/roadmap.md`、`docs/design/requirements/03-out-of-scope.md` |

## 版本约定

- **结构**：可版本化的区域统一用 `<区>/<版本>/index.html`（如 `manual/v2.7.1/`、`dev/v2.7.1/`）。
- **当前版**：首页、导航、版本切换器都指向"当前版"。当前版 = **站点最近一次同步到的 release**
  （目前 `v2.7.1`；仓库代码可能已领先，站点按节奏跟进，不要求实时追平）。
- **版本切换器**：用 `.verswitch` 构件，当前版标 `class="on"`，其余为历史版链接。
- **历史快照**：旧版本页**全部保留**（便于回溯演进），并明确标注"历史"。当前不设上限——
  若将来版本页过多，再决定归档策略。新增一版时：拷当前版页 → 改版本号与内容 → 旧版页改标"历史"
  并把切换器里的 `on` 移到新版。
- **`product/` 暂不版本化**：产品概览变化慢，保持单页。

## `designs/<ver>/` 是什么

`designs/v2.7/index.html` **不是内容页，是一个旧 URL 的重定向兜底页**
（`<meta http-equiv="refresh">` → `dev/v2.7/`）。早期开发指引曾放在 `designs/` 下，
后来迁到 `dev/`；保留这个重定向是为了不断掉外部已分享的旧链接。**不要当死目录删掉。**

## 设计系统约定（改/加页时遵守）

- 所有页 `<head>` 引入共享 `assets/site.css` + `assets/site.js`（用对相对层级的 `../`）。
- 用既有 token 与构件，不要硬编码颜色：蓝图风格、暗色经 `prefers-color-scheme` 自动切换。
- DDD 图用 CSS 构件画（`.ctx` 上下文 / `.agg` 聚合 / `.es-flow` 事件风暴 / `.bc-block`），
  **不用 SVG/mermaid 外链**，零依赖。语义色 token：`--core --support --ar --vo --repo`。
- 动效是渐进增强：无 JS 也能完整阅读，`prefers-reduced-motion` 下全部关闭。
- DDD 绘制规范见仓库 `docs/rules/ddd-design-diagram.md`。
