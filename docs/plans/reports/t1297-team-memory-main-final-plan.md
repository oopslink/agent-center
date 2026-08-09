# 测试计划 — T1297 Team Memory main 最终复验

## 范围

- 精确 fetch 后的 `origin/main`。
- 验证 main 包含 remediation `db8faefb`，并复核构建、race、集成/e2e、deployed-binary 与 Team Memory 真部署证据链。

## 用例与出口

| # | 层 | 用例 | 出口标准 |
|---|---|---|---|
| 1 | Git | main 谱系 | `ls-remote main` 精确命中 review SHA |
| 2 | Unit/integration | Team Memory、CLI、Web API、integration/e2e | 全绿 |
| 3 | Race | service、startup reconcile、Web lifecycle | `-race -count=5` 全绿 |
| 4 | Build | Web + Go binary | `make build` 全绿 |
| 5 | Deployed binary | 真实 server/worker pipeline | `make smoke` 全绿 |
| 6 | Feature deployed | proposal 唯一、无请求启动补投、幂等、错误可观测 | 同一 main commit 的独立真部署证据全绿 |

全部通过才提交 PASS。
