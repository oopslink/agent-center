# T224 / I7-A2 验收报告 — UI 护栏可配生效 + Version 同级 + 回归

- **验收人**: agent-center-tester3
- **日期**: 2026-06-20
- **被测构建**: `origin/dev/i7` HEAD `7bc126f1`（含 F1 PUT-501 修复 `70185c80`），`make build` 后 turnkey 实例 `agent-center install test-instance --id t224 --with-seed`
- **方法**: 真浏览器 run-real + verify-not-trust（UI 操作，后端 `curl /api/system/wake-guardrail` 断言真值）
- **总结论**: ✅ **PASS**（§1 保存真生效 + 校验、§2 Version 同级、§5 回归 全部通过）

---

## §1 Settings 护栏参数「保存后真生效」 — ✅ PASS

护栏面板「唤醒护栏」含 5 项：`max_depth` / `cycle_window_sec` / `cycle_threshold` / `rate_per_min` / `chain_token_budget`，默认 `4 / 300 / 3 / 10 / 16`。

| # | 步骤 | 期望 | 实测 | 结论 |
|---|------|------|------|------|
| 1.1 | 打开 System→Settings，看默认态 | 5 项 = 4/300/3/10/16 | UI = `4,300,3,10,16`；`curl` 后端同值 | ✅ |
| 1.2 | UI 改 `max_depth` 4→7、`cycle_window` 300→600，保存 | 保存成功 | 保存后 `curl` 读回 `{"max_depth":7,"cycle_window_sec":600,...}` | ✅ |
| 1.3 | **硬刷新页面**（面板每次从后端读） | 仍显示 7/600 = 真持久化 | 重新加载后 UI = `7,600,3,10,16` | ✅ |
| 1.4 | 填非法值 `max_depth=0` 保存 | UI 提示 + 后端拒绝 | UI 红字「所有阈值必须为正整数（> 0）。」；`curl` 后端仍 7/600（未污染）；直接 `PUT depth=0` → **HTTP 400 `invalid_config: wake.max_depth must be > 0`** | ✅ |

**回归点（F1）**：之前 :7357/t218-part1 上保存返回 `501 settings store not configured`、不持久化；本构建已修复，PUT 200 落库。

证据：`01-settings-guardrail-BEFORE-{light,dark}.png`、`03/04-settings-guardrail-AFTER-{light,dark}.png`、`05-settings-invalid-depth0-light.png`、`api-evidence.txt`。

## §2 Version 提级 — ✅ PASS

| # | 步骤 | 期望 | 实测 | 结论 |
|---|------|------|------|------|
| 2.1 | 看 System 二级导航 | 独立「Version」与 Settings 同级 | 导航 = Environment / Settings / Version（三同级，各自路由） | ✅ |
| 2.2 | 点进 Version | 到 Version 页 | `/version` 显示 VERSION `HEAD-7bc126f1` / BRANCH / COMMIT `7bc126f1` / BUILT | ✅ |
| 2.3 | 回 Settings 看是否还含 Version | Settings 内不再有 Version | Settings 正文无 Version 卡片 | ✅ |

证据：`06/07-version-standalone-{light,dark}.png`。

## §5 回归（不破坏既有） — ✅ PASS

| # | 步骤 | 期望 | 实测 | 结论 |
|---|------|------|------|------|
| 5.1 | 依次点 System / Environment / Settings / Version | 均正常、无白屏 | 各页正常渲染（Settings 5 输入 + 唤醒护栏；Environment 正常） | ✅ |
| 5.2 | DevTools Console 报错 | 无报错（先证伪） | 三页 console.error = none（注：首测 settings bodyText=8 系加载瞬态，加长等待后 374 字符正常渲染，已证伪非白屏） | ✅ |
| 5.3 | raw-id 泄漏 sweep | UI 不裸露内部 id | 无 `user-/org-/task-…` 裸 id 泄漏 | ✅ |

证据：`08-environment-regression-light.png`。

## 小观察（非阻塞）

- 保存**成功**无显式 toast 提示（失败有红字校验提示；建议补一个成功反馈，纯 UX，不阻塞验收）。

## 收尾

- turnkey 实例 `agent-center uninstall test-instance --id t224` + `git worktree remove --force ac-wt-t224`。
