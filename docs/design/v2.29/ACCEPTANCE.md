# 变更记录 / 审计模块 —— 全真验收清单 (A–I)

> 特性：通用变更记录 / 审计账本 (change-log ledger)，设计 [`docs/design/features/2026-07-03-change-log-audit-design.md`](../features/2026-07-03-change-log-audit-design.md)
> 分支：`feat/audit-log-impl` @ `7e5be90f` · 迁移 `0099_v229_pm_audit_log`
> 验收方式：全真隔离测试实例（`agent-center install test-instance --with-seed`，真 launchd center + worker + 真 install config），真浏览器 + 真 web API 端到端。**非 parity、非跑单测充数**。
> 隔离实例：`~/.agent-center-test/audit1/`（web :57211，与 prod `~/.agent-center` 进程+数据隔离）。
> 验收人：tester3（Tester 出口标准 + 独立设计，Dev 零参与）。日期：2026-07-05。

验收项数 = 特性交付面（迁移 / 后端账本 / 读 API / 三详情页前端 / 系统-actor 归属 / 零回归），逐项留【截图证据】。**无截图 = 该项不通过。**

| 项 | 验收对象 | 出口标准 | 证据 | 结论 |
|---|---|---|---|---|
| **A** | 迁移 / schema | `pm_audit_log` 建表（11 列、`DEFAULT ''`/`'{}'`）、两查询索引、FK-free、head schema 98→99、空表零基线 | `evidence/A-schema.png` | ✅ PASS |
| **B** | Task 状态迁移账本 | `created` + `status_changed` 全入口（override/block/unblock/complete/reopen/batch），`from/to`+`actor` 真值正确 | `evidence/BC-task-ledger.png` | ✅ PASS |
| **C** | Task 归属账本 | `assigned`/`reassigned`/`unassigned`，`from/to`+`actor` 正确 | `evidence/BC-task-ledger.png` | ✅ PASS |
| **D** | Issue 账本 | `created`/`status_changed`(transition+set)/`metadata_edited`（记 `fields`，不存全文 diff） | `evidence/D-issue-ledger.png` | ✅ PASS |
| **E** | Plan 账本 | `created`/`node_added`/`dependency_added`/`dependency_removed`/`started`/`stopped`，`detail` 结构化 | `evidence/E-plan-ledger.png` | ⚠️ PASS w/ 低危 finding E-1 |
| **F** | 系统/gate 驱动 change_types + actor 归属 | 6 类系统/gate change_type 均有写入点且 actor=`system:<reconciler>` | `evidence/F-system-actor.png` | ❌ **FAIL — finding F-1** |
| **G** | 读 API | `GET .../{issues\|tasks\|plans}/{id}/audit`：游标分页 + `limit` 校验(400) + 成员鉴权(401) + 结构化 DTO | `evidence/G-readapi.png` | ✅ PASS |
| **H** | 三详情页时间线 | Task/Issue 侧边栏「变更记录」段 + Plan「变更记录」tab，人话渲染 + 彩点彩标签 + i18n + light/dark | `evidence/H-*.png` (7 张) | ❌ **FAIL — finding H-1**（渲染+色彩 PASS，i18n FAIL） |
| **I** | 零回归 + best-effort 不阻塞 | 纯加法（空表零影响）；审计写 SAVEPOINT 隔离，失败不回滚主 mutation | `evidence/I-zero-regression.png` | ✅ PASS |

## 总体结论：**RESOLVE FAILURE（退回开发）**

核心账本（A–D、G、I）端到端真跑全绿；但 **F、H 各有 1 条 confirmed finding**，其中 **F-1 为设计 §5 承诺的写入点缺失**（plan `decision_outcome`/`loopback` 完全未记账），命中特性 §1 动机的核心用例。按验收方法论「发现问题 → resolve failure 退回开发」，**不放行 Ship**。

## Findings（详见 acceptance report）
- **F-1（中-高）**：plan `decision_outcome` / `loopback` 审计写入点缺失 —— 枚举与前端渲染都在，但无任何代码 append 这两类审计行；plan 的 gate 结果 + loopback 重开**不进账本**。
- **H-1（中）**：`ObjectAuditTimeline` 组件零 i18n —— 段标题、change-type 标签、人话文案全为硬编码英文；在产品默认中文 UI 下整段渲染英文（Plan tab **标签**已本地化，内容未）。
- **E-1（低）**：`dependency_removed` 的 `detail.from/to` 为空 —— 删除的依赖边端点未记录，UI 显示 "removed dependency →"（空）。
