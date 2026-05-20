# Phase 3: Discussion Core

> DDD Core BC: Discussion · 依赖 Phase 1（Shared Kernel + Events 总线 + Conversation BC）+ Phase 2（TaskRuntime Core，含 TaskRepository）· 解锁 Phase 4（Observability 投影 + 查询面）
>
> 纪律：按里程碑顺序 / 模块完备不半成品 / 单测 ≥ 90% + 集成 + e2e + 测试报告

## § 0. 目标

**Discussion BC 完整战术工件落地 + Phase 2 的 IssueConcludeSpawn stub 被完整实装替换。** 用户 / supervisor / agent 视角下，本 phase 完成后系统应支持：

- 用户 / supervisor 通过 CLI `issue open` 起一个 Issue（议事 thread）；CLI 路径下 Issue 创建即写 events 表的 `issue.opened`，conversation_id 暂为 null（懒创建路径，[ADR-0021 § 1 + § 7](../design/decisions/0021-issue-as-conversation.md)）
- 用户在飞书 @bot / Web Console 起 Issue（同步建路径）时，Discussion BC 同事务调 Conversation BC 建 `kind=issue` Conversation + 写 `issue.conversation_id`；本 phase 提供 application service 入口，Bridge 实际发卡推到 Phase 5
- Agent（worker daemon 中转）调 `agent-center open-issue` 开 Issue —— **[conventions § 1 单一来源](../rules/conventions.md) 路径**：worker 不能造 Task，想要新工作必走 Issue
- 议事 IO 走 `issue comment` facade，内部 1:1 翻译为 Phase 1 的 `conversation add-message`（不引入 IssueComment 实体，[ADR-0021 § 3](../design/decisions/0021-issue-as-conversation.md)）
- `issue conclude --resolution=closed_with_tasks --spawn-tasks=...` 同事务**批量** spawn N (N ≥ 1) Task + 写 1 条 `kind=system` Message + 推进 Issue 状态机到 closed_with_tasks，all-or-nothing
- Phase 2 留的 `IssueConcludeSpawn` stub 在本 phase 替换为完整实装：调 TaskRuntime BC `TaskRepository.Save` 批量建 N 个 task（带 `from_issue_id` 血缘）+ emit `issue.concluded` / `issue.tasks_spawned` + N × `task.created`

**DDD 意义**：Discussion 是 v1 Issue / Task 协作链上的关键 Core BC（[strategic/03-bounded-contexts § 2](../design/architecture/strategic/03-bounded-contexts.md)）。本 phase 走通了 Core BC 之间唯一的"同事务跨 BC 双写"路径（Discussion → TaskRuntime + Discussion → Conversation），是 Phase 4+ 所有跨 BC 投影 / Bridge 同步的前置依赖。

---

## § 1. DDD 工件清单

### 1.1 Aggregate Roots

| AR | 路径 | 状态机 | 标识 |
|---|---|---|---|
| **Issue** | `internal/discussion/domain/issue.go` | 6 态：open / under_discussion / concluded / closed_no_action / closed_with_tasks / withdrawn（详见 [discussion/00-overview § 1.1](../design/architecture/tactical/discussion/00-overview.md) 状态机图） | ULID/UUID；身份不变；opener 不可改 |

### 1.2 Entities（子从属）

**无**。议事消息走 Conversation Message（[ADR-0021 § 3](../design/decisions/0021-issue-as-conversation.md)）；IssueComment 实体不存在。

### 1.3 Value Objects

| VO | 路径 | 用途 |
|---|---|---|
| **IssueOrigin** | `internal/discussion/domain/issue_origin.go` | issue 来源 5 类（cli / web / feishu_at / supervisor / agent_open_issue）；驱动 conversation_id 同步建 vs 懒创建分支（[discussion/00-overview § 4.1](../design/architecture/tactical/discussion/00-overview.md)） |
| **IssueResolution** | `internal/discussion/domain/issue_resolution.go` | `{kind, summary, tasks?: []IssueConcludeTaskSpec}`；kind ∈ `closed_no_action / closed_with_tasks / withdrawn`；`closed_with_tasks` 时 tasks 必非空（[discussion/00-overview § 1.3](../design/architecture/tactical/discussion/00-overview.md)） |
| **IssueConcludeTaskSpec** | `internal/discussion/domain/issue_conclude_task_spec.go` | batch 单条 task 规格：`{local_id?, title, description, depends_on: []string}`；depends_on 元素是 batch 内 local_id 或既有 task uuid（[task-runtime/00-overview § 3.4](../design/architecture/tactical/task-runtime/00-overview.md)） |
| **IssueStatus** | `internal/discussion/domain/issue_status.go` | 6 态枚举 + 合法跃迁表 |
| **IdentityRef** | 复用 Phase 1 `internal/conversation/domain/identity_ref.go` | opener / concluded_by 字段使用 |
| **IssueID / ProjectID / ConversationID / TaskID** | `internal/sharedkernel/ids.go`（Phase 1 已建） | typed string aliases |

### 1.4 Repositories

| Repo | 路径 | 主要方法 | sentinel errors |
|---|---|---|---|
| **IssueRepository** | `internal/discussion/domain/issue_repository.go`（interface）/ `internal/discussion/persistence/issue_repository_sqlite.go`（实现） | `FindByID` / `FindByProject` / `FindByStatus` / `FindByOpener` / `Save` / `UpdateStatus` (CAS, with `from, to, version`) / `UpdateConversationID` / `UpdateConclusion` / `UpdateRelatedConversationIDs` | `ErrIssueNotFound` / `ErrIssueAlreadyExists` / `ErrIssueInvalidTransition` / `ErrIssueVersionConflict` / `ErrIssueAlreadyConcluded` / `ErrIssueWithdrawn` / `ErrIssueNoConversationBound`（[discussion/00-overview § 5.1](../design/architecture/tactical/discussion/00-overview.md)） |

> **不引入** `IssueCommentRepository`（[ADR-0021 § 3](../design/decisions/0021-issue-as-conversation.md)；议事消息查询走 Conversation BC `MessageRepository.FindByConversationID(issue.conversation_id)`）

### 1.5 Domain Services

