# AI Runtime 统一配置技术方案

| 字段 | 值 |
|---|---|
| 状态 | Revised（2026-08-09：Runtime Profile 已退役） |
| 日期 | 2026-07-22；2026-07-29 更新；2026-08-09 Profile 清退 |
| 范围 | Organization CLI Catalog、Model Catalog、Worker 能力匹配、批量导入导出；Runtime Profile、默认 Profile 已移除 |

## 1. 背景

当前 CLI 和模型配置分散在多个入口，交互与数据源不一致：

- Agent 配置使用前端硬编码 CLI、硬编码模型建议和自由输入；
- Executor candidate 使用另一套按 CLI 分组的本地模型建议，并允许自由输入；
- Team Role 使用独立的硬编码 CLI / 模型下拉；
- Organization Model Catalog 已支持 CRUD 和 JSON 批量导入，但未成为上述入口的权威数据源；
- Agent profile、Task model routing 等既有逻辑仍直接保存模型字符串。

这会造成同一模型存在不同标识、无效 CLI / Model 组合无法统一校验、默认值重复维护、配置无法可靠跨环境迁移等问题。

本方案将用户可配置的 AI runtime 收敛到一个入口，同时将长期配置与 Worker 的瞬时状态解耦。

### 1.1 当前实现基线与实施方向修正

截至 2026-07-29，main 已有部分 Catalog、Resolver、Import / Export、审计和管理面实现，但
`selection -> snapshot -> capability -> executor` 运行链尚未形成完整、可恢复、可回滚的闭环。
因此本文不再按 Catalog、Import / Export、Web、全部业务入口、Scheduler 横向铺开，而改为纵向薄切：

1. 先冻结运行契约和当前实现矩阵；
2. 硬化已落地的 Catalog、Snapshot、审计和 Import / Export；
3. 只接 Agent 一条真实运行链；
4. 完成 capability matching 和等待恢复状态机；
5. 运行闭环通过独立真验后，才扩 Team Role、Executor candidate 和完整管理 UI；
6. 最后迁移、切换并清理 legacy。

在第 1–4 步完成前，暂停继续扩大管理面和业务入口。配置面可见不等于执行链可用，实施优先级必须由真实运行闭环决定。

## 2. 目标与非目标

### 2.1 目标

1. 用户只在 `Organization Settings > AI Runtime` 定义组织级 CLI 和模型目录。
2. Agent 的 desired runtime config、Runtime effective config 和 `allowed_executors` 继续直接保存在 Agent 配置中，不经 Runtime Profile 间接绑定。
3. CLI / Model 定义不依赖当前 Worker；调度时才匹配 Worker 实际能力。
4. 配置可批量导入、导出、审阅、审计，并可跨环境迁移。
5. 历史配置和执行记录可解释，不因 Catalog 后续变化而漂移。

### 2.2 非目标

- 不由 Worker 自动创建或修改 Organization Catalog。
- 不让调度器静默替换用户选择的 CLI、模型或参数。
- Task 级 runtime 选择器是否保留不在本设计中预设；既有 `task.model` / F3 modelrouter 的语义和优先级必须在新 Resolver 切流前单独定案。
- 不在 Runtime Catalog 中保存 API key、Token、Worker 本地路径等 Secret 或环境数据。
- 不要求 Worker 枚举云端模型清单；模型通常是传给 CLI 的参数。

## 3. 核心原则

系统将配置、能力和调度分成三个层次：

| 层 | 权威信息 | 生命周期 |
|---|---|---|
| Runtime Catalog | 组织希望使用的 CLI 和 Model 定义 | 长期、用户管理 |
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
| Runtime Selection | 对直接 CLI / Model override 的解析输入；Profile 引用已退役 |
| Runtime Snapshot | 一次执行实际采用的不可变 CLI、Model 和 Parameters 快照 |
| Worker CLI Capability | Worker 扫描上报的 CLI 安装、版本和 feature |
| Basic Capability Coverage | 仅按 CLI、version、features 和健康窗口计算的基础能力覆盖，不代表某个 execution 一定可调度 |
| Effective Schedulability | 在基础能力之外叠加 project、team、workspace、权限、并发和其它运行约束后的特定 execution 可调度性 |

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
- Model 默认参数必须通过每个关联 CLI 的 `parameter_schema` 校验；对象级参数差异由 Agent 直接运行时配置承载。
- Model Catalog 是唯一模型定义源。业务页面不得接受未入 Catalog 的自由文本模型。
- 管理员需要未知模型时，先创建 Model Definition，再在业务配置中选择。

