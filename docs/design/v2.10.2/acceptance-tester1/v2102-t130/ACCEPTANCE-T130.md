# v2.10.2 — [Tester1 验收] claimability/authz 硬门 (T130 / task-d61859ad)

**结论：GO ✅（5/5 全过，含 5 条 live HTTP 红线码 + 全测试层绿）**

- 分支/commit：`v2.10.2` @ `27184d7b`（= origin/v2.10.2 frozen tip；T130 已合入 abe6dc50 + fix 22939238）
- run-real 实例：`test-v2102acc`，binary `agent-center v2.10.2-27184d7b`，admin API `https://127.0.0.1:57011`
- 被测 agent：`01KV5SAN4BCR4NV2T57YRH22HD`（identity `agent:agent-4d821b1d`，project Alpha `project-2ee9e524` 成员）
- 证据：`redline_http_transcript.txt`（完整请求/响应可复现）· `s1_redline_http_evidence.png` · `s2_tests_green.png`

## 逐条结论（带证据）

| # | 硬门要求 | 验收标准 | live 证据（HTTP 码） | 测试层 | 状态 |
|---|---|---|---|---|---|
|1|backlog task（不在真实 Plan、不在 Pool；含 builtin = backlog）**不可被 claim**|claim_task → 拒|`POST /claim_task` 于 backlog task → **HTTP 409 `not_claimable`**（"task is not claimable from the assignment pool"）|pm `ClaimPoolTask`：Backlog/Structured/NonMember/SecondClaimLoses/HoldingCap 全 PASS|✅|
|2|backlog **不可经「直接指派→start_work」变 running**（本次新堵路径）|direct assign 铸出 queued 工作项 → start_work 被拒，task 仍 open|`POST /assign_task`→200 铸 WI；`POST /start_work` → **HTTP 409 `task_not_runnable`**；事后 DB：task=`open`、work_item=`queued`（未翻转）|agent `StartWork_BacklogTaskRejected_T130`（拒后留 queued）+ `GateError_Propagated`；admin e2e `DirectAssignBacklog_RejectedEndToEnd_T130`|✅|
|3|**builtin 计划 ≠ 真实 Plan** 判定正确|谓词 `!plan.IsBuiltin()`：builtin 仅 dispatched 才可跑|DB：`plan-84a88fdb is_builtin=1 [Built-in]`（池）vs 真实 plan `is_builtin=0`；谓词据此分流|pm `EnsureTaskRunnable_BuiltinNotDispatched_Rejected`（builtin 未派=拒）+ `DispatchedPoolMember_OK`（派后放行）+ `RealPlanNode_OK`|✅|
|4|task 入真实(非 builtin) Plan 节点 或 Pool 后**可正常 running**（不误伤合法路径）|同一被拒 WI 加入真实 plan 后 start_work 成功|`POST /create_plan`(real,is_builtin=0)→`POST /add_task_to_plan`→200→**同一 WI** `POST /start_work` → **HTTP 200 `active`**|pm `RealPlanNode_OK`/`DispatchedPoolMember_OK`；admin e2e 同一 WI add-to-plan 后可启|✅|
|5|越权/非法操作 **fail-closed**|未授权一律拒、不泄露存在性|5a 伪造 agent_id → **404 `not_found`**；5b claim 不存在 task → **404 `not_found`**（opaque，T83 §4.3）；5c 无 bearer → **401 `auth_missing`**|claim 错误映射：ErrNotMember/NotFound→404 opaque、NotClaimable/AlreadyClaimed/CapReached→409|✅|

## 无回归
- `go vet ./internal/projectmanager/service ./internal/agent/service ./internal/admin/api` → clean
- 设计要点（核对）：闸口落在 **open→running 边界**，**不落在 assign**（assign 仍解耦记归属，零回归 71 测夹具）；agent BC 仅依赖 `TaskRunGate` 端口、经组合根注入 pm 适配器，无反向 import。

## 备注
- 红线 #2/#1 为本轮新堵路径，均以净 v2.10.2 frozen tip 二进制 live 复现，HTTP 码非 mock。
- run-real 探针为临时数据（probe task/plan），落在 v2102acc 沙箱，不影响代码冻结树。
