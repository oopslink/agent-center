# 测试计划 — Plan 单调生命周期、Remediation Stage 与 AssignmentPool

## 范围

- 被测对象：ProjectManager Plan/Task/Stage domain、SQLite repositories/migrations、orchestration graph compiler/driver、admin/API/CLI/agent tools、Web Work Board/Plan detail、reconciler 与真实部署 binary。
- 目标：验证 ADR-0055 的单调历史、reject 后动态追加任意增量 Stage、Plan completion gate，以及独立 AssignmentPool 的随时领取与低优先级自动分配。
- 不在范围：引入外部 LLM SDK、替换 orchestration engine、跨 Project 的全局任务市场。

## 前置条件

- 独立 worktree 和临时 SQLite/config/ports；不复用开发数据库。
- 可控 clock、ID generator、proposal source、dispatcher、transaction failure point。
- deployed smoke 必须构建并启动当前源码的真实 center/worker/web binary，不走 in-process shortcut。

## 用例矩阵

| # | 层级 | 目标 | 预期 |
|---|---|---|---|
| L1 | domain | Plan 合法状态边 | 仅 `pending→running↔paused→done/discarded`；terminal 永久 |
| L2 | domain | Task terminal 单调 | completed/discarded 的 Start/SetStatus/Reopen 全拒绝 |
| L3 | domain | Stage projection | dispatched 后不可编辑；members terminal 后 awaiting_verdict；Verdict 后永久 accepted/rejected |
| L4 | domain | one Gate/one Verdict | 相同 idempotency 重放返回同一 Verdict；不同第二次裁决冲突 |
| L5 | compiler | 任意 remediation topology | 单节点、线性、并行/汇合均可；cycle/坏 ref/空 gate/跨 Project 拒绝 |
| L6 | compiler | history/boundary invariant | old Task/Stage/Node byte-for-byte 不变；仅 continuation boundary 改边 |
| L7 | domain | cumulative acceptance | 新 gate contract 包含 base contract、reject evidence 与新 exit criteria |
| L8 | domain | generation/budget | 跨代递减；耗尽进入显式 blocker；extend 有权限和 audit |
| P1 | property | 随机 DAG 增量插入 | 100+ seed 均保持无环、历史不变、每 reject 最多一代 |
| C1 | concurrency | Verdict replay | 并发相同 request 只落一行、只创建一个 Continuation |
| C2 | concurrency | proposal append CAS | 并发 append 仅一个成功；失败者 stale，无部分 topology |
| C3 | concurrency | settle vs reject | open Continuation 时 settle 不得成功；CAS 无丢更新 |
| C4 | concurrency | pause vs proposal | paused 时可保存 proposal，不能 commit/dispatch；resume 恢复一次 |
| R1 | recovery | commit 后响应丢失 | idempotency lookup 返回已创建 Stage/Task，不重复插入 |
| R2 | recovery | outbox/restart | 重启 reconciler 补齐未 dispatch ready node，事件只投递一次 |
| I1 | integration | reject 主路径 | base Stage reject → 新 Stage dispatch；旧 Task 保持 completed、旧 Stage rejected |
| I2 | integration | 第二代 remediation | 新 Gate 再 reject → generation+1，不 reopen/克隆固定旧 Stage |
| I3 | integration | pass 主路径 | remediation Gate pass → Continuation closed → 原 downstream 才释放 |
| I4 | integration | paused plan | pause 后无新 dispatch；resume 后从同一 frontier 继续 |
| A1 | auth | gate verdict 权限 | 仅 evaluator；owner override 需 reason/message/audit |
| A2 | auth | append/control 权限 | planner/creator/owner 可用；普通 member 与跨 Project reference 不泄露 |
| S1 | security | prompt/data boundary | malicious Finding/evidence 只能成为 data，不能扩权或越界 topology |
| M1 | migration | legacy status | draft/running/done 正确 backfill；ambiguous archived fail closed |
| M2 | migration | reopen history | 可证边界被 split；不可证记录进入 resolution report 且 cutover 非零 |
| M3 | migration | builtin extraction | Task identity、claimable/held/assignee/auto-assign/holding cap 对账一致 |
| M4 | migration | round trip | 每个新 migration up/down/up 独立通过，schema version 精确 |
| AP1 | pool/domain | singleton/membership | 每 Project 一个 flat Pool；重复 add 幂等或 typed conflict |
| AP2 | pool/concurrency | claim CAS | 多 agent 竞争仅一个获胜；Task 保持 open + assigned |
| AP3 | pool/service | 随时领取 | 有 structured Plan ready work 时显式 claim 仍成功（holding cap 内） |
| AP4 | pool/scheduler | background priority | auto-assign 先 structured Plan，再 Pool；无 Plan work 时分配 Pool |
| AP5 | pool/recovery | release/expiry | holder/owner 可 release；过期原子回 ownerless 并有 audit |
| AP6 | pool/lifecycle | terminal safety | completed/discarded Pool Task 永不重新 claim，不通过 reopen 回池 |
| UI1 | web | Plan controls/read model | 无 draft/reopen；Start/Pause/Resume/Discard；ledger/frontier/continuation 可区分 |
| UI2 | web | Assignment Pool | 显示 Background/pull anytime、claimable/held；claim/release 反馈正确 |
| D1 | deployed | 真 binary reject e2e | HTTP/tool 完成 gate=reject 并带 proposal，查询到新 Stage，旧实体不变 |
| D2 | deployed | 真 binary restart | proposal commit 后重启实例，reconciler 恢复推进且不重复 append |

