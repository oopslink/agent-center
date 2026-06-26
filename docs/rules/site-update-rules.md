# 官网（site）更新规则 · Site Update Rules

适用**每次发版**(owner 2026-06-15 立)。配套封版总流程见 [`RELEASE-PROCESS.md`](./RELEASE-PROCESS.md) step5。

> **本规则的由来(教训)**:v2.10.0 封版时,首页特性卡片 + 路线图更新了,但 **`sites/` 的「用户手册 / 开发指引」整页文档仍停在 v2.8** —— 实际上 v2.9 / 2.9.1 / 2.9.2 三个版本都漏了手册更新,首页所有「用户手册 / 开发指引」链接也一直指向 `manual/v2.8/` / `dev/v2.8/`。根因:历次发版只盯"首页特性卡片",从没把**版本化的手册/开发指引正文**纳入封版清单。本规则就是堵这个口。

## 1. 「site」到底包含什么(别只改首页)

`sites/` 是一套**多页静态站**(GitHub Pages,从 `main` 出),一次发版要同步的远不止首页:

| 文件 / 目录 | 性质 | 发版要做什么 |
|---|---|---|
| `sites/index.html` 特性卡片段 | 每版重写 | 加「vX.Y.Z 新特性」卡片(**真实截图** → `sites/assets/vX.Y.Z/`) |
| `sites/index.html` 路线图卡片 | 每版改一行 | 「vX.Y.Z 已发布」+ 承接上一版 |
| `sites/index.html` **所有 manual/dev 链接** | 每版重指 | 顶栏导航、hero 按钮、文档板块卡片、设计版本归档卡 —— 全部从旧版指到 vX.Y.Z(`grep 'manual/v2\.' / 'dev/v2\.'` 自查无残留) |
| **`sites/manual/vX.Y.Z/index.html`** | **每版新建** | 基于上一版手册 + CHANGELOG 增量(安装/部署/CLI/MCP 工具/Web Console/约定 逐节核对当前代码) |
| **`sites/dev/vX.Y.Z/index.html`** | **每版新建** | 基于上一版开发指引 + 架构演进(限界上下文 / 核心机制 / Event Storming) |
| 各页 **verswitch(版本切换器)** | 每版更新 | 新版页 = 「vX.Y.Z 当前」+ 旧版链接;旧版页 = 顶部加「最新 → vX.Y.Z」,自身降为「历史」(避免用户停在旧版以为是当前) |

## 2. 发版 site 检查清单(硬门,封版前)

- [ ] `sites/manual/vX.Y.Z/index.html` 已建,版本号/面包屑/hero/verswitch 全是 vX.Y.Z
- [ ] `sites/dev/vX.Y.Z/index.html` 已建,同上
- [ ] 手册正文逐节按 CHANGELOG + 当前代码核对(MCP 工具名、路由、CLI、约定 —— **不照搬上一版**,删掉已移除项,补新增项)
- [ ] `sites/index.html`:特性卡片(真实截图)+ 路线图行 + **所有 manual/dev 链接**指向 vX.Y.Z
- [ ] 旧版手册/开发指引页的 verswitch 已加「最新 → vX.Y.Z」并降级自身
- [ ] `grep -rE 'manual/v[0-9]|dev/v[0-9]' sites/index.html` 确认无指向旧版的"当前"链接
- [ ] 至少肉眼/截图核验新页能正常渲染(Chrome headless 截图)

## 3. 经验教训(避免再犯)

1. **「更新 site」≠「改首页」**。site 是多页站,手册/开发指引是**按版本归档的整页**,必须每版新建一份并重指所有链接。封版清单必须**点名 manual/dev 正文**,不能只写"sites 新特性卡片"。
2. **链接残留是隐性 bug**:首页有 6 处 manual/dev 链接(顶栏×2、hero、文档板块×2、设计版本归档),漏一处就有用户进到旧版。发版用 grep 全量自查。
3. **手册要核当前代码,不能照搬**:旧手册会带已删除的工具/路由(如 `verify_task` v2.9.1 已删)。新版逐节核对代码,删旧补新。
4. **旧版页要"自降级"**:旧版 verswitch 标"当前"会误导。发版时把旧版页指向最新、自身改"历史"。
5. **截图要真实**:特性卡片用 run-real 截图(`sites/assets/vX.Y.Z/`),不用 mockup。

> 见 [[release-process]]:本规则是封版 step5「封版前文档更新」里 site 部分的细则。
