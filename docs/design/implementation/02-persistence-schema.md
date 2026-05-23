> 📌 **v2 update applied (P12 S6, 2026-05-24)** — v2 撤回了 Bridge BC + 飞书集成 (per ADR-0031)；ADR-0017/0021/0022 superseded by ADR-0039. v1 strikethrough-vendor 行块已在本次 sweep 中删除 / 改写；剩余 vendor / Bridge / 飞书 引用作 historical context 保留。当前 active 设计以 ADR + decisions/README 为准。

# 持久化 schema

> **实现层** · 把 [P8a](../ddd-blueprint.md) 各 BC § 5 的 Repository 接口绑到 SQLite。
>
> v1 默认嵌入式 SQLite（[domain-vision § B2](../architecture/strategic/00-domain-vision.md)），单文件、零运维。本文档**仅元层规则 + 代表性 BC 切片**；其余 BC 的完整 schema 推迟到落代码时以 migration SQL 为准。

## § 1. SQLite dialect 落地

### 1.1 Driver

选用 **`modernc.org/sqlite`**（纯 Go port）：

- 纯 Go，无 CGO，**单 binary cross-compile 友好**（与 [conventions § 10](../../rules/conventions.md) 一致）
- API 与 `database/sql` 一致

不选 `mattn/go-sqlite3`（CGO 依赖 GCC，跨平台部署麻烦）。

### 1.2 连接参数

DSN 必带：

```
file:agent-center.db?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON&_synchronous=NORMAL
```

| 参数 | 值 | 理由 |
|---|---|---|
| `_journal_mode` | `WAL` | 写不阻塞读；worker daemon 高频 push projection 关键 |
| `_busy_timeout` | `5000` (ms) | 写锁等待；避免 `SQLITE_BUSY` 短路 |
| `_foreign_keys` | `ON` | SQLite 默认关 FK；显式开启确保引用完整性 |
| `_synchronous` | `NORMAL` | WAL 模式下足够安全 |

### 1.3 兼容性立场（v1）

[conventions § 9](../../rules/conventions.md) 要求"贴 SQL 共同子集"。本文档对此的具体应对：

- **写法**仍贴 [§ 9.0 禁忌清单](../../rules/conventions.md)（`TEXT` 存时间戳 / ULID / JSON；ULID 字符串 PK；`INTEGER` 0/1 当 boolean）—— 这本身就保住了大部分可移植性
- **v1 不引入 dialect 抽象层** / **不跑 PG CI**（[§ 9.2 "CI 双引擎跑"](../../rules/conventions.md) 在 v1 暂不强制）。切 PG 时按"重做"处理而非平滑迁移
- 实现仅依赖 `database/sql` + `modernc.org/sqlite`，无 ORM

---

## § 2. ID 生成（ULID）

库：**`github.com/oklog/ulid/v2`**

- 26 字符 Crockford Base32（如 `01ARZ3NDEKTSV4RRFFQ69G5FAV`），存为 `TEXT PRIMARY KEY`
- 单调生成器（`MonotonicEntropy`）保证同毫秒内严格递增
- 字典序 ≈ 时间序，满足 [observability § 1.1](../architecture/tactical/observability/00-overview.md) cursor 假设

例外：**`Project.id` 是用户输入的 slug**（[workforce/02-project § 2](../architecture/tactical/workforce/02-project.md)），TEXT PK，应用层校验 slug 格式。

---

## § 3. 时间 / Boolean / JSON 编码

| 维度 | 编码 | 例 |
|---|---|---|
| 时间戳 | `TEXT` 存 ISO 8601（UTC，`Z` 后缀） | `2026-05-20T10:23:00.123Z` |
| Boolean | `INTEGER` 0 / 1 | `is_active INTEGER NOT NULL DEFAULT 0` |
| JSON | `TEXT` 存 marshal 后字符串；**不在 SQL 里 `json_extract`** | `refs TEXT NOT NULL` 存 `{"task_id":"...","worker_id":"..."}` |
| Enum | `TEXT` + 应用层校验 | `status TEXT NOT NULL` 值域闭集 |