| Service | 路径 | 职责 |
|---|---|---|
| **IssueLifecycleService** | `internal/discussion/service/issue_lifecycle_service.go` | Open / Withdraw / Conclude 状态机推进 + 同事务跨 BC 协作；持有 IssueRepository + ConversationLifecycleService（Phase 1 facade）+ IssueConcludeSpawn 引用 + EventSink |
| **IssueCommentService** | `internal/discussion/service/issue_comment_service.go` | facade：`Comment(ctx, issueID, content, kind, actor)` 内部翻译为 Conversation BC `AddMessage(conversation_id=issue.conversation_id, ...)`；conversation_id 为 null 时返回 `ErrIssueNoConversationBound` |
| **IssueBindConversationService** | `internal/discussion/service/issue_bind_conversation_service.go` | 懒创建路径：同事务建 `kind=issue` Conversation + 写 `issue.conversation_id` |
| **IssueLinkConversationService** | `internal/discussion/service/issue_link_conversation_service.go` | 弱关联：append `conv_id` 到 `issue.related_conversation_ids` JSON 数组 |
| **IssueConcludeSpawn**（**TaskRuntime BC owner**） | `internal/taskruntime/service/issue_conclude_spawn.go` | **本 phase 替换 Phase 2 stub**：入参 `IssueConcludeSpec`，单事务内 spawn N Tasks（写 task 行 + 填 `from_issue_id`）+ 解析 batch 内 `local_id` → uuid + 校验 dep 图无环 + 返回 N 个 task.id；Discussion BC 是 caller |

> `IssueConcludeSpawn` 的物理 owner 是 TaskRuntime BC（[task-runtime/00-overview § 3.4](../design/architecture/tactical/task-runtime/00-overview.md)；保留 Phase 2 已确定的 BC 归属），Discussion BC 是 caller。本 phase 在 Discussion BC 内调用，但实装代码补在 `internal/taskruntime/service/` 下，替换 Phase 2 的 stub。

### 1.6 Application Services（CLI handler 层）

| Handler | CLI | 路径 |
|---|---|---|
| `IssueOpenHandler` | `agent-center issue open <project_id> <title> [--description=...]` | `internal/cli/issue/open.go` |
| `IssueCommentHandler` | `agent-center issue comment <issue_id> --content=... [--kind=text\|agent_finding\|...]` | `internal/cli/issue/comment.go` |
| `IssueConcludeHandler` | `agent-center issue conclude <issue_id> --resolution=closed_with_tasks --spawn-tasks=<json-file\|inline>` | `internal/cli/issue/conclude.go` |
| `IssueWithdrawHandler` | `agent-center issue withdraw <issue_id> --reason=... --message=...` | `internal/cli/issue/withdraw.go` |
| `IssueBindConversationHandler` | `agent-center issue bind-conversation <issue_id> [--channel=feishu] [--auto\|--to=<conv_id>]` | `internal/cli/issue/bind_conversation.go` |
| `IssueLinkConversationHandler` | `agent-center issue link-conversation <issue_id> --conversation=<conv_id>` | `internal/cli/issue/link_conversation.go` |
| `OpenIssueHandler`（agent audience） | `agent-center open-issue <project_id> <title> [--description=...]` | `internal/cli/agent/open_issue.go`（顶层 verb，[03-cli § 8.2](../design/implementation/03-cli-subcommands.md)） |

### 1.7 Domain Events（emit 给 Phase 1 events 表）

本 phase 引入 5 个 Discussion BC 事件 + 1 个 TaskRuntime 事件（`task.created` 来自 IssueConcludeSpawn 实装路径）：

| event_type | 触发 | refs JSON | 关键 payload |
|---|---|---|---|
| `issue.opened` | IssueLifecycleService.Open 成功 | `{issue_id, project_id, conversation_id?}` | `{title, opener, origin}` |
| `issue.discussion_started` | 首条非 opener Message 写入 → status 转 under_discussion | `{issue_id, conversation_id, first_message_id}` | `{first_sender}` |
| `issue.concluded` | conclude 成功（所有终态：closed_no_action / closed_with_tasks / withdrawn-via-conclude）| `{issue_id, conversation_id?}` | `{resolution_kind, summary, concluded_by}` |
| `issue.tasks_spawned` | conclude with `closed_with_tasks` → spawn N tasks 成功 | `{issue_id, task_ids: []}` | `{count, task_ids}` |
| `issue.withdrawn` | Withdraw 路径 | `{issue_id}` | `{reason, message, withdrawn_by}` ([conventions § 16](../rules/conventions.md)) |
| `task.created` × N | IssueConcludeSpawn 同事务每个 Task 入库 | `{task_id, project_id, from_issue_id}` | `{title, parent_task_id?, depends_on_task_ids}`（Phase 2 已定义事件 schema，本 phase 仅复用 emit 路径）|

> 议事消息走 Phase 1 `conversation.message_added`（已埋好），**不**新增 `issue.commented`（[ADR-0021 § 11](../design/decisions/0021-issue-as-conversation.md)）。

### 1.8 Context Map 关系

| 上游 → 下游 | 模式 | 本 phase 触达 |
|---|---|---|
| **Discussion → TaskRuntime** | Customer-Supplier | ✅ IssueConcludeSpawn 调 TaskRepository.Save（Phase 2 提供）；本 phase 实装 |
| **Discussion ↔ Conversation** | Shared Kernel / 1:1 | ✅ `issue.conversation_id` 1:1 强引用；同事务双写 |
| **Discussion ↔ Workforce** | Shared Kernel | ✅ `issue.project_id` 引用（Phase 1 提供 ProjectRepository） |
| **TaskRuntime → Discussion** | Customer-Supplier（反向） | ✅ Worker 上 agent 调 `open-issue` 命令 Discussion 建 Issue |
| **Cognition → Discussion** | "User" via tools | 🚧 Supervisor 调 issue facade —— **Phase 6 才真正 wire**；本 phase 留 CLI 入口可被任何 actor 调 |
| **Discussion → Bridge** | Customer-Supplier（outbound）| ❌ **本 phase 不接入**（Phase 5）；本 phase 只 emit 事件，Bridge 订阅推到 Phase 5 |
| **Observability ← Discussion** | Open Host | ⚠️ 本 phase emit 事件落 events 表 OK；五动词查询面 Phase 4 才上 |

---

## § 2. 上游依赖（来自 Phase 1 + Phase 2 的工件）

