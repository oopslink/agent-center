# Supervisor/executor 与复杂工作流：逐案证据（006）

2026-11-02。单AI探索，尚未人工复核。9例目的抽样；全量包用于上下文查询，证据表为定向深读子集，未声称逐条阅读全部2650条消息。所有时间为UTC。

[综合分析](agent-harness-division-workflow-006.md) · [私有数据与复现入口](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/README.md)

这里的“报告/承认/finding”是参与者记录；数据库 action/audit 证明相应记录或转移存在，detail 中的解释仍不自动成为独立根因证明。源码与回放另见综合分析 S01—S08。

## C01 I166：动作提示与执行模式冲突

`plan-523d5d71`；分析层：Agent 内 Supervisor/executor＋外部 PD 协调。

**设计预期**：执行状态给出下一步，Supervisor 按任务模式调度并识别进展。

**失败链**：T2225 的 inline 合同与 fork 提示矛盾；7条报告重复等待同一类 fork 409。较早 T2219 还曾把测试环境视为应由平台提供的前置工具，把空 executor 当无进展。

**恢复与承担者**：用户纠正测试职责并催查；PD 纠正下一动作后，正式审计出现 start→running→completed。源码片段回放确认提示/守卫不相容。部署 timeout/attempt 缺失并未因启动诊断任务而解决。

**正反证据**：同案 PD 早先能识别冲突并 inline 开始；dev2 已有 inline harness 交付，Tester 有独立检查报告。冲突不是所有 Agent 必定失败的充分条件。

**工程判断**：不能继续让 LLM 裁决确定性的模式/动作矛盾；派发、提示、恢复应共享同一可执行合同。保留 inline 路径与独立测试，不能据此裁定所有 Supervisor 无用。

**边界**：没有原始 fork 请求/响应与运行二进制绑定；本轮没有重现 HTTP 409 或重跑 LLM，不能将所有时间损失归因于此冲突。

