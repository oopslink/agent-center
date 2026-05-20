---
layout: home

hero:
  name: agent-center
  text: DDD 设计文档
  tagline: 7 个限界上下文 / 21 个 ADR / 战略 + 战术 + 实现层完整呈现
  actions:
    - theme: brand
      text: 战略层入口（必读）
      link: /design/architecture/strategic/00-domain-vision
    - theme: alt
      text: 战术层入口（7 BC）
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
    details: 7 个 BC 各自的聚合 / Domain Service / Factory / Repository / Invariants / 跨 BC 协作。
    link: /design/architecture/tactical/task-runtime/00-overview
    linkText: 浏览 BC
  - icon: 📜
    title: 决策（ADR）
    details: 21 个架构决策记录，含演进链（0007 → 0009 → 0017 → 0020 → 0021）。
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
    details: BlobStore / 持久化 schema / CLI 子命令等实施细节（部分 TBD）。
    link: /design/implementation/
    linkText: 实现层
---

## 7 个限界上下文（BC）地图

```mermaid
flowchart TB
    subgraph Vendor[" "]
        feishu[飞书]
        dingtalk[DingTalk]
        web[Web chat]
    end

    subgraph B[BC7 Bridge ACL 唯一调 vendor SDK]
        FB[FeishuBridge]
        DB[DingTalkBridge<br/>v2+]
        WB[WebBridge<br/>v2+]
    end

    feishu -. WebSocket .-> FB
    dingtalk -. WebSocket .-> DB
    web -. WebSocket .-> WB

    subgraph Domain[领域层 BC1-BC6]
        BC1[BC1 TaskRuntime<br/>Task / TaskExecution / InputRequest<br/>+ worker 运行时]
        BC2[BC2 Discussion<br/>Issue 单聚合]
        BC3[BC3 Workforce<br/>Worker / Project / Mapping / Proposal]
        BC4[BC4 Cognition<br/>SupervisorInvocation / Memory]
        BC6[BC6 Conversation<br/>Conversation / Message / Identity]
    end

    BC2 -.Shared Kernel 1:1.-> BC6
    BC1 -.Shared Kernel 1:1.-> BC6
    BC2 -- Customer-Supplier --> BC1
    BC1 -- Customer-Supplier --> BC2
    BC1 -.Shared Kernel.-> BC3
    BC2 -.Shared Kernel.-> BC3

    B -- Customer-Supplier --> BC2
    B -- Customer-Supplier --> BC6
    B -- Customer-Supplier --> BC1
    BC6 -. Pub/Sub .-> B

    subgraph Cross[跨 BC actor]
        BC5[BC5 Observability<br/>Open Host / Subscribe-only]
    end

    BC1 -.->|emit events| BC5
    BC2 -.->|emit events| BC5
    BC3 -.->|emit events| BC5
    BC4 -.->|emit events| BC5
    BC6 -.->|emit events| BC5
    B -.->|emit events| BC5

    BC4 ==>|User via tools| BC1
    BC4 ==>|User via tools| BC2
    BC4 ==>|User via tools| BC6

    classDef domainBox fill:#e8f4f8,stroke:#1e88e5,stroke-width:2px
    classDef bridgeBox fill:#fff3e0,stroke:#fb8c00,stroke-width:2px
    classDef obsBox fill:#f3e5f5,stroke:#8e24aa,stroke-width:2px
    classDef vendorBox fill:#fafafa,stroke:#9e9e9e,stroke-width:1px,stroke-dasharray: 5 5
    class BC1,BC2,BC3,BC4,BC6 domainBox
    class FB,DB,WB,B bridgeBox
    class BC5 obsBox
    class feishu,dingtalk,web vendorBox
```

**关键解读**：

- **领域层 BC1-BC6**：零 vendor 依赖；只跟其它 BC 通过 Shared Kernel / Customer-Supplier 模式交互；所有外发通过 emit domain events
- **Bridge BC7（ACL）**：唯一调 vendor SDK 的地方；订阅领域事件做 outbound；inbound 调领域 API 写入；不持业务聚合
- **Observability BC5（Open Host）**：所有 BC emit 事件到 `events` 表；只订阅不发起，提供统一查询接口（inspect / query / ps / stats / logs）
- **Cognition BC4 跨切**：Supervisor 通过 CLI 工具（同 user 用的同一套）调任何 BC 的动作命令；不为 supervisor 单造 RPC

## DDD 推进状态

7 个 BC 的战术设计 + Repository 接口签名全部 ✅；剩 implementation 层 SQL schema / dialect 适配（TBD）+ Saga（v1 不必）。详细推进 plan 见 [DDD 蓝图](/design/ddd-blueprint)。

## ADR 演进主线

```mermaid
graph LR
    A0007[ADR-0007<br/>Conversation 层] -- Refined by --> A0009[ADR-0009<br/>Issue 解耦]
    A0007 -- Refined by --> A0021
    A0009 -- Superseded by --> A0021[ADR-0021<br/>Issue ↔ Conversation 1:1]
    A0016[ADR-0016<br/>bound thread] -- Superseded by --> A0017[ADR-0017<br/>Task ↔ Conversation 1:1]
    A0017 -- Refined by --> A0021
    A0020[ADR-0020<br/>Card 限制 Bridge<br/>中间方案] -- Superseded by --> A0021

    classDef accepted fill:#e8f5e9,stroke:#43a047,stroke-width:2px
    classDef superseded fill:#fafafa,stroke:#9e9e9e,stroke-width:1px,stroke-dasharray: 5 5
    classDef current fill:#fff9c4,stroke:#fbc02d,stroke-width:3px
    class A0007,A0017 accepted
    class A0009,A0016,A0020 superseded
    class A0021 current
```

完整 21 个 ADR 见 [决策索引](/design/decisions/)。
