# Plan 单调生命周期、增量 Remediation 与 AssignmentPool 实施设计

> Status: Accepted for implementation
> Date: 2026-07-30
> Decision: [ADR-0055](../decisions/0055-monotonic-plan-lifecycle-and-remediation-stages.md)

## 1. 目标与范围

本设计把 ADR-0055 映射到当前 ProjectManager / orchestration / admin API / Web 架构，完成三个相互依赖的切换：

1. Plan 采用 `pending → running ↔ paused → done/discarded` 的单调生命周期，archive 成为正交标记；
2. Stage Gate reject 记录不可变 Verdict，并在当前 DAG 的 continuation boundary 追加新的 Remediation Stage，不再 reopen 已完成 Task / Stage；
3. AssignmentPool 保留“低优先级、随时可领取”的产品能力，但从 builtin always-running Plan 中抽离。

本次不引入新的 LLM SDK。生成 remediation proposal 的主体仍是现有 `supervisor_inline` gate evaluator：它读取验收条件、提交证据和 reject 结论，通过 typed tool payload 返回下一代 topology。人工 evaluator 也可在 gate reject 后通过同一命令补交 proposal。

## 2. 统一语言与不变量

| 术语 | 定义 | 核心不变量 |
|---|---|---|
| Plan | 一次可演化、可审计的执行记录 | 状态只按合法边迁移；`done/discarded` 永久终态 |
| Stage | 某一代提交给 Gate 验收的不可变工作集合 | 首次 dispatch 后 topology 不可修改；终态不可回退 |
| Gate | Stage 的一次验收点 | 一个 Gate 最多一个 GateVerdict |
| GateVerdict | evaluator 对特定 Gate 的不可变 pass/reject 事实 | append-only；重放同一 idempotency key 返回同一结果 |
| Continuation | 从某次 reject 到后续 accepted/pruned 的逻辑义务 | Plan 完成前所有 continuation 必须 closed |
| Remediation Stage | 基于 reject 信息新建的一代增量 Stage | `origin_verdict_id`、`continuation_id`、`generation` 必须完整 |
| Continuation boundary | 可安全插入新 Stage 的未 dispatch 下游 frontier | append 只能改这条边界，不能修改已 dispatch/terminal Node |
| AssignmentPool | 每 Project 一个、低优先级、可 pull-claim 的平面任务池 | 不属于 Plan；显式 claim 不受 background 排序阻断 |

Task 状态收敛为 `open → running → completed/discarded`，blocked/lease 是正交执行信息。任何 terminal → nonterminal 写入口均删除；后续工作只能创建新 Task，并以 `follows_task_id` / `origin_verdict_id` 保留血缘。

## 3. 目标状态机

```text
Plan:
  pending ──start──> running ──pause──> paused
     │                  ▲                 │
     │                  └────resume───────┘
     └──────────────discard──────────────> discarded
                        running/paused ──settle──> done

Stage projection:
  pending → running → awaiting_verdict → accepted | rejected

Gate reject:
  GateVerdict(reject)
      → Continuation(awaiting_remediation)
      → RemediationProposal
      → append Remediation Stage(generation+1)
      → Continuation(executing)
      → next GateVerdict(pass/reject)
```

`paused` 只关闭新 dispatch。它不改变 Task/Stage/GateVerdict，不把 Plan 变回可自由编辑状态；proposal 可以生成并持久化，但 paused 时不得 commit topology 或 dispatch。

## 4. 持久化模型与迁移单元

迁移按 expand → backfill/cutover → contract 分离，每个 migration 可独立 round-trip。

### 4.1 AssignmentPool expand

新增：

```text
pm_assignment_pools(
  id PK, project_id UNIQUE, scheduling_class='background',
  auto_assign_enabled, holding_cap, created_at, updated_at, version
)

pm_assignment_pool_tasks(
  pool_id, task_id UNIQUE, priority, added_by, added_at,
  claimed_by, claimed_at, claim_expires_at, version,
  PK(pool_id, task_id)
)
```

Task identity 不变。backfill 将 builtin Plan 中 Task 写入 membership；ownerless `open` 为 claimable，已 assignee 的 `open` 为 held，`running` 继续使用既有 execution lease。兼容 reader 可解析旧 pool plan id，但 cutover 后不再写 builtin Plan/graph/dispatch record。

### 4.2 Plan lifecycle expand/cutover

`pm_plans` 新增 `archived_at`、`archived_by`，状态约束切为 `pending|running|paused|done|discarded`。映射：

- `draft → pending`
- `running → running`
- `done → done`
- `archived`：只在有完成证据时迁为 `done + archived_at`；其余由 auditor 输出人工选择，cutover fail closed

先抽离 builtin rows，再启用全称状态约束。archive 只写 marker，不再级联改变 Task lifecycle。

