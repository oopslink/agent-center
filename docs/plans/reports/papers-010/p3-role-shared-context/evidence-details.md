# P3 补充附录：名称、授权与共享读取明细

本附录集中007修订移出的日期、治理与加载明细。正文保留六概念区分、定量结果及体验证据；本附录不增加实验或将报告升级为已核事实。

## A. 偏好、授权与治理记录

| 记录日期／时间 | 内容 | 证据定位与边界 |
|---|---|---|
| 2026-06-15 | 名称可点击并打开Agent详情 | N01-E01，交互偏好 |
| 2026-06-29 | 页面优先显示名字 | N01-E02，交互偏好 |
| 2026-07-03 | 消息用稳定ID，展示再转名称；两种提及功能一致 | N01-E04、E05，设计要求 |
| 2026-08-21 | 提及时使用名字 | N01-E03，指代偏好 |
| 2026-08-13 03:09 UTC | PD报告`not_memory_curator`，可读取／提案而不能晋升／拒绝 | N02-E01，参与者报告 |
| 同日04:10 UTC | 同身份Curator记录创建 | N02-E02，冻结记录；不替代完整授权历史 |
| 同日06:42 UTC | 报告完成提案处理 | N02-E03，参与者报告 |

治理报告提出整理20条重叠提案、建立规范条目和替代关系，后续报告8条晋升、12条拒绝及待处理清零（N04-E02、E03）。研究未取得完整记忆仓库正文，这些数量不表示名称造成的成本或浪费。

历史机制验收报告在100条、每条32KiB合成正文条件下，给出execute索引25条且序列化小于16KiB的结果；集成报告称成果进入主线（N05-E03、E07、E08）。这是机制报告，不是本研究重跑的模型效果或生产token收益。

## B. 材料筛选与配置规模

原专项筛查26,961条消息，得到835条去重词法候选；33条用户命中逐条筛选后，23条归为偏好或设计输入。目的性选择19个锚点，得到90条去重邻接消息；最终编码42个证据用途、41项不同源记录。N02与N04复用的同一记录只计一次来源。

16组Profile名称与Identity显示名对应16个稳定身份。14个Agent有团队角色，15行角色指派覆盖7种角色定义，其中一个身份兼任PD与PM。名称与身份、角色、权限的实际关系见原配置映射。

230条规则加载行覆盖12个Agent、两个团队和8个commit。主团队贡献228行、11个Agent及6个commit；另一团队贡献2行、1个Agent及2个commit。已保存加载行的时间范围为2026-08-14至2026-09-21。跨团队不合并同名规则版本。

## C. 共享与敏感性计算

版本化规则以`(team, commit, rule_slug)`为单位，共34条；30条被至少两个Agent读取，共129个Agent—规则关联。折叠版本后以`(team, rule_slug)`计数，共10条，其中9条被多个Agent读取；此口径不再说明读取同一版本。

规则集合按`(team, commit, agent)`去重，同team、同commit的Agent对计算`J(a,b)=|R_a∩R_b|/|R_a∪R_b|`。双方角色集合有交集编码为同角色；双方已知且无交集编码为跨角色。8个team–commit层中2个只有一个Agent，其余形成78个对×commit观测、46个不同Agent对。再限定phase后，19个层形成68个配对观测，同角色27、跨角色41。

共同分层等权先在每层分别求两类配对的平均J，再对同时存在两类观测的层等权。commit口径有5个共同层，commit加phase有4个。共同支持限制和权重同时变化，不能将前后差别完全解释为纯权重效果。逐一删层后重算其余共同层等权均值差，范围见正文表6，不是置信区间。

## D. 审计context与角色配置口径

context按`(agent, team, context-kind, context-id)`去重，优先使用execution_id，否则使用planning_session_id。230条加载行对应67个context，其中4个execution、63个planning。context不是模型turn；phase字段中的execute也不等同context类型execution。

完整矩阵下方的角色配置按冻结角色集合整体计数，PD/PM合为一个配置，避免把同一成员重复计为两个读者。这与配对分析中的角色集合相交定义不同。28/34条规则跨至少两种配置，正文表5各配置规则集合有交叠，不应相加。

图2的R20–R23对应最早观测commit `daabe994`的四条规则，具体slug保留在正文表3；全部R01–R34及C1–C8映射在006补充索引中。

## E. 原始来源与复算入口

| 原来源标签 | 既有材料 |
|---|---|
| N01-E01、消息01KV5HZX… | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E01.json) |
| N01-E04、ID与显示分离 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E04.json) |
| S04、S06、S09、S01—S11、S01—S04、S15—S16、S09—S11、S、带commit、行范围与哈希的固定源码摘录 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/source-evidence.md) |
| M1、阅读覆盖 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/coverage.md) |
| M2、聚合 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/derived/metrics.json) |
| X、007排除台账 | [打开](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-identity-cases-007.md) |
| 制图底表与原生定位 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P3/data.json) |
| 源文件、字段与哈希 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P3/source-provenance.json) |
| N01-E02、页面偏好 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E02.json) |
| N01-E03、提及偏好 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E03.json) |
| N01-E05、等价要求 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E05.json) |
| N01-E06、实现报告 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E06.json) |
| N02-E01、拒绝 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N02-E01.json) |
| N02-E02、Curator行 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N02-E02.json) |
| N02-E03、处理报告 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N02-E03.json) |
| M3、配置映射 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/derived/identity-role-map.json) |
| N03-E01、事实 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E01.json) |
| N03-E02、修正 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E02.json) |
| N03-E03、注入消息 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E03.json) |
| N04-E02、规范化 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E02.json) |
| N04-E03、治理结果 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E03.json) |
| N04-E04、传达问题 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E04.json) |
| N04-E06、容量担忧 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E06.json) |
| N05-E01、拒绝硬编码 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E01.json) |
| N05-E02、短索引设计 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E02.json) |
| N05-E03、加载接口 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E03.json) |
| N05-E07、历史验收 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E07.json) |
| N05-E08、交付报告 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E08.json) |
| 逐对底表与公式 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/data/P3-pairs.json) |
| 阶段条件分析 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/data/P3-phase-pairs.json) |
| 全部结果 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/summary.json) |
| N06-E01 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N06-E01.json) |
| context及全矩阵 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/data/P3-contexts.json) |
| 完整规则／commit映射 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/supplement.md) |
| N05-E05、快照 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E05.json) |
| N05-E06、谱系纠正 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E06.json) |
| 自述副本 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/raw/developer-reflection.json) |


## 011机制对照

新增256次短题模型响应，与历史消息、图快照及十二对结构反例分开计数。全部反例、规定正例、字段反馈和方法偏差见[实验附录](controlled-experiments.md)。未重跑历史生产验收，明显系统bug继续排除设计归因。
