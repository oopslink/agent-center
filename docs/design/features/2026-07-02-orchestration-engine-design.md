# 通用编排引擎设计

> **状态**: 设计已确认，待实现计划
>
> **动机**: 现有 Plan DAG + Task 状态机中，开发周期（cycle）的业务语义（CycleNodeRole、merge guard、scaffold、decision auto、未合并看板）硬编码在引擎层。编排系统应是通用的，cycle 等定制化流程应是基于编排系统的参数化实例——由 Agent 读 markdown 模板后调 MCP tools 建图执行，而非编译在引擎代码中。
>
> **设计原则**:
> - 引擎是纯通用的，没有任何业务语义
> - 模板是 markdown 文档，面向 Agent 阅读，不面向引擎解析
> - 预制的 coding flow 和第三方团队的 flow 走完全相同的路径
> - 节点能力通过 webhook/REST/CLI 扩展
> - guard 不是引擎概念，Agent 自行决定检查时机

---

## 1. DDD 定位

编排引擎是 **ProjectManager BC 内部的核心子域**（Core Subdomain），放在 `internal/projectmanager/orchestration/`。

理由：
- PM 的 Task 和编排 Node 存在高频交互，跨 BC 通信成本高
- 编排是 PM 的核心能力，不是基础设施

Node 是独立聚合，Task 通过外键关联 Node。一个 Task 可以不在任何 DAG 中（backlog），进入 Plan 时创建对应 Node 并关联。

---

## 2. 领域模型

### 2.1 聚合与值对象

```
orchestration/
  Graph (聚合根)        — 一张 DAG，属于某个 Plan
    +-- Node (实体)     — 业务节点 或 控制节点
    +-- Edge (值对象)   — 有向依赖边（from -> to）
    +-- ActionLog (值对象) — 节点生命周期事件记录
```

### 2.2 Graph

```
Graph {
  id:        GraphID
  planID:    PlanID       // 所属 Plan，1:1
  status:    draft | running | done | archived
  nodes:     []Node
  edges:     []Edge
  createdAt: time
  updatedAt: time
}
```

状态机：

```
draft -> running -> done -> archived
draft -> archived                      // 未启动直接归档
running -> draft                       // 回退到草稿，允许重新编排
```

自动完成：当所有 end 节点的上游链路都已完成（所有业务节点 completed 或 discarded），Graph 自动转为 done。

### 2.3 Node

```
Node {
  id:          NodeID
  graphID:     GraphID
  category:    business | control
  controlKind: start | end | condition   // 仅 control 节点
  title:       string
  status:      open | running | completed | reopen | discarded
  outcome:     string                    // completed 时由执行者设置，如 "success"/"failure"/自定义
  metadata:    map[string]any            // 自由扩展，引擎不解释
  actionLogs:  []ActionLog              // append-only 历史
}
```

状态机：

```
open -> running -> completed -> reopen -> running -> ...
open/running -> discarded (终态)
```

两种节点类型：
- **业务节点** — 有 assignee（通过关联 Task），实际工作由人/Agent/webhook 完成
- **控制节点** — start、end、condition；不关联 Task

### 2.4 Edge

```
Edge {
  fromNodeID: NodeID
  toNodeID:   NodeID
}
```

纯依赖关系。conditional/loopback 语义由 condition 节点的 onSuccess/onFailure 表达，不在边上。

### 2.5 派生状态

引擎从 Node.status + Edge 依赖关系派生节点的可执行性：
- **blocked** — 存在未完成的上游依赖
- **ready** — 所有上游依赖已完成，可以开始

---

## 3. condition 节点

### 3.1 配置

condition 节点的判定配置存在其 metadata 中：

```json
{
  "evaluator": "upstream_outcome",
  "logic": "and",
  "on_success": ["node-3", "node-4"],
  "on_failure": ["node-1", "node-2"],
  "max_rounds": 3,
  "on_max_exceeded": "force_success"
}
```

### 3.2 三种判定来源

**upstream_outcome** — 读取所有直接上游业务节点的 outcome：
- `and`：全部 outcome 为 success 才成功
- `or`：任一 outcome 为 success 即成功

**external_hook** — 调 HTTP endpoint：
```json
{
  "evaluator": "external_hook",
  "hook_url": "https://ci.example.com/api/check",
  "hook_method": "GET",
  "success_condition": "response.status == 'passed'"
}
```

**manual** — 等待人或 Agent 通过 `resolve_condition(nodeID, result)` 设置。

### 3.3 触发时机

- upstream_outcome / external_hook：当 condition 的所有上游节点都达到终态（completed 或 discarded）时，引擎自动执行判定
- manual：引擎将 condition 标记为 waiting，等外部调用 resolve