### 4.3 Remediation expand

新增：

```text
pm_gate_verdicts(
  id PK, project_id, plan_id, stage_id, gate_task_id UNIQUE,
  outcome CHECK(pass|reject), evidence, reviewed_sha,
  actor_ref, idempotency_key UNIQUE, created_at
)

pm_plan_continuations(
  id PK, project_id, plan_id, root_stage_id, current_stage_id,
  trigger_verdict_id UNIQUE, status, generation,
  remaining_budget, boundary_fingerprint,
  pending_proposal_id, closed_by_verdict_id,
  created_at, updated_at, version
)

pm_remediation_proposals(
  id PK, project_id, plan_id, continuation_id,
  trigger_verdict_id, idempotency_key UNIQUE,
  based_on_plan_version, boundary_fingerprint,
  payload_json, status, diagnostics_json,
  created_by, created_at, committed_at
)

pm_plan_topology_outbox(
  id PK, project_id, plan_id, proposal_id UNIQUE,
  event_type, payload_json, status, attempts,
  created_at, delivered_at
)
```

`pm_stages` 新增 `origin_verdict_id`、`continuation_id`、`generation`、`acceptance_contract`、`topology_fingerprint`。这些字段对 legacy base Stage 使用空 origin、generation=0。

### 4.4 Migration auditor

只读 auditor 在 cutover 前输出：legacy archived plan、当前或历史 terminal→nonterminal Task、活动 reopen round、latest-wins verdict、builtin pool 对账。存在无法证明的 Plan/Task 时返回非零退出码，并生成逐行 resolution report；不猜测历史。

## 5. 命令契约

### 5.1 Plan lifecycle

- `StartPlan(project_id, plan_id, actor)`：仅 pending；首次编译 base DAG。
- `PausePlan(...)`：仅 running；关闭自动 proposal commit 与 dispatch。
- `ResumePlan(...)`：仅 paused；先 reconcile proposal/outbox，再推进 ready frontier。
- `SettlePlan(...)`：仅 running/paused；要求有效路径 terminal 且 continuation 全 closed。
- `DiscardPlan(..., reason)`：pending/running/paused → discarded；停止新 dispatch，活动 work 走既有 cancel port。
- `ArchivePlan(...)`：仅 terminal；写 archive marker，可幂等读取但重复写返回 typed conflict。

所有命令显式携带 project_id，并验证资源同属该 Project。Pause/Resume/Discard/预算扩展只允许 Plan creator 或 Project owner。

### 5.2 RecordGateVerdict

输入：

```json
{
  "project_id": "project-1",
  "plan_id": "plan-1",
  "stage_id": "stage-1",
  "gate_task_id": "task-gate-1",
  "outcome": "reject",
  "evidence": "integration test fails on ...",
  "reviewed_sha": "abc123",
  "idempotency_key": "gate-1:abc123:reject",
  "remediation": { "...": "optional proposal" }
}
```

同一事务写 terminal gate Task、append-only GateVerdict、Stage projection所需事实和 Continuation。pass 关闭当前 continuation 并释放既有 downstream；reject 不修改任何旧 Task/Stage，创建或推进 Continuation。不同 payload 重用 key、或同一 gate 第二次裁决，返回 conflict。

### 5.3 RemediationProposal

```json
{
  "proposal_id": "proposal-1",
  "idempotency_key": "verdict-1:g1",
  "based_on_plan_version": 7,
  "boundary_fingerprint": "sha256:...",
  "name": "修复验收失败",
  "rationale": "根据 reject evidence ...",
  "tasks": [
    {
      "ref": "fix",
      "title": "修补失败路径",
      "description": "...",
      "assignee_ref": "agent:a",
      "dispatch_mode": "normal",
      "follows_task_id": "task-old"
    }
  ],
  "edges": [{"from": "fix", "to": "verify"}],
  "gate": {
    "assignee_ref": "user:pd",
    "acceptance_contract": "原 contract + reject evidence + remediation exit criteria"
  }
}
```

compiler 为纯逻辑：先解析 snapshot，再验证 ref、assignee、DAG、Stage coverage、累积验收契约、generation/budget、based-on version 和 boundary fingerprint。输出 canonical topology diff 或有序 diagnostics，不产生副作用。

commit 在单事务中：

1. CAS Plan version、Continuation version/boundary；
2. 创建全新 Task、Stage、Gate evaluator 与 graph Node；
3. 把 continuation boundary 中原本指向 downstream roots 的边替换为 `old gate → remediation entries → remediation gate → downstream roots`；
4. 不修改旧 Node/Task/Stage 的属性或状态；
5. 更新 Continuation、Plan topology version，写 audit/outbox；
6. running 时推进 dispatch，paused 时保留待 reconcile。

