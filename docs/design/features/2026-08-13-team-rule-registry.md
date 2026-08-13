# Team Rule Registry：索引常驻、正文按需加载

状态：Frozen for implementation  
Issue：`issue-bfc8c3b1`  
基线：`origin/main@1372c021166490215450f4d05c754cf97acf03f7`

## 1. 问题与目标

当前 `get_team_rules(phase)` 返回该阶段全部规则正文，runtime 再把正文整体写进
Supervisor 或 Executor 上下文。规则越多，每次运行携带的无关正文越多；虽然快照已有
team-memory commit，却没有记录 agent 实际按需读取了哪些规则。

本方案交付两个结果：

1. **防上下文膨胀**：默认上下文只放短索引，不放全部正文；正文由 agent 按需读取。
2. **可审计**：每次读取都绑定 team-memory commit，并在 TaskExecution 审计中记录已加载
   rule ID，能够回答“哪个执行在什么版本下读过哪条规则”。

不改 Task / Plan / PlanNode 模型，不新增 `work_type`、角色枚举或规则分类枚举。

## 2. 统一语言与边界

- **Rule Index Entry**：规则目录项，包含 `slug`、标题、适用提示、阶段和正文大小；不含正文。
- **Rule Body**：一条规则的完整 Markdown 正文。
- **Rule Snapshot**：同一 team-memory commit 下的索引及已读取正文集合。
- **Rule Load Audit**：某 TaskExecution 在某 commit 下成功读取一条 Rule Body 的事实。

规则文件仍由 Cognition / Team Memory 的 center-hosted git repo 持有。Admin agent-tool 是
AppService 对外表面；runtime 不直读 git repo。TaskRuntime 只持不可变审计投影，不成为
规则内容的第二权威源。

## 3. 最小产品流程

```text
fresh Supervisor / Executor
        |
        v
get_team_rule_index(phase)
        |
        +-- prompt: commit + 短索引（不含正文）
        |
agent 判断当前动作所需规则
        |
        v
get_team_rule(slug, commit)
        |
        +-- 返回同一 commit 的 Rule Body
        +-- 追加 Rule Load Audit（幂等）
```

索引项的 `description` 必须写成一句“何时需要读”的适用提示。agent 可根据任务语义选择，
系统不猜任务类型。不可违反的安全不变量仍由状态机、权限和 gate 强制，不能降级为 prompt。

## 4. 接口契约

### 4.1 `get_team_rule_index`

请求：

```json
{"agent_id":"agent-…","phase":"execute","execution_id":"exec-…"}
```

响应：

```json
{
  "team_id":"team-…",
  "phase":"execute",
  "commit":"<40-char sha>",
  "rules":[
    {
      "slug":"verify-world-state-after-actions",
      "title":"命令成功不等于世界已改变",
      "description":"状态写入、代码交付、关闭 issue 或运行配置变更时读取。",
      "applies_to":["execute","review","recovery"],
      "body_bytes":1234,
      "source_path":"rules/verify-world-state-after-actions-….md"
    }
  ],
  "skipped_nonstandard":[],
  "refresh_semantics":"…"
}
```

排序固定为 `slug` 升序。只返回 enabled 且命中 phase 的规则。响应禁止出现 `body`。

### 4.2 `get_team_rule`

请求：

```json
{
  "agent_id":"agent-…",
  "slug":"verify-world-state-after-actions",
  "commit":"<index response commit>",
  "execution_id":"exec-…"
}
```

`commit` 必填。服务必须从该 commit 读取，不能从当前 HEAD 偷换正文：

- commit 不存在或不属于该 team repo：`rule_snapshot_not_found`；
- slug 在该 commit 不存在、disabled 或不适用于快照 phase：`team_rule_not_found`；
- 成功：返回完整规则字段和同一 commit。

读取成功后追加幂等审计键
`(execution_id, team_id, commit, slug)`。没有 `execution_id` 的 planning session 仍可读，审计落在
`planning_session_id`；两者都没有时拒绝正文读取，避免不可归属的审计事实。

### 4.3 兼容接口

`get_team_rules(phase)` 在一个发布周期内保留，标记 deprecated，供旧 runtime 使用；新 runtime
只调用 index + entry。迁移完成后删除兼容接口，避免长期双轨。

## 5. Runtime 上下文与预算

