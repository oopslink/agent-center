# 开发周期编排规则（Cycle Flow Template）

> 本文档是开发周期（cycle）的编排模板。Agent 阅读本文档后，调用编排引擎 MCP tools 建图并推进流程。

## 输入参数

- **version**: 版本号（如 v2.13.0）
- **features**: 功能列表，每个功能包含：
  - name: 功能名称
  - spec: 规格说明（可选）
  - max_review_rounds: 最大评审轮数（默认 3）

## 分支模型

```
main ← dev/{version}（本周期集成主干）← 各 feature 分支（从主干切、合回主干）
```

- `main` 只在"发布"节点收一次
- 所有合并校验必须基于 `origin`（fetch 后判断），禁信本地 stale

## 编排结构

### 1. 创建图

调用 `create_graph`，传入对应 plan 的 ID。

### 2. 创建节点

按以下结构创建节点：

**全局节点：**

| 节点 | 类型 | 说明 |
|---|---|---|
| 开发主分支 | business | 从 main 切 dev/{version} 并推到 origin |
| 集成完成检查 | control (condition) | 所有 feature 集成完成后触发 |
| 验收 | business | 在集成主干上运行完整测试 |
| 发布 | business | dev/{version} → main + tag |

**每个 feature 创建：**

| 节点 | 类型 | 说明 |
|---|---|---|
| {feature.name} - 开发 | business | 从 dev/{version} 切 feature 分支，实现功能 |
| {feature.name} - 评审 | business | code review + 构建/测试门禁 |
| {feature.name} - 评审决策 | control (condition) | 评审是否通过 |
| {feature.name} - 集成 | business | 合并 feature 分支到 dev/{version} |

### 3. 配置 condition 节点

**评审决策（每个 feature）：**
```json
{
  "evaluator": "manual",
  "on_success": ["{feature.name} - 集成"],
  "on_failure": ["{feature.name} - 开发"],
  "max_rounds": 3,
  "on_max_exceeded": "discard"
}
```

Agent 在评审节点完成后，根据评审结果调用 `resolve_condition` 设置 success/failure。

**集成完成检查：**
```json
{
  "evaluator": "upstream_outcome",
  "logic": "and",
  "on_success": ["验收"]
}
```

所有 feature 的集成节点都 outcome=success 后自动通过。

### 4. 建立依赖关系

```
start → 开发主分支

开发主分支 → 所有 feature 的开发节点

每个 feature 内部：
  开发 → 评审 → 评审决策 → 集成

所有集成节点 → 集成完成检查

集成完成检查 → 验收 → 发布 → end
```

### 5. 绑定任务

每个 business 节点使用 `bind_task_to_node` 关联一个 Task。控制节点不需要绑定。

## 节点完成标准（Agent 自行执行的具体检查）

以下检查由 Agent 自行执行，引擎不强制。Agent 应在调用 `complete_graph_node` 前完成这些检查。

### 开发主分支
```bash
# 确认 dev/{version} 已推到 origin
git ls-remote origin dev/{version}
# 应返回非空结果（至少一个 ref）
```

### 开发节点
```bash
# 确认 feature 分支已推到 origin
git ls-remote origin {feature_branch}

# 在 feature 分支上运行构建
git checkout {feature_branch}
make build
```

### 评审节点
```bash
# 在 feature 分支上运行完整门禁（构建 + lint + 类型检查 + 测试）
git checkout {feature_branch}
make build lint
# 前端项目额外运行
pnpm --dir web test

# 确认 code review 意见已解决（Agent 应检查 PR 状态或评审会话记录）
```

### 集成节点
```bash
# 确认 feature 分支已合并到集成主干
git fetch origin
git merge-base --is-ancestor origin/{feature_branch} origin/dev/{version}
# exit 0 = 已合并, exit 1 = 未合并

# 如果未合并，Agent 应执行合并：
git checkout dev/{version}
git pull origin dev/{version}
git merge {feature_branch}
git push origin dev/{version}

# 合并后再次验证
git fetch origin
git merge-base --is-ancestor origin/{feature_branch} origin/dev/{version}
```

### 验收节点
```bash
# 在集成主干上运行完整测试套件
git checkout dev/{version}
git pull origin dev/{version}
make build lint test
# 前端
pnpm --dir web test
```

### 发布节点
```bash
# 合并集成主干到 main
git checkout main
git pull origin main
git merge dev/{version}
git push origin main

# 打 tag
git tag {version}
git push origin {version}
```

## 评审决策的回退行为

当 Agent 对评审决策节点调用 `resolve_condition` 并设置 result=failure 时：

1. 引擎自动将该 feature 的开发节点（以及链路上的评审节点）回退为 reopen 状态
2. 开发者（人或 Agent）重新认领开发节点，修复问题
3. 重新经过评审 → 评审决策流程
4. 达到 max_rounds 上限后，该 feature 的评审决策节点被 discard（功能搁置）

## Agent 执行流程

1. 读取本模板 + 用户提供的 version 和 features 参数
2. 调用编排引擎 MCP tools 建图（create_graph → add_graph_node → add_graph_edge）
3. 调用 start_graph 启动图
4. 轮询 get_ready_nodes 获取可执行节点
5. 对每个 ready 节点：
   - 认领并执行工作
   - 完成后调用 complete_graph_node 设置 outcome
   - 如果是 condition 节点的上游，等待 condition 自动或手动判定
6. 持续推进直到图自动完成（所有业务节点 completed/discarded）
