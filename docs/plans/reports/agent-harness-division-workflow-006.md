# Supervisor/executor 分工与复杂工作流失效研究（006）

2026-11-02（America/Los_Angeles）。**单AI探索，尚未人工复核。** 本轮研究用户指定的第一项 Supervisor/executor 与第三项机制流程复杂度；名字与 DAG evolution 仍保留为后续两个专项。本报告服务于失败复盘和工程取舍，不要求创新点。

## 1. 本轮结论

**最明确的问题，是系统把动作、权限、交付、验收与完成状态之间的一致性留给 LLM 临场协调。** 这在一些片段中把 Supervisor 从工作组织者变成流程矛盾的解释者：要判断该信哪份状态、为什么被拒绝、谁有权恢复、怎样才能让已完成的工作收口。层级并没有自动吸收复杂性，反而多出要同步的边界。这个判断有具体行为和源码支持，不需要推测模型内部“认知崩溃”。

但目前**不能判定整个 Supervisor/executor 方案净收益为负，或换单 Agent 就一定更好**。没有匹配任务的并发收益、总成本或受控对照。可以明确判定不能照原样保留的，是相互矛盾的下一动作、与能力不相容的职责、忽略验收结果的放行、以及没有改变阻塞条件的恢复循环。

本轮还发现了实际收益：Tester 在单测全绿时发现浏览器缺陷，integration 阻止未经通过的候选进入 main、拒绝错误的重建分支指令，开发角色共享了具体 schema 问题。这些收益来自**新的事实、独立检查和真实能力**，不是角色数量或称呼本身。[逐案证据 C01—C09](agent-harness-division-workflow-cases-006.md)

## 2. 数据、方法与证据强度

从既有30例中目的性选择9例，纳入前已经知道部分结果，非随机、非盲评。冻结包包含256条 Task、2650条消息，其中290条 sender 为 user；这是可检索上下文规模，不是全部逐条编码的样本。本轮定向深读、保留89项源记录，人工相关选编24项，其中23条 user 消息、1条把外部前提指给 user 的 generation。后者不是已发生的人类操作。没有将290条用户消息计为290次兜底。

| 案例 | Plan | 本轮关注 | Task / 消息 |
|---|---|---|---|
| C01 I166 | `plan-523d5d71` | inline/fork 冲突、环境职责、进展误读 | 18 / 214 |
| C02 v2.16 | `plan-bdccf57b` | Ship 责任、Git 能力、错误根因解释 | 19 / 156 |
| C03 Progress Control | `plan-f4e88e01` | 循环前提、旧条件、强制收口 | 52 / 635 |
| C04 Collaboration Insight | `plan-236dadc8` | 恢复权限、交付政策与任务合同 | 37 / 212 |
| C05 Access | `plan-a77ec987` | 局部检查误当产品 PASS | 15 / 500 |
| C06 Mermaid | `plan-67822059` | 有效独立检查、错误放行、针对性修复 | 5 / 80 |
| C07 v2.10.3 | `plan-f7f98cfe` | 发布职责/顺序与 Agent 主动纠错 | 2 / 43 |
| C08 v2.14 | `plan-e3ee9116` | 依赖语义与跨角色信息收益 | 39 / 333 |
| C09 Spark Host | `plan-70ab192f` | 外部环境/授权对照片段 | 69 / 477 |

这9个 Plan 在最终快照中均为 done；本研究分析的是过程中具体失败和恢复，不把终态当成功率。累计 Task 不代表同时活跃图大小；消息数不是 token、成本、调用次数或认知负荷。

证据区分四类：原始数据库中的状态/动作审计；用户或 Agent 的报告与解释；固定提交的源码/设计文档；本轮独立执行的源码片段回放。审计 detail 和 delivery JSON 中由 Agent 填写的解释仍是参与者解释，存进数据库不会自动升级为独立事实。

**版本边界**：C02、C07、C08 发生在6月，早于设计文档记载的6月28日 Supervisor/executor 讨论，不能用它们证明后来的 Agent 内部架构造成失败。它们研究的是跨 Agent 角色和流程。历史设计取自固定提交，包含后续修订，不冒充最早原稿，也不等同于当时部署的二进制。

## 3. 方案原本承诺了什么

