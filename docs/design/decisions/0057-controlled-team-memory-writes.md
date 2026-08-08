# 0057. Controlled Team Memory Writes via Proposal and Promotion

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-08-09 |

## Context

Team Memory 的 canonical 内容位于 Center 托管的 per-team Git repo：`entries/` 存共享经验，
`rules/` 存运行规约，`MEMORY.md` 是派生索引。当前 agent-facing MCP 只有
`get_team_rules`；现有 `TeamMemoryProducer` 只服务 template seed/migration，不能直接升级成
通用写入口。

让任意 Agent 或 Executor 直接 CRUD canonical 文件会带来四类问题：并发重复/覆盖、一次性
现场经验被错误升格、跨 Team 越权、以及规则在缺少审阅时立刻影响后续 plan/execute/review/
recovery。只允许 human Web 写则又阻断 Agent 从任务复盘中持续学习。

## Decision

Team Memory 写入采用两阶段模型：

1. **Proposal**：当前 Team 的 Agent 可通过 MCP 提交 `add/update/disable/delete` 候选变更；
   proposal 写入同一 Team Git repo 的 `proposals/`，不进入 `MEMORY.md`，不被 runtime 加载。
2. **Promotion**：Human owner/admin 可通过 Web 审核；被 Team policy 显式授权的
   **Team Memory Curator Agent** 可通过 MCP `promote/reject`。Promotion 在一个 Git commit
   中原子修改 canonical entry/rule、更新 proposal 状态并重建索引。

Team Memory 是 Cognition bounded context 的 per-team aggregate，`team_id` 是 aggregate id，
Git HEAD commit 是 aggregate version。所有写操作必须经过新的 `TeamMemoryService`；MCP、Web、
template seed 和 migration 都是 adapter，不得直接调用 Store 改文件。

默认 policy 为 `proposal_only`：Agent 可提案，只有 human owner/admin 可审核。Owner/admin 可在
Team 设置中显式配置 `curator_agent_refs`，授权指定 Agent 自动 promotion；不能用自由文本
capability tag 充当授权。

Execution Run/Executor 永不持 Center 凭据，不能直接写；它们的 finding 由 Supervisor 通过 MCP
形成 proposal。

## Consequences

正面：

- Agent 获得可审计的持续学习入口，同时 canonical memory 保持单一写入门。
- Git 原生保存 author、diff、proposal、review 与 rollback；runtime 继续按 commit 冻结读取。
- Proposal 与 canonical 同 repo，promotion 可在单 commit 内保持原子性。
- Human 管理和 Curator Agent 自动治理共享同一 Application Service 与权限规则。

代价：

- Team aggregate 增加 Team Memory policy；Cognition BC 需要 proposal 状态机与 Git CAS。
- Web 需要 proposal review surface；MCP 需要 propose/list/get/review 工具。
- 并发 promotion、幂等、secret/path/content validation 必须成为硬门禁。

## Alternatives Considered

1. **任意 Agent 直接 CRUD canonical**：拒绝；并发污染且没有升格门。
2. **只有 human Web 可写**：拒绝；无法形成 Agent-native 学习闭环。
3. **用 Agent capability tags 判断 Curator**：拒绝；dispatch 标签不是授权边界。
4. **Proposal 存 SQLite、canonical 存 Git**：拒绝；一次 promotion 跨两个权威存储，无法原子提交。
5. **每个 proposal 独立 repo/branch**：拒绝；生命周期与清理复杂，单 repo `proposals/` 已能隔离 runtime。
