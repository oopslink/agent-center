# T218 (I7-D3) — System/Settings 唤醒护栏参数配置面板 + Version 提级

## part2「Version 提为 System 级导航」（已在 dev/i7）
`dev/t218-version-promote` 已并 dev/i7：System 二级导航 Environment / Settings / **Version**（Version 独立页，从 Settings 移出）。见下方截图左栏 col②。

## part1「护栏参数配置面板」（本次，接 I7-M1）
Settings 页新增「唤醒护栏」面板，走 `GET/PUT /api/system/wake-guardrail`（I7-M1/T216 的 live center settings）：查看 + 编辑 + 保存 5 项阈值，带默认值 + 校验（全 >0）+ 范围说明；保存即生效（WakeGuard 每次评估读 store，无需重启）。

## run-real（验收：改值→后端读到新值）
- 真服务：`make build` 起本地 center（:7357，自建 sqlite + master key）→ 注册沙箱 `t218-sandbox` → System → Settings。
- 改 `最大唤醒链深度` 4→**7**、`环检测窗口` 300→**600** → 保存 → UI 显示「已保存并生效」。
- 后端 `GET /api/system/wake-guardrail` 返回 `{"max_depth":7,"cycle_window_sec":600,...}` ✅；刷新页面 UI 仍读到 7/600（持久化 + 全链路 roundtrip）。
- AFTER 截图：[`T218-wake-guardrail-saved.png`](./T218-wake-guardrail-saved.png)（改 7/600 + 已保存并生效 + 左栏 Version 同级）、[`T218-wake-guardrail-after-reload.png`](./T218-wake-guardrail-after-reload.png)（刷新后仍 7/600，持久化 + 全链路 roundtrip）。基线默认值 4/300 即面板默认态，无需单独 before 图。

## 顺带修复的 I7-M1(T216) 生产接线 bug（run-real 抓到）
保存初次 501 `settings store not configured`。根因：**生产 webconsole 路径 `runWebConsole`（`internal/cli/webconsole_wiring.go`）构建 `HandlerDeps` 时漏接 `SettingsStore`**；T216 只在 `buildWebConsoleHandler`（**test-only**）里接了 → wiring 单测绿、真服务 PUT 501。修复：`runWebConsole` deps 补 `SettingsStore: settingssql.NewStore(a.DB, a.Clock)`。已同步 dev1/PD。

## 门
`lint-spa-tsc` / `lint-spa-eslint` / `no-raw-colors-spa` / `make build` / `go vet` 绿；新增 `WakeGuardrailPanel.test.tsx`(4) + Settings/client 相关 vitest 绿。
（注：full vitest 有 1 个 pre-existing 失败 `a11y no-emoji-icons`，源于 v2.11.0 提醒 UI emoji，非本任务引入，另报 PD。）