| 历史设计 | 明确意图 | 本轮能否确认收益 |
|---|---|---|
| S01：单 Agent 内常驻 Supervisor＋按需 executor | 将串行粒度下沉到 executor；并行任务、按难度选模型；Supervisor 统一读取消息和写中心，executor 工作区隔离 | 机制与意图可定位；并发提速、模型费用下降、净收益**未测量** |
| S01/S02：唯一中心写入者、文件交换、独立 worktree | 限制凭据与并发写入边界，保留同一 Agent 的责任 | 隔离有明确目的，本轮不能据协调失败删掉；没有安全收益的独立效果估计 |
| S03：完成后由 Supervisor LLM 判交付 | 替代将 turn-success 自动当完成的旧路径；要求核实 Git/推送/任务成果，文档使用“根治完成不交付”的强措辞 | 设计自己已承认裁决可能漏，需 pending、Tick、nudge、超限 block、重启重建；**新增裁决层没有自行消除完成与交付之间的证明义务** |
| C02/C07 的角色规则与 C03 的控制策略 | 集成/发布职责清楚，验收后推进，完成条件可解释 | 实际仍需反复纠正；同时有独立检查真正阻止错误的正例 |

S03 的补偿机制不是“必然失败”的证据；它说明把判断交给一个常驻 LLM 后，仍需为裁决的可靠到达、及时处理和结果一致性付出工程成本。不能把这些成本都叫无用，但也不能仅凭增加 Supervisor 就宣布问题解决。**隔离执行、权限控制、语义判断可以分别评价，不必捆成一个全保留或全取消的选择。**

源码与设计来源均已逐字保存，完整提交与 SHA256 见[来源登记](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/derived/source-evidence.json)：

| 来源 | 固定版本 / 位置 | 用途与限制 |
|---|---|---|
| S01 | `e59d7d52…` [并发设计](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/sources/agent-concurrent-execution.md:1) | 设计意图、模型路由、context/worktree 与单写者；不是效果评估 |
| S02 | 同提交 [身份合同](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/sources/agent-supervisor-executor-identity-contract.md:7) | executor 是同一 Agent 的执行部分；隔离边界保留责任 |
| S03 | 同提交 [7月6日完成裁决设计](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/sources/2026-07-06-executor-completion-supervisor-judged.md:15) | 从自动回写到 LLM 显式裁决；不将文档内故障叙述另计为本轮新增案例 |
| S04 | 同提交 [状态提示](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/sources/execution_state.go:89)＋[fork 守卫](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/sources/executor_runtime.go:911) | 确认源码中的 inline/fork 矛盾；生产409仍缺原始请求/响应 |
| S05 | `1ba42c24ac613d23bcfa5a0c14ae1ca121032bed` [push.go](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/sources/push-1ba42c24.go:68) | 允许识别已远端推送的合规分支，拒绝 main/master/base；候选版本不等于已部署版本 |
| S06 | `7aceccfdcddda92821e72862d9dacfe2f2c3b5a8` [scaffold](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/sources/scaffold-7aceccfd.go:244) | addNode 创建未指派 Task；没有此处硬编码 integration 的证据 |
| S07 | S04 的精确片段＋未修改共享类型 | [8个边界回放结果](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/replay/results.json)；仅检查选定代码片段相容性 |
| S08 | 沿用004轮 Git 检查 | [Mermaid ancestry 命令与退出码](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/sources/mermaid-ancestry-command.json)；只证明提交包含关系，不证明浏览器验收正确 |

## 4. Supervisor/executor 为什么在这些片段中失效

### 4.1 动作提示和执行约束互相矛盾，Supervisor 没有消除冲突

C01 的 T2225 是部署链诊断任务。合同和任务快照是 `supervisor_inline`，参与者却反复报告执行状态要求 `fork_executor`。09:46:49—10:04:16 UTC 保存了7条 fork 409/等待报告。10:09:12 用户要求 PD 查问题；PD 明确 inline 合同并要求 start_task；10:10:09 action 与审计记录任务启动，10:14:11 completed。首条报告至启动约23分20秒，**只是观察窗口，不是可归因的纯浪费时间**。[C01-E11—E23](agent-harness-division-workflow-cases-006.md)

源码提供了比事后自述更强的解释：可用任务分支直接给 `fork_executor`，没有按模式区分；另一处守卫明确拒绝 `supervisor_inline`。本轮执行了该循环、状态 helper 和模式守卫的原始代码片段，配合原样共享类型与合成边界输入：

