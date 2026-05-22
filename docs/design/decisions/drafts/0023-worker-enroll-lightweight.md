# 0023. Worker Enroll 轻量化

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-22 |
| Related | v2 议题 E1（[v2-kickoff-2026-05-22 § 1 E1](../../drafts/v2-kickoff-2026-05-22.md)）；[ADR-0008](../0008-worker-project-mapping-via-discovery-proposal.md)（**不 supersede**，mapping 流程仍走 proposal-then-accept）|

## Context

v1 的 Worker enroll 流程「**两步两机**」且 `worker.yaml` 厚：

1. Center 同机签 bootstrap token：`agent-center worker enroll --worker-id=...` → 终端打印
2. 用户在 worker 机手写 `worker.yaml`，含 `worker_id` / `bootstrap_token` / `center_endpoint` / `concurrency` / `discovery (scan_paths/exclude/scan_interval)` / `agent_cli` 等
3. `agent-center worker run --config=worker.yaml` 启动 daemon → 兑换 session token → 长连

具体痛点：

- 必须 SSH 到 center VPS 跑命令 → 复制 token → 回 worker 机粘贴
- `worker.yaml` 字段多（scan_paths / exclude / scan_interval / agent_cli 等），新手不知道怎么填
- `agent_cli` 是用户静态声明，跟实际安装情况可能不符
- bootstrap token 一次性，漏抄过期即作废，无 reissue 路径
- 配置变更必须改 yaml + 重启 daemon

[竞品报告 § 3.6 + § 5.3](../../../research/competitive-analysis-2026-05-21.md) 对照 Slock 的「秒级 enroll」体验，认定这是 v2「算力管理」thesis 的入门项。

## Decision

把 Worker enroll 重塑为 **一行命令接入 + 服务端配置主导 + 凭据 stateful** 模型。

### 1. 一行命令接入

Worker 机执行：

```
agent-center join <center-endpoint> --token=<bootstrap-token> --worker-id=<id>
```

内部走 `POST <center-endpoint>/enroll-tokens/{token}/exchange`（与 CLI 共用 endpoint）兑换 session token、落 credential 文件 (`~/.agent-center/credentials` 或等价)、建立长连接。

**`worker.yaml` 退化为 identity-only**：

```yaml
worker:
  id: mac-mini-1
  center_endpoint: https://center.example.com:7000
  # session_token 不入 yaml，落独立 credential 文件（权限 0600）
```

enroll 之前不需要 yaml；`agent-center join` 成功后 daemon 自动写出。所有 v1 在 `worker.yaml` 里的**行为配置** (`concurrency` / `discovery` / `agent_cli`) 迁到服务端（见 § 3）。

### 2. BootstrapToken 升级为独立 Entity

v1 的 bootstrap token 是 CLI 一次性打印字符串，无 DB 持久状态。v2 升级为 **`BootstrapToken` 独立 Entity**（同属 Workforce BC，归 Worker 间接持有）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | ULID | 主键 |
| `worker_id` | str | 绑定 Worker（不可变）|
| `value_hash` | str | token 字面值的 hash（明文不落 DB，仅签发时返回 CLI）|
| `status` | enum | `active / used / expired / revoked` |
| `created_at` | timestamp | 签发时间 |
| `expires_at` | timestamp | TTL 默认 30 min |
| `used_at` | timestamp? | 兑换为 session 的时刻 |
| `revoked_at` | timestamp? | 撤销时刻 |
| `created_by` | str | 签发 actor（center admin / API caller）|

**状态机**：

```
                            ┌──────┐
issue ─→ active ─ exchange→ │ used │  (terminal)
           │                └──────┘
           ├─ TTL hit ─────→ expired
           └─ reissue/manual─→ revoked
```

**Reissue 规则**：

| 旧 token 状态 | 允许 reissue | 行为 |
|---|---|---|
| active | ✅ | 旧 → revoked；新 active TTL 30 min |
| expired | ✅ | 旧保留 expired；新 active TTL 30 min |
| revoked | ✅ | 旧保留 revoked；新 active TTL 30 min |
| used | ❌ | 拒绝；CLI 引导用户走 remove + re-enroll |

**核心不变量**：

