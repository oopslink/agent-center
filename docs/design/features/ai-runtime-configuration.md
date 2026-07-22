# AI Runtime 统一配置技术方案

| 字段 | 值 |
|---|---|
| 状态 | Proposed |
| 日期 | 2026-07-22 |
| 范围 | Organization CLI Catalog、Model Catalog、Runtime Profile、默认值、Worker 能力匹配、批量导入导出 |

## 1. 背景

当前 CLI 和模型配置分散在多个入口，交互与数据源不一致：

- Agent 配置使用前端硬编码 CLI、硬编码模型建议和自由输入；
- Executor candidate 使用另一套按 CLI 分组的本地模型建议，并允许自由输入；
- Team Role 使用独立的硬编码 CLI / 模型下拉；
- Organization Model Catalog 已支持 CRUD 和 JSON 批量导入，但未成为上述入口的权威数据源；
- Agent profile、Task model routing 等既有逻辑仍直接保存模型字符串。

这会造成同一模型存在不同标识、无效 CLI / Model 组合无法统一校验、默认值重复维护、配置无法可靠跨环境迁移等问题。

本方案将用户可配置的 AI runtime 收敛到一个入口，同时将长期配置与 Worker 的瞬时状态解耦。

## 2. 目标与非目标

### 2.1 目标

1. 用户只在 `Organization Settings > AI Runtime` 定义 CLI、模型、运行参数和组织默认值。
2. Agent、Executor、Team Role 复用同一个选择组件，不再维护本地候选列表。
3. 默认场景零配置：业务对象默认继承组织默认 Runtime Profile。
4. 高级场景可选择其他 Profile 或创建对象级 override。
5. CLI / Model 定义不依赖当前 Worker；调度时才匹配 Worker 实际能力。
6. 配置可批量导入、导出、审阅、审计，并可跨环境迁移。
7. 历史配置和执行记录可解释，不因 Catalog 后续变化而漂移。

### 2.2 非目标

- 不由 Worker 自动创建或修改 Organization Catalog。
- 不让调度器静默替换用户选择的 CLI、模型或参数。
- 第一阶段不新增 Task 级 runtime 选择器；Task 继承最终执行 Agent 的配置。
- 不在 Runtime Catalog 中保存 API key、Token、Worker 本地路径等 Secret 或环境数据。
- 不要求 Worker 枚举云端模型清单；模型通常是传给 CLI 的参数。

## 3. 核心原则

系统将配置、能力和调度分成三个层次：

| 层 | 权威信息 | 生命周期 |
|---|---|---|
| Runtime Catalog | 组织希望使用的 CLI、Model、Profile 和默认值 | 长期、用户管理 |
| Worker Capability Scan | Worker 实际安装的 CLI、版本、feature 和健康状态 | 动态、机器上报 |
| Scheduler | Runtime 要求与 Worker 能力的匹配结果 | 每次调度计算 |

简化表达：

```text
Catalog（期望状态） + Worker Scan（实际能力） -> Scheduler（运行时匹配）
```

因此，无匹配 Worker 不阻止管理员提前保存配置；真正执行时若无法匹配，任务进入明确的等待状态，而不是被静默改配或记为 executor 失败。

## 4. 统一语言

| 术语 | 定义 |
|---|---|
| CLI Definition | 组织允许配置的一类 agent CLI，例如 `codex`、`claude-code` |
| Model Definition | 模型的权威定义，包括传给 CLI 的真实标识及兼容 CLI |
| Runtime Profile | 一个命名的 `CLI + Model + Parameters` 可复用组合 |
| Organization Default Profile | Organization 唯一的默认 Runtime Profile |
| Runtime Selection | Agent、Executor 或 Team Role 对继承、Profile 引用或 override 的选择 |
| Runtime Snapshot | 一次执行实际采用的不可变 CLI、Model 和 Parameters 快照 |
| Worker CLI Capability | Worker 扫描上报的 CLI 安装、版本和 feature |
| Coverage | 当前 Worker 集合对某个 Runtime Profile 的可执行覆盖情况 |

## 5. 领域模型

### 5.1 CLI Definition

