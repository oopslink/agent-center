# InputRequired 协议

任务执行中，worker 上的 agent 需要 supervisor / 用户拍板才能继续 —— 同步阻塞式的请示协议。

> InputRequest 仍是独立聚合（独立表 / 状态机 / 超时）；UI 投递走 [Task ↔ Conversation 1:1 模型](../decisions/0017-task-as-conversation.md)：写一条 `content_kind=agent_finding, input_request_ref=<id>` 的 Message 到 `task.conversation_id`，Bridge 渲染时附按钮。详见 [ADR-0017 § 5](../decisions/0017-task-as-conversation.md)。

## 触发场景

- Agent 想跑危险命令（`rm -rf`、`git push -f`、`docker prune`）
- Agent 遇到歧义（"用 REST 还是 GraphQL？"）
- Agent 完成里程碑要 gate（"测试通过了，要继续写 PR 描述吗？"）
- Agent 撞到意料之外的情况（"测试在另一个无关模块也挂了，要修吗？"）

## 调用方式

Agent 通过 CLI 命令主动请示：

```
agent-center request-input --question="..." [--options="A,B,C"] [--urgency=normal|urgent]
```

行为：**阻塞**，等中心回应。stdout 输出 JSON：

```json
{ "answer": "...", "decided_by": "human:hayang | supervisor:invocation-id", "decided_at": "..." }
```

超时：默认 24h（可配置），超时返回非零退出码 + 错误 JSON。

Agent 怎么知道有这个命令？通过 [`worker-agent.md`](10-skill-cli-tooling.md) skill。

## 完整流程

```
1. Worker daemon spawn agent，env 注入:
     AGENT_CENTER_TASK_ID=42
     AGENT_CENTER_EXECUTION_ID=a3f2-1d
     AGENT_CENTER_CONVERSATION_ID=<task.conversation_id> or ""
     AGENT_CENTER_WORKER_SOCK=/var/run/agent-center-worker.sock

2. Agent 跑到岔路 → 执行 `agent-center request-input "..."`

3. CLI 子命令逻辑:
   - 检测到 env 中有 WORKER_SOCK
   - 连接到该 unix socket
   - 发 RPC: { task_id, execution_id, kind: "request-input", payload: {...} }
   - 阻塞读 response

4. Worker daemon 收到 RPC:
   - 生成 input_request_id
   - 通过 center RPC 单事务执行 (ADR-0014 § 2 同事务原则):
       a. 写 input_request 行 (status=pending)
       b. 更新 task_execution.pending_input_request_id + status = 'input_required'
       c. INSERT events: input_request.requested + task_execution.input_required
       d. 解析 task.conversation_id:
          - 非 null → 写一条 Message (kind=agent_finding, input_request_ref=<id>,
            content=问题文本, direction=outbound) 到该 conversation
          - null → 触发 ADR-0017 § 10.4 硬规则 fallback:
                  bind-card → notification.default_channel 配置项指定的渠道
                  未配置 → 整事务回滚，InputRequest 创建失败，
                          center emit task_execution.failed (reason='no_input_channel')
   - 把 (request_id → response channel) 放本地 pending map

5. Center 端事件流转:
   - conversation.message_added emit → FeishuBridge 订阅 → 投递 (见 Step 6)
   - input_request.requested emit → supervisor 唤醒（事件白名单已包含）

6. FeishuBridge 渲染 Message:
   - 检测 input_request_ref ≠ null → 渲染为 interactive card
   - join InputRequest 表取 options / urgency
   - 卡片样例:
       [🟡 Task #42 等你拍板]
       ❓ 要不要把 enroll 流程从 CLI 改成自动注册?
       🤖 Supervisor 建议: 保持 CLI（一致性更好，supervisor_summary 占独立 Message）
       📎 上下文: ...最近 30 行 agent 思考（可选 [查看详情] 走 peek-trace）...
       [选 A: 保持 CLI] [选 B: 自动注册] [自己写答复] [取消任务]
   - 投递成功 → 回填 message.vendor_msg_ref；emit channel.delivered

7. 用户响应路径（三选一，详见后续节）:
   a. 点卡片按钮 → Bridge 收 card.action → 调 InputRequest.respond + 写一条
      Message (kind=text, direction=inbound, sender=user) 留痕
   b. 在 task conversation thread 内 @bot 自由文本 → Bridge 写 inbound Message →
      supervisor 唤醒 → 解析意图 → 调 InputRequest.respond（烧 LLM；用户不知道 slash 时兜底）
   c. 用 /answer <task_id> <text> slash 命令 → Bridge 直接调 InputRequest.respond +
      写一条 Message 留痕（不经 supervisor，[ADR-0017 § 6](../decisions/0017-task-as-conversation.md)）

8. Center 收到 InputRequest.respond:
   - 写 input_request.response_text, responded_at, responded_by
   - status: pending → responded
   - 更新 task_execution.status 回到 working；pending_input_request_id = null
   - emit input_request.responded
   - Bridge 订阅 → update_card 把卡片按钮置灰，显示 "✅ 已答复 <answer>"
     (state-machine 驱动的一次性变更，不是 ADR-0016 驳回的周期 refresh)
   - 发 InputResponseEvent 到对应 worker

9. Worker daemon 收到 InputResponseEvent:
   - 在 pending map 找 request_id 对应的 channel
   - 把 response 写进 channel → CLI 子命令解阻塞
   - CLI 在 agent 子进程 stdout 打印 JSON answer
   - Agent 拿到回答继续干
```