| 证据 | UTC / 类型 / 发起者 | 观察与用途 |
|---|---|---|
| [C01-E01](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E01.json) | 2026-09-16T09:46:01.994178Z / tasks / agent | T2225 快照为 supervisor_inline / evidence_only；合同要求诊断部署链，不能由此还原每次历史投影。（`tasks:task-d0229e0d`） |
| [C01-E02](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E02.json) | 2026-09-16T04:15:13.373398Z / messages / agent | PD 在较早 T2219 片段识别 fork 提示与 inline 合同冲突，选择 start_task；同类冲突没有必然使所有 Agent 失误。（`messages:01M2M6T2EXEV90WS5T4M2YJ5Y6`） |
| [C01-E03](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E03.json) | 2026-09-16T04:15:46.37046Z / messages / agent | PD 识别 inline 任务没有 executor 不等于挂死，拒绝无依据 reset。（`messages:01M2M6V2P2CAHW0R08DQJ0CZ8D`） |
| [C01-E04](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E04.json) | 2026-09-16T07:16:19.706159Z / messages / agent | PD 结束未产出环境的协调任务，承认反复汇报不构成有效处置。（`messages:01M2MH5P3T2VMPZGVEBRT7FSN8`） |
| [C01-E05](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E05.json) | 2026-09-16T08:51:41.698219Z / messages / user | 用户要求直接测试，纠正把测试工作变成等待平台工具的推进方向。（`messages:01M2MPMA028XH0FW3KV8TSQ1BA`） |
| [C01-E06](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E06.json) | 2026-09-16T08:52:26.99498Z / messages / user | 用户明确测试环境与工具属于开发测试职责，未要求 Agent Center 提供标准产品工具。（`messages:01M2MPNP7JR06KV1W830FDGF3Z`） |
| [C01-E07](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E07.json) | 2026-09-16T09:05:40.822332Z / messages / agent | dev2 报告 inline worktree 交付测试 harness 6060270a；本轮没有重跑该 harness。（`messages:01M2MQDXEPQQ5KV6FTNFW1BMEH`） |
| [C01-E08](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E08.json) | 2026-09-16T09:06:36.613107Z / messages / agent | PD 承认空 executor 列表不能推断无进展，纠正先前催 fork。（`messages:01M2MQFKY5APR7JCCZACQH9FSY`） |
| [C01-E09](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E09.json) | 2026-09-16T09:12:19.441239Z / messages / agent | Tester 报告独立 12/12 通过，同时任务执行合同要求 fork 而不能收口；测试结果为当时报告。（`messages:01M2MQT2QHB9F06WJ9H5RTZ4K5`） |
| [C01-E10](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E10.json) | 2026-09-16T09:44:30.906765Z / messages / agent | T2224 部署超时且缺 attempt，是另一个未解决问题，不能用 T2225 启动成功替代部署成功。（`messages:01M2MSN0XTQBP213KHBE0VP77D`） |
| [C01-E11](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E11.json) | 2026-09-16T09:46:49.681966Z / messages / agent | T2225 第1条纳入的 fork 409/等待报告。（`messages:01M2MSS8EHW6X4D6TQ4W1T8XW2`） |
| [C01-E12](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E12.json) | 2026-09-16T09:47:30.402268Z / messages / agent | T2225 第2条纳入的 fork 409/等待报告。（`messages:01M2MSTG725JB6DQ0STDD1NMQ1`） |
| [C01-E13](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E13.json) | 2026-09-16T09:49:26.497747Z / messages / agent | T2225 第3条纳入的 fork 409/等待报告。（`messages:01M2MSY1K12C3XYV3ZMWEYDHXW`） |
| [C01-E14](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E14.json) | 2026-09-16T09:51:27.53258Z / messages / agent | T2225 第4条纳入的 fork 409/等待报告。（`messages:01M2MT1QSCG04S58PPT4B8C8ZF`） |
| [C01-E15](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E15.json) | 2026-09-16T09:54:23.052822Z / messages / agent | T2225 第5条纳入的 fork 409/等待报告。（`messages:01M2MT736C8EJ1BDDBSK3W71KK`） |
| [C01-E16](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E16.json) | 2026-09-16T09:58:18.873746Z / messages / agent | T2225 第6条纳入的 fork 409/等待报告。（`messages:01M2MTE9FS3TK5ME0WV33Z9D2E`） |
| [C01-E17](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E17.json) | 2026-09-16T10:04:16.864866Z / messages / agent | T2225 第7条纳入的 fork 409/等待报告；7是消息数，不是独立底层调用数。（`messages:01M2MTS73091D0R7F54RQRCB47`） |
| [C01-E18](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E18.json) | 2026-09-16T10:09:12.61233Z / messages / user | 用户要求 PD 调查此问题；不是用户直接给出源码修复。（`messages:01M2MV27X44D2DXYRHWJ3910YN`） |
| [C01-E19](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E19.json) | 2026-09-16T10:09:52.589159Z / messages / agent | PD 明确 inline 合同优先，要求停止 fork 并 start_task。（`messages:01M2MV3EYDC7WD9QP1ABVSSZEF`） |
| [C01-E20](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E20.json) | 2026-09-16T10:10:09.219124Z / actions / agent | 正式 action 为 agent_started，时间 10:10:09 UTC。（`actions:01M2MV3Z6BB9DT1WSE7PVP83GR`） |
| [C01-E21](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E21.json) | 2026-09-16T10:10:09.227645Z / audit / agent | 任务 open→running 审计。（`audit:01M2MV3Z6BB9DT1WSE7VG0MNJ6`） |
| [C01-E22](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E22.json) | 2026-09-16T10:10:32.447204Z / messages / agent | PD 定位两处源码语义矛盾，同时明确 409 的具体分支尚无运行证据。（`messages:01M2MV4NVZX2ZGBZH08MH0ZD0W`） |
| [C01-E23](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E23.json) | 2026-09-16T10:14:11.105327Z / audit / agent | 诊断任务 running→completed；不等于生产部署完成。（`audit:01M2MVBBD11R9NJJDR3X1DTJHS`） |
| [C01-E24](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C01-E24.json) | 2026-09-16T10:18:48.424911Z / messages / user | 用户提出代码已合并应结束任务、停止无限 evolution；属于停止/范围决策。（`messages:01M2MVKT78QVAYF8QP02P4ANPP`） |

## C02 v2.16：Ship 职责与可用能力不相容

`plan-bdccf57b`；分析层：多 Agent 角色协作；早于6月28日设计记录。

**设计预期**：PD 决定发布，integration 做开发期集成，分工使发布可控。

**失败链**：用户反复纠正 Ship 归属；PD 同时报告没有执行 Git 的工具。任务可以转 owner，却不会由此获得执行能力。

**恢复与承担者**：integration 报告实际完成 Git main/tag，PD 协调任务归属；这证明委托可以补能力，也暴露规则不明确时的来回纠正。

**正反证据**：串行集成等待 Gate 有防误合目的；当事人“scaffold 默认硬派 integration”的解释被候选源码中的 UNASSIGNED 创建方式削弱。