```ts
type CliDefinition = {
  id: string
  key: string                 // 稳定键，如 codex；组织内唯一
  display_name: string
  executable: string          // 默认可执行文件名，不是 Worker 绝对路径
  version_constraint?: string // 可选 semver constraint
  required_features: string[]
  parameter_schema: JsonSchema
  enabled: boolean
  created_at: string
  updated_at: string
}
```

约束：

- `key` 是跨环境导入导出的稳定身份，创建后不可修改；数据库 `id` 不跨环境传播。
- `parameter_schema` 定义该 CLI 可接受的用户参数、类型、枚举、默认值和敏感性。
- CLI Definition 可以由系统预置，也可以由 Organization 管理员新增。
- 创建或启用 CLI 时不要求当前存在支持它的 Worker。
- `executable` 仅表示逻辑命令；Worker 上的真实路径由 capability scan 上报。

### 5.2 Model Definition

```ts
type ModelDefinition = {
  id: string
  key: string                    // 稳定键，组织内唯一
  model_key: string              // 传给 CLI 的真实值
  display_name: string
  compatible_cli_keys: string[]
  default_parameters: Record<string, unknown>
  enabled: boolean
  metadata: {
    context_window?: number
    input_cost_per_mtok?: number
    output_cost_per_mtok?: number
    tier?: string
  }
  created_at: string
  updated_at: string
}
```

约束：

- `model_key` 是运行时真实参数，不使用展示别名替代。
- 一个 Model 可兼容多个 CLI；至少关联一个 CLI。
- Model 默认参数必须通过每个关联 CLI 的 `parameter_schema` 校验；存在差异时由 Profile 覆盖。
- Model Catalog 是唯一模型定义源。业务页面不得接受未入 Catalog 的自由文本模型。
- 管理员需要未知模型时，先创建 Model Definition，再在业务配置中选择。

### 5.3 Runtime Profile

```ts
type RuntimeProfile = {
  id: string
  key: string                 // 稳定键，如 default-coding
  name: string
  description?: string
  cli_key: string
  model_key: string           // 引用 ModelDefinition.key
  parameters: Record<string, unknown>
  enabled: boolean
  created_at: string
  updated_at: string
}
```

Profile 保存可复用的有效组合。Organization 另存一个 `default_runtime_profile_id`，并满足：

1. 必须引用启用的 Profile；
2. Organization 必须且只能有一个默认 Profile；
3. Profile 的 Model 必须兼容其 CLI；
4. 合并后的参数必须符合 CLI schema；
5. Profile 可在当前没有匹配 Worker 时创建或设为默认，但 UI 必须展示 coverage warning。

### 5.4 Runtime Selection

Agent、Executor candidate、Team Role 统一保存：

```ts
type RuntimeSelection =
  | { mode: "inherit" }
  | { mode: "profile"; profile_id: string }
  | {
      mode: "override"
      cli_id: string
      model_id: string
      parameters: Record<string, unknown>
    }
```

语义：

- `inherit`：执行时解析 Organization 当前默认 Profile；这是创建对象时的默认值。
- `profile`：显式引用一个 Runtime Profile。
- `override`：高级用户对当前对象显式指定组合，仍然只能引用 Catalog 条目。

对象级 override 不成为新的 Catalog 定义。用户可选择“另存为 Profile”，但这是一个明确且受权限控制的 Catalog 写操作。

### 5.5 Runtime Snapshot

每次创建实际执行时，将解析结果冻结：

```ts
type RuntimeSnapshot = {
  schema_version: 1
  cli_key: string
  cli_executable: string
  cli_version_constraint?: string
  required_features: string[]
  model_key: string              // 传给 CLI 的真实值
  parameters: Record<string, unknown>
  source: "org_default" | "profile" | "override"
  profile_id?: string
  resolved_at: string
}
```

Snapshot 写入 execution record。Catalog、Profile 或默认值后续变化只影响新 execution，不修改已创建 execution 的语义和审计结果。

## 6. 配置解析

后端提供单一 `RuntimeResolver`，所有入口禁止自行拼装 CLI / Model：

```text
RuntimeSelection
  -> 找到 Organization default / Profile / override
  -> 校验 CLI 与 Model 均存在且 enabled
  -> 校验 Model compatible_cli_keys 包含 CLI
  -> 参数合并：CLI schema defaults < Model defaults < Profile/override
  -> 按 CLI parameter_schema 校验最终参数
  -> 生成 RuntimeSnapshot
```

