# 0055. Plan 单调生命周期与增量 Remediation Stage

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-07-29 |
| Accepted | 2026-07-30 |
| Amends | ADR-0046 的 Task `reopened` 决定（该决定无独立 ADR 文件） |
| Supersedes on acceptance | `v2.9-plan-orchestration.md` 的 `draft ↔ running`；`2026-07-02-orchestration-engine-design.md` 与 `2026-07-03-plan-stage-model-design.md` 的 reopen/loopback 返工语义；`2026-07-05-plan-live-topology-edit.md` 的“撤已跑的活走 reopen”语义；ADR-0049 中 builtin assignment pool 作为 always-running Plan 的存储建模（其 claimable pool 用户能力继续保留） |

## Context

当前 Plan 推进把三个不同概念压进了同一组可逆状态：

1. `Plan.running → Plan.draft` 同时表示“停止派发”和“重新进入可编辑模式”。这让一个已经产生执行历史的 Plan 看起来像尚未开始的草稿，也让 pause 与 topology authoring 没有领域边界。
2. `Task.completed → Task.reopened` 同时承载人工重开、Decision loopback、Stage Gate reject 后返工。Task 的完成事实因此可以被撤销；Plan 下游已经消费的完成事实却不一定同步回退。
3. Stage Gate reject 被解释成“重新执行原 Stage”。但 reject 提供的是新增信息：缺陷、未满足的验收条件、证据和修补建议。正确的下一步通常是基于这些信息生成一个增量处理流程；它可能与原 Stage 相似，也可能只有一个修补 Task，或成为完全不同的子 DAG。

由此出现两个根本矛盾：

- **历史事实不单调**：已经完成的 Task / Stage 被原地改回未完成，审计、进度和依赖满足关系都会倒退。
- **控制流不自适应**：引擎只能 reopen 或克隆旧子图，不能把 reject 信息编译成新的增量 Stage。

Plan 是一条持续演化的执行记录，不是可反复清空重写的草稿。Gate reject 也不是 rollback；它是生成后续工作的领域事实。

## Decision

### 1. Plan 使用单调生命周期

Plan 状态收敛为：

```text
pending → running ↔ paused
pending | running | paused → discarded
          running | paused → done
```

完整迁移：

| From | To | 动作 | 语义 |
|---|---|---|---|
| 创建 | `pending` | CreatePlan | Plan 已存在，但从未派发 |
| `pending` | `running` | StartPlan | 基础 DAG 编译通过，开启推进 |
| `pending` | `discarded` | DiscardPlan | 未开始即放弃 |
| `running` | `paused` | PausePlan | 显式关闭新 dispatch |
| `paused` | `running` | ResumePlan | 恢复 dispatch |
| `running` / `paused` | `done` | SettlePlan | 所有有效路径收敛，且不存在未决 continuation |
| `running` / `paused` | `discarded` | DiscardPlan | 明确放弃剩余 continuation |

`done` 与 `discarded` 是永久终态，不存在离开终态的迁移。

Plan 不再暴露任意 `SetStatus`；只能通过上表的显式领域动作迁移。

状态语义：

- **pending**：Plan 已持久化，可自由编辑基础 topology，但从未 dispatch。它不是“文稿草稿”；尚未提交的文稿属于 UI 本地状态或未来独立的 `PlanProposal`。
- **running**：编排器允许推进。等待 GateVerdict、等待 Remediation Stage 生成、没有 ready node、等待人工裁决都仍是 `running`，由 frontier / blocked-on read model 解释原因。
- **paused**：用户或 operator 显式设置的 automatic-progression latch。它不撤销已发生的 Task / Stage / GateVerdict；允许在途 execution 回写事实、允许已有事实自然收敛到 `done`，但阻止新的 dispatch、自动 remediation proposal 生成和自动 topology commit。
- **done**：所有有效分支均已收敛，没有未决 GateVerdict、Remediation Stage 生成、人工裁决或未完成 frontier。
- **discarded**：Plan 被明确放弃；未完成 continuation 不再执行。

`paused` 只能由显式动作进入，不能由“当前无 ready node”推导。Gate reject 也不自动 pause Plan。Plan 暂停期间收到 reject 时只持久化 Verdict 与待处理 Continuation；Resume 后 reconciler 再恢复自动生成。用户可在 paused 下显式提交 topology edit，但该动作必须逐次授权，不能把 paused 等价为后台仍会自动改图。