新 `RuleSnapshot` 保存 Index Entries，不保存全部 Body。启动 prompt 固定渲染：

```text
## Team Rule Index (execute)
team=… commit=…
Read a rule with get_team_rule before acting when its description applies.
- verify-world-state-after-actions — 状态写入、代码交付……时读取。
```

硬限制在 Team Memory 规则提交校验和读取端同时执行：

- 单条 description：最多 240 UTF-8 bytes；
- 单个 phase 的索引：最多 64 条且最多 16 KiB；
- 超限返回 `team_rule_index_too_large`，不得静默截断；
- Rule Body 不预注入，因此正文总量不占启动上下文预算。

选择 16 KiB 是协议字节预算，不依赖 tokenizer；行为稳定且容易测试。若团队确有超过 64 条
同阶段硬规约，应先合并/淘汰，而不是提高默认预算。

## 6. 审计模型

TaskExecution 审计追加事件 `team_rule.loaded`：

```json
{
  "execution_id":"exec-…",
  "team_id":"team-…",
  "team_memory_commit":"<sha>",
  "rule_slug":"verify-world-state-after-actions",
  "phase":"execute",
  "loaded_at":"…"
}
```

重复读取同一审计键不重复发事件。`list_task_executions` / `get_task_execution` 返回：

```json
{
  "team_rule_snapshot":{"team_id":"…","commit":"…","phase":"execute"},
  "loaded_rule_ids":["verify-world-state-after-actions"]
}
```

数组按 slug 排序。审计不保存 Rule Body；复核时用 commit + slug 从 git 历史重放。

## 7. 故障与刷新语义

- 新 fork / 新 planning generation 读取当前 HEAD，形成新快照。
- 在途执行与 tier-1/2 recovery 保持原 commit；tier-3 reset 形成新快照。
- 索引加载失败必须在 prompt 中显式显示 unavailable，不能伪装成“无规则”。
- 单条正文读取失败不得回退到当前 HEAD；agent 应报告阻塞或重试原 commit。
- non-standard 文件继续跳过并显式返回路径，保持现有防御性读取契约。

## 8. 实现切片

1. **Cognition reader**：支持按 commit 读取索引、按 commit + slug 读取正文及预算校验。
2. **Admin / agent tools**：增加 index / entry 接口、鉴权、稳定错误码和 MCP 暴露。
3. **Runtime**：启动只冻结索引；为 Supervisor / Executor 提供 entry 读取能力；删去 prompt
   中的全正文渲染。
4. **Observability**：持久化 `team_rule.loaded`，扩展 TaskExecution read model。
5. **迁移与文档**：保留旧接口一个发布周期，更新 Team Memory 写作说明和 operator 文档。

## 9. 验收契约

1. 100 条大正文规则下，启动 prompt 增量只随索引增长，且受 16 KiB 硬上限约束。
2. index 响应及启动 `input.json` 不含任何 Rule Body。
3. 使用 index 返回的 commit 读取规则，即使 HEAD 随后变化，仍返回原版本正文。
4. 不存在/跨 team commit、disabled/不适用规则均 fail closed，不回退 HEAD。
5. 同一执行重复读取同一规则仅产生一个审计事实。
6. TaskExecution read model 返回快照 commit 和排序后的 `loaded_rule_ids`。
7. 新 fork 看到新 commit；在途执行和 tier-1/2 recovery 保留旧 commit。
8. 旧 `get_team_rules` 在兼容期契约测试保持通过。
9. 相关 Go 单测、契约测试、`go test ./...` 与并发审计路径的 race 测试全部通过。

## 10. 明确推迟

以下不是 V1 范围，记录为后续节奏项：语义/向量检索、自由标签、多维规则图、自动冲突消解、
基于 token 的动态裁剪、自动把 memory 晋升为 rule。V1 用短目录 + agent 语义判断，先以审计数据
观察漏读和误读，再决定是否增加召回机制。

## 11. 自检

- 单一来源：Rule Body 只在 Team Memory git repo；审计只存 commit + slug。
- AppService 边界：runtime 通过 agent-tool 读取，不直访 repo。
- 可观测性：读取成功产生幂等审计事件，并进入 TaskExecution read model。
- AI native：目录表达适用语义，不硬编码任务分类，不引入 LLM SDK。
- 失败语义：超限和快照丢失均 fail loud，不静默裁剪或切换 HEAD。