**工程判断**：以所需能力与授权约束派发；将发布决策责任与执行 Git 的技术委托写清。不能把当前不一致用再提醒角色名称解决。

**边界**：没有定位最终 owner 由调用层、提示还是后续 assign 写入；候选源码不证明当时运行版本，不能宣布模板是已确认根因。此例不能归因于后来的 Agent 内 S/E。

| 证据 | UTC / 类型 / 发起者 | 观察与用途 |
|---|---|---|
| [C02-E01](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C02-E01.json) | 2026-06-27T15:15:17.817656Z / messages / agent | 集成队列串行等待 Gate 有合理质量目的，不能因为等待就判浪费。（`messages:01KW4TCFNSNWKXDVG4ASPQG855`） |
| [C02-E02](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C02-E02.json) | 2026-06-27T15:25:51.842141Z / messages / user | 用户质疑功能是否真正集成，要求 ancestry 证据；缺陷本身仍需独立核验。（`messages:01KW4TZTV2EEVBW99EYEFC3VSC`） |
| [C02-E03](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C02-E03.json) | 2026-06-27T15:42:20.339951Z / messages / user | 用户纠正 Ship 归 PD、integration 只负责开发期集成。（`messages:01KW4VY05KNWK47NWJHD9Z3MKV`） |
| [C02-E04](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C02-E04.json) | 2026-06-27T15:46:11.074678Z / messages / agent | integration 报告完成 main 7aceccfd/tag，并称 PD 在租约中接走任务。（`messages:01KW4W51G2S2PRPJ44MQ4MP2BM`） |
| [C02-E05](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C02-E05.json) | 2026-06-27T15:46:57.198234Z / messages / agent | PD 承认没有执行 Git 的工具/仓库能力，要求决定授权或委托方式；职责文字不能创造能力。（`messages:01KW4W6EHEJFB91EB9Q466B9CE`） |
| [C02-E06](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C02-E06.json) | 2026-06-27T15:47:30.77645Z / messages / agent | integration 把错派归因 scaffold 默认指给自己；须与 S06 源码反证合读。（`messages:01KW4W7FARSH6ARPG5RDXHEJDA`） |
| [C02-E07](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C02-E07.json) | 2026-06-27T15:47:53.700659Z / messages / agent | PD 复述 scaffold 默认指派解释并提出手动重指；重复说法不是独立确认。（`messages:01KW4W85Q47GXEY5GKXZWK54M3`） |

## C03 Progress Control：控制流程成为自己的阻塞

`plan-f4e88e01`；分析层：Plan 条件、完成策略与执行控制。

**设计预期**：明确 gate、完成条件和恢复路径能持续推进，失败后补修复链可恢复。

**失败链**：审计记录先合 main 才 complete 与先 complete 才 merge 的循环前提；后期业务节点完成仍受旧 condition 阻塞，新增 runtime 任务没有自动删除旧阻塞条件。

**恢复与承担者**：先增加不依赖 main 的证据路径打断循环；最终 agent:PD force_completed 绕过4个旧条件。用户催查与追问，Agent 执行处置。

**正反证据**：Agent 拒绝用迟到 non_delivery 回滚已完成事实；补充上下文、独立验收及部分修复可能必要，不以52个 Task 本身判浪费。

**工程判断**：完成策略必须与依赖图联合校验；旧条件无 evaluator 时提供可审计的有界终态处置。不能把无限追加恢复任务当成已修好平台。

**边界**：“第4次”是用户/Agent 叙述，本轮独立定位了最后一次 force_completed，没有重建4个完全独立片段；运行版本差异仍为报告。