`DiscardPlan` 从 `pending` 可立即完成；从 `running` 发起时必须先关闭新 dispatch，并等待 active execution 终止后才能写入 `discarded`。若终止需要异步跨 BC 协调，使用 Plan 聚合内持久化的 `PlanDiscardRequest` operation Entity 表达操作进度，Plan 在此期间保持显式 `paused`；Resume 必须先取消该 request。失败必须上报，不能增加一个对用户不可见的 Plan lifecycle state。

### 2. Archive 与 lifecycle 正交

Archive 是只读展示 / 保留策略，不是 Plan 的业务推进状态。Plan 归档后仍保留其 lifecycle terminal truth（`done` 或 `discarded`）；归档通过正交的 `archived_at` / `archived_by` 表达。

归档前 Plan 必须已经处于 `done` 或 `discarded`。Archive 不改变 lifecycle status，Unarchive 若未来支持也不改变 lifecycle status。

### 3. Task 与已执行 Stage 的历史永久不可变

- Task 的 `completed` / `discarded` 是永久终态；删除 `TaskReopened` 以及所有 `completed → reopened/open/running` 路径。
- Task 不再暴露可绕过不变量的自由 `SetStatus`；每条合法迁移由具名领域动作表达。
- 用户发现已完成 Task 仍需补充工作时，创建新的 follow-up Task，并记录其来源关系；不重开旧 Task。
- 一次 executor 失败后的 retry 属于 execution / attempt 层；它不能通过改写已完成 Task 表达。
- Stage 一旦任一成员被 dispatch，其成员集合、内部 topology、acceptance contract 与已产生结果即冻结。Stage 内已经完成的 Task 永不被 Gate reject 改写。

Plan topology 可以继续扩展，但扩展不得修改已执行 Stage 的内部历史。

Stage immutability 覆盖成员、内部 Edge、Task 结果与 GateVerdict；Stage 之外、通往尚未 dispatch downstream roots 的 continuation Edge 属于 Plan topology，可以通过带版本的 topology commit 被 supersede，旧版本仍保留在 audit / revision 中。

Stage 状态是投影，不提供任意 `SetStatus`：

```text
pending → running → awaiting_verdict → accepted
                                  └──→ rejected
```

- 有 Gate 的 Stage 在所有 required member Task completed（或分支被明确 prune）后进入 `awaiting_verdict`；Task 仅仅 terminal 不等于 Stage 完成。
- `accepted` / `rejected` 都是 Stage 的永久终态。Reject 之后推进的是同一 PlanContinuation 上的新 Stage，不是旧 Stage 回到 running。
- 无 Gate 的纯 barrier Stage 在全部 required member Task completed（或分支被明确 prune）后直接投影为 `accepted`。
- 现有 `StageReopen`、`GateReopened` 与 `StageGateReopenRequest` 退役。
- 现有 per-Stage `maxRounds` / `DefaultStageMaxRounds` 退役；重试上限统一迁移到 Continuation 的跨代 RemediationPolicy。

下文的 Stage `settled` 是集合谓词，表示 Stage 已进入 `accepted` 或 `rejected`；它不是第三个持久化状态。

### 4. GateVerdict 是不可变事实

每个 Stage Gate 恰好产生一条不可变的 terminal `GateVerdict`，至少包含：

- verdict identity；
- source gate / source stage；
- outcome（pass / reject）；
- 结构化 findings / 未满足的 acceptance criteria；
- evidence references；
- actor 与 occurred_at。

`GateVerdict` 是 Plan 聚合内的 immutable Entity；不提供 Update。删除仅随 Plan cascade 发生。

- `pass`：释放既有 continuation。
- `reject`：保持原 Stage 与 Verdict 不变，触发 Remediation Stage 生成。

数据库以 `gate_id` 唯一约束保证 exactly-once；重复 command 通过 idempotency key 返回已有 Verdict。已经裁决的 Gate 不允许“重新裁决”。若输入错误或需要追加审查，必须在同一 Continuation 上创建新的 review/remediation Stage 与新 Gate；旧 Verdict 的事实与控制流效果均不撤销。

### 5. PlanContinuation 是动态插入的稳定锚点

`frontier` 是随执行不断变化的 read model，不能作为持久化写入目标。Plan 聚合新增 `PlanContinuation` Entity，表达“某个 Gate 后仍需闭合的一项逻辑义务”。基础 DAG compile 时为 gated boundary 建立 Continuation；后续每代 Remediation Stage 都挂在同一 identity 上。