配置来源优先级只有两层：

1. Organization default；
2. Agent / Executor / Team Role 显式选择。

第一阶段 Task 不增加第三层 override。既有 `task.model` 在迁移期继续兼容，但新 UI 不再写入；移除前需要单独确认既有 F3 模型路由语义如何映射到 Profile。

## 7. Worker 能力与调度

### 7.1 Worker 上报

Worker capability scan 上报实际能力：

```ts
type WorkerCliCapability = {
  cli_key: string
  executable_path: string
  version: string
  features: string[]
  scanned_at: string
  healthy: boolean
}
```

Worker 上报只更新 Worker 自身 capability projection，不创建、覆盖或删除 Organization Catalog。

### 7.2 匹配规则

Scheduler 使用 Runtime Snapshot 匹配 Worker：

1. Worker 在线且健康；
2. Worker 上报了相同 `cli_key`；
3. CLI 版本满足 `version_constraint`；
4. Worker features 覆盖 `required_features`；
5. 继续满足现有 team、project、workspace、并发量和调度约束。

模型默认不纳入 Worker capability：模型是传给 CLI 的参数。只有 CLI 的本地 adapter 明确声明某模型需要额外 feature 时，才通过 `required_features` 表达。

### 7.3 无匹配 Worker

无匹配 Worker 时：

- execution 进入 `waiting_for_capability`；
- 保存结构化 reason，例如 `missing_cli`、`version_mismatch`、`missing_feature`；
- UI 展示具体缺失条件；
- Worker 上线或 capability 更新后触发重新调度；
- 不创建一个随后立即失败的 executor；
- 不静默降级到其他 CLI、模型或 Profile。

### 7.4 Coverage

Coverage 是动态只读 projection：

```ts
type RuntimeCoverage = {
  profile_id: string
  online_worker_count: number
  eligible_worker_count: number
  status: "available" | "degraded" | "unavailable"
  reasons: Array<{ code: string; count: number; message: string }>
  calculated_at: string
}
```

Coverage 用于配置页提示和调度诊断，不参与 Catalog 的保存准入。

## 8. Web Console 设计

### 8.1 唯一入口

将现有 Model Catalog 升级为 `Organization Settings > AI Runtime`，包含三个 Tab：

| Tab | 用途 |
|---|---|
| Profiles | 默认首页；管理 Runtime Profile、默认值和 coverage |
| Models | 现有 Model Catalog；增加稳定 key、兼容 CLI、默认参数、启停状态 |
| CLIs | 管理 CLI Definition、版本约束、feature 和参数 schema |

顶部提供 `Import`、`Export`。权限沿用 Organization 管理权限；无管理权限用户只读。

### 8.2 Profile 编辑

编辑顺序固定：

1. 选择 CLI；
2. 选择与该 CLI 兼容且启用的 Model；
3. 根据 CLI `parameter_schema` 渲染参数控件；
4. 展示当前 Worker coverage；
5. 保存，可选设为 Organization default。

CLI 变化后，若当前 Model 不兼容则清空 Model 并要求重新选择，不做隐式替换。

### 8.3 业务选择器

Agent、Executor candidate、Team Role 共用 `RuntimeProfileSelector`：

```text
运行配置
  继承组织默认值（默认编码 / codex / gpt-5.2-codex）
  默认编码
  高质量评审
  低成本任务
  自定义...
```

- 默认选中“继承组织默认值”；
- Profile 行同时展示 CLI / Model，避免只凭名称误选；
- “自定义”位于折叠的高级区域；
- 历史已停用或已删除引用继续可见，并显示“不可用于新执行”；
- 无 coverage 时允许保存，但在选择器和详情页显示 warning；
- 不在业务页面提供自由文本 CLI / Model。

## 9. 批量导入与导出

### 9.1 文件格式

YAML 是默认格式，同时接受等价 JSON。文件必须带 schema version，并以稳定 `key` 建立引用：

```yaml
schema_version: 1
kind: agent-center-ai-runtime
exported_at: "2026-07-22T10:00:00Z"
runtime:
  clis:
    - key: codex
      display_name: Codex
      executable: codex
      enabled: true
      required_features: []
      parameter_schema: {}
  models:
    - key: gpt-5-2-codex
      model_key: gpt-5.2-codex
      display_name: GPT-5.2 Codex
      compatible_cli_keys: [codex]
      enabled: true
      default_parameters: {}
  profiles:
    - key: default-coding
      name: 默认编码
      cli_key: codex
      model_key: gpt-5-2-codex
      enabled: true
      parameters: {}
  default_profile_key: default-coding
```

