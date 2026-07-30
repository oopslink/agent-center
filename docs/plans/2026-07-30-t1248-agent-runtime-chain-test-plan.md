# 测试计划 — T1248 Agent 单入口纵向运行链

## 范围

- Agent `inherit/profile/override` selection 的持久化与解析。
- `start_task` 冻结 Snapshot，worker admission 后重读 Snapshot。
- Snapshot 驱动 executor CLI/model/parameters；F3 与 flag-off 兼容。
- retry/resume/reassign 复用同一 execution Snapshot。
- Team Role 与 Executor candidate 不在范围。

## 用例清单

| # | 层 | 目标 | 预期 |
|---|---|---|---|
| 1 | unit | selection round-trip / 无效显式引用 | 三态可存；无效 profile fail closed |
| 2 | unit | resolver 优先级与 shadow | Agent selection 被采用；shadow 不写 Snapshot |
| 3 | unit | adapter argv | 已注册参数结构化映射；未知参数 fail closed |
| 4 | integration | task lifecycle | 首次 start 冻结；retry/resume/reassign 字节不变 |
| 5 | integration | worker fork | admission 后重读，WorkItem 取冻结 CLI/model/parameters |
| 6 | regression | flag OFF / F3 | 无 Snapshot 时旧 modelrouter 可执行；`task.model` 仍硬覆盖 |
| 7 | gate | build/vet/test/race | 全部通过，race 无报告 |

## 出口标准

- 上述自动化全部通过。
- 新增运行链测试确实执行（`-v` 可见测试名）。
- `go test ./...`、`go vet ./...`、`make test-race` 通过。
- 特性分支推送到 origin，不合并 main。
