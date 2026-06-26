> 📌 **v2 update applied (P12 S6, 2026-05-24)** — v2 撤回了 Bridge BC + 飞书集成 (per [ADR-0031](../decisions/0031-v2-drop-bridge-vendor-integration.md))；ADR-0017/0021/0022 superseded by [ADR-0039](../decisions/0039-conversation-business-model-v2-unified.md)。本文 v1 vendor / Bridge / 飞书 引用块在本次 sweep 中改为 strikethrough + "(v2 删 per ADR-0031)" 注；当前 active 配置 schema 以 [§ 8 minimal example](#-8-minimal-example) 为准。

# 配置文件 schema

> **实现层** · `agent-center` 单 binary 多模式共用一份 YAML 配置文件 + env 覆盖 + CLI flag 兜顶。
>
> 本文档 **元层规则 § 1-5 + 多模式 gating § 6 + 配置项参考 § 7（按主题分组）+ 代表性 yaml 范例 § 8 + ADR/NF 对位 § 9**。配置项的默认值与单位以本文档为准；新增字段必须按 § 5 演进规则。

## § 1. 配置文件定位

### 1.1 默认路径

| Mode | 默认路径 | flag 覆盖 |
|---|---|---|
| `agent-center server` | `/etc/agent-center/config.yaml` | `--config=<path>` |
| `agent-center worker` | `~/.agent-center-worker/config.yaml` | `--config=<path>` |
| `agent-center supervisor` | 不读 config 文件（短生命周期，参数走 CLI flag）| - |

### 1.2 格式

YAML 1.2，UTF-8。文件不存在 + 没有 flag 指定 → 走 built-in defaults（仅当所有必填字段都有 default 时）；否则启动失败（[§ 4](#-4-验证--启动失败语义)）。

---

## § 2. 加载优先级

```
CLI flag > env var > YAML file > built-in default
```

### 2.1 Env var 命名

`AGENT_CENTER_<PATH>` —— 前缀 `AGENT_CENTER_` + YAML 路径用 `_` 拼接，全 UPPER_CASE：

| YAML | Env var |
|---|---|
| `blob_store.root` | `AGENT_CENTER_BLOB_STORE_ROOT` |
| `notification.default_channel` | `AGENT_CENTER_NOTIFICATION_DEFAULT_CHANNEL` |
| `supervisor.invocation_timeout_seconds` | `AGENT_CENTER_SUPERVISOR_INVOCATION_TIMEOUT_SECONDS` |
| `worker_config.discovery.scan_interval` | `AGENT_CENTER_WORKER_CONFIG_DISCOVERY_SCAN_INTERVAL` |

`.` 跟 snake_case 内的 `_` 一律映射为 `_`，扁平到单层。解析时按已注册的 YAML schema 走"最长匹配前缀"消歧 —— 因此 schema 设计**不允许出现路径冲突**（如同时有 `foo.bar_baz` 和 `foo_bar.baz`），否则启动 fail-fast（[§ 4](#-4-验证--启动失败语义)）。

### 2.2 CLI flag 优先级

部分常用字段开 flag，运行时覆盖 env / file：

| Field | Flag |
|---|---|
| 整个 config 文件 | `--config=<path>` |
| `server.listen_addr` | `--listen=<addr>` |
| `worker_config.center_endpoint` | `--center=<url>` |
| `blob_store.root` | `--blob-root=<path>` |

只暴露**调试 / 部署常用**字段当 flag，避免 CLI surface 膨胀。

---

## § 3. 凭据处理

按 [conventions § 13 安全 / 隐私](../../rules/conventions.md) —— **凭据不允许明文进 YAML 文件文本**：

| 路径 | 推荐 |
|---|---|
| `*_secret` / `*_token` / `*_password` 等字段 | 走 env var（首选）或引用文件路径（`*_secret_file: /path/to/secret`） |
| YAML 里出现明文 secret | 启动失败 + 告警；走 [§ 4](#-4-验证--启动失败语义) |

每个 secret 字段有对应的 `<field>_file: <path>` 备用 key（读取文件首行作为 secret）。例：

<!-- v2 example: every secret uses *_file or env var; no vendor blocks -->
```yaml
# v2 example: SecretManagement BC AES-encrypts secrets at rest; the
# only file-on-disk reference is the master key:
secret_management:
  master_key_file: "/etc/secrets/agent-center/master.key"
# Individual user secrets are CRUD'd via the CLI (`agent-center secret
# create ...`) — never appear in YAML. References inside mcp_config use
# the `secret:<name>` SecretRef syntax (ADR-0027).
```

### 3.1 v1 凭据存储

VPS 单节点 + SSH 跑 admin（[domain-vision § B2](../architecture/strategic/00-domain-vision.md)）：

- `/etc/secrets/*` 文件权限 0600，owner = `agent-center` system user
- 不引入 vault / sops 之类的依赖（v1 简化）
- env 注入凭据时由 systemd `EnvironmentFile=` 加载（[06-deployment.md](06-deployment.md) 详）

---

## § 4. 验证 / 启动失败语义

按 [conventions § 17 错误显式化](../../rules/conventions.md) —— 配置错误**绝不吞**：

| 情况 | 行为 |
|---|---|
| 必填字段缺失（无 default） | exit 2 + stderr 输出 `config: <field>: required` |
| 类型错误（如 `int` 字段填 `"abc"`） | exit 2 + stderr 输出具体 YAML 路径 + 期望类型 |
| 枚举值非法 | exit 2 + stderr 列出全部合法枚举 |
| YAML 解析错误（语法）| exit 2 + stderr 输出行号 + 上下文 |
| Env var 路径有歧义（多个 YAML 路径映射到同一 env name） | exit 2 + stderr 列出冲突路径；schema 不允许这种 collision |
| Secret 字段在 YAML 内明文 | exit 2 + stderr 提示走 `_file:` / env |
| 引用文件不存在（`*_file: /path`） | exit 2 + stderr 路径不存在 |
| 字段已 deprecated（§ 5）| stderr WARN 但启动继续；deprecated → removed 用两个 release 缓冲 |
| 未知字段（YAML 里有不认识的 key） | exit 2 + stderr 路径 + "did you mean ...?" |

**未知字段 fail-fast** 是为避免错字（`blob_stoer.root`）silently ignored；[§ 17 "未知协议字段当 noop 不上报"是禁止的](../../rules/conventions.md)。

启动流程：

```
1. parse CLI flag → 拿 --config=<path>
2. load YAML file
3. apply env var override
4. apply flag override
5. validate（按上表）
6. fail → exit 2 + 完整诊断到 stderr
7. pass → 继续启动（迁移 / 服务 / 等）
```

---

## § 5. Schema 演进规则

按 [conventions § 5 文档先行 / § 12 命名一致](../../rules/conventions.md)：

| 变更 | 规则 |
|---|---|
| 加字段（有 default） | 直接 PR；本文档同步 |
| 加字段（无 default，必填） | **breaking change** → ADR 评估 + 升级 playbook |
| 改字段语义 / 单位 | breaking change → ADR；新 key + 旧 key deprecate 同步 |
| 删字段 | 先标 deprecated（含警告），下版才移除 |
| 改默认值 | breaking change（已部署的没显式配置者会变） → ADR |

deprecated 字段标记：

```yaml
# config.yaml
old_field: 123  # WARN: deprecated since 2026-06-01, use new_field
new_field: 123
```

启动时遇 deprecated 字段：stderr WARN 一行但**不阻断**；移除时一并更新文档历史。

---

## § 6. 各运行模式的 config gating

YAML 是**单一文件多模式共用**。每个 mode 启动时**只读它需要的 section**；其他 section 即使填了也不影响。

| Mode | 必读 section |
|---|---|
| `server` | `server` / `blob_store` / `execution` / `secret_management` / `web_console` / `peek` |
| `worker` | `server`（sqlite_path / admin_socket_path）/ `worker`（enrollment identity）|
| `supervisor` | 不读 config（CLI flag 注入参数）|

如果 worker 配置文件里写了 `server` section，worker 启动忽略；server 启动时不会去找 worker config 文件。**这是约定，避免逐 mode 一份 YAML 的运维负担**。

`agent-center` 启动时 self-doc `--help-config <mode>` 列该 mode 关心的 key 集合。

---

## § 7. 配置项参考

每条字段：路径 / 类型 / 默认 / 来源 / 用途。

### 7.1 `server.*`

> **v2.7+ UPDATE (2026-06-26)**: `server.listen_addr` 已 DEPRECATED（v2.9 #174）— 该值从未实际绑定端口，Web Console 使用 `web_console.listen_addr`，admin 端使用 `admin_socket_path` / `admin_tcp_listen`。保留字段仅为兼容现有 config 文件的 strict decode。新增 `admin_tcp_listen` / `admin_tls_*` / `bootstrap_public_url` / `instance` 字段。

| Field | Type | Default | 来源 | 用途 |
|---|---|---|---|---|
| `server.listen_addr` | string | (无) | - | **DEPRECATED v2.9** — 保留仅为 strict decode 兼容，值被忽略 |
| `server.sqlite_path` | path | `/var/lib/agent-center/agent-center.db` | [02-persistence § 1.2](02-persistence-schema.md) | SQLite 数据库文件 |
| `server.admin_socket_path` | path | `/run/agent-center/admin.sock` | [NF5](../requirements/02-non-functional.md) | 本机 admin CLI unix socket |
| `server.admin_tcp_listen` | string | (空=禁用) | v2.3-7a | 可选 TCP admin 监听地址（如 `0.0.0.0:7300`），启用时 auto-gen TLS cert |
| `server.bootstrap_public_url` | string | (空=从 admin_tcp_listen 派生) | v2.7 #200 | Web Console "Add Worker" 展示的外部可达 admin host:port |
| `server.admin_tls_cert_path` | path | (auto: `<sqlite_dir>/admin-tls.crt`) | v2.3-7a | TCP admin TLS 证书路径 |
| `server.admin_tls_key_path` | path | (auto: `<sqlite_dir>/admin-tls.key`) | v2.3-7a | TCP admin TLS 私钥路径 |
| `server.instance` | string | `"default"` | v2.7.1 #211 | 多 center 实例名称；`install center --instance <name>` 设定 |

### 7.2 `blob_store.*`

| Field | Type | Default | 来源 | 用途 |
|---|---|---|---|---|
| `blob_store.kind` | enum `local` / `s3` | `local` | [01-blob-store § 实现](01-blob-store.md) | BlobStore 后端 |
| `blob_store.root` | path / URL | `/var/lib/agent-center/blobs` | [01-blob-store § 配置](01-blob-store.md) | LocalDir root；s3 时 `s3://bucket/prefix` |
| `blob_store.retention_days` | int | 90 | [01-blob-store § GC](01-blob-store.md) | blob 保留期 |
| `blob_store.s3.endpoint` | URL | - | [01-blob-store § 配置](01-blob-store.md) | 仅 kind=s3 |
| `blob_store.s3.region` | string | - | 同上 | 仅 kind=s3 |
| `blob_store.s3.access_key_id` | string | env | 同上 | 凭据走 env / `*_file` |
| `blob_store.s3.secret_access_key_file` | path | - | 同上 | 走 [§ 3](#-3-凭据处理) |

### 7.3 `supervisor.*`

| Field | Type | Default | 来源 | 用途 |
|---|---|---|---|---|
| `supervisor.max_concurrent_invocations` | int | 5 | [ADR-0013](../decisions/0013-supervisor-invocation-concurrency.md) | 全局并行上限 |
| `supervisor.coalescing_window_seconds` | int | 30 | [ADR-0013 § 3](../decisions/0013-supervisor-invocation-concurrency.md) | 同 scope 事件合并窗口 |
| `supervisor.coalescing_max_age_minutes` | int | 5 | 同上 | 合并硬上限 |
| `supervisor.invocation_timeout_seconds` | int | 180 | [ADR-0013 § 6](../decisions/0013-supervisor-invocation-concurrency.md) | scope_kind ≠ global 的 timeout |
| `supervisor.invocation_timeout_global_seconds` | int | 600 | 同上 | scope_kind=global 的 timeout |

### 7.4 ~~`notification.*` / 7.5 `bridge.*`~~ (RETIRED)

Both sections removed in v2 per [ADR-0031](../decisions/0031-v2-drop-bridge-vendor-integration.md). v2 routes user-visible
notifications through the Web Console + SSE; there is no
notification fallback channel and no vendor Bridge configuration.

> **注**：Config struct 中 `NotificationConfig` 仍保留（`notification.default_channel`），但仅为 strict decode 兼容。实际运行中该值未被使用。

### 7.6 `execution.*`

| Field | Type | Default | 来源 | 用途 |
|---|---|---|---|---|
| `execution.submitted_timeout_seconds` | int | 300 | [task-runtime § 3.3](../architecture/tactical/task-runtime/00-overview.md) | submitted 状态超时 |
| `execution.default_timeout_hours` | int | 6 | 同上 | execution 总超时 |
| `execution.dispatch_ack_timeout_seconds` | int | 30 | [ADR-0011 § 2](../decisions/0011-dispatch-reliability-protocol.md) | dispatch ACK 等待 |
| `execution.input_request_ping_hours` | int | 4 | [task-runtime § 3.3](../architecture/tactical/task-runtime/00-overview.md) | InputRequest 定时 ping |
| `execution.input_request_timeout_hours` | int | 24 | 同上 | InputRequest 最大等待 |
| `execution.shim_hello_timeout_seconds` | int | 60 | [ADR-0018 § 7](../decisions/0018-detached-agent-via-per-execution-shim.md) | shim 首次 hello 超时 |
| `execution.shim_goodbye_ack_timeout_hours` | int | 24 | 同上 | shim goodbye 确认 |
| `execution.max_executions_per_task` | int | 3 | [task-runtime § 3.1](../architecture/tactical/task-runtime/00-overview.md) | 单 task 最大 retry |
| `execution.kill_grace_seconds` | int | 5 | [05-agent-adapters § 4.2](05-agent-adapters.md) | SIGTERM → SIGKILL grace |

### 7.7 `worker.*`（worker mode 专用）

> **v2.7.1 UPDATE (2026-06-26)**: 原 `worker_config.*` 的大部分字段（`exec_base_dir` / `agents_base_dir` / `gc_exec_retention_hours` / `heartbeat_*` / `daemon_socket_path` / `session_token_file` / `center_endpoint`）已不在 config.yaml 中配置。v2.7.1 #249 简化 worker config 为 enrollment identity 单一来源：installer 把 worker_id + token 直接写入 config.yaml（0600）。实际 Config struct 如下。

| Field | Type | Default | 来源 | 用途 |
|---|---|---|---|---|
| `worker.worker_id` | string | - | v2.7.1 #249 | worker 唯一 ID；installer 写入，不可变 |
| `worker.worker_name` | string | - | v2.7.1 #249 | worker 显示名 |
| `worker.bootstrap` | string | - | v2.7.1 #249 | 一次性 bootstrap token（enroll 后消耗）|
| `worker.token` | string | - | v2.7.1 #249 | 长期 session token（enroll 后写入）|
| `worker.server_fingerprint` | string | - | v2.7.1 #249 | center TLS 证书指纹（TCP admin 连接校验）|

> **注**：v2.7.1 之前的 `worker_config.*` 路径已废弃。Token 不再出现在 `ps` / systemd unit / launchd plist 中（安全改进）。`worker run` 从 config.yaml 读取这些字段；显式 CLI flag 仍可覆盖（back-compat + 高级用途）。

### 7.8 `observability.*`

| Field | Type | Default | 来源 | 用途 |
|---|---|---|---|---|
| `observability.events_retention_days` | int | 365 | [NF9](../requirements/02-non-functional.md) 隐含 | events 表保留期 |
| `observability.unknown_event_alert_threshold_per_24h` | int | 100 | [05-agent-adapters § 3.1](05-agent-adapters.md) | adapter unknown event escalate 阈值 |
| `observability.ps_watch_interval_seconds` | int | 1 | observability § 7.1 | `ps --watch` 刷新间隔 |
| `observability.peek_trace_buffer_lines` | int | 200 | [ADR-0015 § 4](../decisions/0015-agent-trace-not-in-events-table.md) | peek-trace 默认窗口 |

### 7.9 `secret_management.*`（[ADR-0026](../decisions/0026-user-secret-management-bc.md)）

| Field | Type | Default | 来源 | 用途 |
|---|---|---|---|---|
| `secret_management.master_key_file` | path | - | [ADR-0026 § 4](../decisions/0026-user-secret-management-bc.md) | AES-GCM master key（32 bytes base64）文件路径；mode 0600；center 进程启动加载；**不入 DB / 不入 event** |
| `secret_management.audit_retention_days` | int | 365 | 同上 | `user_secret.*` 事件保留（沿用 events 表，**事件不含明文**）|

### 7.10 `server.*`（center 进程 + built-in supervisor，[ADR-0029](../decisions/0029-supervisor-as-builtin-agent-instance.md)）

| Field | Type | Default | 来源 | 用途 |
|---|---|---|---|---|
| `server.builtin_agents_base_dir` | path | `~/.agent-center/agents` | [ADR-0029 § 3](../decisions/0029-supervisor-as-builtin-agent-instance.md) | built-in AgentInstance 持久 home_dir 根（center 机；v2 仅 supervisor 一个，路径 `~/.agent-center/agents/supervisor/`）|
| `server.supervisor_agent_cli` | enum | `claude-code` | 同上 | built-in supervisor 用的 agent CLI；可通过部署配置改 |


> Master key 一旦丢失，所有 user_secret 不可解。运维需自管 key 备份；[roadmap](../roadmap.md) 接 Vault / KMS 作为可选来源。

### 7.11 ~~`agent_cli.*`~~ (RETIRED)

> **v2.7+ UPDATE**: `agent_cli.*` 配置段已不在 Config struct 中。Agent CLI binary 路径现在通过 agent adapter 的 `Probe()` 方法自动发现（PATH 查找）。Binary 名默认为 `claude` / `codex` / `opencode`；如需覆盖，通过 Agent BC 的 `cli` 字段在 Web Console 中配置。

---

## § 8. 代表性完整 yaml 范例

### 8.1 server 模式

`/etc/agent-center/config.yaml`：

```yaml
server:
  # listen_addr: ":7000"  # DEPRECATED v2.9 — 值被忽略；端口由 web_console.listen_addr 控制
  sqlite_path: "/var/lib/agent-center/agent-center.db"
  admin_socket_path: "/run/agent-center/admin.sock"
  # admin_tcp_listen: "0.0.0.0:7300"  # 可选：启用 TCP admin（auto-gen TLS cert）
  # instance: "default"               # 多实例部署时设定

blob_store:
  kind: "local"
  root: "/var/lib/agent-center/blobs"

# (v2 removed notification.* + bridge.* + supervisor.* + observability.*
# per ADR-0031 + CLI simplification — Web Console + SSE replaces.)

execution:
  submitted_timeout_seconds: 300
  default_timeout_hours: 6
  dispatch_ack_timeout_seconds: 30
  input_request_ping_hours: 4
  input_request_timeout_hours: 24
  shim_hello_timeout_seconds: 60
  shim_goodbye_ack_timeout_hours: 24
  max_executions_per_task: 3
  kill_grace_seconds: 5

web_console:
  enabled: true
  listen_addr: "127.0.0.1:7100"  # loopback only per ADR-0037
```

### 8.2 worker 模式

`~/.agent-center-worker/config.yaml`（v2.7.1+: enrollment identity only；`agent-center install worker` 自动生成）：

```yaml
worker:
  worker_id: "W-laptop-hayang"
  worker_name: "laptop-hayang"
  token: "<session-token>"             # install worker 写入；不再出现在 ps 中
  server_fingerprint: "<cert-fingerprint>"  # TCP admin TLS 指纹

server:
  sqlite_path: "~/.agent-center-worker/agent-center.db"  # worker 本地 DB
  admin_socket_path: "~/.agent-center-worker/admin.sock"
```

---

## § 9. 与 ADR / NF / Repository 对位

| 设计层来源 | 落到 § 7 哪节 |
|---|---|
| [ADR-0006 BlobStore](../decisions/0006-blob-store-for-large-content.md) | `blob_store.*` |
| [ADR-0011 Dispatch reliability](../decisions/0011-dispatch-reliability-protocol.md) | `execution.dispatch_ack_timeout_seconds` |
| [ADR-0013 Supervisor 并发](../decisions/0013-supervisor-invocation-concurrency.md) | `supervisor.*` |
| [ADR-0015 trace 不进 events 表](../decisions/0015-agent-trace-not-in-events-table.md) | `observability.peek_trace_buffer_lines` |
| ~~[ADR-0017 Task ↔ Conversation](../decisions/0039-conversation-business-model-v2-unified.md) § 10.5~~ <!-- v1 ref: ADR-0017 superseded by ADR-0039 --> | ~~`notification.default_channel`~~ (v2 删 per ADR-0031) |
| [ADR-0018 Per-execution shim](../decisions/0018-detached-agent-via-per-execution-shim.md) | `execution.shim_*` / `worker_config.exec_base_dir` / `gc_exec_retention_hours` |
| [NF1 零 LLM SDK](../requirements/02-non-functional.md) | 整个 config 不含任何 LLM SDK key |
| [NF6 并发 capacity](../requirements/02-non-functional.md) | `worker_config.concurrency.per_agent_type` |
| [NF9 events 表](../requirements/02-non-functional.md) | `observability.events_retention_days` |
| [NF11 worker daemon socket](../requirements/02-non-functional.md) | `worker_config.daemon_socket_path` |
| [conventions § 13 安全](../../rules/conventions.md) | [§ 3 凭据处理](#-3-凭据处理) |
| [conventions § 17 错误显式化](../../rules/conventions.md) | [§ 4 验证 / 启动失败](#-4-验证--启动失败语义)（未知字段 fail-fast）|

---

> **本文档 scope**：v1 配置文件 schema + 凭据 + 加载优先级 + 各 mode gating + 完整字段参考 + 范例 yaml。后续新增字段按 § 5 演进规则；secret / 凭据落部署细节见 [06-deployment](06-deployment.md)。v2.7+ sweep (2026-06-26): § 7.1 更新 server 字段、§ 7.4 标注 RETIRED、§ 7.7 更新为 `worker.*` enrollment identity、§ 7.11 标注 RETIRED、§ 8 范例同步。实际 Config struct 以 `internal/config/config.go` 为准。
