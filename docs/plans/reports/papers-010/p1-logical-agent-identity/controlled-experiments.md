# P1受控实验001：结果、反证与归因边界

2026-11-03。同一AI开展的探索性实验，尚未人工复核。**清除可控混杂后，未检出统一身份封装或常用角色标签的不利效应。** 三个首步未入队执行体的输出发生在旧历史条件下，属于多做一次合法准备动作；全部在下一步启动。当前结果不支持把它们写成身份混淆或停摆，也不支持训练结构冲突已成立。

## 1. 完成与保全

冻结主设计A512次动作选择、B32次规则缺失诊断，全部完成，响应均可解析、模型名均匹配，无传输重试、缺失或越权。预定C共16组复用A首步，全部已到达控制边界，因此新增调用为0。观察到三个首步未推进后，另在调用前保存事后诊断协议，对全部三个追加反馈，各1次，共3次。正式模型调用总计547，另有2次不计入样本的连通性请求。

[冻结协议](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/protocol.md) · [设计哈希](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/design-lock.json) · [方法细节](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/method-details.md) · [全部输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/design.json) · [调用完成状态](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/run-status.json) · [逐行评分](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/analysis/rows.csv)。原始逐尝试argv、输入、stdout、stderr、时长、usage及完整输出在raw/和recovery-raw/。没有调用生产产品或执行历史部署。

## 2. 分层结果

| 条件／指标 | Sonnet 5.5 | Haiku 5.5 | 含义 |
|---|---:|---:|---|
| 说明一致＋无旧协议 | 64/64 | 64/64 | 干净子集首次到达控制边界 |
| 说明一致＋旧协议记录 | 64/64 | 63/64 | 1次先start，没有入队执行 |
| 遗留reset说明＋无旧协议 | 64/64 | 64/64 | 此强度的说明干预未降低本测量表现 |
| 遗留reset说明＋旧协议记录 | 64/64 | 62/64 | 2次先start，没有入队执行 |
| 全部A | 256/256 | 253/256（98.83%） | 有限合成情境的首步推进，不是真实交付率 |
| A主体匹配且动作合法 | 256/256 | 256/256 | 无错主体、越权、重复fork、不当reset或无请求等待 |
| B回答unspecified | 16/16 | 16/16 | 默认预期诊断，不按正确率统计 |
| 预定C首步到边界 | 8/8 | 8/8 | 未触发后续，不能声称已测纠错 |

干净子集内，每个模型的统一身份32/32、组件表述32/32；32个配对的推进率差均为0个百分点。常用标签与中性标签也是各32/32、差0个百分点。未检测到差异不证明两种架构等价：两组共享完整关系说明，只改一句身份表述，且基线存在天花板。

保持工具说明一致，Haiku的旧历史/干净历史配对为63对同时推进、1对只在干净条件推进，差−1.5625个百分点；Sonnet为64对同时推进、差0。保持无旧历史，说明一致性配对两模型均64对同时推进、差0。没有显著性检验；这些是固定题集上的描述差异，不是独立生产任务的效果估计。

![完整条件矩阵、配对差异与默认选择](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/figures/p1-fig4-controlled-experiments.png)

图：上部64格合计512输出，每格8个身份×标签×呈现组合；左下为模型分层的预定配对差异，身份/标签只取干净子集，历史只取一致工具，说明只取干净历史；右下为32诊断选择。没有总体置信区间。[矢量PDF](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/figures/p1-fig4-controlled-experiments.pdf) · [SVG](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/figures/p1-fig4-controlled-experiments.svg) · [图源](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/make_figure.py) · [图形底表](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/analysis/figure-data.json)。

## 3. 全部三个首步未推进输出及反向证据

三个输出均来自Haiku的S1、组件表述、呈现版本0，全部正确选择controller。可见理由均把获取lease/进入running当作fork之前的步骤，但冻结协议允许从open直接fork。它们既没有声称中心会自动创建执行体，也没有放弃后续任务；因此“旧规则导致等待中心”不能由这些文本推出。

| 原始输出 | 工具／标签 | 首步 | 仅换一个因素的对照 | 反馈后下一步 |
|---|---|---|---|---|
| `A-haiku-C08-S1-v0` | consistent / neutral | start_task | history: `A-haiku-C04-S1-v0` → fork_executor；identity: `A-haiku-C06-S1-v0` → fork_executor；variant: `A-haiku-C08-S1-v1` → fork_executor | fork_executor |
| `A-haiku-C16-S1-v0` | stale / neutral | start_task | history: `A-haiku-C12-S1-v0` → fork_executor；identity: `A-haiku-C14-S1-v0` → fork_executor；variant: `A-haiku-C16-S1-v1` → fork_executor | fork_executor |
| `A-haiku-C15-S1-v0` | stale / standard | start_task | history: `A-haiku-C11-S1-v0` → fork_executor；identity: `A-haiku-C13-S1-v0` → fork_executor；variant: `A-haiku-C15-S1-v1` → fork_executor | fork_executor |

