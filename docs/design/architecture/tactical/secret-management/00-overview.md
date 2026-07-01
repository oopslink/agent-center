# SecretManagement BC — DDD 战术设计 Overview

> **DDD 战术层** · BC: SecretManagement（BC8，Supporting Domain）
>
> 中心化管理**用户密钥**（user-domain secrets）：存储 / 读取 / rotate / revoke / audit。
>
> **本 BC 不管系统内部凭证**（BootstrapToken / session_token / 飞书 app_secret / S3 access key 等）—— 系统凭证由各 BC 自管，详见 [ADR-0023](../../../decisions/0023-worker-enroll-lightweight.md) / [conventions § 13](../../../../rules/conventions.md)。

设计依据：[ADR-0026 SecretManagement BC](../../../decisions/0026-user-secret-management-bc.md)。

---

## § 0. BC 一览

### 0.1 职责

| 维度 | 内容 |
|---|---|
| **聚合管理** | UserSecret（独立 AR）|
| **加密** | AES-GCM 256-bit；master key 从配置文件加载，**不入 DB**；明文不入 DB |
| **解析协议** | worker daemon spawn agent 前 just-in-time 拉明文 → 临时落盘（mode 0600）→ execution 后清理 |
| **审计** | 全部 access / rotate / revoke 落 domain event；access 事件不含明文 |
| **不在范围** | 系统内部凭证（BootstrapToken / session_token / 飞书 app_secret / S3 key）—— 各 BC 自管 |

### 0.2 UL 切片

- `UserSecret`（AR，用户密钥实体）
- `SecretRef`（VO，引用语法 `secret:<name>`）
- 行为动词：`Create` / `Rotate` / `Revoke` / `Resolve`（解析为明文）
- 状态机词汇：UserSecret `active` / `revoked`

### 0.3 Context Map 位置

- **Workforce → SecretManagement**（Customer-Supplier）—— AgentInstance.config.mcp_config 内嵌 SecretRef；worker daemon 调 SecretResolutionService.resolve 拿明文
- **Observability ← SecretManagement**（Open Host）—— 订阅 `user_secret.*` 事件做投影 / 审计；event payload 不含明文
- **Cognition → SecretManagement**（Read-only metadata）—— Supervisor 可看 secret 元数据做派单判断；不能 create / rotate / revoke / 读明文

---

## § 1. 聚合清单（X.1）

### 1.1 Aggregate Roots

| 聚合 | 文件 | 状态机 | 身份 / 不变性 |
|---|---|---|---|
| **UserSecret** | [01-user-secret.md](01-user-secret.md) | 2 态（active / revoked）| ULID `id`；`name` 全局唯一；revoked 是终态 |

### 1.2 Entity（子从属）

无。

### 1.3 Value Objects

| VO | 用在哪 | 描述 |
|---|---|---|
| **SecretRef** | mcp_config / 未来 cloud_credential 等字段内嵌 | 引用语法 `secret:<name>`；指向某 UserSecret |
| **SecretKind** | UserSecret.kind | `mcp` / `cloud_credential` / `repo_deploy_key` / `other`（开放扩展）|
| **MasterKey** | center 进程启动时加载 | 32 字节 base64，从配置文件读；不入 DB |
| **EncryptedValue** | UserSecret.value_ciphertext + value_nonce | AES-GCM 加密结果 |

---

## § 2. Invariants 索引（X.2）

- **UserSecret Invariants** → [01-user-secret.md § 5](01-user-secret.md)

**跨聚合 / 跨 BC 不变量**：

1. **DB 不存明文**：仅 `value_ciphertext` + `value_nonce`
2. **Master key 不入 DB / 不入 event payload / 不入 trace**
3. **CLI 不打印明文**（仅 create 成功时回显 Reference 标识）
4. **worker daemon resolve 必须 worker_id 跟 AgentInstance.worker_id 一致**（防越权）
5. **SecretRef 语法 `secret:<name>` 全局唯一定位 UserSecret**

---

## § 3. Domain Services（X.3）

### 3.1 UserSecretService

**职责**：UserSecret 全生命周期 —— create / rotate / revoke。

