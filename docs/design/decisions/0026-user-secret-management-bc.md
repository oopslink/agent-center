# 0026. SecretManagement BC：中心化用户密钥管理

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-24 |
| Delivered | P8 § 3.7-3.8 — `6d39ee8` (UserSecret AR + SecretRef VO). P11 F11 — `fc433f9` (UI no-plaintext-echo). |
| Related | v2 议题 G4 衍生（[v2-kickoff-2026-05-22 § Group 2 G4](../../drafts/v2-kickoff-2026-05-22.md)）；为 [ADR-0027 MCP per-agent 注入](0027-mcp-per-agent-injection.md) 提供基础设施；不动 [ADR-0023 BootstrapToken](0023-worker-enroll-lightweight.md) / [conventions § 13](../../../rules/conventions.md) 系统凭证 |

## Context

v2 在 G4 讨论中暴露出需求：用户配 MCP server 需要存 GitHub PAT / DB password 等 secret。最早设计想走「占位符 + worker 端 env 替换」，但 @oopslink 提议「中心化管理用户密钥」 —— 不只是引用语法统一，**统一存储 / 统一 rotate / 统一审计**。

### v2 系统涉及的 secret 一览

| 类型 | 例子 | 现状 / 处置 |
|---|---|---|
| **用户密钥（user-domain）** | MCP env vars（GitHub PAT / DB pwd / ...）；未来云 Computer 凭据；未来 repo deploy key | **G4 + 本 ADR 新引入 SecretManagement BC 统一管** |
| **系统内部凭证（system-internal）** | Worker BootstrapToken（[ADR-0023](0023-worker-enroll-lightweight.md)）/ session_token（worker 本地 credential 文件）/ BlobStore S3 access key | **不动** —— 各自 BC 已就位 |

### 为什么把 user secret 单独 carve 出来

- 数量在涨：v2 G4 已是 1 类（MCP），E2/v3 roadmap 还会涨（云凭据 / repo key）
- 用户主权：用户密钥**是用户的资产**，跟系统凭证（BootstrapToken / session）不同 —— 用户需要明确的「我有哪些密钥 / 在哪用 / 何时换」掌控感
- 安全 hygiene：DB 不存明文；CLI 不打印；just-in-time 解析；都是密钥管理的标准动作

### 为什么不把系统内部凭证一起搬

- BootstrapToken 等已经设计了带业务语义的状态机（active / used / expired / revoked + reissue 规则），是 Workforce BC 的有机部分；硬迁会破坏 [ADR-0023](0023-worker-enroll-lightweight.md) 的设计内聚
- S3 key 等进程级凭证是单一全局值，跟「用户管理的多份 secret」语义不同；conventions § 13 路径已足够
- v2 不做无 ROI 的统一

## Decision

引入 **`SecretManagement`** 作为新的支撑 BC（Supporting Domain），管理**用户密钥**。

### 1. 新 BC：SecretManagement（BC8）

| 维度 | 内容 |
|---|---|
| Kind | Supporting Domain |
| 范围 | 用户密钥的存储 / 读取 / rotate / revoke / audit |
| 不在范围 | 系统内部凭证（BootstrapToken / session_token / S3 key 等）|

### 2. 核心聚合 `UserSecret`（独立 AR）

```
user_secret (
  id                  ULID
  name                str          -- 全局唯一，用户起名（如 "github-pat" / "prod-db-pwd"）
  kind                enum         -- mcp | cloud_credential | repo_deploy_key | other（开放扩展）
  value_ciphertext    bytea        -- AES-GCM 加密的密钥明文
  value_nonce         bytea        -- AES-GCM nonce
  state               enum         -- active | revoked
  created_at          ISO8601 TEXT
  created_by          str          -- 创建者 actor
  last_used_at        ISO8601 TEXT, nullable -- 最后一次被 resolve 的时间
  rotated_at          ISO8601 TEXT, nullable -- 最后一次 rotate 的时间
  revoked_at          ISO8601 TEXT, nullable
  revoked_by          str, nullable
  version             int          -- 乐观锁
)

UNIQUE INDEX user_secret_name_uq (name)
```

> `value_ciphertext` 实际数据库实现可用 BLOB / bytea 列；ULID 主键沿用项目惯例。

### 3. 状态机

```
[*] → active ──→ active   (rotate：value_ciphertext + value_nonce 重写，bump rotated_at + version)
        ↓
     revoked   (terminal；不可逆；后续 resolve 拒绝)
```

`active` 是非终态；rotate 是更新 value，不改 state。`revoked` 是 terminal。

### 4. 加密模型

| 维度 | 内容 |
|---|---|
| **加密算法** | AES-GCM 256-bit |
| **Master key 来源** | `secret_management.master_key_file` 配置项（[implementation/04-configuration § 7.10](../../implementation/04-configuration.md)）—— 文件首行作 master key（base64 编码），mode 0600，systemd `LoadCredential=` / env 提供；**绝不入 DB** |
| **明文生命周期** | DB 只存 ciphertext；明文仅在 worker daemon `spawn agent` 前短暂出现：内存 → mode 0600 临时文件 → 进程结束后 unlink |
| **CLI 输入** | `agent-center secret create --name=<n> --kind=<k>` 交互输入 secret 明文（stdin / pinentry），CLI 进程内立即加密后丢明文 |
| **CLI 输出** | **永不打印**明文（仅在 create 成功时显示「Reference as: `secret:<name>`」）|