时间戳由应用层 `time.Now().UTC().Format(time.RFC3339Nano)` 生成；SQLite 字符串比较与字典序一致，可直接 `ORDER BY occurred_at` 排序。

---

## § 4. 乐观锁 SQL 模板（version CAS）

P8a 各 Repository 的 `Update*` 方法带 `version int` 参数（[conventions § 9.0](../../rules/conventions.md) 禁用行级锁的替代）。模板：

```sql
UPDATE <table>
SET <updated_cols...>, version = version + 1, updated_at = ?
WHERE id = ? AND version = ?
RETURNING version;
```

实现层判定：

- `RowsAffected == 0` → 返回 `Err*VersionConflict`
- `RowsAffected == 1` → 成功，新 `version` 通过 `RETURNING` 拿回

`Save` 语义是"新建 + 全量更新"：

- 新建走 `INSERT INTO <table> (..., version) VALUES (..., 1)`
- 更新走上述 CAS 模板（caller 持有当前 version 字段）

> SQLite 3.35+ 支持 `RETURNING`；`modernc.org/sqlite` 内置 SQLite 3.40+。

---

## § 5. Tx via ctx（WithTx / TxFromCtx）

P8a 接口签名一律 `(ctx context.Context, ...)`，**不带显式 tx 参数**。tx 通过 ctx 传递：

```go
// internal/persistence/tx.go
package persistence

type txKey struct{}

func WithTx(ctx context.Context, tx *sql.Tx) context.Context {
    return context.WithValue(ctx, txKey{}, tx)
}

func TxFromCtx(ctx context.Context) (*sql.Tx, bool) {
    tx, ok := ctx.Value(txKey{}).(*sql.Tx)
    return tx, ok
}

// Repository helper：根据 ctx 选 Tx 或 DB
type sqlExecutor interface {
    ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
    QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
    QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (r *taskRepo) executor(ctx context.Context) sqlExecutor {
    if tx, ok := TxFromCtx(ctx); ok {
        return tx
    }
    return r.db
}
```

application service 跨聚合写：

```go
func (s *TaskService) CreateTaskAndConversation(ctx context.Context, ...) error {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil { return err }
    defer tx.Rollback() // 幂等

    txCtx := persistence.WithTx(ctx, tx)
    if err := s.taskRepo.Save(txCtx, task); err != nil { return err }
    if err := s.convRepo.Save(txCtx, conv); err != nil { return err }
    if err := s.eventSink.Emit(txCtx, evt); err != nil { return err }
    return tx.Commit()
}
```

约束：

- Repository 实现层**禁止**直接拿 `*sql.DB` 跑 INSERT/UPDATE；必须先 `executor(ctx)` 取
- ctx 内无 tx 时退化到连接池（独立事务，单 Repository 简单查询用）
- **Repository 方法内不允许 `BeginTx`**：tx 边界归 application / domain service

---

## § 6. Migration 工具（golang-migrate embed FS）

库：**`github.com/golang-migrate/migrate/v4`**

文件布局：

```
internal/persistence/migrations/
  ├─ 0001_init.up.sql
  ├─ 0001_init.down.sql
  ├─ 0002_xxx.up.sql
  ├─ 0002_xxx.down.sql
  └─ ...
```

注入：

```go
//go:embed migrations/*.sql
var migrationsFS embed.FS
```

按 [conventions § 9.1](../../rules/conventions.md)：

- 仅 additive（加列 / 加表 / 加索引）
- 删列 / 改类型 → 新表 + copy + rename 两步迁移
- down migration 在 v1 仅做最小可逆性

`agent-center server` 启动自动 `migrate.Up`（fail-fast）。

---

## § 7. 表 / 列命名约定

