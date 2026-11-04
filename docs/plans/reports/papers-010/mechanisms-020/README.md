# P2–P5：理论框架、受控决策与证据复算（020）

本轮新增的是 P3/P5 的 80 次实际模型处置、P2/P4 对既有运行和代际的再分析。四篇不是独立部署复现。P1 维持 019 版。原现场故障与实验仪器排除规则不变，没有改产品或部署。所有结果是 AI 辅助探索，尚未人工复核。

| 稿件 | 理论问题与对照 | 新证据及限制 |
|---|---|---|
| [P2](../p2-workflow-responsibility/paper.md) | 协调理论把分析单位从“多少角色”转为“哪些活动依赖需要维护”。固定交付目标，要多走哪些决策？ | 12 对中控制端共增 52 次、工作端增 39 次，各对两端均增加。删去停摆对后其他 11 对的中位增量仍为 7。分析既有执行，没有新独立样本；按交付结果选的子集只作敏感性检查。[全部配对](p2-pairs.csv) · [图7](../p2-workflow-responsibility/figures/p2-fig7-paired-decisions.pdf) |
| [P3](../p3-role-shared-context/paper.md) | 角色授权决定可读范围；消费者当前义务决定相关性。删除个人名后，再比较能否获得合适的跨角色事实。 | 无个人名、人名、角色标签三组分别 8/8 正确判定当前证据、8/8 正确请求旧证据的补充。单人团队体验不计入自动处置；模型全在首轮结束，48 次不是长期交付。[48 次动作与理由](p3-outcomes.csv) |
| [P4](../p4-dag-evolution/paper.md) | 计划修复要定位哪些依赖失效、哪些成果仍有效。比较拓扑与状态，再独立核对候选谱系。 | 166 次拓扑改变中 101 次共有任务转完成，3 次不变中亦有 1 次；不将这些字段等同验收。保留全部父节点 149/169，逐一排除单个 Plan 后比例为 85.4%–91.1%。G6 缺失 G5 已验后端、G7 恢复祖先关系；重复建链的技术原因仍未知。[169 个转移](p4-transitions.csv) · [留一分析](p4-leave-one-plan-out.csv) · [十项 Git 重放](git-replay.json) |
| [P5](../p5-memory-applicability/paper.md) | 组织记忆的获取、保留、提取与合乎任务合同的使用分开评估。固定任务事实，只换团队规则。 | 在已读规则条件下，宽规则使 1/4 中间节点暂缓、3/4 仍交接；无规则、条件化、新旧显式替代各 4/4 交接。最终阶段各 4/4 暂缓。不能据此声称条件化比无规则好、替代标记有独立效果或某层长期记忆更优。[32 次动作与理由](p5-outcomes.csv) |

**原始输入、评分与反证。** [冻结设计](experiment/design.json)、[协议](experiment/protocol.md)、[冻结文件哈希](experiment/lock.json)、[全部实际模型返回](experiment/turns.json)、[请求审计与模型标识](experiment/requests.csv)、[分组结果](experiment/summary.json)。两模型、固定场景、每格两个工单标记，并非 80 个独立项目。P3 的两种旧凭据输入无法区分底层候选真值；P5 的评分以当前任务合同为准，暂缓可体现谨慎，不能据此指控模型错误。全部样本在一个动作后结束，没有格式／传输错误。无后验重试。模型报告价合计 0.2115597 美元，非扣费账单。

**理论依据。** P2 使用[协调理论原文](https://crowston.syr.edu/sites/default/files/acmcs94.pdf)解释交接，P3 使用[角色访问控制原文](https://csrc.nist.gov/csrc/media/projects/role-based-access-control/documents/sandhu96.pdf)区分身份和权限，P4 使用[计划修改与复用原文](https://www.sciencedirect.com/science/article/pii/0004370292900564)解释局部修复，P5 使用[组织记忆原文](https://journals.aom.org/doi/10.5465/AMR.1991.4278992)区分保留和提取。原理论均不是对本部署的实证证据。[出版记录与撤稿状态核验](citation-check.json)；P5 出版方标识在 Crossref 映射到 JSTOR 记录，作者、题名、卷期页及原文首页已核对。

**复算。** [输入版本及 SHA](input-sources.json)、[P2/P4 分析脚本](reanalyse.py)、[统计输出](reanalysis.json)、[Git 核对脚本](verify_git.py)。脚本默认使用本机冻结研究目录；换机器需设置 PAPER_ROOT、STUDY_ROOT 并获得相同历史数据，Git 复核还需相应对象。实验的[完整原始请求、stdout/stderr 与工单状态](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p35-conditions-005)另存于本机，[压缩包与逐文件校验入口](experiment/archive.json)。在原始目录运行 `python3 -B audit.py` 可按实际可见材料复算。重新采样须将 [experiment](experiment/study.py) 整个目录复制到空目录（保留 sandbox），在认证的 Claude CLI 环境运行 `python3 -B study.py run`；不保证重采样逐字相同。

[四篇修改前后快照、差分与验证](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/020-p2345-mechanisms/README.md)。