| 证据 | UTC / 类型 / 发起者 | 观察与用途 |
|---|---|---|
| [C03-E01](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C03-E01.json) | 2026-08-27T09:10:00.553872Z / messages / user | 用户询问 dev 是否理解背景，不据此推断 dev 实际没读过。（`messages:01M117QF39XXMSHDGHX2Y3NJBH`） |
| [C03-E02](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C03-E02.json) | 2026-08-27T09:10:18.449461Z / messages / user | 用户要求 PD 解释任务背景。（`messages:01M117R0JHCKVPQEGJPT44CZTZ`） |
| [C03-E03](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C03-E03.json) | 2026-08-27T09:13:41.2769Z / messages / agent | PD 提供较具体的领域与实现说明，说明 context 传递有额外工作；收益未独立测量。（`messages:01M117Y6MW359Q8ZWNAB8NNMRY`） |
| [C03-E04](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C03-E04.json) | 2026-08-27T09:14:35.458941Z / messages / user | 用户要求节点描述给出详细背景、技术与验收。（`messages:01M117ZVJ2SXGR2TT98M1KCXQ4`） |
| [C03-E05](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C03-E05.json) | 2026-08-27T11:41:50.854962Z / audit / agent | 拓扑提交说明先合 main 才能 complete 与先 complete 才能走唯一 merge 的冲突，并增加脱离 main 的证据路径；是审计内记录的诊断。（`audit:01M11GDFW6SGJH50ED77VQEHWN`） |
| [C03-E06](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C03-E06.json) | 2026-08-28T14:39:40.510149Z / audit / agent | 后期在 46/46 与空 frontier 下仍增加 runtime 修复链，显示产品收口与平台修复耦合；不能断言所有修复都无效。（`audit:01M14CZTEYNEY396TRVXVN4QZY`） |
| [C03-E07](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C03-E07.json) | 2026-08-28T15:36:10.697532Z / messages / agent | Agent 面对已完成任务的迟到 non_delivery 拒绝再次 block，保持已知交付事实。（`messages:01M14G7969YVMJ7S3MVCNX3B2J`） |
| [C03-E08](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C03-E08.json) | 2026-08-28T15:47:24.186504Z / messages / user | 用户指出第4次 all tasks done 而 Plan running；4是参与者计数，不是本轮独立重建的4次。（`messages:01M14GVTWTYDD27KVSR92GA86A`） |
| [C03-E09](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C03-E09.json) | 2026-08-28T15:48:04.802823Z / audit / agent | force_completed 审计由 agent 身份执行，明确绕过4个 unresolved_condition；不是人工直接改库。（`audit:01M14GX2J2Z7JBV1GFBZ7K4ZA4`） |
| [C03-E10](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C03-E10.json) | 2026-08-28T15:50:14.19682Z / messages / user | 用户追问为什么重复发生。（`messages:01M14H10XMJJ5M8BJHC10BG6HM`） |
| [C03-E11](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C03-E11.json) | 2026-08-28T15:51:25.739215Z / messages / agent | PD 说明 52/52、空 ready_set、missing_gate_evaluator 与旧运行版本；最终强制收口是数据处置，不是已部署平台修复。（`messages:01M14H36SBVRAE30BWKFHTVP01`） |

## C04 Collaboration Insight：恢复受阻与交付政策疑点

`plan-236dadc8`；分析层：Supervisor/executor、租约/owner、交付政策。

**设计预期**：租约和归属防止互相接管，自动交付检测防止 commit 未 push。

**失败链**：PD 报告 lease/inflight/owner 各自拒绝恢复；T2154 被要求 push main，快照却为 executor_fork。交付与权限是否匹配比“分支名字不好”更值得核对。

**恢复与承担者**：integration Agent 报告保留工作区后完成推送与 manual_recovery。用户曾要求直接由 Supervisor 完成，但没有证明当时成功切换。

**正反证据**：此前候选确有 dirty/未 push 等问题；候选 push.go 可识别已远端持久化的允许分支，且明确拒绝 main。16:36:06 未推送报告先于16:36:39已推送报告。

**工程判断**：保留租约、分支保护和真实远端交付检查；补能力相容的恢复入口与交付合同检查。不能凭参与者抱怨就删除防误推 main 的保护。

**边界**：不能确认误报由非标准分支单独触发，也未确定实际二进制含哪版 push 逻辑；manual_recovery 是 Agent 的记录，不证明人类手工修复。

