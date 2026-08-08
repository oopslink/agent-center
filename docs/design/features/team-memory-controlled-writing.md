# Team Memory 受控写入能力

> 架构决策：[ADR-0057](../decisions/0057-controlled-team-memory-writes.md)。
> 存储与接口细节：[Team Memory Write Protocol](../implementation/team-memory-write-protocol.md)。

## 1. 产品目标

让 Team 在执行任务过程中持续沉淀共享经验和运行规约，同时保证 Agent 的一次性输出不会
未经治理就成为影响全队的 canonical memory。

用户应能回答：谁提出了什么、证据来自哪里、谁批准/拒绝、canonical 哪个 commit 生效、
哪些在飞 Run 仍使用旧快照。

## 2. 角色与权限

| 角色 | 提案 | 查看提案 | Promote/Reject | 直接改 canonical |
|---|---:|---:|---:|---:|
| Team member Agent/Supervisor | 是 | 本 Team | 否 | 否 |
| Curator Agent（policy 显式授权） | 是 | 本 Team | 是 | 仅通过 promotion |
| Human owner/admin | 是 | 本 Org Team | 是 | 仅通过同一 service |
| Executor | 否 | 否 | 否 | 否 |
| 非 Team Agent / 跨 Org | 否 | 否 | 否 | 否 |

Team 默认 `proposal_only`，不自动配置 Curator。Human owner/admin 在 Team Settings 中维护
`curator_agent_refs`；删除成员时自动撤销其 curator grant。

## 3. Proposal 模型

Proposal 是 Team Memory aggregate 内的实体：

- `proposal_id`：`tmprop-{ULID}`。
- `operation`：`add | update | disable | delete`。
- `target_kind`：`entry | rule`。
- `target`：update/disable/delete 必须带 `source_path`、文件内 UUID 和 `expected_blob_hash`。
- `candidate`：add/update 的 title、description、body；rule 另含 enabled/applies_to。
- `rationale`：为什么值得成为团队记忆。
- `evidence_refs`：task/issue/conversation/message/plan/commit 等弱关联。
- `author_ref`、`created_at`、`idempotency_key`。
- `status`：`pending | promoted | rejected | superseded`。
- review metadata：reviewer、comment、reviewed_at、promotion commit。

状态机：

```text
pending ──promote──> promoted
   ├────reject────> rejected
   └──supersede───> superseded
```

终态不可重开；修改候选内容必须 supersede 旧 proposal 并创建新 proposal，保留审计链。
canonical UUID 与 source path 在 update/disable 中不可变；rename 必须用 add + 明确 supersede/delete
旧条目的提案表达，避免 Git path rename 与 identity 同时变化。

## 4. Agent-facing MCP

### `propose_team_memory_change`

当前 Agent 的 Team 由 Center 解析，调用方不传 team_id，避免 confused deputy。输入 operation、
kind、candidate/target、rationale、evidence_refs、idempotency_key。返回 proposal id、repo commit、
validation findings 与状态。

### `list_team_memory_proposals`

按 status/kind 分页读取当前 Team proposal；默认 pending。

### `get_team_memory_proposal`

读取单个 proposal、canonical target 当前 blob hash、diff preview 和 review history。

### `review_team_memory_proposal`

仅 Curator Agent 可用；action 为 `promote | reject`，必须携带 `expected_repo_commit`、
`expected_proposal_status=pending`、review comment。返回最终 commit 与 refresh impact。

工具描述必须明确：proposal 不会立即进入运行时；promotion 后也只影响新的 planning generation
和新的 executor fork，在飞快照不变。

## 5. Human Web

Team Detail > Memory 增加：

- `Entries / Rules / Proposals` 三个筛选，不新增一级 Team Rules 页面。
- owner/admin 可创建、查看 diff、promote/reject；普通 member 只读。
- Team Settings 增加 Curator Agents 管理与 policy 状态。
- canonical 条目展示 source path、UUID、last commit；Rule 展示 enabled/applies_to。
- promotion 后显示“新 Run 生效；当前在飞 Run 保持 commit X”的提示。