数据库 ID、Secret、Worker capability、coverage、绝对路径和健康状态不得导出。

### 9.2 导出范围

支持：

- 完整 Runtime Catalog；
- 仅 CLI、Model 或 Profile；
- 列表勾选条目；
- YAML 或 JSON。

部分导出必须自动包含被选条目的依赖，或明确提示产生的是不可独立导入的 partial bundle。默认采用“包含依赖”。

### 9.3 导入流程

导入必须经过四步：

```text
Upload -> Preview -> Confirm -> Atomic Apply
```

Preview 返回逐项 diff：`create`、`update`、`unchanged`、`conflict`、`invalid`、`disable`，并单独突出默认 Profile 变化。

导入策略：

| 策略 | 语义 |
|---|---|
| merge | 默认；按稳定 key 新增或更新，不处理文件中缺失项 |
| create_only | 仅新增，已存在项跳过 |
| replace | 文件成为目标状态；缺失项停用，不硬删除 |

规则：

- Preview 与 Apply 使用同一后端校验器；
- Apply 是单事务，任一错误则整批不落库；
- Preview 返回短期 `validation_token`，Apply 必须携带该 token；
- token 绑定 Organization、文件摘要、策略和 Catalog revision，避免预览后并发覆盖；
- 不支持的更高 schema version 拒绝导入；未知可忽略字段产生 warning；
- 导入不因当前无匹配 Worker 失败，但 Preview 展示导入后的 coverage；
- 每次导入记录操作者、文件 SHA-256、策略、变更摘要和结果。

## 10. API 契约

### 10.1 Catalog

```text
GET    /api/orgs/{org_id}/ai-runtime/clis
POST   /api/orgs/{org_id}/ai-runtime/clis
PATCH  /api/orgs/{org_id}/ai-runtime/clis/{id}

GET    /api/orgs/{org_id}/ai-runtime/models
POST   /api/orgs/{org_id}/ai-runtime/models
PATCH  /api/orgs/{org_id}/ai-runtime/models/{id}

GET    /api/orgs/{org_id}/ai-runtime/profiles
POST   /api/orgs/{org_id}/ai-runtime/profiles
PATCH  /api/orgs/{org_id}/ai-runtime/profiles/{id}
PUT    /api/orgs/{org_id}/ai-runtime/default-profile
```

删除使用受约束的 `DELETE` 或统一停用：被引用项不可硬删除，API 返回引用数量与替换建议。默认 Profile 在切换默认值前不可停用或删除。

### 10.2 解析与 Coverage

```text
POST /api/orgs/{org_id}/ai-runtime/resolve
GET  /api/orgs/{org_id}/ai-runtime/coverage
GET  /api/orgs/{org_id}/ai-runtime/profiles/{id}/coverage
```

`resolve` 主要供服务端应用层复用；若暴露给前端，只返回校验和预览结果，不包含 Secret。

### 10.3 导入导出

```text
GET  /api/orgs/{org_id}/ai-runtime/export?scope=all&format=yaml
POST /api/orgs/{org_id}/ai-runtime/import/preview
POST /api/orgs/{org_id}/ai-runtime/import/apply
```

现有 `/api/model-catalog` 和 `/api/model-catalog/import` 在迁移期保留兼容适配器，内部转调新应用服务。

## 11. 一致性、并发与审计

- Catalog 聚合维护单调递增 `revision`；所有更新支持 optimistic concurrency。
- 设置默认 Profile 与清除旧默认值在同一事务完成。
- Import Apply 锁定或 CAS Catalog revision，避免 lost update。
- Profile、Model、CLI 停用是软状态变化；历史 Snapshot 永不被反向修改。
- 配置变更产生审计事件，至少包含 actor、organization、entity key、before / after 摘要和时间。
- 参数中标记为 secret 的字段不进入 Catalog；应保存 Secret reference，并由执行环境解析。

## 12. 错误模型

统一返回机器可判定的 reason：