### 5.3 Runtime Profile（已退役）

T1310 起不再提供 Runtime Profile、Organization default Profile、Profile coverage 或 Profile API。
删除迁移会在发现 `ai_runtime_profiles` 行或 `ai_runtime_catalogs.default_profile_id` 绑定时失败，
避免在无法证明安全时破坏真实绑定。

### 5.4 Runtime Selection

Runtime Selection 不再接受 Profile 引用：

```ts
type RuntimeSelection =
  | { mode: "inherit" }
  | {
      mode: "override"
      cli_id: string
      model_id: string
      parameters: Record<string, unknown>
    }
```

语义：

- `inherit`：不再解析 Organization default Profile；没有直接配置时返回 `runtime_default_missing`。
- `override`：高级用户对当前对象显式指定组合，仍然只能引用 Catalog 条目。

Agent CLI / Model 与 `allowed_executors` 是当前生产运行路径的权威配置；Catalog 只提供可管理的 CLI / Model 定义。

### 5.5 Runtime Snapshot

每次创建实际执行时，将解析结果冻结：

```ts
type RuntimeSnapshot = {
  schema_version: 1
  catalog_revision: number
  cli_key: string
  cli_executable: string
  cli_version_constraint?: string
  required_features: string[]
  model_key: string              // 传给 CLI 的真实值
  parameters: Record<string, unknown>
  parameters_digest: string      // canonical parameters 的摘要，不含 Secret 明文
  source: "override"
  resolved_at: string
}
```

Snapshot 在 **TaskExecution 首次创建时**与 execution record 原子写入。Catalog 后续变化只影响新 execution，
不修改已创建 execution 的语义和审计结果。retry、resume 和保留同一 execution 语义的 reassign 必须复用已保存 Snapshot，
不得重新读取可变默认值；只有创建新的 TaskExecution 才重新解析。

Snapshot 中的参数必须是 canonical、确定性编码。Secret 参数只保存 Secret reference，摘要基于脱敏后的 canonical representation
计算；DB 导出、API、审计和日志均不得出现 Secret 明文。

## 6. 配置解析

后端提供单一 `RuntimeResolver`，所有入口禁止自行拼装 CLI / Model：

```text
RuntimeSelection
  -> 读取直接 override
  -> 校验 CLI 与 Model 均存在且 enabled
  -> 校验 Model compatible_cli_keys 包含 CLI
  -> 参数合并：CLI schema defaults < Model defaults < override
  -> 按 CLI parameter_schema 校验最终参数
  -> 生成 RuntimeSnapshot
```

第一条纵向链只接 Agent，配置来源优先级固定为：

1. Agent 直接 runtime config / override；
2. 无直接配置时返回 `runtime_default_missing`。

Team Role 和 Executor candidate 不在第一条链中隐式加入优先级；它们必须在后续各自接入时单独定义继承与覆盖语义。
既有 `task.model` / F3 modelrouter 在迁移期继续兼容，但新 Resolver 切流前必须明确选择以下之一并形成 ADR：

- 保留 Task override，并定义它相对 Agent selection 的优先级及 Snapshot provenance；
- 映射为 Model Definition 引用；
- 只读兼容并明确退场条件。

在该决定落地前，不删除旧字段，不让新旧路由同时静默生效。

### 6.1 CLI Adapter 与参数安全边界

CLI Definition 是 Catalog 配置，不是任意进程启动能力。运行时必须通过受控 `CliAdapterRegistry` 按 `cli_key`
选择代码内注册的 adapter；不得把用户输入的 `executable` 当任意二进制路径执行，也不得把未知参数直接拼入 argv 或 shell。

- `executable` 只用于 adapter 允许的逻辑命令和 capability 对账；
- 参数必须先通过已声明的 JSON Schema 支持子集，再由 adapter 显式映射到 argv / env / config；
- 未支持的关键 schema keyword、未知参数、冲突参数一律 fail closed；
- 禁止 shell 字符串拼接；进程启动使用结构化 argv；
- adapter 负责标记 Secret reference 的解析位置，解析后的 Secret 不进入 Snapshot、日志或审计。

