# AI Runtime 统一配置实施 Plan

> 设计文档：[AI Runtime 统一配置技术方案](../design/features/ai-runtime-configuration.md)
>
> 日期：2026-07-22
>
> 目标：将 CLI、Model、Runtime Profile 和组织默认值收敛到 AI Runtime 单一入口；业务对象统一引用配置；调度时匹配 Worker 能力；支持完整批量导入导出，并通过迁移与端到端验收。

## 1. 交付原则

- 按依赖顺序执行，上一阶段出口未通过不得进入下一阶段。
- 新旧路径采用兼容迁移，不做一次性破坏性切换。
- Catalog 定义不依赖 Worker；Worker capability 只在 coverage 和调度时使用。
- 所有业务入口经同一个 RuntimeResolver，不允许复制解析逻辑。
- 每个实现任务必须包含单元测试；共享契约变更必须包含集成测试。
- 最终验收必须覆盖空 Worker、Worker 上线恢复、历史数据迁移和跨环境导入导出。
- 每个交付分支从当时最新 `origin/main` 创建；合入前核验 merge-base、测试和迁移可逆性。

## 2. 阶段与依赖

```text
P0 Contract baseline
  -> P1 Runtime Catalog domain + persistence
  -> P2 Resolver + legacy adapter
  -> P3 Import / Export
  -> P4 AI Runtime Web
  -> P5 Agent / Executor / Team Role integration
  -> P6 Worker capability + Scheduler matching
  -> P7 Migration + cleanup
  -> P8 Acceptance + release gate
```

P3 可在 P2 完成后与 P4 的静态页面骨架并行；P5、P6、P7 必须按顺序执行。P8 是独立验收关卡，不与功能实现合并。

## 3. P0：契约基线与特性开关

### Task P0.1：现状基线与字段清单

产出：

- 记录现有 Model Catalog schema、API、MCP tools 和 import 行为；
- 记录 Agent、Executor candidate、Team Role、Task model 的所有读写点；
- 记录 Worker CLI scan payload 与 Scheduler 当前筛选条件；
- 建立旧值映射清单，包括 `opus-4.8` 与 `claude-opus-4-8` 等别名。

验收：

- [ ] 清单覆盖后端 domain / persistence / API / MCP、前端和迁移；
- [ ] 每个旧字段均标出 owner、读路径、写路径和退场阶段；
- [ ] 至少采集一份生产或等价测试数据的去敏样本验证映射。

### Task P0.2：共享契约与 feature flag

产出：

- 定义 CLI Definition、Model Definition、Runtime Profile、Runtime Selection、Runtime Snapshot DTO；
- 定义统一 reason codes；
- 增加 `ai_runtime_catalog_v2` 和 `ai_runtime_scheduler_matching` 开关；
- 确定 Catalog revision / optimistic concurrency 契约。

测试：

- DTO JSON round-trip；
- reason code 稳定性；
- feature flag 默认关闭时旧行为不变。

出口：

- [ ] OpenAPI / Go / TypeScript 契约命名一致；
- [ ] 不引入第二套 `model_key` 语义；
- [ ] 设计文档中的所有核心类型都有明确代码 owner。

## 4. P1：Runtime Catalog Domain 与持久化

### Task P1.1：CLI Definition

实现：

- CLI Definition aggregate、repository、application service；
- 稳定 `key`、display name、executable、version constraint、required features、parameter schema、enabled；
- 系统预置 CLI seed；
- Organization 隔离与权限校验。

测试：

- key 唯一、不可修改；
- schema 合法 / 非法；
- 启停与组织隔离；
- 当前无 Worker 时仍可创建和启用。

### Task P1.2：扩展 Model Catalog

实现：

- 增加稳定 `key`、`compatible_cli_keys`、`default_parameters`、`enabled`；
- 保留现有成本、上下文和 tier 字段；
- 旧 `model_id` 明确迁移为运行时 `model_key`，禁止展示别名替代真实值；
- 现有 CRUD / MCP tools 转接新 application service。

测试：

- Model 至少兼容一个存在的 CLI；
- 多 CLI 兼容；
- 默认参数 schema 校验；
- 旧 API 请求与响应兼容。

### Task P1.3：Runtime Profile 与默认值

实现：

- Runtime Profile aggregate、repository、CRUD；
- Organization `default_runtime_profile_id`；
- CLI / Model 兼容与参数校验；
- 被引用项停用 / 删除保护；
- Catalog revision 与审计事件。

测试：

- Organization 恰有一个有效默认 Profile；
- 默认值原子切换；
- 不兼容 CLI / Model 拒绝；
- 被引用 Profile / Model / CLI 不可硬删除；
- 并发 revision 冲突不覆盖新数据。

P1 出口：

