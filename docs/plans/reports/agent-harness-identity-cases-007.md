# 名字、角色与共享知识：案例证据和排除记录（007）

2026-11-02。单AI探索，尚未人工复核。依据用户后续范围，明显系统bug移出方案负效应主证据。原始记录留在私有研究目录；消息为参与者说法，源码能力不等于部署效果。

[专项研究](agent-harness-identity-007.md) · [数据与过程](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/README.md)

## N01 可读名字：界面与寻址偏好

筛选：**INCLUDE_EXPERIENCE**。

| 证据 | 时间 / 类型 | 观察及边界 |
|---|---|---|
| [N01-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E01.json) | 2026-06-15T11:52:09.97455Z / user_preference | 用户要求用 Agent 名字，并允许点击进入详情；支持可读辨认/导航偏好，不是交付提升实验。（`messages:01KV5HZXFPZ13DJSRMZ8N1X058`） |
| [N01-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E02.json) | 2026-06-29T16:18:15.131091Z / user_preference | 用户明确页面优先显示名字而非 ID；界面偏好与机器主键分开。（`messages:01KWA2S6EVQP2DNK6ASAF7B04N`） |
| [N01-E03](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E03.json) | 2026-08-21T09:57:11.599262Z / user_preference | 用户要求 at Agent 用名字；这是可读交互偏好，不证明路由必须依赖名字字符串。（`messages:01M0HW1HSFGZY859N8NP4VR76V`） |
| [N01-E04](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E04.json) | 2026-07-03T03:52:26.000839Z / user_contract | 用户提出消息使用ID、显示时转名字，直接说明两层可以分离。（`messages:01KWK1PE4GTCX08NJ3X4H9MNXG`） |
| [N01-E05](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E05.json) | 2026-07-03T03:55:18.313821Z / user_contract | 用户要求 @displayname 与 @id 功能等价且都显示名字；不将相关解析bug作为负效应。（`messages:01KWK1VPD9NQK9TKQ5AMVCAJWQ`） |
| [N01-E06](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E06.json) | 2026-07-03T06:34:58.886126Z / implementation_report | Agent 报告名字可点击、原ID保留在hover；仅佐证界面实现报告，未独立重跑UI或量化省时。（`messages:01KWKB02E6VQJPP36CFV09QKG2`） |

## N02 身份、角色与权限可以独立变化

筛选：**MECHANISM_ONLY**。

| 证据 | 时间 / 类型 | 观察及边界 |
|---|---|---|
| [N02-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N02-E01.json) | 2026-08-13T03:09:29.849368Z / normal_authorization_boundary | PD 报告可读/提案但不能promote/reject；不当作角色方案失效，只说明称呼PD不自动等于Curator授权。（`messages:01KZWHH9BSKYJGP6Q2AYRJRJ05`） |
| [N02-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N02-E02.json) | 2026-08-13T04:10:29.090198Z / stored_authorization | 同一PD identity的Curator记录created_at为2026-08-13 04:10:29 UTC，位于拒绝报告与成功报告之间；仅表记录，不是完整授权事件史。（`team_memory_policy_curators:0`） |
| [N02-E03](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N02-E03.json) | 2026-08-13T06:42:29.58964Z / authorization_and_memory_report | 随后同一PD报告处理完提案，明确新session/fork与在途执行器采用不同快照；没有提出靠改名解决权限。（`messages:01KZWXQ9JN8KHZBXH4ENCXG5QQ`） |

## N03 专业角色与共享事实的正例

筛选：**INCLUDE_POSITIVE**。

