# CLAUDE.md

本文件引用 agent-center 项目的协作规则与设计文档导航。任何代码 agent / 人类协作者在改动本仓库前，请先读相关规则。

## 规则索引

所有规则集中在 [`docs/rules/`](docs/rules/) 下：

| 规则 | 描述 |
|---|---|
| ⭐ **[项目规约](docs/rules/conventions.md)** | **MUST-READ**：所有设计 / 开发 / 测试前必读。**§ 0 DDD + 统一语言是设计方法论根基**；其它跨切原则（单一来源、可观测性、AI native、零 LLM SDK 等）+ 自检清单 |
| [文档管理](docs/rules/documentation.md) | 设计文档目录组织、分层职责、ADR 格式、"出范围 vs 推迟"区分、doc-first 工作流 |
| [测试规约](docs/rules/testing.md) | 单元行覆盖率 ≥ 90%、测试计划/报告模板、关键 e2e 路径、契约测试、可测性 |

**新功能 / 新设计提交前**：过 [项目规约 § 15 自检清单](docs/rules/conventions.md#-15-新功能--新设计自检清单)。

## 设计文档

正式设计文档统一在 [`docs/design/`](docs/design/) 下，分层：

- [requirements/](docs/design/requirements/) — 需求层
- [architecture/](docs/design/architecture/) — 架构层
- [implementation/](docs/design/implementation/) — 实现层
- [decisions/](docs/design/decisions/) — ADR

入口：[docs/design/README.md](docs/design/README.md)。

## 项目简介

参见 [README.md](README.md)。
