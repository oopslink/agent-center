# 验收方法论 (Acceptance Methodology)

> **所有 agent-center 发布验收 / feature 验收 / release-gate 工作必读。** 跟 [测试规约 `testing.md`](testing.md) 平级、互补：`testing.md` 管**测试代码**这条切面（单元 / 集成 / 覆盖率 / 测试计划报告 / 构建发布门），本文管**独立黑盒验收**这条切面（真实使用、真安装、真浏览器、证据归档、能力完备性）。

本文是 [`v271-retrospective.md` § 1](v271-retrospective.md) 那批 T-1..T-9 规则的**常驻方法论展开**——retro 正文记录的是某次发布的 WHY/起因，本文是后续每次发布都遵循的 HOW。

**文档分工（避免双真相）：**

| 文档 | 管什么 | 关系 |
|---|---|---|
| 本文 `acceptance-methodology.md` | 验收**方法论 / 原则**（怎么独立黑盒验收一个发布） | 你在这 |
| [`docs/release/acceptance-checklist.md`](../release/acceptance-checklist.md) | 某版本**逐域 WHAT + 验法 + 出口标准 + 真 install harness** | 本文给原则，它给逐域清单；不重复 |
| [`testing.md`](testing.md) | 测试**代码**规约（覆盖率 / 计划报告模板 / 三层 inventory / **构建发布门 §5** / **deployed-smoke §2.3**） | 本文 T-6 / deployed-smoke **指针引它，不复制** |
| [`conventions.md`](conventions.md) | id 分层 / ref-vs-id / schema-change 工作单位 | 验收里涉及 id/ref 形态时引它 |

> **原则铁律**：本文只写验收切面的**净新**内容；凡 `testing.md` / `acceptance-checklist.md` 已有的（构建门、deployed-smoke 计数、逐域 harness），本文**只引不抄**。

---

## § 0. 元根：验收是「真实使用」的测试，不是对代码的 parity 推理

v2.7 / v2.7.1 周期里几乎每一个逃逸到用户手里的缺陷（`#159` files 501 / `#161` AirPlay 占 :7000 / `#150` launchctl 旧 API / `#155`·`#160` 裸 ref / `#199` worker-run flag 集 / `#202`·`#203`·`#204` / `#211` default-prefix 发现 → `#212` / `#240` createDm 裸 ref / `#241` find_org_agent→assign_task 链）都落在同一条缝里：**代码或单测说"没问题"，但真安装 / 真浏览器 / 真端到端使用说不是**。

验收的职责就是站在这条缝上。下面每一节都是把"按方便测的方式测"替换成"按用户真实部署 + 使用的方式测"。

**自检（贯穿全文）：** 我是在 _推理_ 它对，还是在 _按用户的用法跑_ 它？by-parity / 读代码 / `--dry-run` / 共享 predicate 推理 **都不算验收证据**。

---

## § 1. T-1 真实使用路径（real-usage path）

验收跑在**真 `install` / `upgrade` 路径**上：真 launchd / systemd unit、真生成的 `config.yaml`（**不手搓 config**）、真浏览器、真目标环境、真 agent / worker 跑**用户从 Web Console 真实 copy 的那条命令**。

> 具体的真 install harness（隔离 prefix、验 config 真含 `blob_store`、避 :7000 AirPlay、注册拿 session）见 [`acceptance-checklist.md` 执行指引 → 通用 harness](../release/acceptance-checklist.md)。本文不复制，只强调下面几条**净新纪律**。

### § 1.1 真浏览器 UI 断言锚 rendered/computed 真值（来源：Tester2）

UI 断言锚定**产品 rendered / computed 真值**，不靠 class 名 / 属性 / 源码推断：

- **颜色 / 样式** → `getComputedStyle()` 真值（如 `rgb(239, 68, 68)`），不猜 class token。`#250` Reset 按钮红：class 实际是 `danger` 而非 `red`，猜 class 名会**假阴**。
- **受控输入值** → 读 `.value` **property**（via `evaluate`），不是 `getAttribute("value")`（属性只反映初值、不反映当前态）。
- **存在 / 行为** → rendered DOM 态 / 真实 navigation / HTTP 码 / 字节断言。
- class 名"暗示"某属性 = **假设，不是证据**；report 或 clear 之前都要核实真值（防假阳 / 假阴）。