所有对应的统一身份表述、无旧规则和版本1输出均直接fork。三个异常没有发生在统一身份组，对“统一身份必然制造误归属”构成反向材料；这同样不足以宣称统一身份具有稳健收益。三个观察集中于同一情境，并非三个独立故障或独立重复实验。

为区分额外步骤与停摆，主实验后对全部三个输出补做反馈观察，另立[事后协议](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/posthoc-recovery-protocol.md)与[调用前哈希](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/posthoc-recovery-lock.json)。模拟器只回报start的真实结果：running、lease active、没有worker请求；没有提供“下一步应fork”的人工纠正。三者下一步均选择fork_executor，达到请求入队边界，共3次追加调用。原来的253/256首步结果不改写；也不能把这种有状态反馈的二步路径外推为完整交付成功。[逐步原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/analysis/posthoc-recovery-summary.json) · [三组配对详情](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/analysis/nonprogress-comparisons.json)。

## 4. 正面行为与默认预期

按“每模型每情境的干净成功输出，ID字典序首条”固定选择16个正例，而非挑选合意解释。两模型在已有待受理fork和健康worker情境均合理等待，健康未知先查询，无中心权限的worker上报controller，已拒绝且获授权时安排修复，缺外部审批时请求授权。全量行为保存在逐行表；[正例选择](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/analysis/evidence-selection.json)与[原始内容](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/analysis/positive-examples.json)保留。

32个不完备协议诊断全部选择unspecified，解释通常区分共享归属与下一动作主体。没有观察到Supervisor词汇触发固定的默认创建者或修复者。题目明确提示“不确定则回答未指定”，可能压低默认补全行为；不能由此否定模型拥有隐含倾向，不能宣称训练内化结构不存在。它测到的是该提示条件下的显式选择。

## 5. 本轮对候选机制的约束

| 命题 | 本轮结果 | 仍缺的辨别证据 |
|---|---|---|
| 统一逻辑身份自身导致错主体 | 干净条件无差异；3例也全部不在统一身份组 | 长上下文、真实工具与实际多会话的拓扑对照 |
| 工具说明不一致是历史主因 | 无旧历史时此reset文字干预无效应 | 历史实际输入；更贴近reset恢复路径的任务，而非推断缺陷无害 |
| 旧上下文可能改变动作顺序 | Haiku出现1个一致工具下的配对差异；3例均有旧记录 | 更多固定任务与独立重复；区分上下文效应与采样变异 |
| 模型把Supervisor角色内化成只监督 | 明确协议下未见此行为，默认诊断均未指定 | 同源模型的受控训练干预；当前两个商业模型不能识别来源 |
| 多一步操作等同工作流停摆 | 3例下一步均fork，反对此等同 | 长程运行、真实产物、持续时间和完成率 |

本轮最有意义的收敛是：**责任表述、合法动作顺序、长期停摆与训练原因应作为不同层次的变量测量。** 当前结果支持继续调查契约与上下文的交互，削弱直接以统一身份解释既有事故的说法；没有证明任何单一根因。

## 6. 局限与后续判别

八类短题均提供明确当前状态、权限与责任映射，可能掩盖长任务下的表示负担。两呈现版本改变多个位置/顺序条件，不是随机重复。工具干预只改reset描述，且多数正确路径不调用reset；旧规则明确标作历史。中性标签仍保留fork_executor等动作名；系统提示使用泛称controller，因此不是完全无角色词基线。两个当前商业模型不能代表历史未知模型，CLI固定隐藏提示与模型训练均未被隔离。

恢复只模拟有限动作集合，无完整生产工具和代码验收。预定C没有错误可供恢复，事后3例又均来自同一S1；因此不能估计纠错能力或停摆率。没有同源训练干预，不能验证训练冲突的因果来源。后续应先加入协议仍一致的长程状态变换与真实工具执行，预先固定任务和反例，再分别比较简短身份宣言、显式关系表与确定性调度；不要通过删去必要信息人为制造失败。训练层需要可审计基础模型和成对轨迹训练，不用商业模型差异替代。

## 7. 复算、验证与费用

14个离线单元测试方法通过，含状态规则、因素隔离、解析、超时、失败重试及防覆盖；全量544原始结果的输入哈希、模型名、分母、独立首步动作真值表和配对差异通过4216项机械核对。后续专项检查另存。图形经过边界检查与人工式AI视觉检查，未进行独立或人工科学评审。

547次正式调用的CLI标价估计合计2.3246508美元，另有2个连通性请求不在此数。它不是订阅账单，也不是生产成本实验。时长与usage仅保存在[执行汇总](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/analysis/execution-summary.json)及原始记录中，不作跨模型性能排名。

[测试报告](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/test-report.md) · [结果核验](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/analysis/validation.json) · [全目录哈希清单](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/manifest.json)。不覆盖原009未执行设计，论文新版本与精确差分另存。
