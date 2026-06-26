# Cycle 节点图标准规格（Cycle Node-Graph Spec）— v2.13.0 / F1

> **本文是什么。** 把一个开发 cycle 的全流程（开主干 → 开发 → review → 集成 →
> 验收 → ship）固化为一张 **plan 节点图（DAG）模板**：定义节点类型、各节点完成
> 定义（DoD）、DAG 形状、建议 owner 角色映射、以及节点元数据 schema。
>
> **为什么。** 迭代「不顺畅」反复同病：忘切主干、忘把分支合回主干、验收散在孤立
> 分支。根治方向（@oopslink 2026-06-21 拍板）：**不靠文字纪律自觉，把每一步编排成
> plan 节点 —— 用 DAG 结构强制顺序与完成定义。** 文字 SOP 降为兜底说明。
>
> **它的下游。** 本规格是 F2/F3/F4 的实现依据：
> - **F2** `scaffold_cycle_plan(version, features[])` —— 按本规格自动建整张图（结构 only，owner 留空待 PD 指派）。
> - **F3** 运行时合并校验护栏 —— `Integrate(T)` complete 时按节点元数据校验已合回主干。
> - **F4** 未合并分支看板 —— 未完成 `Integrate(T)` 节点清单（集成闸对账用）。
>
> 来源 issue：**I18 = issue-b10f3ca1**。设计已锁定（2026-06-21），本文只做规格化。
> 状态：F1 交付物。建议 owner=Dev、reviewer per-feature。

---

## 1. DAG 形状（一句话 + 图）

```
S0 → (Dev(T) → Review(T) → Integrate(T)) × N → 集成完成 Gate → Accept → Ship
```

- `S0` 是所有 feature 链的共同上游（每条 `Dev(T)` blocked-by `S0`）。
- 每个 feature `T` 是一条独立的 **Dev → Review → Integrate** 链（Review per-feature）。
- `集成完成 Gate` 是 barrier：blocked-by **所有** `Integrate(T)`。
- `Accept` blocked-by `集成完成 Gate`（验收挪到集成之后 = 直接根治「验收散在孤立分支」，在合好的主干上验整体）。
- `Ship` blocked-by `Accept`。

```
                 ┌── Dev(T1) → Review(T1) → Integrate(T1) ──┐
                 │                                           │
   S0 ───────────┼── Dev(T2) → Review(T2) → Integrate(T2) ──┼──→ 集成完成 Gate ──→ Accept ──→ Ship
                 │                                           │     (barrier，                 (dev/vX.Y.0
                 └── Dev(Tn) → Review(Tn) → Integrate(Tn) ──┘      blocked-by 全部 Integrate)   → main + tag)
```

### 1.1 分支模型
`main ← dev/vX.Y.0（本 cycle 集成主干）← 各 feature 分支（从主干切、合回主干）`。
- `main` 只在 **Ship** 收一次。
- 判集成一律 `fetch` 看 `origin/dev/vX.Y.0`，**禁信本地 stale**（T229 教训，见 §6）。

---

## 2. 节点类型 + DoD（完成定义）

每个节点的 DoD 是 **可被结构化检查或人工对照的硬条件**；标 done 即代表 DoD 满足。

### 2.1 `S0 开发主分支`
- **做什么**：从 `main` 切 `dev/vX.Y.0` 集成主干并推到 origin + 在 plan 会话周知。
- **DoD**：
  1. `origin/dev/vX.Y.0` 存在（`git ls-remote origin dev/vX.Y.0` 有结果）。
  2. 已在 plan 会话周知「主干已开，feature 链从此切」。
- **元数据**：`base = main`（S0 的 base 即上一稳定线），`branch = dev/vX.Y.0`。
- **下游**：所有 `Dev(T)` blocked-by S0。

### 2.2 `Dev(T)` 开发
- **做什么**：从 `dev/vX.Y.0` 切 feature 分支 `T` 实现。
- **DoD**：
  1. feature 分支已实现该 feature 的全部范围。
  2. 分支已推到 `origin`（本地 commit 不算交付，见 §6）。
  3. 自测/构建在 feature 分支可过（细节门交给 Review 的 §-1 gate）。
- **元数据**：`branch = <T号>`（默认），`base = dev/vX.Y.0`。
- **blocked-by**：`S0`。
- **注**：Dev「完成」≠ 已合回主干；合回由对应 `Integrate(T)` 把关。

### 2.3 `Review(T)` 评审（per-feature）
- **做什么**：在 feature 分支 `T` 上做 code review + **§-1 真实 make gate**。
- **DoD**：
  1. code review 通过（无阻塞性意见或意见已解决）。
  2. **§-1 gate 真跑且绿**：`build` / `lint` / `tsc -b`（前端含 `pnpm --dir web test`，
     make build/lint 不跑 vitest）—— 在 feature 分支跑，带证据。