| 维度 | 约定 | 例 |
|---|---|---|
| 表名 | 复数 / snake_case | `tasks`, `task_executions`, `events`, `task_execution_projections` |
| 列名 | snake_case；外键 `<entity>_id` | `task_id`, `worker_id`, `conversation_id` |
| 时间列 | `created_at` / `updated_at` / 业务时间 `<event>_at` | `occurred_at`, `dispatched_at`, `cancel_requested_at` |
| 状态列 | `status`（业务状态机） vs `state`（子状态如 dispatch_state） | `status`, `dispatch_state` |
| Reason / message | 终态列分别 `<state>_reason` / `<state>_message` **平铺**；不组成 JSON（[§ 16](../../rules/conventions.md)） | `completed_reason`, `completed_message` |
| 主键 | `id TEXT PRIMARY KEY` | ULID 字符串 |
| 乐观锁 | `version INTEGER NOT NULL DEFAULT 1` | |
| 索引 | `idx_<table>_<cols>` / `uniq_<table>_<cols>`；列顺序与索引名一致 | `idx_task_executions_worker_status` |

---

## § 8. BC 实现切片（代表性 BC：TaskRuntime + Observability）

其余 5 BC（Cognition / Workforce / Discussion / Conversation / ~~Bridge (v2 deleted per ADR-0031)~~）的 DDL 不在 P8b 展开；落代码时按 § 1-7 套用即可。出现新 schema pattern 时回 P8b 补元层规则。

### 8.1 TaskRuntime

#### 8.1.1 DDL

```sql
-- tasks ---------------------------------------------------------------
CREATE TABLE tasks (
    id                       TEXT PRIMARY KEY,
    project_id               TEXT NOT NULL,
    parent_task_id           TEXT,
    from_issue_id            TEXT,
    title                    TEXT NOT NULL,
    description              TEXT NOT NULL DEFAULT '',
    status                   TEXT NOT NULL,
    conversation_id          TEXT,                       -- 1:1 强引用（ADR-0017，superseded by ADR-0039；v2 待 Phase 10 重写）
    current_execution_id     TEXT,
    created_at               TEXT NOT NULL,
    updated_at               TEXT NOT NULL,
    version                  INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_tasks_project_status ON tasks (project_id, status);
CREATE INDEX idx_tasks_parent ON tasks (parent_task_id) WHERE parent_task_id IS NOT NULL;

-- task_executions -----------------------------------------------------
CREATE TABLE task_executions (
    id                          TEXT PRIMARY KEY,
    task_id                     TEXT NOT NULL,           -- 强引用（ADR-0010）
    worker_id                   TEXT NOT NULL,           -- 强引用 / 不可变
    status                      TEXT NOT NULL,           -- submitted/working/.../completed/failed/killed
    dispatch_state              TEXT NOT NULL,           -- pending_ack/acked/...（与 status 正交，ADR-0011）
    pending_input_request_id    TEXT,
    cancel_requested_at         TEXT,
    cancel_reason               TEXT,
    cancel_message              TEXT,
    completed_reason            TEXT,
    completed_message           TEXT,
    failed_reason               TEXT,
    failed_message              TEXT,
    killed_reason               TEXT,
    killed_message              TEXT,
    created_at                  TEXT NOT NULL,
    updated_at                  TEXT NOT NULL,
    version                     INTEGER NOT NULL DEFAULT 1
    -- task_id 语义上强引用 tasks.id；引用完整性由 TaskExecutionRepository / DispatchService 保证（conventions § 9.w，不声明 FK）
);
CREATE INDEX idx_task_executions_task ON task_executions (task_id);
CREATE INDEX idx_task_executions_worker_status ON task_executions (worker_id, status);
CREATE INDEX idx_task_executions_active ON task_executions (status)
  WHERE status IN ('submitted', 'working', 'input_required');
```

#### 8.1.2 Repository → SQL 映射

仅展开有 trick 的方法；纯 CRUD（`FindByID` / `Save` 新建 / `FindByProject` / 等）直白对位 `SELECT` / `INSERT` / `WHERE`，略。

**TaskExecutionRepository.UpdateStatus**（CAS + 状态机校验）：

```sql
UPDATE task_executions
SET status = ?, updated_at = ?, version = version + 1
WHERE id = ?
  AND status = ?        -- from 状态校验
  AND version = ?
RETURNING version;
```

`from` 不匹配 / `version` 不匹配都通过 `RowsAffected == 0` 体现 → 默认返回 `ErrTaskExecutionVersionConflict`，调用方决定是否重试。

