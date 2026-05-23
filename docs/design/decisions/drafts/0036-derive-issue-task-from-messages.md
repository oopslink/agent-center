# 0036. 从 Conversation Messages 派生 Issue / Task（v2 CV4）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-23 |
| Related | v2 议题 CV4；CV3 [ADR-0035](0035-cross-conversation-message-carryover.md) 的**写路径** —— CV3 定 carry-over 数据模型，CV4 定怎么用户触发派生 + 建立 carry-over references |

## Context

CV3 [ADR-0035](0035-cross-conversation-message-carryover.md) 引入 `conversation_message_reference` 表（carry-over）。但只有读 CLI；CV4 补 **写路径**：用户在父 conversation 选 message → 触发 Issue / Task 派生 → 同事务建立 carry-over refs。

### 用户故事

> 在 channel 里讨论 → 选某段 messages → 「开 Issue 讨论这事」 → 子 Conversation 起始含选定 messages 作上下文

### 现有 Issue / Task 派生路径（v1）

- supervisor `agent-center issue open ...` — supervisor 决策时
- user `agent-center issue open ...` — 用户直接开
- ~~飞书 @bot 在 thread 里说「开 Issue」~~ → vendor 撤回（[ADR-0031](0031-v2-drop-bridge-vendor-integration.md)）

→ CV4 加 **「from conversation messages」** 第三条入口。

## Decision

### 1. 派生支持的目标 entity

| 目标 | 支持？ |
|---|---|
| Issue | ✅（主用例）|
| Task | ✅（次要但允许 —— 用户直接派 Task）|
| 子 channel | ❌ v2 不做（用户 v3+ roadmap）|

### 2. CLI flags

**派 Issue**：

```bash
agent-center issue open \
    --from-conversation=<conv-id-or-name> \
    --select-messages=<msg_id1>,<msg_id2>,... \
    --project=<project-id> \
    --title="..." \
    [--description="..."]
```

**派 Task**：

```bash
agent-center task new \
    --from-conversation=<conv-id-or-name> \
    --select-messages=<msg_id1>,<msg_id2>,... \
    --project=<project-id> \
    --agent=<agent-instance-name-or-id> \
    --title="..." \
    [--description="..."]
```

**flag 命名约定**：

- `--from-conversation` = 父 conversation（id 或 name 二选一；CLI 解析）
- `--select-messages` = 选定 message id 列表（逗号分隔）；可空 = 不 carry-over，纯派生
- `--project` = **必填**（Issue / Task 必属一个 project；不从父 conv 推断 —— 父 conv 可能涉及多个 project）

### 3. 同事务流程

```
tx (issue open / task new with carry-over):
  1. 校验:
     - from-conversation 存在 + state ∈ {active}
     - 调用 user (actor) 是 from-conversation 的 active participant ([ADR-0034](0034-conversation-participants-field.md) 严格 join)
     - select-messages 都属于 from-conversation
     - project 存在
     - (Task) agent-instance 存在 + state ∈ {idle, active}
  2. INSERT Conversation:
     - kind = 'issue' (or 'task')
     - parent_conversation_id = <from_conv_id>
     - name = <title>
     - description = <description>?
     - created_by = <user>
     - participants = [{identity_id=<user>, role='member', joined_at=now, joined_by='system'}]  -- Q5 仅 creator
       (Task 额外加 supervisor + agent_instance.identity, per CV2b auto-add 规则)
  3. INSERT Issue (or Task):
     - conversation_id = <new>
     - project_id = <project>
     - title, description, ...
  4. INSERT conversation_message_reference rows (per [ADR-0035](0035-cross-conversation-message-carryover.md)):
     - child_conv_id = <new>
     - source_msg_id = each msg in select-messages
     - source_conv_id = <from_conv_id>
     - order_in_child = i (按 source posted_at 升序)
  5. emit events:
     - conversation.opened (kind=issue/task, with_carry_over=true if N>0)
     - issue.opened (or task.created)
     - conversation.participant_joined (creator)
     - conversation.message_references_added (count=N) -- 可选事件 / 含 source_conv_id

事务失败任一步 rollback all
```

### 4. 校验细节

| 校验 | 失败 reason |
|---|---|
| `from-conversation` 不存在 | `from_conversation_not_found` |
| `from-conversation` not active | `from_conversation_not_active` |
| User 非 active participant | `not_a_participant` |
| 至少一条 select message 不属于 from-conversation | `invalid_message_selection` |
| project 不存在 | `project_not_found` |
| (Task) agent-instance 不存在 / archived | `agent_not_available` |

CLI 输出明确错误（per [conventions § 16](../../../rules/conventions.md) reason+message 双字段）。

### 5. Auto-participants 规则

新 conversation 默认 participants：

- **Issue**: 仅 creator (user)
- **Task**: creator (user) + supervisor (agent:<supervisor.id>) + 执行 agent (agent:<worker_agent_instance.id>)；per [ADR-0034 § 4](0034-conversation-participants-field.md) auto-add 规则

后续 invite 由 channel owner（= conversation.created_by）手动加。

### 6. 哪些父 conv kind 允许派生

**任何 kind 允许**（用户决定；跟 [ADR-0035 § 3](0035-cross-conversation-message-carryover.md) 一致）。典型：