| reason | 场景 |
|---|---|
| `runtime_cli_not_found` | CLI 引用不存在 |
| `runtime_model_not_found` | Model 引用不存在 |
| `runtime_model_cli_incompatible` | Model 与 CLI 不兼容 |
| `runtime_parameters_invalid` | 参数不符合 CLI schema |
| `runtime_profile_disabled` | 新执行引用停用 Profile |
| `runtime_default_missing` | Organization 无有效默认 Profile |
| `runtime_catalog_revision_conflict` | 并发修改冲突 |
| `runtime_import_schema_unsupported` | 导入文件版本不支持 |
| `runtime_import_validation_failed` | 导入预检失败 |
| `runtime_worker_capability_unavailable` | 调度时无匹配 Worker |

错误同时携带面向用户的 message 和结构化 details，禁止仅返回自由文本。

## 13. 权限

| 操作 | Organization admin | 普通成员 | Agent runtime |
|---|---:|---:|---:|
| 查看 Catalog / Profile | 是 | 是 | 按需只读 |
| 查看 coverage | 是 | 是 | 是 |
| 修改 Catalog / 默认值 | 是 | 否 | 否 |
| 导入配置 | 是 | 否 | 否 |
| 导出非敏感配置 | 是 | 可按现有组织策略开放 | 否 |
| 选择已有 Profile | 是 | 是 | 否 |
| 创建对象级 override | 是 | 按组织策略 | 否 |

MCP 写工具必须复用相同权限与应用服务，不能绕过 Web API 的校验和审计。

## 14. 迁移方案

### Phase 1：新模型与兼容层

1. 扩展现有 Model Catalog，增加稳定 `key`、`compatible_cli_keys`、默认参数和 `enabled`。
2. 新增 CLI Definition、Runtime Profile、Organization default 和 RuntimeResolver。
3. 用现有硬编码值生成系统预置 CLI / Model / Profile。
4. 保留旧 API，转调新应用服务；读取仍兼容旧字段。

### Phase 2：统一管理界面

1. 将 Model Catalog 页面升级为 AI Runtime 三 Tab。
2. 实现 Profile、默认值、coverage、导入预览和完整导出。
3. 现有 JSON Model import 迁移到 versioned bundle；继续接受旧数组格式并显示 deprecated warning。

### Phase 3：业务接入

1. Agent、Executor candidate、Team Role 增加 `runtime_selection`。
2. 接入共享 `RuntimeProfileSelector`。
3. 迁移旧 `cli/model`：可精确映射时引用 Profile，否则创建命名为 `migrated-*` 的 Profile 或 override。
4. 无法映射的值必须显式列入迁移报告，禁止静默替换。

### Phase 4：调度闭环

1. 执行创建时生成 Runtime Snapshot。
2. Worker scan 按 `cli_key` 上报 capability。
3. Scheduler 实现 capability matching 和 `waiting_for_capability`。
4. capability 更新触发等待任务重新调度。

### Phase 5：清理

1. 停止旧字段双写；
2. 移除前端 `CLI_OPTIONS`、`KNOWN_MODELS` 和 Team Role 本地常量；
3. 移除旧 API 适配器；
4. 评审并迁移既有 `task.model` / modelrouter 逻辑；
5. 发布迁移报告和回滚说明。

## 15. 向后兼容策略

迁移窗口采用“新字段优先、旧字段兜底”：

```text
runtime_selection 存在 -> RuntimeResolver
runtime_selection 不存在且 legacy cli/model 存在 -> LegacyAdapter -> RuntimeSnapshot
两者均不存在 -> Organization default
```

写路径在短迁移期可双写，但必须由单一应用服务完成；前端不得自行维护两套值。所有 legacy fallback 计数进入 observability，计数归零后才能删除兼容代码。

## 16. 测试与验收

### 16.1 Domain / API

- CLI / Model / Profile 唯一 key 与启停约束；
- Model / CLI 兼容校验；
- 参数三层合并与 schema 校验；
- 默认 Profile 唯一性与事务切换；
- Runtime Selection 三种模式解析；
- Snapshot 不受后续 Catalog 修改影响；
- Import preview / apply 一致、原子回滚、revision 冲突；
- merge / create_only / replace 三种策略；
- v1 bundle、未知字段和不支持版本处理；
- 权限和审计事件。

### 16.2 Scheduler

