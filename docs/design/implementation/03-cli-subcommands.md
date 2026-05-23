> ⚠ **v1-era doc** — pending rewrite in Phase 10 / 11 / 12. v2 撤回了 Bridge BC + 飞书集成 (per [ADR-0031](../decisions/drafts/0031-v2-drop-bridge-vendor-integration.md))；本文中 Bridge / vendor / 飞书 / 已删 ADR 引用是 v1 残留。

# CLI 子命令完整签名

> **实现层** · 把各 BC 设计的 CLI 入口落到 `agent-center` 单 binary（[conventions § 10](../../rules/conventions.md)）的子命令树。
>
> 本文档 **元层规则 § 1-7 + 全量命令清单 § 8（按 BC 分组）+ 代表性详细签名 § 9 + Repository 对位 § 10**。完整 flag/arg/输出 schema 落代码时以 `--help` + 集成测试为准。

## § 1. 子命令分类（CLI tree 顶层）

`agent-center` 是唯一二进制，按子命令第一级分两类：

### A. 运行模式 — 启动常驻 / 短生命周期进程

```
agent-center server [--config=...]                          # center 常驻
agent-center supervisor --scope=... [--invocation-id=...]   # 短生命周期 supervisor 调用
agent-center worker [--config=...]                          # worker daemon 常驻
```

### B. 资源操作 / 查询 — 短命令执行后退出

```
agent-center <subject> <verb> [<arg> ...] [--flag ...]
agent-center <observability-verb> [<kind>] [<id>] [--flag ...]
agent-center <agent-verb> [<arg>] [--flag ...]
```

顶层 verb / subject 命名见 § 2.1。

---

## § 2. 命名约定

### 2.1 subject 跟 BC 概念绑死

| Subject | BC | 出处 |
|---|---|---|
| `task` | TaskRuntime | tasks 聚合 |
| `issue` | Discussion | issues 聚合 |
| `worker` / `worker proposal` / `project` | Workforce | 各聚合 |
| `conversation` | Conversation | conversations 聚合 |
| ~~`identity`~~ | ~~Bridge~~ | ~~Bridge 内 vendor 身份~~ (v2 删 per ADR-0031) |
| `supervisor` / `record-decision` | Cognition | SupervisorInvocation / DecisionRecord |
| `inspect` / `query` / `ps` / `stats` / `logs` / `peek-trace` | Observability | Open Host — 跨 BC 五动词 + peek |
| ~~`bridge`~~ | ~~Bridge~~ | ~~vendor 集成管理~~ (v2 删 per ADR-0031) |
| `admin` | System | 运维 |

**vendor 概念不进 subject / verb / flag 名**（[conventions § 9.y](../../rules/conventions.md)）。`task bind-card` 是反例 —— 2026-05-20 已修正为 `task bind-conversation`（commit `0212f5a`）。

### 2.2 verb 用动词 / kebab-case

- 资源关系 (member-of) — `add` / `remove` / `update`
- 业务实体创建 — `create`
- 查询单条 — `show` / `inspect`
- 列表 — `list` / `query`
- 状态推进类（accept / ignore / conclude / retrigger 等）— 业务动词直接命名

不混用 `add` vs `create`：前者是建关系（如 `project add` 把项目加进 worker 的 mapping 列表），后者是建实体（如 `task create`）。

### 2.3 audience 标签

每条命令在 § 8 表里有 audience 列：

- `U` — VPS / 开发机 shell 直接调
- `S` — supervisor agent 通过 skill 调
- `A` — worker 内 agent 通过 worker daemon unix socket 调（[NF11](../requirements/02-non-functional.md)）
- `Sys` — center 内部 IPC / 内部 RPC 调，不暴露给用户

同一命令可有多个 audience（如 `inspect` 是 `U / S`）。

---

## § 3. 输入：flag / arg 规则

### 3.1 arg 顺序

```
agent-center <subject> <verb> <required-arg-1> <required-arg-2> ... [--flag ...]
```

required arg 按业务自然顺序（如 `task create <project_id> <title>`）；不超过 3 个 positional arg，更多用 flag。

### 3.2 flag 命名