**TaskExecutionRepository.UpdateCompleted / UpdateFailed / UpdateKilled**（终态）：同时写 `status` + `*_reason` + `*_message` + version CAS：

```sql
UPDATE task_executions
SET status = 'completed',
    completed_reason = ?,
    completed_message = ?,
    updated_at = ?,
    version = version + 1
WHERE id = ?
  AND version = ?
  AND status IN ('working', 'input_required')
RETURNING version;
```

**TaskRepository.FindBlockedBy** — task A 阻塞了哪些 task：v1 schema 无显式 blockers 列，**实现 TBD**（blocker 关系数据建模未定，可能用 task 内 JSON `blocker_task_ids` 字段或独立 join 表）。

#### 8.1.3 关键实现要点

- **task + conversation 同事务双写**（[ADR-0017](../decisions/0039-conversation-business-model-v2-unified.md) a/e 路径）：application service 用 § 5 tx-via-ctx 模板；`tasks.conversation_id` 在创建即填 <!-- v1 ref: ADR-0017 superseded by ADR-0039 -->
- **CAS 重试边界**：Repository 层**不重试**；返回 `*VersionConflict` 后由 caller（通常 supervisor）决定。避免 Repository 内置 retry 与 application 层重试策略叠加
- **dispatch_state vs status**：两个状态机正交（[ADR-0011](../decisions/0011-dispatch-reliability-protocol.md)），各自 UPDATE 各自列；dispatch_state 不参与 status CAS 校验

---

### 8.2 Observability

#### 8.2.1 DDL

```sql
-- events --------------------------------------------------------------
CREATE TABLE events (
    id              TEXT PRIMARY KEY,            -- ULID
    occurred_at     TEXT NOT NULL,               -- ISO 8601（业务时间）
    seq             INTEGER NOT NULL,            -- 单调递增（per partition 或全局，应用层维护）
    event_type      TEXT NOT NULL,               -- <bc>.<entity>.<action>
    refs            TEXT NOT NULL DEFAULT '{}',  -- JSON：{task_id?, execution_id?, ...}
    actor           TEXT NOT NULL,               -- user:hayang / supervisor:inv-id / worker:W-1 / system
    payload         TEXT NOT NULL DEFAULT '{}',  -- JSON：event-specific
    correlation_id  TEXT,
    decision_id     TEXT,                        -- supervisor 决策触发事件时填
    created_at      TEXT NOT NULL                -- INSERT 时刻（系统时间，可 ≠ occurred_at）
);
-- append-only：无 version 列、无 UPDATE 路径
CREATE INDEX idx_events_occurred_at ON events (occurred_at);
CREATE INDEX idx_events_type ON events (event_type);
CREATE INDEX idx_events_correlation ON events (correlation_id) WHERE correlation_id IS NOT NULL;
CREATE INDEX idx_events_decision ON events (decision_id) WHERE decision_id IS NOT NULL;

-- task_execution_projections -----------------------------------------
-- BC: Observability（conventions § 9.z 物理隔离；与 task_executions PK 1:1）
CREATE TABLE task_execution_projections (
    task_execution_id              TEXT PRIMARY KEY,    -- = task_executions.id
    current_activity               TEXT,                -- 人话描述："正在分析 src/foo.go"
    current_activity_at            TEXT,
    total_tool_calls               INTEGER NOT NULL DEFAULT 0,
    total_tokens_input             INTEGER NOT NULL DEFAULT 0,
    total_tokens_output            INTEGER NOT NULL DEFAULT 0,
    working_seconds_accumulated    INTEGER NOT NULL DEFAULT 0,
    last_push_at                   TEXT NOT NULL
    -- task_execution_id PK 也是引用 task_executions.id（1:1）；引用完整性由 TaskExecutionProjectionRepository UPSERT 路径保证（conventions § 9.w，不声明 FK）
);
CREATE INDEX idx_proj_last_push ON task_execution_projections (last_push_at DESC);
```

#### 8.2.2 Repository → SQL 映射

**EventRepository.Append**（append-only INSERT）：

```sql
INSERT INTO events (id, occurred_at, seq, event_type, refs, actor, payload, correlation_id, decision_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
```