**False-alarm rule-out（机制对 ≠ 实测过；单次实测红 ≠ 真 finding）**：一次红 / 失败的观测是**假设，不是结论**——传出去之前先**证伪**两类噪声，定位到**具体真因**再定性：

- **① harness error（我自己的命令/脚本/逻辑）**：shell 退出码陷阱（如 `find -exec grep -l X && echo WARN` 永远打印 WARN，因为是 *find* 的退出码不是 grep 的匹配）、错路径、错 endpoint 格式、stale selector。
- **② 环境噪声（无关的真实状态/活动）**：共享机上别的 live 进程、预置状态、随时间变的文件（日志、SQLite `-wal` sidecar）、并发活动。
- 这是「verify before claiming PASS」的镜像 —— 这里是 **verify before claiming FAIL**。两者同理：**绝不报一个自己没证实的状态**。
- **对称适用 UI sweep**：一条 "raw-id leak" 或 "console error" 同样可能是自己的 script bug / stale selector / 无关进程，先复现 + inspect 真因再传。UI 反例（与后端 D1/E4 对仗）：sweep 报 "raw-id leak" 实为 testid selector 命中了 **#192 content-豁免的 JSON-viewer 子节点**（见 [`ux-standards.md`](ux-standards.md) raw-ID 边界）；"console error" 实为浏览器扩展 / 无关 404，非被测页产生。
- 实例（#255 验收，见 `docs/release/evidence/v28-255-test-instance-acceptance-report.md`）：D1 整树 byte-hash "FAIL" 实为本机一个 **live prod 实例在写自己的 SQLite DB**（churn ≠ test 污染，靠"零 test 引用 / 文件数不变 / 结构内容字节稳定"证伪）；E4 "token in plist" 实为上面那条 `find -exec grep -l` 退出码 bug。两条若闻报必传 = 假安全警报误锁 PR。
- 警告：rule-out ≠ explain-away —— 必须查到**具体真因**（那个退出码 bug、那个持有 DB 的 PID），不是挥手"大概是噪声"。

> #192 零-raw-id 的 chrome / content / id-as-content 三层边界（验收 sweep 怎么扫）见 [`ux-standards.md`](ux-standards.md) 的 raw-ID 边界节，本文不复制。

### § 1.2 路径 / 发现 / 布局类，跑产品默认 prefix

涉及安装 prefix、`list-local-centers` 发现、文件布局的特性，必须装在**产品默认 prefix**（`~/.agent-center[.<instance>]`），不能用隔离 `/tmp`。`/tmp`-隔离会绕过"路径约定本身"的 bug——`#212`（`list-local-centers` 把 worker 装报成 center）就藏在 `/tmp` 后面。数据 / 内容类测试仍可用 `/tmp` 图清理方便。

### § 1.3 跑用户真实 copy 的那条命令，端到端

CLI / install / worker 路径的验收，必须**真跑用户从 install 输出 / UI 里 copy 的字面命令**，看它**往哪写**、**拒什么 flag**。`#202`·`#203`·`#204` 三个 release blocker 全藏在"我按 parity 读了共享 predicate"里，真跑 `agent-center worker run` 字面命令时一秒掉出来。诊断产品 spawn 的 CLI 时，**复制每一个 flag**（含 `--setting-sources ""` 这种空值），别简化。

**自检：** 我跑的是用户会 copy 的那条命令吗？还是我自己拼的简化版？

---

## § 2. T-3 验链路，再扫整类

- **验链路**：多工具 / 多步流程要把上一个的**输出喂进下一个的输入**，端到端串。`#241`（`find_org_agent` 返裸 id → `assign_task` 要 prefixed ref）就是只单测每个工具、没串 `find→assign` 链漏掉的。
- **扫整类**：一个 finding 暴露一个**类**时，枚举并测该类**每一个可达消费点**，不只补被报的那一个。v2.7.1 的 "裸 business-id 喂进 ADR-0033 ref-校验端点" 类（`#240` → `#241` → `#244`）= 一类；所有 createDm / invite / assign / member-add 端点 × 每个产 id 的 tool/UI 都要扫。

