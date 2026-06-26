# agent-center 全流程 E2E 测试发现清单 (2026-06-14)

> 测试者：Claude Code agent。环境：本机 Linux (aarch64) 独立实例
> `~/.agent-center.e2e`（center web :7180 / server :7150 / admin TLS :7380），
> 真实 worker `worker-78bcd874`(linux-box-1) + 真实 claude-code agents。
> 构建：`main-edeb5ea`。

测试方式：用户面 HTTP API（curl + 会话 cookie）+ Web Console UI（Playwright）
双轨驱动，覆盖：建 org → 建 project → 加真实机器(worker enroll) → 加 agent →
下发任务 → 多 agent @mention 通讯 → 各页面 UX。

每条发现：复现步骤 / 期望 vs 实际 / 严重度(P0 阻断 / P1 功能错 / P2 体验) / 状态。

---

## 已验证可用（happy path PASS）

- ✅ 工具链构建：`make build` 产出嵌入 SPA 的单二进制（修了 pnpm 代理 + CI=true 坑）。
- ✅ `install center` 前台安装 + `agent-center server` 启动；Web Console / health / version API 正常。
- ✅ signup → org → project 创建（会话 cookie 鉴权）。
- ✅ `mint-enroll` → `install worker` → `worker run`（带 proxy）→ worker 上线，
  Fleet 显示 online + 检测到 claude-code 2.1.177 / codex 能力。
- ✅ 创建 claude-code agent → start → lifecycle=running/available，computer connected。
- ✅ 下发任务 → 分配给 agent → **真实 claude 执行 pwd/date** → 会话回帖 →
  task.status=completed, completed_by=agent。**端到端打通。**

---

## 发现清单

### F-1 [P2] agent 完成任务时发出两条近重复的"完成"消息
- 复现：建任务"Report current date and working directory" → 分配给 Alice(claude-code)。
- 实际：Alice 在任务会话连发两条（间隔 4s）：
  1. "Done. Exact output: `pwd`: …/workspace `date`: Sun Jun 14 13:40:13 CST 2026"
  2. "Ran `pwd` and `date`. pwd: …/workspace ; date: …"
- 期望：单条完成汇报。
- 备注：疑为 agent pull-loop 的 progress + final 双发，或 LLM 自身多发。需确认是
  平台双发还是 claude 行为。**待定根因**。

### F-2 [非问题] messages 端点不返回 sender_display_name
- 复现：`GET /api/orgs/{slug}/conversations/{id}/messages` 每条消息只有
  `sender_identity_id`，无 `sender_display_name`。
- 验证结论：Playwright 走查显示 channel UI **正确渲染了 "Alice"/"Bob" 名字**（前端从
  participants 列表解析 identity→名字）。**用户不可见，非 bug，关闭。**

### F-3 [P1 ★主因 ✅已修已验] agent 不知道自己的身份（display name），多 agent 场景相互混淆
- 复现：建两个 claude-code agent（Alice / Bob，同 worker），建 channel 含两人，
  人类发 "@Alice please send a message to @Bob asking the date…"（文本里同时含
  @Alice 和 @Bob，故两人都被这条**人类**消息唤醒）。
- 实际：**Bob 在自己的活动轨迹里写 "I've confirmed my role: I'm Alice"**，随后
  "以 Alice 身份给 Bob 发问题"，并 `sleep 30` 等"Bob"回答。Bob 全程自认 Alice，
  从未正确回答日期。多 agent 协作彻底跑偏。
- 期望：每个 agent 明确知道自己是谁，不会冒认另一个 agent。
- 根因（已定位代码）：
  1. `internal/claudestream/agent_system_prompt.go` 的 `AgentWorkQueueSystemPrompt`
     **完全不含 agent 的名字/身份**；`BuildStreamingArgv`(argv.go) 仅注入该 const +
     `--session-id <agentID>`（不透明 ULID，非名字）。
  2. `get_my_profile`(admin/api/agent_tools_profile.go) 返回 org/projects/capabilities，
     **唯独不返回 agent 自己的 display_name 和 identity ref** → agent 即便主动查也
     learn 不到"我是谁"。
  3. agent 是长生命周期（`--session-id` 持久化会话），一旦误判身份会跨多次唤醒持续错。
- 已落地修复（取低风险、根因对齐方案；避开 7-参 `BuildStreamingArgv` 全调用点 churn）：
  1. `get_my_profile`(admin/api/agent_tools_profile.go) 现返回 `display_name` +
     `agent_ref`（"agent:<member-id>"）——agent 能查到"我是谁"。
  2. 工具描述(mcphost/server.go) 强化："调它确认 WHO YOU ARE；多 agent 共处一会话时
     绝不因消息里出现某名字就冒认那个 agent"。
  3. system prompt(claudestream/agent_system_prompt.go) 顶部加 **"== Who you are =="**
     段：启动即 get_my_profile 确认身份；仅当 @自己 display_name 才是 directed at you；
     @别人=对方；绝不冒认他名 agent / 不 @自己。
  4. class-guard：`TestGetMyProfile_Populated` 加 display_name/agent_ref 断言。
