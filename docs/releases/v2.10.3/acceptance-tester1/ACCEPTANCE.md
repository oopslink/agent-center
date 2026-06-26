# v2.10.3 — [Tester1 验收] §2 data/API + 授权硬门（T170 issue 工具 + T167 文件域）

**结论：GO ✅**

- 范围：T170（agent issue 全生命周期 MCP 工具）+ T167（Plan chat 附件文件域）
- 候选分支/commit：`origin/v2.10.3-int` @ **`80794195`**（基于 origin/main；T170 merge 80794195 ← T167 merge 45f7dac7 ← v2.10.2 b4daf2c2）
- 方法学：`docs/rules/acceptance-methodology.md` §2 —— **不靠读代码 parity，按真实调用跑**：每条经 `writeToolsFixture` 起真 admin server（完整 `AuthMiddleware` + pm→outbox→projector 全管线），真 HTTP 调用取真状态码/真响应体。
- 证据目录：`evidence/`
  - `01_t170_issue_tests_PASS.png` … `04_full_packages_PASS.png`：关键 test PASS 终端截图
  - `05_live_shapes_issue.png` / `06_live_shapes_files.png`：真 endpoint 响应 shape + 授权 403/200 边界 live 实测（probe 取证后已删，冻结树不留临时测试）
  - `07_git_ls_tree_proof.png` + `git_ls_tree_proof.txt`：feature 文件存在于被 tag 的 commit 80794195 的 `git ls-tree` 实证
  - `*.txt`：完整可复现的终端 transcript

---

## T170 — agent issue 全生命周期工具

### 1. 逐工具真调（全 200）
证据：`05_live_shapes_issue.png`（每条均真 HTTP 调用 + 真响应体）

| 工具 | endpoint | live 结果 |
|---|---|---|
| create_issue | `POST /admin/agent-tools/create_issue` | 200 → `{issue_id}`；`created_by=agent:AG1`、tags 落库、status=open |
| get_issue | `POST /admin/agent-tools/get_issue` | 200 → 完整 shape（含 `tags` 数组，T170 新增）|
| update_issue | `POST /admin/agent-tools/update_issue` | 200 `{ok:true}`；dirty-only patch（仅传字段变，余者不动 — `PartialPatch_LeavesOthers`）|
| close_issue / reopen_issue | `.../close_issue` `.../reopen_issue` | 200 `{ok,status:closed}` / `{status:open}` |
| post_issue_message（@mention + 线程）| `.../post_issue_message` | 200 `{message_id}`；顶层 + `parent_message_id` 线程回复均成功，@mention 作纯 content 透传 |
| list_issues（按状态/作者过滤）| `.../list_issues` | 200；`status=in_progress` 过滤命中 1、`author=user:owner` 过滤命中 1、无过滤命中全部 |
| get_issue | （见上）| — |
| list_tasks_of_issue（issue→task 派生反查）| `.../list_tasks_of_issue` | 200 → 仅返回 `derived_from_issue==issue_id` 的 task（不含无关 task）|

### 2. 授权红线（独立复核，非只读测试 — live HTTP 码）
证据：`05_live_shapes_issue.png` 下半部 RED 段

| 红线 | live 结果 |
|---|---|
| 非成员/外项目 create_issue | **HTTP 403** `not_a_project_member`（不误报 404）|
| 非成员 get_issue（外项目 issue）| **HTTP 403**，不泄露 issue 内容 |
| 非成员 post_issue_message | **HTTP 403** |
| 非成员 list_tasks_of_issue | **HTTP 403** |
| cross-worker get_issue | **403**（单测 `TestGetIssue_CrossWorker_403` PASS）|

**get_issue 放宽边界两侧均验**（task 第 2 条核心）：
- `TestGetIssue_MemberNoDerivedTask_OK` → **成员即使没有 derive task 也能读**（200，完整 shape）✅
- `TestGetIssue_NonMember_403` → 非成员读不到（403）✅
- 服务层 `GetIssueForMember`（`internal/projectmanager/service/reads.go:42`）：先 `FindByID`（缺失→`ErrIssueNotFound`→404）再 `requireProjectMember`（非成员→`ErrNotMember`→403）。旧 OQ4「持有派生 task 的 WorkItem」严格自有作用域已被 owner-approved 放宽为 project-member 读。

