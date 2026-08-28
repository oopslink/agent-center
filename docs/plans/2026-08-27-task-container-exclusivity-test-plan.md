# 测试计划 — Task 工作容器互斥快修

## 范围

- 被测对象：ProjectManager Plan / Backlog / AssignmentPool 领域服务、Web Console API 与 Work Board。
- 冻结不变量：任一非终态 Task 同时只属于 Plan、Backlog、AssignmentPool 之一。
- 不在范围：改变 Task 生命周期状态机、Plan DAG 调度策略或 AssignmentPool 匹配排序。

## 用例清单

| # | 层 | 目标 | 预期 |
|---|---|---|---|
| 1 | migration | 清理升级前 `plan_id != ''` 且仍有 Pool membership 的重复行 | 仅删除重复行，合法 Pool 行保留 |
| 2 | domain/service | Pool/Backlog Task 加入 pending Plan | 同事务清除 Pool membership，Backlog/Pool 均不可见 |
| 3 | domain/service | pending、running Plan 的 ready、future/blocked、running/blocked 历史节点 | Plan 生命周期内不投影到 Backlog/Pool |
| 4 | domain/service | 历史重复 Pool row 的查询、claim、auto-assign 权威门 | 查询排除且不可 claim/auto-assign |
| 5 | domain/service | 从 pending Plan 显式移除 | 只回 Backlog，不隐式进入 Pool |
| 6 | domain/service | Backlog Task 后续独立 dispatch 到 Pool | 从 Backlog 消失，在 Pool 恰好出现一次 |
| 7 | API | 真实 HTTP Backlog/Pool 查询及 Pool dispatch | 与领域权威归属一致，同 Task 不重复 |
| 8 | frontend | 三份查询缓存短暂不一致 | 按 Plan > Pool > Backlog 去重，future/ready/running/blocked 均由 Plan 占有 |
| 9 | regression | Go / Web 全量、lint、build | 全绿，无类型或构建回归 |
| 10 | deployed smoke | 构建真实 binary 并驱动 Web Console HTTP 生产路由 | 归属读写链与测试链一致 |

## 出口标准

- 用例 1–10 全部通过。
- `go test ./...`、`pnpm test --run`、`make lint`、`make build` 全绿。
- 推送后的 `origin/main` 精确指向交付 SHA，且交付 SHA 为基线 `origin/main` 的后代。
