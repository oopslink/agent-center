# v2.13.0 / I18 — 集成后主干整体验收报告（T272 Accept）

- **Issue**: I18 = issue-b10f3ca1 「Cycle 流程编排成 plan 节点图 + 合并校验护栏」
- **Plan**: plan-0abb326f（本 cycle dogfood 自身的节点图）
- **验收对象**: `origin/dev/v2.13.0 @ 8eb1ffc1`（集成完成 Gate T271 已 done，26/28；所有 Integrate 节点已合回主干）
- **Binary**: `agent-center dev/v2.13.0-8eb1ffc1`
- **验收人**: agent-center-tester1
- **结论**: **F1–F4 + 5 条 I18 核心验收准则 = GO**。另抓到 1 个 B 轨（控制流）逃逸缺陷 F-T272-1，单列，不阻塞 5 条核心准则，建议 Ship 前修。
- **证据**: `evidence/v2130-t272/`（workspace），见每条引用。

> 方法学：F1–F4 主要是后端（MCP 工具 / 运行时护栏 / 查询）+ 1 个薄前端面板。关键步骤用
> **真 git 仓库** 与 **真 sqlite（migrations 0066/0067 落库）** 跑产线路径取 AFTER 证据，
> 而非 fake；前端面板由单测 + tsc/eslint/vitest 闸覆盖。临时 probe 跑完即删、主干保持 frozen。

---

## 1. 逐条验收准则对账（I18 §验收）

| # | 准则 | 结论 | 证据 |
|---|---|---|---|
| 1 | cycle plan 一眼是节点图，依赖连好、PD 完成指派 | ✅ GO | scaffold 产出 15 节点图 + 控制流边，**owner 全空**；且 owner 未指派时 `StartPlan` 被拒（坐实"PD 必须完成指派"）。`02-f2-f4-gating-probe.txt` |
| 2 | 任一 feature 未过 Review/未 Integrate → 集成闸不开 → Accept/Ship 阻塞 | ✅ GO | 派生视图：仅 S0 ready；Dev/Integrate/Gate/Accept/Ship 全 `blocked`。`02-...txt` + 生产侧旁证：本 plan 的 Accept(T272) 直到集成闸 T271 done 才 claimable |
| 3 | Review 打回时 Integrate 保持 blocked；修复（可多轮重跑）重审通过才解锁 | ✅ GO | 控制流路由单测全绿：`ConditionalRouting`(pass→Integrate / reject_exhausted→Escape)、`LoopbackNotForwardBlocking`、`CycleControlFlowRouting`。`03-criteria-3-4.txt` |
| 4 | `Integrate` 未合回主干无法 complete（origin 判定，错误可操作） | ✅ GO | **真 git** run-real：已合→放行、未合→挡下、缺分支→fail-closed；only-trust-origin（always-fetch，origin 合并后 false→true）。错误文案点名 branch/base + "合并并推 origin 重试"。`01-f3-realgit.txt` + `03-...txt`(错误 sentinel) |
| 5 | PD 一键查未完成 `Integrate`（= 未合并分支）清单 | ✅ GO | `ListUnmergedIntegrations` 精确列 2 个未完成 Integrate 分支，doc-only(skip) 不入榜，`AllMerged=false`。`02-...txt` |
| — | 验收在集成闸之后、于主干上整体进行（F1 §7 补充项） | ✅ GO | 本报告即在 `origin/dev/v2.13.0@8eb1ffc1`（集成闸 done 后）整体验收 |

---

## 2. 交付物逐项（F1–F4）

### F1 — Cycle 节点图标准规格文档 ✅
`docs/design/v2.13.0/cycle-node-graph-spec.md`：节点类型 + DoD + DAG 形状 + 建议 owner 映射 + 节点元数据 schema(§4 branch/base/skip_merge_check) + 给 F3 的护栏契约(§5) + §7 验收对照。下游 F2/F3/F4 实现与本规格一致（见下）。

### F2 — `scaffold_cycle_plan(version, features[], max_review_rounds?)` ✅
真 sqlite 跑 `Service.ScaffoldCyclePlan`（features=[F1 码, F2 码, F9 doc-only]）产出：
- **15 节点**：S0 / 3×Dev（2 码 + 1 文档）/ 2×Review / 2×Decision / 2×Integrate / 2×Escape / Gate / Accept / Ship。
- **owner 全空**（PD 指派前不带 assignee，符合 finding 01KVKXFP 约束：带 assignee 即预派发绕过 DAG 闸）。
- **doc-only 折叠**为单 Dev（`skip=true`、`branch=T<n>` 默认 T 号），无 Review/Decision/Integrate/Escape。
- **元数据落库**：每码节点 `branch`(=feature 分支) + `base=dev/v2.13.0`（migration 0067 role 列 + 0066 branch/base/skip）。
- **控制流边**（B2 升级）：`Dev→S0`(seq)、`Review→Dev`、`Decision→Review`、`Integrate--conditional(pass)-->Decision`、`Escape--conditional(reject_exhausted)-->Decision`、`Decision--loopback(reject,max=3)-->Dev`；`Gate→{两 Integrate, doc Dev}`、`Accept→Gate`、`Ship→Accept`。边方向遵守 ComputePlanView condUp 约定（From=下游/To=decision）。
证据 `02-f2-f4-gating-probe.txt`。

