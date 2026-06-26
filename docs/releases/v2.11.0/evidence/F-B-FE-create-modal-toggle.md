# F-B（FE）— 新建弹窗「以本人身份创建提醒文本」开关

验收 finding **F-B**（`docs/design/v2.11.0/ACCEPTANCE.md`）：新建弹窗缺「以本人身份创建提醒文本」开关（mockup 底部蓝色 toggle）。BE 契约字段由 **T242**（`deliver_as_creator`，默认 ON）落地；本任务 **T243** 接 FE。

## 修复（分支 `dev/v2110-t243-fb`）
- `web/src/components/ReminderCreateModal.tsx`：内容下方加蓝色 toggle（`role="switch"`，周期/一次性两态共用，默认 ON，带说明文案「到点的提醒消息以创建者身份发出（关闭则以系统身份）」）。on=`bg-brand` / off=`bg-border-strong`，均为 theme token，dark 安全。
- `web/src/api/reminders.ts`：`CreateReminderInput` + `Reminder` 加 `deliver_as_creator`；create payload 带上。
- `web/src/components/ReminderCreateModal.test.tsx`（新增 3 例：渲染默认 ON / 提交 `deliver_as_creator=true` / 切换后提交 `false`）；`Reminders.test.tsx` fixture 补字段（tsc 真门要求）。

## AFTER 证据（真浏览器 run-real，both-mode）
- Light：[`F-B-FE-create-modal-toggle-light.png`](./F-B-FE-create-modal-toggle-light.png)
- Dark：[`F-B-FE-create-modal-toggle-dark.png`](./F-B-FE-create-modal-toggle-dark.png)
- 复现：`make build` → 起本地 center（:7356，自建 sqlite + master key）→ 自助注册沙箱 `t243-sandbox` → `/reminders` → 新建提醒 → 弹窗底部可见蓝色 toggle「以本人身份创建提醒文本」（默认 ON）+ 说明。两张分别为 `ac.theme=light` / `dark`。（截图用一次性态以便 toggle 完整入框；周期态同样渲染该 toggle。）

门：`lint-spa-tsc` / `lint-spa-eslint` / `no-raw-colors-spa` / `vitest`（9 绿）/ `make build` 全绿。