| 证据 | 时间 / 类型 | 观察及边界 |
|---|---|---|
| [N03-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E01.json) | 2026-06-21T16:27:27.567091Z / new_information | Dev角色共享真实表名、SQLite、NULL索引等6项具体差异；价值来自新增事实，不是名字字符串。（`findings:01KVNG49YFFXCDTNXSD3GTZX5X`） |
| [N03-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E02.json) | 2026-06-21T17:09:07.734193Z / use_of_information | 后续finding报告NULL-aware索引修正，说明有具体消费者；没有匿名对照。（`findings:01KVNJGKGP9WP8CHJRNAXZT3BH`） |
| [N03-E03](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E03.json) | 2026-06-22T23:48:50.40319Z / shared_context | 系统注入19条finding；记录传递存在，不证明全部被阅读或遵守。（`messages:01KVRVS73387DEFGXVM0GW2HNR`） |
| [N03-E04](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E04.json) | 2026-08-17T08:53:35.958879Z / independent_review | Mermaid Tester发现测试全绿时的浏览器渲染缺陷；把REJECT后ready的系统bug从该正例切除。（`messages:01M07ET7JPD8CN5DW22MPGJAQA`） |
| [N03-E05](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E05.json) | 2026-08-17T09:13:15.332019Z / review_report | 修复后独立浏览器复验报告通过；本轮不重跑浏览器，不能量化角色或名字的净收益。（`messages:01M07FY7A42V2NZ4480MZS96N5`） |
| [N03-E06](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E06.json) | 2026-06-16T01:53:27.945879Z / independent_fact_check | integration核远端发现trunk已具备成果并拒绝错误重建；价值是独立事实检查，早期跨Agent角色不等于内部S/E。（`messages:01KV724CC96MF8A8SKPZACE06C`） |

## N04 共享记忆需要筛选与治理

筛选：**INCLUDE_DESIGN_PROCESS**。

| 证据 | 时间 / 类型 | 观察及边界 |
|---|---|---|
| [N04-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E01.json) | 2026-08-13T02:06:52.924993Z / user_observation | 用户询问如何处理已有很多proposals；未把数量当浪费。（`messages:01KZWDYMFW1JGAPN2PY4MBT2M5`） |
| [N04-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E02.json) | 2026-08-13T02:07:54.081231Z / governance_work | PD报告20条pending重叠，提议canonical归并与保留替代关系；这是共享知识治理步骤，不是命名造成的故障。（`messages:01KZWE0G71JP57W9EX9RXZS0ER`） |
| [N04-E03](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E03.json) | 2026-08-13T06:42:29.58964Z / governance_result_report | PD报告8 promoted/12 rejected、pending0并保留审计；结果为参与者报告，未独立复核Git memory库正文。（`messages:01KZWXQ9JN8KHZBXH4ENCXG5QQ`） |
| [N04-E04](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E04.json) | 2026-08-13T13:01:34.326923Z / user_question | 用户追问沉淀的规则如何传达给团队成员；组织记忆的存在不等于已进每次context。（`messages:01KZXKDD7P45YDMRKJPAK4BQ0A`） |
| [N04-E05](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E05.json) | 2026-08-13T13:02:06.771371Z / mechanism_report | PD区分Team Rules加载、memory按scope召回、在途不刷新和流程强制；其中规则数量与前报告不一致，不用于精确库存判断。（`messages:01KZXKECXKM7EP8X6RY3BJBNPP`） |
| [N04-E06](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E06.json) | 2026-08-13T13:04:02.497709Z / user_concern | 用户提出context膨胀担忧，不能把担忧记成已观察到模型性能下降。（`messages:01KZXKHXY1KNH3NH8WWZP8ZBXN`） |
| [N04-E07](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E07.json) | 2026-08-13T13:04:21.883853Z / design_tradeoff | PD提出按阶段、短规则、正文外置与预算；预测膨胀而非测得token消耗。（`messages:01KZXKJGVVSK1H7Q31Y6C9PECJ`） |

## N05 共享存储、索引、正文与本次context不同

筛选：**INCLUDE_DESIGN_PROCESS**。

