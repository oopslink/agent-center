# Agent Harness 谱系重建逐案附录

**单AI探索，尚未人工复核；无独立重跑。** 本附录由已保存的语义标注JSON生成；生成脚本不替代语义复核。研究方法和限制见[0.4轮研究报告](agent-harness-lineage-004.md)。

保存10个存在的Plan端点、1个缺失Plan ID与1个无plan_id替代Task；端点包按ID去重为268 Task、3086 Message、63 generation。并非逐条读完全部消息。

## 1. 全部Plan的本地判断

下面每一行只概括该Plan本地可见证据。主表锚点以外的后继Plan也列出，不把它们本地已知的结果隐藏。

| Plan | 快照状态 | Task / generation / Message | B1本地判断 |
|---|---|---|---|
| `plan-d7ec8ee4` | discarded | 148 / 33 / 935 | 已discarded，明确转交新Plan；后继终验未知。 |
| `plan-929d9aaf` | done | 59 / 12 / 502 | 本Plan A–F下报告main终验PASS；生产release独立。 |
| `plan-569ab651` | done | 15 / 1 / 825 | 当前done，曾报9a512ef9 PASS；后继当前有效工作不由这一旧PASS覆盖。 |
| `plan-a77ec987` | done | 15 / 10 / 500 | 当前done但7700038b PASS撤销、曾重开；本地仅知e91创建。 |
| `plan-e91d02b8` | done | 6 / 1 / 23 | 新IA/矩阵下报告1b5804fd终验PASS；不恢复旧候选PASS。 |
| `plan-d1680545` | paused | 6 / 1 / 68 | paused；Operation替代交付和合main本地已报告；整体生产结果未知。 |
| `plan-7fa6888f` | done | 3 / 0 / 20 | done；合并门与验收转交可见，两个功能后续终验未知，sites上线自报。 |
| `plan-f7f98cfe` | done | 2 / 0 / 43 | 两项功能GO＋发布main/tag报告；sites并轨/直接过主张不等于逐模块验收。 |
| `plan-becb5960` | done | 8 / 3 / 86 | done是REJECT收口；局部PASS不覆盖terminal REJECT。 |
| `plan-67822059` | done | 5 / 2 / 80 | 同Plan候选修复后PASS并集成，分支新tip未验收。 |

替代`task-d26fcf10`：completed，无plan_id；自身会话含首次无交付、后续push与验收叙述。缺失`plan-2d41d1da`不加入状态分母。

## 2. 六组比较

### G01 Insight跨Plan重建

本地锚点：`plan-d7ec8ee4`。关联端点：`plan-d7ec8ee4`, `plan-929d9aaf`。

- **B0**：Plan=discarded；Task状态={"completed": 18, "discarded": 40, "failed": 90}。无成功/失败自动映射。
- **B1**：旧Plan discarded；本地可知新Plan已启动且重写A–F/发布边界，后继最终结果不可核实。
- **B2**：后继在I-C1下报告双验收及main c558505c终验PASS，持久报告可读；旧Plan保持discarded，生产release未知。
- **差异**：后继结果补全＋验收范围界定；不是旧失败翻成旧成功。
- **限制**：全部207 Task含支持性/平台修复工作，不能解释为纯Insight开发量；未检验简化是否导致结果。

关联容器累计：2 Plan、207唯一Task、45唯一generation；不是目标纯工作量或成本。

主要证据：[I-HANDOFF](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/I-HANDOFF.json)、[I-FINAL](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/I-FINAL.json)、[I-ARTIFACT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/insight-final-report.md)。

### G02 Access撤回与合同变更

本地锚点：`plan-a77ec987`。关联端点：`plan-569ab651`, `plan-a77ec987`, `plan-e91d02b8`。

- **B0**：Plan=done；Task状态={"completed": 13, "discarded": 2}。无成功/失败自动映射。
- **B1**：a77当前done；本地已经可见7700038b PASS被用户异议后撤回、重开、新文档与新Plan创建，后继最终结果未知。
- **B2**：串起569→a77→e91的Access部分承接；A-C2新IA/canonical下1b5804fd报告终验PASS并有持久报告；旧7700038b PASS仍撤销。
- **差异**：后继结果补全＋撤销/合同版本分离；不能把撤销发现归功于跨Plan关联。
- **限制**：三Plan范围不完全等价；36 Task是关联容器规模；同一PD设计实施验收减弱独立性；不是独立确认视觉合格。

关联容器累计：3 Plan、36唯一Task、12唯一generation；不是目标纯工作量或成本。

主要证据：[P-A1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-A1.json)、[A-OLD-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-OLD-PASS.json)、[A-REVOKE](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-REVOKE.json)、[A-REOPEN-AUDIT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-REOPEN-AUDIT.json)、[A-CONTRACT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/access-adr0059.md)、[A-FINAL](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-FINAL.json)、[A-ARTIFACT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/access-s6-report.md)。

### G03 Operation无Plan替代Task

本地锚点：`plan-d1680545`。关联端点：`plan-d1680545`, `task-d26fcf10`。