- 验收（决定性）：重部署后建**全新** agent Carol/Dave（新 prompt + 无污染历史），
  同一"两人都被 @、各自报名"的场景：
  - Carol → "I am Carol (agent:agent-fbbd9ba6)." ✓
  - Dave  → "I am Dave (agent:agent-1f65dca0)." ✓
  各报各名、agent_ref 精确、零冒充（对比修复前 Bob 自认 Alice）。截图
  `/tmp/e2e-verify/B-identity-check-channel.png`。**闭环。**
  （注：旧 Alice/Bob 的 claude 会话已把"我是 Alice"写进持久历史，故用新 agent 验证；
   存量被污染会话需 reset 才能纠正——属会话历史性质，非代码缺陷。）

### F-5 [P2 ✅已修已验] agents 列表"最后活动"渲染原始 stream-json
- 复现：agent 跑过任务后看 Agents 页 LAST ACTIVITY 列 → 显示
  `Jun 14, 2026, 1:53 PM ... {"raw":{"type":"system","subtype":"commands_changed",...`
  （原始 JSON）。
- 根因：`activityPreviewText`(webconsole/api/handlers_agent.go) 找不到顶层
  `text/result/tool_name/event` 时**回退 `return payload`**（整段 raw JSON）；
  `system`/`commands_changed` 事件顶层只有 `{type,subtype,raw}` → 原始 JSON 泄露到 UI。
- 修复：最终回退改为 `return ""`（永不返回原始 payload）；非 JSON 也返回 ""。
  → 无意义事件渲染空状态，不再泄露内部 JSON。+ 新增 class-guard 单测
  `TestActivityPreviewText_NoRawJSONLeak`。
- 验收：重部署后 `GET /api/orgs/e2e/agents` → Alice/Bob `last_activity_content=null`，
  Carol/Dave 显示有意义文本（"I've identified myself…"）。截图
  `/tmp/e2e-verify/A-agents-after-fix.png` LAST ACTIVITY 列干净无 JSON。**闭环。**

### F-4 [P2 设计约束，记录非改] channel/DM 内 agent→agent @mention 不唤醒对方
- 现象：Alice 发 "@Bob what date is it today?"（Alice 是 agent 发送者）**没有唤醒 Bob**。
- 代码依据：`wake_projector.go` 注释 —— #185 conversational-wake "wakes agents ONLY on
  messages from a human sender"，"an agent's own reply never wakes any agent"（结构性
  loop-break，防 agent 互相唤醒成无限循环）。
- 结论：这是**有意的防循环设计**，非 bug。多 agent 协作的受支持路径是"人类编排"或
  "plan/task 系统编排"（系统投递 @mention）。改它会引入 runaway loop 风险，**不修**，
  仅记录为能力边界。（与 F-3 不同：F-3 是真 bug。）

### F-7 [P1 ✅已修已验] Web Console 里给任务分配 agent 不会 dispatch，任务永远卡 open
- 复现（**纯 UI 驱动**发现）：UI 建任务 → 打开任务 → Edit Task → Assignee 选某 agent →
  Save。任务 assignee 设上了，但 agent 从不执行，任务一直 `open`、会话 "No messages yet"。
- 根因（前端用错端点）：`TaskEditModal` 用 `useUpdateTask`（批量 `PATCH /tasks/{id}`
  → `pmBatchUpdateTaskHandler` → `BatchUpdateTask`）提交 assignee。`BatchUpdateTask`
  只 emit `pm.task.state_changed`，而 dispatch/wake projector 监听的是
  `pm.task.assigned`/`reassigned`（由专用 `AssignTask` 发出）。后端**两条 assign 路径
  语义不同**（专用端点 dispatch+授权 project membership；批量 PATCH 故意"轻量"不
  dispatch，多处测试依赖此设计）。`useAssignTask`（走专用端点、会 dispatch）其实早已
  存在且 PlanDetail 在用——TaskEditModal 选错了路径。
- 修复（前端对齐，零后端改动）：`TaskEditModal` 改 assignee 时走 `useAssignTask`
  （专用 dispatch 端点），其余字段仍走批量 PATCH；清空(unassign) 仍走 PATCH（无需
  dispatch）。更新单测断言"真分配走 assign 端点"。
- 验收：见下方"纯 UI 建 agent + 下发任务端到端"——重建 SPA 重部署后，UI 里 Edit→
  Assignee→Save 即触发 agent 执行，任务跑到 completed。**闭环。**

---

## 验收总结（逐条对照）

