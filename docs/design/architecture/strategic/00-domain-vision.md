> ⚠ **v1-era doc** — pending rewrite in Phase 10 / 11. v2 撤回了 Bridge BC + 飞书集成 (per [ADR-0031](../../decisions/0031-v2-drop-bridge-vendor-integration.md))；本文中 Bridge / vendor / 飞书 / 已删 ADR 引用是 v1 残留。

# agent-center 领域愿景（Domain Vision）

> **DDD 战略层**

> Domain Vision Statement (Eric Evans) —— agent-center 存在的根本理由 + 关键策略立场 + 核心边界。
>
> 跟 [00-system-overview](02-system-overview.md)（工程视角：拓扑 / 角色 / 数据流）区别：本文档讲**领域取舍**，不讲实现机制。
>
> 跟 [01-bounded-contexts § 1 通用语言](03-bounded-contexts.md#-1-通用语言ubiquitous-language) 区别：UL 是**术语权威**，本文档是"为什么有这些术语"的锚。

最后更新：2026-05-17。

---

## § 1. 核心价值（Thesis）

**agent-center 是给个人用户的多 AI agent 中控——让 LLM-driven Supervisor agent 代替人，看着 N 个 worker agent 同时在多个项目上干活（编码 / 写作 / 投研 / 等）。**

不存在的话，用户得自己肉眼盯多个 agent 进程 / 终端窗口：谁干什么、何时介入、出了事怎么办——这个**认知负担**是 agent-center 接管的核心问题。

## § 2. 关键策略立场（Strategic Stances）

四条相互支撑的立场，每条都是"反常识的赌注"——区别于同类系统的常规做法：

### S1. 调度逻辑 prompt 化

派单 / 重派 / Issue 收敛 / 跨任务协调，全由 Supervisor agent 读上下文自己决策；**不写硬编码 scheduler，不维护策略配置表，不上 DAG 引擎**。

- 反面：传统 task queue 有 worker pool + retry policy + priority queue 等机制；agent-center 把这些塞进 LLM agent 的 prompt
- 依据：[conventions § 3 AI Native](../../../rules/conventions.md#-3-ai-native) + [ADR-0003](../../decisions/0003-supervisor-not-brain.md)

### S2. Supervisor 短生命周期 + 文件 Memory

Supervisor **不是常驻 daemon**，事件触发 spawn 一次就退出；跨次记忆走 markdown 文件（不进 DB session 表）。

- 反面：常规 daemon + in-memory state + DB-persisted session
- 依据：[ADR-0013](../../decisions/0013-supervisor-invocation-concurrency.md) + [ADR-0012](../../decisions/0012-memory-file-based.md)

### S3. 对话即决策审计

Supervisor 每一步关键决策都经 用户 UI（v1: → v2: Web Console / CLI per ADR-0031）对用户可见 + 可干预；**不藏后台、不静默调度**。

- 反面：传统调度系统决策落 log / DB，要查时去后台翻
- 依据：[discussion/00-overview](../../retired/discussion/00-overview.md)（RETIRED / historical）+ [conversation/00-overview](../tactical/conversation/00-overview.md) + [ADR-0007](../../decisions/0007-conversation-as-unified-session.md)（Conversation 作统一会话层）+ [ADR-0039](../../decisions/0039-conversation-business-model-v2-unified.md)（v2 统一 Conversation 业务模型；原 ADR-0021 Issue ↔ Conversation 1:1 已 superseded）+ （v2 撤回 per ADR-0031）

### S4. Center 单一权威

Worker / agent **不主动造任务、不互相派单**；所有任务来源由 Center 仲裁，Worker 想要新工作只能开 Issue 让 Center 拍板。

- 反面：自治 agent / 分布式协同 / agent mesh
- 依据：[conventions § 1 单一来源 / 无野任务](../../../rules/conventions.md#-1-单一来源--无野任务)

## § 3. 边界（Boundaries）

两条决定核心定位的边界——**不是**完整出范围清单（详见 [requirements/03-out-of-scope.md](../../requirements/03-out-of-scope.md)），只是"读者看到立刻明白 agent-center **不是什么**"的锚。

### B1. 不接 git / SCM

agent-center **不做** clone / pull / push / merge / 冲突解决。git 是 agent 自己干活的一部分（提交 / 推送 / 发版等）；Worker enroll 时声明"我有项目 X 在路径 Y"。

→ 跟 GitHub Actions / Dagger / Tekton 等 CI/CD 类工具的本质区别。

### B2. v1 是个人工具，不是多租户 SaaS

v1 范式：单 VPS / 单库 SQLite / 无 auth / 无 tenant / SSH 跑 admin。多用户 / SaaS 是"长期愿景"——见 [roadmap § 长期愿景](../../roadmap.md#长期愿景)——按 roadmap 说法"基本是重新做项目"。

→ v1 所有简化决策（无 RBAC / 无 tenant 隔离 / 无计费）的总开关。

---

## § 4. 相关文档

| 文档 | 视角 |
|---|---|
| 本文档 | 领域 / Domain Vision |
| [00-system-overview](02-system-overview.md) | 工程 / 拓扑 / 数据流 |
| [01-bounded-contexts](03-bounded-contexts.md) | 战略 / BC + UL + Context Map |
| [requirements/00-overview](../../requirements/00-overview.md) | 需求总览 |
| [requirements/03-out-of-scope](../../requirements/03-out-of-scope.md) | 完整出范围清单 |
| [ddd-blueprint](../../ddd-blueprint.md) | DDD 推进状态 |

**Subdomain 标注**（Core / Supporting / Generic）以本文档为锚，落 [ddd-blueprint P4](../../ddd-blueprint.md#-32-中优架构清晰度)。
