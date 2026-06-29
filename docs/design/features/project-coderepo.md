# Project CodeRepo — 技术方案

> issue-577a7b0e 的姊妹特性 · 来源 issue **I51 / issue-f980c8de**
> 范围：**多仓配置 + agent 标准仓库信息接口 + 用户查看 remote(commits/branches)**。
> **不含**：工作区自动 provision、git 生命周期自动化（建分支/Integrate/Ship/PR）——那些属团队模版（issue-7cc29084）/其他特性。
> 配套 mockup：`docs/design/assets/project-coderepo-mockup.html`

## 1. 背景与现状

`pm.CodeRepoRef`（`internal/projectmanager/code_repo_ref.go`）今天已是「项目 ↔ (url, label) 的轻量引用，本就可挂多个」，代码注释明确「a lightweight reference, NOT a VCS integration」。

当前**唯一**实际用途：Integrate 节点完成时的 **merge-check 祖先校验**（`assign_flow.go` `primaryRepoURL` → `MergeChecker`：`git fetch` + ancestry；项目无 CodeRepoRef → 自动跳过，T330）。

缺口（即本特性要补的）：
1. repo 不是结构化「一等公民」——缺 default_branch / provider / primary 标记、缺 update/delete/set-primary。
2. **agent 没有标准接口**拿到「项目代码在哪」——现在 dev/executor 靠**约定本地路径**（`~/works/codes/...`）在跑。
3. 用户**看不到 remote**（commits/branches）。
4. 私有仓**没有凭据管理**（隐式靠机器 SSH key）。

## 2. 目标（本特性边界）

- A. 多仓配置 CRUD：每项目 N 个仓，结构化字段 + 主仓。
- B. agent 标准仓库信息接口（MCP 工具）。
- C. 用户只读查看 remote 的 commits / branches。
- D. 只读凭据管理（供 B 的 live 查询 + C 的 viewing）。
- **非目标**：clone/worktree provision、自动 merge/tag/push/PR、webhook（初版）。

## 3. 数据模型

扩展 `CodeRepoRef` 为结构化仓库实体（同表加列，迁移 0086+）：

| 字段 | 说明 |
|---|---|
| `id` / `project_id` | 既有 |
| `label` | 既有，显示名 |
| `description` | **新**：一句话仓库用途简介，让 agent **不 checkout 即可了解该仓功能**（oopslink 要求）。由 §4 agent 接口与 viewing/列表返回展示 |
| `url` | 既有，clone/remote URL |
| `provider` | **新**：`github` / `gitlab` / `git`（generic）。决定 viewing/agent-live 走哪个适配器；空/未知 → `git` 回退 |
| `default_branch` | **新**：默认分支（viewing 默认展示、agent 取值）。空 → 运行时探测或留空 |
| `is_primary` | **新**：项目主仓（唯一）。merge-check 用主仓；agent get_repo_info 默认返回主仓 |
| `credential_ref` | **新**：指向只读凭据（见 §6），nullable（公开仓不需要） |
| `added_by` / `created_at` | 既有 |

约束：每项目 `is_primary` 至多一个（set-primary 时清旧）。`url` 非空。`provider` 枚举校验。

## 4. Agent 标准仓库信息接口（MCP）

新增两个**只读** agent 工具，作为 agent 知道「代码在哪」的标准入口，替掉约定本地路径：

- `list_project_repos(project_id)` → `[{label,description,url,provider,default_branch,is_primary}]`（**静态配置，便宜、无需凭据**）。
- `get_repo_info(project_id, repo_id?|primary, live?)` →
  - 默认（`live=false`）：单仓静态配置。
  - `live=true`：附带 remote 最近 commits（sha/msg/author/time）+ branches（**需凭据 + 远端调用**，见 §5/§6）。

口径：默认静态、live 可选——避免每次都打远端。返回 schema 与 §3 字段一致，FE 与 agent 共用同一形状。其中 `description`（§3 新增）是关键：agent 调 `list_project_repos` 即可**不 checkout** 判断「该仓是干什么的、该去哪个仓找代码」。

## 5. 用户查看 remote（commits / branches）

**provider 抽象**（`internal/coderepo/provider`）：接口 `ListCommits(ctx, repo, branch, limit)` / `ListBranches(ctx, repo)`。

| provider | 实现 | 说明 |
|---|---|---|
| `github` | **go-github**（REST） | 富数据（author/avatar/链接/分页），免 clone。v1 首选（覆盖我们自己的仓） |
| `gitlab` | go-gitlab | 后续按需 |
| `git`（generic / 未知） | **`git ls-remote`**（branches）+ 轻量 fetch/log（commits） | provider 无关回退，无富链接 |

**推荐 v1**：先实现 `github`（go-github）+ `git` 回退两个适配器，接口预留多 provider。**不 clone**——viewing 与 agent-live 都走 API/ls-remote。

缓存：初版可不做（实时查 + 短 TTL 内存缓存防抖即可）；webhook 刷新留作后续。

## 6. 凭据（只读）

- **作用域**：建议**项目级**只读凭据（一个 repo 一个 `credential_ref` 或项目共享一个），覆盖私有仓的 viewing/agent-live。
- **类型**：GitHub PAT / fine-grained token（只读 contents）或 GitHub App installation token（更可扩展，多仓鉴权最优）；generic git 用 deploy token / basic。
- **存储**：加密落地（复用 center settings/secrets，**加密列**或独立 secrets 表），绝不明文返回 API（mask 显示 `••••`）。
- **作用**：只读。本特性不写 remote。

## 7. API 面

- 项目设置：`GET/POST/PATCH/DELETE /api/orgs/{slug}/projects/{id}/code-repos`（CRUD + set-primary）。
- 凭据：随 repo 写入（mask 读出）。
- viewing：`GET .../code-repos/{repo_id}/commits?branch=&limit=` / `.../branches`（经 §5 provider 适配器）。
- agent MCP：`list_project_repos` / `get_repo_info`（§4）。
- 项目 GET 投影补 code-repos 概要。

## 8. FE（见 mockup）

- 项目设置 → **Code repositories**：仓列表（provider badge / default_branch / primary★ / Edit / Delete / Add）+ Add/Edit 表单（label/**description**/provider/url/default_branch/凭据 mask）；列表每行展示一句话 description。
- **Remote 查看**：选仓 → Commits / Branches 两 tab，commits 列 sha/msg/author/相对时间，只读，标注「live · remote，不 clone」。

## 9. 实施拆解（建议 cycle plan）

- **BE-1**：数据模型（CodeRepoRef 结构化字段 + 迁移 + CRUD + set-primary + 凭据加密存储）+ 项目 API。
- **BE-2**：provider 抽象 + go-github 适配器 + git 回退 + viewing API；agent MCP `list_project_repos`/`get_repo_info`（含 live）。
- **FE**：仓配置 UI + remote 查看 UI。
- Integrate → Accept（tester）→ Ship。

## 10. 待 oopslink 拍板（开 plan 前）

1. **viewing/live 取数**：v1 = go-github + git 回退（推荐）？还是先只 git ls-remote（provider 无关、最省，但 commit 数据简陋）？
2. **agent 接口**：默认静态、live 可选（推荐）确认？
3. **凭据作用域**：项目级 token（推荐）vs org 级？用 GitHub App 还是 PAT 起步？

## 附：选型结论
不自造 VCS。git 机械操作沿用 `git` CLI；远端富数据用 forge SDK（go-github/go-gitlab）；自建托管（Gitea/Forgejo）本特性**不需要**。难点不在 git 本身，在凭据 + 多 provider 抽象 + 不泄密，已在 §5/§6 收敛。