| 父 kind → 子 | 典型场景 |
|---|---|
| `channel` → `issue` | 主用例：群里讨论 → 开 Issue |
| `channel` → `task` | 群里发现具体改动 → 直接派 task |
| `issue` → `task` | Issue 议事 conclude 时携 issue messages 派 Task（跟 IssueConcludeSpawn 路径并存 / 互补）|
| `task` → `issue` | task 跑过程中发现新议题，从 task thread 开 sub-issue |
| 其他 | 用户自由 |

### 7. 派生权限

**v2 trust user + require active participant**：调用者必须是 from-conversation 的 active participant（[ADR-0034](0034-conversation-participants-field.md)）。

v3+ 多用户场景可加细粒度权限。

### 8. CLI 体验

```
$ agent-center channel show ac-design | tail -10
... messages ...
[msg-01HE7XYZ] user:hayang  2026-05-23T11:00  "我觉得 dispatch 的 retry 策略要加一下..."
[msg-01HE7XZ1] agent:01HE6  2026-05-23T11:01  "可以；建议指数 backoff + 上限"
[msg-01HE7XZ2] user:hayang  2026-05-23T11:02  "对，我开个 Issue 详谈"

$ agent-center issue open \
    --from-conversation=ac-design \
    --select-messages=msg-01HE7XYZ,msg-01HE7XZ1,msg-01HE7XZ2 \
    --project=agent-center \
    --title="dispatch retry 策略"
✓ Created issue 'dispatch retry 策略' (id=<new>); conversation kind=issue;
  Carry-over: 3 messages from 'ac-design';
  Participants: user:hayang (owner).
```

### 9. 跟现有 Issue / Task 创建路径并存

- `agent-center issue open --project=... --title=...`（无 `--from-conversation`）= 现有路径，无 carry-over
- `agent-center issue open --from-conversation=... --select-messages=... --project=... --title=...` = CV4 新路径，含 carry-over

→ 同一命令两种调用方式；CLI 解析 flag presence 决定走哪条事务。

### 10. 事件

| 事件 | payload |
|---|---|
| `conversation.opened` | 含 `with_carry_over: bool` / `parent_conversation_id?` |
| `conversation.message_references_added` | `child_conv_id, source_conv_id, msg_ids, by, created_at` |
| `issue.opened` / `task.created` | 现有事件，加 `from_conversation_id?` / `carry_over_msg_count?` 字段 |

## Consequences

**正面**：

- 用户故事「群里选段聊天开子话题」直接表达
- 跟现有 Issue / Task 创建路径并存（flag-based）；不破坏现有 API
- 严格 participant 校验跟 CV2b 一致；用户体验直观
- 同事务保证「Conversation + Issue/Task + carry-over refs + participants」一致性
- 为 Web Console UI（W1）提供清晰的「选 messages → 派生」 workflow 基础

**负面 / 待跟进**：

- CLI flag 数量增加（`--from-conversation` + `--select-messages` 两个新 flag）；user 学习曲线
- 同事务跨表写入（4 个 table：conversation / issue/task / conversation_message_reference / events）；事务量大但 SQLite 单库 OK
- 选 message 的 UX 在 CLI 上不顺手（需手敲 msg_id）；Web Console W1 提供多选 UI 才好用
- Task 派生时 agent-instance 必填，但 v2 用户场景可能未建 agent；引导文案要明确（"先 `agent create`"）

## Alternatives Considered

### A. 仅 Issue 派生（不支持 Task）

- ✅ 范围最小
- ❌ 用户偶尔想直接派 Task；增加同 flag 路径成本极低
- 否决

### B. Auto-participants = creator + parent active participants（继承式）

- 父群所有人自动同步进新子 conv
- ✅ 维护社交语境
- ❌ 易导致「莫名其妙被拉进子话题」；不符 v2 严格 invite 原则
- 否决（仅 creator + 必要系统 actor）

### C. project_id 从父 conv 自动推断

- 自动看父 conv 关联的 project
- ❌ 用户已表态「一个会话里可能讨论多个项目」；推断不可靠
- 否决（必填）

### D. 派生命令独立（不复用 `issue open`）

- 新命令 `agent-center conversation derive --kind=issue ...`
- ✅ 显式区分
- ❌ 用户已会用 `issue open / task new`；加新命令分散
- 否决（flag-based 复用）

## References

### v2 ADRs

- [ADR-0021 Issue as Conversation](../0021-issue-as-conversation.md) - 待 rewrite
- [ADR-0017 Task as Conversation](../0017-task-as-conversation.md) - 待 rewrite
- [ADR-0024 AgentInstance](0024-agent-instance-first-class.md) - Task agent 校验
- [ADR-0031 Drop Bridge](0031-v2-drop-bridge-vendor-integration.md) - 撤回 vendor 入口
- [ADR-0032 CV1 Channel](0032-conversation-channel-as-first-class.md) - parent_conversation_id 用
- [ADR-0033 Identity refactor](0033-identity-model-refactor.md) - actor 校验
- [ADR-0034 Participants 字段](0034-conversation-participants-field.md) - 严格 participant 校验
- [ADR-0035 Carry-over 模型](0035-cross-conversation-message-carryover.md) - CV3 数据模型 = 本 ADR 写入对象

### 跨 BC

- [conversation/01-conversation.md](../../architecture/tactical/conversation/01-conversation.md) - schema 引用
- [discussion/00-overview.md](../../architecture/tactical/discussion/00-overview.md) - Issue 创建 service
- [task-runtime/01-task.md](../../architecture/tactical/task-runtime/01-task.md) - Task 创建 service

### 来源

- 2026-05-22 → 2026-05-23 #agent-center 讨论