JSON Schema v1 至少明确支持：`type`、`properties`、`required`、`enum`、`const`、数值和字符串边界、
数组 item 校验及 `additionalProperties: false`。其它会改变校验语义的 keyword 未实现时必须拒绝 schema，
不能静默忽略。

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
  expires_at: string
  healthy: boolean
}
```

Worker 上报只更新 Worker 自身 capability projection，不创建、覆盖或删除 Organization Catalog。
上报必须绑定已认证 Worker 身份并拒绝跨 Worker 写入；Center 负责解析和规范化版本，使用 TTL 与健康窗口判断新鲜度。
过期、身份不匹配、版本不可解析或健康探测失败的 capability 不得参与匹配。

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
- 不静默降级到其他 CLI 或模型。

`waiting_for_capability` 是 TaskExecution 的持久化状态，不是临时队列标签。状态机必须定义：

| 触发 | 行为 |
|---|---|
| 首次无匹配 Worker | 原子保存 Snapshot、reason 和等待状态；executor 数保持 0 |
| capability 新增或恢复 | 在同一 execution 上以幂等键重新驱动；最多创建一个 executor |
| capability 抖动或重复上报 | 合并重复信号，不重复 dispatch |
| Center / Worker 重启 | 从持久化等待状态恢复订阅和重驱动能力 |
| Task 取消或进入终态 | 退出等待并禁止后续 capability 信号复活 |
| reassign | 重算 effective schedulability，但复用原 Snapshot；不重复创建 executor |
| 等待超时 | 保持可解释的结构化 reason，按策略升级人工处理，不伪装为 executor failure |

重新驱动必须使用 durable idempotency key（至少包含 execution id 与 snapshot identity），并在 executor 创建前以原子状态转换守门。

### 7.4 Capability Diagnostics

Profile coverage projection 已退役。面向具体 execution 的页面和 API 必须使用 Effective Schedulability，
叠加 team、project、workspace、权限、并发量和其它现有调度约束，并明确展示阻断 reason。

## 8. Web Console 设计

### 8.1 唯一入口

将现有 Model Catalog 升级为 `Organization Settings > AI Runtime`，包含两个 Tab：

| Tab | 用途 |
|---|---|
| Models | 现有 Model Catalog；增加稳定 key、兼容 CLI、默认参数、启停状态 |
| CLIs | 管理 CLI Definition、版本约束、feature 和参数 schema |

顶部提供 `Import`、`Export`。权限沿用 Organization 管理权限；无管理权限用户只读。旧 `tab=profiles` URL 不再渲染 Profile UI。

### 8.3 业务选择器

Agent、Executor candidate、Team Role 不再共用 Runtime Profile 选择器。Agent 配置继续直接编辑 CLI、Model、reasoning、mode、
provider 与 `allowed_executors`。

```text
运行配置
  CLI
  Model
  allowed_executors