### 5. SecretRef VO

引用语法（用在 `mcp_config` 等需要密钥的地方）：

```
secret:<name>
```

例子：`secret:github-pat`、`secret:prod-db-pwd`。

JSON 引用：

```json
{
  "mcpServers": {
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": { "GITHUB_TOKEN": "secret:github-pat" }
    }
  }
}
```

> v2 仅一种 SecretRef 语法（中心化 lookup）。未来若引入其他来源（env / file / vault / keyring）再扩展为 `<scheme>:<key>` 形式。

### 6. CLI

| 命令 | 用途 | 同机要求 |
|---|---|---|
| `agent-center secret create --name=<n> --kind=<k>` | 创建（stdin / pinentry 输入明文）| center 同机 |
| `agent-center secret list [--kind=<k>] [--state=<s>]` | 列（**不显示明文**；只显示元数据 + last_used_at）| center 同机 / 远程 |
| `agent-center secret rotate <name>` | 重写 value（stdin / pinentry 输入新值）| center 同机 |
| `agent-center secret revoke <name>` | 终态 revoke | center 同机 |
| `agent-center secret usage <name>` | 反查：哪些 AgentInstance 通过 SecretRef 引用了它 | center 同机 / 远程 |

> v2 不提供 `secret show <name>` 显示明文 CLI —— 用户忘记了就 rotate；明文跨命令边界不传递。

### 7. 解析协议（Resolution Flow）

```
派单（worker daemon spawn agent 前）:
  1. 读 AgentInstance.config.mcp_config 拿到含 SecretRef 的模板
  2. 找出所有 secret:<name> 引用 → 调 center API SecretResolutionService.resolve(name, agent_instance_id)
  3. center 校验：
     - UserSecret.state = active ?
     - 调用方是合法 worker daemon（session_token 有效）?
     - 调用方所属 worker 跟 AgentInstance.worker_id 一致?
     全过 → 解密 + 返回明文 + emit `user_secret.accessed`
  4. worker daemon 内存里把 SecretRef 替换为明文 → 写 home_dir/mcp_config.runtime.json (mode 0600)
     -- 路径含 .runtime 后缀，跟 home_dir/mcp_config.json (模板) 区分
  5. spawn agent CLI（按 adapter 把 mcp_config.runtime.json 翻译为该 CLI 需要的格式）
  6. agent 进程结束 / shim goodbye → daemon unlink home_dir/mcp_config.runtime.json
  7. 若 execution 异常 / worker 重启 → daemon 启动时主动扫并清理 home_dir/agents/*/mcp_config.runtime.json
```

> 解析失败（secret 不存在 / revoked / 权限不足）→ NACK reason `secret_unresolvable`（详见 [ADR-0011](../0011-dispatch-reliability-protocol.md) NACK 表扩展 + [task-runtime/02-task-execution § 7](../../architecture/tactical/task-runtime/02-task-execution.md) failed_reason）。

### 8. 授权矩阵

| 动作 | 允许方 | 备注 |
|---|---|---|
| create / rotate / revoke | **用户 only** | 严格沿 ADR-0025 原则（资产配置归用户）|
| read 明文 | **worker daemon only** | 经过 session_token 认证；只能 resolve 当前派单的 AgentInstance 引用的 secret |
| list 元数据 | 用户 / supervisor（只读元数据，不含明文）| supervisor 可看「哪些 secret 存在」做派单判断，但永不拿明文 |
| usage 反查 | 用户 / supervisor | 同上 |

**Agent 进程 / Supervisor 永不直接获取明文**。

### 9. 事件

| 事件 | 触发 | payload |
|---|---|---|
| `user_secret.created` | create | id, name, kind, created_by |
| `user_secret.rotated` | rotate | id, name, by, rotated_at |
| `user_secret.revoked` | revoke | id, name, by, reason? |
| `user_secret.accessed` | resolve（解析成功）| id, name, by (worker daemon), agent_instance_id, accessed_at |
| `user_secret.access_denied` | resolve 失败 | id?, name, by, reason (`not_found / revoked / not_authorized`) |

### 10. Invariants

1. `name` 全局唯一（UNIQUE INDEX）
2. `revoked` 是终态，不可 rotate / re-activate（需 create 新 secret）
3. DB 不存明文；只存 `value_ciphertext` + `value_nonce`
4. Master key 不入 DB
5. CLI 不打印明文（除 create 时回显成功 Reference）
6. Worker daemon 只能 resolve 当前合法绑定的 AgentInstance 引用的 secret（worker_id 一致性校验）

### 11. 跨 BC 关系

