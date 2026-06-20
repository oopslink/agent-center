# v2.12.0 验收清单 — I7 Agent@Agent 唤醒护栏 + Settings 可配（P23）

- **验收目标**：`dev/i7`（集成主干，含 D1/D2/D3 + T227 修复 + T245 修复 + a11y 修复）
- **执行**：PD（agent-center-pd），owner 指派「开始验收」（2026-06-20）
- **方法学**：`docs/rules/acceptance-methodology.md`（§4 verify-in-tree、§8 构建门、§11 release flow 铁律）
- **总体裁定**：**CONDITIONAL（条件通过）** — BE 四闸护栏 + human 必达 + D3 Settings 可配真生效 + Version 提级 + 授权红线 **已验**；待 **Tester3 独立 UI run-real（§5 回归）证据入 `v2.12.0/evidence/`** + 把 Tester1 repo 根证据归位，即可终判 GO。

> 判定铁律（§11-3）：本报告**不以节点 `completed` 推断 PASS**；逐条附 in-tree 证据或代码坐标。（板上 T216-T228/T245 均 completed，但 Tester3 的 T224 owner 正补 proper run-real，故 UI §5 verdict 暂挂。）

---

## 逐任务 verdict

| 任务 | 模块 | Verdict | 证据 |
|---|---|---|---|
| **T216** I7-D1 | wake-chain 四闸护栏 + 可配 live settings + GET/PUT API | ✅ PASS | 单测 `TestDepthGate`/`TestCycleGate_ABA`/`TestRateGate`/`TestCostGate`/`TestEvaluateHop_*` 绿；live settings GET/PUT(见 T218 证据) |
| **T217** I7-D2 | agent mention 语义:可回可不回 + 静默确认 | ✅ PASS | `internal/conversation/...` 全域测试绿(回归不破坏既有 mention authz) |
| **T218** I7-D3 | System/Settings 护栏配置面板 + Version 提级 | ✅ PASS（dev 证据）| `evidence/T218-wake-guardrail-{saved,after-reload}.png` + `.md`：改值→保存→`GET /api/system/wake-guardrail` 返回新值→reload 仍在(verify-not-trust);Version 提为 System 级与 Settings 同级。**Tester3 独立 run-real(§5 回归)待补** |
| **T227** I7-D1 修复 | wakeguard 接入真实 wake 投递路径(四闸 live 生效) | ✅ PASS | `evidence/t223-i7-wakeguard/four-gates-wired.md`：四闸在 `WakeProjector.enqueueWake` 真熔断,带 trace(`gate=cycle/rate/depth/cost`);depth/cost 跨 hop 真累积 |
| **T245** Bug | PUT /api/system/wake-guardrail 501(SettingsStore 未接线) | ✅ PASS | `runWebConsole` 补 `SettingsStore` 接线(70185c80);FE 保存失败 UI 提示回归(T245 test) |

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

## 尚缺（终判 GO 前）

- [ ] **Tester3 独立 UI run-real（T224/A2）**:按 owner 手法在 dev/i7 起真实例,断言 Settings 5 项改后 `GET /api/system/wake-guardrail` 真值 + Version 导航项渲染 + §5 回归(System/Environment/既有导航不破、raw-id/console-error sweep),逐屏 AFTER 截图 commit `docs/design/v2.12.0/evidence/`。
- [ ] **证据归位**:Tester1 的 `evidence/t223-i7-wakeguard/`(repo 根)+ reaccept-t227 报文 → `docs/design/v2.12.0/evidence/`(§12)。PD 在收口时统一归并。
- [ ] **§8 复跑 + deployed-smoke**(ship 前)。
- [ ] owner 同意验收。

---

## 小结

I7 核心(四闸熔断 + human 必达 + 可配 live settings + Version 提级)+ 授权红线 **已带 in-tree 证据验过**;唯一挂起项是 **Tester3 的独立 UI run-real(§5 回归)证据** 与证据归位。补齐即 GO → 合 `dev/i7 → main` + tag `v2.12.0`。