- [ ] Catalog API 在无 Worker 环境可完成 CLI、Model、Profile 和默认值全流程；
- [ ] 所有写操作有权限校验和审计；
- [ ] migration up / down 在空库和带旧数据数据库均通过；
- [ ] `go test ./...` 通过，新 domain / service 代码行覆盖率不低于 90%。

## 5. P2：RuntimeResolver、Snapshot 与兼容层

### Task P2.1：RuntimeResolver

实现单一解析链：

```text
Selection -> default/profile/override
 -> CLI + Model enablement
 -> compatibility
 -> CLI defaults < Model defaults < Profile/override
 -> JSON Schema validation
 -> immutable RuntimeSnapshot
```

测试矩阵：

- inherit / profile / override 三种来源；
- 各层参数覆盖；
- disabled、not found、incompatible、invalid parameters；
- Snapshot source 与 profile provenance；
- Catalog 后续修改不改变已保存 Snapshot。

### Task P2.2：LegacyAdapter

实现：

- 旧 `cli/model` 到 Catalog / Profile 的解析；
- 无法精确映射时生成结构化迁移问题，不静默替换；
- `runtime_selection` 优先、legacy fallback 次之、组织默认最终兜底；
- legacy fallback 指标。

测试：

- 已知别名精确映射；
- 未知模型保留原始值并报告；
- 新旧字段同时存在时新字段胜出；
- feature flag 关闭时旧路径完全兼容。

P2 出口：

- [ ] 所有新执行可生成完整 Snapshot；
- [ ] Resolver 不读取 Worker 状态；
- [ ] LegacyAdapter 覆盖现有数据样本；
- [ ] 错误均含 reason、message 和 details。

## 6. P3：批量导入与导出

### Task P3.1：Versioned Bundle 与导出

实现：

- `agent-center-ai-runtime` schema v1；
- YAML 默认、JSON 可选；
- all / CLI / Model / Profile / selected scopes；
- 部分导出默认包含依赖；
- 只使用稳定 key 引用；
- 排除 Secret、DB ID、Worker capability、路径和健康状态。

测试：

- YAML / JSON 确定性输出；
- 完整与部分依赖闭包；
- 导出内容无敏感字段；
- export -> import -> export 语义等价。

### Task P3.2：Preview 与 Atomic Apply

实现：

- preview diff：create / update / unchanged / conflict / invalid / disable；
- merge / create_only / replace；
- replace 只停用缺失项，不硬删除；
- validation token 绑定 org、文件摘要、mode、Catalog revision；
- apply 单事务；
- 导入审计。

测试：

- 任一非法条目导致整批不落库；
- preview 后 revision 变化使 token 失效；
- 默认 Profile 变化被单独标记；
- 更高 schema version 拒绝；
- v1 未知可忽略字段 warning；
- 无 Worker 不阻止 apply。

### Task P3.3：旧 Model Catalog Import 兼容

实现：

- 接受现有 JSON array upsert / replace；
- 转换为 v1 bundle 后走同一 validator / transaction；
- 返回 deprecated warning 和迁移提示。

P3 出口：

- [ ] 跨两个 Organization 的导出导入成功且不依赖 DB ID；
- [ ] preview 与 apply 使用同一校验结果；
- [ ] replace 不导致引用悬空；
- [ ] 导入失败后数据库逐表一致性校验通过。

## 7. P4：AI Runtime Web Console

### Task P4.1：页面与导航

实现 `Organization Settings > AI Runtime`：

- Profiles 默认 Tab；
- Models Tab 升级现有 Model Catalog；
- CLIs Tab；
- 管理员编辑、普通成员只读；
- coverage 摘要与无 Worker warning。

### Task P4.2：Profile 与 schema-driven 参数编辑

实现：

- CLI -> compatible Model 联动；
- CLI schema 驱动参数输入控件；
- 设置默认值；
- CLI 改变后清理不兼容 Model 并要求确认；
- 禁用项和引用保护提示。

### Task P4.3：Import / Export UX

实现：

- 文件上传与 YAML / JSON parse error；
- diff 预览、策略选择、默认值变化二次确认；
- validation token apply；
- 完整 / 分类 / 多选导出。

测试：

- React component / API integration tests；
- loading、empty、error、permission denied、revision conflict；
- 键盘操作和表单可访问性；
- desktop 1440x900、mobile 390x844 截图检查；
- 长 CLI / Model / Profile 名称不溢出、不重叠。

P4 出口：

- [ ] 用户可只在 AI Runtime 完成所有定义和默认值操作；
- [ ] 当前无 Worker 时可保存，warning 明确但不阻断；
- [ ] 导入不会上传即覆盖，必须经过 preview / confirm；
- [ ] `pnpm test`、typecheck、production build 通过。

## 8. P5：业务入口统一

### Task P5.1：共享 RuntimeProfileSelector

