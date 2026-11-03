# DAG evolution 008：案例、反证与排除记录

2026-11-02。**单AI探索，尚未人工复核。** 全部案例时间为UTC。下文“PASS/REJECT”均为历史记录中的结论，除明确标注的Git检查外，本轮没有重跑产品验收。Plan、Task和generation是关联容器，不是相互独立的实验。

总方法与取舍见[专项报告](agent-harness-evolution-008.md)。完整原生记录见[证据索引](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/index.md)；结构数字来自[逐generation差分](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/derived/generation-deltas.json)。`+节点/+边/−边`比较整个历史快照，不能解释为活跃任务增量或工作成本。

## E01 Mermaid：代码修复在改图之前

对象：`plan-67822059`，2026-08-17。纳入正常产品缺陷、代码修复、独立检查片段；排除旧集成在REJECT后ready的调度问题。

| 时刻 | 事实及来源 |
|---|---|
| 08:53:35 | Tester报告回归通过，但真实浏览器6类Mermaid出现pending，给出REJECT。[E01-01](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-01.json) |
| 09:00:40.896165 | 已报告推送修复`6855b0bd`，基于拒绝候选`deccd130`，调整初始化/渲染/SVG队列并补测试。[E01-02](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-02.json) |
| 09:01:27.383034 | `generation-384637ec`正式补修复和复验：+2节点/+1边，保留旧节点及集成阻塞历史。[E01-04](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-04.json) |
| 09:13:15 | Tester对同一`6855b0bd`真浏览器复验报告PASS；含6个成功块、1个预期错误块及多视口。[E01-03](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-03.json) |

**判断：**独立检查提供新事实，代码改变渲染行为，图为后续复验和集成补齐正式路径。修复报告比改图早46.486869秒，足以否定“这次图变化先产生了修复”的时间解释；不等于知道代码实际完成时刻，也不证明后续图无用。没有人工新增需求的记录；该Plan源包无user消息不等于整个需求历史无人工贡献。不能把同案调度bug重新计入DAG负效应。

## E02 组织Graph：局部迭代有信息，图历史不保证代码继承

对象：`plan-236dadc8`，2026-09-03。重点是G5至G9的产品语义迭代，不将早期runner/liveness及最后交付检测问题作为设计负例。此处“组织Graph”是被开发的可视化产品，须与用于执行计划的DAG区分。

1. G5候选`2c6a5d38`已通过组织首屏、真实后端Graph、关系、筛选等主体检查；14:24发现**同一语义边跨cursor页不稳定归并**。`generation-e34fe3b7`在14:26追加最小G6：+3节点/+3边/−2边，明确保留G5成果。
2. G6候选`2aebe30c`局部cursor测试虽通过，15:22审查却发现它从旧main `14ef9fd7`重建，丢失G5功能。`generation-22f1c16c`追加G7：+3节点/+3边/−3边，要求基于精确G5并复验不回退。
3. G7 `4d4ccf32`恢复正确祖先，聚合/count/time/polarity通过；15:40新发现**合并边只保留一个effect ID，无法取齐多页证据**。`generation-170ca966`追加+3节点/+3边/−2边，要求点击时请求两个effect端点并去重共享事件。
4. G8同project用例通过后，15:56发现**跨project贡献者的请求错误共用第一个project ID**。`generation-99b0e9c7`再追加+3节点/+3边/−2边，明确P1/P2请求与证据去重。16:24最终`1ba42c24`报告PASS，16:36报告main已接收。

本轮独立读取本地Git对象确认：G5不是G6祖先；G6的父提交为旧main；`internal/observability/collaborationeffect/graph_sqlite.go`在G5存在、在G6不存在；G5是G7和最终候选的祖先。这里没有用“参与者说丢代码”代替全部检查，也没有宣称已独立重验全部UI能力。[Git命令与结果](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json)

**正证据：**各轮反例有不同输入与断言，图把剩余缺口变成局部修复及新复验路径；不能将多次REJECT等同于同一件事反复做。**不利证据：**保留G5节点和“基于G5”的文字，确实没阻止G6的产物回退。**归因限制：**没有完整executor输入、工作区选择和运行版本，错基线可能有多种来源，不能把它当已排除bug的DAG因果负例。它支持“必须单独验证产物继承”这个边界判断。

本Plan另有用户催查、要求supervisor接手、继续等消息，不能称整段完全无人介入；具体cursor/多effect/跨project诊断来自Agent。最后的“manual recovery”也是Agent报告；其将自动eager-push判定称为误报，但本轮只核实提交关系，未验证该检测根因。

关键来源：[G5拒绝 E02-01](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-01.json) · [G6回退 E02-04](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-04.json) · [G7新缺口 E02-05](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-05.json) · [最终验收 E02-06](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-06.json) · [收尾限制 E02-07](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-07.json)。各代原生快照见E02-08至11。

## E03 Spark：补齐原合同；不同SHA也能保留成果

对象：`plan-5a5f20b6`，2026-09-01。初始generation的T1927已经写明“11个主页面与25类向导”；25不是失败后新增的产品范围。[原始快照 E03-11](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-11.json)

