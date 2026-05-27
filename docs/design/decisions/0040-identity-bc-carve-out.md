# 0040. Identity BC carve-out（v2.6）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-27 |
| Delivered | v2.6 design phase；详 [v2.6-design § 3.2 / § 4](../../plans/v2.6-design.md) |
| Supersedes | ~~[ADR-0033 Identity 模型重构](0033-identity-model-refactor.md)~~（v2.6 重新设计 Identity 模型 + BC 边界 + ID 格式 + kind 枚举；详 [ADR-0045](0045-identity-id-format.md)） |
| Related | v2.6 周期一并立的 [ADR-0041 Organization](0041-organization-multi-tenant.md) / [ADR-0042 Member](0042-member-ar.md) / [ADR-0043 Auth](0043-auth-passcode-jwt.md) / [ADR-0044 Supervisor cut](0044-supervisor-cut.md) / [ADR-0045 Identity ID format](0045-identity-id-format.md) |

## Context

v2 GA 时 [ADR-0033](0033-identity-model-refactor.md) 将 Identity AR 留在 **BC6 Conversation** 内部，理由是当时 Identity 主要服务于 Message.sender 引用，且当时 v2 明确「不做多租户 / 不做组织概念」（[ADR-0033 § Context 用户表态](0033-identity-model-refactor.md)）。

### v2.6 前后浮现的问题

1. **Identity 语义远超 Conversation 域**：v2.6 引入 Organization + Member + Auth 后，Identity 自身的语义（actor / permission 挂载点 / 跨 BC 引用根）大幅超出 Conversation BC 的"消息时间线"职责，违反 Single Responsibility
2. **Member 模型无所归属**：v2.6 引入的 Member AR（Identity ↔ Organization 关系）若塞进 Conversation BC，跟 channel/message 概念无关，污染该 BC
3. **Organization 模型无所归属**：同理，Organization 是 v2.6 新概念，跨 BC 容器，硬塞 Conversation BC 不合适
4. **Auth 跨切面**：passcode + JWT + signup/signin/logout 流程需要一个统一的"身份门面"BC 来承载，而非散布在多个 BC
5. **跨 BC ACL 复杂度**：v2.6 全表 `organization_id` 化（[ADR-0041](0041-organization-multi-tenant.md)）后，几乎所有 BC 都要 reference Identity / Organization；Identity 留在 Conversation BC = Conversation 成为隐式的中转节点

### 决策出发点

v2.6 把 Identity 从 Conversation BC 抽出，提升为独立 BC9 Identity，承载：
- Identity AR（从 Conversation 迁出）
- Organization AR（新增）
- Member AR（新增）
- Invitation AR（新增，schema 占位 only）

## Decision

### 1. 立 BC9 Identity 为独立 Bounded Context

| 属性 | 值 |
|---|---|
| BC 序号 | BC9 |
| 中文名 | 身份上下文 |
| 子域分类 | **Supporting-Essential**（业务支撑，所有 BC 依赖，但不直接产生用户价值） |
| 持久化 | SQLite 主库；与现有 BC 同库共存 |
| 上下游 | 上游 (Supplier)；所有其它 BC 是下游 (Customer) |

### 2. BC9 Identity 聚合清单

| AR | 职责 | 详 spec | 详 ADR |
|---|---|---|---|
| **Identity** | 全局唯一身份记录（user / agent） | [v2.6-design § 4.2.1](../../plans/v2.6-design.md) | [ADR-0045](0045-identity-id-format.md) |
| **Organization** | 多租户工作空间 + slug 路由 | [v2.6-design § 4.2.2](../../plans/v2.6-design.md) | [ADR-0041](0041-organization-multi-tenant.md) |
| **Member** | Identity↔Organization 关系，承载 role + status | [v2.6-design § 4.2.3](../../plans/v2.6-design.md) | [ADR-0042](0042-member-ar.md) |
| **Invitation** | 邀请记录（schema 占位，流程 v2.7） | [v2.6-design § 4.2.4](../../plans/v2.6-design.md) | — |

### 3. Conversation BC 瘦身

- Identity AR 从 BC6 Conversation 迁出 → BC9 Identity
- `Message.sender_identity_id` 从 BC 内引用变 BC 间 ID 引用（弱关联 + AppService 写时校验，per [v2.6-design § 4.8.3 AS-2](../../plans/v2.6-design.md)）
- Conversation BC 不再 INSERT / UPDATE Identity 行；只读 Identity 用作 author 显示
- 现有 `docs/design/architecture/tactical/conversation/02-identity.md` 归档为 historical；新 `tactical/identity/00-overview.md` + `tactical/identity/01-identity.md` 承载

### 4. Context Map：Customer-Supplier

```
                    ┌──────────────────┐
                    │  BC9 Identity    │← Customer-Supplier 上游
                    └────────┬─────────┘
       ┌────────────┬────────┼────────────┬────────────┐
       ▼            ▼        ▼            ▼            ▼
  Conversation  Workforce  TaskRuntime  Cognition   SecretManagement
  (msg.sender)  (org+ai)   (actor)      (subscr)    (org_id)
```