| 证据 | 时间 / 类型 | 观察及边界 |
|---|---|---|
| [N05-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E01.json) | 2026-08-13T13:20:12.25234Z / user_design_choice | 用户反对给每个节点硬编码work_type；正常设计取舍，不视为产品bug。（`messages:01KZXMFGYW5SGZXHAB9YJ1RA3D`） |
| [N05-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E02.json) | 2026-08-13T13:20:41.624062Z / design_revision | PD改为短索引、按需正文、保留已读记录；语义选择仍依赖执行者，权限仍由代码强制。（`messages:01KZXMGDMRT4NM4F6AC5FXEEC1`） |
| [N05-E03](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E03.json) | 2026-08-13T15:02:32.058249Z / mechanism_requirement | 仅更新memory不等于稳定按需加载；需要读取API和加载审计。属于表示/加载策略区别。（`messages:01KZXTAWVT4FWEJHZ5W47C38KY`） |
| [N05-E04](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E04.json) | 2026-08-13T15:23:53.851345Z / user_scope | 用户授权落地防context膨胀与审计的方案；不是名字实验。（`messages:01KZXVJ0KV8HEJJVYNQC3T4VYJ`） |
| [N05-E05](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E05.json) | 2026-08-13T16:29:16.715797Z / implementation_report | 报告runtime只带索引、按slug+commit读正文并保留恢复快照。（`messages:01KZXZ9QHBQAY87R7G2347KQCP`） |
| [N05-E06](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E06.json) | 2026-08-13T16:54:13.056351Z / counter_information | integration核实两候选为siblings，纠正上游声称串链；共享叙述须核验，不宣称共享自动保证事实正确。（`messages:01KZY0QCT0WE2FEYYYS61399G7`） |
| [N05-E07](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E07.json) | 2026-08-13T17:10:12.297009Z / bounded_acceptance_report | Tester报告100×32KiB正文的合成验收中execute索引25条且小于16KiB；这是当时验收报告，不是本轮生产token节省或LLM性能实验。（`messages:01KZY1MNJ9H80N9J1FA1Y0HGE0`） |
| [N05-E08](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E08.json) | 2026-08-13T17:13:25.403893Z / delivery_report | 报告Rule Registry与证据进入main c3897e71；不推断部署或全员遵守。（`messages:01KZY1TJ4VRKMGJ235D5WZ7F0R`） |

## N06 开发者直接体验

筛选：**INCLUDE_EXPERIENCE**。

| 证据 | 时间 / 类型 | 观察及边界 |
|---|---|---|
| [N06-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N06-E01.json) | 2026-11-02 / retrospective_self_report | 开发者本人自述团队leader体验、掌控感与“上瘾”。无具体时间案例；不能拆出名字单变量因果，也不作临床判断。（`U007-01`） |

## X01 名字/ID混拼与解析诊断

筛选：**EXCLUDE_BUG_OR_UNCERTAIN**。

| 证据 | 时间 / 类型 | 观察及边界 |
|---|---|---|
| [X01-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/X01-E01.json) | 2026-07-03T03:04:00.680275Z / stored_malformed_literal | 原消息实际包含@agent-center-35ac0e16，确认混拼串存在；不能由此证明漏唤醒。（`messages:01KWJYXRX8E0K19NK9CB2EG7XK`） |
| [X01-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/X01-E02.json) | 2026-07-03T03:34:20.604326Z / participant_diagnosis | 参与者声称回执/渲染/唤醒口径不一致；主分析按bug/未定原因排除，源码反证另存。（`messages:01KWK0NA5W3V7K6YQQW701Q35R`） |
| [X01-E03](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/X01-E03.json) | 2026-07-03T03:43:34.98461Z / uncertainty_admitted | 参与者明确无法确认是否同时传mention_refs，也不能证明模型当时只掌握ID。（`messages:01KWK167J8RWC3MZTJ64Y5C99J`） |
| [X01-E04](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/X01-E04.json) | 2026-07-03T03:45:33.843841Z / prompt_diagnosis | 关于主工具说明和可选参数引导的解释；未取得模型完整输入，不据此归因名字有害。（`messages:01KWK19VMKVPM83SX9Y6ATCH66`） |
| [X01-E05](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/X01-E05.json) | 2026-07-03T03:57:09.527962Z / user_resolution | 用户选精确匹配；需求选择不能当作最终修复部署已完成。（`messages:01KWK1Z30QBGX558WB44E95T6M`） |

