# 0006. 大文件走 BlobStore，DB 只存相对路径

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-16 |

## Context

agent-center 会产生几类**大块内容**：

- 任务原始日志归档（`.log.gz`，MB 级）
- Agent trace 归档（`.jsonl.gz`，KB-MB 级）
- 未来可能的 Issue / 任务附件

把这些塞进 SQLite / Postgres 是反模式：

- 拖慢小查询
- DB 备份成本爆炸
- 难以独立扩展存储后端

## Decision

引入 **BlobStore 抽象**：

- 大文件**不进 DB**
- DB 表里只存**相对路径**（如 `tasks/42/log.log.gz`）
- BlobStore 接口提供 Put / Get / Delete / Exists / List / URL
- v1 实现：**LocalDirBlobStore**（本地目录）
- 未来扩展：S3-compatible (MinIO / OSS / AWS S3 / R2)
- 切换实现**不需要改 DB 数据**（路径仍然有效）

详细接口、路径约定、配置、迁移流程见 [implementation/01-blob-store.md](../implementation/01-blob-store.md)。

## Consequences

正面：

- **DB 轻**：业务表都是结构化小行，查询快
- **存储后端可替换**：未来想用对象存储，换实现 + 数据 cp 即可
- **运维灵活**：本地目录方便 v1 单机部署；对象存储方便规模化
- **路径语义可读**：`tasks/42/log.log.gz` 比 UUID blob key 直观

负面 / 待跟进：

- 多了一层抽象，开发者要记得"日志路径"不是真正的 OS path
- 需要 GC：定期清理过期 blob
- 备份策略要把 DB + BlobStore 两边一起备份

## Alternatives Considered

### A. 全塞 DB（TEXT / BLOB 字段）

- Pro: 备份只有一处
- Con: DB 膨胀；查询慢；难独立扩展

### B. 直接写 OS 路径（不抽象）

- Pro: 简单
- Con: 未来切对象存储要改业务代码；不一致风险高

### C. 一开始就上 S3

- Pro: 直奔最终态
- Con: v1 个人 / 小规模场景，引入 S3 凭据管理、网络依赖、成本不必要

## 不走 BlobStore 的内容（参考）

| 内容 | 存储 |
|---|---|
| Project 元信息 / Task / Issue / Comment 文本 | DB 字段 |
| Supervisor memory 单条 | DB 字段 |
| Events 表条目 | append-only 关系表 |
| 项目 CLAUDE.md / AGENTS.md | 在项目仓库 |
| `worker-agent.md` / `supervisor.md` skill | 跟随 binary embed |
