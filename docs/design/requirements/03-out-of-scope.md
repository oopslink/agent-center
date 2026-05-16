# 出范围（永远不做）

本文档列出**边界决策** —— 永远不在 agent-center 职责里的能力，或跟核心定位冲突的需求。

> **跟"推迟"的区分：** 节奏决策（v1 不做但早晚要做）见 [roadmap.md](../roadmap.md)，不混在这里。规则见 [conventions.md § 6](../../rules/conventions.md#-6-范围决策两分出范围-vs-推迟)。

一旦写下，重新讨论需要新证据，不轻易翻案。

---

## 1. Git 同步（clone / pull / push / merge / 冲突解决）

agent-center 是任务调度器，不是 SCM 工具。git 操作是 agent 自己干活的一部分（commit、PR 创建）；让 center 介入 git 会跨进它的核心定位，并带来 per-project 凭据 / 冲突策略 / 仓库元数据等一连串负担。Worker enroll 时声明"我有项目 X 在路径 Y"是有意为之的边界。

## 2. Worker 之间直接互派任务

跟 [conventions § 1 单一来源 / 无野任务](../../rules/conventions.md#-1-单一来源--无野任务) 正面冲突。Worker → Worker 直接派单 = 绕开 center 的权威，打破"center 是任务唯一权威"这条根本原则。Worker 想协调别人来干活的正确路径是开 Issue 让 center / 用户拍板。

## 3. Trace 全文向量检索

agent-center 是调度 + 结构化观测平台，不是搜索引擎。个人单用户场景的 trace 数据量天花板有限（百 task / 天 × 全年 ≈ 几 GB），grep / SQL LIKE 永远够用。向量检索 = embedding 模型 + vector DB + 索引维护成本，对个人项目是过度工程。

## 4. Per-project 自定义 task lifecycle

所有 task 走同一套 A2A 状态机（submitted / working / input_required / completed / failed / canceled）是有意的统一。Per-project 自定义会碎片化 observability（fleet view 跨不了项目）、复杂化 supervisor 状态推理、混乱不同项目的语义。**不同领域的"状态展示名称"差异是 UI 层问题**（写作项目里 `completed` 可以显示为"已交稿"），不是状态机问题 —— 这种支持。

---

## 由独立 ADR 承载的边界决策

下列决策的"不做"由专属 ADR 单独承载，不在本文重复列出：

| 决策 | ADR |
|---|---|
| 不引入 MCP 协议 | [ADR-0001](../decisions/0001-no-mcp.md) |
| 不引入 LLM SDK 依赖 | [ADR-0002](../decisions/0002-no-llm-sdk-use-cli-agents.md) |
| Agent-center 不维护项目宪章 / charter 文档 | [ADR-0005](../decisions/0005-project-charter-stays-in-project-repo.md) |