| 维度 | 内容 |
|---|---|
| 入参 | `CreateRequest { name, kind, plaintext, created_by }` / `RotateRequest { name, plaintext, by }` / `RevokeRequest { name, by, reason? }` |
| 校验 | name 全局唯一（create）；state=active（rotate / revoke）|
| 加密 | 内存里 AES-GCM 加密 → 写 ciphertext + nonce 入库；明文立即丢弃 |
| 事件 | `user_secret.created / rotated / revoked` |

详见 [01-user-secret.md § 6](01-user-secret.md)。

### 3.2 SecretResolutionService

**职责**：worker daemon 调，拿明文。

| 维度 | 内容 |
|---|---|
| 入参 | `ResolveRequest { name, agent_instance_id }`；caller = worker daemon（session_token 认证）|
| 出参 | `ResolveResponse { plaintext }` |
| 校验 | UserSecret.state=active；caller worker_id == agent_instance.worker_id |
| 解密 | AES-GCM decrypt(value_ciphertext, value_nonce, master_key) |
| 事件 | `user_secret.accessed { id, name, by=worker_id, agent_instance_id, accessed_at }`；失败 emit `user_secret.access_denied { name, by, reason }` |

详见 [01-user-secret.md § 7](01-user-secret.md)。

### 3.3 SecretUsageQueryService

**职责**：反查某 secret 被哪些 AgentInstance 引用。

| 维度 | 内容 |
|---|---|
| 入参 | `name` |
| 出参 | `[]AgentInstanceID`（按 worker_id 分组）|
| 实现 | 扫 `AgentInstance.config.mcp_config` 内 `secret:<name>` 引用（JSON 路径解析） |

详见 [01-user-secret.md § 8](01-user-secret.md)。

---

## § 4. Factories（X.4）

### 4.1 UserSecretFactory

**唯一 caller**：UserSecretService（§ 3.1）的 create 路径。

入参：`{ name, kind, plaintext, created_by }`。

内部：

```
plaintext → AES-GCM(master_key) → { ciphertext, nonce }
state = active
```

明文出 Factory 即丢。

---

## § 5. Repositories（X.5）

```go
type UserSecretRepository interface {
    FindByID(ctx context.Context, id UserSecretID) (*UserSecret, error)
    FindByName(ctx context.Context, name string) (*UserSecret, error)
    FindAll(ctx context.Context, filter UserSecretFilter) ([]*UserSecret, error)  // 返回元数据；ciphertext 字段调用方一般不读
    Save(ctx context.Context, s *UserSecret) error                                    // 新建 + rotate / revoke 全量写（含 version 乐观锁）
    UpdateState(ctx context.Context, id UserSecretID, from, to SecretState, version int) error
    UpdateValue(ctx context.Context, id UserSecretID, ciphertext, nonce []byte, version int) error  // rotate
    UpdateLastUsedAt(ctx context.Context, id UserSecretID, at time.Time) error          // resolve 时更新
}

// Domain errors
var (
    ErrSecretNotFound          = errors.New("secret-management: user secret not found")
    ErrSecretNameTaken         = errors.New("secret-management: name already taken")
    ErrSecretRevoked           = errors.New("secret-management: secret is revoked")
    ErrSecretVersionConflict   = errors.New("secret-management: version conflict")
    ErrSecretAccessDenied      = errors.New("secret-management: caller not authorized to resolve this secret")
    ErrMasterKeyUnavailable    = errors.New("secret-management: master key not loaded; center misconfigured")
)
```

### 5.x 约定

- DB 列存 `value_ciphertext bytea` + `value_nonce bytea`；FindAll 默认返回元数据可不查二进制列（按 driver 实现）
- Repository 不暴露明文
- Master key 由 SecretResolutionService 通过 `secret_management.master_key_file` 加载，Repository 不持有
- Domain errors 用 sentinel error pattern

---

## § 6. 跨聚合引用出方向（X.6）

| 引用方 → 被引方 | 强弱 | 触发场景 | ADR |
|---|---|---|---|
| **AgentInstance.config → UserSecret**（通过 SecretRef 字符串） | 弱 / 字符串引用 | 用户 `agent config set mcp_config=...` | [ADR-0026](../../../decisions/0026-user-secret-management-bc.md) / [ADR-0027](../../../decisions/0027-mcp-per-agent-injection.md) |

