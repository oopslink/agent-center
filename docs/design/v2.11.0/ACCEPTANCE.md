# v2.11.0 验收清单 — Agent 提醒 / 定时（Reminder · I4）

- **验收目标**：`dev/remainder` @ `72d46403`（含 T205/T206/T207 + T229 + T239/F1 + T240/F2）
- **执行**：PD（agent-center-pd），owner 指派「执行验收」（2026-06-20）
- **方法学**：`docs/rules/acceptance-methodology.md`（§4 verify-in-tree、§8 构建门、§11 release flow 铁律）
- **总体裁定**：**CONDITIONAL（条件通过）** — 后端/数据/API/红线/构建门全绿；UI 对 mockup 有 **2 处实质偏离**待 fix-or-waive；真浏览器 UI run-real 截图 + deployed-smoke 待补，方可终判 GO。

> 判定铁律（§11-3）：本报告**不以节点 `completed` 推断 PASS**；每条结论附 in-tree 证据（`evidence/`）或代码坐标。

---

## 逐任务 verdict（验收项数 = 任务数）

| 任务 | 模块 | Verdict | 证据 |
|---|---|---|---|
| **T205** 提醒-1 | 领域 + 持久化 + 调度（Cognition BC） | ✅ PASS | `evidence/go-tests-reminder.txt`：`reminder`/`reminder/sqlite` 全绿（聚合不变式、CAS、FindDue、时区 NextRunAt） |
| **T206** 提醒-2 | Reminder MCP 工具 + admin API + 护栏 | ✅ PASS | `internal/admin/api` 测试绿；403 映射 `cross_project_reminder`/`reminder_forbidden`（`agent_tools_reminders.go:104-107`） |
| **T207** 提醒-3 | 管理 UI（对 mockup 1:1） | ⚠️ **CONDITIONAL** | 见下「mockup 对照 findings」F-A / F-B / F-C |
| **T229** fix | 自我提醒不再误判跨 project | ✅ PASS | 已集成进目标（`merge-base` 实证）；`TestNewReminder_CrossProjectGuard` / `TestApp_Create_CrossProjectGuard`（owner 豁免）绿 |
| **T239** F1 | fired 事件入 outbox → remindee 真投递/唤醒 | ✅ PASS | `TestScheduler_FiredEvent_LandsInOutbox` / `TestDeliveryProjector_FiredEvent_Delivers` 绿 |
| **T240** F2 | outcome 反映真实投递 + skip_if_overlap 真在飞行态 | ✅ PASS | `TestScheduler_OverlapLifecycle_PendingThenDelivered` / `TestScheduler_SkipIfOverlap_PrevPending_Skips` / `TestRecordSkip_*` 绿 |

---

## 授权红线（PD 必核） — 全过 ✅

| 红线 | 结论 | 证据 |
|---|---|---|
| 同 project 护栏：跨 project 设提醒被拒 | ✅ | `TestNewReminder_CrossProjectGuard` / `TestApp_Create_CrossProjectGuard` → `ErrCrossProjectReminder` → HTTP 403 |
| owner 豁免跨 project 守卫 | ✅ | 同上用例 `CreatorIsOwner` 分支放行 |
| class-guard：非创建者/非 owner 改他人提醒被拒 | ✅ | `TestApp_Update_Authz`（陌生人 pause → `ErrReminderForbidden`） |
| 可见性：owner 看全部、非 owner 只看自己创建 | ✅ | `TestApp_ListOrgReminders_OwnerSeesAll_NonOwnerOwnOnly` |
| 自我提醒不被误判跨 project（I4 核心用例） | ✅ | T229 已集成，跨 project 守卫用例绿 |

---

## §8 构建发布门 — ✅ 绿

- `make lint`（go vet + no-vendor/mock/doc-drift/raw-colors/idtail-hash + `tsc -b` + eslint）→ **exit 0**
- `make build`（`vite build` + `go build`，Reminders bundle 编译入包 `Reminders-CY_js-Oj.js`）→ **exit 0**
- 证据：`evidence/build-gate-make-lint-build.log`

---

## mockup 对照 findings（screen ①②③ vs `mockups/reminder-mockup-v0.1-I4.png`）

**对齐良好**：列表页 3 统计卡 + 7 列表格（对象/触发/下次触发/内容/创建者/状态/操作）+ 周期·一次性·状态徽章 + 行内 暂停/恢复/取消 + 行→详情+历史；新建弹窗 触发方式切换 + cron 表达式 + 5 常用预设 + 人话预览 + 时区 + 一次性 日期/时间 + 预览 + 提醒内容。

| # | 偏离 | 严重度 | 代码坐标 | 处置 |
|---|---|---|---|---|
| **F-A** | 左 rail 少「提醒我的」范围（mockup 是 全部/我创建的/提醒我的 3 项） | 中 | `web/src/pages/Reminders.tsx` `RANGES` 仅 all/created；`web/src/api/reminders.ts` `ReminderListFilter='all'\|'created'` | **待 owner**：fix（补 remindee 维度，前后端）or waive |
| **F-B** | 新建弹窗缺「以本人身份创建提醒文本」开关（mockup 周期态底部蓝色 toggle） | 中 | `web/src/components/ReminderCreateModal.tsx` 无该控件；后端契约亦无对应字段 | **待 owner**：fix or waive |
| **F-C** | 实现多出「高级」区（跳过重叠 + 结束条件），mockup 未画 | 低（增强） | `ReminderCreateModal.tsx` 高级块 | 建议**保留**（后端已支持，T240 测试覆盖）；待 owner 确认 |

---

## 尚缺（终判 GO 前必补）

- [ ] **F-A / F-B 的 fix-or-waive 裁决**（owner）；若 fix → 开 FE 任务修到 1:1 后复签。
- [ ] **真浏览器 UI run-real 截图**（§4.3：用户视角端到端 + 关键步骤截图，内嵌报告、可复现 capture 脚本）—— 当前 UI 证据为代码级，未出浏览器实拍。
- [ ] **deployed-smoke ≥ 1**（§8 硬发布门，含跨版本升级真跑）。
- [ ] mockup 已入仓（`mockups/reminder-mockup-v0.1-I4.png`）✅ 本次补齐。

---

## 已通过项小结

后端领域/持久化/调度/投递、MCP+admin API、授权红线、自我提醒、防风暴（skip-overlap/真投递 outcome）、构建硬门 —— 全部 **GREEN（带 in-tree 证据）**。阻挡终判 GO 的是 **UI 对 mockup 的 2 处偏离 + 真浏览器证据/smoke 待补**，均不涉及后端正确性。