- **B0**：Plan=paused；Task状态={"failed": 2, "completed": 3, "discarded": 1}。无成功/失败自动映射。
- **B1**：父Plan paused，旧Task discarded；本地已明确替代Task completed、cda9136c交付及main ae578a8d；六领域整体/生产部署未知。
- **B2**：读取无plan_id替代Task及delivery字段、第一次non_delivery后有push；与本地结果主张一致；本地Spark Git缺两个对象，main集成仍只有参与者证据。
- **差异**：证据交叉核对与归属补全；没有结果翻转，也没有达到Git产物验证。
- **限制**：只评价Operation子目标；不把六领域Plan判为成功；结构化delivery本身也来自系统/agent记录。

关联容器累计：1 Plan、7唯一Task、1唯一generation；不是目标纯工作量或成本。

主要证据：[O-LINK](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-LINK.json)、[O-CLOSE](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-CLOSE.json)、[O-NEW](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-NEW.json)、[O-NONDELIVERY](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-NONDELIVERY.json)、[O-DELIVERY](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-DELIVERY.json)。

### G04 v2.10.3分项验收转交

本地锚点：`plan-7fa6888f`。关联端点：`plan-7fa6888f`, `plan-f7f98cfe`。

- **B0**：Plan=done；Task状态={"completed": 3}。无成功/失败自动映射。
- **B1**：开发Plan done；本地有80794195合并门、两个功能验收Task待交接，另声称sites已上线并转交缺失Plan；完整功能验收/最终发版未定位。
- **B2**：T171/T172落在f7f98cfe，对T167/T170报告GO，报告在402a5cea树可读；发布会话报告main/tag。sites仅转交/并轨/直接过叙述，完整模块验收仍未确认，Pages成功部署未知。
- **差异**：两个功能及发布证据补全＋纠正误连；不合并缺失sites Plan，不赋整Plan统一PASS。
- **限制**：新发现端点只作一跳显式Task归属扩展；sites后续任务未继续递归；两个功能GO不能证明sites模块或真实agent工具全链通过。

关联容器累计：2 Plan、5唯一Task、0唯一generation；不是目标纯工作量或成本。

主要证据：[V-FEATURES](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-FEATURES.json)、[V-TRANSFER](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-TRANSFER.json)、[V-T171](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-T171.json)、[V-T172](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-T172.json)、[V-GO1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-GO1.json)、[V-GO2](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-GO2.json)、[V-SITES-LIMIT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-SITES-LIMIT.json)、[V-RELEASE](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-RELEASE.json)、[V-ARTIFACT1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/v2103-tester1.md)、[V-ARTIFACT2](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/v2103-tester2.md)。

### G05 done但REJECT收口对照

本地锚点：`plan-becb5960`。关联端点：`plan-becb5960`。

- **B0**：Plan=done；Task状态={"completed": 8}。无成功/失败自动映射。
- **B1**：done/8 completed；本地terminal REJECT、no Ship、不合main，下一轮条件仅宣布。
- **B2**：仍为拒绝收口，未交付；结构化decision=reject佐证，不需要跨Plan补救。
- **差异**：结果不变；已被完整本地证据纠正的状态误读不算关联增益。
- **限制**：没有独立重跑600.522s超时；未来fresh-main合同不能当已通过。

关联容器累计：1 Plan、8唯一Task、3唯一generation；不是目标纯工作量或成本。

主要证据：[N-REJECT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/N-REJECT.json)、[N-DECISION](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/N-DECISION.json)、[N-LOCAL-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/N-LOCAL-PASS.json)。

### G06 同Plan修复与候选版本对照

本地锚点：`plan-67822059`。关联端点：`plan-67822059`。

- **B0**：Plan=done；Task状态={"completed": 5}。无成功/失败自动映射。
- **B1**：done；本地已有deccd REJECT→6855 PASS→main b0c40；同名分支新tip7ca明确未获验收。
- **B2**：判断不变；Git仅补证6855是b0c40祖先，不把7ca当已验收。
- **差异**：结果不变；候选SHA变化不自动生成新合同。
- **限制**：研究者未重跑浏览器或测试；没有证据认为整个需求合同改变。

关联容器累计：1 Plan、5唯一Task、2唯一generation；不是目标纯工作量或成本。

主要证据：[M-REJECT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/M-REJECT.json)、[M-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/M-PASS.json)、[M-INTEGRATION](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/M-INTEGRATION.json)、[M-ANCESTRY](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/mermaid-ancestry.txt)。

## 3. 类型化关系与纳入决定