11:29审查`ce5bbe69`发现只有13类向导，而测试仅要求至少12类，测试绿不足以满足25类合同。`generation-b78cacaf`新增3节点/+4边/−4边，明确补齐向导、保留已有页面/详情，并接续可靠性与发布核验。13:02对`87eb1749`报告25个唯一ID及状态验收通过。[拒绝 E03-01](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-01.json) · [通过 E03-02](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-02.json)

此处有必须保留的反证：修复链把旧成果重建为`20e942f6`，本轮Git核得它与`ce5bbe69`具有相同父提交`c854aac0`和相同完整文件树`038278d8`。**SHA变化本身不是丢成果或重复实现的证据。** 这里只确认快照内容相同，不估计重建所花工作。

随后13:11，下游可靠性候选`5033340b`又被拒绝：继承的是旧拒绝基线，没有纳入已验收Web候选`87eb1749`。`generation-246fabed`新增2节点/+3边/−3边并强化候选合同；此前路径也已有生命周期与Web前提，不能说本轮首次发明了这两条依赖。13:31另有`generation-57b10f96`处理lease/stale，按未定运行问题排除设计负效应。13:43对恢复候选`d4cc8f6c`报告通过，本轮Git确认`87eb1749`不是坏候选祖先、是恢复候选祖先。[坏基线 E03-03](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-03.json) · [恢复 E03-05](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-05.json)

**结果边界：**14:44发布矩阵虽作为报告被接收，其内容仍为NO-GO，包含命令路径、组件类别、gRPC和缺Docker环境等问题。因此只能说向导及该恢复候选有局部正证据，不能声称整个产品完成或所有后续推进都由`246fabed`造成。[发布NO-GO E03-06](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-06.json)

## E04 Insight：重复五节点与保留有效候选

对象：`plan-929d9aaf`，2026-08-31。此Plan已是004重建后的开发交付合同；不能将其结果回填旧Insight Plan。

| 时刻 | 原生变化 / 后续证据 |
|---|---|
| 19:46:41 | `generation-8285e89c`建立canonical G9，+6节点/+7边/−1边 |
| 20:06:37 | `generation-fa2eacb6`再次基于原G8拒绝新增5个G9节点，+6边；没有相应的新产品缺口证据 |
| 20:07:12—20:07:33 | 重复根节点先running，再discard；其余4个也discard。一个参与者随后仍报告已有executor，故不能认定未执行 |
| 20:08:00 | PD说明首次读取截断，完整快照后发现canonical链已存在并撤销重复；**这是根因自述** |
| 20:14:49 | canonical候选`f4c35357`仍有效，报告为原G8的后继 |
| 20:26—20:29 | 视觉证据未交付；`generation-b8c6c9e1`只新增视觉复验/Ship/终验3节点，+4边/−4边，保留候选与技术验收 |
| 21:08:48 | 对最终main `c558505c`报告PASS，持久报告指明G9祖先及A–F范围；本轮未重新执行 |

**重复事实比重复原因更确定。** 五个重复Task ID、创建时间和discard状态有原生数据；根节点曾running有审计。可观察窗口内没有独立新增交付，支持将这五节点记为重复建链；不能定量估算开销。读取截断可能涉及API、上下文选择或并发，本轮不能隔离，所以不计入设计负效应或“LLM认知问题”主证据。

**有效部分：**后续没有因为视觉证据缺失就重做健康代码，而是保留同一候选、补缺失验收并接续交付。缺失证据的原因未知，退出设计失败归因；“只补缺口”的决策与最终报告仍可观察。旧失败/过期提示未触发再改图也保留为反证，但不把错误提示当设计缺陷。

来源：[重复诊断 E04-03](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-03.json) · [曾运行审计 E04-12](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-12.json) · [只补证据 E04-06](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-06.json) · [终验 E04-08](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-08.json) · [复用004持久报告](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/sources/insight-final-report.md)。原生代际、五Task和审计见E04-09至18。

## E05 Spark Host：用户改需求，外部等待，最终放弃范围

对象：`plan-70ab192f`，2026-09-02至09-04。23个generation不能笼统称为重复折腾：其中混合产品变更、普通代码修复、运行恢复、发布授权和真实设备缺失。

用户先要求不要给自己排任务、技术调整在会话讨论；`generation-f2504959`移除人工Gate并保留既有技术方案。后来用户又明确接受后续手工发布/真实Host验收。这是用户对责任边界的调整，不应被自动归为失败补救。

09-02 16:27用户提出Mac可加入、一台Mac模拟多个Spark、Node Agent不应DGX专用，17:04批准在当前Plan实施；`generation-4a151a8c`新增2节点/+2边/−1边。09-03用户说明没有对象存储/CDN，要求Manager默认本地提供制品服务；`generation-1d659ac1`新增2节点/+2边。后续又要求统一Mac/Linux及产品页方案。这些包含**真实的新产品/环境信息**，不能算相同验收合同的重复失败。[Mac需求 E05-06](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-06.json) · [本地制品需求 E05-08](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-08.json)