ref-vs-id 的边界判据（identity 要 `kind:` 前缀 / entity-id 裸用）见 [`conventions.md` § 12](conventions.md)。

**自检：** 这个 finding 是孤例还是一类？同类的其它消费点我都跑了吗？

---

## § 3. T-4 全产品整合走查跑在 owner 之前

上线前必须有一次**真 install + 真浏览器 + 全产品**的整合走查，发生在我们自己的周期里、在 owner 需要 dogfood 之前。v2.7 第一次真整合发生在 owner 的 E2 阶段（owner 比我们先撞到缺口）——这是**时序倒置**，是本周期所有 Tester 教训的元指向。

---

## § 4. T-5 证据即代码（evidence is code）—— verify-in-tree

验收证据（报告 + 截图）**必须提交进被 tag 的那个 commit**。在声明"证据 commit-aligned `<tag>`"之前，**真跑核实**：

```bash
git ls-tree -r <tag-or-commit> -- docs/release/evidence/<report>.md
git ls-tree -r <tag-or-commit> -- docs/release/evidence/<screenshots-dir>/
```

每一个证据文件（报告 + 每个截图目录 / INDEX）都要在输出里**真实在那棵 tree 里**。"在盘上" / "我刚写完" / "untracked 在 worktree" / "在 `git stash`" **都不算**。

> 真实事故（v2.7.1 ship）：被 tag 的 `bdc9818` 里**没有**验收报告和 29 张截图，五个角色的 ship 帖都 echo "commit-aligned" 却没人 `ls-tree`，根因是 untracked 证据被一次 working-dir clean 抹掉。教训：**证据要像代码一样被 commit**，untracked ≠ 契约绑定。

签字方（含 IntegrationDev 的 ship 帖）对"证据在 tree 里"负 `git ls-tree` 实证之责；其它角色的庆祝 ack 不能替代这次 verify。链路细节见 [`v2.7-delivery-process.md`](v2.7-delivery-process.md) 的 evidence-in-tree 节。

**自检：** 我说"证据对齐 commit"之前，真 `git ls-tree` 看到文件在那棵 tree 里了吗？

---

### § 4.1 每个验收点内嵌可视证据（inline evidence per acceptance point）

证据不能只在文字里**引用路径**（如"截图见 `screenshots/`"）—— **每一个功能模块 / 验收点都必须在报告里 inline 内嵌它的可视证据**，让审阅者在验收点旁边直接看到证据、不用去翻目录。

- **FE / UI 功能验收点** → inline 内嵌**真实例截图** `![模块·状态](screenshots/<name>.png)`。**both-mode 命门**（颜色 / 对比 / 主题相关的点：chips、badge、气泡、deleted-sender 降级等）附 **light + dark 两张**。
- **后端 / data-API 验收点** → inline 内嵌**可视证据 artifact**：关键 test PASS 终端截图、部署结构证据（如 `SELECT sql FROM sqlite_master WHERE name='idx_...'` 输出）、真 endpoint API 响应 shape + 查询数（坐实 N+1 常数级）等。
- 所有证据文件存 `docs/release/evidence/screenshots/`、**文件名与验收点小节对应**、真实例产出，并遵守 § 4 的 verify-in-tree（commit 进被 tag 的 commit + `git ls-tree` 实证）。
- 文字结论（"§3.3 GO"）**不能替代**可视证据——结论 + 内嵌证据二者都要有。

> 真实事故（v2.8.1 ship）：报告做完后 @oopslink 审 PDF 指出"验收报告里没有相关功能模块的截图，那个验收点应该有截图证据"——报告只在文字里引用了 `screenshots/` 路径、没把图嵌到验收点旁、也没覆盖每个功能模块。教训：**验收点 = 文字结论 + 内嵌可视证据**，光有结论不够。