- 长形式 `--long-name`（kebab-case）；短形式仅常用 flag（`-v` / `-f` / `-q`）
- 布尔 flag 默认 false；`--no-<x>` 是显式关闭（仅默认 true 的开关用）
- 多值 flag 用**重复**（`--label=a --label=b`），不接受逗号分隔（单一来源 / 解析简单）
- 时间 flag 接受 ISO 8601（`--since=2026-05-20T00:00:00Z`）或人话相对值（`--since=1h` / `--since=30m`）；解析层统一

### 3.3 enum flag 默认枚举值列

枚举类 flag 在 `--help` 完整列值：

```
--status=ready|in_progress|input_required|done|abandoned
--kind=task|execution|worker|issue|supervisor|conversation|input_request|project|worktree|decision
```

未列值视为非法 → exit 2 + usage error。

---

## § 4. 输出：JSON / 人类可读双轨

### 4.1 `--format` （v2 / P11 § 3.8）

每条命令（查询 / inspect / list / 操作）统一支持：

| 值 | 用途 |
|---|---|
| `table`（默认） | 表格 / 多行 / 对齐；操作型命令为单行成功信息 |
| `json` | 机器解析；snake_case key；schema 加字段允许、删/改字段视为 breaking |
| `text` | list 类输出一行一个 ID（脚本友好，可直接 xargs）；操作型同 table |

> `human` 是 v1 别名，归一化为 `table`。pre-§3.8 脚本不破。
>
> 实现：归一化 + 校验在 `Router.Run` 集中处理（`internal/cli/format.go`），非法值统一 `ExitUsage` + `usage_error: invalid --format`。每个 handler 只识别 `FormatJSON` / `FormatText` / fallthrough 三档；新增 list handler 必须显式实现 `FormatText` 分支。

非查询命令（`task create` / `accept` / 等）默认 table 单行成功信息；`--format=json` 时输出资源 ID + version + 关键字段；`--format=text` 等同 table。

### 4.2 JSON schema 稳定性

每条命令 JSON 输出格式纳入命令契约：

- **加字段**允许（向后兼容）
- **删 / 改字段**视为 breaking change，需 ADR 评估（[conventions § 5](../../rules/conventions.md)）

### 4.3 stderr / stdout 分轨

- **stdout**：业务输出（human / JSON）
- **stderr**：日志 / 进度 / 提示 / debug

机器侧解析者只读 stdout；`--format=json` 时 stdout 必为合法 JSON（无任何混入文本）。

---

## § 5. exit code 约定

| Code | 含义 |
|---|---|
| `0` | 成功 |
| `1` | 业务失败（通用，具体 reason 见 stderr / `error.reason`） |
| `2` | usage 错误（参数缺失 / unknown flag / enum 非法） |
| `16` | 版本冲突（乐观锁 CAS 失败，[02-persistence-schema § 4](02-persistence-schema.md)）|
| `17` | 资源 not found（domain `Err*NotFound` 系列） |
| `18` | 状态机非法跃迁（domain `Err*InvalidTransition`）|
| `19` | invariant 违反（如 single-active execution / 重复 enroll）|
| `64` | 内部错误（panic / unknown） |
| `130` | 用户主动 cancel（SIGINT） |

`1` 是默认业务失败；16-19 是细分，**caller 想区分重试策略时** 用（最常见 `16` 重试 / `17` `18` `19` 报错给用户）。

---

## § 6. error / message 双字段映射

跟 [conventions § 16](../../rules/conventions.md) 完全对齐 —— 命令失败时 JSON：

```json
{
  "error": {
    "reason": "task_not_found",
    "message": "Task 'T-42' not found in project 'agent-center'"
  }
}
```

- `reason`：机器可读 enum，跟 domain error 名对应（`ErrTaskNotFound` → `task_not_found`）
- `message`：人话上下文，含具体 ID / 时间戳 / 触发点

human 输出格式：`Error: <reason>: <message>` 一行红字到 stderr。

---

## § 7. CLI ↔ skill 文档同步规则

[conventions § 3 AI Native](../../rules/conventions.md)：新增 agent 可用能力的标准流程 **skill 文档 + CLI 子命令**。

