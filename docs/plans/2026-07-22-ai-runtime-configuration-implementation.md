# AI Runtime 统一配置实施 Plan

> 设计文档：[AI Runtime 统一配置技术方案](../design/features/ai-runtime-configuration.md)
>
> 初稿：2026-07-22；重新基线：2026-07-29
>
> 跟踪 issue：`issue-0c155dbd`

## 1. 目标与当前判断

目标不是先完成一个配置页面，而是可靠闭合：

```text
Agent Runtime Selection
  -> immutable TaskExecution Runtime Snapshot
  -> effective capability matching
  -> waiting / exactly-once re-drive
  -> supervisor / executor 使用冻结参数启动
```

main 已有部分 Catalog、Resolver、Import / Export、审计和管理面实现，但这条运行链尚未完整闭合。
旧计划按 Catalog、Import / Export、Web、全部业务入口、Scheduler 横向铺开，会扩大“能配置但不能可靠执行”的表面积。
本计划改为纵向薄切：先补契约并硬化基础层，只打通 Agent，再完成能力等待闭环，最后才扩 UI 和其它入口。

在阶段 0–3 通过前：

- 暂停继续扩 AI Runtime UI、Team Role 和 Executor candidate；
- Import / Export 只补正确性、安全和回滚，不继续优先扩功能；
- 不删除 legacy 字段、API 或 modelrouter；
- 不在 prod 试错；所有真跑使用隔离全真实例。

## 2. 全局不变量

1. Runtime Snapshot 在 TaskExecution 首次创建时原子冻结。
2. retry、resume、保留同一 execution 语义的 reassign 复用同一 Snapshot。
3. Catalog、Profile 和 Organization default 的变化不修改历史 execution。
4. 第一条链的优先级仅为：Agent override > Agent Profile > Organization default。
5. `task.model` / F3 modelrouter 在切新 Resolver 前必须形成明确 ADR。
6. Secret 只以 reference 存储；DB 导出、API、审计、日志均无明文。
7. CLI 启动只能经过受控 adapter registry；禁止任意 executable、shell 拼接和未知参数透传。
8. 无匹配 Worker 时 executor 数为 0；能力恢复后同一 execution 最多启动 1 个。
9. Basic Capability Coverage 不等于 Effective Schedulability。
10. 每个阶段的“完成”由可观察结果决定，不由任务状态或代码存在决定。

## 3. 阶段依赖

```text
S0 重新基线与契约
  -> S1 硬化基础层
  -> S2 Agent 单入口纵向链
  -> S3 能力匹配与等待恢复闭环
  -> S4 管理 UI 与其它入口
  -> S5 迁移与切换
  -> S6 清理
```

每阶段采用 Dev -> Review -> Decision -> Integrate -> Gate -> Accept 的独立裁决流。
Accept 必须在隔离安装实例真跑，结论落为 `pass` / `reject`；没有验收 outcome 不得进入下一阶段。

## 4. S0：重新基线与契约收口

### 4.1 当前实现矩阵

从最新 `origin/main` 盘点并记录：

| 面 | 必查项 | 输出 |
|---|---|---|
| Catalog | CLI、Model、Profile、default、revision、权限、审计 | implemented / partial / missing |
| Resolver | selection 来源、参数合并、schema、legacy fallback | 真实调用链与生产者 |
| Snapshot | 创建时点、存储位置、retry / resume / reassign 行为 | 生命周期时序图 |
| Import / Export | preview、apply、revision、Secret、权限、回滚 | 契约差异 |
| Worker | capability 身份、TTL、version、features、health | 上报与过期规则 |
| Scheduler | matcher、现有 project / team / workspace / concurrency 约束 | 完整调用链 |
| UI | 已有页面和真实 API，是否仍使用 fixture / 常量 | 可达性矩阵 |

盘点必须追到生产调用链，不以相似函数、测试 helper 或文档声明代替。

### 4.2 必须形成的契约

- Snapshot 创建时点与原子写入边界；
- selection 优先级与 provenance；
- `task.model` / F3 modelrouter ADR；
- stable key、runtime model identifier 与展示名的边界；
- Secret reference、解析责任和脱敏边界；
- `CliAdapterRegistry`、允许的 schema 子集和 fail-closed 行为；
- `waiting_for_capability` 状态机；
- Worker capability 身份、TTL、版本解析和健康窗口；
- Basic Capability Coverage / Effective Schedulability 命名与 API；
- feature flags、shadow compare、catalog revision 和事件 / 指标。

### 4.3 S0 放行条件

- [ ] 上述矩阵对应最新 main，且每项有代码路径证据；
- [ ] Snapshot、优先级、`task.model`、Secret、adapter、等待状态机无未决语义；
- [ ] 需要多选一的架构决定已有 ADR；
- [ ] 设计文档、Go / API / TypeScript 命名一致；
- [ ] 每个后续阶段有独立回滚开关和可观察出口。

## 5. S1：硬化已落地基础层

### 5.1 Snapshot 与 Resolver

