# P5 初步实现梳理与设计取舍候选

日期：2026-11-03（America/Los_Angeles）。配套[研究计划](agent-harness-memory-011.md)。本轮是源码核查和元数据覆盖审计，不是生产效果实验。以下“收益/代价”是实现性质或待检验机制；没有用它们推断总体成功率。尚未人工复核。

后续增量：round-002 已补查记忆Git、部署产物、57条原生变更和230条规则读取回执，见[研究报告](agent-harness-memory-study-011.md)。下文保留首轮源码判断，M01–M08 是实现证据编号；后续案例同名编号属于独立的案例命名空间。新增版本核验覆盖9个文件、3个参考commit；不能消除dirty binary及历史激活时间缺失。

## 1. 版本边界

固定研究基线完整SHA见[source-manifest.json](memory-011/source-manifest.json)。22份文件已在仓库外保存原文与哈希，并与主工作区`ddaeb887`比较：8项相同、7项不同、7项不存在。详见[source-version-comparison.json](memory-011/source-version-comparison.json)。Team proposal/promotion及rule index相关代码存在明显版本差异。旧架构文档还描述CLAUDE.md、7类scope和不同加载方式，不能直接当作本轮源码的运行事实。

两份源码均未与每个历史任务的部署二进制绑定；实现存在、部署启用和任务实际使用三个层次必须另行核对。

## 2. 实现证据与取舍

| ID | 已核查的实现 | 可能的收益 | 代价/风险与证据边界 |
|---|---|---|---|
| M01 | `Engine`以per-agent `<home>/memory`为根，project/task/issue是其内部路径；Center RepoKind枚举agent/team/global，没有独立project repo kind | 私有经验按任务与项目定位；文件可直接检查 | 同项目各agent的project笔记不因此自动一致；不能仅凭本模块枚举断言系统任何位置都不存在项目知识共享 |
| M02 | 路径模型含8种scope；`AncestorScopes`可组织global→team→project→task/issue，并另行处理Supervisor | 具有从通用到具体的组织方式 | 这是路径和渲染顺序，不能保证模型遵循语义优先级；存在team层也不证明运行时已将所有共享正文复制到该路径 |
| M03 | 启动披露函数的自动候选是global根MEMORY.md和supervisor.md；其他Markdown列入按需读取清单。默认正文预算24 KiB、每文件8 KiB、清单4 KiB/80项 | 控制启动披露规模，保留主动读取入口 | 完整prompt还含引导文本、包裹标签、政策等，24 KiB不是总prompt/token上限；索引可见也不证明正文被读取；路径发现/排序和清单截断需测 |
| M04 | Agent记忆自动提交路径调用`CommitDirty`；Engine明确只负责本地commit，不负责push | 本地修改可保留Git历史，便于差分和恢复 | commit不能等同于Center已同步或其他agent已可用；实际push、失败重试、同步周期需沿上层调用核查 |
| M05 | Team Store区分`entries/`经验和`rules/`规则，MEMORY.md为派生索引；研究基线提供phase-scoped rule index和按commit读取正文 | 可区分经验与运行规则，按plan/execute/review/recovery筛选并定位版本 | 索引字段是否足以触发正确读取未测；规则正文加载机制不能外推为所有entries都被自动使用 |
| M06 | 研究基线的Team策略默认为proposal_only，显式curator grant可审核；提案不进入canonical索引；promotion将内容、提案状态和索引写入同一Git提交，采用版本冲突控制 | 审计和并发一致性有所保障，可阻止未批准内容立即影响运行 | 多一个审核阶段和等待点；提交原子性/权限有效不证明知识正确，审核质量和人工负担需实证 |
| M07 | rule reader/runtime保存commit和刷新语义：在途执行保留其已持久化输入，新fork或特定重置再读取 | 同次执行可复现，减少中途知识变化 | 更新可能不能立即影响旧执行；应区分有意冻结、失效策略不合适和系统bug |
| M08 | executor writeback的MemoryWriter接口在已检索生产调用点中未见`WithMemoryWriter`接线，调用只见测试；Supervisor可另行编辑个人记忆或提交Team proposal | 避免未经判断的执行输出自动升格为长期经验 | 这里只能说该自动写回入口未接线，不能说整个系统没有学习；后续需核查Supervisor实际是否做了筛选、写回及复用 |

## 3. 需优先验证的机制