- **元数据**：随 feature 链，`branch = <T号>`，`base = dev/vX.Y.0`。
- **blocked-by**：`Dev(T)`。

### 2.4 `Integrate(T)` 集成（**合并校验落点**）
- **做什么**：合 feature 分支 `T` → `dev/vX.Y.0`，fetch 后确认 `origin` 含该 commit。
- **DoD（complete 时由 F3 护栏强制校验）**：
  1. `origin/dev/vX.Y.0` **--contains** `T` 分支 HEAD（合并已落 origin 主干）。
  2. 判定**只信 origin**，禁本地 stale。
  3. 未满足 → **complete 被挡下**，返回可操作的错误（见 §5 / §6）。
- **元数据**：`branch = <T号>`，`base = dev/vX.Y.0`（作合并校验输入）。
- **blocked-by**：`Review(T)`。
- **持有者**：PD 每 cycle 指派，可指定专职 integrator。

### 2.5 `集成完成 Gate`（集成闸 / barrier）
- **做什么**：关门核对 —— 全部 feature 是否都已 Integrate 回主干。
- **DoD**：
  1. **所有** `Integrate(T)` 节点 done（DAG 天然保证：任一未 done 则本节点 blocked）。
  2. PD 对照 **F4 未合并分支看板**确认零未合并 feature（= 零未完成 Integrate 节点）。
- **blocked-by**：**所有** `Integrate(T)`。
- **是什么**：PD ship 前的关门对账点；与 F4 看板一一对应。

### 2.6 `Accept 验收`
- **做什么**：在 `dev/vX.Y.0` 主干（已集成全部 feature）上，对照 mockup/DoD **整体验收** + 出验收报告。
- **DoD**：
  1. 在主干上 run-real 逐模块验收（不是孤立 feature 分支、不是 mock-only）。
  2. 出验收报告（带证据/截图，遵循 ACCEPTANCE-EVIDENCE-SPEC）。
  3. 验收期发现的修复 commit **直接进主干**，由 integrator 收口确认（不另开孤立分支）。
- **blocked-by**：`集成完成 Gate`。
- **持有者**：建议 tester。

### 2.7 `Ship`
- **做什么**：`dev/vX.Y.0 → main`、打 tag `vX.Y.0`、清理分支/worktree。
- **DoD**：
  1. `main` 已含本 cycle 全部 feature（`origin/main` --contains `dev/vX.Y.0` HEAD）。
  2. tag `vX.Y.0` 已打并推。
  3. feature 分支 + worktree 已清理。
- **blocked-by**：`Accept`。
- **持有者**：建议 PD（或专职 integrator）。

---

## 3. 建议 owner 角色映射

> **工具只生成结构、不硬编 owner**；下表是 PD 每 cycle 指派时的**参考默认**
> （@oopslink 拍板：「Plan 任务节点由工具生成，指派给谁由 PD 自己决定」）。

| 节点 | 建议 owner | 备注 |
|---|---|---|
| `S0` | PD | 切主干 + 周知 |
| `Dev(T)` | dev | 每 feature 一名 |
| `Review(T)` | reviewer / §-1 agent | **per-feature**，可与 Dev 不同人 |
| `Integrate(T)` | **专职 integrator**（PD 每 cycle 指派） | 合并校验落点 |
| `集成完成 Gate` | PD | 关门对账 |
| `Accept` | tester | 集成全部完成后才开始 |
| `Ship` | PD / integrator | 收口 + tag + 清理 |

---

## 4. 节点元数据 Schema

> 合并校验（F3）与未合并看板（F4）的输入。**模型现状**：`Task`（即 plan 节点的
> 载体）当前**无** branch/base 字段，节点也无任意元数据位（`internal/projectmanager`：
> 边 = `Dependency{FromTaskID, ToTaskID}`，node_status 派生不落库）。因此 **F2 需引入
> 节点元数据**。本节定义其**逻辑 schema**，存储落点交 F2 决定（见 §4.3 建议）。

### 4.1 字段

| 字段 | 类型 | 必填 | 默认 | 含义 / 用途 |
|---|---|---|---|---|
| `branch` | string | 是 | **`<T号>`**（节点 org_ref，如 `T262`） | 该节点工作所在的 feature 分支名；F3 校验 `--contains` 的对象 |
| `base` | string | 是 | **`dev/vX.Y.0`** | 集成主干分支名；F3 校验「branch 是否已合进 `origin/<base>`」的目标 |
| `skip_merge_check` | bool | 否 | `false` | 结构化豁免合并校验（纯文档/无代码 feature，链省到 `Dev`、无 `Integrate`）。**仍计入集成闸**，只是 Integrate 校验不适用 |

