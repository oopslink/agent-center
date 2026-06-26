# F-A — 提醒列表补「提醒我的」范围（remindee 维度）

验收 finding **F-A**（`docs/design/v2.11.0/ACCEPTANCE.md`）：列表左 rail 只有 `全部 / 我创建的`（2 项），mockup（`mockups/reminder-mockup-v0.1-I4.png`）是 `全部 / 我创建的 / 提醒我的`（3 项）。

## 修复（T241，分支 `dev/v2110-t241-fa`）
- FE 契约 `ReminderListFilter` 加 `'remindee'`；`web/src/pages/Reminders.tsx` `RANGES` 补「提醒我的」（顺序 全部→我创建的→提醒我的，沿用 `FilterItem` 选中态）。
- BE webconsole 列表端点 `GET /api/orgs/{slug}/reminders?filter=remindee` → `ListReminders{RemindeeAgentID: bareRef(caller)}`（**后端过滤**，走既有 `ListByRemindee`，不前端兜底）。语义 = 提醒「当前查看身份」的（remindee 维度）。
- `web/src/pages/Reminders.test.tsx` 加 `filter=remindee` 断言。

## AFTER 证据（真浏览器 run-real）
- 截图：[`F-A-reminders-remindee-filter-after.png`](./F-A-reminders-remindee-filter-after.png)
- 复现：`make build` → 起本地 center（自建 sqlite + master key，:7355）→ 自助注册沙箱组织 `t241-sandbox` → 导航 `/organizations/t241-sandbox/reminders` → 点「提醒我的」。
- 可见：范围 rail 三项 `全部 / 我创建的 / 提醒我的`，「提醒我的」为选中态（蓝），页头 `提醒 · 提醒我的`，列表按 remindee 维度过滤（沙箱新组织无提醒 → 空列表）。

门：`lint-spa-tsc` / `lint-spa-eslint` / `vitest`(6 绿) / `go build`+`vet` / webconsole api test / `no-raw-colors-spa` / `no-idtail-hash` / `make build` 全绿。