### 3.4 成功路由

激活 on_success 列表中的下游节点（status -> open/ready）。

### 3.5 失败回退

on_failure 指定**回退目标节点**，引擎沿 DAG 反向遍历，从 condition 到 on_failure 目标之间链路上的所有 completed 节点都回退为 reopen。

示例：
```
A(completed) -> B(completed) -> C(completed) -> condition(failure)
                                                  on_failure: [A]

回退结果：A, B, C 全部 -> reopen
```

回退约束：
- 只回退 completed 状态的节点，discarded 节点不回退
- 控制节点（start/end/其他 condition）不参与回退，跳过
- 回退时在每个节点的 action log 中记录 `reactivated_by: conditionNodeID, round: N`

### 3.6 最大轮次

可选配置 `max_rounds`。达到上限后：
- `on_max_exceeded: "force_success"` — 强制走 on_success 路由
- `on_max_exceeded: "discard"` — condition 节点标记为 discarded

不配置 max_rounds 则无限制。

---

## 4. Graph 动态修改

Graph 在 draft 和 running 状态下都可以修改：

| 操作 | draft | running | 约束 |
|---|---|---|---|
| 添加节点 | 可以 | 可以 | 无 |
| 删除节点 | 可以 | 仅 open/reopen 状态的节点 | running/completed 节点不可删 |
| 添加边 | 可以 | 可以 | 不得形成环（condition 的 on_failure 回指不算边，不参与环检测） |
| 删除边 | 可以 | 可以 | 无 |
| 修改节点 metadata | 可以 | 可以 | 引擎不解释 metadata，随时可改 |

---

## 5. MCP Tools 接口

### 5.1 图管理

| Tool | 参数 | 说明 |
|---|---|---|
| `create_graph` | planID | 为 Plan 创建空图（自动含 start + end 节点） |
| `get_graph` | graphID | 获取完整结构（节点、边、状态） |
| `start_graph` | graphID | draft -> running，激活 start 节点的下游 |
| `finish_graph` | graphID | 手动标记 done（或引擎自动判定） |

### 5.2 节点操作

| Tool | 参数 | 说明 |
|---|---|---|
| `add_node` | graphID, category, controlKind?, title, metadata? | 添加节点 |
| `remove_node` | nodeID | 删除节点 |
| `update_node` | nodeID, title?, metadata? | 修改节点元数据 |
| `start_node` | nodeID | open/reopen -> running |
| `complete_node` | nodeID, outcome | running -> completed |
| `discard_node` | nodeID | open/running -> discarded |
| `resolve_condition` | nodeID, result(success/failure) | 手动设置 condition 判定结果 |

### 5.3 边操作

| Tool | 参数 | 说明 |
|---|---|---|
| `add_edge` | graphID, fromNodeID, toNodeID | 添加依赖边（自动环检测） |
| `remove_edge` | graphID, fromNodeID, toNodeID | 删除边 |

### 5.4 查询

| Tool | 参数 | 说明 |
|---|---|---|
| `list_nodes` | graphID, status?, category? | 按条件筛选节点 |
| `get_node` | nodeID | 获取节点详情（含 action log） |
| `get_ready_nodes` | graphID | 获取当前可执行的节点列表 |

### 5.5 Task 关联

| Tool | 参数 | 说明 |
|---|---|---|
| `bind_task` | nodeID, taskID | 将 Task 关联到 Node |
| `unbind_task` | nodeID | 解除 Task 关联 |

---

## 6. 模板与 Agent 协作模型

### 6.1 模板是 markdown

模板不是结构化的 YAML/JSON，而是一份描述编排规则的 markdown 文档。面向 Agent 阅读，不面向引擎解析。

示例（简化的 cycle 模板）：

```markdown
# 开发周期编排规则

## 输入
- version: 版本号（如 v2.13.0）
- features: 功能列表，每个功能有 name 和 spec

## 编排结构
1. 创建 start 节点
2. 创建 "开发主分支" 业务节点（从 main 切 dev/{version}）
3. 对每个 feature 创建：
   - "{feature.name} - 开发" 业务节点
   - "{feature.name} - 评审" 业务节点
   - "{feature.name} - 评审决策" condition 节点
     - evaluator: manual
     - on_success: ["{feature.name} - 集成"]
     - on_failure: ["{feature.name} - 开发"]（回退重做）
     - max_rounds: 3, on_max_exceeded: discard
   - "{feature.name} - 集成" 业务节点
4. 创建 "集成完成检查" condition 节点
   - evaluator: upstream_outcome, logic: and
   - on_success: ["验收"]
5. 创建 "验收" 业务节点
6. 创建 "发布" 业务节点
7. 创建 end 节点

## 依赖关系
- start -> 开发主分支
- 开发主分支 -> 所有 feature 的开发节点
- 每个 feature 内：开发 -> 评审 -> 评审决策 -> 集成
- 所有集成节点 -> 集成完成检查 -> 验收 -> 发布 -> end

## 节点完成标准（供 Agent 判断）
- "集成" 节点完成前，Agent 应检查分支是否已合并到主干
- "评审" 节点完成前，Agent 应检查 code review 是否通过
- "验收" 节点完成前，Agent 应在集成主干上运行完整测试
```