| ID | 严重度 | 描述 | 处置 | 状态 |
|---|---|---|---|---|
| F-1 | P2 | agent 完成任务发两条近重复消息 | 疑 LLM 自身行为，非平台确定性 bug | 记录观察 |
| F-2 | — | messages 端点无 sender_display_name | UI 从 participants 正确解析，用户不可见 | 非问题/关闭 |
| **F-3** | **P1★** | **agent 不知自身身份→多 agent 冒认** | **get_my_profile 返身份 + system prompt 身份定向 + 工具描述** | **✅ 已修已验** |
| F-4 | P2 | channel 内 agent→agent @mention 不唤醒 | 有意的防循环 loop-break，改它有 runaway 风险 | 设计约束/不改 |
| **F-5** | **P2** | **agents"最后活动"渲染原始 stream-json** | **activityPreviewText 永不回退 raw payload** | **✅ 已修已验** |
| F-6 | P2 | 终态任务默认隐藏，空状态文案不提示 | v2.9.1 有意设计；仅文案可优化 | 记录/低优先 |
| **F-7** | **P1** | **UI 给任务分配 agent 不 dispatch，任务卡 open** | **TaskEditModal 改走专用 assign 端点（dispatch 路径）** | **✅ 已修已验** |

**三个真 bug（F-3、F-5、F-7）已修复并在重新部署的真实实例上逐条复测确认闭环。**
（F-7 由"纯 UI 驱动建 agent + 下发任务"端到端发现——只用 API 驱动时走的是已 dispatch 的专用端点，掩盖了 UI 实际用错端点的问题；说明全 UI 链路验证不可省。）

### 修复后 Web Console 全 UI 驱动端到端（非 API 驱动）
`tests/e2e/v2/ac-ui-e2e-verify.mjs`：Playwright 真正操作 UI —— `/signin` 登录 →
打开 identity-check 频道 → **在消息输入框里打字** "@Carol quick check: reply … your
own name and the result of 6 times 7." → **点 UI 发送按钮** → 等 Carol 回复在 UI 实时渲染：
> **Carol：「I am Carol, and 6 times 7 is 42.」**（身份正确 + 答案正确，0 console / 0 page error）

截图 `/tmp/e2e-ui-e2e/3-after-reply.png`。这条确认 F-3 修复在**纯 UI 链路**（人在浏览器里
打字发送 → 真实 claude agent 唤醒 → 回复实时回流渲染）同样成立，不止 API 层。

### 修复后 Web Console 全 UI 驱动「建 agent + 下发任务」端到端
`tests/e2e/v2/ac-ui-agent-task-e2e.mjs`：全程 UI 操作 —— `/signin` 登录 → Agents 页
**+ Add Agent**（填名、选 worker、提交）建 agent → 进详情页点 **Start** 启动 → 项目
Tasks tab **+ New Task** 建任务 → 打开任务 **Edit Task → Assignee 选该 agent → Save**
→ 轮询任务在 UI 里跑完。结果（截图 `/tmp/e2e-ui-agent-task/8-task-completed.png`）：
> 任务 T4 STATUS = **COMPLETED**（<1min）；assignee = Grace；agent 回复
> 「The hostname command output is: ubuntu」。0 console / 0 page error。

**此流程同时发现并验证了 F-7**：修复前 UI 分配后任务永远 OPEN（agent 不 dispatch），
修复后 UI 分配即触发 dispatch、agent 执行、任务 COMPLETED。这是端到端最强证据
（建 machine + 建 agent + 下发任务 + agent 执行，全部经 Web Console 真实操作）。

### 验证质量门
- `go build ./...` OK；`go vet`(受影响包) OK。
- 受影响包测试全绿：claudestream / mcphost / admin/api / webconsole/api。
- lint 门禁：lint-vendor / lint-mock-default / lint-doc-impl-drift 全过。
- 新增 class-guard：`TestActivityPreviewText_NoRawJSONLeak`（F-5）、
  `TestGetMyProfile_Populated` 加 display_name/agent_ref 断言（F-3）。
- 全量 `go test ./...`：仅 `TestSupervisorSession_DetachSurvives` 偶发失败，
  单独重跑连续通过 → 全量并行下的 flaky（supervisor.instance 文件竞争），
  与本次改动无关（未触碰 workerdaemon/agentsupervisor），符合 handoff §7 所述环境敏感测试。
- Web Console 全页 Playwright 走查：**0 console error / 0 page error**，SPA 干净。
- F-7（前端）门禁：`tsc --noEmit` OK、ESLint clean、**前端全量 vitest 920 通过/106 文件**；
  `TaskEditModal.test.tsx` 更新断言"真分配走 assign 端点"。后端 `go build ./...` OK
  （F-7 无后端改动）。

### E2E 环境（可复现）
- 独立实例 prefix `~/.agent-center.e2e`，端口 web:7180 / server:7150 / admin:7380
  （刻意避开文档中的 prod `~/.agent-center` 与 launchd 管理的 test-* 沙箱）。
- 真实 worker `worker-78bcd874`(linux-box-1)，真实 claude-code agents
  （worker 带 proxy env，spawn 的 claude 用 `~/.claude/.credentials.json` 认证）。
- 截图：`/tmp/e2e-shots/`（走查全套）、`/tmp/e2e-verify/`（F-3/F-5 修复后证据）。
- 驱动脚本：`tests/e2e/v2/ac-ui-walkthrough.mjs`、`ac-verify-shots.mjs`。