Web 不直接操作 Git 文件，全部调用与 MCP 相同的 TeamMemoryService。

## 6. 写入语义

- **add**：生成 `entries/<slug>-<uuid>.md` 或 `rules/<slug>-<uuid>.md`。
- **update**：保持 UUID 和 source path，按 expected blob hash CAS 替换内容。
- **disable**：仅用于 rule，保持文件 identity，将 `enabled=false`。
- **delete**：删除指定文件；Rule 默认引导 disable，delete 仍需显式 review comment。
- `MEMORY.md` 永远由 service 重建，输入不接受 index 内容。

同一 idempotency key + author + Team 重试必须返回同一 proposal；相同 key 不同 payload 返回冲突。

## 7. Runtime 一致性

Promotion 成功后 canonical repo HEAD 前移：

- 新 planning MCP generation 重新读取新 commit。
- 同 generation 的 planning snapshot 继续冻结。
- 新 executor fork 读取新 commit。
- 在飞 executor 保持 `input.json.team_rules.commit`，不热注入、不重启。

响应返回 `effective_for=new_sessions_and_forks` 及 old/new commit；Activity/audit 可追溯 proposal id。

## 8. 安全与内容门禁

- org/team membership 与 Curator/human role 双重鉴权；跨 Org 统一 404。
- path 由 service 生成/解析，调用方不能提供任意 filesystem path。
- body、description、evidence 数量和总 payload 有上限；拒绝 NUL、非法 frontmatter、未知 phase。
- secret-shaped token、私钥、worker/admin credential 命中即 hard reject，不允许 Curator override。
- repo URL、本机绝对路径、疑似专有 token 等标为 warning；promotion 必须在 review comment 中确认。
- Markdown 作为不可信内容展示，Web 转义 HTML/URL；runtime 注入沿用现有 prompt 内容边界。

## 9. 可观测性

新增 Cognition domain events：

- `team_memory.proposed`
- `team_memory.promoted`
- `team_memory.rejected`
- `team_memory.promotion_conflicted`

事件携带 org/team/proposal/operation/kind/actor/source path/old-new commit/reason taxonomy，正文不进
events 表。Git commit 保存完整内容与 diff。由于 Git 与 events DB 无法同事务，Git transition 是
权威；幂等 projector 以 `(team_id, proposal_id, status, promotion_commit)` 生成稳定 event id，持久化
checkpoint 并在 restart/reconcile 时补投，不能使用“push 成功后 best-effort emit”的丢事件实现。
`inspect/stats` 增加 pending proposal 数、最近 promotion、
冲突/拒绝计数；Agent Activity 展示 proposal/promote 摘要。

## 10. 验收标准

1. Team member Agent 能幂等提案但不能 promotion；非 Team/cross-org 被拒绝。
2. 显式 Curator Agent 与 human owner/admin 可审核；撤销 grant 后立即失权。
3. add/update/disable/delete 均按 UUID/blob CAS，冲突不覆盖；MEMORY.md 确定性重建。
4. 两个并发 proposal 不丢；同 proposal 并发 promote 只有一个成功。
5. proposal 永不进入 get_team_rules；promoted rule 仅影响新 generation/fork。
6. secret hard reject、warning acknowledgement、payload/path/frontmatter 门禁有效。
7. Web/MCP 使用同一 service，Git 是 canonical 唯一来源，events 不存正文。
8. Go race、全量 Go/Web、真实 bare repo push/rebase/restart smoke 全绿。

## 11. 五轮 Grill 结论

1. **边界**：否决裸 `write_team_memory`；采用 proposal/promotion，Executor 永不直写。
2. **操作与并发**：从“只新增”补全 add/update/disable/delete、UUID/blob CAS、幂等与同 proposal 单赢家。
3. **授权与 DDD**：否决 capability tag 授权；Team policy 保存 Curator grant，所有 adapter 进入 Cognition AppService。
4. **安全与观测**：补 secret hard reject、warning acknowledgement、内容不进 events、Git diff 与标准事件。
5. **生效与兼容**：明确 snapshot 冻结、旧 MCP/读路径兼容、mixed-version rollout 与真实 Git smoke。
