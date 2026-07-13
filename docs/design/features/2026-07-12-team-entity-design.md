# Team 一等实体 — 技术设计

- Issue: `issue-c59e4598`
- 状态: 设计（与 oopslink 讨论敲定，逐点见下；仍有一处 open）
- 关联: `issue-7cc29084`（团队模版，本设计 subsumes/升级它）；与 coderepo 特性正交（coderepo=代码从哪来；team=谁带什么经验来做）

---

## 1. 背景与需求

**问题**：目前只有 agent-center 这一个项目有"能干活的团队"——角色分工（PD / N×dev / integration / tester）+ Dev→Review→Ship 流程 + 积累的经验（memory / skills / rules）。新项目接进来是空的：没团队、没经验、没流程，基本空转。

**两个核心需求（oopslink 定）**：
1. **快速创建团队**：一键起一支带角色配比的团队。
2. **积累并复制团队经验**：把当前团队积累的经验（流程 / 技能 / 原则 / 知识）快速复制到其他项目。

**范围**：
- **MVP** = 建团队 + 复制经验。
- **将来**（先不做）：一个 team 同时服务多个项目；跨 org 共享（= 模版导入导出的自然结果，见 §6）。

---

## 2. 核心概念

- **Team = org 级一等实体**，承载三样：**成员配置**（角色的 cli/model/capability_tags/并发）、**工作规约**（流程 / 编码规范 / 裁决惯例 / 清理规则）、**共享经验/技能**（memory / skills / rules）。共享经验/技能是 team 存在的核心目的。
- **成员**：agent **独占一个 team**（一个 agent 只属于一个 team，保证团队经验边界干净）；human **可属多个 team**。
- **Team 参与 project**：team 去某个项目干活。MVP 先"一个 team 服务一个 project"，模型允许将来一 team 多 project。
- 一句话：**team = 谁带什么经验来干；project = 在哪干、干什么**。

---

## 3. 经验的 scope 模型

**消费顺序**（一个 agent 干活时查经验/技能，越具体越优先）：

```
agent 自己  →  team（共享）  →  project  →  global
```

- **agent 自己**：这个 agent 自己攒的。
- **team（共享）**：团队共享的经验/技能，所有成员往里贡献、都能读 —— 核心。
- **project**：项目专属事实（这个 repo 的结构、内部代号、某个具体 bug）。
- **global**：平台级。

**team 与 project 是两条正交轴**，不是一条线性链：team 经验**跟着团队走**（团队去哪个项目都带着）；project 事实**留在项目**（哪个团队来都是这些）。撞车时按"对当前上下文更专属者赢"（项目具体事实盖团队通用规约；团队规约在项目没特别指定处生效）。

**积累纪律**（这条决定"共享"和"抽模版"都干净）：团队干活时——**通用教训写 team scope**（共享、可抽模版）、**项目专属写 project scope**、**只跟这个 agent 的写 agent-self**。写对 scope，共享才准、抽取才干净。

---

## 4. 存储：center-hosted git + sqlite

统一模型：**center 托管一个 git 系统，agent memory 和 team memory 都是里面的 repo，runtime checkout/push**。经验文件本就是文件（markdown+frontmatter、SKILL.md、rules），用 git 存比 sqlite blob 干净（版本/历史/diff/merge 免费），且跟现有 per-agent memory 的 git 模型一致。

**两分**：

- **结构化实体数据** → **center sqlite**（新表 + migration）：`teams`(id/org/name)、`team_members`(team/member_ref/role；agent 独占约束、human 可多)、`team_projects`(team↔project 关联)、角色配置。关系型、要查询。
- **经验文件**（team memory / skills / rules，以及 agent memory）→ **center-hosted git repos**：每个 agent 一个 repo、每个 team 一个 repo。

### 4.1 durability 收益

现在 agent memory 是 **worker 本地** git repo（`…/agents/<id>/memory`）——worker 挂了/迁移，memory 就在那台盘上、很脆。搬到 center-hosted 后，memory **跟着 agent 走、不跟 worker 走**，worker 死了也不丢。team 共享也白拿：team memory 就是"center 上另一个共享 repo"，所有成员 clone/push，跟 agent memory 一模一样的 checkout/push，不用单独的同步机制。

### 4.2 git host 选型：bare repo + git smart-HTTP（方案 A）

选型标准（按重要性）：① 鉴权能复用 center 的 per-agent token（HTTP 天然能按 repo 路径门；SSH 要另搞 key 管理+自己的 auth）② 新组件越少越好 ③ 别引入一个平台（UI/issue/CI/自带 DB）。

