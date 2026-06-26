> 📌 **v2 update applied (P12 S6, 2026-05-24)** — v2 撤回了 Bridge BC + 飞书集成 (per ADR-0031)；ADR-0017/0021/0022 superseded by ADR-0039. v1 strikethrough-vendor 行块已在本次 sweep 中删除 / 改写；剩余 vendor / Bridge / 飞书 引用作 historical context 保留。当前 active 设计以 ADR + decisions/README 为准。

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

## § 8. 命令清单

> **v2.7 #162 RETIREMENT NOTICE (2026-06-26)**：v2.7 大幅精简 CLI surface。所有数据管理 + 数据查询命令已退役，管理功能迁移至 Web Console API。CLI 仅保留**部署 / 生命周期 / 运维**命令（约 9 个顶层 + 子命令）。

### 8.1 当前命令清单（v2.7+）

以下是 `internal/cli/build.go` 中实际注册的全部命令：

| 命令 | audience | 简述 |
|---|---|---|
| `agent-center version` | U | 版本号 / git commit / build time |
| `agent-center help` | U | 显示帮助信息 |
| `agent-center server [--config=...]` | U | center 常驻进程；启动时自动 `migrate.Up` |
| `agent-center migrate` | U | migration 命令组 |
| `agent-center migrate up` | U | 手动跑 migration（fail-fast 调试用）|
| `agent-center migrate v1-to-v2` | U | v1 → v2 数据迁移工具 |
| `agent-center admin backup` | U | SQLite 备份 |
| `agent-center admin blob-migrate` | U | BlobStore 后端切换数据迁移 |
| `agent-center admin token create\|list\|revoke` | U | Admin bearer token 管理（v2.3-3a）|
| `agent-center bootstrap [check-systemd]` | U | 首次安装检查（Phase 7）|
| `agent-center install center` | U | 安装 center（systemd/launchd + symlink，v2.4）|
| `agent-center install worker` | U | 安装 worker daemon |
| `agent-center install test-instance` | U | 创建隔离测试沙箱（1 center + N workers，v2.8 #255）|
| `agent-center uninstall center` | U | 卸载 center（v2.5.1）|
| `agent-center uninstall worker` | U | 卸载 worker daemon |
| `agent-center uninstall test-instance` | U | 拆除测试沙箱（v2.8 #255）|
| `agent-center upgrade center` | U | 升级 center binary（v2.5.2）|
| `agent-center upgrade worker` | U | 升级 worker binary |
| `agent-center worker enroll` | U | worker 首次注册到 center |
| `agent-center worker list` | U | 列所有 worker |
| `agent-center worker status <worker_id>` | U | 单个 worker 详情 |
| `agent-center worker run [--config=...]` | U | 启动 worker daemon |
| `agent-center worker shim <execution_id>` | Sys | per-execution shim 进程（用户不直接调）|
| `agent-center worker mcp-host` | Sys | per-agent stdio MCP server（v2.7 b3-i；daemon 内部 spawn）|
| `agent-center worker agent-supervisor` | Sys | 持久化 per-agent supervisor（v2.7 D2-f s1；daemon 内部 spawn）|
| `agent-center list-local-centers` | U | 列出本机所有 center 部署实例（v2.7.1 #211，多实例支持）|
| `agent-center list-local-workers` | U | 列出本机所有 worker 部署（prod + test，v2.8 #170）|
| `agent-center list-test-instances` | U | 列出测试沙箱（v2.8 #255）|

### 8.2 已退役命令（v2.7 #162）

以下命令组在 v2.7 #162 中**全部退役**，数据管理 / 数据查询功能迁移至 Web Console API（`/api/*` JSON endpoint + SSE）：