实现：

- inherit / profile / override 三态；
- 默认 Profile 摘要；
- Profile 显示 name、CLI、Model；
- override 位于高级设置；
- disabled / historical reference 和 coverage warning。

### Task P5.2：Agent 接入

- Agent create / edit 使用共享 Selector；
- 主 runtime 配置改存 Runtime Selection；
- 旧值通过 LegacyAdapter 展示和迁移；
- 移除 Agent 页面 `KNOWN_MODELS` / 本地 CLI 列表依赖。

### Task P5.3：Executor candidate 接入

- candidate 配置改存 Runtime Selection；
- 每个 candidate 可继承、选 Profile 或 override；
- 保持并发、候选排序和 fallback 的现有业务语义；
- 移除按 CLI 的本地模型建议映射。

### Task P5.4：Team Role 接入

- Role 定义使用共享 Selector；
- 从 Role 创建 Agent 时保留 selection 语义；
- Role 编辑 / 删除与 Runtime Profile 引用保护一致；
- 移除 Team Role 硬编码 CLI / Model 常量。

P5 集成验收：

- [ ] 三个入口组件和 API 均使用同一 Runtime Selection 契约；
- [ ] 新对象默认保存 inherit，不复制当时默认值；
- [ ] 修改组织默认值后，inherit 对象的新 execution 使用新配置；
- [ ] 显式 Profile / override 不受组织默认切换影响；
- [ ] 前端源码不再存在业务 CLI / Model 候选硬编码；
- [ ] 旧数据仍可查看、编辑和执行。

## 9. P6：Worker Capability 与 Scheduler

### Task P6.1：Capability Scan 契约

实现：

- Worker 上报 `cli_key`、executable path、version、features、health、scanned_at；
- Center 保存 capability projection；
- CLI 未入 Catalog 时仍可保留 scan 事实，但不自动创建 Catalog。

### Task P6.2：Coverage Projection

实现：

- Profile eligible Worker 统计；
- available / degraded / unavailable；
- missing_cli / version_mismatch / missing_feature reasons；
- capability 或 Worker health 变化时刷新。

### Task P6.3：Scheduler Matching

实现：

- 读取 Runtime Snapshot，不读取可变 Profile；
- 匹配健康、CLI、版本、features，再叠加现有 team / project / workspace / concurrency 条件；
- 无匹配 Worker 进入 `waiting_for_capability`；
- capability 更新触发重试；
- 禁止隐式切换 CLI / Model。

测试：

- 精确匹配、多 Worker、版本边界、feature 子集；
- 无 CLI / 版本不符 / feature 缺失 reason；
- 等待时不创建 executor；
- Worker 上线后自动恢复；
- Worker 再次离线时不破坏 Catalog；
- 原有调度约束回归。

P6 出口：

- [ ] 配置期与 Worker 状态完全解耦；
- [ ] coverage 与 Scheduler 使用同一 capability matcher；
- [ ] waiting 状态可观测、可恢复、不计 executor failure；
- [ ] Scheduler 并发与重复调度测试通过。

## 10. P7：数据迁移与旧路径清理

### Task P7.1：迁移工具

实现 dry-run 和 apply：

- 扫描旧 Agent / candidate / Role / Task 模型字段；
- 生成 alias mapping、Profile / override 建议；
- 输出 mapped / ambiguous / unknown / invalid 报告；
- apply 可重复执行且幂等；
- 保存迁移批次与回滚数据。

### Task P7.2：灰度与双读退出

步骤：

1. 开启 Catalog v2，保留旧执行路径；
2. 导入 seed 和运行 dry-run；
3. apply 数据迁移；
4. 开启新 Resolver / Selector；
5. 观察 legacy fallback 指标；
6. fallback 归零后停止双写；
7. 删除旧 API adapter、字段和硬编码。

### Task P7.3：Task model routing 决策

既有 `task.model` 与 modelrouter 不得顺手删除。单独完成：

- 确认 Task override 是否保留；
- 若保留，改为引用 Runtime Profile 或 Model Definition，并定义优先级；
- 若推迟，旧字段只读兼容并记录退场条件；
- 补齐对 F3 路由用例的回归测试。

P7 出口：

- [ ] 生产等价数据 dry-run 无未解释 unknown；
- [ ] apply 两次结果一致；
- [ ] rollback 演练可恢复旧读路径；
- [ ] legacy fallback 连续观察窗口为零；
- [ ] 全仓检索确认硬编码和旧写路径已清除或有明确保留说明。

## 11. P8：独立验收流程

P8 由未参与主要实现的 reviewer / QA 执行。功能实现完成只可标记 delivered，必须通过本阶段才能完成 Plan。

### Gate A：代码与迁移审查