| 边 | from → to | 类型 / 决定 | 限定范围与反证 | 证据 |
|---|---|---|---|---|
| L01 | `plan-d7ec8ee4` → `plan-929d9aaf` | same_goal_successor / accepted_scoped | Insight产品开发交付。生产release移到Plan之外，平台修复排除；不得把新A–F PASS回填为旧全部要求满足 | [I-REQUEST](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/I-REQUEST.json), [I-HANDOFF](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/I-HANDOFF.json), [P-I1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-I1.json) |
| L02 | `plan-a77ec987` → `plan-e91d02b8` | same_goal_successor / accepted_scoped | Access产品整改。同一高层目标，但IA/canonical与执行者变更；不声称a77标题中的Assignment Pool全部承接 | [A-REQUEST](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-REQUEST.json), [A-HANDOFF](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-HANDOFF.json), [P-A2](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-A2.json), [A-CONTRACT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/access-adr0059.md) |
| L03 | `task-b0d4ecaa` → `task-d26fcf10` | task_replacement / accepted_scoped | issue-14f10668 Operation子目标。子目标替代不代表六领域Plan整体交付；本地Git缺两个SHA，main主张仍E1 | [O-LINK](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-LINK.json), [O-CLOSE](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-CLOSE.json), [O-OLD](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-OLD.json), [O-NEW](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-NEW.json) |
| L04 | `plan-7fa6888f` → `plan-2d41d1da` | acceptance_handoff / unresolved_endpoint | sites v2.10.2模块核验。端点Plan/Task查询为空，只有单条ID引用；不合并，不推断从未存在或验收失败 | [V-TRANSFER](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-TRANSFER.json) |
| L05 | `plan-569ab651` → `plan-a77ec987` | same_goal_successor / accepted_scoped | Access有效未终态工作承接。仅据快照描述明确引用，一跳扩展；缺完整变更时点，整Plan目标不等价 | [P-A0](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-A0.json), [P-A1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-A1.json) |
| L06 | `plan-7fa6888f` → `plan-f7f98cfe` | acceptance_handoff / accepted_scoped | T167附件与T170 Issue工具，经T171/T172；后续发布会话。不等价于sites模块验收；不能用两个功能GO覆盖旧Plan全部目标 | [V-FEATURES](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-FEATURES.json), [V-T171](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-T171.json), [V-T172](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-T172.json), [V-GO1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-GO1.json), [V-GO2](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-GO2.json) |
| L07 | `A-C1` → `A-C2` | scope_revision / accepted | Access两入口与Team Role属性的新IA/矩阵。 | [A-ADR](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-ADR.json), [A-CONTRACT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/access-adr0059.md), [A-MATRIX](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/access-state-matrix.md) |
| L08 | `A-E1` → `A-E4` | acceptance_revocation / accepted | a77的产品终验结论。旧撤销不因新SHA PASS恢复；不自动撤销569其它SHA的全部历史判定 | [A-OLD-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-OLD-PASS.json), [A-USER](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-USER.json), [A-REVOKE](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-REVOKE.json), [A-REOPEN-AUDIT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-REOPEN-AUDIT.json) |
| L09 | `I-C0` → `I-C1` | scope_revision / accepted | 开发完成与生产release分离；A–F。 | [I-HANDOFF](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/I-HANDOFF.json), [P-I1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-I1.json) |

L04保留未决；它不是“已确认不存在”，也不能进入同目标合并计数。L05仅由当前描述明确承接支持，缺完整历史生效时点。1758条字面命中及30例既有cross-plan线索不是已逐条筛完的排除集。

## 4. 合同片段

contract_id为研究标识，非生产实体/数据库version/generation。包含父子范围、partial和仅宣布的未来条件；不能将下表行数当已执行合同版本数。