### F3 — 运行时合并校验护栏 ✅
- 落点：`CompleteTask` 在开事务前调 `guardIntegrateMerge`（`assign_flow.go`）。
- 仅 `role==integrate` 触发；`skip_merge_check`/非 integrate 角色/无 branch-base 放行；**无 CodeRepoRef 或 fetch/校验出错 → fail CLOSED**（`ErrIntegrateMergeUnverifiable`）；未合 → 挡（`ErrIntegrateBranchNotMerged`，文案可操作）。
- git 适配器 `mergecheck`：per-repoURL **bare mirror 永远 fetch** 后 `merge-base --is-ancestor`，exit0=合/exit1=未合/其他=错，缺 ref=错；环境中性化（GIT_CONFIG_GLOBAL=/dev/null 等）。
- **真 git run-real** 四象限全过 + only-trust-origin（`01-f3-realgit.txt`）。

### F4 — 未合并分支看板 ✅
- `ListUnmergedIntegrations(plan)` → `UnmergedBoard{Detail, Unmerged[], AllMerged()}`，投影 `role==integrate && node_status!=done`。
- `CycleNodeMetaPort` 经真 adapter `TaskRepoCycleMeta`（读同一 TaskRepo，F2 写入处）注入，composition root(`app.go`) 已接线（非测试 fake）→ 看板不会恒空。
- MCP `list_unmerged_branches` + web `GET …/plans/{id}/unmerged-branches` + `PlanDetail` `UnmergedBranchesPanel` 全路由在 dev binary 上已服务（wire-check 403"需 worker bearer"，非 404）。
- run-real 列出精确 2 条未合分支、doc-only 不入榜（`02-...txt`）。

---

## 3. 缺陷 / 观察

### F-T272-1 — loopback 逃生边判向反了 ✅ 已修（本 cycle，oopslink 拍板"在本 plan 修复"）
> **处置**：一行修复 `e.FromTaskID==changed`→`e.ToTaskID==changed` + 复现回归测试
> `TestApplyLoopbacks_ExhaustionRoutesToEscape`（RED→GREEN），已推 `origin/fix/t272-loopback-escape @ 8ea4a90b`，
> §-1 gate 全绿（make build + make lint + go test ./internal/projectmanager/...）。**待 integration-dev 合回 `dev/v2.13.0`**，合后在主干复验、本节转最终签字。下为缺陷原貌存档：

- 文件：`internal/projectmanager/service/plan_controlflow.go:116`（`applyLoopbacks`）。
- `changed` = 刚完成的 Decision；逃生 conditional 边约定 `From=Escape / To=Decision`（与 scaffold 产出、`plan_controlflow_test.go` 一致）。
- 第 116 行用 `e.FromTaskID == changed` 找逃生边——但逃生边的 `From` 是 Escape 节点、永不等于 decision；故 `hasEscape` 恒 false。
- 后果：回环**耗尽**时不写 `RecordDecisionOutcome(reject_exhausted)`、不路由 Escape，只 `notifyLoopStuck`。偏离 B0 §4.1/§9「耗尽自动路由 Escape」。
- 范围/严重度：**只**影响 B 轨（B1 引擎 / B3 决策自动化）的耗尽路径；**不**触及 5 条 I18 核心准则；happy path（pass→Integrate、reject→下一轮）完好；非死循环（会通知 stuck 可人工兜底）。
- 来历：B2-Dev 在 finding 01KVMGW9 已提、留 B3 跟进，但 B3(437a1d1f) 未动此文件 → 仍在主干。
- 修复：一行 `e.FromTaskID == changed` → `e.ToTaskID == changed`（走 integration-dev 合回主干）。
- 证据 `04-finding-loopback-escape.txt`。

### 观察（非阻塞）
- 第 71 行同样用 `FromTaskID == changed` 但对 loopback 边是**正确**的（loopback `From=decision`），勿误改。
- 存量门修复：主干靠 `dev2/v213-fix-emoji-icon-a11y`(✓→SVG) 让 §-1 web-test 转绿（finding 01KVKZZA）；本次后端验收未走 web vitest，按既有 finding 采信。

---

## 4. 证据清单（`evidence/v2130-t272/`）
- `00-build-and-suite.txt` — make build 绿 + 全量 projectmanager 测试绿
- `01-f3-realgit.txt` — F3 真 git run-real（合/未合/缺分支/only-trust-origin）
- `02-f2-f4-gating-probe.txt` — F2 图形 + 准则1/2 gating + F4 看板（真 sqlite）
- `03-criteria-3-4.txt` — 准则3 路由单测 + 准则4 可操作错误 sentinel
- `04-finding-loopback-escape.txt` — F-T272-1 缺陷分析 + 行级证据
- `05-dev-web-console.png` — dev/v2.13.0 SPA 首启页（证 SPA 在本 build 正常服务）

> 注：F4 看板面板/plan 图的像素截图需 worker+agent 喂满 seed（重）；当前由
> `handlers_pm_unmerged_f4_test` + web vitest/tsc/eslint 闸覆盖。如需面板像素证据可按需补做。
