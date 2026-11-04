# 主张与文献支持关系

T5–T7、T12 写正文时按本表取引用。每行给出：这一节的核心主张、可定位支持的 bib key、以及**该主张中属于本研究新增、文献无法支持的部分**。最后一列是防止把本研究的观察伪装成已有共识的闸门。

## A 篇

| 小节 | 核心主张 | 可引用（已核验） | 文献支持不到、须标为本研究观察的部分 |
|---|---|---|---|
| 2.1 绑定六元组 | 一次交付的可执行性取决于目标、动作、主体、对象、合同、授权、前提是否都已确定 | `MC94`（协调即依赖管理）、`schmidt1996coordination`（机制＝协议＋承载它的制品）、`feldman2003reconceptualizing`（规则文本与规则执行是两个对象） | 六元组本身的切分方式，以及"断裂发生在边界处"这一组织方式 |
| 2.2 已有研究覆盖什么 | 失效分类、协调、拓扑、记忆各自回答不同问题 | `R01`、`shah2026characterizing`、`albayaydh2026beyond`、`zhou2026messages`、`pitre2026understanding`、`R05`、`R03`、`macedo2026harness`（harness 的可引用定义） | 这些工作未覆盖"单部署长期记录＋产物级核验"这一证据类型 |
| 2.3 概念来源 | 持续目标、协调、RBAC、计划修复、组织记忆各提供一条判别标准 | `CL90_COMMIT`、`MC94`、`RBAC96`、`KH92`、`WU91`、`babli2023plan`（承诺保持 vs 成本）、`ackerman2000reexamining`（记忆片段脱离情境后不可用） | 借用限度：这些理论都不是对本部署的实证 |
| 3.3 方法限制 | 单部署、事后框架、未预注册各限制哪类推断 | `RV16_METHOD` | 未完成全库盲法编码、AI 参与全流程这两项限制 |
| 4.1 责任绑定 | 一次执行终止不等于目标责任终止 | `CL90_COMMIT`（尝试结束不是放弃目标的理由）、`arora2026specifying`（委派自治边界该被显式规定）、`R14`、`marla2026permission`（重试路径上的授权再评估） | 本部署中责任外置的具体形态与人工纠偏过程 |
| 4.2 凭据绑定 | 一张回执是否对应当前候选，是必须被管理的依赖 | `wang2026approval`（批准—执行绑定失效）、`moreau2010foundations`（他方签发的断言此方是否接受）、`muto2026attestable`、`castillo2026evidence` | 本部署中对象与阶段错配的具体记录 |
| 4.3 信息绑定 | 消费者此刻的职责决定需要哪些事实，与材料由谁产生无关 | `RBAC96`、`R08`、`R32`、`zhou2026messages`、`yang2026privileges`（工具选择中的过度授权）、`R15`、`R17` | 230 条读取回执所显示的跨成员到达情况 |
| 4.4 成果与合同绑定 | 保留了计划节点不等于后继候选继承了已验证产物 | `KH92`、`nebel1995plan`（复用不必然优于重建）、`veloso1993derivational`（复用推导轨迹而非成品）、`vanderkrogt2005coordination`、`komenda2012decentralized`、`yuan2026replan`、`R26`、`R27` | Git 产物级核验显示的继承中断；旧 PASS 撤回后的适用范围 |
| 4.5 适用范围绑定 | 规则的阶段条件与任务阶段不匹配时，共享通路正常也会出问题 | `WU91`、`olivera2000memory`、`argote2000knowledge`、`feldman2003reconceptualizing`、`joshi2026deontic`（带条件的规范形式化）、`qi2026memory`（偶然共现被误晋升为团队规则）、`kirchdorfer2026organizational`、`park2023generative`、`packer2023memgpt`、`xu2025amem`、`zhang2025gmemory`、`wu2025longmemeval` | 宽 main 规则与中间候选合同形成循环的具体记录 |
| 4.6 剩余义务 | 局部完成与未满足的交付义务可以同时成立 | `R01`（验证失败类）、`R05`、`liu2026know` | 报告任务完成而产品未可发布的具体记录 |
| 5.2 训练先验 | 行为可由训练塑造，但本研究不能据此归因 | `R30`、`R31` | 历史部署模型的训练内容未知；无同源训练干预 |
| 5.3 跨代重复 | 单代无环不排除跨代重复 | `KH92`、`yuan2026replan` | 五个重复节点全部废弃这一有限重复记录 |

## B 篇

| 小节 | 核心主张 | 可引用（已核验） | 文献支持不到、须标为本研究观察的部分 |
|---|---|---|---|
| 相关研究 | 架构与角色划分可独立于模型能力影响结果 | `orogat2026architectural`、`hong2026learning`（协作量与收益非单调）、`zhou2026token`（以 token 计量协调成本）、`mediratta2026decentralized`、`zhang2026silo`、`RV16_METAGPT`、`RV16_CHATDEV`、`RV16_AUTOGEN`、`R11`、`R28`、`R29` | 本实验的两种具体角色配置下的决策数差 |
| 实验设计约束 | 必须在真实运行器上做实验，且必须固定 harness 版本 | `sghaier2026dont`（harness 版本演进的影响大于模型差异）、`macedo2026harness`、`R03` | 本运行器的具体实现与 design-lock |
| 实验一 协调成本 | 相同交付目标下，增加交接会增加决策数 | `MC94`、`zhou2026token`、`hong2026learning` | 本实验测得的具体开销与交付率 |
| 实验二 凭据认可 | 跨会话的有效凭据可能不被接受，这不是技术不可行 | `wang2026approval`、`moreau2010foundations`、`simmhan2005survey`、`muto2026attestable`、`asad2026kettle`、`castillo2026evidence`、`tamanna2024analyzing`（机制存在但不被采用的实证）、`sadowski2018modern`（人类评审依赖自动化证据的基线） | 本实验操纵三个可见性水平后的实测结果 |
| 局限 | 零结果不等于等效 | `RV16_METHOD` | 未做等效性检验这一点须自述 |

## 使用纪律

1. **预印本须标身份。** 新增 24 条为 arXiv 预印本（多为 2026 年），未经同行评审。正文引用时写"预印本"，不与正式出版文献并列作为已确立结论。
2. **最后一列不得为空。** 任何一节若最后一列写不出"文献支持不到的部分"，说明该节没有本研究的贡献，或者把观察说成了共识——两种情况都要回头改正文。
3. **检索非系统综述。** 每主题 1–2 条定向检索式，相关研究节须写明这一点。

## 本步未使用 bib-search-citation skill 的理由

计划 Task 2 Step 4 写的是用 `bib-search-citation` skill 核对支持关系。该 skill 的用途是对本地 `.bib` 做交互式检索并生成 `\cite{}` 片段；本步需要的是一份**持久的主张—引用映射**，供 T5–T7 与 T12 逐节取用，并且要同时记录"文献支持不到的部分"——这一列不可能由检索产生，只能由写作者判断。因此直接产出本表。两篇 `.bib` 的可解析性与被引情况仍由 `crossrefs.py --bib` 把关。