**结论：A**：
- center 每个 agent/team 建一个 **bare repo**（`git init --bare`）在自己盘上。
- 用 git 自带的 **`git-http-backend`**（smart-HTTP 协议）暴露成 HTTP endpoint，**用 center 现有 bearer-token 鉴权按 repo 路径门着**（一个 runtime 只能 clone 自己的 agent repo + 所属 team repo）。或一层薄 Go handler 包 `git-upload-pack`/`git-receive-pack`。
- 成员 `git clone/pull/push https://center/…/<repo>.git`，带 per-agent token。
- **复用一切**：git 二进制（已是依赖）+ center HTTP + 鉴权。不用新 daemon、不用 SSH key 管理、不引平台。

淘汰的：**soft-serve**（SSH 原生、每 runtime 管 SSH key + 自己的 auth，跟标准①冲突 + 多进程）；**gitea**（全平台，标准③否）；**go-git**（纯 Go in-process 实现 smart-HTTP，比 A 多代码 + server 端不如 git 二进制久经考验，留作将来去 shell 依赖的升级项）。

### 4.3 读写路径

- **读（快）**：runtime boot/materialize 时 clone/pull 自己的 agent repo + 所属 team repo → 本地工作副本；读 memory 走本地副本（无每次查经验的网络往返）。
- **写**：本地 commit + **push 回 center**（center 是真源）；team-scoped 写 push 到 team repo，别的成员下次 pull 同步下来。
- **一致性**：center 权威、成员**最终一致**（boot + 周期 + 自己写完各 pull 一次）。经验多是追加型、冲突少；真撞了靠 git merge / 按条目版本 / last-write 收（细节见 §8）。

---

## 5. 渐进式加载

复用现有 memory 系统的"索引常驻 + 按需召回"，team memory 套同一套：

- **索引常驻**：每条一行（slug + 一句话描述/钩子），像现在的 `MEMORY.md`。同步下来后每次 session 加载进成员 context。不管 team memory 攒多大，常驻的只是这份轻量索引。
- **完整条目按需召回**：完整正文只在相关时才加载（拿当前任务跟索引描述做相关性/tag 匹配，命中的才拉进 context）。
- **技能懒加载**：skills（SKILL.md）本就懒——索引里只有描述，正文触发时才载。
- **预算封顶**：每轮按相关性取 top-N，别撑爆 context。

git 负责"存储+同步（checkout/push+版本）"，索引+召回负责"消费（进 context）"，两者叠加：checkout 下来的 repo 里有 `MEMORY.md` 索引（常驻）+ 条目文件（按需载）。

---

## 6. Team 模版

**Team 模版 = org 级用户自管 artifact**，三条创建/管理路径都是一等能力：

- **`extract_from_team`**：从活 team 快照 → 草稿（只抽 team/全局 scope 的**可泛化层**，剥掉项目专属；用户再 curate 保证干净）。
- **`create` / `update` / `delete` / `list`**：手动建/编辑。
- **`import` / `export`**（JSON 文件）：备份、跨环境搬，**也天然是"跨 org 共享"的机制**（export 一个文件 → 别的 org import，不用另做 sync/授权管线）。

**可泛化 vs 项目专属**靠 §3 的 scope 边界切（team/全局 scope=可泛化、可带走；project scope=专属、必剥）+ 手动 curate；**不做自动脱敏管线**（MVP）。

**内容三块**：① 角色配比 + 每角色配置（cli/model/tags/并发）② 工作规约（引用一个 workflow 模版 + 编码规范/裁决惯例/清理规则）③ 可泛化经验（skills/rules/原则）。

**实例化（project-independent，issue-c4dccae0）** = 按角色配比建 N 个新 agent（新身份）+ seed 它们 memory（那套可泛化经验）+ 绑 workflow 模版。团队是 **org 级实体、与 project 无关**；要把团队投到某个/某些 project 上干活是**独立的一步 associate_project**（§3 "team 与 project 是两条正交轴"）。这样一个 team 天然可服务多个 project、也可先建后关联。

---

## 7. role→agent 映射（角色 → 具体 agent）

**前提（oopslink 定）**：**先有 team，plan 是照着 项目 + team 现状 制定的；team 换了就重新制定 plan。** plan **不是**"团队可移植"的产物——所以 role→agent **不是运行期晚绑定引擎**，而是 **build plan 时（author-time）照 team 名册派**。

