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

**实例化到 project** = 按角色配比建 N 个新 agent（新身份）+ seed 它们 memory（那套可泛化经验）+ 绑 workflow 模版 → 直接能跑 Dev→Review→Ship。

---

## 7. 分期

- **Phase 1（MVP）**：Team 实体 + 成员/关联（sqlite）+ **team-memory center-hosted git（方案 A）** + 渐进式加载（复用）+ Team 模版（3 路径）+ 实例化到 project + role→agent 派活解析（§8）。
- **Phase 2**：**agent memory 迁移到 center-hosted git**（比只做 team memory 大、动到跑着的东西，故拆后）。
- **将来**：一个 team 服务多个 project；跨 org 共享（export/import 已覆盖，见 §6）。

---

## 8. 待定（open）

- **role→agent 派活解析**：plan 里"Review→team 的 dev"、"Ship→PD"怎么落到具体 agent —— 需 team 提供一张 role→agent 映射给 plan authoring 用。这条无论如何都要做，具体设计待定（本文档待补）。
- **team-memory 并发写一致性**：多个成员同时往 team repo push 的冲突处理（git merge / 按条目版本 / last-write），细节待定。
- **agent memory 迁移路径**（Phase 2）：从 worker 本地 repo 迁到 center-hosted 的具体步骤 + 存量迁移。
- **与现有"直接 project 成员"并存/迁移**：现在 agent 直接是 project 成员、角色靠约定；引入 team 后 = agent 属于 team、team 参与 project，存量项目怎么迁入 team。