至少记录：

```text
id
plan_id
origin_stage_id
current_gate_id
downstream_root_node_ids
generation
remediation_policy
version
```

Continuation 的协调状态由事实投影为：

```text
awaiting_verdict → awaiting_remediation → awaiting_verdict → ... → closed
                                      └──→ awaiting_approval
```

- Gate pass、条件分支被明确 prune，或 Plan discarded，才会闭合该 Continuation。
- Gate reject 只使 Continuation 等待下一代 remediation，不会把 source Stage 变回 active。
- `downstream_root_node_ids` 定义可改写边界。新增子 DAG 只能插在当前 Gate 与这些尚未 dispatch roots 之间；历史 Stage、其它 Continuation 和已 dispatch Node 只能作为 evidence/reference，不能被新增控制边修改。
- Continuation 更新与 Plan topology 共用 `plan.version` CAS；它是命令锚点，`PlanFrontier` 仍只负责解释“现在能做什么/为何阻塞”。

每个 Continuation 携带跨代 `RemediationPolicy`，至少包括 `mode = automatic | approval_required`、`max_generations` 与 `max_added_tasks`。预算按整个血缘累计，不能因创建新 Stage 而重置。预算耗尽，或 proposal 请求超出 Project capability / risk policy 时，进入 `awaiting_approval` 并产生 human decision；Plan 保持 `running`。有权限的 operator 可带 `reason + message` 扩展预算、批准一次 proposal、显式 waive 某项 finding，或 discard Plan，但不能 reopen 历史 Stage。Waive 会修改下一代 AcceptanceContractSnapshot，并作为独立 audit fact；它不改写旧 Verdict。

### 6. Reject 生成增量 Remediation Stage，而不是 reopen

Remediation Stage 是普通 Stage 的一种来源类型，不是新的 Aggregate。它使用 `StageOrigin` Value Object 记录血缘：

```text
kind = remediation
triggered_by_verdict_id
remediates_stage_id
generation
rationale
```

Remediation Stage 的内部 topology 根据 Reject Verdict、PlanFinding 和当前未推进 frontier 动态生成：

- 可以复用原 Stage 的模板；
- 可以只修补其中一部分；
- 可以增加调查、实现、迁移、验证等新 Task；
- 可以形成与原 Stage 完全不同的并行 / 顺序子 DAG。

引擎不得把“克隆旧 Stage”当作默认领域语义。是否复用结构是 planner 对本次 Verdict 的决定。

Remediation Stage 使用全新的 Task、Node 和 Gate。它自己的 Gate 若再次 reject，则基于新的 Verdict 继续追加下一代 Remediation Stage；不修改任一历史 Stage。

新 Gate 的 `AcceptanceContractSnapshot` 必须是累积的：继承原始 acceptance criteria、携带所有尚未关闭的 findings，并逐项说明本代 topology 如何修补与验证。Planner 可以带理由增加或细化 criteria，但不能静默删除旧 criteria；删除或降级必须经有权限的人显式批准并写 audit event。这样新 Gate 审核的是“原目标 + 增量修补后的整体结果”，而不是只看 patch 是否执行过。

### 7. 生成与提交分为两阶段

LLM / Supervisor 生成 topology 不在数据库事务内执行：

1. Gate reject 在 ProjectManager AppService 内持久化 `GateVerdict` 并 emit 事件。
2. Supervisor 根据 Verdict + PlanFinding + Continuation boundary 生成 `RemediationStageProposal` command payload。
3. ProjectManager AppService 编译并验证 proposal。
4. 通过一次带 `base_version` CAS 的 topology commit 原子插入新 Stage，更新 Continuation 与 `plan.version`，写 audit ledger / outbox。
5. commit 后由既有 auto-advance 路径派发新 Stage 的 ready roots。

同一 `triggered_by_verdict_id` 最多成功 append 一个 Remediation Stage；事件 replay / Supervisor retry 必须幂等。

Proposal envelope 必须携带 `proposal_id`、`idempotency_key`、`triggered_by_verdict_id`、`continuation_id`、`based_on_plan_version` 与 boundary fingerprint。CAS 冲突说明 proposal 的前提已经失效：调用方必须重新读取 snapshot 并重新生成或显式 rebase，禁止对新版本盲重试旧 payload。若 commit 已成功但响应丢失，按 verdict/idempotency lookup 返回已创建的 Stage。

