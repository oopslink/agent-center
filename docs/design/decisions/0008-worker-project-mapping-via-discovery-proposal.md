# 0008. WorkerProjectMapping 走"自动发现 + 用户确认"流程

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-16 |

## Context

Worker 跟 Project 的关系（哪台 worker 能执行哪个项目、本地路径在哪）必须维护起来。三条路线：

- **A. 静态声明**：worker.yaml 列每个项目 + 路径；要改就编辑 + 重启
- **B. 纯自动发现**：worker 扫本地 git repos，自动注册（无确认）
- **C. 自动发现 + 用户确认**：worker 扫候选 → 推飞书让用户审 → 用户 ✅ 才生效

讨论中明确：

- A 个人项目频繁加 repo 不实际；编辑 YAML + 重启的体验差
- B "太魔法"，会把 `quick-prototype` / `old-experiment` / 临时 fork 等无用 repo 也纳入，难管理
- C 兼顾自动化与可控

同时澄清：**Worktree 是动态的**（per task 创建 / GC），不能跟 base_path 混为一谈也不进 mapping 表；worktree_root 按约定 = `base_path + ".wt"`，也不存。

## Decision

**WorkerProjectMapping 走 C 路线：自动发现 + 用户飞书确认。**

引入新聚合 `WorkerProjectProposal`：

- Worker 周期扫 `discovery.scan_paths`（worker.yaml 配置）
- 候选作为 Proposal 上传，状态机 `pending → accepted / ignored / superseded`
- 通过飞书卡片让用户审；点 ✅ 才升级为 `WorkerProjectMapping`
- 点 ❌ 标 `ignored`，worker 下次扫不再提
- 用户后悔可 `agent-center worker proposal unignore <id>` 重置

**Worktree 处理**：

- `worker_project_mappings` 只存 `base_path`，**不存** `worktree_root`
- `worktree_root` 按约定推导 = `base_path + ".wt"`
- 具体活跃 worktree 通过 `events` 表 + AgentSession 投影实时呈现，不进 schema

边界处理见 [architecture/07-worker-model.md § WorkerProjectMapping 创建与维护](../architecture/07-worker-model.md#workerprojectmapping-创建与维护)。

## Consequences

正面：

- **零 worker.yaml 编辑**：加项目只是把 repo clone 到 scan_paths 下，等 worker 扫到并推送 → 点 ✅
- **不会随便建无用 mapping**：用户主动 ✅ 才生效
- **跨 worker 一致**：同一项目在不同 worker 上各自经历 propose → confirm 流程，center 自动复用 project 实体
- **Worktree 跟 mapping 解耦**：mapping 是稳定关系，worktree 是动态运行时；模型边界清晰
- **复用现有 escalation 通道**：proposal 走飞书卡片，跟 Issue / InputRequest / Suggestion 一致的用户审视模式

负面 / 待跟进：

- 多一个聚合 `WorkerProjectProposal` 与一张表 `worker_project_proposals`
- 用户首次部署时会收到一批 proposal（启动 worker → 扫 scan_paths → 一片候选）；UX 上 supervisor 需要把多条 proposal 合理打包成卡片，避免刷屏
- Worker 必须有"已询问过的"记忆，避免重复轰炸（依赖 center 的 proposal 表，worker 提议前先 query）

## Alternatives Considered

### A. 静态声明（worker.yaml 列 project + path）

- Pro: 直观、git 管控
- Con: 加项目要改文件 + 重启；UX 烦琐

### B. 纯自动发现 / 无确认

- Pro: 极致零配置
- Con: 太魔法，把不相关 repo 也建出来；项目命名 / 元数据无法控制

### D. CLI 命令运行时管理（`agent-center worker map-project ...`）

- Pro: 不重启 worker
- Con: 跟 worker 真实状态可能漂移；多一套 API；命令式不如自动化

## 影响范围

- 新增聚合 / 表：`WorkerProjectProposal` / `worker_project_proposals`
- `worker_project_mappings` 删 `worktree_root` 字段
- 新增 CLI：`worker proposal list / unignore`
- Worker.yaml 新增 `discovery` 段；删原静态 `projects` 段
- Supervisor 需要新增"如何把多条 proposal 打包成 1 张卡片"的策略（默认逐条 / 阈值打包）
- 详细流程文档：[architecture/07-worker-model.md](../architecture/07-worker-model.md)
- 影响功能需求：[requirements/01-functional.md](../requirements/01-functional.md)（新增 F23）
