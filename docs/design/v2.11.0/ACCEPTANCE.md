# v2.11.0 验收清单 — Agent 提醒 / 定时（Reminder · I4）

- **验收目标**：`dev/remainder` @ `a8402e36`（含 T205/T206/T207 + T229 + T239/F1 + T240/F2 + **T241/F-A + T242·T243/F-B 修复**）
- **执行**：PD（agent-center-pd），owner 指派「执行验收」（2026-06-20）；F-A/F-B 修复后复验同日。
- **方法学**：`docs/rules/acceptance-methodology.md`（§4 verify-in-tree、§8 构建门、§11 release flow 铁律）
- **总体裁定**：**GO（功能验收通过）** — 逐任务 + 授权红线 + 构建门 + 全测全绿且证据 in-tree；mockup 2 处偏离已 fix 并复验 1:1（含 both-mode）；F-C 按 owner 保留。**唯一剩余硬门**：deployed-smoke ≥1（§8，ship 时跑）+ owner 同意验收（§11-4）。

> 判定铁律（§11-3）：本报告**不以节点 `completed` 推断 PASS**；每条结论附 in-tree 证据（`evidence/`）或代码坐标。

---

## 逐任务 verdict（验收项数 = 任务数）

| 任务 | 模块 | Verdict | 证据 |
|---|---|---|---|
| **T205** 提醒-1 | 领域 + 持久化 + 调度（Cognition BC） | ✅ PASS | `evidence/go-tests-reminder.txt`：`reminder`/`reminder/sqlite` 全绿（聚合不变式、CAS、FindDue、时区 NextRunAt） |
| **T206** 提醒-2 | Reminder MCP 工具 + admin API + 护栏 | ✅ PASS | `internal/admin/api` 测试绿；403 映射 `cross_project_reminder`/`reminder_forbidden`（`agent_tools_reminders.go:104-107`） |
| **T207** 提醒-3 | 管理 UI（对 mockup 1:1） | ✅ **PASS（修复后）** | F-A/F-B 已修并复验 1:1，见下「mockup 对照 findings」 |
| **T229** fix | 自我提醒不再误判跨 project | ✅ PASS | 已集成进目标（`merge-base` 实证）；`TestNewReminder_CrossProjectGuard` / `TestApp_Create_CrossProjectGuard`（owner 豁免）绿 |
| **T239** F1 | fired 事件入 outbox → remindee 真投递/唤醒 | ✅ PASS | `TestScheduler_FiredEvent_LandsInOutbox` / `TestDeliveryProjector_FiredEvent_Delivers` 绿 |
| **T240** F2 | outcome 反映真实投递 + skip_if_overlap 真在飞行态 | ✅ PASS | `TestScheduler_OverlapLifecycle_PendingThenDelivered` / `TestScheduler_SkipIfOverlap_PrevPending_Skips` / `TestRecordSkip_*` 绿 |
| **T241** F-A 修复 | 列表补「提醒我的」scope | ✅ PASS | `Reminders.tsx` RANGES 含 `remindee`；`ReminderListFilter` 加 `remindee`；`TestApp_List_Filter`/`TestRepo_ListFilters` 绿；AFTER `evidence/F-A-reminders-remindee-filter-after.png`（真浏览器，3-scope rail，选中「提醒我的」） |
| **T242·T243** F-B 修复 | 「以本人身份」字段 + 投递身份 + 弹窗开关 | ✅ PASS | BE `deliver_as_creator`（domain+repo+API），`TestRepo_DeliverAsCreator_RoundTrip` 绿；FE 弹窗蓝色 toggle 默认 ON；AFTER `evidence/F-B-FE-create-modal-toggle-{light,dark}.png`（both-mode） |

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

| # | 偏离 | owner 裁决 | 处置结果 | 复验证据 |
|---|---|---|---|---|
| **F-A** | 左 rail 少「提醒我的」范围 | **fix** | ✅ 已修（T241）：`RANGES` + `ReminderListFilter` 加 `remindee`，端点后端过滤 | AFTER `evidence/F-A-reminders-remindee-filter-after.png`（真浏览器 3-scope）+ `TestRepo_ListFilters` 绿 |
| **F-B** | 新建弹窗缺「以本人身份创建提醒文本」开关 | **fix** | ✅ 已修（T242 BE `deliver_as_creator` 字段+投递身份 / T243 FE 蓝色 toggle 默认 ON） | AFTER `evidence/F-B-FE-create-modal-toggle-{light,dark}.png` + `TestRepo_DeliverAsCreator_RoundTrip` 绿 |
| **F-C** | 实现多出「高级」区（跳过重叠 + 结束条件） | **保留** | ✅ 保留不动（后端已支持，T240 覆盖） | — |

---

## 尚缺（ship 前）

- [x] **F-A / F-B fix + 复验 1:1（含 both-mode）** — 完成（T241/T242/T243，AFTER 证据 in-tree）。
- [x] **F-A/F-B UI run-real 截图** — devs 真浏览器 AFTER 截图已 commit `evidence/`（导航真实可达、both-mode）。
- [ ] **deployed-smoke ≥ 1**（§8 硬发布门）—— PD 在 ship 阶段跑。
- [ ] **owner 同意验收**（§11-4）。
- [x] mockup 已入仓（`mockups/reminder-mockup-v0.1-I4.png`）。

---

## 小结（GO）

逐任务 + 授权红线 + §8 构建门 + 全测 —— 全 **GREEN（in-tree 证据）**；mockup 2 处偏离按 owner 裁 **fix 并复验 1:1**，F-C 保留。功能验收 **GO**。剩 deployed-smoke（ship 时跑）+ owner 同意 即可合 `dev/remainder → main` + tag `v2.11.0`。