- 同一 `worker_id` 同时**至多 1 个 active token**
- `used` 是终态，不可 reissue（一次性凭证使命已尽；rotate 走 [roadmap](../../roadmap.md) v3）
- 并发 reissue 用 `SELECT ... FOR UPDATE` 锁住 `(worker_id, status=active)` 行；事务内 revoke 旧 + insert 新

**Reissue 拒绝时的 CLI 提示**：

```
$ agent-center worker token reissue --worker-id=mac-mini-1
✗ Worker 'mac-mini-1' 的 enroll token 已使用，无法 reissue。
  Worker 当前身份由 session_token 维持，enroll token 已完成使命。

  如需替换：agent-center worker remove mac-mini-1 后重新 enroll
  如需 rotate（v3 roadmap）：见 docs/design/roadmap.md
```

### 3. 行为配置移到服务端

v1 在 `worker.yaml` 里的 `concurrency` / `discovery` / `agent_cli` 字段，v2 一律迁到 **Worker AR 在 center DB 的字段**：

```
Worker {
  // identity（v1 → v2 不变）
  id, status, last_heartbeat,

  // v2 新增：behavior config（center DB 主导）
  concurrency:  { per_agent_type: int },                       // 默认 2
  discovery:    { scan_paths: [str], exclude: [str],
                  scan_interval: duration },                    // 默认 scan_interval=1h
  capabilities: [{ agent_cli: str, detected: bool, enabled: bool }],
}
```

**Config 同步协议**：

- Worker 长连建立后，**reconcile 第一步**就拉 config：`ReconcileResponse` 顺带返回 `WorkerConfig { concurrency, discovery, capabilities_enabled }`；worker 端覆盖 in-memory（不落盘）
- 用户在 center 改配置（CLI / API）→ center 通过长连接 push `worker.config.updated` 事件 → worker 收到后重拉 config（**无需重启 daemon**）
- 重连时仍走 reconcile，重新拉一次 config

### 4. Capabilities 自动探测

Worker daemon **每次 online**（首次 + 重连）都跑探测：`which claude` / `which codex` / `which gemini` 等，结果上报 center。

Center 端 Worker AR 的 `capabilities` 数组每项含：

- `agent_cli`：CLI 名
- `detected`：探测结果（worker 上报）
- `enabled`：用户开关（默认 = detected；可在 CLI 关掉某项）

Center 派单时只考虑 `detected=true && enabled=true` 的 CLI。

### 5. CLI 变化

| 用途 | v1 | v2 |
|---|---|---|
| 签发 enroll token | `agent-center worker enroll --worker-id=...`（仅 center 同机）| `agent-center worker token issue --worker-id=...` 走 endpoint，可远程（仍需 admin 凭证）|
| Worker 接入 | 编辑 `worker.yaml` + `agent-center worker run --config=worker.yaml` | `agent-center join <center-endpoint> --token=<t> --worker-id=<id>`（首次）；之后 `agent-center worker run` 自动读 credential |
| Reissue token | ❌ | `agent-center worker token reissue --worker-id=<id>` |
| Revoke token | ❌ | `agent-center worker token revoke --token-id=<id>` |
| 列 token | ❌ | `agent-center worker token list --worker-id=<id>` |
| 改 config | 改 yaml + 重启 | `agent-center worker config set <id> <key>=<value>`（长连推 update）|
| 关 capability | ❌（声明式）| `agent-center worker capability disable <id> <agent_cli>` |

`worker.yaml` 不再是 enroll 输入，只在 daemon 启动时被读出 identity。

### 6. 新增 / 改名事件

| 事件 | 触发 | payload 关键字段 |
|---|---|---|
| `worker.bootstrap_token.issued` | 签发 | `token_id, worker_id, expires_at, created_by` |
| `worker.bootstrap_token.used` | 兑换成 session | `token_id, worker_id, used_at` |
| `worker.bootstrap_token.expired` | TTL 到期（扫描器扫到）| `token_id, worker_id` |
| `worker.bootstrap_token.reissued` | reissue 操作 | `new_token_id, old_token_id, worker_id, reissued_by, old_status_at_reissue` |
| `worker.bootstrap_token.revoked` | 手动 revoke 或 reissue 撤旧 | `token_id, worker_id, revoked_by, reason` |
| `worker.config.updated` | 用户改 config（长连接推 worker）| `worker_id, changed_fields, by` |
| `worker.capability.detected` | online 探测上报 | `worker_id, capabilities` |

