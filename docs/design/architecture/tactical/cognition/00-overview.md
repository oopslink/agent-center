# Cognition BC — DDD 战术设计 Overview

> **DDD 战术层** · BC: Cognition
>
> Memory + Reminder + WakeGuard。当前 Cognition BC 聚焦于 file-based Memory 管理 + Agent 提醒调度 + 唤醒边界防护。

---

## § 0. BC 一览

### 0.1 职责

| 维度 | 内容 |
|---|---|
| **聚合管理** | Memory（独立 AR，file-based）/ Reminder（独立 AR，[03-reminder.md](03-reminder.md)）|
| **Memory 管理** | file-based；ancestor walk 自动加载；零 SQL 表 |
| **Reminder** | 一次性 / cron 定时提醒，到点唤醒目标 agent 并注入提醒文本（[03-reminder.md](03-reminder.md)）|
| **WakeGuard** | 唤醒边界防护（[04-wake-guardrail.md](04-wake-guardrail.md)）|

### 0.2 UL 切片

来自 [strategic/03-bounded-contexts § 1](../../strategic/03-bounded-contexts.md) 标 Cognition 上下文的术语：

- `Memory`（聚合根，独立；file-based）
- `Reminder`（聚合根，独立；定时唤醒 agent）

### 0.3 Context Map 位置

[strategic/03-bounded-contexts § 3](../../strategic/03-bounded-contexts.md)。

---

## § 1. 聚合清单（X.1）

### 1.1 Aggregate Roots

| 聚合 | 文件 | 状态机 | 身份 / 不变性 |
|---|---|---|---|
| **Memory** | [02-memory.md](02-memory.md) | 无状态机（CLAUDE.md 文件 + git commit 历史承担演进）| 由文件路径定位：`$MEMORY_DIR/{scope_path}/CLAUDE.md` |
| **Reminder** | [03-reminder.md](03-reminder.md) | 5 态（active / paused / cancelled / completed / expired）| ULID；owner + agent 可操作 |

---

## § 2. Invariants 索引（X.2）

每个聚合自己维护 invariants 节，本 § 仅做索引：

- **Memory Invariants** → [02-memory.md § 5](02-memory.md)
- **Reminder Invariants** → [03-reminder.md](03-reminder.md)

---

## § 3. Domain Services（X.3）

---

## § 4. Factories（X.4）

### 4.1 MemorySkeletonFactory

**职责**：订阅 lifecycle 事件，立即创建 scope 对应的 `CLAUDE.md` 空骨架 + git commit（author=`system:bootstrap`）。

| 触发事件 | 创建文件 |
|---|---|
| `project.created` | `projects/<id>/CLAUDE.md` |
| `task.created` | `projects/<X>/tasks/<id>/CLAUDE.md` |
| `issue.opened` | `projects/<X>/issues/<id>/CLAUDE.md` |
| `worker.enrolled` | `workers/<id>/CLAUDE.md` |
| `conversation.opened` | `conversations/<id>/CLAUDE.md` |

详见 [02-memory § 4 冷启动骨架](02-memory.md)。

---

## § 5. Repositories（X.5）

接口签名（Go-style，含 `ctx context.Context` 参数；架构层契约，跟实现解耦）：

### 5.1 MemorySkeletonFactory + MemoryGitOpsService（Memory 特殊路径）

Memory **不走标准 Repository 抽象** —— Memory 物理形态是 `$AGENT_CENTER_MEMORY_DIR/` git 仓，由 supervisor 用 claude 原生 `Edit` / `Write` 工具直接读写（[ADR-0012](../../../decisions/0012-memory-file-based.md)）。但需要两个架构层契约：

**MemorySkeletonFactory**（cold-start 时建空骨架）：

```go
type MemorySkeletonFactory interface {
    // CreateSkeleton 建空 CLAUDE.md + git commit (author=system:bootstrap)
    CreateSkeleton(ctx context.Context, scopeKind ScopeKind, scopeKey string) error
}
```

**MemoryGitOpsService**（invocation 退出时 center 兜底 commit）：

```go
type MemoryGitOpsService interface {
    // AutoCommitDirty 检查仓内 dirty 变化 → git add -A && git commit (author=supervisor:<inv-id>)
    AutoCommitDirty(ctx context.Context, invocationID InvocationID, scopeKind ScopeKind, scopeKey string) error
}
```

**Domain errors**：

```go
var (
    ErrMemoryDirNotInitialized = errors.New("cognition: memory dir not initialized as git repo")
    ErrMemoryFileExists        = errors.New("cognition: memory file already exists (skeleton)")
    ErrMemoryGitOpFailed       = errors.New("cognition: memory git operation failed")
)
```

### 5.2 约定

- Memory **不**走传统 Repository 抽象 —— 直接走 file ops；但 SkeletonFactory + GitOpsService 是架构层契约
- Repository 是**领域层抽象接口**；实现层落到 [implementation/02-persistence-schema.md](../../../implementation/)（Memory 物理形态见 [ADR-0012](../../../decisions/0012-memory-file-based.md)）
- Domain errors 用 sentinel error pattern；调用方用 `errors.Is` 判定

---

## § 8. Out-of-Scope / Future Work

| 项 | 归属 |
|---|---|
| Memory 并发写 advisory lock | [roadmap](../../../roadmap.md) |
| Memory 跨 BC 聚合查询（grep 工具化、Web Console 可视）| [roadmap](../../../roadmap.md) |
| Memory file 体积监控告警（自动提醒 supervisor 压缩）| [roadmap](../../../roadmap.md) |

---

## § 9. References

### 相关 ADR

- [ADR-0002 不用 LLM SDK 走 CLI agent](../../../decisions/0002-no-llm-sdk-use-cli-agents.md)
- [ADR-0012 Memory file-based + git](../../../decisions/0012-memory-file-based.md)

### 战略层

- [strategic/03-bounded-contexts § 1 UL](../../strategic/03-bounded-contexts.md)（Cognition 上下文术语）
- [strategic/03-bounded-contexts § 2 BC4 Cognition](../../strategic/03-bounded-contexts.md)
- [strategic/03-bounded-contexts § 3 Context Map](../../strategic/03-bounded-contexts.md)

### 同 BC 内聚合详情

- [02-memory.md](02-memory.md) — Memory AR（file-based + git 仓）
- [03-reminder.md](03-reminder.md) — Reminder AR
- [04-wake-guardrail.md](04-wake-guardrail.md) — WakeGuard

### 跨 BC 协作文档

- [conversation/00-overview.md](../conversation/00-overview.md) — conversation.message_added 唤醒事件
- [observability/00-overview.md](../observability/00-overview.md) — events 表
- [agent-harness/01-prompt-assembly.md](../agent-harness/01-prompt-assembly.md) — Worker-side prompt 组装（跟本 BC 独立）

### 横切方法论

- [conventions](../../../../rules/conventions.md) § 0 DDD / § 1 无野任务 / § 2 可观测性 / § 8 BlobStore（prompt_blob_ref）/ § 16 reason+message