| 合同 | 适用范围 / 状态 | 要求与变更 | 证据 |
|---|---|---|---|
| I-C0 | Insight旧Plan / observed | Overview/Agents/Projects；Project交付/Execution健康/Evolution lineage；阶段验收与remediation。变更/限制：原始v1设计全文和历史每次门禁变更未完整重建；生产release旧边界只由后继说明逆向可见 | [P-I0](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-I0.json), [I-REQUEST](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/I-REQUEST.json) |
| I-C1 | Insight新开发交付 / observed | A–F：事实链、UI与状态矩阵、同SHA技术/产品PASS、进main、新main隔离终验。变更/限制：相对I-C0明确新增/重申A–F；排除平台修复；生产release独立，不降低为done即PASS | [I-HANDOFF](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/I-HANDOFF.json), [P-I1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-I1.json), [I-ARTIFACT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/insight-final-report.md) |
| A-C0 | 569 Access三页全真交付 / observed | RAM/Team CRUD、Subject access、canonical视觉/行为、main/fresh instance。变更/限制：早期合同只从当前描述和验收行定位；不完整恢复每个旧mock版本 | [P-A0](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-A0.json), [A0-REJECT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A0-REJECT.json), [A0-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A0-PASS.json) |
| A-C1 | a77 Access承接 / observed | 附件物化/Team复验、RAM/Subject remediation、PD集成、三页canonical全状态终验。变更/限制：承接569有效未终态工作；标题比Access研究子目标更宽；旧PASS含父任务证据继承后被撤销 | [P-A1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-A1.json), [A-OLD-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-OLD-PASS.json), [A-REVOKE](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-REVOKE.json) |
| A-C2 | e91 ADR-0059整改 / observed | Access两入口；Team Role RAM Roles属性；新canonical全状态矩阵；唯一PD集成；fresh exact-SHA验收。变更/限制：确立新产品IA、保留CAS/audit；废止独立mapping页面；禁止父任务或默认单截图替代终验 | [P-A2](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-A2.json), [A-CONTRACT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/access-adr0059.md), [A-MATRIX](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/access-state-matrix.md), [A-FINAL](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-FINAL.json) |
| O-C0 | Spark六领域整体 / observed | 六Issue独立开发/提交/验收，全部后统一集成回归和生产部署。变更/限制：整体交付边界保持，当前只深读Operation | [P-O0](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-O0.json) |
| O-C1 | Operation替代Task局部 / observed | Operation执行取消重试恢复/幂等/副作用/审计；测试commit push；executor不得直接合main。变更/限制：已确认同Issue目标替代，执行归属修正；不是全部六领域交付 | [O-OLD](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-OLD.json), [O-NEW](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-NEW.json), [O-LINK](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-LINK.json) |
| V-C0 | v2.10.3开发容器 / observed | T167全会话附件、T170 Issue工具；T169 sites讨论/迁移。变更/限制：同Plan含可分离交付子目标，不能整体以两个功能验收覆盖 | [P-V0](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-V0.json), [V-FEATURES](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-FEATURES.json), [V-TRANSFER](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-TRANSFER.json) |
| V-C1 | T171/T172功能验收与发布 / observed | 80794195 T167/T170 data/API与浏览器/服务契约测试，持久证据入发布树。变更/限制：验收责任转交；agent侧部分采用服务契约集成测试，不能写成真实多agent端到端全覆盖 | [V-T171](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-T171.json), [V-T172](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-T172.json), [V-GO1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-GO1.json), [V-GO2](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-GO2.json), [V-ARTIFACT2](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/v2103-tester2.md) |
| V-C2 | sites模块验收主张 / partial_unresolved | M1入口/M2手册历史/M3 DDD历史/M4一致性→报告，v2.10.2 sites。变更/限制：只有转交描述和后来并轨/直接过叙述；端点缺失，不视为已执行完整合同 | [V-TRANSFER](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-TRANSFER.json), [V-REPORT-SCOPE](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-REPORT-SCOPE.json), [V-SITES-LIMIT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-SITES-LIMIT.json) |
| N-C0 | Plan节点移出不变量 / observed | 从未start/dispatch且pending才可移出；历史保护、并发、批量原子性、错误一致性。变更/限制：ce097局部PASS与636b最终REJECT是不同候选/裁决范围，不互相覆盖 | [P-N0](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-N0.json), [N-LOCAL-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/N-LOCAL-PASS.json), [N-DECISION](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/N-DECISION.json), [N-REJECT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/N-REJECT.json) |
| N-C1 | 下一轮节点移出修复 / announced_not_observed | fresh-main新immutable SHA；独立provenance；扩大history/status/remove-start/remove-edge/batch rollback race×10。变更/限制：明确未来门禁增强，但本轮没有新候选获此合同PASS的证据 | [N-REJECT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/N-REJECT.json) |
| M-C0 | Mermaid功能安全交互 / observed | Markdown Mermaid、modal缩放拖动/导出复制、独立安全交互验收、main集成。变更/限制：deccd→6855是候选版本变化；未发现足以新建合同版本的需求变化；7ca分支漂移不继承PASS | [P-M0](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-M0.json), [M-REJECT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/M-REJECT.json), [M-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/M-PASS.json), [M-INTEGRATION](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/M-INTEGRATION.json) |

## 5. 验收与转交时间线

时间均为源记录UTC，事件仅记录可观察部分；没有把快照当前描述当成历史每次合同全文。空候选表示该引用没有直接给出可绑定的SHA。