| 证据 | UTC / 类型 / 发起者 | 观察与用途 |
|---|---|---|
| [C04-E01](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C04-E01.json) | 2026-09-03T15:57:40.639159Z / tasks / agent | T2154 要求合入并 push main，而快照 dispatch_mode=executor_fork；delivery 为 Agent 填写的 manual_recovery。（`tasks:task-e8015edf`） |
| [C04-E02](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C04-E02.json) | 2026-09-03T12:47:58.753615Z / messages / agent | 此前候选存在无有效新增提交、pushed=false、dirty 等真实交付问题；non_delivery 不能一概认定误判。（`messages:01M1KMZKS1MEWEHA8NM12MDR8H`） |
| [C04-E03](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C04-E03.json) | 2026-09-03T13:26:54.708796Z / messages / user | 普通进展询问，不自动归为人工救援。（`messages:01M1KQ6WZM5GEA83XZ23685M7Q`） |
| [C04-E04](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C04-E04.json) | 2026-09-03T13:31:04.339295Z / messages / user | 用户追问是否真的执行；不能仅凭该问句确定 executor 已死。（`messages:01M1KQEGRKSHHV0DJZMRFHMMZT`） |
| [C04-E05](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C04-E05.json) | 2026-09-03T13:36:53.627563Z / messages / user | 用户提议 supervisor 直接完成；不是成功切换执行路径的证明。（`messages:01M1KQS5VVAVA94G0XXMQPZ49W`） |
| [C04-E06](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C04-E06.json) | 2026-09-03T14:19:20.564652Z / messages / agent | PD 报告 lease/inflight/owner 权限分别阻止 reset、supersede、fail，恢复动作缺乏可行出口；没有原始 RPC trace。（`messages:01M1KT6X3MHHJK4T7A7V287GMT`） |
| [C04-E07](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C04-E07.json) | 2026-09-03T16:36:06.134653Z / messages / agent | PD 16:36:06 报告候选已 commit、尚未 pushed，要求保留工作区再恢复。（`messages:01M1M21ABPQ91RTBA0A3DRTSKV`） |
| [C04-E08](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C04-E08.json) | 2026-09-03T16:36:16.020634Z / messages / agent | 发起“人工接管”的是 agent:PD，目标为 integration Agent。（`messages:01M1M21M0M0PT3ZSS98RZDNFS0`） |
| [C04-E09](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C04-E09.json) | 2026-09-03T16:36:39.231592Z / messages / agent | integration 16:36:39 报告 main 已推送，同时称分支规则误报；时间顺序与 S05 不能支持确定假阴性。（`messages:01M1M22ANZ2670D875B8XGHE7R`） |
| [C04-E10](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C04-E10.json) | 2026-09-03T16:38:17.076981Z / messages / agent | integration 说明已恢复且禁止再次 reset；缺历史运行二进制映射，根因保持未定。（`messages:01M1M25A7M2RFKGTZRGDXJ644R`） |

## C05 Access：PASS 没有覆盖用户要的产品

`plan-a77ec987`；分析层：PD/Tester 产品验收与证据继承。

**设计预期**：多道验收与矩阵可以可靠判断交付完整。

**失败链**：旧候选7700038b报告 PASS；用户指出与 mock 不一致后，PD 承认把局部功能/导航检查当成整页产品验收。

**恢复与承担者**：撤销旧结论并重开；004轮已区分后续新 IA 合同与新 SHA 的 PASS，不复活旧 PASS。

**正反证据**：系统保留了纠错与重开记录；后继完成说明可修复，不证明原验收充分。Mermaid 的具体缺陷检查构成对照。

**工程判断**：验收应绑定用户目标、具体候选和实际可观察检查；不能把检查表齐全、继承旧证据或多角色签字当作全目标通过。

**边界**：本轮未独立对照旧页面与 mock，失败判据来自用户评价及 PD 明确承认；不能推断所有视觉验收必然要人完成。

| 证据 | UTC / 类型 / 发起者 | 观察与用途 |
|---|---|---|
| [C05-E01](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C05-E01.json) | 2026-08-23T11:01:01.702542Z / messages / agent | 旧 7700038b PASS 报告包含继承证据，不能替代整页产品检查。（`messages:01M0Q4FW46TMHQBVTDRACYXR3X`） |
| [C05-E02](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C05-E02.json) | 2026-08-23T13:55:15.249504Z / messages / user | 用户指出重构与 mock 不符；用户评价与独立视觉测量分开。（`messages:01M0QEEWNHYVBK3N0V8ZQKM0ET`） |
| [C05-E03](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C05-E03.json) | 2026-08-23T13:56:08.015795Z / audit / agent | 旧结论后出现 reopen/paused 审计。（`audit:01M0QEGG6FZPM9MFRYX0ZFAE2P`） |
| [C05-E04](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C05-E04.json) | 2026-08-23T13:56:16.46955Z / messages / agent | PD 承认把局部功能检查当整页产品验收并撤销旧 PASS；旧候选与后继新合同不能混同。（`messages:01M0QEGRENQ54HM6FJVG202V46`） |

## C06 Mermaid：独立角色有收益，硬门禁没有完全兑现

`plan-67822059`；分析层：Tester/integration/PD＋依赖与 evolution。

**设计预期**：独立验收阻止缺陷合入，REJECT 后修复并复验。

**失败链**：Tester 已 REJECT，但下游仍有 ready/派发信号；只有节点完成条件不足以表达验收结果。需要 Agent 主动拒绝错误信号。