**自检：** 报告里每个功能模块 / 验收点旁边，审阅者能直接看到它的内嵌截图 / 证据 artifact 吗（不是只看到一句 `screenshots/` 路径）？both-mode 的点有 light+dark 两张吗？

### § 4.2 走真实用户可达路径验，不只直接 URL（reachability）

验一个页面/功能时，**必须从真实用户入口导航过去**（点侧栏 nav / 链接 / 走真实 journey），**不能只敲组件的直接 URL**。直接 URL 验得了组件渲染对，但**验不出"用户实际点到的是不是这个页面"**——孤儿路由、导航指向另一个旧页面、未删的重复页，都会让一个**用户根本到不了的页面**通过验收。

- §3.3/run-real 验页面：从首页/侧栏**点导航过去**（模拟真实用户），不只 `goto(直接 URL)`。
- 验 **nav 入口 + 快捷键 + 面包屑 + 创建后跳转** 链到的**确实是被改的那个页面/组件**（catch 导航-页面错位、孤儿路由）。
- 一个功能有多个同类页面时（如两个 agents 页），确认改的 + 验的是**用户实际到达的那个**，且**没有未链接/可达的重复旧页**（搜全 nav/route 入口确认单一 canonical）。
- §4.1 的内嵌证据截图应来自**真实导航到达的页面**、附导航路径，不是直接 URL 开的孤儿页。

> 真实事故（2026-06-10 v2.8.1 ship 后 @oopslink catch）：agent 列表增强（#256）建在 `Agents.tsx`(路由 `/agents`)、§3.3 直接开 `/agents` URL 验过、报告截图也是它；**但 /agents 是孤儿路由（导航没链）**，导航「Agents」实际指向 `/members/agents`(旧 `MembersAgents.tsx`) → 用户点 Agents 看到的是旧页、增强版到不了。根因是 v2.7 一次导航整合只删了到 /agents 的链接、没删页面=遗留孤儿，增强又改在孤儿上。教训：**验"用户可达性"、不只"组件渲染"**。

**自检：** 我验的这个页面，是**点真实导航**到的、还是直接敲 URL 到的？用户点 nav/链接，到的是不是这个被改的页面？有没有未删的可达重复旧页？

### § 4.3 用户视角端到端走查 + 关键步骤截图（release 验收报告同样适用，且要可复现）

§4.1（内嵌截图）/ §4.2（真实可达路径）/ §0–§1（真实使用）合起来已要求"验收 = 真浏览器走用户路径 + 内嵌可视证据"。本节把它**显式收紧到 release-gate / PD 的发布验收报告本身**，堵住"跑了但报告纯文字"的缝：

- **每次发布验收必须产出**：(1) 从**用户视角端到端**走关键流程（真实例 + 真浏览器 + 真导航，**不许**直连 API / 直贴 URL）；(2) **关键步骤逐步截图**，每图配「步骤 / 期望 / 实测」，**内嵌进验收报告**。
- **适用范围含 PD 的 release 验收报告**，不只单 feature-PR 验收。一份没有关键步骤截图的发布验收报告 = **未通过**——无可视证据不算验收（与 §5.1 run-real 硬门同理）。
- **截图要可复现**：优先用**提交进仓库的 capture 脚本**（如 `tests/e2e/v2/capture-<ver>.mjs`：起真实例 → 用与 Web Console 同一套 `/api` 播种真实场景 → Playwright 驱动 SPA 逐步截图 → 输出 `docs/release/evidence/<ver>-screenshots/`）。脚本随证据一起 commit（§4 verify-in-tree），任何人可一键重跑复核。
- 证据三件套（报告 + 截图目录/INDEX + capture 脚本）按 §4 commit 进被 tag 的 commit，并 `git ls-tree` 实证在 tree 里。

> 真实事故（2026-06-14 v2.9.1）：PD 首版发布验收报告是**纯文字、零截图**，@oopslink 指出"质量不行，缺少截图，从用户角度重新执行验收程序"。根因：run-real / UI 证据没留存（无截图、无 trace、无 capture 脚本），报告只剩文字结论。教训：**用户视角端到端的关键步骤截图 + 可复现 capture 脚本，是发布验收的硬交付，不是可选附件。**

