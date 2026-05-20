# Project 聚合

> **DDD 战术层** · BC: Workforce · 聚合: Project（独立 AR）

Project 是任务归属的逻辑容器（代码 repo / 写作 / 投研 / ...）。每个 Task 必属于一个 Project；每个 Issue 也属于一个 Project。Project 本身是**纯元数据聚合**，不持有任务 / 议题 / 任何动态状态。

---

## § 1. 状态机

**无状态机**（v1）。Project 是 CRUD 风格的元数据：add / update / remove。

未来如要管理 "active / archived"（隐藏不再活跃的 project，避免用户看一长串），可以加状态机；v1 不引入。

---

## § 2. 字段

```
project (
  id                  TEXT  -- slug，全局唯一；如 'agent-center' / 'my-blog'
  name                TEXT  -- 显示名
  kind                TEXT  -- 'coding' | 'writing' | 'investing' | null
  default_agent_cli   TEXT  -- 'claude-code' | 'codex' | 'opencode' | null（v1 默认 claude-code）
  default_workspace_mode TEXT  -- 'worktree' | 'direct'（v1 默认 worktree；详见 task-runtime/02-task-execution § 8）
  description         TEXT, nullable
  created_at          ISO8601 TEXT
  created_by_identity_id TEXT  -- 'user:hayang' 或 supervisor 自动创建时填 'system'
)
```

**注意**：

- `id` 是 slug 不是 UUID —— 让用户输入 `task create --project=agent-center` 比 UUID 友好
- Project 跟 git repo 不强绑：可以一个 project 对应多 worker 上的不同 base_path（通过多条 WorkerProjectMapping）

---

## § 3. 创建路径

### 3.1 ProposalReview accept 时自动创建（主路径）

ProposalReviewService（[00-overview § 3.3](00-overview.md)）accept 一条 Proposal 且 project 不存在时，**同事务**用 `proposal.suggested_project_id` + `suggested_kind` 创建 Project + 创建 WorkerProjectMapping。

### 3.2 CLI 手动创建（次路径，v1 罕见）

```
agent-center project add <project_id> --name=... [--kind=...] [--default-agent-cli=...]
```

用于：
- 用户预先建好 project（再让 worker 扫到时归并到这个 project）
- supervisor 自主开 issue / task 但无对应 project 时（v1 supervisor 不主动开 project，由 supervisor 提示用户）

### 3.3 不自动创建的场景

- ❌ Task 创建时若 project 不存在 → API 报错（用户必须先有 project）
- ❌ Issue 创建时若 project 不存在 → 同上
- 这保证 Project 是"刻意建立的"实体；避免 typo 或 orphan project

---

## § 4. CLI

| 命令 | 用途 |
|---|---|
| `agent-center project add <id> --name=... [--kind=...] [--default-agent-cli=...] [--default-workspace-mode=...]` | 创建 |
| `agent-center project list [--kind=...]` | 列表 |
| `agent-center project show <id>` | 详情（含相关 worker mapping 数 / task 数）|
| `agent-center project update <id> [--name=...] [--kind=...] [--default-agent-cli=...]` | 编辑 |
| `agent-center project remove <id>` | 删除（v1 严格：必须先无 active task / mapping）|

---

## § 5. Project Invariants

1. **project_id 全局唯一**：slug 形式（lowercase / hyphenated），不允许同名（命名冲突 → ProposalReview 飞书卡片标红让用户改）
2. **project_id 不可变**：一旦创建不能改 id（改 id 等价于建新 + 删旧 + 数据迁移；v1 不支持）
3. **name / kind / default_* 字段可更新**：通过 `project update`
4. **remove 前提**：必须先无 active Task / 无 active WorkerProjectMapping；否则报错（避免悬空引用）；可用 `project show <id>` 查依赖
5. **kind 枚举不可穷举**：v1 列了 coding / writing / investing 但允许 null；v2+ 可能扩；启发式 detection 走 ProposalDiscovery（[03-worker-project-proposal § 3](03-worker-project-proposal.md)）

---

## § 6. 跨聚合引用入方向

| 引用方 → 本聚合 | 强弱 | ADR |
|---|---|---|
| **WorkerProjectMapping → Project**（`mapping.project_id`）| 强 / 不可变 | - |
| **Task → Project**（`task.project_id`，TaskRuntime BC）| 强 / 不可变 | - |
| **Issue → Project**（`issue.project_id`，Discussion BC）| 强 / 不可变 | - |
| **WorkerProjectProposal → Project**（间接，via `suggested_project_id` / accept 后 `resulting_mapping.project_id`）| 弱 | - |

---

## § 7. 事件

| 事件 | 触发 | payload |
|---|---|---|
| `project.created` | CLI / ProposalReview accept 自动建 | project_id, name, kind, created_by_identity_id |
| `project.updated` | CLI update | project_id, changed_fields |
| `project.removed` | CLI remove | project_id |

---

## § 8. References

- [00-overview.md § 3.3 ProposalReviewService](00-overview.md)（accept 时自动建 Project 路径）
- [01-worker.md § 4 WorkerProjectMapping](01-worker.md)
- [03-worker-project-proposal.md](03-worker-project-proposal.md)
- [task-runtime/01-task.md](../task-runtime/01-task.md) — Task 引用 project_id
- [discussion/00-overview.md](../discussion/00-overview.md) — Issue 引用 project_id
- [ADR-0008](../../../decisions/0008-worker-project-mapping-via-discovery-proposal.md)
