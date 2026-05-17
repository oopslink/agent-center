# InputRequired 协议

任务执行中，worker 上的 agent 需要 supervisor / 用户拍板才能继续 —— 同步阻塞式的请示协议。

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
     AGENT_CENTER_WORKER_SOCK=/var/run/agent-center-worker.sock

2. Agent 跑到岔路 → 执行 `agent-center request-input "..."`

3. CLI 子命令逻辑:
   - 检测到 env 中有 WORKER_SOCK
   - 连接到该 unix socket
   - 发 RPC: { task_id, execution_id, kind: "request-input", payload: {...} }
   - 阻塞读 response

4. Worker daemon 收到 RPC:
   - 生成 input_request_id
   - 写一条 input_request 行 (status=pending)
   - 上报 InputRequestedEvent → center
   - 把 (request_id → response channel) 放 pending map

5. Center 收到 InputRequestedEvent:
   - 入库 / 落事件流
   - 更新 task_execution.pending_input_request_id, task_execution.status = 'input_required'
   - 触发 supervisor 唤醒（事件白名单已包含）

6. Supervisor 看 InputRequest:
   - v1 策略：组装飞书卡片，可选附带"自己倾向的答案"，推给用户
   - 不自动回答

7. 飞书卡片:
   [🟡 Task #42 等你拍板]
   ❓ 要不要把 enroll 流程从 CLI 改成自动注册?
   🤖 Supervisor 建议: 保持 CLI（一致性更好）
   📎 上下文: ...最近 30 行 agent 思考...
   [选 A: 保持 CLI] [选 B: 自动注册] [自己写答复] [取消任务]

8. 用户点按钮 → 飞书事件 → center:
   - 写 input_request.response_text, responded_at, responded_by='human:hayang'
   - 发 InputResponseEvent 到对应 worker
   - task_execution.status 回到 working

9. Worker daemon 收到 InputResponseEvent:
   - 在 pending map 找 request_id 对应的 channel
   - 把 response 写进 channel → CLI 子命令解阻塞
   - CLI 在 agent 子进程 stdout 打印 JSON answer
   - Agent 拿到回答继续干
```

## 超时 / 升级

| 时点 | 行为 |
|---|---|
| T+0 | InputRequested |
| T+4h | Supervisor wake：用户没回，提醒一次（飞书 ping） |
| T+24h | Worker 端 CLI 超时，返回 error 给 agent；Agent 通常 fail 任务；TaskExecution.status = `failed`, reason=`input_timeout`, message="<具体超时上下文>"；Supervisor 收到 failed 决定是否重派 |

T1 / T2 都是 per-project 可配置（v1 全局默认 4h / 24h，不做 per-project）。

## 与 Issue 的区分

参见 [03-issue-discussion.md § "Issue 与其他实体的区别"](03-issue-discussion.md#issue-与其他实体的区别)。

## 关键概念

- **Aggregate**: InputRequest（聚合根）
- **Event**: `InputRequested` / `InputResponseProvided` / `InputRequestTimedOut` / `InputRequestCanceled`
- **TaskExecution 状态适配**: `working → input_required → working`（resume）或 `→ failed`（timeout）；详见 [02-task-model.md § 3](02-task-model.md)
- **错误协议**: 所有 timeout / cancel 等失败回执必须同时携带 `reason` + `message` 双字段（[conventions § 16](../../rules/conventions.md)）

具体表 schema 见 [implementation/02-persistence-schema.md](../implementation/02-persistence-schema.md)（TBD）。
