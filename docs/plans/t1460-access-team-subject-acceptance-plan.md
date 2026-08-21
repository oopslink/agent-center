# T1460 独立验收计划 — Access / Team Role / Subject access

## 范围

- 被测候选：fresh `origin/main` 的 `00adf6b7b3c6871e3dff118d4af2b85a2c23a1c6`。
- 被测表面：RAM Role CRUD、Team Role CRUD/映射、Subject access direct binding Grant/Revoke，以及 Web HTTP、SPA、admin/MCP 生产入口。
- 安全边界：owner/admin 授权、非管理员 403、跨组织 fail-closed、版本 CAS 409、引用删除保护与改派、审计、授权即时生效。
- 不在范围：S3 的 mockup 像素对照和正式部署签收；该工作由 T1461 执行。本任务仍必须至少运行一次 fresh deployed-binary smoke。

## 前置条件

- 从共享预克隆仓库 fetch 后，以 `origin/main` 建立独立 worktree，不复用开发者工作区或数据库。
- 使用测试临时目录、临时 SQLite、随机组织/实体，禁止访问生产 agent-center 状态存储。
- 验收结论只认候选 SHA 上可复现证据；上游 finding 仅作为风险提示。

## 用例清单

| # | 层 | 目标 | 输入 / 操作 | 预期 |
|---|---|---|---|---|
| A1 | static/schema | RAM Role 领域字段真实持久化 | 检查迁移、表、repository 查询 | `stable_key` 与 `scope` 是独立持久化字段，有唯一性/合法值约束，不与 name 混同 |
| A2 | integration | RAM Role 完整 CRUD 与版本化编辑 | list/search/detail/create/edit(name/key/description/scope/permissions)/delete | 字段 round-trip；编辑生成新版本；旧版本可见；审计完整 |
| A3 | integration | RAM Role CAS 与删除保护 | stale expected version；被 Team Role/direct binding 引用时删除 | stale 返回 409 且无部分写；引用返回 409；改派后可删除 |
| A4 | UI/deployed | RAM Role SPA 生产链 | 浏览器走创建、编辑全部字段、删除双确认 | 真 HTTP 写入；刷新后状态一致；无 mock handler 兜底 |
| T1 | integration | Team Role 完整 CRUD | create/edit/delete functional role，编辑配置与 RAM Role 集合 | 完整 round-trip、版本 CAS 409、审计 |
| T2 | integration/UI | Team Role 删除保护与改派 | 删除仍有成员的 role，指定 replacement 后重试 | 未改派阻断；原子改派后删除；成员来源链即时更新 |
| T3 | deployed/admin | Team Role 多生产入口 | HTTP、SPA、admin/MCP 对同一聚合读写 | 入口一致，均经 AppService，不直读 DB |
| S1 | integration/UI | Subject access Grant/Revoke | direct binding preview/confirm、即时权限查询、revoke preview/confirm | 来源链含 direct binding → RAM Role；grant/revoke 即时生效；审计可追溯 |
| S2 | security | 403 与跨组织 fail-closed | member session、org-B 访问 org-A ids | 写操作 403；读/写不泄漏目标存在性；无跨 org 变更 |
| S3 | concurrency | Direct binding CAS/幂等 | stale preview/token、重复 confirm/revoke | 409/失效错误；重复请求不产生重复 grant/audit |
| R1 | regression | 相关 Go 与 SPA 单测 | authorization/team/webconsole/persistence；Access/Team UI tests | 全绿 |
| R2 | build | 可发布门禁 | `make lint`、`make build` | 全绿 |
| R3 | deployed smoke | fresh binary 真实 HTTP/UI/后台链 | 构建后二进制、unix socket、Playwright deployed spec | 至少一条三页真实生产链全绿 |
| R4 | full regression | 全量 Go 与 SPA suite | `go test ./...`、`pnpm test` | 全绿 |

## 异常路径覆盖

- [ ] RAM Role / Team Role stale CAS 409 且无部分写
- [ ] RAM Role 被 Team Role 与 direct binding 引用时删除阻断
- [ ] Team Role 有成员时删除阻断并要求 replacement
- [ ] 非管理员 403
- [ ] 跨组织实体 id fail-closed
- [ ] preview/confirm token 失效与重复请求幂等
- [ ] DB 写失败/事务回滚由现有单元测试覆盖

## 出口标准

- [ ] A1–R4 每项均 pass；任何需求缺失、skip 或只在 mock 层成立均为 blocking reject。
- [ ] 测试报告按 unit / integration-with-mocks / deployed-binary smoke 分层列数。
- [ ] `make lint` 与 `make build` 全绿。
- [ ] evidence commit 推至远端；结构化 verdict 记录 reviewed SHA 与逐项结果。
- [ ] 只有交付已进入 `origin/main` 后才完成任务；reject 证据同样需先合入 `origin/main`。