| 事件 | UTC | 类型 / 合同 | 候选 | 解释与证据 |
|---|---|---|---|---|
| G01/I-E1 | 2026-08-31T10:15:25.444288Z | rebuild_and_scope_revision / I-C1 | 未给出 | 旧目标承接且范围边界调整；[I-HANDOFF](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/I-HANDOFF.json) |
| G01/I-E2 | 2026-08-31T20:43:07.524565Z | product_pass_reported / I-C1 | f4c35357ea4a93286ea1b7926df91fd7a0e77859 | 产品PASS不单独等于最终main验收；[I-G9-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/I-G9-PASS.json) |
| G01/I-E3 | 2026-08-31T21:08:48.039156Z | final_pass_reported / I-C1 | c558505c75b3940cc942ff8eb4cd761429f30f92 | 可读终验报告，生产release仍独立；[I-FINAL](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/I-FINAL.json), [I-ARTIFACT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/insight-final-report.md) |
| G02/A-E0 | 2026-08-21T15:12:26.387602Z | prior_plan_pass_reported / A-C0 | 9a512ef96f323bd1448d719381ef9fdbd0deceb6 | 不把后继a77的撤销自动倒灌此候选；[A0-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A0-PASS.json) |
| G02/A-E1 | 2026-08-23T11:01:01.702542Z | pass_reported / A-C1 | 7700038b0f27c4d927b806149c7fb87c32c29f0c | 随后针对同候选产品验收撤回；[A-OLD-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-OLD-PASS.json) |
| G02/A-E2 | 2026-08-23T13:55:15.249504Z | user_challenge / A-C1 | 7700038b0f27c4d927b806149c7fb87c32c29f0c | 用户异议是后续撤回的重要来源；[A-USER](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-USER.json) |
| G02/A-E3 | 2026-08-23T13:56:08.015795Z | reopened / A-C1 | 7700038b0f27c4d927b806149c7fb87c32c29f0c | 审计记录重开paused；当前done不能覆盖历史；[A-REOPEN-AUDIT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-REOPEN-AUDIT.json) |
| G02/A-E4 | 2026-08-23T13:56:16.46955Z | acceptance_revocation / A-C1 | 7700038b0f27c4d927b806149c7fb87c32c29f0c | 旧PASS失效；本地B1已经知道；[A-REVOKE](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-REVOKE.json) |
| G02/A-E5 | 2026-08-23T15:01:10.193486Z | scope_revision / A-C2 | 7232c04df2bc902353107404d18b4bc3f5bd5712 | 新IA权威文档，不能视为原合同一字不变；[A-ADR](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-ADR.json), [A-CONTRACT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/access-adr0059.md) |
| G02/A-E6 | 2026-08-23T15:06:29.038164Z | rebuild / A-C2 | 未给出 | 六任务与全部PD组织变化；[A-HANDOFF](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-HANDOFF.json) |
| G02/A-E7 | 2026-08-23T15:28:07.572206Z | canonical_reject / A-C2 | 未给出 | 矩阵要求未变，canonical候选不满足；该REJECT消息未直接给出SHA，不补造；[A-S1-REJECT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-S1-REJECT.json) |
| G02/A-E8 | 2026-08-23T15:34:52.426323Z | canonical_pass / A-C2 | 06183d8cdf4d3374fe24e218b6a7bd803bf6093c | 仅canonical修正版PASS，不是产品最终PASS；[A-S1-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-S1-PASS.json), [A-MATRIX](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/access-state-matrix.md) |
| G02/A-E9 | 2026-08-23T17:32:39.702734Z | final_pass_reported / A-C2 | 1b5804fdee477c4901276bff2ff8c62a95c72e19 | 新合同新SHA报告PASS；不恢复旧7700的PASS；[A-FINAL](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-FINAL.json), [A-ARTIFACT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/access-s6-report.md) |
| G03/O-E1 | 2026-09-06T09:45:52.326155Z | non_delivery / O-C1 | 未给出 | 替代Task内部第一次仍无可交付变更；[O-NONDELIVERY](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-NONDELIVERY.json) |
| G03/O-E2 | 2026-09-06T12:38:04.25653Z | pushed_delivery_reported / O-C1 | cda9136c4359cd1d1e01831692bf029c396b795c | 可读结构化delivery与会话一致；[O-DELIVERY](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-DELIVERY.json), [O-NEW](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-NEW.json) |
| G03/O-E3 | 2026-09-07T03:47:21.954009Z | replacement_and_merge_reported / O-C1 | ae578a8d6b4e5f96ef518a2cf1c723bf8afdd5b4 | 父Plan本地已知道；无B1→B2结果翻转；[O-LINK](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-LINK.json) |
| G04/V-E1 | 2026-06-16T00:52:13.213093Z | feature_acceptance_handoff / V-C1 | 80794195 | 两个明确Task，不是sites未知Plan；[V-FEATURES](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-FEATURES.json) |
| G04/V-E2 | 2026-06-16T01:00:59.941973Z | data_api_go_reported / V-C1 | 80794195 | T167/T170有限范围；[V-GO1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-GO1.json), [V-ARTIFACT1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/v2103-tester1.md) |
| G04/V-E3 | 2026-06-16T01:04:20.582989Z | sites_handoff_unresolved / V-C2 | 未给出 | 端点缺失，保持未知；[V-TRANSFER](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-TRANSFER.json) |
| G04/V-E4 | 2026-06-16T01:17:16.051399Z | sites_direct_pass_claim / V-C2 | 未给出 | 不能恢复成逐模块验收通过；[V-SITES-LIMIT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-SITES-LIMIT.json) |
| G04/V-E5 | 2026-06-16T01:30:03.875719Z | browser_go_reported / V-C1 | 80794195 | 浏览器+服务契约，agent全链没有独立证明；[V-GO2](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-GO2.json), [V-ARTIFACT2](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/v2103-tester2.md) |
| G04/V-E6 | 2026-06-16T02:08:51.252117Z | release_and_main_reported / V-C1 | 402a5cea | 发布主张与持久报告；Pages部署成功仍未知；[V-RELEASE](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-RELEASE.json), [V-MAIN](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-MAIN.json) |
| G05/N-E1 | 2026-08-28T02:50:15.439374Z | local_review_pass / N-C0 | ce0974237250a2647825f50b3f73894dbf1a8994 | 不能覆盖terminal decision；[N-LOCAL-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/N-LOCAL-PASS.json) |
| G05/N-E2 | 2026-08-28T04:35:59.712032Z | terminal_reject / N-C0 | 636b3fc1 | 600.522s硬超时叙述；no Ship；[N-DECISION](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/N-DECISION.json), [N-REJECT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/N-REJECT.json) |
| G06/M-E1 | 2026-08-17T08:53:35.988875Z | candidate_reject / M-C0 | deccd130247fd3a8036ec107d90b02c42553d762 | 浏览器异步状态失败；[M-REJECT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/M-REJECT.json) |
| G06/M-E2 | 2026-08-17T09:20:11.626314Z | repaired_candidate_pass / M-C0 | 6855b0bdc4c150d8c7442a76ce352b86da8043a2 | 同需求修复后验收；[M-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/M-PASS.json) |
| G06/M-E3 | 2026-08-17T09:22:56.823425Z | integration_reported / M-C0 | b0c40b7c7e9971c75933d709b9bab2e161576e85 | 祖先关系可读Git验证；漂移分支7ca不继承PASS；[M-INTEGRATION](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/M-INTEGRATION.json), [M-ANCESTRY](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/mermaid-ancestry.txt) |