09-03 14:33报告精确候选`dc19fa11`与Agent1.4.4已部署健康，Mac产品页命令已完成CA、下载与摘要步骤，但sudo、真实Linux/DGX和public-CA目标未具备。`generation-a2577996`新增2节点/+2边：真实用户提供前提，Agent在已有部署上完成剩余验证，明确不重复部署。[部署边界 E05-12](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-12.json) · [改图 E05-14](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-14.json)

**后续没有证明全矩阵通过。** 09-03 15:40暂停；09-04 06:13，两任务都以“按项目清理要求归档、不再执行”discard。当前Plan done不能抵消这个事实。[暂停 E05-15](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-15.json) · [用户前提任务放弃 E05-16](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-16.json) · [剩余验收放弃 E05-17](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-17.json)

**判断：**图表达了新增范围和等待边界；不能替用户提供设备与授权。等待是正常条件，不作为DAG失败；错误upstream提示和过期lease退出设计负效应。最终“收口”部分来自停止执行，不能拿它证明evolution成功完成剩余范围。用户关于“很多evolution还4/7”的体验保留，但不代替成本或根因证据。

## E06 Access：换合同的成功，与不增代的修复

对象：`plan-a77ec987 → plan-e91d02b8`，2026-08-23。它们是交付目标下的跨Plan重建，不是同一Plan的parent-generation关系。

旧Plan曾对`7700038b`报告PASS。13:55用户否定整页结果，PD随后撤回局部检查外推的整页结论；14:52用户进一步明确新IA：Access只保留两入口，Team Role的RAM Roles在Team Role中管理。15:03要求新建Plan并统一交给PD。新Plan的六个Task在新ADR-0059全状态合同下执行，17:32对`1b5804fd`报告终验PASS。[旧PASS E06-01](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-01.json) · [撤回 E06-03](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-03.json) · [新产品要求 E06-04](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-04.json) · [终验 E06-09](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-09.json)

因此不能说“换一张更小DAG把原合同自动修好了”：人工否定、合同更换、实现变化、负责人集中同时发生。也不能用它证明一个PD胜过多角色。旧PASS撤销仍有效，不能被新合同结果追认。[新合同原文](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/sources/access-adr0059.md)

其中一个更小的片段提供了不必每次增代的反例：S1在15:28因缺Risk字段、typed-delete预填且默认启用被REJECT；负责人要求在**原分支追加commit**。15:34对`06183d8c`报告两项已修、同Task通过。原生新Plan只有一个初始generation，六Task数量与初始快照一致。可确认此局部修复未新增generation；不据此断言整个Plan从未做其他live edit。[原Task修正 E06-07](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-07.json) · [同Task通过 E06-08](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-08.json) · [唯一根 E06-10](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-10.json)

旧Plan的superseded/effective与调度错误另行排除；此案保留的是语义验收、用户新合同及原任务续修事实。本轮没有重新视觉审查所有原始页面。

## X01—X02及其他排除与混杂

| 片段 | 不允许承担的结论 | 仍保留的观察 |
|---|---|---|
| X01 旧Insight `plan-d7ec8ee4`，33代148Task | 把运行liveness、未加载修复、缺工具、环境/部署混合问题归为DAG正常设计无效 | 用户明确独立开发测试环境、开发与生产发布边界；后继合同在004已分开 |
| X02 I166 `plan-523d5d71` | inline/fork合同错误、REJECT后ready、部署超时not-found证明改图徒劳 | `be56cafc`零拓扑变化选择hold；`eba63900`结合用户澄清职责与新测试夹具，`b9412e39`接续阶段验收报告；并非纯图效应 |
| E01 Mermaid | REJECT后ready证明图方案无效 | 普通渲染缺陷、修复、复验可分离 |
| E02/E03 错候选基线 | 已证明是DAG造成的模型错误 | Git产物关系、保留节点不充分保证成果继承 |
| E03 lease/stale恢复代 | 把运行状态修复算正常机制负担 | 向导与Git关系单独保留 |
| E04 重复G9 | 读取截断已证为复杂度导致的认知问题 | 原生五重复节点及废弃，无新增交付观察；根因未知 |
| E05 真实外部输入 | 等设备/授权等于流程失败 | 可复用已部署成果；后续范围被放弃，不是PASS |
| E06 旧失败传播 | 调度bug证明重建必要/无用 | 人工新合同与原Task续修 |

这些是选定片段的研究地位，不是对全部173个子代完成了根因标注；没有“过滤后失败率”。机器可读版本见[8项排除台账](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/derived/exclusions.json)。X01/X02原生记录见[证据索引](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/index.md)。

## 不支持的总体推断

不能从这些案例估计DAG evolution成功率、节省成本或净损失；不能拿生成代数、累计Task或消息量当token/时间成本；不能将后继PASS归给此前所有改图；不能把原生父子链完整等同于运行正确；也不能用一个有bug的片段否定整项能力。保留的取舍是：局部调整、产物继承检查、合同版本化和明确等待各有具体用途，自动换代不是它们的替代品。