- [ ] 每个交付 commit 均基于预期 `main` baseline；
- [ ] schema migration 可在空库、旧库执行，down / rollback 策略已验证；
- [ ] Catalog、Resolver、Importer、Matcher 没有重复业务规则；
- [ ] 权限、审计、reason + message、Secret 边界符合规约；
- [ ] Go / Web 新代码测试覆盖达到项目标准。

### Gate B：自动化测试

必须记录命令、版本、结果和日志位置：

```bash
go test ./...
go vet ./...
cd web && pnpm test
cd web && pnpm typecheck
cd web && pnpm build
```

另运行：

- migration integration suite；
- import/export round-trip suite；
- scheduler capability matching suite；
- legacy compatibility suite；
- API authorization suite。

全部必须 0 failure；禁止以“非本次变更”忽略失败，需先归因并记录处理决定。

### Gate C：端到端业务验收

准备 Organization A / B、管理员 / 普通成员、至少两个 Worker：

1. **空 Worker 配置**：无在线 Worker 时创建 CLI、Model、Profile 并设默认值，保存成功且显示 unavailable warning。
2. **默认继承**：创建 Team Role 和 Agent，不显式选 runtime；确认保存 inherit。
3. **等待能力**：触发任务；确认 execution 为 `waiting_for_capability`，reason 准确且未创建 executor。
4. **自动恢复**：启动支持目标 CLI 的 Worker；确认无需人工改配置即可自动调度。
5. **Snapshot 冻结**：execution 创建后修改默认 Profile；旧 execution 保持原 Snapshot，新 execution 使用新默认值。
6. **显式覆盖**：Agent、Executor candidate、Team Role 分别选择 Profile 和 override，确认解析来源正确。
7. **兼容校验**：选择不兼容 CLI / Model、非法参数，前后端均拒绝并返回相同 reason。
8. **跨环境迁移**：A 导出 YAML，B preview 后 merge 导入；确认稳定 key 引用、默认值和语义一致。
9. **原子失败**：在 bundle 中加入一个非法条目，确认整批无任何落库。
10. **replace 保护**：replace 缺少被引用 Profile，确认只停用并明确展示 diff，不产生悬空引用。
11. **权限**：普通成员可按策略查看 / 选择，不可修改 Catalog 或导入。
12. **历史兼容**：加载迁移前 Agent / Role / Task，确认可解释、可执行，无模型静默替换。

每个场景保存：输入配置、API / DB 关键断言、execution activity、桌面截图；涉及响应式页面的场景另存 mobile 截图。

### Gate D：非功能验收

- [ ] 1,000 Models、300 Profiles 下列表和 Selector 可用，无明显输入延迟；
- [ ] 1,000 条 bundle preview / apply 在约定超时内完成；
- [ ] 并发修改时 importer 返回 revision conflict，不覆盖他人修改；
- [ ] Worker capability 高频更新不写 Catalog、不造成调度风暴；
- [ ] 日志、导出、审计中不出现 Secret；
- [ ] 长名称、空状态、错误状态在 desktop / mobile 无溢出或重叠。

### Gate E：发布与回滚演练

- [ ] 在 staging 按 P7 顺序执行一次完整升级；
- [ ] 关闭新 flag 可恢复旧读路径；
- [ ] migration apply 前后数据数量与引用完整性对账；
- [ ] 回滚不会删除 Runtime Snapshot 或审计记录；
- [ ] 发布 runbook 包含开关顺序、监控指标、告警阈值和负责人。

### 验收结论

Reviewer 输出独立验收报告，至少包含：

- 被验收的 `main` commit SHA；
- 设计文档与 Plan 版本；
- Gate A-E 逐项 pass / fail；
- 自动化命令结果；
- E2E 证据链接；
- 遗留风险和是否阻断；
- 最终 verdict：`pass` 或 `reject`。

只有 `pass` 且阻断项为零时，Plan 才可完成。`reject` 必须创建 rework task，修复后从受影响 Gate 起重新验收。

## 12. 完成定义

Plan 完成必须同时满足：

- [ ] AI Runtime 是用户定义 CLI、Model、Profile 和默认值的唯一入口；
- [ ] Agent、Executor candidate、Team Role 复用统一 Selector 和数据源；
- [ ] Organization 默认值和对象级覆盖语义稳定；
- [ ] Catalog 定义不依赖 Worker，Scheduler 运行时匹配能力；
- [ ] 无能力 execution 可等待并自动恢复；
- [ ] 完整配置支持 versioned YAML / JSON 批量导入导出；
- [ ] 历史数据完成迁移，无静默模型替换；
- [ ] 旧硬编码和旧写路径已清理；
- [ ] 自动化测试全绿；
- [ ] 独立验收 Gate A-E 全部通过；
- [ ] 所有提交已合入并推送远程 `main`；
- [ ] staging 部署验证与回滚演练完成。
