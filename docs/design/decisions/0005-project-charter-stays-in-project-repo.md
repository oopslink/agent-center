# 0005. 项目宪章留在项目仓库

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-16 |

## Context

agent-center 支持多领域项目（编程 / 写作 / 投研 ...），每个项目有自己的领域规范、风格、约束。

初稿设计在 project 表里存 `charter_md` 字段（项目宪章 markdown），每次 agent 干活时注入到 prompt。

讨论中发现：

- 项目本身通常已有 `CLAUDE.md` / `AGENTS.md` / `README.md` 之类的本地约定文件
- Claude code 等 CLI 启动时**自动读取**这些文件（agent CLI 的标准行为）
- 在 agent-center 再存一份，等于跟项目仓库的内容**两套并存**，有同步问题

## Decision

**Agent-center 不管理项目宪章 / charter 文档**。项目自有的本地约定文件（`CLAUDE.md` / `AGENTS.md` / `README.md` 等）：

- 留在**项目仓库**里
- 由 **agent CLI 在 worktree cwd 自然加载**
- agent-center **不复制、不缓存、不注入**

Worker daemon 唯一的责任：spawn agent 之前 `cd` 到 worktree 路径。

## Consequences

正面：

- **项目自治**：agent 行为约定跟代码一起版本化，跟着 git 走
- **零同步问题**：不存在 "中心存的 charter 跟项目仓库里的 CLAUDE.md 不一致" 的尴尬
- **无需迁移**：未来切到非 agent-center 工作流（直接 SSH 跑 claude），约定仍然生效
- **agent-center 更薄**：Project 表近似只是 ID + 几个 metadata，不持有大文本

负面 / 待跟进：

- 不同 agent CLI 的本地约定路径不同（claude code 用 CLAUDE.md，opencode 可能用 AGENTS.md）—— 由 worker adapter 处理
- 如果有跨项目通用的"领域规范"，目前没有地方放（需要时引入 supervisor working memory 的 `global` scope）

## Alternatives Considered

### A. agent-center inline 存 charter_md (TEXT 字段)

- Pro: 中心化，跨 worker 同步
- Con: 跟项目仓库 CLAUDE.md 双源；agent-center 模型变重

### B. agent-center 存 charter_path（路径指向 worker 本地文件 / BlobStore）

- Pro: 不 inline 大文本
- Con: 还是双源；agent CLI 不会主动读这个 path（要 worker 注入）

### C. 项目自治（本决定）

- Pro: 单一来源，无同步成本
- Con: agent-center 失去对"项目规范"的可见性（但本来也不该参与）

## 影响范围

- Project schema 中**不出现** `charter_md` / `charter_path`
- Worker daemon 实现：spawn agent 必 `cd worktree`，不做注入
- 文档中所有"charter"提及为"项目本地约定文件"