若 proposal 生成或编译失败，Plan 保持 `running`，frontier 显示等待 remediation planning / human decision，并显式升级；不得伪装成 `paused` 或吞错。

动态生成复用既有 Supervisor spawn-agent CLI 路径，不引入 LLM SDK。若暴露 agent-facing proposal / append 能力，必须同时提供 skill 指引与 `agent-center` CLI command，并通过 transport 进入 ProjectManager AppService；不新增 MCP-only 写入口。需要人立即拍板且 Plan 无法继续时使用 InputRequest，不把同步阻塞问题伪装成 Issue 或状态字段。

Verdict、Finding 与 evidence 是不可信数据，不是 Supervisor 指令。Prompt adapter 必须使用结构化/引用边界、长度上限与 secret redaction；大段 evidence 存 BlobStore，只把有界摘要和 reference 放入 context，防止 feedback prompt injection 与上下文无限增长。

GateVerdict + `remediation_requested` outbox 在同一事务写入；Stage/Task/Node/Edge/Continuation/version/audit/outbox 在另一个单事务提交。事件消费失败后，reconciler 必须扫描“reject Verdict 已存在但没有 appended Stage”的开放 Continuation 重新投递；不能依赖进程内回调保证推进。Plan paused 时 reconciler 只报告待处理，不生成或提交 proposal。

### 8. 动态插入只改未执行 frontier

Stage Gate 是严格 barrier：Verdict 产生前，其控制的下游不得 dispatch。因此 reject 时，下游 continuation 仍然可安全编辑。

AppendRemediationStage 的 topology commit：

1. 新增 Remediation Stage、全新 Tasks、Nodes、内部 Edges 与新 Gate；
2. 把新 Stage 接到对应 PlanContinuation 的当前 Gate 后；
3. 把该 Continuation 捕获的、尚未 dispatch 的 downstream roots 改为等待新 Gate pass；
4. 保留历史 topology diff / revision，不删除审计事实；
5. 校验 DAG 无环、引用同 Plan、assignee 可解析、Stage boundary 合法；
6. 事务内重查 Plan status、Continuation version/boundary 与所有被动 frontier node 仍未 dispatch，否则 CAS / mutability conflict，调用方基于新版本重新规划。

动态插入可以改变 Plan 当前有效 topology，但不能：

- 删除或修改已 dispatch / running / terminal Node；
- 改变已执行 Stage 的成员或内部 Edge；
- 撤销旧 GateVerdict；
- 让已完成 Task 回到 active set。

新增 Stage 的 Task 可以读取历史 artifact / finding，但只能通过显式 reference；reference 不形成 DAG 控制依赖，也不授予跨 Project 访问权限。

### 9. Plan 完成条件按 continuation 收敛判断

Plan 只有同时满足以下条件才能进入 `done`：

- 所有有效、未被条件剪枝的 Stage 均已 settled；
- 所有 PlanContinuation 均为 `closed`；Plan discarded 时不再进入 done，而是以 `close_reason=plan_discarded` 终止开放 Continuation；
- 不存在等待生成 / 提交的 Remediation Stage；
- 不存在 pending human decision；
- frontier 为空；
- 没有 active execution。

节点“当前全部 terminal”不足以单独推出 Plan done；一个尚未被处理的 reject Verdict 是未闭合 continuation。

`SettlePlan` 必须在同一串行化/CAS 事务中重查 Plan status/version、active execution、开放 Continuation、pending decision/remediation 与 frontier。并发 reject 或 append 只要先提交，就会使 settle 失败并重算；不能先读“全 terminal”再无条件写 `done`。

### 10. Topology 可变性与 Plan status 分离

| Plan status | Topology 规则 |
|---|---|
| `pending` | 基础 DAG 可自由编辑；Start 时完整 compile |
| `running` | 已执行历史冻结；允许对未 dispatch frontier 做 CAS topology commit，并允许 append Remediation Stage |
| `paused` | 历史冻结；允许显式 operator topology edit；禁止自动 proposal、自动 commit 与新 dispatch；Resume 后先 reconcile 再推进 |
| `done` / `discarded` | topology 完全只读 |

Pause 不恢复 draft 权限。删除 `StopPlan: running → draft`；由 `PausePlan` / `ResumePlan` 取代。

### 11. AssignmentPool 保留低优先级可领取任务池，但不再伪装成 Plan

