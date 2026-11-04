# P5 记忆案例、反证与排除附录（round-002）

作者：oopslink · [作者主页](https://github.com/oopslink)；无资助。单 AI 探索，尚未人工复核。正文见[研究报告](agent-harness-memory-study-011.md)。下述 7 组是有目的抽取的关联材料，不是 7 个独立试验。时间均为 UTC；消息表达与原生动作分列。完整原文仅在本地受限研究目录保留。

## A. 纳入与排除口径

| 组 | 用途 | 正证据／结果 | 反证与排除 |
|---|---|---|---|
| M01 团队整理 | 治理正例 | Git 与领域事件支持 20 个早期候选→8 晋升、12 拒绝；有具体归并理由 | 同一 curator 判断，未独立验证内容真值；不算交付收益 |
| M02 宽 main 规则 | 有界设计负例 | 文本约束冲突；T1529、T1535 两次原生 block | owner 对话也传递该规则；缺完整输入，不归因于记忆独立作用或模型认知负荷 |
| M03 中间节点例外 | 有界复用正例 | 多 agent 回执、明确引用、T2222/T2223 原生 completed | PD 提示、图与证据共同变化；原始恢复异常和后续部署故障排除，整个 Plan 不算成功 |
| M04 analyst 更新 | 发布缺口及反证 | 3 个 pending；旧 canonical 规则仍被读取 | 同一会话已采用新结构；私人 Git 仅自报，不判学习失败 |
| M05 遗留笔记 | 记录／追溯正例 | 文件保存用户要求与验收缺口，任务 block 可交叉核对 | 无历史版本／实际读证据；executor 空转、API 未接线排除 |
| M06 quickfix 漂移 | 排除设计效应 | 自报私人操作说明过时，提出 canonical 文档替代 | 原文／输入缺失且并发工作区混杂，不能归因私人记忆 |
| M07 初始化骨架 | 覆盖审计 | Git bootstrap 与启动日志相容 | 不能以当前空内容推断历史从未写入；不计失败率 |

## B. M01：整理并非只有通过，也包括拒绝泛化

编码 Team `team-f5a633d6` 在 08-10 和 08-12 共提出 20 个候选；到 08-13 完成 8 个晋升、12 个拒绝，HEAD 为 `daabe994c47ab24fff003760b48835b96e1db42a`。8 个 canonical 文件为 4 rules、4 entries。后续再晋升 6 个 add，最终成为 9 rules、5 entries。

12 条拒绝理由的人工语义分类由本轮 AI 完成：11 条明确提到由 canonical 条目吸收、拆分或替代；`tmprop-01KZV20V4NN1MAGVEZF6J1DQEV` 则认为 DAG 运行实现尚不宜上升为全局硬规则。这不是互斥的质量标签；例如被归并项也可能同时包含本地路径、过大清单等问题。未经第二名标注者复核，不能将其称为可靠的自动分类器结果。

| 证据 | 定位 | 能证明什么 |
|---|---|---|
| 30 个提案最终 frontmatter 与所有历史版本 | 本地 `derived/head-documents.json`、各 Team `history.bundle` | 作者、reviewer、状态、source_path 与理由 |
| 57 次原生变更 | `raw/db/memory_events-corrected.json`；[逐项比对](memory-011/round-002/derived/event-git-validation.json) | 每个 commit 均对应一次提案或审核状态变化 |
| 整理结果消息 | `01KZWXQ9JN8KHZBXH4ENCXG5QQ`，08-13 06:42:29 | agent 对整理结果的说明，与 Git 相容 |
| 早期强规则晋升说明 | `01KZWVCFCHH17P4HW9HKJ51GEZ`，08-13 06:01:37 | owner 要求和 agent 解释；其中自报读取不冒充次日才开始留存的审计 |

## C. M02：仅改节点名称，没有解除知识约束

知识项 `origin-main-required-before-completion` 源于 `tmprop-01KZNJ25ZYWZB7946GKVHZ70R1`，08-10 提出、08-13 06:01:20 晋升。canonical 文本把所有任务完成与成果进入 main 绑定。下表将消息细节留在附录，正文只讨论约束冲突。

| 时间 | 类型与原生定位 | 事件及证据边界 |
|---|---|---|
| 08-25 07:13:36 | rule audit；integration-dev runtime generation 226；commit `daabe994…` | 同一 agent 当日有该规则正文回执；不能仅凭时间确定它与晚间 Task 的完整输入完全一致 |
| 22:08:50 | Task action `01M0XFG3KBP0QQPK675R02AGWY`；Task `task-0179221b`（T1529） | block 理由指出任务禁止合 main，但完成又要求 main，下游 T1530 依赖 completed |
| 22:09:35 | rule audit；PD generation 384；commit `daabe994…` | PD 成功查询同一规则并留下原生回执 |
| 22:12:27 | 消息 `01M0XFPQXEPNPQQECMYJPZ7004` | PD 自报新链：evidence-only 冻结→独立验收→main 集成；认为新命名已避免冲突 |
| 22:12:42 | Task action `01M0XFQ6BDBK133D5GJNMMZNAQ`；Task `task-0d32ed36`（T1535） | 再次 block；指出宽规则没有 evidence-only 例外，候选仍不得先完成 |

两次 block 有动作表和消息互证，不把循环仅建立在事后总结上。保留的结论是知识适用层级与任务合同冲突，且一次改图未改变该约束；不扩写为所有停摆都由记忆、DAG 或 LLM 内在偏见造成。此处是行为规范之间的逻辑矛盾，不用调度或 executor 故障承担解释。

## D. M03：条件化规则与后续交接

`intermediate-candidate-nodes-complete-before-final-main-integration` 于 08-29 04:59:57 提出，05:00:10 晋升（commit `fb1683adbb45c806062d79d0420c21dcd6381337`）。规则限定中间实现／验收节点，可在唯一远端候选成立后完成；最终集成／发布与 Plan 仍需核 main。首次原生回执为 05:01:25，累计 19 条、8 名 agent。其来源任务 T1751 同时有 executor 恢复异常；不以该异常证明记忆或分工失效。

| 时间 | 定位 | 本轮取证结论 |
|---|---|---|
| 09-16 03:28:20 | tester1 generation 273 的新规则回执 | tester 早于目标接力已有正文访问；不等同于完整 Task 输入 |
| 08:55:45 | PD generation 624 的新规则回执 | commit `534e484d83296fda146f79634014a917da715e0f` 下新规则可解析 |
| 09:17:11 | 消息 `01M2MR2ZK8PQ35DBJRZD76G3KH` | PD 明确引用规则，接受候选 `686baeaa` 与 `6060270a` 的验收，要求 T2222 无需等 main；后置 T2223／T2224 |
| 09:17:50 | 消息 `01M2MR466E1QKRSD2SJD0GDNM4`；原生事件 `01M2MR466HWP7XDVTMCXE88R1A` | tester 明确采用例外；`task-39dfd88f` 从 running→completed |
| 09:18:22 | integration-dev generation 313 的新规则及旧 main 规则回执 | 两条规则均能被读取；没有虚构旧规则已被删掉 |
| 09:31:34 | 原生事件 `01M2MRXANZ2F8PSQFKNF09RRCC` | `task-2fdb1668`（T2223）从 running→completed |
| 09:44:30 | Task action `01M2MSN0XVHRM9RH4Q8RQR7WFP` | 发布 Task `task-936b29d3` failed，正式部署超时且无 attempt 记录；排除出记忆设计负效应，也阻止把本例算作完整交付成功 |

报告中的收益是**协调条件得以落实并发生局部状态推进**。原生 completed 不自动等于独立验收真值；本轮未重跑历史测试、部署或页面。PD 提示、候选产物和 DAG 增补都是共同干预，没有仅改变记忆的对照。

## E. M04：Team 提案待审与当次输出改变并存

analyst Team `team-772532f5` 首条规则于 09-01 03:15:17 经用户批准，commit `4e27ee87585f9acaa26fdde575dac17230d76cf8`；规则正文 7,593 bytes。后续三个提案均为 add，未替换原规则。

| 候选 | 提出时间 | 截止时 pending 天数 |
|---|---|---:|
| `tmprop-01M22DDE0KGCWD03ZFX4ERGMFN` | 09-09 06:24:19 | 54.01 |
| `tmprop-01M31H4RR5ZE5H18BNPGNVZ8GH` | 09-21 08:25:57 | 41.93 |
| `tmprop-01M31P3QD3Q83835RY1DZFR2MS` | 09-21 09:52:45 | 41.87 |

09-09 06:06:53 的消息 `01M22CDGT2C2PCM0T3ZDBQ32WF` 已使用 HOT／STORY 双榜，早于首个新提案。06:25:09 的消息 `01M22DEYN8YDDQ4BYCZG5SP6M9` 明确写明个人 issue 文件已提交 `0a003f7`，Team 提案为 pending。后者 Git 与数据库可核，前者未恢复私有对象，维持“自报”级别。

两条原生规则回执分别在 09-09 06:22:59 与 09-21 07:32:28，commit 分别为 `4e27ee87…` 与 `1270b127…`。第二个 commit 包含第一项 pending 提案，但两次 canonical 规则 blob 相同；尚未包含稍后 09-21 的两个新提案。这里不存在“用未来提案检查过去读取”的时间倒置。没有记录的审批动机，也没有跨 agent 实际需求分母，不把 pending 天数判为造成损失的时间。

## F. M05–M07：保留但不承担设计成败的材料

M05：`worker-legacy/working-tree/projects/project-c7073e48/tasks/task-224b8f8a/MEMORY.md`（12,652 bytes）记录 T1032 的用户定稿、候选与验收问题。原生 block `01KXCV5T61EPM8B98JWABNPSAR` 同样指出导航定稿未落地；随后 `01KXCVHW3B044CK2XG1RV891PM` 报告 executor 空转并要求人工选择。笔记与动作能证明问题被记录，但没有 Git 历史／读取证据，不能认定因记忆而避免或造成了错误。空转、API 接线缺口均排除。

M06：07-02 对话 `01KWHEW4D7H31KY53YF0867N69`；消息 `01KWHFR3ZXSRY4X5FJR22720K6` 自报沿用私人 quickfix 操作造成共享 clone 干扰，`01KWHFVVT6YJ3828WWKCFERARX` 追溯旧说明并建议以项目 canonical 文档为准。缺当时文件、Git 和输入，且操作／并发问题可解释结果；从设计负例排除，仅保留为待核实的知识维护候选。

M07：现存 PD 与 xloop 私有 memory 各 2 个 system:bootstrap 提交，只有根 `MEMORY.md` 与 `supervisor.md` 模板。Worker 日志 `log-04` 原始行 19／20 记录 included=2、truncated=0、omitted=0、body_bytes=645，不能与原始文件共 541 bytes 机械相等（组装还包括封装）；不换算为 token。初始化晚于主要案例，另 14 个 DB agent 的历史 private memory 未恢复。空模板不作为自动学习无效的证据。

## G. 定位、选择与修改记录

[机器可读案例台账](memory-011/round-002/evidence/case-ledger.json)保留纳入状态、支持及限制；[记录定位](memory-011/round-002/evidence/source-locators.json)提供 34 个选定消息／动作／事件的本地源文件、JSON Pointer 与 SHA-256。Team Git、全部回执、源码和部署清单另见数据包，不限于这 34 个定位。消息词法候选与全量提案／审计检查是不同抽样层，不声称所有 585 条候选均经逐条人工或 AI 深读。

本轮纠正两项分析过程问题并保留痕迹：早先查询用错 `team.memory.*` 名空间而得到零行，改用实际的 `team_memory.*` 后得到 57 条；Python 日期解析对可变小数位失败，已规范化后重算。前者是研究脚本查询错误，不是产品事件缺失。图表脚本／环境问题也记录在过程日志，不更改原始数据。旧报告和首轮“尚未取得历史”的状态保存在修改前快照，不回填成早先已知。