允许的新 Stage topology 与原 Stage 无关：可以是单 Task、线性链、并行 fan-out/fan-in 或其它合法 DAG。每个 reject 最多成功 append 一代；过期 proposal 标记 stale 并要求重新生成。

## 6. AssignmentPool 行为契约

每个 Project 恰有一个 AssignmentPool。claimable predicate：

```text
membership active
AND task.status = open
AND task.assignee_ref = ''
AND task.archived_at = ''
AND project active
```

`ClaimPoolTask` 在一个事务内验证 membership、Project member agent、holding cap，并以 membership version + Task ownerless/open 执行 CAS。成功后 Task 仍为 `open`，设置 assignee 与 claim reservation；agent 通过显式 `StartTask` 进入 running。

- 显式 claim：随时可领取，只受权限、状态、holding cap 与 CAS 限制，不因 Plan 有 ready work 而被禁止；
- 自动分配：只选空闲 agent，structured Plan ready work 优先于 scheduling class=`background` 的 Pool；
- release/expiry：只允许 holder 或 owner，恢复 ownerless open，写 audit；
- running lease reclaim：若 Task 仍是有效 pool member，则回 ownerless open；绝不 reopen terminal Task；
- Pool membership removal：只对未运行 Task；已 terminal Task 可保留历史 membership marker但不可再次 claim。

## 7. Read model 与界面

Plan detail 统一返回：

- `status`、`archived_at/by`、`topology_version`；
- immutable stage ledger：generation、origin verdict、accepted/rejected；
- current frontier：ready/running/blocked；
- open continuations：status、remaining budget、proposal diagnostics；
- latest topology diff 与有界 evidence reference。

Web/CLI 不再显示 draft/stop/reopen/builtin Plan。动作改为 Start、Pause、Resume、Discard、Archive；Stage reject 后显示“等待增量方案 / 正在执行第 N 代修补”，并保留旧 Stage 的只读证据。Work Board 保留 Assignment Pool 区域，明确 `Background · pull anytime`，展示 claimable/held/expired 状态。

## 8. 失败、恢复与并发

- Verdict 与 Continuation 创建原子；proposal/commit 可分事务，由 durable outbox + reconciler 补偿；
- 进程在 commit 后丢响应：按 idempotency key 查询已提交 proposal，返回原结果；
- Plan version 或 boundary stale：不做部分写，proposal=`stale`；
- paused 时 proposal 已返回：持久化为 ready，不 commit、不 dispatch；Resume 时 reconcile；
- Settle 与 reject/append 竞争：两者 CAS 同一 Plan/Continuation version；只有 continuation 全 closed 的 settle 可赢；
- budget 耗尽：Plan 仍 running + blocker=`remediation_budget_exhausted`，等待 owner extend/discard；
- proposal invalid/timeout：Plan 仍 running + blocker=`awaiting_remediation`，可重试或人工提交。

## 9. 安全边界

Gate evidence、Finding、conversation summary 一律作为 untrusted data 传给 planner，不能覆盖 system policy、权限、budget 或 acceptance contract。proposal 只能表达白名单 topology 字段，不接受任意 SQL、tool instruction、workspace path 或跨 Project reference。所有 assignee/reference 在 commit 时重新授权和解析。

## 10. 兼容与删除顺序

兼容期保留旧 HTTP/tool 名的 adapter：`stop_plan → pause_plan`，但 wire status 立即返回 `paused`，不再返回 draft。旧 `reopen_task`、`reopen_exhausted_stage`、loopback reject 写入口返回明确的 retired error；read path 可展示 legacy evidence。

删除顺序：新 writer/read model 全量启用 → backfill/auditor 全绿 → deployed smoke 与重启 reconcile 通过 → 删除 builtin Plan special-case → 删除 Task.Reopen/Stage reopen writer → 最后移除 legacy tables/API/schema residue。

## 11. 可观测性

实现 ADR-0055 事件集，并新增指标：

- `pm_plan_open_continuations{status}`
- `pm_remediation_proposals_total{result}`
- `pm_remediation_append_conflicts_total{reason}`
- `pm_assignment_pool_tasks{state}`
- `pm_assignment_pool_claim_total{result}`
- `pm_plan_dispatch_blocked{reason}`

所有 reject、proposal diagnostics、budget 变更、claim expiry 都写 audit；`reason` 与用户可读 `message` 成对出现。

## 12. 实施切片

1. migration auditor + legacy fixtures；
2. AssignmentPool schema/repo/service/backfill + compatibility reader；
3. Plan lifecycle/archive marker + API adapter；
4. GateVerdict/Continuation/domain repo；
5. proposal compiler + atomic topology append + reconciler；
6. gate evaluator tool payload 与 human append command；
7. Task/Stage reopen writer contract removal；
8. CLI/Web/read model；
9. deployed-binary e2e、migration report、legacy cleanup。