- **关系类型**：Customer-Supplier；ID 引用；弱关联（SQLite FK 软约束）；事件驱动级联清理
- **Anti-Corruption Layer**：暂不立独立 ACL；通过 published `identity.id` / `organization.id` / `member.id` 走 ID 引用 + `IdentityBCFacade` 暴露 read-only 查询给下游 BC
- **一致性窗口**：弱关联，Identity / Organization 软删不立即级联，下游 BC 订阅 Domain Event 异步处理

### 5. 物理目录约定

按 [DDD blueprint § 1.2](../ddd-blueprint.md) 约定：

```
docs/design/architecture/tactical/identity/
├── 00-overview.md         (BC entry + § X.1-X.6 wrap)
├── 01-identity.md         (Identity AR)
├── 02-organization.md     (Organization AR)
├── 03-member.md           (Member AR)
└── 04-invitation.md       (Invitation AR; v2.7 实现)
```

代码层组织（Go）：

```
internal/identity/                  (新增 BC9)
├── domain/                          (AR + VO + Domain Service)
├── repository/                      (Repository 实现)
├── appservice/                      (Application Service)
└── facade/                          (IdentityBCFacade for downstream BCs)

internal/conversation/identity/      (旧位置 — v2.6 后删除)
```

## Consequences

### 正面

- BC 边界更清晰：Identity / Organization / Member 同源管理；跨 BC 引用通过 published ID + Facade
- Conversation BC 责任收窄，回归"消息时间线"本职
- 为后续 SaaS 演进（v2.7+ 多用户 / Email / SSO / PAT / ABAC）打好 BC 容器
- DDD blueprint 8 BC active landscape（含 BC8 SecretManagement + BC9 Identity 新增）
- BC9 自治承载 Auth 流程，集中而非散布

### 负面 / 待跟进

- **现有代码 + schema 迁移工作量大**：identity 表迁出 + Conversation BC 内引用全改 + Workforce BC `agent_instances.identity_id` 显式 FK 改造（详 [v2.6-design § 5 / § 9](../../plans/v2.6-design.md)）
- **跨 BC FK 软约束**：SQLite 跨表 FK + CHECK 在事务层校验有限；应用层 `IdentityBCFacade` + `AS-1/AS-2/AS-3` invariants 守门（详 [v2.6-design § 4.8.3](../../plans/v2.6-design.md)）
- **历史 ADR supersede 链**：[ADR-0033](0033-identity-model-refactor.md) 全文 supersede；ADR-0024 / 0029 / 0034 等涉及 Identity / AgentInstance 联动的 ADR 需 cross-reference 更新
- **现有 system Identity 行**：v2 时 ADR-0033 § 1 引入的 `system` singleton Identity，v2.6 删除（[ADR-0045 § 2](0045-identity-id-format.md)）；纯系统动作 actor=NULL（v2.6-design 决策 #20）
- **Branch 工作**：v2.6 在独立 `v2.6` 分支推进，避免影响 main 上的 v2.5.x 小版本节奏

## Alternatives Considered

### A. 保留 Identity 在 Conversation BC，新增 Org/Member 各自独立 BC

- ✅ 最小动作：Identity 不动
- ❌ Identity 跨 BC 引用根仍在 Conversation BC，跨 BC ACL 复杂
- ❌ 三个 BC（Conversation / Org / Member）协调，跨 BC invariant 更多
- 否决：违反 cohesion；Identity / Org / Member 同语义簇应同 BC

### B. 不立独立 BC，把 Org / Member 也塞进 Conversation BC

- ✅ BC 数量不变
- ❌ Conversation BC 严重 mission creep；从"消息时间线"变成"身份/组织/成员/消息"四不像
- ❌ v2.6 后的 v2.7+ SaaS 演进时还得再抽 BC，二次重构
- 否决

### C. 把 Identity / Org / Member 直接放 application 层不立 BC

- ✅ 跳过 BC 概念开销
- ❌ 违反 DDD 战略设计；其它 BC 已有清晰 BC 边界，唯独 Identity 不立 BC 不对称
- ❌ Domain Event 命名 / Repository 归属 / Domain Service 归属都没地方放
- 否决

## References

### v2.6 ADRs

- [ADR-0041 Organization concept + multi-tenant schema](0041-organization-multi-tenant.md)
- [ADR-0042 Member AR (Identity↔Organization)](0042-member-ar.md)
- [ADR-0043 Auth v2.6 (passcode + JWT cookie)](0043-auth-passcode-jwt.md)
- [ADR-0044 Supervisor cut](0044-supervisor-cut.md)
- [ADR-0045 Identity ID format (worker-style)](0045-identity-id-format.md)

### 影响 BC

- [tactical/identity/00-overview.md](../architecture/tactical/identity/00-overview.md)
- [tactical/conversation/02-identity.md](../architecture/tactical/conversation/02-identity.md) — 归档 historical
- [tactical/workforce/](../architecture/tactical/workforce/) — AgentInstance.identity_id FK 改造

### 来源

- 2026-05-26 / 2026-05-27 #agent-center DM 讨论（@oopslink ↔ @AgentCenterPD）
- [v2.6-design.md](../../plans/v2.6-design.md) — v2.6 主体 spec