`worker.enrolled` / `worker.online` / `worker.offline` / `worker.heartbeat` 语义不变。

### 7. Worker Invariants 变更

- ❌ 删：~~bootstrap token 一次性~~（被结构化）
- ✅ 改：**enroll token used 是终态；同 worker 同时至多 1 个 active token**
- 其余 Worker invariants（identity 不可变 / session 1:1 / online↔offline 反复 / offline 不接派单 / heartbeat 静默 → offline）**不动**

## Consequences

**正面**：

- 加节点真正秒级：复制一行命令到 worker 机即接入
- `worker.yaml` 从 6 节降到 1 节（identity）
- 配置变更即时生效（长连接推送），不重启 daemon
- `agent_cli` 自动探测，避免「声明 vs 实际不符」
- token 泄露 / 漏抄有应对路径（reissue + revoke）+ 完整审计事件
- BootstrapToken 状态机为 v3 rotate 留接口（reissue 拦截 used 时含引导路径）

**负面 / 待跟进**：

- 新增 `bootstrap_tokens` 表（无 back-compat 顾虑，schema 直接重建）
- Worker AR schema 加 3 个 JSON 列（concurrency / discovery / capabilities）
- 长连接协议加 `worker.config.updated` 推送 + `WorkerConfig` 落 reconcile response —— TaskRuntime BC 的 reconcile 协议要同步改
- CLI 表面变化大：v1 用户从零 enroll，无迁移路径（符合「不考虑向后兼容」）
- `agent-center worker enroll` 命令名废弃，改 `agent-center worker token issue` —— 文档全量替换

## Alternatives Considered

### A. 保留 v1 流程，仅做 `worker.yaml` 字段精简

- 不动 BootstrapToken / 不加 config 服务端化 / 不加长连推送
- ✅ 改动最小
- ❌ 没解决「两步两机」核心痛点；token 漏抄过期仍要重来；`agent_cli` 仍要手填
- ❌ 跟 thesis「算力管理 优化和重构」不匹配
- → **否决**

### B. 完全 server-side state（worker 机零 credential 文件）

- worker 机不存任何 credential；每次 daemon 启动都重新 enroll
- ✅ 最干净
- ❌ daemon 重启或机器重启都要重 enroll → 反而比 v1 还重
- → **否决**

### C. Slock 风格自动发现（mDNS / IP autodetect）

- worker 机零参数 `agent-center join` → 局域网 mDNS 找 center
- ✅ 极致丝滑
- ❌ 多 center 场景概念污染；安全姿态弱；多数 agent-center 部署 center 在 VPS（非局域网）
- → **否决**（可放 v3 roadmap 给本地多机集群场景）

## References

### 同 BC

- [workforce/01-worker.md](../../architecture/tactical/workforce/01-worker.md) —— Worker 聚合 + BootstrapToken + WorkerProjectMapping 子从属
- [workforce/00-overview.md](../../architecture/tactical/workforce/00-overview.md) —— BC 入口（含 Domain Services）

### 跨 BC

- [task-runtime/00-overview.md](../../architecture/tactical/task-runtime/00-overview.md) —— ReconcileService 协议拓展（config-fetch）

### 相关 ADR

- [ADR-0008](../0008-worker-project-mapping-via-discovery-proposal.md)（mapping 流程不动）
- [ADR-0014](../0014-event-sourcing-level.md)（新事件遵循同框架）
- [ADR-0019](../0019-bc-scheduling-execution-merged-to-task-runtime.md)（reconcile 协议归属）

### 实施层

- [implementation/04-configuration.md](../../implementation/04-configuration.md) —— `worker_config` schema v2

### 来源

- [docs/design/drafts/v2-kickoff-2026-05-22.md § 1 E1](../../drafts/v2-kickoff-2026-05-22.md)
- [docs/research/competitive-analysis-2026-05-21.md § 3.6 + § 5.3](../../../research/competitive-analysis-2026-05-21.md)
- [docs/design/requirements/01-functional.md F23](../../requirements/01-functional.md)