**自检：** 这份发布验收报告里，有没有从**用户视角端到端**走查的**关键步骤截图**（配 步骤/期望/实测）？这些截图能不能用**提交进仓库的脚本**一键复现？

---

## § 5. T-9 能力完备性 / CRUD-生命周期验收

> 这是 [`v271-retrospective.md` § 11](v271-retrospective.md) 产品面 "CRUD enumeration before ship" 的**验收侧对偶**：PM 在设计期 enumerate，Tester 在验收期 catch。

**验收必须核"该有的齐不齐"，不只测"已有的能不能用"。** 对每个被管理实体（Agent / Worker / Member / Channel / DM / Project / Issue / Task …），验收程序里跑一遍完备性核查；**缺失的基本操作本身就是一条 finding**——写进验收报告，不静默放过：

- [ ] **Create** — UI + API 入口有。
- [ ] **Read** — 列表 + 详情 +（适用时）跨 org / 跨 project 搜索。
- [ ] **Update** — rename / 改元数据 / move —— 有，或显式 deferred（release note 标明哪版补）。
- [ ] **Delete** — 含 cascade 影响（删了它在会话 / task 里的引用怎么办）—— 有，或显式 deferred + 理由。
- [ ] **生命周期 / 状态流** — start / stop / archive / reset / suspend，**入口对称**：能进某状态就要能出（或锁定是刻意的产品决策）。
- [ ] **入口对称性** — 有进就有出：Members 能 Add Agent 就要能 Remove；详情页能加成员就要能删成员。
- [ ] **跨 BC 关联** — 实体在另一 BC 里参与（assignee / participant / owner）时，两个 context 的管理面都要核。

v2.7 / v2.7.1 实例：agent 建好无 delete（`#197` 才补）、DM 无 dedup（`#215` 才补）、project member 无 add/remove UI（`#207` 才补）、`find_org_channel` 缺（`#246` 才补）——这些都该在 release-1 验收时就 surface，而不是 dogfood 撞出来。

边界：**做不做某个缺失操作是 PM 的优先级裁决**；Tester 只负责让这个缺口在验收期**被看见**，使取舍是显式的而非隐式漏掉。

**自检：** 这个实体的基本管理面（增删改查 + 生命周期 + 入口对称）我是按"用户合理预期"核了完整性，还是只测了已实现的那几个操作？

---

## § 6. T-7 用例独立性

验收用例由 **Tester（出口标准）+ PD（意图）** 从 spec / 产品意图设计；**Dev 零设计参与**——Dev 事后暴露 finding 并修，但不为自己的实现共同设计 pass/fail 标准。这条是让 Tester 的 "unit-green / real-broken" 直觉保持诚实的前提。

---

## § 7. T-8 报告用统一语言（ubiquitous language）

验收报告对产品 owner 说**域 / 模块 / 功能 / 做了什么**的人话（"DM 详情头现在显 `@<对端>` 而不是 'Direct message'"），不是实现简写（`participants[*] != self`）。代码坐标、PR / commit 引用放开发者向的 thread，不进报告正文。

> 测试计划 / 测试报告的**模板**（计划-报告 1:1 编号对齐、三层 inventory）见 [`testing.md` § 2](testing.md)，本文不复制模板，只加这条"对 owner 用统一语言"的语域要求。

---

## § 8. 构建发布门 + deployed-smoke（指针，不复制）

- **构建发布门（T-6）**：任何 ship / release / 关键 PR 验收前必跑 `make lint`（含 `lint-spa-tsc` 即前端 `tsc -b`）+ 一次 `make build`（或 `make release` 干跑），任一红 = 阻断。**vitest 全绿不算前端可发布证据。** 细则见 [`testing.md`](testing.md) § 5。
- **Deployed-smoke ≥ 1 是硬发布门**：deployed-binary smoke 计数 = 0 的 phase / release 不许 close；尤其**跨版本升级真跑**（真 v_prev → v_new 安装、迁移在真旧数据上真跑、在标准目标环境上）至少 1 次。三层 inventory 与计数标准见 [`testing.md`](testing.md) § 2.3。

