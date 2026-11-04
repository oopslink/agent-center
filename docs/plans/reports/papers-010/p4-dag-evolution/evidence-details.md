# P4 补充附录：候选谱系、采样范围与差分定义

本附录集中007修订移出的提交标识、案例时序与计算细节。所有信息来自既有冻结记录或保存的Git命令结果，不代表重新运行历史验收。

## A. 案例与候选定位

Mermaid的四个事件精确时间保留在正文表2：2026-08-17 UTC的浏览器拒绝、修复报告、generation记录和复验报告。修复报告早于改图46.486869秒，报告时间不表示实际代码修改完成时间。`generation-384637ec`较父代新增修复与复验两个节点、一条边，累计由3节点2边增至5节点3边，见图2。

| 案例 | 标识 | 记录含义及边界 |
|---|---|---|
| Mermaid | `6855b0bd` | 推送修复候选与同候选复验报告；时序见E01-02、E01-03 |
| Access原Task续修 | `06183d8c` | 原分支追加提交后，同Task通过报告；前后约六分钟 |
| Spark等价替代 | `ce5bbe69`、`20e942f6` | Git核得父提交相同，完整tree同为`038278d8`；不同SHA不表示文件内容丢失 |
| Insight局部补证据 | `f4c35357` | 视觉证据缺失时保留的候选及技术验收报告 |
| Insight后续终验 | `c558505c` | 终验报告对象，不能反向证明重复G9有益 |
| Access旧合同 | `7700038b` | 原整页PASS后来撤回 |
| Access新合同 | `1b5804fd` | 信息架构改变后的新Plan终验报告，不追认旧PASS |

组织Graph的G5/G6/G7代际与代码关系见图3和008-GIT：G5不是G6祖先，Graph后端文件在G5存在、G6缺失；G5后来是G7及最终候选的祖先。G8缺独立代码关系核验，没有补画关系。

Spark原合同要求25类向导，候选只有13类，旧测试阈值为至少12类；后续报告补齐25类。另有下游可靠性候选未继承已验Web候选及恢复候选的祖先核查，最终发布矩阵仍为NO-GO。这些记录分别涉及合同覆盖、产物继承与发布条件。

重复G9的五个节点、一个节点进入running及最终废弃保留为原生事实。根因“读取截断”是参与者解释，缺完整响应和并发轨迹，不计入设计失效或浪费估算。Spark Host的两个剩余任务最终因项目清理废弃，不作为双平台全矩阵验收通过。

## B. 筛查、阅读和结果比较层级

原结构筛查覆盖108个Plan，32个有generation，205条由32个根与173个子代构成。六个显式夹具贡献10条记录；排除后为26个根、169个子代，共195条，表1给出拓扑变化分类。

原研究筛查全部generation的reason与evidence文本，选择九个Plan源包。其414个Task、116条generation与3,420条消息是容器规模，不表示全部深读；编码包含100项不同原生记录，形成六主案例与两混合排除案例。三个源码blob读取失败已保留日志，不推定相应实现不存在。

谱系比较复用六组案例、13个合同片段和27个事件。B0为状态展示；B1包含完整单Plan本地证据，包括已出现的后继叙述；B2增加明确关联及端点。B2不是结果真值，也没有把B1故意缩成状态页面。

## C. 图结构、权重与字段差分

对父子节点集定义保留率`R=|V_parent∩V_child|/|V_parent|`、新增占比`A=|V_child−V_parent|/|V_child|`。边按端点、kind、when、max_rounds归一为元组；属性编辑可表现为一删一增。原生from_task_id所指Task依赖to_task_id，图中展示方向需要与字段语义对应。

169个父子转移来自20个有子代Plan，另外6个Plan仅有根。转移等权与Plan等权分别回答典型转移和典型有子代Plan的平均新增占比，结果为12.7%与18.9%。根／最后比较覆盖全部26个有记录Plan，最后按时间确定，不作为成功终局。

字段比较仅覆盖父子共有Task的status、assignee_ref、description、title、delivery_contract。一个转移可改变多个字段，Task也可跨转移重复；新建或删除Task不进入共有字段计数。三次拓扑未变记录仍有2、2、1个Task状态变化；未检查完整序列化快照的每个字段，不声称全内容等价。

显式夹具排除不等于已排除全部系统故障。结构计数没有转成“有效恢复率”或“无效折腾率”；代码、补充信息、人工新要求及验收范围仍分别核对。

## D. 原始来源与复算入口

| 原来源标签 | 既有材料 |
|---|---|
| 008-M1、总报告 | [打开](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-008.md) |
| 008-GIT、source-checks.json | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json) |
| 008-M2、结构聚合 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/derived/delta-summary.json) |
| 008-M3、阅读覆盖 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/reading/coverage.json) |
| 008-X、E04-14—18、E01—E06、排除台账 | [打开](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-cases-008.md) |
| 004-M、方法与层级 | [打开](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-lineage-004.md) |
| 制图底表与原生定位 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P4/data.json) |
| 源文件、字段与哈希 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P4/source-provenance.json) |
| E01-01、拒绝 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-01.json) |
| E01-02、修复报告 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-02.json) |
| E01-04、代际原记录 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-04.json) |
| E01-03、复验 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-03.json) |
| E06-07、原Task续修 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-07.json) |
| E06-08、同Task通过 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-08.json) |
| E06-10、唯一根快照 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-10.json) |
| E02-01、cursor缺口 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-01.json) |
| E02-05、多effect | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-05.json) |
| E02-06、终验报告 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-06.json) |
| E02-04、错基线 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-04.json) |
| E03-11、初始合同 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-11.json) |
| E03-01、13对25 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-01.json) |
| E03-02、补齐报告 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-02.json) |
| E03-03、下游错候选 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-03.json) |
| E03-05、恢复候选 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-05.json) |
| E03-06、NO-GO | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-06.json) |
| E04-03、根因自述 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-03.json) |
| E04-10、重复G9 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-10.json) |
| E04-12、running审计 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-12.json) |
| E04-06、仅补证据 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-06.json) |
| E04-08、终验报告 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-08.json) |
| E04-13、discard审计 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-13.json) |
| E05-06、Mac要求 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-06.json) |
| E05-08、本地分发要求 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-08.json) |
| E05-12、部署与缺口 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-12.json) |
| E05-14、复用部署链 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-14.json) |
| E05-24、任务状态1 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-24.json) |
| E05-25、任务状态2 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-25.json) |
| E06-01、旧PASS | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-01.json) |
| E06-03、撤回 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-03.json) |
| E06-04、新IA | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-04.json) |
| E06-09、新终验 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-09.json) |
| E05-16、放弃授权任务 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-16.json) |
| E05-17、放弃验收任务 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-17.json) |
| 全部父子差分 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/data/P4-deltas.json) |
| 26个Plan配对表 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/data/P4-plans.json) |
| 方案与权重定义 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/protocol.md) |
| 逐Task字段差分 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/data/P4-attributes.json) |
| Plan索引与根／最后配对 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/supplement.md) |
| 004-C、B1/B2比较 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/derived/comparisons.json) |
| 原G9 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-09.json) |
| Task1 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-14.json) |
| Task2 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-15.json) |
| Task3 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-16.json) |
| Task4 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-17.json) |
| Task5 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-18.json) |
| 暂停 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-15.json) |


## 011机制对照

新增256次短题模型响应，与历史消息、图快照及十二对结构反例分开计数。全部反例、规定正例、字段反馈和方法偏差见[实验附录](controlled-experiments.md)。未重跑历史生产验收，明显系统bug继续排除设计归因。