## 用户响应的三条路径

### A. 卡片按钮

最常见。Bridge 收 `card.action.trigger` 事件 → 解析 `input_request_id` + 用户选项 → 调 center 的 InputRequest.respond API + 写一条 inbound `text` Message 到同 conversation 作为留痕（`content = "选: <option>"` 或自由输入的文本）。

### B. 自由文本 @bot

用户在 task conversation thread 内说话（不点按钮）：

- Bridge 收 `im.message.receive_v1` → 走 [12-conversation.md § 6 inbound](12-conversation.md) 写 Message
- conversation.message_added emit → supervisor 唤醒
- supervisor 看 conversation 当前是否有 pending InputRequest → 解析意图 → 调 InputRequest.respond

这条路径**烧 supervisor LLM**，用作"用户不知道 slash 命令时的兜底"。

### C. Slash 命令 `/answer`

```
/answer <task_id> <text>
```

由 [ADR-0017 § 6](../decisions/0017-task-as-conversation.md) 提前到 v1（飞书 D2 slash 模式首批命令之一，详见 [09-feishu-integration.md § 9](09-feishu-integration.md)）。Bridge 收 slash → 直接调 InputRequest.respond + 写一条 `text` Message 留痕 → **不经 supervisor**。

理由：input 响应是高频 + 结构化场景，自由文本 @bot 走 supervisor 解析增加延迟 + 误解风险。

## 超时 / 升级

| 时点 | 行为 |
|---|---|
| T+0 | InputRequest 创建 + Message (input_request_ref) 写入；Bridge 渲染卡片投递 |
| T+4h | Supervisor wake：用户没回，提醒一次（飞书 ping，走 supervisor_summary Message 同 conversation） |
| T+24h | InputRequest → `timed_out`；emit `input_request.timed_out`；Bridge 订阅 → update_card 显示 "⏰ 已超时"；Worker 端 CLI 超时，返回 error 给 agent；Agent 通常 fail 任务；TaskExecution.status = `failed`, reason=`input_timeout`, message="<具体超时上下文>"；Supervisor 收到 failed 决定是否重派 |

T1 / T2 都是 per-project 可配置（v1 全局默认 4h / 24h，不做 per-project）。

## 与 Issue 的区分

参见 [03-issue-discussion.md § "Issue 与其他实体的区别"](03-issue-discussion.md#issue-与其他实体的区别)。

## 关键概念

- **Aggregate**: InputRequest（聚合根，独立表 / 独立状态机 / 独立超时；不合并到 Conversation Message）
- **Event**: `input_request.requested` / `input_request.responded` / `input_request.timed_out` / `input_request.canceled`
- **TaskExecution 状态适配**: `working → input_required → working`（resume）或 `→ failed`（timeout）；详见 [02-task-model.md § 3](02-task-model.md)
- **UI 集成**: 同事务写 Message (`kind=agent_finding, input_request_ref=<id>`) 到 task.conversation_id，Bridge 渲染附按钮；状态变化时 Bridge `update_card` 置灰（合法的一次性 state-machine 驱动 update，**不是**周期 refresh）。详见 [ADR-0017 § 5](../decisions/0017-task-as-conversation.md)
- **conversation_id=null fallback**: b/c/d 来源 task 触发 InputRequest 时若未绑 conversation，center 硬规则自动 bind 到 `notification.default_channel`；未配置 → InputRequest 创建失败，`task_execution → failed (reason=no_input_channel)`（[02-task-model.md § 3.6](02-task-model.md)；Task 没有 failed 状态，失败只在 execution 层面）；详见 [ADR-0017 § 10.4](../decisions/0017-task-as-conversation.md)
- **错误协议**: 所有 timeout / cancel 等失败回执必须同时携带 `reason` + `message` 双字段（[conventions § 16](../../rules/conventions.md)）

具体表 schema 见 [implementation/02-persistence-schema.md](../implementation/02-persistence-schema.md)（TBD）。