| 退役命令组 | 原 BC | 退役原因 |
|---|---|---|
| `project` (add/list/show/update/remove) | Workforce | Web Console API |
| `conversation` (add-message/list/read) | Conversation | Web Console API |
| `channel` | Conversation | Web Console API |
| `message` | Conversation | Web Console API |
| `agent` (create/list/...) | Agent | Web Console API |
| `secret` (create/list/...) | SecretManagement | Web Console API |
| `task create / bind-conversation / ...` | TaskRuntime | Web Console API（TaskRuntime BC 退役，由 ProjectManager BC 取代）|
| `issue open / comment / conclude / ...` | Discussion | Web Console API |
| `inspect / query / ps / stats / logs / peek-trace` | Observability | Web Console API |
| `request-input / report-progress / report-artifact / report-failure` | TaskRuntime | 由 Agent BC + MCP 通道取代 |
| `supervisor retrigger / record-decision` | Cognition | Web Console API |
| ~~`identity` / `bridge`~~ | ~~Bridge~~ | v2 ADR-0031 删除 |

> **注**：这些退役命令的 audience 含 `S` / `A` 的功能现在走 MCP server（`worker mcp-host`）和 admin HTTP endpoint，不再通过 CLI 子命令暴露。

---

## § 9. 代表性详细签名

> **v2.7 NOTE**: 原 § 9 的 5 条代表性签名中，`task create`、`inspect execution`、`worker proposal accept`、`peek-trace` 对应的 CLI 命令已在 v2.7 #162 退役。下面保留 `agent-center server` 启动流程（仍有效）+ `install center` 作为当前命令的代表性签名。

### 9.1 `agent-center server` — 启动流程

```
agent-center server [--config=<path>]
```

启动顺序（fail-fast，任何步骤失败立即 panic 退出）：

1. 解析 config（默认 `/etc/agent-center/config.yaml`）
2. 打开 SQLite（DSN 见 [02-persistence-schema § 1.2](02-persistence-schema.md)）
3. `migrate.Up`（自动跑 embed 的 migration FS）
4. 初始化 BlobStore（按 config）
5. 启动 admin endpoint（unix socket + 可选 TCP TLS）
6. 启动 Web Console HTTP server（`:7100`，loopback only per ADR-0037）
7. 启动 outbox relay + 定时器（reminders / GC / etc.）
8. ready；接受请求

### 9.2 `agent-center install center` — 部署安装

```
agent-center install center [--instance=<name>]
```

- 创建系统用户 / 目录 / systemd(Linux) 或 launchd(macOS) service unit
- symlink binary 到 `/usr/local/bin/agent-center`（或 versioned path）
- `--instance` 支持单机多实例部署（v2.7.1 #211）；不同 instance 使用不同 install prefix + service label
- 若已安装则自动走升级路径（等效 `upgrade center`）

---

## § 10. CLI ↔ Repository / API 对位

> **v2.7+ UPDATE**: 数据管理操作不再通过 CLI 子命令，而是通过 Web Console API endpoint（`/api/*`）完成。CLI 保留的命令主要对应部署 / 生命周期操作。

| 操作领域 | v2.7 之前（CLI 命令）| v2.7+（当前入口）|
|---|---|---|
| Task / Issue / Plan 管理 | `task create` / `issue open` / ... | Web Console API (`/api/...`) |
| Conversation / Message | `conversation add-message` / `conversation read` | Web Console API + SSE |
| Agent 管理 | `agent create` / ... | Web Console API |
| Secret 管理 | `secret create` / ... | Web Console API |
| Worker 注册 | `worker enroll` | **CLI 保留** |
| Worker 状态 | `worker list` / `worker status` | **CLI 保留** + Web Console |
| 部署安装 | `install center\|worker` | **CLI 保留** |
| 升级 | `upgrade center\|worker` | **CLI 保留** |
| 卸载 | `uninstall center\|worker` | **CLI 保留** |
| Observability 查询 | `inspect` / `query` / `ps` / `stats` / `logs` / `peek-trace` | Web Console API |
| Migration | `migrate up` / `migrate v1-to-v2` | **CLI 保留** |
| Admin token | `admin token create\|list\|revoke` | **CLI 保留** |
| 多实例管理 | N/A | `list-local-centers` / `list-local-workers` / `list-test-instances`（**CLI 新增**）|

---

> **本文档 scope**：v1 实现层 CLI 元层规则 + v2.7+ 命令清单。v2.7 #162 将 CLI 从 50+ 命令精简至约 9 个顶层部署 / 生命周期命令。数据管理功能全部迁移至 Web Console API。当前命令的完整 flag / arg / 输出 schema 以 `--help` 文本 + `internal/cli/build.go` 为准。
