---
layout: home

hero:
  name: agent-center
  text: DDD 设计文档
  tagline: 6 个限界上下文 / 34 个 ADR / 战略 + 战术 + 实现层完整呈现（v2.0 GA 2026-05-24）
  actions:
    - theme: brand
      text: 战略层入口（必读）
      link: /design/architecture/strategic/00-domain-vision
    - theme: alt
      text: 战术层入口（6 BC）
      link: /design/architecture/tactical/task-runtime/00-overview
    - theme: alt
      text: ADR 索引
      link: /design/decisions/
    - theme: alt
      text: DDD 蓝图（进度 / Plan）
      link: /design/ddd-blueprint

features:
  - icon: 🎯
    title: 战略层
    details: 领域愿景 + 子域分类 + 系统总览 + 限界上下文 / Ubiquitous Language。先看这里建立全局视角。
    link: /design/architecture/strategic/00-domain-vision
    linkText: 进入战略层
  - icon: 🧩
    title: 战术层
    details: 6 个 BC 各自的聚合 / Domain Service / Factory / Repository / Invariants / 跨 BC 协作。
    link: /design/architecture/tactical/task-runtime/00-overview
    linkText: 浏览 BC
  - icon: 📜
    title: 决策（ADR）
    details: 34 个架构决策记录（v1 期 0001-0019 + v2 期 0023-0039）；v2 撤回了 v1 的 0009/0017/0020/0021/0022。
    link: /design/decisions/
    linkText: ADR 索引
  - icon: 🛠
    title: 项目规约
    details: 跨切原则 + DDD 方法论 + 命名 + 可观测性 + 测试 + 持久化等硬约束。
    link: /rules/conventions
    linkText: Conventions
  - icon: 🗺
    title: 蓝图（Plan）
    details: DDD 设计推进 plan + status。哪些做完、哪些下一步、driver 是哪条 ADR。
    link: /design/ddd-blueprint
    linkText: 蓝图
  - icon: 🔧
    title: 实现层
    details: BlobStore / 持久化 schema / CLI 子命令 / Web Console SPA / 部署等实施细节。
    link: /design/implementation/
    linkText: 实现层
---

## 6 个限界上下文（BC）地图

> v2 架构（per [ADR-0031](/design/decisions/0031-v2-drop-bridge-vendor-integration.md) 撤回 v1 Bridge BC + vendor 集成；新增 BC8 [SecretManagement](/design/decisions/0026-user-secret-management-bc.md)）。用户入口收窄到 Web Console + CLI（per [ADR-0037](/design/decisions/0037-web-console-as-main-user-ui.md) / [ADR-0038](/design/decisions/0038-cli-ux-enhancement.md)）。

```mermaid
flowchart TB
    subgraph User[用户层]
        WC[Web Console SPA<br/>+ CLI]
    end

    subgraph Domain[领域层 BC1-BC6 + BC8]
        BC1[BC1 TaskRuntime<br/>Task / TaskExecution / InputRequest<br/>+ worker 运行时]
        BC2[BC2 Discussion<br/>Issue 单聚合]
        BC3[BC3 Workforce<br/>Worker / Project / Mapping / Proposal<br/>+ AgentInstance / BootstrapToken]
        BC4[BC4 Cognition<br/>SupervisorInvocation / Memory]
        BC6[BC6 Conversation v2<br/>Conversation / Message / Identity / ChannelMgmt / CarryOver / Derivation]
        BC8[BC8 SecretManagement<br/>UserSecret + SecretRef VO]
    end

    WC -- HTTP/SSE --> BC1
    WC -- HTTP/SSE --> BC2
    WC -- HTTP/SSE --> BC3
    WC -- HTTP/SSE --> BC6
    WC -- HTTP/SSE --> BC8

    BC2 -.Shared Kernel 1:1.-> BC6
    BC1 -.Shared Kernel 1:1.-> BC6
    BC2 -- Customer-Supplier --> BC1
    BC1 -- Customer-Supplier --> BC2
    BC1 -.Shared Kernel.-> BC3
    BC2 -.Shared Kernel.-> BC3
    BC3 -- references --> BC8

    subgraph Cross[跨 BC actor]
        BC5[BC5 Observability<br/>Open Host / Subscribe-only]
    end

    BC1 -.->|emit events| BC5
    BC2 -.->|emit events| BC5
    BC3 -.->|emit events| BC5
    BC4 -.->|emit events| BC5
    BC6 -.->|emit events| BC5
    BC8 -.->|emit events| BC5

    BC4 ==>|via CLI tools| BC1
    BC4 ==>|via CLI tools| BC2
    BC4 ==>|via CLI tools| BC6

    classDef domainBox fill:#e8f4f8,stroke:#1e88e5,stroke-width:2px
    classDef obsBox fill:#f3e5f5,stroke:#8e24aa,stroke-width:2px
    classDef userBox fill:#fff3e0,stroke:#fb8c00,stroke-width:2px
    class BC1,BC2,BC3,BC4,BC6,BC8 domainBox
    class BC5 obsBox
    class WC userBox
```

**关键解读（v2）**：

- **领域层 BC1-BC6 + BC8**：零 vendor 依赖；BC 之间通过 Shared Kernel / Customer-Supplier 模式交互；所有外发通过 emit domain events
- **用户入口**：Web Console SPA（loopback bind, [ADR-0037](/design/decisions/0037-web-console-as-main-user-ui.md)）+ CLI；vendor IM 接入留给 v3+ 重新设计
- **Observability BC5（Open Host）**：所有 BC emit 事件到 `events` 表；只订阅不发起，提供统一查询接口（inspect / query / ps / stats / logs）
- **Cognition BC4 跨切**：Supervisor 通过 CLI 工具（同 user 用的同一套）调任何 BC 的动作命令；不为 supervisor 单造 RPC
- **SecretManagement BC8**：v2 新增；中心化 user secret 管理；plaintext 永不在 UI / API / SSE / log 出现（[ADR-0026 § 5](/design/decisions/0026-user-secret-management-bc.md)）

## DDD 推进状态

v2 GA 闭环：6 个 BC 的战术设计 + Repository 接口签名 + 实现层 SQL schema / SQLite 适配全部 ✅。详细推进 plan 见 [DDD 蓝图](/design/ddd-blueprint)。