| 关系 | 描述 |
|---|---|
| **Workforce → SecretManagement**（Customer-Supplier）| AgentInstance.config.mcp_config 内嵌 SecretRef；worker daemon 经 SecretResolutionService.resolve 拿明文 |
| **Observability ← SecretManagement**（Open Host）| 订阅 `user_secret.*` 事件做审计投影；**`user_secret.accessed` 事件不含明文**，仅 metadata |
| **Cognition → SecretManagement**（read-only metadata）| Supervisor 可调 `secret list` / `secret usage` 看元数据做派单判断；不能 create / rotate / revoke / 读明文 |

### 12. Authorization 落地（worker daemon ↔ center 解析 RPC）

```
SecretResolutionService.resolve(name string, agent_instance_id ULID) (plaintext string, error)
  caller: worker daemon（session_token 认证）
  steps:
    1. SELECT user_secrets WHERE name=? AND state='active'  → if miss → ErrNotFound
    2. SELECT agent_instances WHERE id=agent_instance_id
       → verify worker_id == session.worker_id（防越权）
    3. AES-GCM decrypt(value_ciphertext, value_nonce, master_key) → plaintext
    4. UPDATE last_used_at = now
    5. emit user_secret.accessed { id, name, by=worker, agent_instance_id }
    6. return plaintext
```

## Consequences

**正面**：

- 用户密钥集中存 + 集中 rotate + 集中 audit
- DB 不留明文；明文不跨进程边界（仅 worker daemon 短暂持有）
- v2 G4 MCP 直接受益；E2 云凭据 / v3 repo deploy key 等无缝挂同模型
- SecretRef VO 在 config 文件里替代明文，**可放心 git diff / 截图分享**
- 跟 [ADR-0025](0025-agent-create-via-cli-not-protocol.md) 同源原则：资产配置归用户，supervisor 不持创建权

**负面 / 待跟进**：

- 新 BC 增加 DDD 数（BC8）+ 一张表（`user_secrets`）+ Repository + Service + CLI + 测试
- master_key 管理：master_key 一丢 → 所有 secret 不可解；运维上需用户自管 key 备份（写文档）
- master_key rotate 是大事：v2 不做（master key 滚动需要解-加密所有 ciphertext 行）；写进 roadmap
- worker daemon resolve RPC 增加 1 次 round-trip / spawn —— 性能影响微小
- 单次 secret rotate 不影响**正在跑的 execution**（home_dir/mcp_config.runtime.json 已落盘）；下次 spawn 才用新值

## Alternatives Considered

### A. 不引入 BC，纯 SecretRef VO + 各方自 resolve

- ✅ 工程量小
- ❌ 散乱：每方自存（env / file）；无集中 audit / rotate
- ❌ 用户要在多处放真值，认知负担高
- 否决：用户明确要中心化

### B. 完整 Secret BC 含系统凭证

- 同时迁 BootstrapToken / S3 key 进 SecretManagement BC
- ❌ BootstrapToken 有 state machine 紧耦合 Worker enroll；硬迁破坏 [ADR-0023](0023-worker-enroll-lightweight.md)
- ❌ 进程级单值凭证 env 已就位；搬来无 ROI
- 否决：scope 控制在用户密钥

### C. 直接接入外部 KMS / HashiCorp Vault / SOPS

- ✅ 工程成熟；rotation / audit 都有
- ❌ 个人工具用户没动力部署 Vault 等基础设施
- ❌ 多一层依赖；v1 vision「单 VPS 简洁」违和
- 推迟到 v3+ roadmap（接入 Vault 作为可选 master_key 来源）

### D. 用 OS keyring（macOS Keychain / Linux Secret Service）

- ✅ 集成深
- ❌ 跨机器（worker 在不同机）不便
- ❌ 跟「center 集中管理」直接冲突（每台机自己存）
- 否决

## References

### 同 BC

- [secret-management/00-overview.md](../../architecture/tactical/secret-management/00-overview.md)
- [secret-management/01-user-secret.md](../../architecture/tactical/secret-management/01-user-secret.md)

### 相关 ADR

- [ADR-0023 Worker Enroll 轻量化](0023-worker-enroll-lightweight.md)（BootstrapToken Entity 保留在 Workforce）
- [ADR-0024 AgentInstance 一等公民化](0024-agent-instance-first-class.md)（config.mcp_config 字段）
- [ADR-0025 agent:create 协议 = G1 endpoint](0025-agent-create-via-cli-not-protocol.md)（"资产配置归用户"原则）
- [ADR-0027 MCP per-agent 注入](0027-mcp-per-agent-injection.md)（G4 ADR；引用本 ADR）

### 跨 BC

- [workforce/04-agent-instance.md](../../architecture/tactical/workforce/04-agent-instance.md) —— AgentInstance.config.mcp_config 包含 SecretRef
- [agent-harness/01-prompt-assembly.md](../../architecture/tactical/agent-harness/01-prompt-assembly.md) —— worker daemon spawn 前 just-in-time resolve

### 横切

- [conventions § 13 安全 / 凭据处理](../../../rules/conventions.md)（系统凭证仍走原路径；user secret 走本 BC）

### 来源

- [docs/design/drafts/v2-kickoff-2026-05-22.md § Group 2 G4](../../drafts/v2-kickoff-2026-05-22.md)
- 2026-05-22 #agent-center 讨论中用户提议