`seq` 由应用层维护 monotonic counter（单 center 单进程，内存 atomic + 启动时 `SELECT MAX(seq) FROM events` 初始化）。

**TaskExecutionProjectionRepository.UpdateProjection**（worker daemon push 路径，UPSERT）：

```sql
INSERT INTO task_execution_projections (
    task_execution_id, current_activity, current_activity_at,
    total_tool_calls, total_tokens_input, total_tokens_output,
    working_seconds_accumulated, last_push_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (task_execution_id) DO UPDATE SET
    current_activity = excluded.current_activity,
    current_activity_at = excluded.current_activity_at,
    total_tool_calls = excluded.total_tool_calls,
    total_tokens_input = excluded.total_tokens_input,
    total_tokens_output = excluded.total_tokens_output,
    working_seconds_accumulated = excluded.working_seconds_accumulated,
    last_push_at = excluded.last_push_at;
```

**不走 version CAS** —— projection 是高频覆盖的派生数据，多写一次旧值不损 invariant。

**EventRepository.QueryByRef / QueryByTimeWindow**：append-only 流用 ULID `id` 当稳定 cursor（与 `occurred_at` 字典序兼容）：

```sql
SELECT * FROM events
WHERE occurred_at >= ? AND occurred_at < ?
  AND (? = '' OR id > ?)         -- cursor，空串 = 首页
ORDER BY id ASC
LIMIT ?;
```

#### 8.2.3 关键实现要点

- **events 同事务双写**（[ADR-0014 § 2](../decisions/0014-event-sourcing-level.md)）：状态表 UPDATE + events INSERT 必须同 tx；`EventSink` domain service 暴露给各 BC，内部接 `EventRepository.Append`
- **events.refs JSON 形状固定**：`{task_id?, execution_id?, input_request_id?, issue_id?, worker_id?, ...}`；**不查 JSON 内容**（§ 9.0）；按某一 ref 反查时走 `payload` 反范式列（未来扩展点，例如新增 `task_id_indexed TEXT GENERATED ALWAYS AS (json_extract(refs,'$.task_id')) STORED` —— 此为 SQLite-only 特性，v1 不上）
- **projection 表 不加 ON DELETE CASCADE**：task_execution 终态后 v1 不删 / 不 GC；projection 行清理留待运营脚本
- **BlobStore 路径列**（`*_blob_path`）落在 task / task_execution 哪一表的归属，BlobStore doc 与各 BC overview 之间有 ambiguity；P8b 不展开该字段，落代码时统一（[01-blob-store § 路径约定](01-blob-store.md)）

---

## § 9. 与 P8a Repository 接口的对位

| BC | Repository（P8a 接口） | 物理表（P8b） | 关键 SQL |
|---|---|---|---|
| TaskRuntime | TaskRepository | `tasks` | CAS UPDATE |
| TaskRuntime | TaskExecutionRepository | `task_executions` | CAS + 状态机校验 + 终态 reason+message |
| TaskRuntime | InputRequestRepository | `input_requests`（DDL 落代码时给） | CAS |
| TaskRuntime | ArtifactRepository | `artifacts`（DDL 落代码时给） | append-only INSERT |
| Observability | EventRepository | `events` | append-only INSERT |
| Observability | TaskExecutionProjectionRepository | `task_execution_projections` | UPSERT（不走 CAS） |
| Observability | TraceArchiveRepository | **不在 DB** | 见 [01-blob-store](01-blob-store.md) |
| Cognition / Workforce / Discussion / Conversation / ~~Bridge (v2 删 per ADR-0031)~~ | 各 § 5 Repository | DDL 落代码时按 § 1-7 套用 | - |

---

> **本文档 scope**：v1 实现层规则 + 代表性 BC 切片。其余 BC 完整 DDL 落代码时以 migration SQL 为准。出现新 schema pattern（新编码 / 新锁模式 / 新 tx 边界）时回本文档补 § 1-7。
>
> **历史**：2026-05-20 P8b 首版 —— [conventions § 9.z](../../rules/conventions.md) BC 物理隔离生效，`task_execution_projections` 从 `task_executions` 拆出。