- CLI 缺失、版本不符、feature 缺失均产生准确 reason；
- 无匹配 Worker 进入 `waiting_for_capability`，不创建 executor；
- Worker capability 更新后可自动重调度；
- 不发生隐式 CLI / Model 降级；
- 既有 team、project、workspace、并发约束继续生效。

### 16.3 Web

- 三个业务入口只使用共享 Selector；
- 默认继承、Profile 选择和 override 行为一致；
- CLI 变化清理不兼容 Model；
- coverage warning 不阻止保存；
- 停用 / 历史引用可解释；
- 导入逐项 diff、默认值变更确认和导出范围正确；
- 不再从前端硬编码 CLI / Model 候选项。

### 16.4 核心验收场景

1. 在没有任何在线 Worker 时，管理员可导入完整 Runtime Catalog 并设置默认 Profile。
2. 新建 Team Role 不配置 runtime，创建出的 Agent 继承 Organization default。
3. Worker 上线并上报匹配 CLI 后，等待中的 execution 自动进入调度。
4. 修改 Organization default 后，新 execution 使用新值，旧 execution Snapshot 保持不变。
5. 导出 Organization A 配置后可预检并导入 Organization B，不依赖数据库 ID。
6. 导入 replace 不硬删被引用项，而是停用并在 diff 中明确展示。

## 17. 可观测性

至少提供以下指标：

- `runtime_resolution_total{source,result}`
- `runtime_legacy_fallback_total{object_type}`
- `runtime_waiting_for_capability{reason,cli_key}`
- `runtime_profile_eligible_workers{profile_key}`
- `runtime_import_total{mode,result}`
- `runtime_catalog_revision_conflict_total`

日志和 activity 记录 `profile_key`、`cli_key`、Snapshot source 和失败 reason；不得记录 Secret 参数值。

## 18. 风险与处理

| 风险 | 处理 |
|---|---|
| 组织默认 Profile 当前无 Worker 支持 | 允许保存并强提示；执行进入可恢复等待状态 |
| Catalog 标注兼容但 CLI 实际不支持模型 | adapter 返回结构化启动错误；管理员修正 Catalog，不自动换模型 |
| Profile 修改影响运行中任务 | execution 创建时冻结 Snapshot |
| replace 导入误删配置 | 缺失项仅停用；Preview 单列；原子提交和审计 |
| 历史模型字符串无法映射 | 生成迁移报告和显式 override，不静默替换 |
| 参数 schema 演进 | schema version + Profile 重新校验；旧 Snapshot 保持原样 |
| Worker 状态抖动 | Catalog 不受影响；Scheduler 基于健康窗口和现有重试策略处理 |

## 19. 关键决策摘要

1. **配置定义不依赖 Worker。** Worker 是动态资源，不应决定长期配置能否存在。
2. **Model Catalog 保留并成为唯一模型源。** 它不再是孤立页面，而是 AI Runtime 的基础数据。
3. **Runtime Profile 是主要用户接口。** 普通用户选择有业务名称的组合，而不是重复理解 CLI / Model 参数。
4. **默认采用继承。** Organization 可集中切换新 execution 的默认 runtime。
5. **执行必须冻结 Snapshot。** 保证可审计与可复现。
6. **导入先预检后原子应用。** 跨环境配置使用稳定 key，不使用数据库 ID。
7. **无匹配 Worker 是可恢复调度状态。** 不属于 executor 执行失败，也不触发静默降级。

## 20. 实施拆分

| 工作包 | 内容 | 前置 |
|---|---|---|
| A. Runtime Catalog backend | CLI、Model 扩展、Profile、默认值、Resolver、审计 | 无 |
| B. Import / Export backend | versioned bundle、Preview、Apply、revision CAS | A |
| C. AI Runtime Web | 三 Tab、共享表单、coverage、导入导出 | A、B |
| D. Business integration | Agent、Executor、Team Role 统一 Selection / Selector | A、C |
| E. Scheduler matching | Snapshot、capability matching、等待与自动恢复 | A、D |
| F. Migration cleanup | 数据迁移、移除硬编码、旧 API / 字段退场 | D、E |

实施顺序为 A -> B -> C -> D -> E -> F。每个工作包独立具备迁移开关和回滚路径，不做一次性破坏性切换。