> 强引用走 DB FK 不做：AgentInstance.config 是 JSON 包，多个 SecretRef 共存；保持弱引用是务实选择。删 UserSecret 时**不**级联清 mcp_config（用户自己改 config 处理）。

---

## § 7. 跨 BC 交互

### 7.1 Supervisor 唤醒事件白名单

Cognition wake scheduler 不订阅 `user_secret.*` 事件 —— secret 生命周期跟 supervisor 决策无关。**例外**：`user_secret.access_denied` 可作为信号（worker resolve 失败 = 派单链上的故障，跟 NACK 同源），但目前 supervisor 通过 `task_execution.failed(reason=secret_unresolvable / mcp_config_invalid)` 已能感知，本 BC event 不进 wake 白名单。

### 7.2 Bridge 渲染（outbound）

无 —— Bridge 不直接渲染 secret 事件（敏感信息 by design）。

### 7.3 Observability 订阅

订阅全部 `user_secret.*` 事件做 audit projection（`secrets_audit_log`）。**事件 payload 不含明文**；只含 metadata（name, kind, by, accessed_at, agent_instance_id 等）。

### 7.4 Customer-Supplier 上下游汇总

| 上游 → 下游 | 模式 | 内容 |
|---|---|---|
| Workforce → SecretManagement | Customer-Supplier | AgentInstance.config 内嵌 SecretRef；worker daemon 调 SecretResolutionService.resolve |
| Cognition → SecretManagement | Read-only metadata | Supervisor `secret list / secret usage` 看元数据；永不读明文 |
| Observability ← SecretManagement | Open Host | 全部 `user_secret.*` 事件订阅做审计 |

完整 context map 见 [strategic/03-bounded-contexts § 3](../../strategic/03-bounded-contexts.md)。

---

## § 8. Out-of-Scope / Future Work

| 项 | 归属 |
|---|---|
| 系统内部凭证迁入（BootstrapToken / app_secret / S3 key）| [roadmap](../../../roadmap.md)（v3+，看 ROI 决策）|
| Master key rotation | [roadmap](../../../roadmap.md)（需 re-encrypt 所有 ciphertext；v2 不做）|
| 外部 KMS / Vault / SOPS 接入 | [roadmap](../../../roadmap.md)（v3+ 作为可选 master_key 来源）|
| 多用户 / SaaS 隔离 | [out-of-scope](../../../requirements/03-out-of-scope.md)（v1 vision 单用户）|
| Secret expiration / TTL 自动 revoke | [roadmap](../../../roadmap.md)（v2 用户手动 rotate / revoke）|

---

## § 9. References

### 相关 ADR

- [ADR-0026 SecretManagement BC](../../../decisions/0026-user-secret-management-bc.md)（本 BC 立项）
- [ADR-0027 MCP per-agent 注入](../../../decisions/0027-mcp-per-agent-injection.md)（消费者）
- [ADR-0023 Worker Enroll 轻量化](../../../decisions/0023-worker-enroll-lightweight.md)（BootstrapToken 留在 Workforce）
- [ADR-0024 AgentInstance 一等公民化](../../../decisions/0024-agent-instance-first-class.md)（config.mcp_config 字段）
- [ADR-0025 agent:create 协议 = G1 endpoint](../../../decisions/0025-agent-create-via-cli-not-protocol.md)（"资产配置归用户"）

### 战略层

- [strategic/03-bounded-contexts § 2 BC8 SecretManagement](../../strategic/03-bounded-contexts.md)
- [strategic/03-bounded-contexts § 3 Context Map](../../strategic/03-bounded-contexts.md)

### 跨 BC

- [agent-harness/01-prompt-assembly.md](../agent-harness/01-prompt-assembly.md) — worker daemon 注入流程

### 横切

- [conventions § 13 安全 / 凭据处理](../../../../rules/conventions.md)
- [implementation/04-configuration.md § 7.10 secret_management.*](../../../implementation/04-configuration.md)