AssignmentPool 的用户能力必须完整保留：它就是 Project 内“优先级不高、没有进入结构化 Plan、agent 可以随时领取”的共享工作池。问题只在于当前实现用一个 always-running builtin Plan 承载它，迫使 Plan lifecycle 出现“永远 running、不能 done、不是 real plan”等例外。

目标模型将 AssignmentPool 建模为 Project 下的 singleton Entity/collection。它随 Project 创建和删除，不是可独立创建、启动、暂停、完成或归档的 managed aggregate，也不参与 DAG、Gate、Plan 进度或 Plan status。所有真实 Plan 因而遵守同一状态机，同时 Work Board 继续保留 Backlog / Assignment Pool / structured Plans 三段。

AssignmentPool 的行为契约：

| 能力 | 决定 |
|---|---|
| Membership | Pool 是 flat task collection，无 Node/Edge/Gate。没有 active execution 的 Task 可在 Backlog、Pool 与尚未启动的 `pending` Plan 间移动；running Task 与已运行 Plan 的历史边界不可改 |
| Claimable | active membership + Task `open` + unassigned + non-archived 才可领取；不再依赖伪造的 Plan `running` 或 Node `dispatched` |
| Claim | Project member agent 可显式领取；`ClaimPoolTask` 使用 CAS，竞争者只能有一个成功。领取只变为 `open/assigned`，不直接变为 `running` |
| Holding | 保留 per-agent holding cap；领取后的 Task 进入 agent 的 held work，开始执行仍走显式 StartTask 与 single-active 约束 |
| Priority | Pool work 使用 `background` scheduling class。推荐、自动分配和默认排序必须让 structured Plan 的 ready work 优先；显式领取只占一个 holding slot，不因低优先级而被禁止 |
| Auto-assign | 保留 Project 级开关；只在没有更高优先级待分配工作且存在符合 capability/load policy 的空闲 agent 时分配。关闭时继续等待手动 assign/claim |
| Return | claimed-open Task 可显式 release；超过 claim reservation deadline 未开始时自动回到 ownerless pool。running Task 继续使用 execution lease，lease reclaim 后若仍有效则回到 Pool，而不是 reopen 历史 Task |
| Visibility | agent 可读取其有权限 Project 的 claimable pool；UI/CLI 显示 claimable、held、starved 与 background priority，不再显示“builtin / always running” |

“低优先级”是调度排序，不是 Task lifecycle，也不是不可领取门禁。显式领取与自动分配共用同一个原子 claim invariant；前者不要求 agent 当前空闲，后者必须要求空闲并服从 Plan-first 排序。claim reservation 是 ownership metadata，不增加 Task status；超时释放必须产生 audit event，避免 Task 被静默抢回。

Pool 内部仍可按 Task priority + created_at 排序，但 `background` scheduling class 默认不能越过 structured Plan 的 ready work。若某个 Pool Task 变成明确的交付关键路径，应将它提升进 `pending` Plan 或走有审计的定向分派，而不是悄悄改变整个 Pool 的优先级。

AssignmentPool 的写入口仍经 ProjectManager AppService。迁移必须保持 Task identity、claim/holding-cap、optional auto-assign、Work Board 拖拽与 `get_my_work` discovery 行为；只是把 membership/claimability 的单一来源从 builtin Plan/Node 改为 AssignmentPool。该边界调整必须先于移除 builtin status special-case。

### 12. 命令权限按领域动作收紧

“是 Project member”不足以授权改变 Plan 控制流。Application Service 必须在 transaction boundary 前后都校验 actor 与目标资源：

| Command | 最低授权原则 |
|---|---|
| RecordGateVerdict | 配置在 Gate 上的 evaluator / assignee；Project owner 的紧急代裁决必须带 `reason + message` 并产生独立 audit event |
| AppendRemediationStage | 绑定该 Verdict 的 system planner，或 Plan creator / Project owner 的显式操作；普通 member 不得向任意 Plan 注入 topology |
| Pause / Resume / Discard / ExtendBudget | Plan creator 或 Project owner；自动 actor 只能执行 policy 明确允许的动作 |
| ClaimPoolTask / ReleasePoolTask | Project member agent，只能操作所在 Project 的 AssignmentPool；claim 还必须通过 holding cap 与原子 ownership CAS |
| Read Plan / Stage / Verdict / evidence | Project membership + evidence 自身可见性；reference 不继承更高权限 |