- Snapshot 增加 `schema_version`、`catalog_revision`、`profile_key/version`、source provenance 和脱敏参数摘要；
- canonical、确定性参数编码；
- execution 与 Snapshot 同事务创建；
- retry / resume / reassign 只读已保存 Snapshot；
- JSON Schema 明确支持子集，未知关键字 fail closed；
- legacy 与新 Resolver shadow compare，记录结构化差异。

### 5.2 审计与 Secret

- before / after 深拷贝，禁止共享可变 map / slice；
- Web API、CLI、agent tool 写入复用同一 AppService、权限和审计；
- Secret 参数只保存 reference；
- API、DB 导出、审计、日志和错误 details 统一脱敏；
- Profile / default 修改和停用返回引用计数、影响预览。

### 5.3 Import / Export

- validation token 绑定 org、bundle digest、mode 和 catalog revision；
- preview 与 apply 复用同一 validator；
- apply 单事务，revision 改变后 token 失效；
- deterministic YAML / JSON round-trip；
- import/export 权限对称；
- 不支持 schema version 和关键字段 fail closed；
- rollback 保留 Snapshot、审计和 legacy 读路径。

### 5.4 自动化与放行

- [ ] Catalog 改动后，已创建 execution 的 Snapshot 字节级不变；
- [ ] retry / resume / reassign 后 Snapshot 字节级不变；
- [ ] CLI / Model / Profile 审计 before / after 正确且无 Secret；
- [ ] schema 未支持关键字被拒绝，不是静默忽略；
- [ ] import 并发冲突不覆盖新 revision；
- [ ] export -> import -> export 语义等价且无 Secret；
- [ ] migration 在空库、旧库和重复执行下通过；
- [ ] `go test ./...`、`go vet ./...` 全绿；迁移版本断言全仓核对。

回滚：关闭 catalog v2 / resolver shadow flag，继续读 legacy；additive schema 不删除。

## 6. S2：Agent 单入口纵向链

### 6.1 范围

只接 Agent。Team Role 与 Executor candidate 保持旧路径，避免一次引入三个选择来源和多个优先级。

### 6.2 实现

1. Agent create / update 保存 `RuntimeSelection`，默认 `inherit`。
2. `RuntimeResolver` 在 TaskExecution 创建事务内解析并冻结 Snapshot。
3. scheduler 只读取 Snapshot，不再读取可变 Profile。
4. supervisor 与 executor adapter 从 Snapshot 获得 `cli_key`、真实 model identifier 和 parameters。
5. 新路径位于 feature flag 后；关闭 flag 时旧 Agent 执行路径逐行为可用。
6. shadow mode 同时计算新旧 resolve 结果，只记录差异，不双写两套独立语义。

### 6.3 测试与真验

- inherit / Profile / override 三态；
- Organization default 改变只影响新 execution；
- disabled / missing / incompatible / invalid 参数 fail closed；
- adapter 使用结构化 argv，不执行 Catalog 任意路径；
- 真创建 Agent、派发任务、启动 supervisor / executor，并在进程侧核冻结的 CLI / model / parameters；
- retry、resume、reassign 后核同一 Snapshot；
- 关闭 flag 后旧路径仍可执行。

### 6.4 S2 放行条件

- [ ] flag ON 的真实 Agent 任务端到端完成；
- [ ] 进程参数来自保存的 Snapshot，不是可变 Profile；
- [ ] shadow diff 可查询且无 Secret；
- [ ] flag OFF 的旧路径真跑通过；
- [ ] Team Role / Executor candidate 没有被部分接入。

## 7. S3：能力匹配与等待恢复闭环

### 7.1 Capability 契约

- Worker 上报绑定已认证 Worker identity；
- Center 规范化 semver，拒绝不可解析版本；
- capability 含 `scanned_at`、`expires_at`、features 和 health；
- TTL 与健康窗口外的记录不参与匹配；
- Worker 只更新自身 projection，不能写 Catalog；
- 重复、乱序和跨 Worker 上报有回归测试。

### 7.2 Matcher

先匹配基础 CLI / version / features / health，再叠加：

- project 与 team 归属；
- workspace / repo 可达性；
- 权限与 Secret 可用性；
- worker / agent / executor 并发约束；
- 现有调度和亲和性约束。

配置页展示 Basic Capability Coverage；execution 详情展示 Effective Schedulability 和完整 reason。

### 7.3 `waiting_for_capability` 状态机

| 场景 | 必须行为 |
|---|---|
| 首次无匹配 Worker | 持久化等待状态与 reason，executor=0 |
| capability 恢复 | 幂等重驱动，同一 execution 最多创建 1 个 executor |
| 重复 / 抖动上报 | 合并信号，不重复 dispatch |
| Center / Worker 重启 | 自动恢复等待订阅和重驱动 |
| Task 取消 / 终态 | 后续 capability 信号不得复活 |
| reassign | 复用 Snapshot，重算 schedulability，不重复 executor |
| 等待超时 | 结构化升级人工处理，不记 executor failure |

executor 创建使用 durable idempotency key，并由原子状态转换守门。

### 7.4 独立全真验收

在隔离实例：