- **team = 权威名册**：实例化在项目上的 team 提供 `role → [agents]`（PD 是谁、devs 是哪几个、integration 是谁、tester 是谁）。
- **建 plan 时的角色便利（author-time helper）**：作者给节点指一个**角色**（+ 可选约束），工具对着**当前 team 名册**解析成具体 agent：
  - 角色 1 个成员（PD / integration）→ 就他；
  - 角色多个成员（dev / tester）→ 按策略挑（默认**最闲的** / 轮转；capability_tags 匹配留将来）；
  - **约束：交叉评审 `Review ≠ Dev 的 agent`** → 解析时避开指定节点（Dev）解析到的 agent。
- **存的是具体 agent**：解析出的具体 agent 写进 plan 节点（author-time 绑定）——plan 最终跟现在一样是具体 agent，只是建的时候角色驱动（省得 ad-hoc 记 dev1-5）。
- **team 换了 → 重新建 plan**（helper 对新名册再跑一遍）。

一句话：**team 提供名册 + 建 plan 时角色便利填具体 agent + 存具体 agent + team 换重建**。

---

## 8. 分期

- **Phase 1（MVP）**：Team 实体 + 成员/关联（sqlite）+ **team-memory center-hosted git（方案 A）** + 渐进式加载（复用）+ Team 模版（3 路径）+ 实例化（project-independent，见 §6/issue-c4dccae0；project 关联走独立 associate_project）+ role→agent 角色便利（§7）。
- **Phase 2**：**agent memory 迁移到 center-hosted git**（比只做 team memory 大、动到跑着的东西，故拆后）。
- **将来**：一个 team 服务多个 project；跨 org 共享（export/import 已覆盖，见 §6）。

---

## 9. 设计加固（3 轮自审）

对 §1–8 做了 3 轮对抗式自审，实质补充如下（细化的定案，非 open）：

**并发写（细化 §4.3）**：5+ agent 同时往一个 team repo push 会频繁非-FF 冲突。解法：**每条经验一个文件**（slug/uuid 命名），并发写碰不同文件 → git 自动 merge；唯一共享的 `MEMORY.md` 索引**从条目文件派生（regenerate）、不手编**（或走 center 串行化更新）。push 前 pull-rebase-retry 兜边角。

**agent memory 可用性（细化 §4.1，Phase 2）**：center-hosted 若每次 boot 全 clone → boot 网络依赖、center 挂了 agent 没记忆。解法：runtime 留**本地工作副本**（跨 boot 持久化），boot 走增量 `git fetch/pull`；center 够不着 → **回落上次本地副本**（降级但能跑）。是"本地缓存 + 可达时同步"。

**角色 team 自定义（细化 §2/§7）**：角色**不硬编码** {PD/dev/integration/tester}——由 **team 模版自定义声明**（角色名 + 配比 + 每角色配置）；plan 节点引用 team 声明的角色名。固定枚举只是 agent-center 这个 team 的角色、非系统约束。

**实例化 ≠ 能跑的团队（细化 §6）**：建 N 个新 agent 还需 **runtime 家 + auth**（codex/claude login、MCP token）。模版带**配置**、不带 runtime/auth（per-deployment）。所以实例化=建身份+配置+memory-repo+绑 workflow；**runtime provisioning（派到 worker、装 auth）是单独一步**（复用现有 enroll/worker-provision 流）。

**抽取 curation 强制（细化 §6）**：scope 过滤不保证干净（team-scope 教训也可能提具体 repo/代号）→ **手动 curation 是 load-bearing、export/cross-org 强制**（抽取产草稿、过审再成可共享模版），防泄漏；加 **scrub 辅助**（高亮疑似专属 token：repo 名、"T950"类代号、路径）。

**访问控制映射（细化 §4.2）**：center 维护 agent→team 映射，git-http 中间件判"这 token 的 agent 属不属 repo 所属 team"→rw；全局 repo 全员可读；human 多 team；实例化时给新 agent 授权其 team repo。

**模版版本不 retro（细化 §6）**：模版=快照、实例独立（Q1 无 live link）、**不 retro-update**；要新经验就重抽 v2 或手动 import 特定条目。

**team scope 写靠 agent 判断（软肋，记录）**：通用 vs 专属靠 agent prompted 判断（同现有 memory 纪律），判错会污染 team scope；靠模版抽取的手动 curation + 可选周期性卫生复审兜。

---

## 10. 待定（真 open）

- **team-memory 并发写的实现细节**：每条一文件 + 索引派生 的具体落地（索引 regenerate 时机、pull-rebase-retry 上限）。
- **与现有"直接 project 成员"并存/迁移**：存量 agent（直接 project 成员）怎么迁入 team；过渡期 team-plan 与 direct-agent-plan 并存（override 兜后向兼容）。
- **team scope 写的纪律强化**：是否要机制（而非只靠 prompt）辅助 agent 判 scope。