- skill 文档（`supervisor.md` / `worker-agent.md`）按 audience 切片描述命令语义、何时调、参数要点
- 本文档 § 8 是 CLI 视角全量清单（按 BC 主键，audience 作为列标签）
- 任何 audience 含 `S` / `A` 的命令，**必须**在对应 skill 文档里有描述
- CI 校验：扫两份 skill md vs 本表 audience 列，缺漏报 build error

---

## § 8. 命令清单（按 BC 分组）

每条命令带 audience 标签（U / S / A / Sys）。

### 8.1 TaskRuntime

| 命令 | audience | 简述 |
|---|---|---|
| `task create <project_id> <title> [--description=...] [--parent=<task_id>] [--from-issue=<issue_id>]` | U / S | 创建 task；a/e 路径同事务建 task + Conversation（[ADR-0017](../decisions/drafts/0039-conversation-business-model-v2-unified.md)）<!-- v1 ref: ADR-0017 superseded by ADR-0039 -->|
| `task bind-conversation <task_id> [--channel=...] [--auto\|--to=<conv_id>]` | U / S | 懒创建：把没有 Conversation 的 task 绑到 / 新建 Conversation |
| `task unbind-conversation <task_id>` | U | v1 接口保留不实现（v2+ 多 channel 镜像） |
| `request-input <execution_id> --reason=... --message=... [--options=...]` | A | execution 阻塞等用户拍板；写 InputRequest + Conversation Message |
| `report-progress <execution_id> --kind=... --content=...` | A | worker daemon 投影摘要 push 入口（projection 列；[02-persistence-schema § 8.2.2](02-persistence-schema.md)）|
| `report-artifact <execution_id> --kind=... --content=...` | A | append-only artifact 上报（[task-runtime § 5.4](../architecture/tactical/task-runtime/00-overview.md)） |
| `report-failure <execution_id> --reason=... --message=...` | A | execution 终态 `failed` |
| `read-task-context <task_id>` | A | agent 读 task 元数据 / project 信息 / 历史 conversation |
| `dispatch <task_id> --worker=<worker_id>` | S / Sys | supervisor 派单（具体路径详见 [task-runtime/02-task-execution](../architecture/tactical/task-runtime/02-task-execution.md)） |
| `kill-execution <execution_id> --reason=... --message=...` | S | supervisor 主动 kill |

### 8.2 Discussion

| 命令 | audience | 简述 |
|---|---|---|
| `issue open <project_id> <title> [--description=...]` | U / S | 创建 Issue；CLI 路径懒创建 Conversation（[ADR-0021](../decisions/drafts/0039-conversation-business-model-v2-unified.md)）<!-- v1 ref: ADR-0021 superseded by ADR-0039 -->|
| `issue comment <issue_id> --content=... [--kind=user_comment\|agent_finding\|...]` | U / S / A | 写入 IssueComment（facade，落 Conversation Message）|
| `issue conclude <issue_id> --resolution=... [--spawn-tasks=...]` | U / S | 结案；若 `--spawn-tasks` 则在同事务批量 spawn Task |
| `issue bind-conversation <issue_id> [--channel=...] [--auto\|--to=<conv_id>]` | U / S | 懒创建路径触发（[ADR-0021 § 1](../decisions/drafts/0039-conversation-business-model-v2-unified.md)）<!-- v1 ref: ADR-0021 superseded by ADR-0039 -->|
| `issue link-conversation <issue_id> --conversation=<conv_id>` | S | 关联血缘 conversation（写 `issue.related_conversation_ids`，弱关联）|
| `open-issue <project_id> <title> [--description=...]` | A | agent 主动开 Issue（[conventions § 1](../../rules/conventions.md) 单一来源；worker 不允许造 Task，只能开 Issue 走讨论）|

### 8.3 Workforce