```

- 不在业务页面提供 Runtime Profile 选择；
- `allowed_executors` 是 Agent CLI/Model 选择和执行路由的权威候选池；
- 不在业务页面提供未受控的 Runtime Profile 绑定。

#### 8.3.1 当前 legacy Agent CLI / Model selector 契约（T1305）

在 `runtime_selection` 字段和 Snapshot 运行链切入前，Agent 配置页先使用共享 selector 数据层读取
AI Runtime Catalog，并继续写 legacy Agent profile 字段：

| 数据 | 权威边界 | 用途 |
|---|---|---|
| Runtime CLI / Model Catalog | `GET /api/orgs/{slug}/ai-runtime` 中的 `clis[]` / `models[]` | 唯一 CLI / Model 候选来源；CLI 按 `key` 去重；Model 按运行时 `model_key` 输出并按 `compatible_cli_keys` 过滤 |
| Agent profile legacy fields | `Agent.profile.cli/model/allowed_executors` | Agent 当前 / effective config 的回显与写入目标；`model` 和 `allowed_executors[].model` 保存运行时真实 model string，不保存 catalog stable key |
| Agent runtime effective config | `get_agent_runtime_effective_config` / Environment read model | 只用于 desired vs observed 诊断；不得作为选择器候选来源，也不得把 observed 值反写 Catalog |
| Worker capability scan | Environment/Fleet capability projection | 只用于 coverage / schedulability 提示；不创建、不修改、不删除 Catalog 条目 |

Selector 语义：

- 共享 selector / hook 的输入只允许依赖 `clis[]`、`models[]` 与 Agent 当前 `cli/model/allowed_executors`；T1310 清退 Runtime Profile 后，`default_runtime_profile_id` / `profiles[]` 字段可以从 API 类型中消失，不得影响 selector 行为；
- 初始默认值不读取 Runtime Profile；当 Agent 当前值为空时，按 Catalog 返回顺序选择第一个 enabled CLI 及其第一个 enabled 且兼容的 Model，作为确定性 UI 默认；
- 若 Agent 已有 legacy 值，优先回显该值；Catalog 中已删除/停用/不兼容的当前值保留为不可选项，保存时必须改成有效组合；
- CLI 变化后，若当前 Model 与新 CLI 不兼容，清空 Model 并要求用户重新选择；不得自动替换成第一个兼容模型；
- Executor candidate 添加行复用同一 CLI / Model 过滤与搜索逻辑；提交前按 `{cli, model}` 去重，服务端再次规范化；
- Catalog loading / error / empty 都是显式状态；用户可刷新 Catalog；empty 状态不得回落到硬编码候选或自由文本；
- 并发配置变化（如 max_concurrent_tasks 改变、allowed_executors 清空）只影响 Agent profile legacy 字段；是否启用并发仍由服务端单一谓词 `max_concurrent_tasks>0 && allowed_executors non-empty` 判定；
- 并发 Catalog 变化由服务端组合校验兜底：`POST /members/agent` 与 `PATCH /agents/{id}/config` 必须校验主 `cli/model` 与每个 `allowed_executors[]` 均存在、enabled 且兼容；失败返回 `runtime_*` reason 与 details。

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
```

数据库 ID、Secret、Worker capability、绝对路径和健康状态不得导出。`profiles` 与 `default_profile_key` 字段已退役，导入时拒绝。

### 9.2 导出范围

支持：

- 完整 Runtime Catalog；
- 仅 CLI 或 Model；
- 列表勾选条目；
- YAML 或 JSON。

部分导出必须自动包含被选条目的依赖，或明确提示产生的是不可独立导入的 partial bundle。默认采用“包含依赖”。

### 9.3 导入流程

导入必须经过四步：

```text
Upload -> Preview -> Confirm -> Atomic Apply
```

Preview 返回逐项 diff：`create`、`update`、`unchanged`、`conflict`、`invalid`、`disable`。

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
- 导入不因当前无匹配 Worker 失败；
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
```

旧 `/ai-runtime/profiles` 与 `/ai-runtime/default-profile` 路由不注册，返回 404。

### 10.2 解析

```text
POST /api/orgs/{org_id}/ai-runtime/resolve
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
- Import Apply 锁定或 CAS Catalog revision，避免 lost update。
- Model、CLI 停用是软状态变化；历史 Snapshot 永不被反向修改。
- 配置变更产生审计事件，至少包含 actor、organization、entity key、before / after 深拷贝摘要和时间；
  `before` 与 `after` 不得共享可变 map / slice，也不得因写后修改互相覆盖。
- 参数中标记为 secret 的字段不进入 Catalog；应保存 Secret reference，并由执行环境解析。
- 修改 Model 或 CLI 前返回引用数量和影响预览；批量切换支持灰度范围与审计。
- 停用 Model 或 CLI 时，已冻结 Snapshot 可继续按原语义重试 / resume；尚未创建 execution 的排队工作按新 Catalog
  重新解析并在不可用时 fail closed，不得一部分读旧值、一部分读新值。

## 12. 错误模型

统一返回机器可判定的 reason：

| reason | 场景 |
|---|---|
| `runtime_cli_not_found` | CLI 引用不存在 |
| `runtime_model_not_found` | Model 引用不存在 |
| `runtime_model_cli_incompatible` | Model 与 CLI 不兼容 |
| `runtime_parameters_invalid` | 参数不符合 CLI schema |
| `runtime_default_missing` | 未提供直接 override 且没有可继承默认运行时 |
| `runtime_catalog_revision_conflict` | 并发修改冲突 |
| `runtime_import_schema_unsupported` | 导入文件版本不支持 |
| `runtime_import_validation_failed` | 导入预检失败 |
| `runtime_worker_capability_unavailable` | 调度时无匹配 Worker |