## 6. 全部引用证据索引

每项链接直达本地源记录副本或已读取文件；完整记录SHA256、来源包路径和适用限制见[证据JSON](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/derived/evidence.json)。E2只证明文件内容/对象或Git关系，不证明历史测试实际运行。

| 证据 | 原始记录ID / 文件 | 记录时刻UTC | 支持内容 |
|---|---|---|---|
| [P-I0](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-I0.json) | `plan:plan-d7ec8ee4` | 2026-08-30T05:38:30.436248Z | 快照Plan字段与当前描述；不得把当前描述回填为创建时原文 |
| [P-I1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-I1.json) | `plan:plan-929d9aaf` | 2026-08-31T10:12:18.832277Z | 快照Plan字段与当前描述；不得把当前描述回填为创建时原文 |
| [P-A0](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-A0.json) | `plan:plan-569ab651` | 2026-08-21T09:06:51.169696Z | 快照Plan字段与当前描述；不得把当前描述回填为创建时原文 |
| [P-A1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-A1.json) | `plan:plan-a77ec987` | 2026-08-22T06:42:07.07916Z | 快照Plan字段与当前描述；不得把当前描述回填为创建时原文 |
| [P-A2](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-A2.json) | `plan:plan-e91d02b8` | 2026-08-23T15:04:52.435937Z | 快照Plan字段与当前描述；不得把当前描述回填为创建时原文 |
| [P-O0](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-O0.json) | `plan:plan-d1680545` | 2026-09-06T09:17:22.962176Z | 快照Plan字段与当前描述；不得把当前描述回填为创建时原文 |
| [P-V0](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-V0.json) | `plan:plan-7fa6888f` | 2026-06-16T00:10:19.63057Z | 快照Plan字段与当前描述；不得把当前描述回填为创建时原文 |
| [P-V1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-V1.json) | `plan:plan-f7f98cfe` | 2026-06-16T01:08:46.502745Z | 快照Plan字段与当前描述；不得把当前描述回填为创建时原文 |
| [P-N0](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-N0.json) | `plan:plan-becb5960` | 2026-08-28T01:07:53.006499Z | 快照Plan字段与当前描述；不得把当前描述回填为创建时原文 |
| [P-M0](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/P-M0.json) | `plan:plan-67822059` | 2026-08-17T08:26:20.159892Z | 快照Plan字段与当前描述；不得把当前描述回填为创建时原文 |
| [I-REQUEST](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/I-REQUEST.json) | `messages:01M1BMK2DPRKB9NSFR5VNS7XTM` | 2026-08-31T10:07:12.310412Z | 用户要求重建承接原交付 |
| [I-HANDOFF](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/I-HANDOFF.json) | `messages:01M1BN2404P5H55K9TCTB0H0WH` | 2026-08-31T10:15:25.444288Z | 明确旧新Plan承接；A–F与生产release边界 |
| [I-FINAL](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/I-FINAL.json) | `messages:01M1CTEFS7VRPWEGSPJSC307PK` | 2026-08-31T21:08:48.039156Z | 新Plan main c558505c 的终验PASS叙述 |
| [I-ATTACH](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/I-ATTACH.json) | `messages:01M1CTGRRZ7YMF8S8AJC7KFSXF` | 2026-08-31T21:10:02.78337Z | 终验持久附件 |
| [A-OLD-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-OLD-PASS.json) | `messages:01M0Q4FW46TMHQBVTDRACYXR3X` | 2026-08-23T11:01:01.702542Z | 7700038b旧PASS；部分验收继承父任务证据 |
| [A-USER](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-USER.json) | `messages:01M0QEEWNHYVBK3N0V8ZQKM0ET` | 2026-08-23T13:55:15.249504Z | 用户明确指出与mock不一致 |
| [A-REVOKE](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-REVOKE.json) | `messages:01M0QEGRENQ54HM6FJVG202V46` | 2026-08-23T13:56:16.46955Z | 旧PASS撤销，声明done重开paused |
| [A-DIFF](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-DIFF.json) | `messages:01M0QF09K7PH9KYE134BTYQMS2` | 2026-08-23T14:04:45.543259Z | 针对7700038b的差异审计；仅记录审计者主张，不独立认定像素差 |
| [A-ADR](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-ADR.json) | `messages:01M0QJ7JXH0K5ZSS9028QCVQV9` | 2026-08-23T15:01:10.193486Z | 7232c04d文档确立两入口与Team Role属性 |
| [A-REQUEST](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-REQUEST.json) | `messages:01M0QJCQRZBEENJ2HXJ0NCQ0HW` | 2026-08-23T15:03:59.007649Z | 用户要求新Plan且全部PD执行 |
| [A-HANDOFF](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-HANDOFF.json) | `messages:01M0QJHA9EC00XTX29FP2ZKABS` | 2026-08-23T15:06:29.038164Z | a77→e91明确承接六任务 |
| [A-S1-REJECT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-S1-REJECT.json) | `messages:01M0QKRYCMG9X9JWGJ1TWCYRVV` | 2026-08-23T15:28:07.572206Z | canonical首版Risk与typed-delete不满足同一矩阵 |
| [A-S1-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-S1-PASS.json) | `messages:01M0QM59RA181QMKVGFFYGHDR9` | 2026-08-23T15:34:52.426323Z | canonical修正版06183d8c获PASS；不是新契约自动产生 |
| [A-FINAL](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-FINAL.json) | `messages:01M0QTWZCPEF3FAXTVYGGZTW2X` | 2026-08-23T17:32:39.702734Z | 新要求下1b5804fd终验PASS与main集成叙述 |
| [A-PREDECESSOR](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-PREDECESSOR.json) | `messages:01M0QDAW33GC9BZ8EPH0PFZB7W` | 2026-08-23T13:35:35.011666Z | 569旧候选仍报告完成；不能把a77撤回直接套到此SHA |
| [O-LINK](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-LINK.json) | `messages:01M1WZMK71XWDDDK56XQPSM1N7` | 2026-09-07T03:47:21.954009Z | 父Plan会话本身已报告替代Task、交付SHA和main ae578a8d |
| [O-CLOSE](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-CLOSE.json) | `messages:01M1X2SF9MWE1773M3Q1V655HQ` | 2026-09-07T04:42:27.508767Z | 旧Task作为重复任务清理 |
| [O-NONDELIVERY](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-NONDELIVERY.json) | `messages:01M1V1RAE6M6HENBS5FF9MMA0T` | 2026-09-06T09:45:52.326155Z | 替代Task首次尝试没有可交付变更 |
| [O-DELIVERY](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-DELIVERY.json) | `messages:01M1VBKM70YXXDG979MX47E14H` | 2026-09-06T12:38:04.25653Z | 替代Task后来有pushed交付 |
| [O-REPORT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-REPORT.json) | `messages:01M1VBN6M7W4X9P9H863SQ5YQG` | 2026-09-06T12:38:55.879072Z | Operation实现和make check通过的supervisor叙述 |
| [V-TRANSFER](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-TRANSFER.json) | `messages:01KV6ZAE362CTNBEVWS740AXV2` | 2026-06-16T01:04:20.582989Z | 缺失端点2d41仅声称sites v2.10.2验收转交 |
| [V-FEATURES](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-FEATURES.json) | `messages:01KV6YM7RXMV9MPNG34N31617M` | 2026-06-16T00:52:13.213093Z | 两个v2.10.3功能移交具体验收Task；sites不在候选 |
| [V-GO1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-GO1.json) | `messages:01KV6Z4A55BVBK2GE9WYERV3P0` | 2026-06-16T01:00:59.941973Z | T171 data/API验收GO，候选80794195 |
| [V-GO2](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-GO2.json) | `messages:01KV70SH739JENYJVWW8KAWX38` | 2026-06-16T01:30:03.875719Z | T172浏览器与服务契约验收GO；非全部真实agent工具链 |
| [V-SITES-LIMIT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-SITES-LIMIT.json) | `messages:01KV7023CKTQ8X095HV08KGCFN` | 2026-06-16T01:17:16.051399Z | sites两Task按指示直接过的叙述，不能视为模块完整独立核验 |
| [V-REPORT-SCOPE](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-REPORT-SCOPE.json) | `messages:01KV6ZSSFK5HXQVKCPCQF31SAY` | 2026-06-16T01:12:43.763824Z | 综合验收将sites并轨的计划性主张 |
| [V-RELEASE](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-RELEASE.json) | `messages:01KV730J1MXKES7PXRRD3WJXNJ` | 2026-06-16T02:08:51.252117Z | 发布、tag、main402a5cea叙述；Pages只是会自动部署 |
| [V-MAIN](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-MAIN.json) | `messages:01KV736EDS5M1P398Y28DJXKPF` | 2026-06-16T02:12:04.153627Z | main与tag402a5cea的参与者核验 |
| [N-REJECT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/N-REJECT.json) | `messages:01M13APF96HNX4MBV002CYQF3B` | 2026-08-28T04:40:22.566588Z | terminal REJECT、done语义与下一轮条件 |
| [M-INTEGRATION](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/M-INTEGRATION.json) | `messages:01M07GFZ5QEPQQMR4CKEMHF4MB` | 2026-08-17T09:22:56.823425Z | reviewed6855进入main；拒绝继承漂移分支7ca的验收 |
| [A-REOPEN-AUDIT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A-REOPEN-AUDIT.json) | `audit:01M0QEGG6FZPM9MFRYX0ZFAE2P` | 2026-08-23T13:56:08.015795Z | 数据库审计确认2026-08-23 13:56:08 reopened→paused事件 |
| [O-OLD](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-OLD.json) | `tasks:task-b0d4ecaa` | 2026-09-06T09:00:07.485416Z | 旧Task所属六领域Plan、same Issue、discarded |
| [O-NEW](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/O-NEW.json) | `tasks:task-d26fcf10` | 2026-09-06T09:40:06.72113Z | 替代Task completed、无plan_id，delivery字段保留branch/head/pushed |
| [V-T171](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-T171.json) | `tasks:task-756df900` | 2026-06-16T00:51:37.137269Z | 具体功能验收Task属于f7f98cfe |
| [V-T172](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/V-T172.json) | `tasks:task-0d3f5e41` | 2026-06-16T00:51:48.260352Z | 另一具体功能验收Task属于f7f98cfe |
| [M-REJECT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/M-REJECT.json) | `pm_plan_review_verdicts:plan-67822059:task-4f8e994a` | 2026-08-17T08:53:35.988875Z | deccd候选浏览器异步渲染REJECT |
| [M-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/M-PASS.json) | `pm_plan_review_verdicts:plan-67822059:task-df1c36dc` | 2026-08-17T09:20:11.626314Z | 6855修复候选PASS |
| [N-LOCAL-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/N-LOCAL-PASS.json) | `pm_plan_review_verdicts:plan-becb5960:task-45b0c8a9` | 2026-08-28T02:50:15.439374Z | ce097局部review PASS不覆盖后续terminalREJECT |
| [I-G9-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/I-G9-PASS.json) | `pm_plan_review_verdicts:plan-929d9aaf:task-0d271e84` | 2026-08-31T20:43:07.524565Z | G9产品PASS绑定f4c35357 |
| [A0-REJECT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A0-REJECT.json) | `pm_plan_review_verdicts:plan-569ab651:task-4e6baccf` | 2026-08-21T11:12:46.989971Z | 569的74bb4e69早期产品REJECT |
| [A0-PASS](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/A0-PASS.json) | `pm_plan_review_verdicts:plan-569ab651:task-6ebad88b` | 2026-08-21T15:12:26.387602Z | 569的9a512ef9局部终验PASS |
| [N-DECISION](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/records/N-DECISION.json) | `pm_plan_decision_outcomes:plan-becb5960:task-9c3fad9a` | 2026-08-28T04:35:59.712032Z | 结构化G2 decision=reject |
| [I-ARTIFACT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/insight-final-report.md) | `evidence/insight-final-report.md` | 未给出 | 3989-byte原始报告绑定c558505c、G9 ancestor与A–F；文件内容核对 |
| [A-CONTRACT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/access-adr0059.md) | `evidence/access-adr0059.md` | 未给出 | 历史7232c04d ADR明确产品合同发生改变 |
| [A-MATRIX](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/access-state-matrix.md) | `evidence/access-state-matrix.md` | 未给出 | 06183d8c冻结全状态矩阵；逐行要求可定位 |
| [A-ARTIFACT](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/access-s6-report.md) | `evidence/access-s6-report.md` | 未给出 | aadffe3a提交中的报告绑定1b5804fd并列出检查；不独立证明测试执行 |
| [V-ARTIFACT1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/v2103-tester1.md) | `evidence/v2103-tester1.md` | 未给出 | 发布402a5cea树中可读的T167/T170 §2 GO报告 |
| [V-ARTIFACT2](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/v2103-tester2.md) | `evidence/v2103-tester2.md` | 未给出 | 发布树中可读的§3/4 GO报告，明确agent端采用服务层集成测试 |
| [M-ANCESTRY](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/mermaid-ancestry.txt) | `evidence/mermaid-ancestry.txt` | 未给出 | git merge-base --is-ancestor 6855 b0c40退出0；仅证明Git祖先关系 |

## 7. 机器可读产物

- [对比JSON](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/derived/comparisons.json) / [CSV](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/derived/comparisons.csv)
- [阅读覆盖与未审范围](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/reading/coverage.json)
- [过程决策](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/logs/decisions.md) / [验证结果](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/logs/validation.json)
- [保存与复现入口](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/README.md)
