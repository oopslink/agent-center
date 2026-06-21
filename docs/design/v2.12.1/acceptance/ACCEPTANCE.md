# v2.12.1 — I19 唤醒 brief owner 上下文 验收签字 (T256)

- **Tester**: agent-center-tester1
- **Date**: 2026-06-21
- **Trunk under test**: `origin/dev/v2.12.1` @ `b71a1a34` (off `main`; T254 `fb929fed` + T255 `fac5a329`, PR #328/#330)
- **Deps**: T254 (I19-核心) completed · T255 (I19-服务端) completed
- **Verdict**: **GO** ✅

## 矩阵签字（plan/issue/task/project × {顶层, thread}）

逐格判定：① header 含 `{kind}_id` + 标题 · ② 「这个{kind}=该id」锚定文案且 thread 内仍在 · ③ task/issue chat 无 "This is a conversation, not a task" 误导文案 · ④ reply hint 正确指向 conversation（thread 内带 parent_message_id）。

| owner \ 位置 | 顶层 (top-level) | thread 内 | 判定 |
|---|---|---|---|
| **plan** (基线 T250) | `[Plan chat — "Reminder feature" (plan_id=…)]` + 锚定「this plan」+ 保留 conv-note（字节级不变） | thread hint(parent_message_id) + plan_id 锚定仍在 | ✅ PASS（byte-stable 快照） |
| **issue** | `[Issue chat — "Login broken" (issue_id=issue-7)]` + 锚定「this issue」+ **无误导文案** | thread hint + issue_id 锚定仍在 + **无误导文案** | ✅ PASS |
| **task** | `[Task chat — "Refactor brief" (task_id=task-9)]` + 锚定「this task」+ **无误导文案** | thread hint + task_id 锚定仍在 + **无误导文案** | ✅ PASS |
| **project** | — | — | **N/A**（OQ1：无 converse 唤醒路径，见下） |

附加格：
- **task / name-miss**：env 标题解析失败 → `[Task chat (task_id=task-x)]` 纯 id 兜底，锚定仍在，**不阻断唤醒**。✅
- **DM / channel（回归）**：无 owner framing，保留「This is a conversation, not a task」，字节级不变。✅

### project 格 N/A 说明（OQ1，独立复核）
project chat 当前**没有 converse 唤醒路径**，故 brief 永不会以 project owner_ref 渲染 → N/A：
- 无 `ConversationKindProject`（`internal/conversation/types.go` 仅 dm/channel/task/issue/plan）；
- `NewProjectOwnerRef` 在**非测试代码零调用**（`grep pm://projects/ --include=*.go` 仅定义 + channel 软标签 `projectRef`）；
- `pm://projects/` 仅作 channel 的可选软标签，不是 owner_ref，channel brief 走 `[Channel #name]` 框架。
- 前向兼容：解析表已登记 `projects` scheme（Anchored=true），`wake_projector.resolveOwnerName` 对 project 返回 false + TODO；若将来 project chat 获得唤醒路径，brief 会自动正确渲染（已被 dump 验证）。

## 红线 / 关键证据（AFTER）
| # | 验证项 | 结果 | 证据 |
|---|---|---|---|
| 1 | 8+格 brief 文案逐格渲染（byte-exact） | PASS | `evidence/01-brief-matrix-dump.txt` |
| 2 | **真实 OS 子进程** stdin 收到 brief（stream-json user 信封；issue 顶层 + task thread） | PASS | `evidence/02-real-process-stdin-capture.txt` |
| 3 | 单测 + 集成测（conversation/workerdaemon/environment.service 全绿） | PASS | `evidence/03-unit-integration-tests.txt` |
| 4 | `make lint`（go vet + tsc -b + eslint + 7 lint 脚本）RC=0 | PASS | `evidence/04-make-lint.txt` |
| 5 | **deployed-smoke** 真实二进制 + 全 app 组装（含 I19 outboxProjectors 接线）`smoke pass: 17s` | PASS | `evidence/05-deploy-smoke.txt` |
| 6 | **OQ4 护栏非空跑**：删 `pm://tasks/` 登记 → 测试 RED，补回 → green | PASS | `evidence/06-oq4-guard-mutation.txt` |

## 方法说明（透明）
- I19 的产物是注入 agent stdin 的 **brief 文本**（非 UI 面），故证据为「brief 文本 + 真实子进程 stdin 抓取 + 日志」，无 Console 截图（该 artifact 无 UI 呈现）。
- 链路逐缝覆盖：`ResolveOwnerContext`(表) → `wake_projector.deliverConverse`(真 repo 集成测，live 标题覆盖 stale conv 名 + owner_ref 透传) → `AgentController.converse`→`sess.Inject(buildConverseBrief)` → `buildConverseBrief`(byte-exact) → **真实子进程 stdin**(本验收新增抓取) → `webconsole_wiring` live 标题 resolver（真 IssueRepo/TaskRepo）→ deployed-smoke 真二进制启动组装。
- 临时 probe（dump 矩阵 + 真子进程抓取）已删除，输出冻结于 `evidence/`。

## 签字
agent-center-tester1 — **GO**。交 PD（agent-center-pd）收口 ship trunk `dev/v2.12.1` → main。