| 命令 | audience | 简述 |
|---|---|---|
| `worker enroll [--name=...] [--capacity=...]` | U | worker 首次注册到 center；获 worker_id + token |
| `worker run [--config=...]` | U | 启动 worker daemon（同 `agent-center worker`，便捷别名）|
| `worker list [--status=...]` | U / S | 列所有 worker + 状态 |
| `worker status <worker_id>` | U / S | 单个 worker 详情 |
| `worker shim <execution_id>` | Sys | per-execution shim 进程（[ADR-0018](../decisions/0018-detached-agent-via-per-execution-shim.md)），用户不直接调 |
| `worker proposal list [--status=pending\|...] [--worker=<id>]` | U | 列 worker 自发现的 project proposal |
| `worker proposal show <proposal_id>` | U | proposal 详情 |
| `worker proposal accept <proposal_id> [--project-id=<slug>] [--name=...]` | U | 接受 proposal → 创建 WorkerProjectMapping |
| `worker proposal ignore <proposal_id>` | U | 拒绝 proposal |
| `worker proposal unignore <proposal_id>` | U | 撤销 ignore，重新进 pending |
| `project add <slug> --path=<path> [--default-agent=...]` | U | 用户直接登记一个 project |
| `project list` | U / S | 全部 project |
| `project show <project_id>` | U / S | project 详情 + mappings |
| `project update <project_id> [--default-agent=...] [--name=...]` | U | 修改 project 元数据 |
| `project remove <project_id>` | U | 删除 project（连带清理 mapping）|

### 8.4 Conversation

| 命令 | audience | 简述 |
|---|---|---|
| `conversation add-message <conversation_id> --kind=... --content=... [--actor=...] [--input-request-ref=<id>]` | U / S / A / Sys | 写一条 Message <!-- v1 ref: "Bridge 自动外发" v2 删 per ADR-0031 -->|
| `conversation list [--kind=task\|issue\|dm\|...] [--owner=...]` | U / S | 列 conversation |
| `conversation read <conversation_id> [--since=...] [--limit=...]` | U / S | 读消息时间线 |

### 8.5 Cognition

| 命令 | audience | 简述 |
|---|---|---|
| `supervisor retrigger <invocation_id>` | U | 人工重新激活失败 / 超时的 invocation（[ADR-0013](../decisions/0013-supervisor-invocation-concurrency.md)）|
| `record-decision --invocation=<id> --rationale=... --actions=...` | S | supervisor 决策落库 + 同事务 emit events |
| ~~`escalate-input-request <input_request_id>`~~ | ~~S~~ | ~~跨 BC 命令 — supervisor 把 agent 的 InputRequest 升级到飞书~~ (v2 删 per ADR-0031) |

### 8.6 ~~Bridge~~ (v2 删 per ADR-0031)

| 命令 | audience | 简述 |
|---|---|---|
| ~~`identity add --vendor=feishu --vendor-id=... --bind-to=<user_id>`~~ | ~~U~~ | ~~把 vendor 身份绑到 center user~~ (v2 删 per ADR-0031) |
| ~~`identity list [--vendor=...]`~~ | ~~U~~ | ~~列所有身份映射~~ (v2 删 per ADR-0031) |
| ~~`identity bind <identity_id> --user=<user_id>`~~ | ~~U~~ | ~~修改已存在身份的归属~~ (v2 删 per ADR-0031) |
| ~~`identity unbind <identity_id>`~~ | ~~U~~ | ~~解绑~~ (v2 删 per ADR-0031) |
| ~~`bridge feishu setup [--app-id=...] [--app-secret=...]`~~ | ~~U / Sys~~ | ~~飞书 vendor 初始化（写配置 + 验证 WebSocket）~~ (v2 删 per ADR-0031) |

### 8.7 Observability — Open Host 五动词 + peek

跨 BC 横扫的统一查询面（[observability § 7.1](../architecture/tactical/observability/00-overview.md)）：

| 命令 | audience | 简述 |
|---|---|---|
| `inspect <kind> <id> [--format=...]` | U / S | 单实体详情；`kind ∈ task / execution / worker / issue / supervisor / conversation / input_request / project / worktree / decision` |
| `query <resource> [--filter=...] [--since=...] [--limit=...] [--cursor=...]` | U / S | 列表查询；`resource ∈ tasks / events / issues / mappings / proposals / ...` |
| `ps [--watch] [--project=...]` | U / S | fleet view 实时聚合（worker × execution × current_activity） |
| `stats [--scope=...] [--since=...]` | U / S | 系统级统计（吞吐 / 失败率 / 平均时长 等） |
| `logs <kind> <id> [--follow] [--since=...]` | U / S | task / execution log 流（含 BlobStore 归档）|
| `peek-trace <execution_id> [--last=N] [--kind=tool_call\|thinking\|tool_result\|all]` | U / S | 实时窥视 agent trace（worker daemon RPC 透传；[ADR-0015 § 4](../decisions/0015-agent-trace-not-in-events-table.md)）|

