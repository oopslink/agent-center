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

| 节点 | 类型 | 角色 | 说明 |
|---|---|---|---|
| 验收计划 | business | PD | 制定**完整验收计划**：逐项列出每一个验收项目 + 所有需要验收的细节（是后续 ⑦全真验收 与 ⑧验收报告 的逐项依据） |
| 开发主分支 | business | PD | 从**最新 main** 切 dev/{version} 并推到 origin |
| 集成完成检查 | control (condition) | — | 所有 feature 集成完成后触发 |
| 全真验收 | business | 专职 tester | 在开发主干上**部署多套全真隔离测试环境**（`~/.agent-center-test/<name>/`），**按验收计划逐项验收**，**必须截图保留证据**；发现问题退回开发。见 `docs/rules/acceptance-methodology.md` + `acceptance-checklist.md` |
| 验收报告 | business | PD | 综合所有验收结果，**逐项标明验收计划里每个验收项的验收情况 + 截图证据** |
| 发布 | business | PD | 验收通过后 dev/{version} → main + tag（Ship） |

**每个 feature 创建：**

| 节点 | 类型 | 角色 | 说明 |
|---|---|---|---|
| {feature.name} - 开发 | business | dev | 基于 dev/{version} **拉开发 worktree**，实现功能 + **单测/集成测试** |
| {feature.name} - 评审 | business | **另一 dev（交叉 review）** | code review + 构建/测试门禁 |
| {feature.name} - 评审决策 | control (condition) | — | 评审是否通过；不过退回开发 |
| {feature.name} - 集成 | business | **专职集成者** | 将 feature 分支集成到 dev/{version}（**只做集成 + 解冲突，别的不做**） |

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
  "on_success": ["全真验收"]
}
```

所有 feature 的集成节点都 outcome=success 后自动通过，进入 ⑦全真验收。

### 4. 建立依赖关系

```
start → 验收计划(PD) → 开发主分支(PD)

开发主分支 → 所有 feature 的开发节点

每个 feature 内部：
  开发(dev·worktree) → 评审(另一 dev 交叉) → 评审决策 → 集成(专职集成者)
    评审决策 reject → 退回该 feature 开发

所有集成节点 → 集成完成检查

集成完成检查 → 全真验收(tester·多套全真隔离环境·逐项·截图) → 验收报告(PD) → 发布(PD·Ship) → end

  全真验收 发现问题 → 退回相关 feature 开发（reopen），循环 开发/集成/验收
  验收报告 判定不通过 → 同上退回开发
```

### 5. 绑定任务

每个 business 节点使用 `bind_task_to_node` 关联一个 Task。控制节点不需要绑定。

## 节点完成标准（Agent 自行执行的具体检查）

以下检查由 Agent 自行执行，引擎不强制。Agent 应在调用 `complete_graph_node` 前完成这些检查。

### 验收计划节点（PD）
```
PD 产出【完整验收计划】，作为后续 ⑦全真验收 与 ⑧验收报告 的逐项依据：
- 逐项列出每一个验收项目（一个功能通常拆成多条可独立验证的验收项）
- 每项写清：验什么、在全真环境里怎么验（真实使用路径）、通过的出口标准、要留什么证据（截图/输出）
- 覆盖所有 feature 的功能点 + 关键边界/恢复/回归；宁细勿漏
- 参考 `docs/rules/acceptance-methodology.md`（真实使用、非 parity）+ `acceptance-checklist.md`（逐域 WHAT + 出口标准）
```

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

### 全真验收节点（专职 tester）
> 验收 = 「真实使用」的黑盒测试，**不是** `make test` / 读代码 / parity 推理。**详见 `docs/rules/acceptance-methodology.md`。** 单测 / 集成测 / build-lint 门禁已在 ③开发 与 ④评审 跑过；这里是**独立黑盒全真验收**，不重复跑单测当验收。

```bash
# 0. 集成主干可构建（快速 sanity，非验收本身）
git checkout dev/{version} && git pull origin dev/{version} && make build

# 1. 用 acceptance-checklist.md 的真 install harness 起【多套全真隔离测试环境】
#    实例落在 ~/.agent-center-test/<name>/（与正式环境 ~/.agent-center/ 进程+数据隔离）
#    真装 / 真配 config（不手搓）/ 真起 center+worker+agent / 真 executor / 真浏览器
#    ⛔ 绝不在正式环境上验收（这是 2026-07 复盘定的红线）

# 2. 按【①验收计划】逐项验收：每个验收项真跑用户真实使用路径、端到端产出真副作用
#    ⭐ 每一项都【截图 / 存原始输出】保留证据（无证据 = 未验收）

# 3. 发现问题 → resolve_condition=failure 退回相关 feature 开发；全过 → success

# 4. 跑完清理隔离实例（scripts/agent-center-test-janitor.sh 或手动），
#    别堆积 oversubscribe CPU（复盘记的 load=80 事故）
```

### 验收报告节点（PD）
```
PD 综合所有全真验收结果，产出【验收报告】：
- 逐项对【①验收计划】里每一个验收项标明验收情况（通过 / 不通过）
- 每一项附截图 / 原始证据
- 全部通过 → 放行发布；任一不通过 → 退回开发（走 开发/集成/验收 循环）
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