> 这两条已是 `testing.md` 的硬约束；本文只把它们登记为验收程序的**入口门**并引过去，不另立标准。

---

## § 9. 操作附录：共用实例双面验模板（piggyback）

v2.7.1 多轮并行验收用的协议，固化下来：

- **端口**：web `:7101` / server `:7051` / admin `:7301`（**避 :7000**——macOS AirPlay 占用）。
- **prefix**：数据 / 内容类用 `/tmp/<round>`；路径 / 发现 / 布局类用产品默认 prefix（见 § 1.2）。
- **announce 协议**：开跑前在 **`#agent-center` 主频道**（不是 thread，方便 Dev/Dev2 看见避端口冲突）announce 实例 + 端口 + 预计时长；跑完 announce release。
- **双面验**：Tester 起 backend / 数据真实例，Tester2 蹭同一实例验 UI/UX——真 install + 真 agent 预算砍半、两面证据交叉。
- **服务态收尾**：launchd / systemd 服务测完 `bootout` + 删 unit/plist，不污染机器；不 `rm` 不是自己建的 `~/.agent-center`。

---

## § 10. 自检清单（验收签字前）

- [ ] 跑在真 `install` / `upgrade` 生成的 config 上，不是手搓 config（§ 1）
- [ ] 真浏览器断言锚 computed/rendered 真值，不靠 class 名猜（§ 1.1）
- [ ] 每条红 / 失败观测都 rule out 了 harness error + 环境噪声、定位到具体真因再定性，没传未证实的假警报（§ 1.1 false-alarm rule-out）
- [ ] 路径 / 发现 / 布局类跑了产品默认 prefix（§ 1.2）
- [ ] CLI / worker 跑的是用户真实 copy 的字面命令（§ 1.3）
- [ ] 多工具流程串了真实链路；finding 暴露的类扫全了（§ 2）
- [ ] 上线前有一次全产品整合走查跑在 owner 之前（§ 3）
- [ ] 证据已 commit 进被 tag 的 commit，并 `git ls-tree` 实证在 tree 里（§ 4）
- [ ] 每个功能模块 / 验收点旁边 **inline 内嵌**了可视证据（FE=真实例截图、后端=test/SQL/API artifact），both-mode 点附 light+dark，非只文字引用路径（§ 4.1）
- [ ] 验的页面是**点真实导航 / 链接**到达的（非只直接 URL），nav 入口/快捷键/面包屑链到的确实是被改的页、无未删的可达重复旧页（§ 4.2 reachability）
- [ ] 发布验收报告含从**用户视角端到端**走查的**关键步骤截图**（配 步骤/期望/实测），且可用**提交进仓库的 capture 脚本**复现（§ 4.3）
- [ ] 每个被管理实体核了 CRUD + 生命周期 + 入口对称完备性，缺失基本操作作为 finding 报出（§ 5）
- [ ] 用例由 Tester+PD 设计、Dev 零设计参与（§ 6）
- [ ] 报告用统一语言、对 owner 说人话（§ 7）
- [ ] `make lint` + `make build` 绿；deployed-smoke ≥ 1（含跨版本升级真跑）（§ 8，引 testing.md）

任何一项 ❌ → 不签字。

---

## § 11. 发布流程（release flow）—— PD 全程负责（owner 2026-06-15/16 固化，v2.10.2 起）

整条「验收 → ship」由 **PD 一个人跑完，IntegrationDev 不参与 ship**。固定五步：