所有 command 必须显式携带 `project_id`。涉及 Plan 的命令校验 Plan、Stage、Gate、Continuation 同属该 Project；Pool 命令校验 AssignmentPool membership 与 Task 都属于该 Project。跨 Project ID 混用统一返回 NotFound 或 Forbidden，不从错误差异泄露资源存在性。

## Consequences

### 正面

- Plan、Task、Stage、GateVerdict 的历史单调，审计和依赖满足事实不再倒退。
- Gate reject 真正消费新信息，编排从“机械重跑”升级为“反馈驱动的增量规划”。
- 动态工作只扩展未推进 frontier，不打断正在执行或已经完成的节点。
- `pending`、`paused`、blocked frontier 的语义分离：是否启动、operator 是否暂停、当前为何不能推进不再混成一个状态。
- `done` 永久，完成后的新需求自然进入 follow-up Task / 新 Plan，而不是复活历史 Plan。
- AssignmentPool 的 claimable pull queue、holding cap、optional auto-assign 与 Work Board 入口继续存在，但不再污染 Plan lifecycle。

### 负面与成本

- 当前 Task / Stage / orchestration Node 的 reopen 路径需要整体退役，影响 domain、service、repo 查询、Web UI、agent tools、tests 与文档。
- 动态 proposal 使 Plan 的总工作量运行中增长；简单的 `done_nodes / total_nodes` 百分比不再稳定。UI 应展示 settled history + current frontier + remediation generations，而不是承诺单调百分比。
- Legacy `PlanArchived` 已丢失 archive 前是 draft 还是 done 的直接状态，需要独立 migration audit 决定 backfill；不得在 schema migration 中凭猜测静默映射。
- Discard running Plan 涉及停止新 dispatch 与终止 active execution；跨 BC 协调必须通过既有 AppService port / event，不能直访 Agent 存储。

### 可观测性

至少提供以下 Domain Events；带 `reason` 的事件必须同时带 `message`：

| Event | 语义 |
|---|---|
| `pm.plan.started` | pending → running |
| `pm.plan.paused` | running → paused |
| `pm.plan.resumed` | paused → running |
| `pm.plan.done` | continuation 全部收敛 |
| `pm.plan.discarded` | Plan 被明确放弃 |
| `pm.plan.gate_verdict_recorded` | 不可变 GateVerdict 入账 |
| `pm.plan.remediation_requested` | reject 触发增量规划 |
| `pm.plan.remediation_stage_appended` | 新 Stage 原子插入成功，携带 verdict/stage/generation/topology version |
| `pm.plan.remediation_failed` | proposal 生成 / 编译失败并进入显式等待或升级 |
| `pm.plan.remediation_proposal_stale` | proposal 因 version/boundary 变化被拒绝，等待重新规划 |
| `pm.plan.remediation_budget_exhausted` | Continuation 跨代预算耗尽，等待人工决定 |
| `pm.plan.remediation_budget_extended` | operator 带理由扩展预算 |
| `pm.plan.continuation_closed` | pass / prune 闭合一条逻辑义务 |

Plan inspect 必须同时展示 lifecycle status、archive marker、当前 topology version、未闭合 Verdict、Continuation blocker、Remediation generation / budget 与 frontier。`ps/stats` 的 active Plan 集合为 running + paused，但必须分栏，不能把 paused 当 running dispatch。

动态 DAG 不再承诺单一单调百分比。UI 与 agent read model 至少拆成：

- immutable Stage ledger：每代 `accepted` / `rejected` Stage、Verdict 与 evidence；
- current execution：ready / running / blocked Node；
- open Continuation：当前 Gate、generation、剩余 budget、等待 proposal/approval 的精确原因；
- topology revision：当前版本与最近一次增量 diff。

Plan `running` 且没有 ready Node 时必须给出机器可读 blocker，不能只显示“0 个任务”。Plan paused 与 Node paused 必须使用不同字段和文案。CLI/Web/agent-facing `get plan`、`get stage` 使用同一 ProjectManager read projection；完整 evidence 通过有界 reference 按需读取，不把整个历史塞入默认响应。

### 可测试性