1. 创建需要目标 CLI 的 Agent 与 TaskExecution；
2. 保持无匹配 Worker，断言 `waiting_for_capability` 且 executor=0；
3. 启动匹配 Worker，断言无需人工改配置即可 executor=1；
4. 重复上报 capability，断言仍为 1；
5. 重启 Center / Worker，断言不重复执行；
6. 分别验证 cancel、reassign、版本不符、feature 缺失、TTL 过期和能力抖动；
7. 对每种情况核 activity、domain event、指标、reason 和进程事实。

S3 通过后，核心运行闭环才算完成。

## 8. S4：管理 UI 与其它业务入口

顺序固定：

1. AI Runtime 管理 UI 对齐已跑通的 Catalog、Snapshot、coverage 和审计；
2. Team Role 接入；
3. Executor candidate 接入。

每个入口分别定义 inherit / Profile / override 语义、与 Agent 的优先级、历史引用和停用行为，并单独完成真验。

UI 约束：

- Coverage 明确标为“基础能力覆盖”，不得承诺某任务可调度；
- execution 页面展示 Effective Schedulability；
- Profile / default 修改前展示引用计数、影响预览和审计；
- 无 Worker 允许保存配置，但 warning 不可省略；
- 不恢复自由文本 CLI / Model；
- desktop、mobile、键盘、权限、长文本和错误态均验收。

## 9. S5：迁移与切换

### 9.1 Dry-run 分类

对生产等价数据输出：

1. 精确映射到现有 Profile；
2. 多对象内容相同，按 canonical 内容哈希复用 Profile；
3. 仅该对象使用，保留对象 override；
4. 无法映射，保留 legacy 并列为人工处理。

禁止为每个无法精确映射对象批量创建一次性 `migrated-*` Profile。

### 9.2 切换顺序

1. additive schema 和 flags 上线，旧路径不变；
2. shadow resolve 并持续比较；
3. 小范围 Agent 切新 Resolver；
4. 扩大 Agent 范围；
5. 切 Team Role、Executor candidate；
6. 最后切 Organization default；
7. 每一步核指标、审计和真执行，并可独立关 flag 回旧读路径。

`task.model` / modelrouter 按 S0 ADR 执行，不留到 cleanup 临时决定。

### 9.3 S5 放行条件

- [ ] dry-run 无未解释 unknown；
- [ ] Profile 去重率和对象 override 数量可审阅；
- [ ] shadow diff 在约定窗口内归零或全部有解释；
- [ ] 每个切换点完成回退演练；
- [ ] Catalog / default 变更不修改历史 execution；
- [ ] 迁移两次结果幂等。

## 10. S6：清理

只有同时满足以下条件才开始：

- legacy fallback 指标连续一个发布窗口为 0；
- 迁移报告无未决项；
- 新路径在隔离实例和灰度范围均完成 retry / resume / reassign / cancel 真验；
- 旧路径回退窗口结束并由 owner 确认。

清理内容：

- 旧字段和兼容写入；
- 旧 API adapter；
- 前端 CLI / Model 常量和自由输入；
- 已被 ADR 取代的 modelrouter 路径；
- 无生产调用的临时 shadow 代码。

清理提交仍需独立部署级验收，确认历史 execution 可读、Snapshot 可解释、retry / resume 正常。

## 11. 可观测性与验收证据

至少提供：

- `runtime_resolution_total{source,result}`;
- `runtime_shadow_diff_total{object_type,diff}`;
- `runtime_legacy_fallback_total{object_type}`;
- `runtime_waiting_for_capability{reason,cli_key}`;
- `runtime_capability_redrive_total{result}`;
- `runtime_executor_dedup_total{result}`;
- `runtime_catalog_revision_conflict_total`;
- `runtime_profile_eligible_workers{profile_key}`。

每个阶段的独立验收报告必须包含：

- 被验收的 main SHA；
- 设计文档与本 Plan 的 commit；
- 自动化命令和原始结果；
- 隔离实例配置、feature flag 与进程指纹；
- API / DB / activity / domain event 关键断言；
- executor 全量枚举，而非只列预期对象；
- 回滚动作和回滚后真执行结果；
- verdict：`pass` 或 `reject`。

## 12. 总体完成定义

- [ ] Agent selection -> Snapshot -> capability -> executor 真实闭环通过；
- [ ] retry / resume / reassign 后 Snapshot 字节级不变；
- [ ] 无能力时 executor=0，能力恢复后 executor=1；
- [ ] 重启、取消、reassign、重复上报和能力抖动不重复执行；
- [ ] Catalog / default 变更不修改历史 execution；
- [ ] Secret 在 DB 导出、API、审计和日志中均无明文；
- [ ] Team Role 与 Executor candidate 已按独立语义接入并真验；
- [ ] dry-run、shadow compare、灰度切换和 flag 回退完成；
- [ ] legacy fallback 连续一个发布窗口为 0；
- [ ] 清理后历史 execution 可读，retry / resume 可用；
- [ ] 所有提交已合入并推送远程 main；
- [ ] 全部部署级真跑在隔离实例完成，未在 prod 试错。