**恢复与承担者**：Tester 给出具体浏览器缺陷；integration 阻止合入；新增修复/复验、实际代码改变、精确 SHA PASS 后集成。

**正反证据**：单测全绿仍发现多图渲染失败，独立角色提供新信息。该包0条 user 消息；PD 所称“人工硬门禁”仍是 Agent 行为。

**工程判断**：保留能发现具体缺陷的独立检查与 SHA 绑定；让调度尊重 verdict，而非用更多人工式消息维持门禁。有效恢复包含代码与复验，不能只归功于改图。

**边界**：本轮未重跑浏览器；既有004 ancestry 仅验证提交关系，不证明浏览器测试真实通过。0条 user 消息不是全局无人参与证明。

| 证据 | UTC / 类型 / 发起者 | 观察与用途 |
|---|---|---|
| [C06-E01](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C06-E01.json) | 2026-08-17T08:53:35.958879Z / messages / agent | Tester 报告单测全绿但多图渲染仍 pending，给出具体浏览器缺陷。（`messages:01M07ET7JPD8CN5DW22MPGJAQA`） |
| [C06-E02](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C06-E02.json) | 2026-08-17T08:53:46.487526Z / messages / agent | 补充 exact SHA deccd 与六 fence、错误图、viewport 等检查，不仅转述开发者结论。（`messages:01M07ETHVQ0VCBQHFVPQVMDN3J`） |
| [C06-E03](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C06-E03.json) | 2026-08-17T08:54:29.908002Z / messages / agent | integration 拒绝 ready 提示，依据 REJECT 停止合入 main。（`messages:01M07EVW8MVWRSQ3T53P87KKJ0`） |
| [C06-E04](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C06-E04.json) | 2026-08-17T08:54:51.738887Z / messages / agent | Tester 重申 ready 不能覆盖结构化 REJECT。（`messages:01M07EWHJT2DB8J040JRZ0P4J7`） |
| [C06-E05](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C06-E05.json) | 2026-08-17T08:55:01.009366Z / messages / agent | PD 所称“人工硬门禁”实际由 Agent 执行；该包没有 user 消息不证明全程无人。（`messages:01M07EWTMH1NQNA9FTDDMKBT7F`） |
| [C06-E06](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C06-E06.json) | 2026-08-17T09:01:27.383034Z / generations / agent | 增加针对缺陷的修复/复验链；已派发的旧集成节点仍需阻止，改图不能自动消除全部风险。（`generations:generation-384637ec`） |
| [C06-E07](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C06-E07.json) | 2026-08-17T09:00:40.896165Z / messages / agent | 修复候选 6855b0bd 对 Mermaid 渲染串行化并补测试，构成新内容而非纯改图。（`messages:01M07F76J007PSFS89WHZ6QQM4`） |
| [C06-E08](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C06-E08.json) | 2026-08-17T09:13:15.332019Z / messages / agent | 独立浏览器复验报告新 SHA 通过；本研究未重跑浏览器。（`messages:01M07FY7A42V2NZ4480MZS96N5`） |
| [C06-E09](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C06-E09.json) | 2026-08-17T09:20:36.082973Z / messages / agent | T1353 结构化 PASS 绑定修复 SHA。（`messages:01M07GBNQJ09A884M34YGJY65H`） |
| [C06-E10](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C06-E10.json) | 2026-08-17T09:22:56.823425Z / messages / agent | main b0c40 包含已验收修复，排除未验收后续改动；ancestry 的独立核验沿用004轮。（`messages:01M07GFZ5QEPQQMR4CKEMHF4MB`） |

## C07 v2.10.3：角色和顺序需人纠正，Agent 也纠正错误前提

`plan-f7f98cfe`；分析层：多 Agent 角色协作；早于6月28日设计记录。

**设计预期**：开发、集成、发布责任清楚可减少交接错误。

**失败链**：用户多次纠正 integration/PD 与 trunk/main 边界、Ship 完成顺序；文字理解没有稳定映射成动作。

**恢复与承担者**：职责/顺序纠正后报告发布到 main 402a5cea/tag。PD 先诊断 trunk 缺失并要求重建，integration 查远端指出已有开发和验收证据，拒绝该错误路径。

**正反证据**：integration 基于实际 Git 事实纠正 PD 和用户转述的指令；因此角色可提供校验收益，人类提示也不是天然正确。

**工程判断**：将发布前置事实落实为可查询/校验的状态；允许有证据地拒绝错误交接。减少纯角色转述，不取消有独立事实检查的角色。

