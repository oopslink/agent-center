# v2.12.0 验收清单 — I7 Agent@Agent 唤醒护栏 + Settings 可配（P23）

- **验收目标**：`dev/i7`（集成主干，含 D1/D2/D3 + T227 修复 + T245 修复 + a11y 修复）
- **执行**：PD（agent-center-pd），owner 指派「开始验收」（2026-06-20）
- **方法学**：`docs/rules/acceptance-methodology.md`（§4 verify-in-tree、§8 构建门、§11 release flow 铁律）
- **总体裁定**：**GO（功能验收通过）** — BE 四闸护栏 + human 必达 + D3 Settings 可配真生效 + Version 提级 + 授权红线 + **Tester3 独立 UI run-real（§1/§2/§5）全 PASS** —— 全部带 in-tree 证据。剩 ship 阶段硬门：§8 复跑 `make lint/build` + deployed-smoke + **dev/i7→main 合并冲突解决**（main 已并入「Reminders 提顶级 + English-only UI」，与本支 emoji→icon 改重叠）+ owner 同意。

> 判定铁律（§11-3）：本报告**不以节点 `completed` 推断 PASS** —— 每条结论附 in-tree 证据。Tester3 的 T224 已交完整 run-real 证据（`evidence/t224/`，verify-not-trust + both-mode），核过为真 PASS。

---

## 逐任务 verdict

| 任务 | 模块 | Verdict | 证据 |
|---|---|---|---|
| **T216** I7-D1 | wake-chain 四闸护栏 + 可配 live settings + GET/PUT API | ✅ PASS | 单测 `TestDepthGate`/`TestCycleGate_ABA`/`TestRateGate`/`TestCostGate`/`TestEvaluateHop_*` 绿；live settings GET/PUT(见 T218 证据) |
| **T217** I7-D2 | agent mention 语义:可回可不回 + 静默确认 | ✅ PASS | `internal/conversation/...` 全域测试绿(回归不破坏既有 mention authz) |
| **T218** I7-D3 | System/Settings 护栏配置面板 + Version 提级 | ✅ PASS | `evidence/T218-wake-guardrail-*` + Tester3 独立复核(见 T224) |
| **T223/T224** 验收 | Tester1 BE 防风暴+授权 / Tester3 UI 可配+Version+回归 | ✅ PASS | T223:`evidence/t223/four-gates-wired.md`(四闸 trace + human bypass);T224:`evidence/t224/`(§1 保存真生效 verify-not-trust + 非法值 400 atomic + §2 Version 同级 + §5 回归,both-mode,`api-evidence.txt`) |
| **T227** I7-D1 修复 | wakeguard 接入真实 wake 投递路径(四闸 live 生效) | ✅ PASS | `evidence/t223/four-gates-wired.md`：四闸在 `WakeProjector.enqueueWake` 真熔断,带 trace(`gate=cycle/rate/depth/cost`);depth/cost 跨 hop 真累积 |
| **T245** Bug | PUT /api/system/wake-guardrail 501(SettingsStore 未接线) | ✅ PASS | `runWebConsole` 补 `SettingsStore` 接线(70185c80);FE 保存失败 UI 提示回归(T245 test) + Tester3 §1.4 实证 |

---

## 授权红线（PD 必核） — 全过 ✅

| 红线 | 结论 | 证据 |
|---|---|---|
| human 通路必达、不被护栏误伤 | ✅ | `TestHumanActor_BypassesAllGates`(单测) + `TestWakeProjector_WakeGuard_HumanBypasses`(projector live path,rate=0 仍投递) |
| 四闸真熔断(深度/环/频控/成本) | ✅ | projector 集成测 4 条全 PASS + 单测;trace 实证 `suppressed by wake-chain guard gate=...` |
| 不破坏既有 mention 授权(D2) | ✅ | conversation 全域回归测试绿 |
| 默认值符契约 | ✅ | `TestDefaultConfig` = max_depth4/cycleN3/rate10/window5m;非法值(depth=0)→400 invalid_config(T218 run-real 实证) |

---

## §8 构建发布门 — 待 ship 前复跑（指针）

- 上次在 dev/i7 跑过 `go build ./...` + I7 测试绿；FE `tsc -b`+`eslint`+`vite build`+full vitest 1156 绿(a11y no-emoji-icons 已修)。
- **ship 前复跑** `make lint`+`make build` 于最终 tip + deployed-smoke ≥1。

---

## 功能验收 = GO；ship 阶段待办（owner 同意后 PD 跑）

- [x] **Tester3 独立 UI run-real（T224/A2）** — PASS,证据 `evidence/t224/`(verify-not-trust + both-mode)。
- [x] **证据归位** — Tester1 `four-gates-wired.md` 已归 `evidence/t223/`。
- [ ] **§8 复跑 + deployed-smoke**(ship 前,于最终 tip)。
- [ ] **dev/i7→main 合并冲突解决** — main 已并入「Reminders 提顶级 + English-only UI」(`6f1afa1e`),与本支 emoji→icon 改重叠;冲突文件 `Reminders.tsx`/`ReminderCreateModal/DetailModal.tsx`/`AppLayout.tsx`/`webconsole/api/server.go`,合并时需手工合(保留 English-only + icon 两者)。
- [ ] **README/CHANGELOG/sites + tag v2.12.0 + 清分支**。
- [ ] **owner 同意验收**(§11-4)。

## 非阻塞观察(release note / 后续)

- 护栏面板**保存成功无显式 toast**(失败有红字校验);纯 UX,建议后续补,不阻塞本次。

---

## 小结（GO）

I7 核心(四闸熔断 + human 必达 + 可配 live settings + Version 提级)+ 授权红线 + Tester1/Tester3 独立 run-real —— 全带 in-tree 证据验过。**功能验收 GO**。剩 ship 硬门(§8+smoke)+ main 合并冲突解决 + owner 同意,即合 `dev/i7 → main` + tag `v2.12.0`。