错误同时携带面向用户的 message 和结构化 details，禁止仅返回自由文本。

## 13. 权限

| 操作 | Organization admin | 普通成员 | Agent runtime |
|---|---:|---:|---:|
| 查看 Catalog | 是 | 是 | 按需只读 |
| 修改 Catalog | 是 | 否 | 否 |
| 导入配置 | 是 | 否 | 否 |
| 导出非敏感配置 | 是 | 可按现有组织策略开放 | 否 |
| 创建对象级 override | 是 | 按组织策略 | 否 |

MCP 写工具必须复用相同权限与应用服务，不能绕过 Web API 的校验和审计。

## 14. 迁移方案

### Phase 1：新模型与兼容层

1. 扩展现有 Model Catalog，增加稳定 `key`、`compatible_cli_keys`、默认参数和 `enabled`。
2. 新增 CLI Definition 和 RuntimeResolver。
3. 用现有硬编码值生成系统预置 CLI / Model。
4. 保留旧 API，转调新应用服务；读取仍兼容旧字段。

### Phase 2：统一管理界面

1. 将 Model Catalog 页面升级为 AI Runtime 两 Tab。
2. 实现 Model / CLI、导入预览和完整导出。
3. 现有 JSON Model import 迁移到 versioned bundle；继续接受旧数组格式并显示 deprecated warning。

### Phase 3：Agent 单入口纵向接入

1. Agent 保持直接 runtime config 与 `allowed_executors`，不接入 Runtime Profile。
2. 在 feature flag 下完成 Agent selection 到 TaskExecution Snapshot，再到 scheduler 和 supervisor / executor 启动参数的真实链路。
3. legacy 路径继续可用，同时 shadow resolve 新旧结果并记录差异；不双写两套独立语义。
4. Team Role 与 Executor candidate 留到运行闭环通过后分别接入。

### Phase 4：调度闭环

1. TaskExecution 创建时原子生成 Runtime Snapshot。
2. Worker scan 按 `cli_key` 上报 capability。
3. Scheduler 实现 capability matching 和 `waiting_for_capability`。
4. capability 更新触发等待 execution exactly-once 重新调度。
5. 隔离实例验证无能力时 executor=0，能力恢复后 executor=1，重启、取消、reassign 和能力抖动不重复执行。

### Phase 5：扩入口、迁移与切换

1. 运行闭环通过后，先接 Team Role，再接 Executor candidate；每个入口单独真验。
2. 对生产等价数据 dry-run：精确映射、对象 override、无法映射三类报告。
3. 先 shadow compare，再小范围切新 Resolver；每一步均可通过 flag 回旧读路径。
4. 既有 `task.model` / modelrouter 按已确认 ADR 迁移。

### Phase 6：清理

1. legacy fallback 指标连续一个发布窗口为 0，且迁移报告无未决项后停止兼容写入；
2. 移除前端 `CLI_OPTIONS`、`KNOWN_MODELS` 和 Team Role 本地常量；
3. 移除旧 API adapter 和旧字段；
4. 发布迁移报告和回滚说明；
5. 清理提交独立部署级验收，确认 retry / resume 和历史 execution 仍可读。

## 15. 向后兼容策略

迁移窗口采用“新字段优先、旧字段兜底”：

```text
runtime_selection 存在 -> RuntimeResolver
runtime_selection 不存在且 legacy cli/model 存在 -> LegacyAdapter -> RuntimeSnapshot
两者均不存在 -> runtime_default_missing
```

写路径在短迁移期可双写，但必须由单一应用服务完成；前端不得自行维护两套值。所有 legacy fallback 计数进入 observability，计数归零后才能删除兼容代码。

## 16. 测试与验收

### 16.1 Domain / API

- CLI / Model 唯一 key 与启停约束；
- Model / CLI 兼容校验；
- 参数合并与 schema 校验；
- Runtime Selection override / inherit 解析；
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

- AI Runtime 管理面只展示 Models / CLIs；
- 旧 Profile tab URL 不再展示 Profile UI；
- CLI 变化清理不兼容 Model；
- 停用 / 历史引用可解释；
- 导入逐项 diff 和导出范围正确；
- 不再从前端硬编码 CLI / Model 候选项。

### 16.4 核心验收场景