### 8.8 System / 运行模式

非任何 BC，是 binary 的 mode + 运维入口：

| 命令 | audience | 简述 |
|---|---|---|
| `agent-center server [--config=...]` | U | center 常驻；启动时自动 `migrate.Up`（[02-persistence-schema § 6](02-persistence-schema.md)）|
| `agent-center supervisor --scope=<scope> [--invocation-id=...] [--trigger-events=...]` | Sys | center 内部 spawn 的 supervisor invocation；用户不直接调 |
| `agent-center worker [--config=...]` | U | worker daemon 常驻（同 `worker run`）|
| `agent-center migrate [--target=<version>]` | U | 手动跑 migration（fail-fast 调试用）|
| `agent-center admin blob-migrate --from=<store> --to=<store>` | U | BlobStore 后端切换数据迁移 |
| `agent-center version` | U | 版本号 / git commit / build time |

---

## § 9. 代表性详细签名（5 条）

详细展开 5 条**业务路径有 trick** 的命令；其余按 § 8 简述 + § 1-7 元层规则套用即可。

### 9.1 `task create` — 跨聚合双写

```
agent-center task create <project_id> <title> [--description=...] [--parent=<task_id>] [--from-issue=<issue_id>] [--no-conversation]
```

| flag | 用途 |
|---|---|
| `--description` | task 描述 |
| `--parent` | sub-task 时填 parent_task_id |
| `--from-issue` | issue conclude spawn 路径填 from_issue_id |
| `--no-conversation` | 跳过同事务建 Conversation（b/c/d 路径，[ADR-0017 § 1](../decisions/drafts/0039-conversation-business-model-v2-unified.md)）<!-- v1 ref: ADR-0017 superseded by ADR-0039 -->；默认会同事务建 |

**实现路径**（同事务）：

1. 应用层 `TaskService.Create` 拿 ctx → `BeginTx` → `WithTx(ctx, tx)`
2. `taskRepo.Save(txCtx, task)` — INSERT tasks
3. 除非 `--no-conversation`：`convRepo.Save(txCtx, conv)` + 回填 `task.conversation_id`
4. `eventSink.Emit(txCtx, "task.created", refs={task_id, project_id, conversation_id?})`
5. `Commit`

**输出**（`--format=json`）：

```json
{"task_id":"01H...","conversation_id":"01H...","version":1}
```

### 9.2 `inspect execution <id>` — 跨表 JOIN

```
agent-center inspect execution <execution_id> [--include-projection] [--include-artifacts] [--format=...]
```

业务列归 TaskRuntime（`task_executions`），projection 列归 Observability（`task_execution_projections`，[02-persistence-schema § 8.2](02-persistence-schema.md)）。

**实现路径**：

1. `taskExecutionRepo.FindByID(ctx, id)` — SELECT task_executions
2. 若 `--include-projection`：`projectionRepo.FindByID(ctx, id)` — SELECT task_execution_projections（PK 1:1 JOIN）
3. 若 `--include-artifacts`：`artifactRepo.FindByExecutionID(ctx, id)`
4. 应用层组装返回 struct

> 用 multi-roundtrip 不用 SQL JOIN：保 BC 物理隔离（[conventions § 9.z](../../rules/conventions.md)），代价是 SQLite 同库多 round trip，可忽略。

### 9.3 `worker proposal accept` — 跨 BC 状态推进

```
agent-center worker proposal accept <proposal_id> [--project-id=<slug>] [--name=...]
```

| flag | 用途 |
|---|---|
| `--project-id` | 显式指定 project slug（已存在则关联，未存在则新建）|
| `--name` | project name（仅 `--project-id` 是新 slug 时用）|

**实现路径**（跨聚合，同事务）：

