> ⚠ **v1-era doc** — pending v2 update. v2 撤回了 Bridge BC + 飞书集成 (per [ADR-0031](decisions/drafts/0031-v2-drop-bridge-vendor-integration.md))；ADR-0017/0021/0022 superseded by [ADR-0039](decisions/drafts/0039-conversation-business-model-v2-unified.md). 本文中 Bridge / vendor / 飞书 引用是 v1 残留。

# Roadmap

记录"推迟做"的功能 —— 节奏决策（v1 不做，但早晚要做）。**明确不做（边界决策）**的去 [requirements/03-out-of-scope.md](requirements/03-out-of-scope.md)。

规则：见 [conventions.md § 6](../rules/conventions.md#-6-范围决策两分出范围-vs-推迟) 与 [documentation.md § 4](../rules/documentation.md#4-范围决策的两种区分)。

> 优先级和时间窗会随实际情况调整。本文档不构成承诺。

---

## v2（短期，紧接 v1 后）

### ~~飞书 Slash 命令（高优）~~ → v3+: 外部 IM / 渠道接入重新设计（per ADR-0031 后规划）

~~D2 路线 —— `/dispatch project=X agent=claude "..."` 这种结构化命令。~~
- ~~**v1 不做的原因**：D1（@bot 自由文本）+ D3（卡片）已覆盖 90% 体验，D2 是 power user / 脚本化场景的 UX 增强~~
- ~~**触发条件**：用户日常飞书内频繁需要批量 / 精确派单；自由文本歧义影响效率~~
- ~~**影响**：Feishu 集成模块加 slash parser；不动核心模型~~

（v2 删 vendor 集成 per [ADR-0031](decisions/drafts/0031-v2-drop-bridge-vendor-integration.md)；slash 命令等 vendor-specific UX 待 v3+ 外部 IM / 渠道接入重新设计时一并考虑）

### Remote CLI

允许从笔记本直接 `agent-center query --remote=vps`，免去 SSH。
- **v1 不做的原因**：架构已经把 transport 抽象做好（CLI 自动按 env 选 unix sock / 远程 RPC），但 admin token 签发 + TCP + TLS 实现未做
- **触发条件**：SSH-to-VPS 频次高到影响体验；远程脚本化操作需求
- **影响**：加 admin token 子命令 + TLS 配置 + 远程 transport 启动开关

### Token cost 折算成钱

v1 已统计 token 数，v2 折算成 RMB / USD。
- **v1 不做的原因**：折算需维护 model-specific 单价表（含 input / output / cache 折扣），烦琐但不复杂
- **触发条件**：用户需要监控月度花销
- **影响**：加 model 单价配置 + `stats` 命令显示 cost

### Per-project 限定可用 agent CLI

项目可声明 `allowed_agent_clis` 限制能跑的 CLI 列表。
- **v1 不做的原因**：v1 任意 worker 上的任意 CLI 都可用；项目只声明 `default_agent_cli`
- **触发条件**：项目级合规需求；偏好 CLI 不同
- **影响**：Project schema 加 `allowed_agent_clis` 列表 + 派单时校验

---

### v3+: 外部 IM / 渠道接入重新设计（per ADR-0031 后规划）

~~多 vendor 接入（DingTalk / Web chat / Slack 等）~~

~~[Bridge BC](architecture/strategic/03-bounded-contexts.md#bc8-bridge渠道桥接层) 架构上支持多 Bridge；v1 只实现 FeishuBridge。~~
- ~~**v1 不做的原因**：飞书 + Web Console 覆盖个人场景；额外 vendor 是体量 / 协作场景拓展~~
- ~~**触发条件**：用户开始通过非飞书入口（公司用 DingTalk / 团队接入 Web chat）~~
- ~~**影响**：每个 vendor 一个 `XxxBridge`，复用 Conversation / Message / Identity / IssueComment 抽象；ChannelBinding 表已就绪~~
- ~~**同一 Conversation 多 vendor 送达**（如紧急 InputRequest 同时飞书 + DingTalk）→ v3 视需求~~

（v2 撤回 Bridge BC + 飞书集成 per [ADR-0031](decisions/drafts/0031-v2-drop-bridge-vendor-integration.md)；外部 IM / 渠道接入在 v3+ 重新设计，届时整体重做架构而非沿用 v1 BridgeBC + Adapter 抽象）

---

## v3（中期）

### AgentImage 模型 + Memory git 化（agent 本体 vs 数据分开）

把 agent **本体**（model + harness + bundled skill + 默认 MCP 模板 + 默认 instructions）打成不可变 **AgentImage**（镜像，含 tag / digest 版本），通过 image registry 分发；agent **数据**（memory / 用户笔记 / 运行时积累）走独立 **git 仓**。本体跟数据**分开管理 + 独立版本化**。来自 [v2-kickoff G5 讨论](drafts/v2-kickoff-2026-05-22.md)，2026-05-22 用户提出。

- **v2 不做的原因**：v2 用户 skill = 文件自管（[ADR-0028](decisions/drafts/0028-skill-file-mount-lite.md)）；中心化 skill 库 / 分发会被 image 模型取代，是 technical debt
- **触发条件**：用户有「跨机器 / 跨用户 distribute agent 模板」需求；或 agent kind 多到手动管理痛
- **影响**：
  - 引入 `AgentImage` 概念 + image registry（自建 / 借现成的）
  - `agent create --image=<name>:<version> --name=<n> [--worker=<id>]`：from image instantiate
  - AgentInstance.image_ref 字段（指 image）
  - Memory 系统升级走独立 git 仓（详 [ADR-0012](decisions/0012-memory-file-based.md) 之外的扩展）
  - 现有 `instructions.md / mcp_config.json / skills/` 文件结构 → 进 image build 的 source
  - Multi-supervisor agent / 其他 built-in 类型扩展
- **依据**：用户长期方向声明（2026-05-22）；image 模型对位容器 / docker 思想

### 云 Computer 节点支持

把 Worker 从「开发机 daemon」扩展为「可注册的算力节点」（含云节点），允许 agent-center 按需在云节点拉起 worker，加大算力池弹性。来自 [v2-kickoff 议题 E2](drafts/v2-kickoff-2026-05-22.md)，2026-05-22 用户决定移到 roadmap。

- **v2 不做的原因**：v2 thesis 把「算力管理」收窄到 E1 enroll 轻量化；云节点引入云凭据 / cloud provider abstraction / 节点拉起 / 计费等新维度，跨度太大；先做轻量本地多机管理打地基
- **触发条件**：用户开始有「临时跑大量 agent 干一个项目」需求；或本地多机算力明显不够
- **影响**：
  - 引入 `ComputerProvider` 抽象（local / aws / gcp / ...）
  - Worker AR 加 `provider_kind` / `provider_metadata` 字段
  - 新协议「按需拉起 worker」：center 调 provider API → 启动云节点 → 节点上自动跑 `agent-center join` → 加入算力池
  - 跟「跨节点 agent 调度」（可能跟 G1 AgentInstance 配套）联动
- **依据**：[竞品报告 § 3.6 + § 3.7](research/competitive-analysis-2026-05-21.md) Slock Computer 抽象

### Web 时间轴可视化（trace flamegraph）

类似 Honeycomb / Jaeger / Chrome devtools 的横向时间轴 + flamegraph，专做 trace 可视化。
- **v1 不做的原因**：F21 Web Console 的基础列表（事件顺序 + tool call 序列 + 时长）已经够看清概况
- **触发条件**：任务复杂度涨高（嵌套 tool calls 几百条）；基础列表难以快速诊断
- **影响**：新增 web console 子页面；可能直接接 Jaeger / OTel UI 而非自研

### Task / Execution 模型扩展

[ADR-0010 两层模型](decisions/0010-task-execution-two-layer-model.md) 与 [task-runtime/](architecture/tactical/task-runtime/00-overview.md) 系列文档之外的推迟项：

- **父子状态联动**：parent task `done` 自动触发所有未完成 sub-task `abandoned`（或相反方向：所有 sub-task done 触发 parent done）。v1 显式 `parent_task_id` 仅做血缘
- **ETA 过期触发 supervisor 唤醒**：v1 ETA 仅做展示，过期不影响系统行为；推迟做 ETA-trigger event 进唤醒白名单（auto-ping）
- **Per-project timeout / `max_executions_per_task` override**：v1 全局默认（5min / 6h / 3 次）；按项目定制需要新增 project schema 字段
- **Per-project workspace_mode 默认值**：v1 task 创建时显式选（默认 worktree）；project schema 可加 `default_workspace_mode`
- **Per-project retry policy 字段**：v1 supervisor 通过 prompt 决策；推迟到 prompt 之外的 declarative 配置
- **Issue reopen 后 amend 已 spawn task 集合**：v1 conclude 是 final；reopen 不能改既有 task 列表
- **复杂 Artifact 维度**：artifact tag / 版本 / 父子引用（如"PR 引用 design doc"）等高级元数据
- **触发条件**：以上各项普遍是"项目级定制"和"高级语义"需求，v1 任务量小不到痛点

### 派单可靠性进阶

[ADR-0011 派单可靠性协议](decisions/0011-dispatch-reliability-protocol.md) 之外的推迟项：

- **完整 fencing token（单调递增 sequence number）**：v1 execution_id 一锤定音兼任 fencing；多 supervisor / 多 center 并发场景需要单调序号
- **Cross-worker dispatch fail-over**：Worker A 失联但 active execution 自动迁移到 Worker B（含 worktree 状态搬运）
- **`--force-abandon`**：kill 超时（worker 离线 / SIGKILL 也不死）也能强 abandon task
- **Dispatch envelope budget / cost limits**：派单时声明"最多 $5 token"，agent 超额自动停
- **Worker quarantine / graceful drain**：把某 worker 标"不接新单"等当前完成
- **触发条件**：上述全是规模 / 并发 / 信任度场景；v1 单用户 / 单 worker 群体接受当前简化

### Supervisor / Cognition 进阶

[ADR-0012 Memory file-based](decisions/0012-memory-file-based.md) + [ADR-0013 Invocation 并发模型](decisions/0013-supervisor-invocation-concurrency.md) + [cognition/00-overview.md](architecture/tactical/cognition/00-overview.md) 之外的推迟项：

- **Supervisor 自动重试**：`claude_nonzero` / `timed_out` invocation 失败后自动用同 trigger 重发一次。v1 选了"alert + 人工 retrigger"防 token 死循环；失败率统计稳定后可加 1 次自动重试
- **Cross-invocation 协调机制**：并行 invocation 间互锁 / 协商，避免"两个 invocation 互不知情下都派任务给 W-2"导致一次 NACK 浪费 token。v1 靠底层单活 / ACK 兜底
- **Memory 并发写 advisory lock**：跨 scope 并行 invocation 同改 `global/CLAUDE.md` / `supervisor.md` / `projects/X/CLAUDE.md` 等共享文件接受偶发 race；v2+ 视场景加 fcntl / flock
- **Memory 跨 BC 聚合查询**：file-based 后 "supervisor 关于 project X 累积了哪些经验" 要靠 grep 文件树。需要可考虑 `agent-center memory search` CLI 工具化 + Web Console memory 浏览页
- **显式 `pending` invocation 状态**：全局 FIFO 队列在 v1 是 in-memory；想看队列长度 / 平均等待时长得加 `pending` 行落 DB
- **Memory file 体积监控告警**：file 体积无硬 cap，依赖 supervisor 自觉压缩；监控大小 + 自动提醒（如 spawn 前注入 "X.md 已 50KB，请考虑压缩" 提示）
- **Memory file 周期性 compaction invocation**：center ticker 扫文件大小，超 threshold emit 合成事件给 global scope，触发一个 supervisor invocation 专责"压缩 X.md"
- **多 supervisor / 跨机器**：v1 单 center 单 VPS；多 center 场景 Memory 同步用 git push/pull 还是其它方案需重新评估
- **触发条件**：以上多数是规模 / 并发 / 体验细节问题，v1 单用户低频不触

### Conversation 模型扩展

[conversation/01-conversation.md § 2](architecture/tactical/conversation/01-conversation.md) 之外的推迟项 —— 当前 `Conversation.kind` 把两条独立轴拍扁成一个 6 值枚举，单 user / 单 channel 假设下不痛，规模化后可能要拆：

- **拆分 `kind` 两轴**：把当前 `dm / group_thread / adhoc / notification / task / issue` 拆成
  - `scope`（受众场景轴）：`dm / group_thread / adhoc / notification`
  - `attached_to`（业务对象耦合轴）：`none / task / issue / ...`
  - v1 现状是 `kind=task` + `task.conversation_id` 双向都做，两轴交叉时表达力不足（如"task 的 group_thread 讨论"无法表达）
- **`group_thread` 嵌套结构**：当前 `kind=group_thread` 是扁平 Conversation，`primary_channel_thread_key` 只是 vendor 字符串；在 Slack/DingTalk 多 channel 场景下，IM 语义里的"channel → thread"嵌套需要一等公民建模（child-of-channel-conversation 指针）
- **Channel 作为领域实体**：当前 channel 仅以 `primary_channel_hint` 字符串体现（[01-conversation.md § 3 注释](architecture/tactical/conversation/01-conversation.md) 已承认"务实而非纯粹"）；多 vendor + 多 channel 时可能要 `conversation_channel_routes` 表 / Channel AR
- **v1 不做的原因**：单 user / 单 channel / ~~FeishuBridge~~ 单一渠道（v2 删 vendor 集成 per ADR-0031），6 kinds 扁平模型够用；拆轴成本高（迁现有数据 + 改所有 query + 改 5 个 ADR 的 1:1 模型）
- **触发条件**：多 vendor 接入落地（特别是 Slack/DingTalk 的 channel-thread 嵌套结构进来）；或出现"同一 task 在多个 scope 内讨论"等 v1 拍扁建模兜不住的场景
- **影响**：Conversation schema 拆列 + 迁数据；[ADR-0017](decisions/0017-task-as-conversation.md) / [ADR-0021](decisions/0021-issue-as-conversation.md) 的 1:1 模型要重新评估；跨 BC 引用方向（`task.conversation_id` / `issue.conversation_id`）保持不变

### Workspace 模式进阶

- **Readonly mount enforcement**：direct 模式强制只读 base_path（v1 仅约定不修改，不强制）
- **Fixed-path workspace 模式**：除 worktree / direct 外，第三种"CWD 指向某固定路径"
- **多路径 workspace**：agent 同时持有多个 CWD 候选
- **触发条件**：agent 自律不够 / 项目跨多个目录

### Observability 进阶

- **实时 stream timeline**：CLI `inspect task X --tail -f` 持续推送新事件
- **全文搜 events**："找含字符串 X 的事件"
- **跨 event 关联推断**："worker 离线导致那条 fail 吗？"等可视化关联
- **触发条件**：基础 inspect / query 不够用时；规模化场景

### DAG 任务依赖的高级特性

v1 已有基础 deps：`task.depends_on_task_ids` JSON 数组 + 运行时可改 + 无环 + supervisor 判断派单（见 [task-runtime/01-task.md § 8 依赖](architecture/tactical/task-runtime/01-task.md)）。下列是更进一步的能力：

- **自动 cascade abandon**：dep 进 `abandoned` 时自动 abandon 依赖它的 task（v1 现状是 supervisor wake 后决定）
- **复杂依赖语义**：OR 依赖（"任一 dep done 即可"）、only-if-failed（"等 dep failed 才跑"）、conditional（"dep.artifact 满足条件才跑"）
- **DAG 可视化**：Web Console ~~/ 飞书~~ (v2 删 vendor per ADR-0031) 展示依赖图
- **拓扑排序自动派单**：center 端做调度器（v1 是 supervisor 一个一个评估）
- **触发条件**：多步流水线高频出现，supervisor 用 working memory 兜不住
- **影响**：跨 BC 影响大，要重新评估"center 不做硬编码调度"的边界

---

## v3+ / 低优先级

### Supervisor 自动收敛 Issue

低风险 / 高置信场景下 supervisor 自动 conclude Issue，不必每次推 ~~飞书~~ vendor (v2 删 vendor 集成 per ADR-0031) 等用户。
- **v1 不做的原因**：v1 一律推 ~~飞书~~ vendor (v2 删 per ADR-0031)；机制（supervisor 决策接口）已留好
- **触发条件**：用户对 supervisor 决策信任度建立；某类 Issue 反复采纳率 100%
- **影响**：Supervisor 加 auto-promote policy 配置；Issue 决策路径加自动分支

### Agent 主动加入 Issue 讨论

直接让 worker agent 在 Issue thread 内发评论（不只是 supervisor 派研究子任务间接参与）。
- **v1 不做的原因**：v1 只有 user + supervisor 两方讨论；agent 通过 `agent_finding` comment 间接参与已够
- **触发条件**：明确场景需要 agent 直接表态
- **影响**：Issue comment 权限模型扩展 + 频率限制 + 信任策略

### 容器化 agent 执行

每个 task 跑在隔离容器里（替代或叠加 worktree）。
- **v1 不做的原因**：worktree 对单用户已足够；引入容器化加 docker daemon / 镜像维护 / mount / 凭据访问等复杂度
- **触发条件**：跑不信任代码 / 需要资源限制 / 防 agent 误操作伤本机
- **影响**：新增 `ContainerAdapter` 平替 `LocalAdapter`；镜像构建 / 版本管理

### Prometheus / OTel / Grafana 接入

把 events / metrics 导出到时序数据库 + 可视化。
- **v1 不做的原因**：v1 观测主体是 trace + fleet view + inspect，不是 metrics
- **触发条件**：任务量大到需要 P99 时长 / 失败率趋势 / worker 利用率看板
- **影响**：加 `agent-center exporter --prometheus` 子命令；定义 metric schema

### Per-project 自定义观测维度

项目可声明额外的 trace 维度 / metric。
- **v1 不做的原因**：v1 观测层统一且 opinionated（[conventions § 2](../rules/conventions.md#-2-可观测性优先)）
- **触发条件**：多项目有共性的"额外维度"需求
- **影响**：定义"扩展点规范"（custom event type / agent 可发的 metric event）让项目按规范对接，**不**让项目侵入 agent-center 观测层代码

---

## 长期愿景

### 多用户 / SaaS

支持多个用户共享 agent-center 实例 / SaaS 化运营。
- **v1 不做的原因**：核心定位"个人工具"，多用户会重构权限模型 / 租户隔离 / 计费 / SLA
- **触发条件**：极远 —— 当个人工具被验证、有规模化需求
- **影响**：基本是重新做项目（认证、权限、隔离、计费、运维全套）

---

## 内容维护

- 新增"推迟"项 → 直接编辑本文件，按版本分组追加
- "推迟"升级为"v1 做"→ 从这里删除，加到 [01-functional.md](requirements/01-functional.md)
- "推迟"降级为"出范围"→ 从这里删除，加到 [03-out-of-scope.md](requirements/03-out-of-scope.md)，记 ADR 说明
- 完成的 → 从这里删除，更新需求 / 架构文档