1. **PD 拆分验收任务 + 制定验收计划**：综合本周期全部任务，按**功能模块且逐个开发任务**拆成验收任务（验收项数 = 任务数，不可少），组成专项验收 plan（DAG）分发 Tester。
2. **Tester 完成验收任务 + 证据提交到仓库**：每个开发任务 run-real 核验，贴**修复后 AFTER 效果图**（不接受 before/问题图）；证据（AFTER 截图 + `ACCEPTANCE-*.md`）**commit 到仓库** `docs/design/<version>/`（命名见 [`docs/design/ACCEPTANCE-EVIDENCE-SPEC.md`]），遵守 § 4 verify-in-tree。
3. **PD 综合出验收报告**：逐任务 pass/fail + 内嵌证据 + 总体结论。**判定铁律**：PD **绝不以节点 `completed` / plan `has_failed` 推断 PASS**——必须核每个任务的**真实 verdict + 证据**（PD 读不到他人任务内容时，从仓库分支 `git archive <branch> docs/design/<ver>` 取已 commit 的证据）；Tester 发现 fail，对应任务/模块节点**不得判绿**，须判 fail 或开回退任务。
4. **Owner 同意验收**。
5. **PD 独立完成 ship 全流程**（顺序）：① 更新 **README + sites**；② **打包 / build**（§ 8 构建门：`make lint` 含 `lint-spa-tsc` + `make build` 绿）；③ **合并到 `main`**（在**干净的临时 worktree**里合，**不碰**带未提交 WIP 的部署 worktree；非 FF 时正常 merge，verify 无冲突 + 构建绿再推）；④ **打 tag `<version>`** 并推送；⑤ **清理所有开发分支**（远端 + 本地 `dev/<ver>-*`、`tester/<ver>-*` + 其 worktree，**保留 tag**），列出已删分支清单。

> 教训来源（v2.10.2，两度误报）：PD 首版凭"模块节点 completed"报"全绿"漏掉 M4 Open-DM fail；二版验收 PDF 用了 before 图被 owner 当场 catch。根因＝**凭节点状态推断 + 拿不到真实 AFTER 证据**。修正即上面第 2–3 步：证据 commit 进仓库 + PD 核真实 verdict。

**§ 11 自检：** 验收项是否逐任务（数量=任务数）？每项有 AFTER 效果图且已 commit 仓库？报告是核真实 verdict 还是凭节点状态？ship 五步（README+sites / 打包 / 合 main / tag / 清分支）是否 PD 独立跑完、没拉 IntegrationDev？

---

## § 12. Doc 目录归档（doc archiving）—— owner 2026-06-16 固化

所有 doc / 设计 / 验收产物统一落 **`docs/design/<version>/`**，按类型分子目录，**禁止往扁平根目录（如旧 `report/`）堆**：

```
docs/design/<version>/
├── mockups/        # 设计稿 html + png
├── evidence/       # 验收 AFTER 截图（§ 4 verify-in-tree、§ 11 第 2 步的落点）
├── acceptance/     # 验收报告（只留 -final）
└── ACCEPTANCE.md   # 逐任务验收清单
docs/_scripts/      # 报告 / 截图生成脚本（gen_*.py 等）
docs/_inbox/        # bug 复现 / 未定版临时截图，修复合并后即清空
```

**三条铁律：**

1. **版本目录化**：文件必带版本前缀或落到对应 `docs/design/<version>/`，不在仓库根或单一扁平目录混堆跨版本/跨类型文件。
2. **只留 final**：同一验收报告的草稿 / `-v2` 等被取代版本，**ship 后删除**，仅保留 `-final`（+ `ACCEPTANCE.md` 清单）。与 § 11 第 5 步「清开发分支」同属 ship 收尾。
3. **临时件进 _inbox**：bug 复现图 / 未定版调整截图先进 `docs/_inbox/`，对应修复合并后**即清空**，不长期留存；确属某版本证据的，归该版本 `evidence/`。

> 背景（2026-06-16）：旧 `report/` 扁平堆 70+ 文件、跨 5 个版本、混 mockup / 截图 / 报告，单 v2.10.0 一版存 3 套重复报告 PDF——无法检索且占空间。本节把「证据 commit 进 `docs/design/<ver>/`」（§ 4 / § 11 第 2 步）从「验收证据」扩到**全部 doc 产物的归档规范**。

**§ 12 自检：** 文件是否落在 `docs/design/<version>/` 对应子目录、没在根目录扁平堆？验收报告是否只留 -final、草稿已删？`docs/_inbox/` 是否在修复合并后清空了？