| 输入 | 可用任务提示 | 本模式守卫 | 是否矛盾 |
|---|---|---|---|
| open / reopened、inline、无活跃绑定 | fork_executor | 拒绝 inline fork | 是 |
| open、executor_fork、无活跃绑定 | fork_executor | 本守卫不拒绝 | 否；其他守卫未执行 |
| open inline、有活跃绑定 | 无该可用任务提示 | 拒绝 inline fork | 本片段无提示冲突 |
| running / blocked inline | 无该可用任务提示 | 拒绝 inline fork | 同上 |
| 空旧模式 | fork_executor | 本守卫不拒绝 | 不代表兼容所有旧任务 |
| inline 模式首尾有空格 | fork_executor | trim 后拒绝 | 是 |

8个 fixture 中3个出现冲突。它们不是从生产抽样得到的8次请求，**3/8不是故障率**。没有执行 RPC、完整 runtime、全部准入条件、调度器或 LLM，没有重现历史 HTTP 409。冻结快照中的 `agent_activity_events` 查询为空，两个目标 Task 的执行镜像查询也无记录；只能说明这些来源没有恢复出所需 trace，不能证明调用没发生。[回放范围与复现](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/replay/README.md)

同案有重要反例：较早 PD 已主动识别冲突，选择 inline start，并拒绝把“没有 executor”当挂死。这说明 Agent 有时能补偿矛盾，不能说明矛盾可接受。让正确性依赖每次都选对互相冲突的规则，会使所谓 Supervisor 的收益被控制面解释工作侵蚀。

### 4.2 职责不等于能力，责任文字也不等于可恢复权限

C02 用户要求 PD 负责 Ship，但 PD 明确报告没有仓库/Git 能力；最终 Git 由 integration 执行。把 Task owner 改成 PD 并不会给它执行能力。问题可通过清楚的授权委托解决，不需要要求每个角色拥有所有权限，但不能同时规定“只能 PD 做”又不给它可用路径。[C02-E03—E05](agent-harness-division-workflow-cases-006.md)

C04 PD 报告恢复动作分别被 live lease、inflight 不可替换、非 owner 权限阻止。每个限制各有保护目的，组合起来却没有它可调用的恢复出口。用户要求 Supervisor 直接做，也没有自动消除这些约束。这里的收益缺口是**正常执行之外的恢复责任与能力没有对齐**，不是证明应废除租约和权限。[C04-E04—E06](agent-harness-division-workflow-cases-006.md)

C04 的 T2154 还要求合入并 push main，但任务快照为 executor_fork；候选版本的交付代码拒绝 executor 把 main 作为交付分支。这是任务合同与执行政策相容性的重要疑点。尚缺运行版本映射和原始 trace，不能把候选源码直接判成当次故障原因。

### 4.3 隔离上下文不会自动产生正确交接

C03 用户要求 PD 向开发解释背景、给节点写清技术和验收；PD 随后提供详细背景。C01 PD 一度用空 executor 列表判断没有进展，却已有 inline harness 工作；C07 PD 认为 trunk 缺失，integration 查实际远端后发现已具备开发与验收证据。控制者与执行者需要共同可核对的工作事实；仅有一个“负责汇总的 Supervisor”不足以保证它掌握最新事实。

这里尚没有重建每个模型 turn 的完整输入，不能把所有片段都编码成已证明的“context 丢失”，也不能断言由名字导致。可确认的是具体误读、补充上下文和事实纠正。[C01-E07—E09、C03-E01—E04、C07-E04—E06](agent-harness-division-workflow-cases-006.md)

## 5. 复杂工作流为什么变成额外工作

### 5.1 局部规则各自合理，组合后出现循环或不一致

C03 的拓扑审计记录：团队规则要求成果先进入 main 才能 complete；DAG 却要求相关节点先 complete 才能进入唯一的 main merge。增加前置验收并不能自动解决这个循环，必须改变验证和合入的依赖关系。该审计记录了通过独立于 main 的证据路径打断循环的处置；本轮没有运行旧图引擎作形式化证明。[C03-E05](agent-harness-division-workflow-cases-006.md)