- proposal compiler 是纯逻辑或注入式 Domain Service：输入 topology snapshot + proposal，输出 validated topology commit / diagnostics。
- 时钟、ID、Supervisor proposal source、dispatch port 全部可注入。
- CAS 冲突、Verdict replay、重复 remediation append、proposal failure、并发 settle/dispatch 抢先、进程在 outbox 前后崩溃必须可确定性注入，不使用 sleep。
- 单元测试锁定 Plan/Task/Stage 永久终态、one Gate/one Verdict、reject 不改旧 Task、Continuation 跨代 budget、cumulative acceptance contract 与 arbitrary remediation topology compile。
- AssignmentPool 单元/并发测试锁定 singleton、flat membership、ownerless claimable predicate、claim→open、竞争 CAS、holding cap、reservation expiry/release 与 Plan-first auto-assign；显式 claim 不受 background 排序阻断。
- 属性测试对随机合法 DAG 插入 proposal 后验证无环、历史 Node 不变、只能改 continuation boundary、每个 reject 最多 append 一代。
- 集成测试锁定 `reject → proposal → append → dispatch → new gate pass → original downstream`，以及第二次 reject 产生 generation+1 而不是 reopen。
- 并发测试锁定 `SettlePlan` 与 reject/append 竞争、pause 期间 proposal 回来、CAS stale 后重规划、commit 成功但响应丢失后的幂等 lookup。
- 安全测试锁定无权限 member 不能裁决/插图、跨 Project ID 不泄露、Finding 中的 prompt injection 只能作为数据。
- migration fixture 覆盖 old draft/running/done/archived、当前 reopened、历史 terminal→nonterminal、latest-wins legacy Verdict 与 builtin pool。
- deployed-binary smoke 至少覆盖一次真实 Gate reject 后新增 Remediation Stage 且旧 Stage/Task/Verdict 保持不变，并验证重启后 reconciler 能补推进。

### 迁移门禁与 rollout

这不是状态字符串 rename。上线前先进入短暂 write/dispatch quiesce，并运行只读 migration auditor；审计报告是 cutover 输入，也是无法自动迁移记录的人工处理清单。

1. **完整历史审计**：扫描 Task lifecycle event/audit history 中所有 terminal → nonterminal 路径，不能只查当前 status=`reopened`，因为现有自由 `SetStatus` 也可能把 terminal Task 改成其它状态。
2. **Task split**：能可靠定位旧 completion boundary 时，冻结原 Task 为 terminal，并为后续工作创建带 `follows_task_id` / Verdict 血缘的新 Task 与 Remediation Stage；证据不足时 fail closed，输出人工 resolution report，禁止猜测或覆盖。
3. **Plan status backfill**：old `draft → pending`、`running → running`、`done → done`。old `archived` 已丢失归档前状态：只有强 completion evidence 时可映射 `done + archived_at`；其余必须人工选择 `done` 或 `discarded`，不能静默推断。
4. **Legacy verdict preservation**：现有 latest-wins decision/review row 只能标记为 `legacy_current` evidence。可用 audit enrich，但不得伪造已被覆盖的历史 Verdict；新 `GateVerdict` 表从 cutover 后 append-only 并以 `gate_id` unique。
5. **Builtin extraction**：把 builtin Plan membership 迁到 AssignmentPool，保持 pool identity 映射与 Task identity；逐项对账 claimable/held/assignee、holding cap、auto-assign 配置和 Work Board 位置。兼容窗口内 legacy pool plan ID 只能通过 read/command adapter 解析，不能继续写 Plan status/Node。能力对账通过后才从真实 Plan query/progress 中删除 builtin exception 并启用 Plan 全称状态约束。
6. **Active graph materialization**：仍处于 reopen round 的活动图必须按可证明的 round boundary 物化成旧 terminal Stage + 新 Remediation Stage/Continuation；无法证明边界的 Plan 在 cutover 前保持 quiesced 并进入人工清单。

Rollout 使用 expand → audit/backfill → quiesced cutover → contract：

- expand 只新增 archive marker、AssignmentPool/membership/claim reservation、PlanContinuation、append-only GateVerdict、StageOrigin/AcceptanceContract 与相关 unique/FK；每组 schema change 是独立工作单位；
- backfill 可重复、幂等、生成逐行诊断，不删除 legacy table；
- cutover 原子切换 writer 与状态约束。旧 binary 不得在 cutover 后继续写，部署必须有 version gate；
- contract 只在新读写路径、reconciler 和 rollback window 全部验证后移除 reopen API/table/special-case。历史表先转 read-only，保留期结束后再单独决定删除。

### 实施顺序