### 6.2 Agent 的工作方式

1. **建图阶段** — Agent 读模板 + 用户输入的参数，调用 `create_graph`、`add_node`、`add_edge` 等 MCP tools 建图
2. **执行阶段** — Agent 查询 `get_ready_nodes`，认领节点，执行工作（调 webhook/REST/CLI），完成后调 `complete_node`
3. **检查阶段** — 模板中描述的"完成标准"由 Agent 自行理解和执行，引擎不强制
4. **动态调整** — 执行过程中 Agent 可以根据情况调用 `add_node`、`add_edge` 动态修改图

### 6.3 模板管理

- 系统预制的模板放在 `docs/flow-templates/` 目录下
- 第三方团队的模板通过 API 上传注册，存储为 markdown 内容
- Agent 建图时引用模板 ID 或直接读模板内容

---

## 7. 与现有代码的关系

### 7.1 包结构变更

```
internal/projectmanager/
  orchestration/               # 新增：编排引擎子域
    graph.go                   # Graph 聚合根
    node.go                    # Node 实体 + 状态机
    edge.go                    # Edge 值对象
    condition.go               # condition 节点判定逻辑
    action_log.go              # ActionLog 值对象
    repository.go              # 持久化接口
    sqlite/                    # SQLite 实现

  # 现有代码（逐步迁移）
  task.go                      # Task 聚合：新增 nodeID 外键
  plan.go                      # Plan 聚合：新增 graphID 外键
  plan_dag.go                  # 迁移到 orchestration/，最终废弃
  plan_view.go                 # 节点状态派生迁移到 orchestration/
  plan_unmerged.go             # CycleNodeRole 等废弃（语义转移到模板文档）
  service/
    plan_scaffold.go           # scaffold_cycle_plan 废弃（Agent + 模板替代）
    assign_flow.go             # guardIntegrateMerge 废弃（Agent 自行检查）
    decision_auto.go           # 废弃（condition 节点替代）
  mergecheck/                  # 保留，转为独立 HTTP endpoint 供 Agent 调用
  gatecheck/                   # 同上
```

### 7.2 Task 字段变更

```
Task {
  // 删除
  - role CycleNodeRole
  - branch string
  - base string
  - skipMergeCheck bool

  // 新增
  + nodeID NodeID   // 关联到 Graph 中的 Node，空则不在任何图中
}
```

### 7.3 Plan 与 Graph 的关系

```
Plan 1:1 Graph     一个 Plan 对应一张编排图
Task N:1 Node      一个 Task 可关联到一个 Node（通过 Task 上的 nodeID 外键）
                   一个 Node 可以没有关联 Task（纯控制节点）
                   一个 Task 可以不关联任何 Node（backlog 任务）
```

### 7.4 数据库变更

新增表：
- `pm_graphs` — Graph 聚合
- `pm_graph_nodes` — Node 实体
- `pm_graph_edges` — Edge
- `pm_graph_node_action_logs` — Node 的 action log

修改表：
- `pm_plans` — 新增 `graph_id` 列
- `pm_tasks` — 新增 `node_id` 列；最终删除 `role/branch/base/skip_merge_check` 列

废弃表（阶段 3 删除）：
- `pm_task_dependencies` — 被 `pm_graph_edges` 替代

### 7.5 迁移阶段

**阶段 1：建引擎，新旧并存**
- 实现 orchestration 子包（Graph/Node/Edge/condition）
- 暴露 MCP tools
- Plan 同时持有旧 DAG 和新 Graph（双写）
- 旧 scaffold/guard/decision 代码不动

**阶段 2：切换到新引擎**
- 新建的 Plan 使用 Graph
- mergecheck/gatecheck 包装为 HTTP endpoint
- cycle 模板 markdown 文档替代 scaffold_cycle_plan
- 旧 DAG 代码标记 deprecated

**阶段 3：清理**
- 删除 plan_dag.go、plan_view.go、plan_unmerged.go
- 删除 scaffold_cycle_plan、guardIntegrateMerge、decision_auto
- Task 上的 role/branch/base/skipMergeCheck 字段删除
- 数据迁移：旧 Plan 的 DAG 数据迁移到 Graph 表