C08 finding 则记录 `depends_on` 没有形成调度所用的 seq 前提，7个 feature 同时启动，需要 PD 停止部分任务并重新排序。图上“画了依赖”与运行时“执行该依赖”不同；把缺失语义留给 Agent 解释，就额外制造了调度管理工作。该条仍是参与者 finding，不能当作已独立复现的引擎缺陷。[C08-E01](agent-harness-division-workflow-cases-006.md)

### 5.2 新任务可以制造活动，却没有改变旧阻塞

C03 后期报告52/52节点已完成、ready_set为空，旧 stage 仍留4个 unresolved_condition。force_completed 的数据库审计明确列出被绕过条件，操作者是 Agent PD。用户和 PD 说这是第4次，本轮没有把这句描述扩充为独立验证的4个事件。

新增恢复链是否有用，要看它有没有消除原条件、改变代码、补充真实信息或满足新前提。此处最终动作是明确绕过旧条件收口，而非证明平台产品已在运行环境修好。候选合 main 与控制当前 Plan 的 runtime 已更新是两件事。[C03-E06—E11](agent-harness-division-workflow-cases-006.md)

### 5.3 “已完成”“PASS”“已交付”不能互相替代

C06 Tester 的 REJECT 已报告真实渲染缺陷，下游却仍有 ready 信号；integration 和 PD 主动阻止合入才保住门禁。C05 则是局部功能检查被误当整页产品 PASS，直到用户指出与 mock 不符，PD 才撤销结论。

两者不是同一错误：前者是**调度没有遵从已经存在的拒绝结论**；后者是**检查内容没有覆盖所声称通过的目标**。加更多状态节点不能自动修好检查内容，增加 Tester 也不能替代把其结果接入放行规则。Mermaid 后续成功包含具体修复、新候选复验和精确 SHA 集成，说明独立检查可以有用。[C05、C06](agent-harness-division-workflow-cases-006.md)

### 5.4 外部前提未满足时，停止也是正确结果

C09 后期缺实际设备、sudo 授权和 public-CA 目标。evolution 把这些前提指给用户，复用已有部署；Agent 停止无效重试。追加 Agent 或重复部署不会产生所缺资源。安全 NO-GO、合理等待和正常产品决定不能全归为流程失败；另一方面，该片段还有“上游全终结”提示与实际 open 节点不符的报告，外部依赖与控制面噪声可以共存。[C09](agent-harness-division-workflow-cases-006.md)

## 6. 哪些工作实际需要谁兜底

以下列的是已观察到的介入，不声称“没有人就永远无法恢复”，也不统计总体人工依赖率。

| 类型 | 具体承担内容 | 证据与边界 |
|---|---|---|
| 用户纠正工作目的 | 测试环境属于开发测试工作，不应无限等待平台标准工具 | C01-E05/E06；之后实际代码由 Agent 完成 |
| 用户纠正职责/顺序 | Ship 属谁、trunk/main 边界、先发布再 complete | C02-E03、C07-E01/E03/E07；文字规则没有稳定执行 |
| 用户补上下文/催查 | 要求详细背景与验收，追查重复阻塞与 Plan 不收口 | C03-E01/E02/E04/E08/E10、C01-E18；不能将催查说成用户亲自修源码 |
| 用户否定错误验收 | 指出 Access 与 mock 不符，迫使旧 PASS 撤销 | C05-E02/E04；本轮未重新做视觉比较 |
| 用户正常决定 | F7 产品范围选择、是否停止扩展；真实设备/授权由 owner 提供 | C08-E08、C01-E24、C09-E02；不是全部应被自动化消除的“救场” |
| Agent 控制介入 | inline start、force_complete、manual_recovery、阻止错误 ready、拒绝重建已有 trunk | C01、C03、C04、C06、C07；“人工/手动”措辞不等于真人操作 |

尤其需要避免把 PD 看成真人。`manual_recovery` 是交付来源标签；C04 请求者和恢复者均为 Agent。C06 的“人工硬门禁”也是 Agent 在聊天中维持门禁。它们说明**控制机制的不足被智能体补偿**，不能据此伪造人力工时。

## 7. 本轮主动收紧的旧解释