- `branch` 默认取节点的 **T 号**（org_ref），可被 PD/Dev 覆写为实际分支名（如 `f1-spec`、`f2-scaffold-tool`）。
- `base` 默认 `dev/vX.Y.0`（本 cycle 主干）；`S0` 例外，其 `base = main`。
- **仓库定位**：合并校验在哪个仓库跑，取 Project 上的 `CodeRepoRef`（url+label）；元数据只携带分支名，不重复仓库地址。

### 4.2 哪些节点带元数据
- `S0`：`branch = dev/vX.Y.0`，`base = main`。
- `Dev/Review/Integrate(T)`：同一条链共享 `branch = <T号>`、`base = dev/vX.Y.0`。
- `Integrate(T)`：是 F3 校验真正生效的节点（complete 时校验）。
- `集成闸/Accept/Ship`：无 feature 分支语义（或仅用 `base` 做 ship 校验）；不参与 per-feature 合并校验。
- 纯文档/无代码 feature：链省到 `Dev`，该 `Dev` 节点 `skip_merge_check = true`（或干脆无 Integrate 节点 = 结构化豁免）。

### 4.3 存储落点建议（交 F2 定）
现状无承载字段，建议二选一（F2 实现时定，本规格不强制）：
1. **节点级元数据 JSON**：在 plan 节点（task-in-plan）上加 `node_meta`（`{branch, base, skip_merge_check}`），与 node_status 一样按 plan 维度解释。
2. 复用 `Task` 上新增的结构化字段（`branch`/`base`/`skip_merge_check`）。

推荐 (1)：元数据是「节点在本 cycle 图里的角色属性」，随 plan 语义，避免污染 backlog Task 的通用模型。

---

## 5. 运行时护栏要求（给 F3 的契约）

`Integrate(T)` 节点 **complete 时**：
1. 读节点元数据 `branch` + `base`，定位 Project 的 `CodeRepoRef` 仓库。
2. 校验 `origin/<base>` 是否 `--contains` `<branch>` 的 HEAD（即分支已合回主干）。
3. **判定只信 origin**：校验前必须 `fetch`，禁用本地 stale ref（见 §6）。
4. `skip_merge_check = true` → 跳过校验直接放行（结构化豁免）。
   - **T330**：Project **未配 `CodeRepoRef`** 时，等同项目级关闸——自动跳过 merge-check 直接放行（无仓可校验，整库覆盖当前+未来所有 Integrate 节点），不再 fail-closed 卡 complete。已配 `CodeRepoRef` 的项目不受影响，校验照常生效。`scaffold_cycle_plan` 亦提供 `skip_merge_check` 入参，建图时即可对整个 cycle 关闸（默认 `false`=保持校验）。
5. 未满足 → **拒绝 complete**，错误信息**可操作**，至少含：
   - 哪个 `branch` 未进哪个 `origin/<base>`；
   - 提示动作（合并并推 origin 后重试）。
6. `集成闸`/`Ship` 被未完成上游阻塞 = DAG 天然，无需额外校验逻辑。

---

## 6. 贯穿纪律（编码进结构，不靠自觉）

- **只信 origin**：所有「是否已合并」判定一律 `fetch` 后看 `origin/...`，禁信本地 stale（T229 教训）。
- **Dev 完成 = 已推 origin**：本地 commit 不算交付；分支必须 `git push -u origin <branch>`。
- **隔离 worktree**：feature 工作用独立 worktree，用完即清。
- **验收在主干**：Accept 在集成后的 `dev/vX.Y.0` 上整体验，不在孤立 feature 分支。
- **修复也回主干**：验收期 fix commit 直接进主干、integrator 收口，不另开孤立分支。

---

## 7. 验收对照（本规格据 I18 §验收）

- [ ] cycle plan 一眼是节点图：`S0→(Dev→Review→Integrate)×N→集成闸→Accept→Ship`，依赖连好；工具生成后 PD 完成指派。
- [ ] 任一 feature 未过 Review / 未 Integrate → 集成闸不开 → Accept/Ship 阻塞。
- [ ] `Integrate` 节点未合回主干无法 complete（origin 判定，错误信息可操作）。
- [ ] PD 一键查未完成 `Integrate`（= 未合并分支）清单。
- [ ] 验收在集成闸之后、于主干上整体进行。

---

## 8. 实现交接（F2/F3/F4 据此开工）

- **F2** `scaffold_cycle_plan(version, features[])`：按 §1 形状一次性建图（S0 / 每 feature 的 Dev→Review→Integrate / 集成闸 / Accept / Ship），连好 §1 的 blocked-by 边；**owner 留空待 PD 指派**；按 §4 给节点带 `branch`(默认 T号) + `base`(dev/vX.Y.0) + 可选 `skip_merge_check`。
- **F3** 运行时护栏：实现 §5 契约。
- **F4** 未合并分支看板：列出未完成 `Integrate(T)` 节点（= 未合并 feature），对应 §2.5 集成闸对账。
