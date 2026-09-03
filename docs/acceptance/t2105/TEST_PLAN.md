# 测试计划 — T2105 Collaboration Insight 新 main 真实链路验收

## 范围

- 被测对象：`origin/main@7cf8beae46c7072e77a5e7871fcccd60905d3f01`。
- 覆盖：真实 ProjectManager AppService dependency release、Observability audit mirror、Collaboration Effect projection/query/evidence、Web 展示，以及确定性重放。
- 不在范围：外部身份提供商和公网部署；本验收使用隔离的本地真实 SQLite、构建产物与部署 smoke。

## 前置条件

- 从共享预克隆仓库 fetch 后，以精确 `origin/main` 建立隔离 worktree。
- 测试数据：Project P、Plan、任务 A（upstream）和 B（dependent），调用生产 `AddPlanDependency(B, A)`。
- 原始命令日志归档至 `docs/acceptance/t2105/logs/`。

## 用例清单

| # | 类型 | 目标 | 输入 | 预期 |
|---|---|---|---|---|
| 1 | integration | 验证依赖方向和真实释放链路 | B depends on A，完成 A | B 完成前不 dispatch，完成后恰好 dispatch 一次；产生 B 的 `dependency_release` effect |
| 2 | integration | 验证 Query DTO、分页和 Evidence | 已投影 effect + source event | graph/effects/summary/cursor 一致；Evidence 返回源 event；跨 project fail-closed |
| 3 | replay | 验证幂等与可回放一致性 | 同一事件重复/乱序投影并 shadow rebuild | effect/checkpoint/diagnostic 稳定，无重复或漂移 |
| 4 | web | 验证用户可见页面 | Collaboration Insight API fixture | 图、summary、timeline、URL filters、lazy Evidence 及错误态均正确 |
| 5 | deployed smoke | 验证真实构建产物运行 | fresh binaries + unix socket + subprocess path | 部署 smoke 全绿，deployed-binary smoke 计数不为 0 |
| 6 | build | 验证 main 可构建 | `make build` | Go binary 与 Web `tsc -b && vite build` 全绿 |

## 异常路径覆盖

- [ ] invalid query / invalid cursor 被稳定拒绝。
- [ ] Evidence 跨 project 查询返回 not found。
- [ ] Web empty / forbidden / 5xx 状态可见。
- [ ] 重复与乱序事件不造成重复 effect。

## 出口标准

- [ ] 用例 1–6 全部 pass，或明确记录 blocking failure。
- [ ] dependency release 相关并发测试以 `-race -count=10` 通过。
- [ ] Web targeted suite 与完整构建通过。
- [ ] 报告按 unit / integration-mocked / deployed-binary smoke 分层。
- [ ] 验收证据提交并推送；仅在证据进入 `origin/main` 后完成 T2105。