| 旧解释或容易形成的说法 | 新核对后的结论 | 为什么改变 |
|---|---|---|
| scaffold 硬编码把 Ship 指给 integration | 错派/职责冲突存在；具体写入层未知 | S06 的 addNode 明确创建 UNASSIGNED，Ship 没有硬编码 assignee；参与者一致解释也可能错 |
| 非 synthetic 分支必然导致已有交付误判 | 此次 false positive 未确认；交付合同/政策冲突是另一待证解释 | S05 先核允许分支的远端 HEAD；16:36:06未推送与16:36:39已推送存在时序；历史二进制未知 |
| PD manual recovery 就是人工兜底 | 按身份字段分别记录 Agent 和 user | C03 force_complete 与 C04/C06 接管都为 Agent 身份 |
| 六月角色混乱证明 S/E 内部模型不成立 | 只能支持跨 Agent 角色流程问题 | 案例早于所引用的6月28日设计记录 |
| 回放已复现生产409 | 已复现选定源码片段的规则冲突 | HTTP、完整准入、历史参数、模型行为都没有执行 |

旧002—004报告作为历史工件保留，不悄悄重写。后续引用以本轮收紧后的结论为准。[判断修订记录](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/derived/analysis-decisions.json)

## 8. 工程取舍：哪些不值得继续原样维持

这些是研究建议，尚未实施产品改动，也没有通过对照实验估算收益。

| 设计做法 | 当前取舍 | 保留的价值 / 替代要求 |
|---|---|---|
| 让 LLM 在冲突的模式提示、合同、守卫之间选一个 | **替换** | 下一动作与准入使用同一合同；不能把规则一致性依赖于更长 prompt |
| 只按角色称呼分派，忽略执行工具/权限 | **替换** | 按所需能力与授权匹配，明确决策责任和技术委托 |
| 以 completed 代替 PASS，或以局部 PASS 代替整目标验收 | **停止该替代** | 具体候选、检查范围、verdict、集成/部署事实分别记录与消费 |
| 阻塞条件未变就重复 fork、nudge、加恢复链 | **有界化** | 保存原产物，确认缺什么变化；无新前提时停止并说明 owner/等待条件 |
| 强制收口被记作平台问题已修复 | **停止该替代** | 处置历史坏状态与验证新运行版本分开 |
| 为每个执行结果一律增加常驻 LLM 判断/转述层 | **重新审查必要性** | 将 Git 可达性、权限和状态相容性交给确定性检查；需要语义判断时才引入有证据输入的审查。未证明全面移除 Supervisor 更好 |
| worktree/凭据隔离、租约、分支保护 | **保留并补恢复路径** | 当前有合理保护目标；删除它们会失去防冲突/越界能力，案例没有证明这些边界无用 |
| 提供独立新信息的 Tester、integration 与跨角色 finding | **保留、有条件使用** | Mermaid、trunk 检查、迁移约束有具体收益；没有新事实或能力的重复交接尚无足够收益证据 |
| 精确针对缺陷的修复→复验 | **保留，后续拆分 DAG 效果** | 有实际代码/信息变化；不能把全部恢复归因于“会改图” |

“看着美好但没有自动兑现”的核心假设，是**组织形式和流程形式足够完整，就能自然得到正确理解、可靠交付和持续推进**。本轮证据已经反驳它在这些片段中的充分性。要进一步判定哪些层级应整体删除，还需要知道它们带来多少独立能力以及更简化流程会漏掉什么，不能只数步骤。

## 9. 保存、验证与下一步

完整数据与过程在[division-006](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/README.md)：冻结协议、9例源包、89项记录与哈希、时间线、人工介入选编、来源及反证、源码片段回放、只读 SQL 与结果、判断修订、脚本、校验日志、修改前后文档均保留。输入沿用45GB冻结快照；本轮未重新复制生产库，没有触发生产执行。

回放退出0，8项预期边界均匹配；证据引用、源包/源码哈希、报告链接与派生计数由脚本校验。校验能发现引用或数据生成错误，**不能替代独立人工审核研究判断**。未运行产品 Go/Web 测试或生产验收，因为本轮仅新增研究工件，没有修改产品代码或提交 Git。

按四重点继续时，下一项研究名字究竟承载身份、权限、记忆、角色还是开发者体验；随后专项拆分 DAG evolution 中改图、代码修复、条件变化与范围改变的各自作用。当前第一、三项已完成这组9例的机制深读与有界回放，仍不是全系统最终因果归因。