**组织边界与知识需求是否错位。** Agent身份、project事实和Team约定是不同维度。把project知识放在个人目录便于管理，却可能使知识转移依赖消息转述或主动提升到共享库。应比较“同agent再次执行”和“更换agent继续同项目”，并检查任务输入本身是否已传递全部事实。结果也可能表明私有记忆已足够，或全量共享反而增加干扰。

**记忆存储与检索决策是否断开。** 当前预算机制把“读什么”的一部分责任交给模型。需要分别定位知识未写入、未列出、正文未读、读后未采用、采用但已过期，不能把每种失败都归为上下文长度。先给予相同正文做上限对照，再测试真实索引读取路径。

**共享知识治理是否产生净收益。** Proposal/promotion的价值可能在于阻止错误泛化，其成本可能表现为队列等待和人工维护。应把“避免了错误推广”的正例与“有用经验长期未被采用”的负例配对，并核实审批前后任务是否存在复用机会。没有机会的条目不能记作复用失败。

**持久化是否形成学习闭环。** Git记录了文件变化，但知识改进要以未来任务中的正确行为为证。分析至少连通来源任务、记忆版本、后续读取和交付证据；事后在文件中找到一句类似教训不足以证明当时影响了决策。

## 4. 实现一致性审计候选（不作为设计失效证据）

研究基线的`centergit.Authorizer`仍按同team成员允许Git读写，而受控写入设计要求通过proposal/promotion。需要继续核对HTTP路由、凭据可达性、服务端hook及部署配置，才能判断是否存在实际绕过路径。当前只记录代码表面之间的待核查关系，未尝试生产写入、未认定已发生越权。若确认为实现缺陷，其造成的片段排除出设计效应比较。

同样，旧文档描述的候选优先级不等于代码的启动候选；主工作区缺少某文件不代表历史生产没有部署过。上述差异在M0/M1先解决，不能当作模型记忆能力的负证据。

## 5. 源码定位

所有链接固定到研究基线提交，哈希和副本另存，后续文件移动不会改变本次证据。各锚点详见[evidence-index.json](memory-011/evidence-index.json)。

| ID | 固定版本定位入口 |
|---|---|
| M01 | [internal/cognition/memory/runtime.go:34](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/cognition/memory/runtime.go#L34)；[internal/cognition/memory/path.go:61](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/cognition/memory/path.go#L61)；[internal/cognition/memory/centergit/repo.go:10](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/cognition/memory/centergit/repo.go#L10) |
| M02 | [internal/cognition/memory/runtime.go:82](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/cognition/memory/runtime.go#L82)；[internal/cognition/memory/path.go:22](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/cognition/memory/path.go#L22) |
| M03 | [internal/cognition/memory/runtime.go:15](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/cognition/memory/runtime.go#L15)；[internal/cognition/memory/runtime.go:252](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/cognition/memory/runtime.go#L252)；[internal/agentruntime/local_runtime.go:1477](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/agentruntime/local_runtime.go#L1477) |
| M04 | [internal/agentruntime/memory_sync.go:14](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/agentruntime/memory_sync.go#L14) |
| M05 | [internal/cognition/memory/centergit/store.go:56](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/cognition/memory/centergit/store.go#L56)；[internal/cognition/memory/centergit/consumer.go:192](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/cognition/memory/centergit/consumer.go#L192)；[internal/cognition/memory/centergit/consumer.go:229](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/cognition/memory/centergit/consumer.go#L229) |
| M06 | [internal/team/memory_policy.go:26](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/team/memory_policy.go#L26)；[internal/admin/api/agent_tools_team_memory.go:184](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/admin/api/agent_tools_team_memory.go#L184)；[internal/cognition/memory/centergit/team_memory_repository.go:284](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/cognition/memory/centergit/team_memory_repository.go#L284) |
| M07 | [internal/cognition/memory/centergit/consumer.go:38](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/cognition/memory/centergit/consumer.go#L38)；[internal/agentruntime/team_rules.go:36](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/agentruntime/team_rules.go#L36) |
| M08 | [internal/agentruntime/orchestrator/writeback.go:160](https://github.com/oopslink/agent-center/blob/48892d61efdf17c69e8c317b64c5bda2c5b18fc2/internal/agentruntime/orchestrator/writeback.go#L160) |

下一轮将增加真实记忆Git覆盖审计、历史部署映射和第一组复用链，不把本轮候选机制写成已证实的因果结论。