1. 在没有任何在线 Worker 时，管理员可导入完整 Runtime Catalog。
2. Agent 直接 runtime config 与 `allowed_executors` 不受 AI Runtime Profile 清退影响。
3. Worker 上线并上报匹配 CLI 后，等待中的 execution 自动进入调度。
4. 修改 Agent direct runtime config 后，新 reconcile 使用新值，旧 execution Snapshot 保持不变。
5. 导出 Organization A 配置后可预检并导入 Organization B，不依赖数据库 ID。
6. 导入 replace 不硬删被引用项，而是停用并在 diff 中明确展示。

## 17. 可观测性

至少提供以下指标：

- `runtime_resolution_total{source,result}`
- `runtime_legacy_fallback_total{object_type}`
- `runtime_waiting_for_capability{reason,cli_key}`
- `runtime_import_total{mode,result}`
- `runtime_catalog_revision_conflict_total`

日志和 activity 记录 `cli_key`、Snapshot source 和失败 reason；不得记录 Secret 参数值。

## 18. 风险与处理

| 风险 | 处理 |
|---|---|
| Catalog 标注兼容但 CLI 实际不支持模型 | adapter 返回结构化启动错误；管理员修正 Catalog，不自动换模型 |
| replace 导入误删配置 | 缺失项仅停用；Preview 单列；原子提交和审计 |
| 历史模型字符串无法映射 | 生成迁移报告和对象显式 override |
| 参数 schema 演进 | schema version + Model 重新校验；旧 Snapshot 保持原样 |
| Worker 状态抖动 | Catalog 不受影响；Scheduler 基于 TTL、健康窗口、幂等重驱动和原子 executor 创建处理 |
| 任意 CLI / 参数注入 | 受控 adapter registry + 结构化 argv + schema 支持子集 fail closed |
| Coverage 数字被误读为可调度承诺 | 文案区分 Basic Capability Coverage 与 Effective Schedulability |

## 19. 关键决策摘要

1. **配置定义不依赖 Worker。** Worker 是动态资源，不应决定长期配置能否存在。
2. **Model Catalog 保留并成为唯一模型源。** 它不再是孤立页面，而是 AI Runtime 的基础数据。
3. **Runtime Profile 已退役。** Agent 直接持有 desired runtime config 和 `allowed_executors`。
4. **不再维护 Organization default Profile。** 没有直接配置时 fail closed，不静默选择。
5. **执行必须冻结 Snapshot。** 保证可审计与可复现。
6. **导入先预检后原子应用。** 跨环境配置使用稳定 key，不使用数据库 ID。
7. **无匹配 Worker 是可恢复调度状态。** 不属于 executor 执行失败，也不触发静默降级。
8. **CLI 启动经受控 adapter。** Catalog 不授予任意进程或参数执行能力。
9. **Coverage 不等于可调度。** 组织级基础能力数字不能替代 execution 级完整约束判断。
10. **先闭合运行链，再扩管理面。** 实施按纵向薄切推进，不按页面或实体横向铺开。

## 20. 实施拆分

| 阶段 | 内容 | 放行结果 |
|---|---|---|
| 0. 重新基线与契约 | 现状矩阵；Snapshot、优先级、`task.model`、Secret、adapter、等待状态机契约 | 无关键语义待定 |
| 1. 硬化基础层 | Snapshot provenance；审计深拷贝与脱敏；schema fail closed；Import / Export 并发与回滚 | 历史 execution 不漂移 |
| 2. Agent 纵向链 | Agent selection -> Snapshot -> scheduler -> supervisor / executor 参数 | flag ON 真执行，flag OFF 可回旧路径 |
| 3. 能力与等待闭环 | capability 身份 / TTL；matching；`waiting_for_capability`；exactly-once re-drive | executor 0 -> 1，重启 / 抖动不重复 |
| 4. UI 与其它入口 | AI Runtime 管理面；Team Role；Executor candidate；影响预览 | 每扩一个入口单独真验 |
| 5. 迁移与切换 | dry-run、shadow compare、灰度切读源 | 可回退且无静默替换 |
| 6. 清理 | fallback 归零后删旧字段、API、常量和 adapter | 历史 execution 与恢复路径仍可用 |

详细任务、依赖、测试和回滚门见
[AI Runtime 统一配置实施 Plan](../../plans/2026-07-22-ai-runtime-configuration-implementation.md)。