**边界**：本轮没有重做发布，Pages 部署未知；此例不证明 Agent 内 Supervisor/executor 结构造成错误。

| 证据 | UTC / 类型 / 发起者 | 观察与用途 |
|---|---|---|
| [C07-E01](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C07-E01.json) | 2026-06-16T01:39:41.396427Z / messages / user | 用户澄清 integration-dev 负责开发阶段。（`messages:01KV71B56MYTKFX58W48VARSTA`） |
| [C07-E02](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C07-E02.json) | 2026-06-16T01:41:52.993389Z / messages / agent | PD 随后仍把 Ship 交给 integration，说明文字纠正未立即稳定落实。（`messages:01KV71F5Q1DGQ5MP1Q29XMQ2H4`） |
| [C07-E03](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C07-E03.json) | 2026-06-16T01:45:51.965601Z / messages / user | 用户再次纠正 Ship 属 PD、trunk 与 feature 的边界。（`messages:01KV71PF2XKD4YY5ERFD4ACJNV`） |
| [C07-E04](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C07-E04.json) | 2026-06-16T01:48:45.675353Z / messages / agent | PD 将问题归因 trunk 缺失并要求重建；下一证据反驳该解释。（`messages:01KV71VRQBZC147BD2RTTQ0KYE`） |
| [C07-E05](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C07-E05.json) | 2026-06-16T01:49:57.64719Z / messages / user | 用户重复新建 trunk 指令；人类介入也可能沿用错误事实前提。（`messages:01KV71XZ0F9CXTWEMS6YEAP4DK`） |
| [C07-E06](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C07-E06.json) | 2026-06-16T01:53:27.945879Z / messages / agent | integration 检查远端后指出 trunk 已含开发与验收证据，拒绝重建造成分叉；是角色提供新事实的正例。（`messages:01KV724CC96MF8A8SKPZACE06C`） |
| [C07-E07](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C07-E07.json) | 2026-06-16T02:00:39.671089Z / messages / user | 用户纠正 Ship 必须先完成 main/tag 再置任务完成的顺序。（`messages:01KV72HHZQN6EXSGA9DCWZ7ZS7`） |
| [C07-E08](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C07-E08.json) | 2026-06-16T02:02:30.440646Z / messages / agent | PD 承认顺序错误并调整。（`messages:01KV72MY58X8TTJ7PCJ3P7DXA9`） |
| [C07-E09](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C07-E09.json) | 2026-06-16T02:08:51.252117Z / messages / agent | main 402a5cea/tag 已发布的报告；Pages 部署保持未独立确认。（`messages:01KV730J1MXKES7PXRRD3WJXNJ`） |
| [C07-E10](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C07-E10.json) | 2026-06-16T02:12:04.153627Z / messages / agent | integration 再次确认 main/tag，不代表独立试验。（`messages:01KV736EDS5M1P398Y28DJXKPF`） |

## C08 v2.14：依赖含义失配，同时有跨角色知识收益

`plan-e3ee9116`；分析层：多 Agent 调度、共享发现；早于6月28日设计记录。

**设计预期**：图表达依赖，专业角色并行实现并共享必要发现。

**失败链**：finding 记录 depends_on 未转换为执行 seq 条件，顺序依赖却并行启动，PD 需停任务调度；用户要求 pull/rebase 并将 Step0写入任务。

**恢复与承担者**：实际顺序调整；跨角色共享6项迁移规格/真实 schema 差异，后续报告 NULL-aware 索引修正；最终报告 SHIPPED。

**正反证据**：findings 提供非平凡的 SQLite/NULL/表名事实；F7 选择 A 是正常产品范围决策。89条 user 消息不能当成89次救援。

**工程判断**：只保留有执行语义的依赖，并让共享发现有明确消费者/验收。不能以专业角色曾协调失败就抹去其发现真实缺陷的收益。

**边界**：依赖未生效来自参与者 finding，未重跑旧调度器；共享注入证明信息渠道存在，不证明被理解，更不是量化净收益。