| 上游工件 | 来源 phase | 本 phase 哪一步会用 |
|---|---|---|
| `EventSink` interface + sqlite 实现 | Phase 1 | 每个 Domain Service emit `issue.*` 事件；IssueConcludeSpawn emit `task.created` × N |
| `events` 表 + migration | Phase 1 | 同上 |
| `internal/persistence/tx.go`（tx-via-ctx 模板，[02-persistence § 5](../design/implementation/02-persistence-schema.md#§-5)）| Phase 1 | Issue open 同步建路径 / conclude 同事务双 BC 写 / bind-conversation 同事务双写 |
| `ConversationRepository` + `MessageRepository`（含 `Append` / `FindRecent` / `UpdateStatus`）| Phase 1 | issue comment facade / 同步建 kind=issue Conversation / conclude 写 system Message |
| `ConversationLifecycleService.Open(kind=issue)` + `AddMessage` | Phase 1 | bind-conversation / 同步建路径 / conclude 写 system message |
| `IdentityRepository` | Phase 1 | opener / concluded_by 校验存在性 |
| `ProjectRepository`（Workforce） | Phase 1 | issue.project_id 校验 |
| `clock.Clock` interface + `realClock` / `fakeClock` | Phase 1 | `opened_at` / `concluded_at` 注入；测试不允许 sleep（[conventions § 14.x](../rules/conventions.md)） |
| `ulid.Gen` interface | Phase 1 | IssueID / 子 ULID 生成 |
| `TaskRepository`（含 `Save` / `FindByID` / `FindByProject`） | Phase 2 | IssueConcludeSpawn 写 N 个 Task |
| `TaskRuntime IssueConcludeSpawn` **stub** | Phase 2 | **本 phase 替换**；Phase 2 已定义函数签名 + 单测占位，本 phase 补完整实装 |
| `Task` AR + `from_issue_id` 字段 + 不可变约束 | Phase 2 | IssueConcludeSpawn 创建 task 时填 from_issue_id |
| `internal/cli/root.go` cobra cmd tree | Phase 1 | 注册 `issue *` / `open-issue` 子命令 |
| 行覆盖率工具 / lint / go vet | Phase 1 (CI) | DoD 校验 |

> **Phase 2 IssueConcludeSpawn stub 现状**：Phase 2 plan 已规定该函数签名 + 返回 `ErrNotImplemented` + 单测占位 + Phase 2 DoD 不要求实装 spawn 行为，本 phase **必须替换为完整实装**（[README § 2.2](README.md) 半成品红线）。

---

## § 3. 工作项分解（严格按依赖顺序）

### 3.1 Issue AR + VO 集 + Repository（先建数据结构）

- **工件**：`Issue` AR / `IssueStatus` / `IssueOrigin` / `IssueResolution` / `IssueConcludeTaskSpec` VO；`IssueRepository` interface + sqlite 实现
- **输入**：Phase 1 sharedkernel IDs / clock / Identity / Project repos；本 phase § 1.3 VO 集
- **输出**：
  - `internal/discussion/domain/issue.go`（AR + 状态机方法 Open/Withdraw/Conclude/RecordDiscussionStart）
  - `internal/discussion/domain/issue_status.go`（6 态 + transition table）
  - `internal/discussion/domain/issue_origin.go` / `issue_resolution.go` / `issue_conclude_task_spec.go`
  - `internal/discussion/domain/issue_repository.go`（interface + sentinel errors）
  - `internal/discussion/persistence/issue_repository_sqlite.go`（含乐观锁 version 列 CAS）
  - `internal/persistence/migrations/0003_discussion_issues.up.sql` + `.down.sql`
- **实现步骤**：
  1. 定义 IssueStatus 枚举 + 合法跃迁集合（白名单，校验 invalid 跃迁返回 `ErrIssueInvalidTransition`）
  2. AR 不变量（[discussion/00-overview § 2](../design/architecture/tactical/discussion/00-overview.md)）：opener 不可变 / withdrawn 后封锁 IO / closed_with_tasks 必带非空 tasks / conversation_id 1:1 不可解绑（除整体 → null）/ status 单调推进
  3. `description` 长文走 BlobStore（[conventions § 8](../rules/conventions.md)）：> 10KB 时写 BlobStore 返 blob_ref 存数据库；< 10KB 直存
  4. Repository 接口签名严格对位 [discussion/00-overview § 5.1](../design/architecture/tactical/discussion/00-overview.md)
  5. SQLite DDL：参考 [discussion/00-overview § 1.1 字段](../design/architecture/tactical/discussion/00-overview.md) + [conventions § 9.0 禁忌表](../rules/conventions.md)（TEXT/ISO8601/JSON-string；version INTEGER；无 AUTOINCREMENT）
  6. `UpdateStatus(ctx, id, from, to, version)` 用 CAS SQL（参考 02-persistence § 8.1.2 模板）；`RowsAffected==0` → `ErrIssueVersionConflict`
  7. Domain errors 用 sentinel pattern；调用方 `errors.Is` 判定
- **DoD**：单测覆盖所有跃迁路径（5 个合法 + N 个非法）+ Repository CAS 单测（成功 / version 冲突）+ migration 在 sqlite `:memory:` 跑通 + go vet 全过 + 行覆盖率 ≥ 90%

### 3.2 IssueLifecycleService（状态机 + Open 同步建/懒创建分支）

- **工件**：`IssueLifecycleService.Open / Withdraw / RecordDiscussionStart`
- **输入**：3.1 工件 + Phase 1 ConversationLifecycleService / IdentityRepository / ProjectRepository / EventSink / Clock / ULID
- **输出**：`internal/discussion/service/issue_lifecycle_service.go`（不含 Conclude；Conclude 在 3.4）
- **实现步骤**：
  1. `Open(ctx, cmd OpenIssueCommand)` 入参含 `origin: IssueOrigin`；分两支：
     - **同步建路径**（origin ∈ {feishu_at, web_console, supervisor_self_open}）：单事务内调 ConversationLifecycleService.Open(kind=issue) → 拿 conv_id → Issue.Save(conversation_id=conv_id) → emit `issue.opened` + `conversation.opened`
     - **懒创建路径**（origin ∈ {cli, agent_open_issue}）：Issue.Save(conversation_id=null) → emit `issue.opened`
  2. `Withdraw(ctx, id, reason, message, actor)`：status CAS open|under_discussion|concluded → withdrawn；reason+message 双字段（[conventions § 16](../rules/conventions.md)）；emit `issue.withdrawn`
  3. `RecordDiscussionStart(ctx, issueID, firstMessageID, firstSenderIdentityID)`：Phase 1 Conversation BC 在 add-message 时通过订阅 / 回调通知（具体 wiring 在 3.5 IssueCommentService 那侧）；status CAS open → under_discussion；emit `issue.discussion_started`
  4. 错误显式化（[conventions § 17](../rules/conventions.md)）：opener 不存在 → `ErrIdentityNotFound` 透传；project 不存在 → 透传；conv 创建失败 → tx rollback；emit 失败 → tx rollback
- **DoD**：同步建 / 懒创建分支单测各 1 + Withdraw 单测（合法 / 非法跃迁 / version conflict）+ RecordDiscussionStart 单测（含已是 under_discussion 时幂等）+ 同事务 conv 失败时 issue 也回滚的失败注入测试

### 3.3 IssueCommentService + IssueLinkConversationService + IssueBindConversationService（facade & 关联管理）

- **工件**：
  - `IssueCommentService.Comment(ctx, issueID, content, contentKind, actor) → MessageID`（facade）
  - `IssueLinkConversationService.Link(ctx, issueID, convID)` （弱关联 append）
  - `IssueBindConversationService.BindAuto(ctx, issueID, channel)` / `BindTo(ctx, issueID, convID)`（懒创建 / 强引用，[ADR-0021 § 7](../design/decisions/0021-issue-as-conversation.md)）
- **输入**：3.1 / 3.2 + Phase 1 ConversationLifecycleService.AddMessage + ConversationRepository
- **输出**：
  - `internal/discussion/service/issue_comment_service.go`
  - `internal/discussion/service/issue_link_conversation_service.go`
  - `internal/discussion/service/issue_bind_conversation_service.go`
- **实现步骤**：
  1. `Comment` facade：load Issue → 若 `conversation_id == null` 返回 `ErrIssueNoConversationBound` → 否则调 Phase 1 `AddMessage(conversation_id, sender=actor, content_kind, content)` → 若是首条非 opener Message，回调 `IssueLifecycleService.RecordDiscussionStart`
  2. `BindAuto` 懒创建：单事务内调 `ConversationLifecycleService.Open(kind=issue, project_id=issue.project_id, primary_channel_hint=channel)` → 拿 conv_id → `IssueRepository.UpdateConversationID` → emit `conversation.opened`（Phase 1 标准事件）+ `issue.opened` 路径不再 emit（已 open）→ 但 emit Discussion-side hook event `issue.conversation_bound`（**不在 § 1.7 列表**；按 ADR-0021 没明示该 event，**改为仅靠 `conversation.opened` 自带 refs.issue_id 让 Bridge 知道是 issue 绑 conv**）
  3. `BindTo` 强引用既有 conv：校验目标 conv kind=issue + status=open + 未被其它 issue 占用 → `IssueRepository.UpdateConversationID`
  4. `Link` 弱关联：校验目标 conv 存在 → 读 issue.related_conversation_ids JSON → append（dedupe）→ `IssueRepository.UpdateRelatedConversationIDs`；不 emit 事件（仅元数据更新，可观测性靠 inspect issue）

> **关于 `issue.conversation_bound` 事件**：ADR-0021 没明示，本 plan 决定**不引入新 event**，复用 `conversation.opened` + 字段 refs.issue_id 区分。这跟 [Phase 4 Observability] 投影一致性需要核对（投影实现时若发现需要专门 event，再回 plan 修订 + 加 ADR）。

- **DoD**：
  - Comment facade 单测：conv 已绑 / 未绑（返 `ErrIssueNoConversationBound`）/ 触发 discussion_started（首条非 opener）/ 不触发（opener 自己评）
  - BindAuto / BindTo 单测：成功 / 目标 conv 非 kind=issue / 目标 conv 已被占用 / issue 已绑 conv 时返错
  - Link 单测：append / dedupe / 目标 conv 不存在
  - 同事务回滚测试：BindAuto 中 UpdateConversationID 失败 → conv 也回滚

### 3.4 IssueConcludeSpawn 完整实装（替换 Phase 2 stub）+ Conclude 同事务跨 BC 写

- **工件**：
  - **`internal/taskruntime/service/issue_conclude_spawn.go`**：Phase 2 留的 stub 替换为完整实装
  - `IssueLifecycleService.Conclude(ctx, cmd ConcludeIssueCommand)`：协调状态机推进 + 跨 BC 同事务写
- **输入**：3.1 / 3.2 + Phase 2 TaskRepository.Save + Task AR with `from_issue_id` 字段 + Phase 1 ConversationLifecycleService.AddMessage
- **输出**：
  - `internal/taskruntime/service/issue_conclude_spawn.go`（完整实装，**含 Phase 2 stub 的所有原签名兼容**）
  - `internal/discussion/service/issue_lifecycle_service.go` 内 `Conclude` 方法（3.2 已建文件，本节扩列）
  - 配套单测 `*_test.go`
- **实现步骤**：

  **IssueConcludeSpawn 实装**：
  1. 入参签名（保持 Phase 2 stub 兼容）：`Spawn(ctx, spec IssueConcludeSpec) ([]TaskID, error)`；`IssueConcludeSpec = { issue_id, project_id, resolution: IssueResolution, closing_comment? }`
  2. **预校验**（事务外，failfast）：
     - resolution.kind == closed_with_tasks 时 tasks 必非空
     - 各 IssueConcludeTaskSpec.title 非空
     - depends_on 元素分两类：batch 内 local_id（前缀如 `local:` 或不是 ULID 格式）vs 既有 task uuid
     - dep 图无环（topo sort 检测）
  3. **事务内**（tx-via-ctx）：
     - 为每个 spec 生成 task uuid（按 spec 出现顺序）
     - 解析 depends_on：local_id → 替换为同 batch 内对应 uuid；既有 uuid → 调 TaskRepository.FindByID 校验存在；任一失败 → return err（tx rollback）
     - 顺序调 TaskRepository.Save 写 N 行：`{id, project_id, title, description, status=open, from_issue_id=spec.issue_id, depends_on_task_ids, conversation_id=null}`
     - emit `task.created` × N（refs.from_issue_id）
     - 返回 `[]TaskID`
  4. 错误处理（[conventions § 17](../rules/conventions.md)）：任何 step 失败 → tx rollback；caller（Conclude）收到 err → 自己也 rollback；不允许 partial spawn

  **IssueLifecycleService.Conclude**：
  1. 入参：`ConcludeIssueCommand = { issue_id, resolution: IssueResolution, concluded_by: IdentityRef }`
  2. **事务内**：
     - load Issue + 校验 status ∈ {open, under_discussion}（concluded 后不能再 conclude → `ErrIssueAlreadyConcluded`；withdrawn 后 → `ErrIssueWithdrawn`）
     - resolution.kind 分支：
       - `closed_no_action`：IssueRepository.UpdateStatus(from, closed_no_action, version) + UpdateConclusion(summary, concluded_by, now) → emit `issue.concluded`
       - `closed_with_tasks`：调 IssueConcludeSpawn.Spawn(ctx, spec) → 拿 `[]TaskID` → 若 issue.conversation_id != null，调 ConversationLifecycleService.AddMessage(conv_id, content_kind=system, content="已 spawn task #X / #Y / #Z", direction=internal) → UpdateStatus + UpdateConclusion → emit `issue.concluded` + `issue.tasks_spawned`
       - `withdrawn`：复用 Withdraw 路径（兼容："conclude 时选 withdrawn"等于撤回）
  3. **同事务回滚保证**：用 Phase 1 的 tx-via-ctx；任一 emit / repo 操作失败 → 整段 rollback；测试必须覆盖（见 § 5.2）
- **DoD**：
  - IssueConcludeSpawn 单测：spawn 1 task / spawn N task / dep 图含 local_id / dep 图含既有 uuid / dep 图含混用 / 环检测 / 既有 uuid 不存在 / TaskRepository.Save 失败回滚
  - IssueLifecycleService.Conclude 单测：closed_no_action / closed_with_tasks (1 task / N task) / withdrawn / 已 concluded / 已 withdrawn / version 冲突
  - 同事务回滚单测：Conclude 中 IssueConcludeSpawn 失败 → issue.status 未变 + 无 task 入库 + 无 events emit（含 task.created 也没 emit）

### 3.5 CLI handlers（issue * + agent open-issue）

- **工件**：[§ 1.6](#16-application-servicescli-handler-层) 列的 7 个 handler
- **输入**：3.1-3.4 工件 + Phase 1 cobra root cmd + `--from-issue` flag 已在 Phase 2 task create 路径预留
- **输出**：`internal/cli/issue/*.go` + `internal/cli/agent/open_issue.go` + handler 注册到 root
- **实现步骤**：
  1. 每个 handler 走 thin layer：parse flags → 构造 Command → 调对应 Domain Service → 输出 result（JSON / human-friendly）
  2. `issue conclude --resolution=... --spawn-tasks=...`：
     - `--resolution` ∈ {closed_no_action, closed_with_tasks, withdrawn}
     - `--spawn-tasks` 接收 JSON file path 或 inline JSON；解析为 `[]IssueConcludeTaskSpec`；resolution=closed_with_tasks 时必填
  3. `open-issue` 是 agent audience 顶层 verb（[03-cli § 8.2](../design/implementation/03-cli-subcommands.md)），origin=agent_open_issue；transport 走 worker daemon → center 的 RPC（Phase 1 已建 unix socket）；本 phase 在 cli 层 hook 进现有 transport，不引入新 transport
  4. `--help` 输出严格对位 [03-cli § 8.2](../design/implementation/03-cli-subcommands.md) 描述
  5. 错误显式化：domain error → exit code 非 0 + stderr 输出 reason + message；不允许 silently exit 0
- **DoD**：
  - 每个 handler 单测（用 mock service 验证 flag → command 映射）
  - `--help` 输出 snapshot 测试（diff 检测，对位 03-cli）
  - exit code 测试：成功 0 / domain err non-zero

### 3.6 端到端验证（issue open → comment → conclude → 看到 task 创建）

- **工件**：端到端测试 harness（`tests/e2e/issue_conclude_spawn_test.go`）
- **输入**：3.1-3.5 全部工件 + Phase 1 e2e harness 框架
- **输出**：1 个 e2e 测试覆盖完整用户场景
- **实现步骤**：
  1. 用临时 SQLite file 起 center mock（不实际 spawn server 进程，直接 cli main + DI）
  2. 顺序执行：
     - `agent-center project add --slug=demo --path=/tmp/...`
     - `agent-center issue open demo "Test issue" --description="..."` → 拿 issue_id
     - `agent-center issue bind-conversation <issue_id> --auto`（懒创建）
     - `agent-center issue comment <issue_id> --content="comment 1" --actor=user:hayang`
     - `agent-center issue comment <issue_id> --content="comment 2" --actor=supervisor:inv-1`
     - `agent-center issue conclude <issue_id> --resolution=closed_with_tasks --spawn-tasks=<json>`，json 含 2 task：
       ```json
       [{"local_id":"a","title":"Task A","depends_on":[]},
        {"local_id":"b","title":"Task B","depends_on":["local:a"]}]
       ```
     - `agent-center query tasks --from-issue=<issue_id>` → 期望返回 2 task，按 from_issue_id 过滤命中
  3. 断言：
     - 2 个 task 入库；id 各自不同；task B.depends_on_task_ids 包含 task A.id（local:a 已解析）
     - events 表含：`issue.opened` × 1 + `conversation.opened` × 1 (kind=issue) + `conversation.message_added` × 2 (comment) + `issue.discussion_started` × 1 (首条非 opener comment) + `issue.concluded` × 1 + `issue.tasks_spawned` × 1 + `task.created` × 2
     - issue.status == closed_with_tasks；conclusion_summary != ""
- **DoD**：上述 e2e pass + 用例文件归档 `tests/e2e/issue_conclude_spawn_test.go` + 测试报告（§ 5.3）行号引用

---

## § 4. Definition of Done

- [ ] § 1 所有工件实现并通过单测
- [ ] § 5 所有测试场景通过（unit + 集成 + e2e）
- [ ] **Phase 2 IssueConcludeSpawn stub 已完整替换**：不再返 `ErrNotImplemented`；旧测试占位重写 / 删除
- [ ] 单测行覆盖率 ≥ 90%（diff + 整体；以 `go test -cover ./internal/discussion/... ./internal/taskruntime/service/issue_conclude_spawn*` 为准）
- [ ] 测试报告归档到 `docs/plans/reports/phase-3-test-report.md`（§ 5 行号 1:1 对应实际用例文件）
- [ ] 触发的 6 个 domain event（`issue.opened` / `issue.discussion_started` / `issue.concluded` / `issue.tasks_spawned` / `issue.withdrawn` / `task.created`）实际进 events 表（集成测试断言）
- [ ] CLI 命令 `--help` 跟 [03-cli § 8.2](../design/implementation/03-cli-subcommands.md) 对齐
- [ ] `go vet ./...` + `go test ./...` + 项目本地 lint 全过
- [ ] § 6 风险项要么处理要么显式 defer 到具体后续 phase
- [ ] **跨 BC 同事务双写保证**：手工故障注入测试通过（IssueConcludeSpawn 中途失败 → issue.status / events / tasks 全 rollback；通过测试日志 + 数据库 dump 双确认）
- [ ] **未引入新事件类型未文档化**：本 phase 没加 `issue.conversation_bound` 之类 ADR-0021 没明示的事件；如要加，先补 ADR

---

## § 5. 测试计划

### 5.1 单测场景（按工件分类）

| # | 工件 | 测试场景 | 关键断言 |
|---|---|---|---|
| U-01 | Issue AR | 合法跃迁 open → under_discussion | 状态变 + version+1 |
| U-02 | Issue AR | 合法跃迁 under_discussion → concluded | 同上 |
| U-03 | Issue AR | 合法跃迁 concluded → closed_with_tasks | conclusion_summary 非空 |
| U-04 | Issue AR | 合法跃迁 concluded → closed_no_action | tasks 为空也 OK |
| U-05 | Issue AR | 合法跃迁 * → withdrawn | reason + message 双字段非空 |
| U-06 | Issue AR | 非法跃迁 closed_with_tasks → open | 返 `ErrIssueInvalidTransition` |
| U-07 | Issue AR | 非法跃迁 withdrawn → concluded | 返 `ErrIssueWithdrawn` |
| U-08 | Issue AR | 非法跃迁 concluded → concluded（重复）| 返 `ErrIssueAlreadyConcluded` |
| U-09 | Issue AR | opener 改写尝试 | panic / 不可达（编译期 immutable） |
| U-10 | IssueRepository (sqlite `:memory:`) | Save 新建 + FindByID | 字段全等 |
| U-11 | IssueRepository | UpdateStatus CAS 成功 | RowsAffected=1 |
| U-12 | IssueRepository | UpdateStatus CAS 版本冲突 | `ErrIssueVersionConflict` |
| U-13 | IssueRepository | UpdateStatus from 状态不匹配 | `ErrIssueVersionConflict`（or invalid transition，按实现选） |
| U-14 | IssueRepository | UpdateConversationID 成功 + 再次写非 null 不允许 | 第二次返错 |
| U-15 | IssueRepository | FindByProject / FindByStatus / FindByOpener | 过滤正确 |
| U-16 | IssueRepository | description 大文 (>10KB) → BlobStore | 数据库存 blob_ref |
| U-17 | IssueOrigin VO | 枚举合法 / 非法 | parse 返 err |
| U-18 | IssueResolution VO | closed_with_tasks 必带 tasks 非空 | 校验返 err |
| U-19 | IssueConcludeTaskSpec | depends_on 区分 local_id vs uuid | 解析器单测 |
| U-20 | IssueConcludeTaskSpec | dep 图环检测 | topo sort 失败返 err |
| U-21 | IssueLifecycleService.Open | 同步建路径（origin=feishu_at）| conv 也建 + issue.conversation_id 非 null + emit 2 event |
| U-22 | IssueLifecycleService.Open | 懒创建路径（origin=cli）| conversation_id=null + emit `issue.opened` 唯一 |
| U-23 | IssueLifecycleService.Open | 同步建路径 conv 创建失败 → issue 也回滚 | DB 中无 issue 行 |
| U-24 | IssueLifecycleService.Withdraw | 合法路径 reason+message 完整 | emit `issue.withdrawn` |
| U-25 | IssueLifecycleService.Withdraw | reason 空或 message 空 | 返参数错（§ 16） |
| U-26 | IssueLifecycleService.RecordDiscussionStart | 首条非 opener | status → under_discussion + emit |
| U-27 | IssueLifecycleService.RecordDiscussionStart | 已是 under_discussion（幂等）| no-op |
| U-28 | IssueCommentService.Comment | conv 已绑 + 首条非 opener | 触发 RecordDiscussionStart |
| U-29 | IssueCommentService.Comment | conv 未绑 | 返 `ErrIssueNoConversationBound` |
| U-30 | IssueCommentService.Comment | opener 自评 | 不触发 discussion_started |
| U-31 | IssueBindConversationService.BindAuto | 成功路径 | conv 建 + UpdateConversationID + emit `conversation.opened` |
| U-32 | IssueBindConversationService.BindAuto | UpdateConversationID 失败 → conv 回滚 | 数据库无 conv 行 |
| U-33 | IssueBindConversationService.BindTo | 目标 conv 非 kind=issue | 返 `ErrConversationInvalidKind` |
| U-34 | IssueBindConversationService.BindTo | 目标 conv 已被占用 | 返 `ErrConversationAlreadyExists` |
| U-35 | IssueLinkConversationService.Link | append + dedupe | JSON 数组无重复 |
| U-36 | IssueLinkConversationService.Link | 目标 conv 不存在 | 返 `ErrConversationNotFound` |
| U-37 | IssueConcludeSpawn | spawn 1 task（resolution=closed_with_tasks） | task 入库 + `from_issue_id` 填 + emit `task.created` × 1 |
| U-38 | IssueConcludeSpawn | spawn 3 task 含 batch 内 local_id 依赖 | local_id 解析为正确 uuid |
| U-39 | IssueConcludeSpawn | spawn 含既有 task uuid 作 dep | 校验 FindByID 命中 |
| U-40 | IssueConcludeSpawn | 既有 uuid 不存在 | tx rollback + 0 task 入库 |
| U-41 | IssueConcludeSpawn | dep 图环 | 预校验返 err（事务外） |
| U-42 | IssueConcludeSpawn | TaskRepository.Save 第 2 条失败 | 全部 N 个 task 都没入库 + 无 emit |
| U-43 | IssueLifecycleService.Conclude | resolution=closed_no_action | issue 终态 + emit `issue.concluded`；不调 IssueConcludeSpawn |
| U-44 | IssueLifecycleService.Conclude | resolution=closed_with_tasks 1 task | spawn 1 + Conv system Message + emit 2 event |
| U-45 | IssueLifecycleService.Conclude | resolution=closed_with_tasks N task | spawn N + emit `task.created` × N + `issue.tasks_spawned` |
| U-46 | IssueLifecycleService.Conclude | conv_id 为 null 时 closed_with_tasks | spawn 仍成功；不写 system Message（无 conv） |
| U-47 | IssueLifecycleService.Conclude | 已 concluded 再 conclude | 返 `ErrIssueAlreadyConcluded` |
| U-48 | IssueLifecycleService.Conclude | IssueConcludeSpawn 失败 → 全部回滚 | issue.status 未变 / 0 task / 0 event emit |
| U-49 | IssueLifecycleService.Conclude | issue.version 在事务前被改 → CAS 冲突 | 返 `ErrIssueVersionConflict` |
| U-50 | CLI `issue open` handler | flag 解析 + DI 调 service | mock service 收到 OpenIssueCommand 字段对齐 |
| U-51 | CLI `issue comment` handler | --content / --kind / --actor | 同上 |
| U-52 | CLI `issue conclude` handler | --resolution + --spawn-tasks=inline JSON | 解析为 IssueConcludeSpec 正确 |
| U-53 | CLI `issue conclude` handler | --spawn-tasks=<file path> | 读文件 + 解析 |
| U-54 | CLI `issue conclude` handler | resolution=closed_with_tasks 但 --spawn-tasks 缺 | 返参数错 + exit code 非 0 |
| U-55 | CLI `issue withdraw` | --reason + --message | 透传 |
| U-56 | CLI `issue bind-conversation` | --auto / --to 互斥 | 互斥校验 |
| U-57 | CLI `issue link-conversation` | --conversation 校验 | 透传 |
| U-58 | CLI `open-issue` (agent verb) | origin=agent_open_issue | service 收到正确 origin |
| U-59 | CLI handlers | domain error → exit code 非 0 + stderr 含 reason+message | 全部 handler 覆盖 |
| U-60 | CLI handlers | `--help` snapshot | 对位 03-cli § 8.2 |

### 5.2 集成测试场景

| # | 场景 | 涉及工件 | 关键断言 |
|---|---|---|---|
| I-01 | Issue + Conversation 同事务建（同步建路径） | IssueLifecycleService.Open / ConversationLifecycleService / sqlite | 两张表都有行 + events 表 2 条 + 同 tx 提交 |
| I-02 | 同 I-01 但 Conversation Open 注入失败 → 全部回滚 | 同上 + fault injector | 0 row in `issues` / `conversations` / `events` |
| I-03 | Issue comment facade → Conversation Message 入库 + `issue.discussion_started` emit | IssueCommentService / MessageRepository | messages 表 + events 表均有对应行 |
| I-04 | IssueConcludeSpawn 实际触发 TaskRepository.Save × N（真实 sqlite） | IssueConcludeSpawn / TaskRepository (Phase 2) | tasks 表有 N 行 + 每行 `from_issue_id` 正确 + events 表 `task.created` × N |
| I-05 | Conclude 同事务 emit `issue.concluded` + `issue.tasks_spawned` + `task.created` × N | IssueLifecycleService.Conclude | events 表 commit 时序：`task.created` × N → `issue.tasks_spawned` → `issue.concluded`（全在同 tx，seq 单调） |
| I-06 | Conclude 中 IssueConcludeSpawn 第 2 条 task 失败 → 全部回滚 | 同上 + TaskRepository fault injector | issues 表 status 未变 + 0 task + 0 event；mock 验证 emit 0 次（所有 emit 在 tx 内，rollback 时也撤回） |
| I-07 | BindAuto 同事务建 conv + 写 issue.conversation_id | IssueBindConversationService | 两表一致 + `conversation.opened` 事件 refs.issue_id 命中 |
| I-08 | BindTo 强引用既有 conv（conv 已被另一 issue 占用）| 同上 | 返 `ErrConversationAlreadyExists` + 无 mutation |
| I-09 | Link 弱关联（多次 link 同 conv）| IssueLinkConversationService | related_conversation_ids JSON 去重 |
| I-10 | Issue 状态 closed_with_tasks 后再 Comment | IssueCommentService + Issue 不变量 | 应用层应拒（终态后封锁 IO，[discussion/00-overview § 2.3](../design/architecture/tactical/discussion/00-overview.md)）；实际行为依 ADR-0021 表态 —— withdrawn 状态封锁 IO，**closed_* 状态保留可读但不写**，本节断言"按规约封锁写入" |
| I-11 | description > 10KB 触发 BlobStore 路径 | IssueRepository | DB 存 blob_ref（≤ 200 字节）；BlobStore 内容可读回 |
| I-12 | events.seq 单调递增（事务内多 emit）| EventSink 整链 | seq strictly monotonic |

### 5.3 e2e 测试场景

| # | 场景 | 用户视角 / 入口 CLI | 关键断言 |
|---|---|---|---|
| E-01 | 完整 Issue → Conclude Spawn 链路 | `agent-center project add` → `issue open` → `issue bind-conversation --auto` → `issue comment` × 2 → `issue conclude --resolution=closed_with_tasks --spawn-tasks=<json>` → `query tasks --from-issue=<id>` | 2 task 入库；task B.depends_on 含 task A.id（local_id 已解析）；events 7 条；issue.status=closed_with_tasks |
| E-02 | Agent 路径开 Issue | `agent-center open-issue <project> "title"` （通过 worker daemon → center） | issue 入库；origin=agent_open_issue；opener_identity 形如 `agent:session-<id>` |
| E-03 | Issue Withdraw 路径 | `issue open` → `issue withdraw --reason=duplicate --message="dup of #5"` | issue.status=withdrawn + events 含 `issue.withdrawn{reason,message}` |
| E-04 | Issue conclude=closed_no_action | `issue open` → `issue conclude --resolution=closed_no_action` | issue 终态；events 无 `task.created` / `issue.tasks_spawned` |
| E-05 | Issue conclude 失败回滚（spawn 中 dep 引用不存在的 task uuid） | 同 E-01 但 spawn-tasks JSON depends_on 含一个伪造 uuid | exit code 非 0；issue.status 未变；tasks 表 0 行；events 表无 `task.created` / `issue.concluded` |

---

## § 6. 风险 / Spike 项

| # | 风险 | 缓解 / Spike |
|---|---|---|
| R-01 | Phase 2 stub `IssueConcludeSpawn` 的函数签名在本 phase 实装时发现不够（如缺 closing_comment / 缺 actor）| 在 § 3.4 实装前先 spike 1 天对齐 Phase 2 签名；若需扩签名，回 Phase 2 plan 加 amendment 而非偷偷改 |
| R-02 | `local_id` 命名空间冲突（batch 内 spec 全用 "a" / "b"，跟既有 task uuid 形式不可区分时）| 强制 local_id 前缀 `local:` 或非 ULID-shape；预校验拒绝歧义；单测 U-19 覆盖 |
| R-03 | 同事务跨 BC（Discussion + TaskRuntime + Conversation）写在 sqlite 单连接下应能跑通；但若 Phase 1 tx-via-ctx 模板未覆盖 3 BC scenario 可能踩坑 | § 3.4 实装前先验证 Phase 1 tx 模板能并发跨 3 BC repo；不行先打补丁回 Phase 1 plan 而非本 phase 私造 |
| R-04 | `issue.conversation_bound` 事件是否需要？本 plan § 3.3 决定不引入，靠 `conversation.opened` 兜底；若 Phase 4 投影发现需要专门事件，会回头 amend | 本 phase 留 escape hatch：BindAuto 调用方可选记一条 `events.bridge_hint` payload，避免后续硬 break；最坏 Phase 4 加 ADR + amend |
| R-05 | BlobStore（description > 10KB）路径如果 Phase 1 没完全实装，本 phase 可能 block | Phase 1 DoD 包含 BlobStore 契约测试；本 phase 测试 U-16 用 Phase 1 提供的 fake BlobStore；真实路径如 Phase 1 未完成，spike defer 到本 phase 内补一个 LocalFs 实现 |
| R-06 | Conclude 中跨 BC emit 失败导致部分 task 已入库但事件没 emit | 用 Phase 1 EventSink 的 tx-内 emit（events 表 INSERT 在同 tx）；emit 失败 = tx commit 失败 = 全 rollback；I-06 显式验证 |
| R-07 | Worker daemon → center 的 RPC transport（`open-issue`）Phase 1 是否就绪？| Phase 1 DoD 含 worker daemon ↔ center RPC（用于 conversation add-message）；本 phase 复用同 transport；如缺 spike 1 天补 |
| R-08 | "终态后封锁 IO" 行为（I-10）跟 ADR-0021 表述需对齐：withdrawn 明示封锁，closed_* 没明示。本 phase 选 "withdrawn / closed_* 都封锁写入，保留读" | 文档化决定：在本 phase 结束后回 [discussion/00-overview § 2.3](../design/architecture/tactical/discussion/00-overview.md) invariants 加一行 "closed_* / withdrawn 同 invariant"；不引入新 ADR |

---

## § 7. 下游解锁

本 phase 完成后，下游 phase 可以基于本 phase 的接口 surface 推进：

**Phase 4（Observability 投影 + 查询面）**：
- 本 phase emit 的 6 个事件类型（`issue.opened` / `issue.discussion_started` / `issue.concluded` / `issue.tasks_spawned` / `issue.withdrawn` / `task.created`-via-spawn）落 events 表后，Phase 4 可订阅投影到 `inspect issue` / `query issues` / `query tasks --from-issue=*` 读模型
- `IssueRepository` 接口 surface 已固定（[discussion/00-overview § 5.1](../design/architecture/tactical/discussion/00-overview.md)），Phase 4 五动词查询面可直接消费

**Phase 5（Bridge ACL Outbound）**：
- 本 phase 同步建路径 emit 的 `conversation.opened` (kind=issue)（refs.issue_id）给 Bridge 提供"发 Issue root card"触发点
- `issue conclude` emit 的 `conversation.message_added` (content_kind=conclusion_draft) 给 Bridge 提供"渲染富卡片含 [确认结论] / [改改] / [不做] 按钮"触发点
- Bridge 实装在 Phase 5 完成；本 phase 仅 emit 事件不实际投递，因此**事件契约**就是本 phase 提供给 Phase 5 的 surface

**Phase 6（Cognition Supervisor）**：
- Supervisor 可调本 phase 的 `issue open` / `issue comment` / `issue conclude` facade（CLI 入口 + 内部 service 引用都已就绪）
- Supervisor 唤醒事件白名单（[discussion/00-overview § 7.1](../design/architecture/tactical/discussion/00-overview.md)）订阅本 phase 事件

**Phase 7（Bridge Inbound + 部署）**：
- 飞书用户在 Issue thread 内回复 → Bridge inbound 调 Conversation BC `conversation add-message`（Phase 1 接口），不直接调本 phase；本 phase 保证 `issue.conversation_id` 1:1 强引用使 Bridge 反查命中

**冻结的 surface（本 phase 之后不允许改语义，[README § 2.1](README.md)）**：
- IssueRepository 接口签名（含 sentinel errors 集合）
- IssueLifecycleService.Open / Withdraw / Conclude 三个公开方法签名
- IssueCommentService.Comment / IssueBindConversationService.BindAuto+BindTo / IssueLinkConversationService.Link 签名
- IssueConcludeSpawn.Spawn 签名（注：跟 Phase 2 stub 兼容）
- 6 个 domain event 名 + refs/payload 字段集
- CLI 7 个子命令的 flag set（含 `--resolution` 取值 / `--spawn-tasks` JSON 格式）

后续 phase 只能**扩列**（加新方法 / 新 event / 新 flag），不能改既有签名 / 字段语义。
