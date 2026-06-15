# 发版 / 封版流程 · Release & Freeze Process

适用**所有版本**(owner 2026-06-15 立)。这是从「验收绿」到「线上可用」的完整 ship 流水线。配套:验收证据见 [`ACCEPTANCE-EVIDENCE-SPEC.md`](./ACCEPTANCE-EVIDENCE-SPEC.md)。

## 流水线(逐步)

1. **开发 → 集成**
   dev off 版本分支(`vX.Y.Z`)declare done → **PD §-1**(独立复跑 `make build` / `make lint` / `make test` + `npm test`,verify-not-trust,隔离 worktree)→ **IntegrationDev 任务化**合进版本分支(每笔合并 = 一个 merge task,不靠频道 @)→ PD §-1 集成树。

2. **验收**
   Tester1 §2(data/API + **授权红线硬门**)+ Tester2 §3 run-real(端到端用户旅程,证据以附件 @PD)按版本 `ACCEPTANCE.md` 逐条签。授权红线必过。

3. **报告 / 签字**
   PD 汇总 §6 签字表 + **内嵌全部端到端关键路径截图**的验收报告(PDF)→ 交 owner。

4. **owner 拍板 ship。**

5. ⚠️ **封版前文档更新(本步必做,owner 2026-06-15 立规则;曾漏)**
   合并进 main / 打 tag **之前**,必须更新:
   - `README.md` + `README.zh-CN.md` —— 顶部 `[!IMPORTANT]` 加本版「shipped / 已发布」blurb(版本号 + 日期 + headline 特性),EN/中**同步**。
   - `CHANGELOG.md` —— 在 `[Unreleased]` 下新建 `## [vX.Y.Z] — YYYY-MM-DD` 段(Added / Changed / Fixed)。
   - `sites/`(site) —— `sites/index.html` 加本版「新特性」卡片段(**用真实截图**,放 `sites/assets/vX.Y.Z/`)+ 更新路线图卡片的「已发布」行。
   这批 commit 进**版本分支**(随 step 6 一起合 main)。

6. **合并 + tag**
   IntegrationDev 任务化把版本分支**合进 `main` + 打 tag `vX.Y.Z`**(同 v2.9.2:Merge→main + annotated tag)。

7. **打包**
   `make release` → 版本 tarball `agent-center-vX.Y.Z-<os>-<arch>.tgz`(含 install / upgrade / uninstall 入口;详见 `docs/release`)。

8. **部署 / promote**
   用 tarball 的 `upgrade` 路径升级运行实例(`docs/deployment`;v2.9.1 起 owner 自行部署,或 PD 代跑)。

## 封版前检查清单(step 5 门)

- [ ] README.md 顶部加本版 blurb(版本号 / 日期 / headline)
- [ ] README.zh-CN.md 同步
- [ ] CHANGELOG.md 新增本版段(Added/Changed/Fixed)
- [ ] sites/index.html 新特性卡片段(真实截图 → `sites/assets/vX.Y.Z/`)
- [ ] sites 路线图卡片「已发布」行更新
- [ ] §-1 全绿 + 验收报告(全嵌截图)已交 owner

> 教训(2026-06-15):ship 流程曾漏「封版前更新 README + site」,导致发版后文档/官网仍停在上一版。本规范把它列为 step 5 硬门 —— **文档/官网更新先于合 main+tag**。