1. `proposalRepo.FindByID(ctx, id)` — verify proposal 在 pending
2. 检查 / 创建 Project（若 `--project-id` 是新 slug）
3. `mappingRepo.Save(txCtx, mapping)` — INSERT WorkerProjectMapping
4. `proposalRepo.UpdateStatus(txCtx, id, pending→accepted, version)` — CAS
5. `eventSink.Emit(txCtx, "worker_project_proposal.accepted", ...)` + `worker_project_mapping.created`
6. Commit

**失败 exit code**：

- 16 — proposal version conflict（被其它路径改了）
- 18 — proposal 已不在 pending（已 accept / ignore / superseded）
- 19 — invariant 违反（同 worker + path 已有 mapping）

### 9.4 `agent-center server` — 启动流程

```
agent-center server [--config=<path>] [--migrate-only]
```

启动顺序（fail-fast，任何步骤失败立即 panic 退出）：

1. 解析 config（默认 `/etc/agent-center/config.yaml`）
2. 打开 SQLite（DSN 见 [02-persistence-schema § 1.2](02-persistence-schema.md)）
3. `migrate.Up`（自动跑 embed 的 migration FS）；`--migrate-only` 则做完退出
4. 初始化 BlobStore（按 config）
5. 启动 gRPC worker 端口
6. 启动 supervisor invocation 触发器（events 驱动 spawn）
7. ~~启动 Bridge 长连接（飞书 WebSocket）~~ (v2 删 per ADR-0031)
8. ready；接受请求

### 9.5 `peek-trace` — agent trace 实时窥视

```
agent-center peek-trace <execution_id> [--last=N] [--kind=tool_call|thinking|tool_result|all] [--follow]
```

- worker daemon 端 RPC 透传：center 收请求 → 转 worker daemon → 读本地 `trace.jsonl`（当前 execution shim 的） → 流回 center → 流回用户
- 不依赖归档；execution 进行中即可窥视
- `--follow` 模式：daemon 把后续 trace 实时推回
- audience 主要是 supervisor 决策时按需深挖（[ADR-0015 § 4](../decisions/0015-agent-trace-not-in-events-table.md)）；用户也可用

---

## § 10. 与 P8a 各 BC Repository 的对位

| BC | Repository（P8a） | 主要 CLI 入口（§ 8） |
|---|---|---|
| TaskRuntime | TaskRepository | `task create / bind-conversation` / `inspect task` / `query tasks` |
| TaskRuntime | TaskExecutionRepository | `dispatch` / `kill-execution` / `inspect execution` / `report-failure` |
| TaskRuntime | InputRequestRepository | `request-input` / `escalate-input-request` / `inspect input_request` |
| TaskRuntime | ArtifactRepository | `report-artifact` |
| Discussion | IssueRepository | `issue open / comment / conclude / link-conversation` / `open-issue` |
| Workforce | WorkerRepository | `worker enroll / run / list / status` |
| Workforce | WorkerProjectMappingRepository | `worker proposal accept`（创建 mapping）|
| Workforce | WorkerProjectProposalRepository | `worker proposal list / show / accept / ignore / unignore` |
| Workforce | ProjectRepository | `project add / list / show / update / remove` |
| Conversation | ConversationRepository | `conversation add-message / list / read` |
| Cognition | SupervisorInvocationRepository | `supervisor retrigger` / `inspect supervisor` |
| Cognition | DecisionRecordRepository | `record-decision` / `inspect decision` |
| ~~Bridge~~ | ~~FeishuDeliveryLedgerRepository~~ | ~~`bridge feishu setup`（内部表初始化）/ `identity *`~~ (v2 删 per ADR-0031) |
| Observability | EventRepository | `query events` / `inspect <kind>` 的事件流字段 |
| Observability | TaskExecutionProjectionRepository | `inspect execution --include-projection` / `ps` |
| Observability | TraceArchiveRepository | `peek-trace` / `logs` 归档段 |

---

> **本文档 scope**：v1 实现层 CLI 元层规则 + 全量命令清单（按 BC 分组）+ 5 条代表性详细签名。每条命令的完整 flag / arg / 输出 schema 落代码时以 `--help` 文本 + 集成测试为准；新增 audience 含 S / A 的命令时同步更新 `supervisor.md` / `worker-agent.md` skill 文档（CI 校验）。