## 关键断言

### Reject 前后快照

测试在 verdict 前保存 base Plan 的旧 Task、Stage、Node、dispatch 与 edge 快照。append 后逐字段比较：除 gate 新增 Verdict 投影外，旧实体的 status/version/topology 均不变化；只允许 continuation boundary 的 outgoing edge 集发生约定变化。新实体必须全部带 `origin_verdict_id`、`continuation_id`、`generation=1`。

### AssignmentPool

claim 使用确定性 barrier 让两个 transaction 同时读到 candidate，再验证 SQLite retry/CAS 后只有一个 holder。显式 claim 用例同时制造一个 structured Plan ready task，证明“低优先级”只影响自动排序，不阻断主动领取。

### 失败注入

- Verdict insert 前后；
- Continuation insert 后；
- proposal compile error；
- topology commit 中间任一步；
- outbox 写入前后；
- dispatch post 成功但 response 丢失；
- pause/settle 与 append 的 CAS 冲突；
- claim membership/Task 双 CAS 冲突。

所有失败必须是全有或全无；重试不产生第二个 Verdict、Stage、Task、edge、claim 或 dispatch record。

## 执行命令与门禁

```text
go test ./internal/projectmanager/... ./internal/admin/api/... ./internal/persistence/...
go test ./...
make test-race
cd web && pnpm test
make smoke
```

另外运行新增的 deployed spec，并把 binary SHA、迁移版本、临时实例日志、API 快照和重启后结果写入测试报告。

## 覆盖率

- 新增/修改生产逻辑 diff line coverage ≥ 90%；
- domain transition、compiler diagnostics、auth、CAS/failure branches 必须直接覆盖；
- 不用 sleep 验证并发，统一使用 channel/barrier/fake clock；
- `make test-race` 必须直接看退出码，按仓库规则 `-race -count=10`。

## 出口标准

- [ ] L1–L8、P1、C1–C4、R1–R2、I1–I4、A1–A2、S1、M1–M4、AP1–AP6、UI1–UI2、D1–D2 全通过；
- [ ] `go test ./...`、`make test-race`、`cd web && pnpm test`、`make smoke` 均 0 failure；
- [ ] migration auditor 对测试 fixture 给出确定性报告，未知历史 fail closed；
- [ ] 真部署实例证明 reject 后新增 Stage、旧 Stage/Task/Verdict 不变，重启后不重复；
- [ ] conventions §15、documentation、testing 自检完成，测试报告归档。