## X02 两套稳定ID的键错配

筛选：**EXCLUDE_BUG**。

| 证据 | 时间 / 类型 | 观察及边界 |
|---|---|---|
| [X02-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/X02-E01.json) | 2026-07-02T10:25:18.489786Z / participant_diagnosis | Agent报告AR ULID写入与IdentityMemberID读取错配；问题是引用一致性，非可读名字或人格作用。（`messages:01KWH5S34SHZ889DBAGYP4KC9P`） |
| [X02-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/X02-E02.json) | 2026-07-02T10:28:31.166248Z / user_relay_not_independent | 用户转贴该诊断要求拆任务；不是第二份独立根因确认。（`messages:01KWH5YZ9Y4P50XNBJATXKFZZ0`） |

## X03 角色描述曾未接入prompt

筛选：**EXCLUDE_IMPLEMENTATION_GAP**。

| 证据 | 时间 / 类型 | 观察及边界 |
|---|---|---|
| [X03-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/X03-E01.json) | 2026-06-30T12:18:37.949529Z / user_question | 用户询问UDE描述是否进system prompt。（`messages:01KWC7F57XCFHZQZ3RZWXR3A39`） |
| [X03-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/X03-E02.json) | 2026-06-30T12:22:15.209118Z / participant_diagnosis | 参与者报告当时description只在UI、不进runtime；不能作为已启用角色提示无效的证据。（`messages:01KWC7NSD9T41DERWASFFXTHEP`） |
| [X03-E03](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/X03-E03.json) | 2026-07-01T04:12:02.585463Z / implementation_report | 后续报告description开关注入实现通过review；说明模型输入须核验，未估计角色提示提升。（`messages:01KWDY0X4S925T2GA1DE5Y5HNP`） |
| [X03-E04](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/X03-E04.json) | 2026-07-02T04:01:52.261724Z / acceptance_limit | 合并后仍待真跑抽检；先前分支PASS不可当所有后继版本验收，当前仅机制背景。（`messages:01KWGFV045RTJY53S937TP0NB3`） |

## 006轮追溯筛选

旧报告保留，下表约束当前复盘的设计价值推断；没有把排除片段从历史数据删除。

| 案例 | 片段 | 当前地位 | 理由 |
|---|---|---|---|
| C01 | inline/fork冲突、空executor误判 | EXCLUDE_BUG | 源码合同冲突从方案负效应剔除；测试职责纠正未证明名字作用。 |
| C02 | Ship错派/无Git能力 | EXCLUDE_CONFIG | 保守剔除不相容配置；仅可说明职责和能力不是同一变量。 |
| C03 | 旧condition/终态收口及循环前提 | EXCLUDE_BUG_OR_UNCERTAIN | 终态不一致为实现问题；规则组合原因未隔离，不靠重新命名为设计问题纳入。 |
| C04 | lease/inflight恢复受阻、branch交付误报 | UNCERTAIN | 运行版本与根因未清，可能bug/config；不用于架构负效应。 |
| C05 | Access把局部检查当整目标PASS | RETAIN_SEMANTIC_HYPOTHESIS | 尚非明显确定性系统bug；保留用户/PD的验收偏差叙述，缺独立视觉复核，不归因名字。 |
| C06 | REJECT后ready | EXCLUDE_BUG | 调度错误剔除；独立Tester发现真实缺陷与复验报告另保留INCLUDE_POSITIVE。 |
| C07 | 角色/顺序混乱 | UNCERTAIN_FOR_NAME_EFFECT | 不由角色误解推断名字问题；integration基于远端事实纠错另保留INCLUDE_POSITIVE。 |
| C08 | depends_on不作为调度前提 | EXCLUDE_BUG | 调度语义错位剔除；共享schema finding另保留INCLUDE_POSITIVE。 |
| C09 | 设备/授权缺失 | EXCLUDE_EXTERNAL_FROM_FAILURE | 正常资源边界，不能证明多Agent方案失效；作为合理权限/等待边界。 |
