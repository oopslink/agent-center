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

## 质量红线

- **所有提交必须通过测试。** 任何 commit 之前必须先运行 `cd web && pnpm test`（前端）或 `go test ./...`（后端），确认 0 failures 后才能 commit。不允许以 "已有失败" 为由跳过。
- **PR 合并前必须全绿。** main 分支上不允许存在失败的测试。如果发现 main 上有失败，优先修复后再开始新功能。
- **不允许 `--no-verify` 跳过 hooks。** pre-commit / pre-push hooks 存在即必须通过。

## 文档导航

`docs/` 按四类组织：

| 类别 | 目录 | 说明 |
|---|---|---|
| 项目规约 | [`docs/rules/`](docs/rules/) | conventions / 文档管理 / 测试 / 发布流程 / 验收规范 / 设计系统 |
| 系统设计 | [`docs/design/`](docs/design/) | requirements / architecture / implementation / decisions / features / deployment / migration |
| 发布产物 | [`docs/releases/`](docs/releases/) | 按版本归档的 release notes / 验收报告 / 截图证据 |
| 专项计划 | [`docs/plans/`](docs/plans/) | 实现计划 / 审计 / 测试报告（已完成的归 `archived/`）|

设计文档入口：[docs/design/README.md](docs/design/README.md)。

## 项目简介

参见 [README.md](README.md)。