| 证据 | UTC / 类型 / 发起者 | 观察与用途 |
|---|---|---|
| [C08-E01](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C08-E01.json) | 2026-06-21T16:20:53.862957Z / findings / agent | 记录 depends_on 未被调度当成 seq 条件、7 feature 并行，PD 需停 F3/F5 再排序；这是 Agent finding，不是调度器原始 trace。（`findings:01KVNFR9F6A1SSPFWEFSGZ0EB1`） |
| [C08-E02](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C08-E02.json) | 2026-06-21T16:27:27.567091Z / findings / agent | 跨角色发现 SQLite/表名/不存在的 transitions/NULL 索引等6项规格现实差异，给下游具体可行动信息。（`findings:01KVNG49YFFXCDTNXSD3GTZX5X`） |
| [C08-E03](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C08-E03.json) | 2026-06-21T17:09:07.734193Z / findings / agent | 后续确认 migration 采用 NULL-aware predicate，显示该协作不只是重复转述。（`findings:01KVNJGKGP9WP8CHJRNAXZT3BH`） |
| [C08-E04](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C08-E04.json) | 2026-06-22T08:26:30.367838Z / messages / user | 用户要求开发前 pull/rebase，补上共享代码前置步骤。（`messages:01KVQ70C2ZGYXBZNM83V3Z2974`） |
| [C08-E05](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C08-E05.json) | 2026-06-22T08:31:18.291729Z / messages / user | 用户要求把 Step0 固定写进任务；机制执行边界仍应与文字说明区别。（`messages:01KVQ7958K13VDCNCD5NTJ2VX6`） |
| [C08-E06](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C08-E06.json) | 2026-06-22T08:23:40.552164Z / messages / user | 正文出现“我(PD)”但 sender 为 user，可能转述，不按措辞改成人工独立诊断或 Agent 发言。（`messages:01KVQ6V688NW16T9VKEBPC4D2T`） |
| [C08-E07](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C08-E07.json) | 2026-06-22T14:17:51.671405Z / messages / agent | F7 给出真实范围选择 A/B/C，并非所有停顿都是流程失效。（`messages:01KVQV3Q9QYXF5T4F01V616XQ5`） |
| [C08-E08](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C08-E08.json) | 2026-06-22T14:28:29.334485Z / messages / user | 用户选择 A，是正常产品范围决定。（`messages:01KVQVQ60P8MM6BY2NVKT6WC9R`） |
| [C08-E09](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C08-E09.json) | 2026-06-22T23:48:50.40319Z / messages / system | Ship ready 注入19条 findings；证明传递渠道存在，不证明所有接收者理解或执行正确。（`messages:01KVRVS73387DEFGXVM0GW2HNR`） |
| [C08-E10](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C08-E10.json) | 2026-06-23T02:29:14.674948Z / messages / agent | 最终 SHIPPED/main 6c8189e2 报告，不抵消此前协调错误。（`messages:01KVS4YXSJMDXPXFWKADJW2ZW5`） |

## C09 Spark Host：外部前提不足不是多加流程能解决

`plan-70ab192f`；分析层：环境/授权与编排边界；负对照片段。

**设计预期**：部署/验收图推进至真实双平台生产验证。

**失败链**：选定后期片段缺真实设备、sudo 授权与 public-CA 目标；反复部署或追加 Agent 不会自动提供这些资源。早期另有产品缺陷，不能把全案都归为外部。

**恢复与承担者**：generation 明确分出 user 环境任务，复用既有部署；Agent 停止无效上游重试并暂停。

**正反证据**：拒绝虚假 smoke 与安全 NO-GO 是必要控制；用户资源/授权决定属于正常前提，而不是 harness 自身应消除的全部人工参与。

**工程判断**：先识别真实前提，缺失时有界暂停并保留已有产物。不能以“自动化”为由假造环境、绕过授权或将完成状态代替真实 smoke。

**边界**：用户被指派不等于已提供环境；最终快照 done 也不证明全矩阵验收，本轮不评价整个69 Task目标的最终成功率。

| 证据 | UTC / 类型 / 发起者 | 观察与用途 |
|---|---|---|
| [C09-E01](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C09-E01.json) | 2026-09-03T14:36:00.800987Z / messages / agent | 部分部署/CA bootstrap 已成功报告，仍缺 sudo、Linux/DGX 与 public-CA 真实目标；不等于全矩阵 smoke 完成。（`messages:01M1KV5DX0GGZQS8F5E8WZ14Y2`） |
| [C09-E02](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C09-E02.json) | 2026-09-03T14:38:19.959018Z / generations / agent | 将真实环境/授权任务指给 user，复用既有部署而不重复部署；授权需求不等于人已经完成。（`generations:generation-a2577996`） |
| [C09-E03](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/evidence/C09-E03.json) | 2026-09-03T15:40:10.674752Z / messages / agent | Agent 暂停无效上游重试并等待真实前提；此后 snapshot done 不证明用户已提供环境或完成真实冒烟。（`messages:01M1KYTXHJ87V0MB22XN29B96S`） |