### 3. Console↔agent 同一份数据
证据：`05_live_shapes_issue.png` 第 2~3 块（agent get_issue 响应 vs 同一 issue 从共享 pm store 读出）
- agent `create_issue` 建的 issue（`issue-9ba63100`），经 agent `get_issue` 与 Web Console 服务来源（`PMService.GetIssue` 同一 store）读出 **id/project_id/title/description/status/created_by/tags/version 逐字段一致**。
- 代码口径：`agentIssueMap`（agent 侧，`agent_tools_passthrough.go`）⇄ `pmIssueMap`（console 侧，`webconsole/api/handlers_pm.go:78`）投影同一 `pm.Issue` AR 的同名字段；console 另带 `org_ref`/`status_changed_at`。单一事实源，无双写分叉。

### 4. §2 验链路 + 扫整类（端到端串）
- issue → `create_task(DerivedFromIssue)` → `list_tasks_of_issue` 反查命中派生 task（`05_live_shapes_issue.png`：`derived_from_issue=issue-9ba63100`，total=1，不含无关 task）。
- 每个可达消费点：create/update/close/reopen/comment(+thread)/list(status,author)/get/list_tasks_of_issue 全部 enumerate 并 live 命中。
- route ⇄ MCP ⇄ admin 一致性：`TestAgentFacingToolParity` + `TestAgentFacingTool_HasAdminRoute` PASS（`03_parity_guards_PASS.png`），7 个新 issue 工具 admin 路由全注册。

---

## T167 — Plan chat 附件文件域

证据：`02_t167_file_domain_PASS.png` + `06_live_shapes_files.png`

按**会话成员资格**判定（复用 v2.9.2 T44 class-guard 模型，`agentParticipantConvScopes` 新增 `ConversationKindPlan` 枚举）：

| 场景 | live 结果 |
|---|---|
| 参与者 plan 会话 upload_file(scope=conversation) | **200** → `{file_uri,transfer_id,...}`；完整 upload→PUT→complete→download 往返，下载字节 match=true |
| post_message + attachment 进 plan 会话 | **200** `{message_id}`；附件元数据落库、conversation live ref 恰 1（无重复）|
| **非参与者** plan 会话 upload | **HTTP 403** `scope_not_in_agent_domain`（fail-closed）|
| **cross-org** plan 会话 upload | **HTTP 403** `scope_not_in_agent_domain`（org-scoped 枚举永不可见）|

关键点：plan 参与关系**独立于 work-item**（PD 开自己的 plan chat 不持有该 plan 内 task 的 work-item），故作用域必须来自参与者枚举而非 work-item 派生 —— 修复正是补上这一枚举。task/issue 会话仍走 work-item 派生，故此处刻意不枚举。

---

## 测试层全绿（无回归）
证据：`01_t170_issue_tests_PASS.png`（19 issue 测试）· `02_t167_file_domain_PASS.png`（4 文件域测试）· `04_full_packages_PASS.png`

```
ok  internal/admin/api              (含 T170 19 测 + T167 4 测 + parity)
ok  internal/mcphost               (parity guards)
ok  internal/projectmanager
ok  internal/projectmanager/service (GetIssueForMember / ListTasksDerivedFromIssueForMember 等)
ok  internal/projectmanager/sqlite
```

## 观察（非阻塞）
- 非成员 get/post/list_tasks_of_issue 的 403 响应体 `error` 码为通用 `"terminal"`（消息 "actor is not a member of this project"），而 foreign create_issue 给出更具体的 `not_a_project_member`。安全属性（403 + 不泄露存在性/内容）成立，仅错误码粒度不一致 —— 留作后续可选打磨，不影响本轮 GO。

## 备注
- run-real probe 为临时取证测试（`zz_v2103_probe_test.go`），取得 live shape/红线码后**已删除**，候选冻结树不含任何临时测试改动（`git status` 干净，仅本验收 docs 落盘）。
