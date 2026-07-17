# I107 / ADR-0054 —— 验收记录（executor 侧自测）

> **本文只记录我**实际跑过**的东西，以及**我没跑成**的东西。
> 按 [`acceptance-methodology.md` § 0](../rules/acceptance-methodology.md)：**by-parity / 读代码 / 共享 predicate 推理都不算验收证据**。
> 下面 § 3 是**尚未完成的验收**，需要 reviewer（agent-center-pd）在有真 agent runtime 的环境里跑。**我没有跑过它，不要把它当已验收。**

## 1. 自测（真跑，直看退出码，未走管道）

| 项 | 结果 |
|---|---|
| `go build ./...` | exit 0 |
| `go vet ./...` | exit 0 |
| `go test ./...`（全量） | exit 0，90 个包 ok |
| `cd web && pnpm test` | exit 0，180 files / 1636 tests passed |

含 migration，故全量跑。版本断言镜像确实散落在别的包里（`internal/conversation/**`、
`internal/cli/handlers_migrate_v1_to_v2.go` 的 `targetSchemaVersion`），已一并 109→110。

## 2. 真实例（隔离，**未碰 prod**）

实例：`~/.agent-center-test/i107/`，用**真二进制** `agent-center server -migrate-only` 跑**真 migrator** 打**真 sqlite 文件**。

| 断言 | 实测 |
|---|---|
| schema 版本 | `SELECT MAX(version) FROM schema_migrations` → **110** |
| 两个部分索引的**实际存储形状** | 从 `sqlite_master` 读回，WHERE 子句确含 `delivered` / `blocked` |
| 新状态能否落库（确认无 CHECK 拦截） | 直插 `delivered` / `blocked` 两行 → OK，读回一致 |
| 索引**是否真被用上**（0111 的意义所在） | `EXPLAIN QUERY PLAN` → 两个查询分别 `SEARCH ... USING INDEX idx_pm_tasks_assignee_running_blocked` / `idx_pm_tasks_assignee_status`。**不是推理它会用，是问了 SQLite** |
| 0111 down 可逆 + parked 行不丢 | 真 DB 上 111 → Down(110) → 再 Up：版本 111，`delivered`/`blocked` 行 **2 → 2 → 2**（索引-only 迁移，无数据损失） |

## 3. ❗ 未完成：稳态验收（park 后不再 fork）

**任务要求的核心判据我没有跑：**

> park 一个 running 任务 → 稳态观察 ≥60s / 多个 recovery tick → 无新 executor
> （**不是单次 kill 成功**——历史上 relaunch 会把它拉回来）

**为什么没跑**：这条需要一个**活的 agent runtime**——真 worker enroll、真 agent CLI、真 LLM、真 fork executor。
本 executor 沙箱里没有这些凭据/网络，**跑不出真 executor**。

**我用什么部分地替代了它（明确不等价，只是最接近的东西）：**

- `TestReconcileOneExecutor_ParkedStatus_NoRecover`——**反复 tick 3 次**（不是单次），断言 `blocked`（含 **reason 为空**的情形）
  与 `delivered` 的 crash 都**不消耗 recovery budget**（= 不 relaunch）。**做过变异验证**：把
  `taskStatusIsSettledOrParked` 里的两个新状态删掉，该测试**确实红**（`blocked_status_no_reason` /
  `delivered_status` 失败，而 `blocked_status_with_reason` 仍绿——正是被 legacy reason 分支接住，
  说明分层是我设计的那样）。
- `TestTaskCancelEvidence_ParkedAndLegacy`——直接钉 should-continue 那个 seam（point-recovery 与
  boot self-reconcile 共用），含大小写、legacy `running`+reason、reassign、以及"健康 running 仍要正常恢复"的反向断言。
- `TestADR54_ParkStopsDispatch`——服务层。**做过变异验证**：同时回退 `EnsureTaskRunnable` 的 park gate
  **和** `plan_view` 的 parked 派生，测试**确实红**；只回退其一不红——即两道防线**真的各自够用**（我在注释里
  声称的 defense-in-depth 是实测的，不是嘴上的）。

**这些都是进程内单测，不是稳态观测。** § 3 的判据仍然**未验收**。

## 4. 顺带发现并修掉的东西（都不是我引入的，是这次改动照出来的）

1. **`activeTaskStatuses` 的"钉子"根本不存在**——`internal/observability/query/service.go` 注释写着
   "Pinned to IsTerminal() by a partition test (TestActiveTaskStatuses_MatchesIsTerminal)"，
   **全仓库 grep 不到这个测试**。所以那个列表实际上**没有任何保护**：加两个非终态status 什么都不红。
   已补上真测试 + 引入 `pm.AllTaskStatuses()` 作单一来源（`TestTaskStatus_IsTerminal_Partition` 原本也是
   手抄列表，加了状态照样绿——一并改成遍历单一来源）。
   **注释断言有守卫 ≠ 有守卫。**
2. **Alerts rail 会对 blocked 任务集体失明**——`handlers_attention.go` 只查 `status=running`。
3. **overdue-block 提醒会对 blocked 任务集体失明**——`listBlockedWithSince` 只列 `running`。
4. **`on_event=blocked` 提醒会永不触发**——`taskEvent` 只在 `running`+reason 时返回 blocked。
5. **agent 负载会少算 parked 任务**（`CountActiveByAssignee` / `ListActiveByAssignee`）。
6. `web` 里 `bg-status-rose-solid` **这个 token 不存在**（只有 rose-bg/fg/border）——一开始我用了它；
   未 build 报错，会**静默渲染成透明**。核了 `index.css` 后换成确实存在的 `orange-solid`。

2–5 是同一个形状：**「blocked 任务」在各处是用 `status=running && blocked_reason!=''` 拼出来的**，
一旦 block 真的改 status，这些地方全部静默失明。都已改成列 `{blocked, running}` 再按 reason 过滤，
两种形状（新的 + 未迁移的 legacy）走同一个循环。

## 5. 兼容性

存量 ADR-0046 形状的行（`status=running` + `blocked_reason`）**不迁移**。理由与做法见
[ADR-0054 § 2.2 / § 4](../design/decisions/0054-task-delivered-blocked-parked-states.md)。
钉子：`TestADR54_LegacyRunningPlusReasonStillUnblocks`、`taskCancelEvidence` 的 legacy 分支测试。