ADR Accepted 后按以下依赖顺序拆分，不把 schema、行为改造和 UI 混成一个大 PR：

1. feature design：冻结 Ubiquitous Language、Continuation/Proposal/AcceptanceContract schema、command auth 与错误契约；
2. read-only migration auditor 与 legacy fixture；
3. schema expand（独立 migration work units）；
4. AssignmentPool extraction、claim/read compatibility adapter 与数据 backfill；
5. Plan/Task/Stage 单调状态机、显式 Pause/Resume/Discard、archive marker 与旧 reopen 写入口封禁；
6. GateVerdict + Continuation + atomic append compiler/outbox/reconciler；
7. Supervisor remediation proposal、budget/approval 与 prompt boundary；
8. CLI/Web/agent read models、operator actions 与 deployed-binary smoke；
9. legacy contract cleanup，并同步所有被 supersede 的文档。

每个行为切片必须先有失败测试，再实现，再运行后端全量测试；涉及共享状态/并发推进的切片还必须执行 `make test-race`，直看退出码。

### 文档跟进

ADR Accepted 后再执行以下同步，不能在 Proposed 阶段把旧设计静默改写成已生效：

1. 更新 Ubiquitous Language 中 Plan / Task / Stage / GateVerdict 状态词汇。
2. 将被取代的 feature design 标注 `Superseded by ADR-0055`，不删除历史文档。
3. 独立 feature design 定义 proposal payload、compiler diagnostics、topology commit、权限与 UI/read model。
4. 独立测试计划与测试报告覆盖 unit / property / concurrency / integration / migration / security / deployed-binary smoke。

## 三轮 Grill 记录

| 轮次 | 追问 | 原方案暴露的问题 | 收紧后的决定 |
|---|---|---|---|
| 1：领域语义 | Task 都完成但 Gate 未裁决，Stage 到底完成了吗？同一 Gate 能否重复裁决？builtin Plan 怎么满足终态不变量？ | Stage complete 含糊；多 Verdict 会变相 reopen；assignment pool 迫使 Plan 永远 running | Stage 拆成 awaiting_verdict/accepted/rejected；one Gate/one Verdict；AssignmentPool 移出 Plan，但完整保留 background claimable pool 契约 |
| 2：DAG 与并发 | 插入到底锚定什么？新 Stage 是否重置 max rounds？proposal 生成后 DAG 变化怎么办？pause/崩溃时谁补推进？ | frontier 不稳定；可无限修补；旧 proposal 可能污染新图；纯事件回调会丢推进 | PlanContinuation + boundary；跨代 budget；version/fingerprint CAS 后重规划；pause latch + outbox/reconciler |
| 3：迁移与运营 | 旧 reopen 历史能否重建？latest-wins Verdict 怎么转 append-only？谁能插图？恶意 finding 会不会变成 prompt 指令？用户如何看懂增长中的 DAG？ | 直接 rename 会伪造历史；权限过宽；生成链路有注入面；百分比和“无 ready node”误导 | quiesced audit + fail-closed split；legacy_current 保真；command-level auth；untrusted evidence boundary；ledger/frontier/continuation 三层 read model |

## Alternatives Considered

### A. 保留 reopen，修复所有下游回退传播

拒绝。即使能递归 reopen 下游，也会撤销已经发生的执行、交付和审计事实；外部副作用无法可靠 rollback，复杂度随 DAG 扩散。

### B. Gate reject 后原样克隆旧 Stage

拒绝作为固定语义。它比原地 reopen 更好，因为旧历史保持不变；但仍忽略 reject 带来的新信息。克隆可以是 planner 对某次 Verdict 的选择，不能是引擎硬编码默认。

### C. Gate reject 后修改原 Stage，增加修补 Task

拒绝。原 Stage 的边界与验收结果会被后来的工作改写，无法回答“当时提交了什么、为什么被拒绝”。应新增 Remediation Stage 并保留血缘。

### D. Gate reject 自动把 Plan 设为 paused

拒绝。reject 表示控制流仍在运行、正在等待生成下一段工作；paused 必须只表达 operator 的显式停止意图。生成失败可进入 blocked frontier / human decision，但 Plan 仍是 running。

### E. 保留 draft 作为暂停后的编辑模式

拒绝。draft 混淆“从未执行”和“执行后暂时停止”，并为修改历史 topology 打开后门。pending 承载首次启动前状态；paused 只控制 dispatch。
