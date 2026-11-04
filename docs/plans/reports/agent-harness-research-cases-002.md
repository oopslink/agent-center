# 30个Plan逐案探索分析
由单一AI分析者完成，尚未人工复核。E2只表示某一关键主张有文件/源码支撑，不等于整案独立验证。

## 1. plan-2e483a10 — 修复唤醒坑：control-connected agent 指派 runnable task 自动唤醒 + --with-agent 自动 dispatch
结果类别：reported_integrated；证据级别：E2；机制：workflow_control, context_transfer
机制所指范围：{'workflow_control': 'task_subject', 'context_transfer': 'run_report'}
pool路径未发唤醒事件；共享finding记录依赖及修复；最终release提交可定位。
替代解释/限制：复杂分工也可交付；零generation是历史记录属性，不是无演化对照。
证据：01KWB0G8G3DP7YG4HSMVXRMZ53, 01KWB13ESXBHFJQ41FYJ3V5PFE, 01KWB1773S2NK5ZGGC981RS208

## 2. plan-f7f98cfe — v2.10.3 验收发版
结果类别：reported_integrated；证据级别：E1；机制：role_boundary, workflow_control
机制所指范围：{'role_boundary': 'run_report', 'workflow_control': 'run_report'}
分支层次规则缺失与Ship职责错位后被纠正，随后报告发布。
替代解释/限制：规则不清与agent错误共同出现，无法归因多层执行结构。
证据：01KV71VRQBZC147BD2RTTQ0KYE, 01KV736EDS5M1P398Y28DJXKPF

## 3. plan-c975baa9 — Plan Stage 模型实现（issue-d07007e5 · v2.42.0）
结果类别：integrated_deployment_pending；证据级别：E1；机制：workflow_control, external_dependency
机制所指范围：{'workflow_control': 'run_report', 'external_dependency': 'run_report'}
重复executor陈旧RED与已发布状态冲突；代码合入，生产重部署另待安排。
替代解释/限制：高负载与过期checkout是替代解释；未确认所有RED均为假阳性。
证据：01KXATP7W7RHVJ4H3WXWM5CX7N, 01KXAVP0QK3NZKWARMHK9CV5VX, 01KXAVQQRXNMSVBFYT08J49TBY

## 4. plan-7fa6888f — v2.10.3
结果类别：handoff_outcome_unresolved；证据级别：E1；机制：本轮未识别明确机制
机制所指范围：{}
三个任务完成，末尾消息将文档站验收移交另一Plan；本案最终验收需跨案核对。
替代解释/限制：会话包含多个版本/交付目标，不能用一条后续消息概括全部产品结果。
证据：01KV6ZAE362CTNBEVWS740AXV2

## 5. plan-60eaefe6 — [全真验收] Reminder entity 状态变更事件触发 (issue-68ccb310)
结果类别：reported_integrated；证据级别：E1；机制：本轮未识别明确机制
机制所指范围：{}
有验收报告，随后发现on_event映射/展示缺口并快修合入。
替代解释/限制：交付后的缺陷不自动说明编排失效；这是复杂流程下交付并修复的反例。
证据：01KWVABJ1JBT47FDRHTB7E1RRM, 01KWVBMWQRCTP2YPEV2CDGHDYJ

## 6. plan-e3ee9116 — v2.14.0 — cycle 控制流图
结果类别：reported_integrated；证据级别：E1；机制：workflow_control, context_transfer
机制所指范围：{'workflow_control': 'run_report', 'context_transfer': 'run_report'}
依赖边不参与控制流派发导致强耦合任务提前并行；PD手工暂停，共享finding传递约束；最终发布。
替代解释/限制：手动调度和代码修复均贡献结果，不能将最终发布视为全自动成功。
证据：01KVQV3Q9QYXF5T4F01V616XQ5, 01KVRVS73387DEFGXVM0GW2HNR, 01KVS4YXSJMDXPXFWKADJW2ZW5

## 7. plan-ac717ff4 — Agent@Agent 唤醒护栏 + Settings 可配(I7)
结果类别：reported_integrated；证据级别：E1；机制：workflow_control, quality_gate, external_dependency
机制所指范围：{'workflow_control': 'task_subject', 'quality_gate': 'run_report', 'external_dependency': 'run_report'}
真实验收发现SettingsStore漏接返501；部分验收曾按指示放行，后续修复并发布。
替代解释/限制：既有有效门禁也有人为放行；完整矩阵是否最终重跑需要原始证据。
证据：01KVHW1DXGEM1YG656AGE42MS0, 01KVHW1EE48BJ8E3R24R4JD921, 01KVJ3EGVPTVGNPFZRD1CTBHZY

## 8. plan-dc1f0b81 — Bug · 会话消息列表旧200窗口
结果类别：reported_integrated；证据级别：E1；机制：本轮未识别明确机制
机制所指范围：{}
单任务链与后续分页补做有合入报告。
替代解释/限制：简单任务正例不能证明简化架构的因果收益；任务难度不同。
证据：01KV7HJ7E36H5MGH5N1ZXT3F5E

## 9. plan-0abb326f — v2.13.0 — I18 落地：Cycle 节点图编排 + 合并校验护栏
结果类别：reported_integrated；证据级别：E1；机制：role_boundary, workflow_control, context_transfer
机制所指范围：{'role_boundary': 'run_report', 'workflow_control': 'run_report', 'context_transfer': 'run_report'}
Ship改派PD；过时的B2/B3未交付判断延迟已ready的Ship；后续恢复。
替代解释/限制：状态来源、git事实及agent记忆不同步；无法单独归因context隔离。
证据：01KVMN97X8Q0XYRYSZ91DCS7X5, 01KVMNGDWRXJ585YHSVNX6YQ8E, 01KVMTEZ8ZSHVJFP1253R12NC8

## 10. plan-93ed52e5 — v2.15.0 — cycle 控制流图
结果类别：reported_integrated；证据级别：E1；机制：本轮未识别明确机制
机制所指范围：{}
多feature集成、独立检查及发布记录完整，明确披露视觉验收方法限制。
替代解释/限制：作为复杂流程可工作的反例；测试报告不是本研究独立运行。
证据：01KVTPHHJJ6BVQ8AV33P1XCSD8, 01KVVBWZ7Q79RX084YV62CTC2X

## 11. plan-bdccf57b — v2.16.0 — cycle 控制流图
结果类别：integrated_deployment_pending；证据级别：E1；机制：role_boundary, workflow_control
机制所指范围：{'role_boundary': 'run_report', 'workflow_control': 'run_report'}
Ship模板挂给integration-dev与角色规则冲突；单一合并者排队；发布后runtime尚需重启。
替代解释/限制：可能是模板和授权不一致，不足以证明角色分工无效。
证据：01KW4TCFNSNWKXDVG4ASPQG855, 01KW4W7FARSH6ARPG5RDXHEJDA, 01KW4W85Q47GXEY5GKXZWK54M3

## 12. plan-80451f29 — [codex] Plan 2：codex 作为 supervisor（两阶段：--json 基础 + supervisor 接线，验收双跑 codex+claude）
结果类别：reported_integrated；证据级别：E1；机制：workflow_control, quality_gate, external_dependency
机制所指范围：{'workflow_control': 'mixed', 'quality_gate': 'run_report', 'external_dependency': 'run_report'}
per-agent配置/认证遗漏、空交付与探针误判被多层检查发现；发布后仍修测试skip漏洞。
替代解释/限制：此处多层核查提供收益；运行环境和配置传播是混杂因素。
证据：01KX8E65JM8NY5VXQZQFHVJK2C, 01KX8F9HEGZM1BKWMB3CXA4ZXA, 01KX8FACY62Q7HQZ96XKW1T7H6

## 13. plan-97ba1bcc — 多语言界面（中文/英文，Settings 切换） i18n
结果类别：reported_integrated；证据级别：E1；机制：本轮未识别明确机制
机制所指范围：{}
合并后独立像素核对和后续术语修复有明确交付叙述。
替代解释/限制：本次筛选没有足够证据赋予协调失败标签；无标签不等于无失败。
证据：01KWDJVX1FQH48MKCFSCKDA29F, 01KWDQ2CCP626WSSZBYQ8SABT4

## 14. plan-50c5c4fa — dispatch 双重派发修复（issue-d118b5dc）
结果类别：integrated_deployment_pending；证据级别：E1；机制：role_boundary, workflow_control, quality_gate
机制所指范围：{'role_boundary': 'task_subject', 'workflow_control': 'mixed', 'quality_gate': 'run_report'}
跨assignee错误fork及重复wake有修复和live验收记录；实际部署延后。
替代解释/限制：故障可以来自确定性路由/身份检查；不能由此推出名字造成模型混淆。
证据：01KXAQNTD4QWHN939NTJ0ZP4SS, 01KXASYBC2KXK0MVYQSVP9VFT3, 01KXAT7MH1YQZPWDS2TKKMNEQN

## 15. plan-67822059 — 聊天 Mermaid 图表与沉浸式查看器落地
结果类别：reported_integrated；证据级别：E2；机制：workflow_control, quality_gate, evolution_targeted
机制所指范围：{'workflow_control': 'run_report', 'quality_gate': 'run_report', 'evolution_targeted': 'run_report'}
并发渲染REJECT后新增修复复验；陈旧执行快照被纠正，最终集成。
替代解释/限制：代码修复与图调整同时发生，不能分离evolution效应。
证据：generation-384637ec, 01M07FCHBRH6N084T0N9159XV2, 01M07GGJCGTN7H0YBSHNYKK438

## 16. plan-929d9aaf — Insight IA 产品交付重建：短链实现、双验收与 main 收口
结果类别：reported_integrated；证据级别：E2；机制：role_boundary, workflow_control, quality_gate, evolution_targeted
机制所指范围：{'role_boundary': 'run_report', 'workflow_control': 'run_report', 'quality_gate': 'run_report', 'evolution_targeted': 'run_report'}
新Plan原称10节点短链，最终累计59Task/12generation；仍有fork权限、验收及证据恢复问题，最终持久验收报告PASS。
替代解释/限制：重建同时改变范围、规则与候选；最终是隔离实例验收，不等于生产部署和持续可靠。
证据：01M1CR099NCDECGP4JY89PXXQC, generation-fa2eacb6, generation-b8c6c9e1, 01M1CTGRRZ7YMF8S8AJC7KFSXF

## 17. plan-236dadc8 — Collaboration Insight 组织级大 Graph 整改
结果类别：reported_integrated；证据级别：E1；机制：workflow_control, quality_gate, evolution_targeted
机制所指范围：{'workflow_control': 'run_report', 'quality_gate': 'run_report', 'evolution_targeted': 'run_report'}
两次具体证据下钻缺陷触发修复；最终分支命名与自动交付判定冲突，经接管恢复。
替代解释/限制：manual recovery是工具流程名，不直接计为真人操作。
证据：generation-170ca966, generation-99b0e9c7, 01M1M25A7M2RFKGTZRGDXJ644R

## 18. plan-70ab192f — Spark Host 一键注册与生产落地
结果类别：partial_delivery；证据级别：E1；机制：workflow_control, external_dependency, quality_gate, evolution_targeted
机制所指范围：{'workflow_control': 'run_report', 'external_dependency': 'run_report', 'quality_gate': 'run_report', 'evolution_targeted': 'run_report'}
部署报告存在但sudo、设备与CA目标缺失，完整冒烟未完成；重复重探被暂停。
替代解释/限制：真实外部依赖不是LLM认知错误；不得以done认定成功。
证据：generation-66cfaf4c, generation-a2577996, 01M1KV5DX0GGZQS8F5E8WZ14Y2, 01M1KYTXHJ87V0MB22XN29B96S

## 19. plan-a77ec987 — Access 三页收口 + Assignment Pool / Backlog 统一推进
结果类别：acceptance_revoked；证据级别：E1；机制：workflow_control, evolution_targeted
机制所指范围：{'workflow_control': 'run_report', 'evolution_targeted': 'run_report'}
先宣布PASS，后因canonical视觉/IA偏离撤销结论，另建全部由PD承担的新Plan。
替代解释/限制：后续单Agent与原分工方案同时改变范围/验收，不构成受控结构对照。
证据：01M0Q4FW46TMHQBVTDRACYXR3X, 01M0QEGRENQ54HM6FJVG202V46, 01M0QJHA9EC00XTX29FP2ZKABS

## 20. plan-becb5960 — Plan 节点移出硬不变量：仅从未执行的 pending task 可移除
结果类别：rejected_closure；证据级别：E1；机制：quality_gate, workflow_control, evolution_targeted, evolution_repeated
机制所指范围：{'quality_gate': 'run_report', 'workflow_control': 'run_report', 'evolution_targeted': 'run_report', 'evolution_repeated': 'run_report'}
REJECT被当作普通完成放行Ship、陈旧阻塞和重复stale-base出现；终态明确是REJECT收口。
替代解释/限制：拒绝候选本身是有效风险拦截，应与错误推进机制分开。
证据：generation-fb76b872, generation-c21fe25a, 01M13ADF830VEG4846HNT869FX, 01M13AQ9HTJNMY5GYYG2KKGJF4

## 21. plan-523d5d71 — I166 Plan DAG 开源框架迁移与交付
结果类别：integrated_deployment_pending；证据级别：E2；机制：role_boundary, workflow_control, external_dependency, evolution_targeted
机制所指范围：{'role_boundary': 'run_report', 'workflow_control': 'run_report', 'external_dependency': 'run_report', 'evolution_targeted': 'run_report'}
inline任务被状态接口提示fork但executor接口禁止；更正动作后启动；后续部署与展示问题仍在。
替代解释/限制：历史源码可证接口矛盾，未证明所有错误fork/409均由此造成。
证据：01M2MV4NVZX2ZGBZH08MH0ZD0W, generation-7d3a1d9f, 01M2QA4GN87FNGYE1N5KE3H1KX

## 22. plan-07c8315b — Insight 产品逻辑整改落地与真实页面复验
结果类别：reported_deployed；证据级别：E1；机制：role_boundary, workflow_control, quality_gate, evolution_targeted
机制所指范围：{'role_boundary': 'run_report', 'workflow_control': 'run_report', 'quality_gate': 'run_report', 'evolution_targeted': 'run_report'}
真实null崩溃REJECT后修复，owner复核报告PASS；旧Gate任务仍open且清理权限不匹配。
替代解释/限制：业务交付与编排卫生可不同步；部署层次需按原始报告限定。
证据：generation-b945e866, 01M16QEX24C1CYH2RSN3QJEQ7Y, 01M16V4DV7WXTQ60V4MVVYWRNK

## 23. plan-f4e88e01 — Plan Progress Control Phase 0：持续推进不变量落地
结果类别：reported_integrated；证据级别：E1；机制：workflow_control, evolution_targeted, evolution_repeated
机制所指范围：{'workflow_control': 'run_report', 'evolution_targeted': 'run_report', 'evolution_repeated': 'run_report'}
52/52完成但旧condition阻止收口，多次加任务恢复frontier未消除状态口径冲突。
替代解释/限制：从记录观察到结构不一致；agent归因四次复发还需原始状态逐次重建。
证据：01M14G7969YVMJ7S3MVCNX3B2J, 01M14H36SBVRAE30BWKFHTVP01, generation-86c48265

## 24. plan-6be24696 — 统一 Access 权限中心落地
结果类别：reported_integrated；证据级别：E1；机制：quality_gate, evolution_targeted
机制所指范围：{'quality_gate': 'run_report', 'evolution_targeted': 'run_report'}
REJECT后追加有界修复并保留验收节点，最终单候选集成。
替代解释/限制：可用作有针对性evolution正例，仍缺固定图对照。
证据：generation-c85afa83, 01M05WBTVH2BDGBH39MBCZFQJC

## 25. plan-5a5f20b6 — Spark DGX Manager V1 开发交付计划
结果类别：reported_deployed；证据级别：E1；机制：workflow_control, quality_gate, evolution_targeted
机制所指范围：{'workflow_control': 'run_report', 'quality_gate': 'run_report', 'evolution_targeted': 'run_report'}
stale执行/活租约阻止恢复；NO-GO与后续真实API修复后有部署及请求受理报告。
替代解释/限制：202/pending仅说明请求受理，不证明整个业务操作已执行成功。
证据：generation-57b10f96, 01M1EPXRW2EMYHAB5TB3H4AEV4, 01M1EW0TMV4D4CTTGE3848K9XB, 01M1FXCW8C318MFFSN8NDQ83KQ

## 26. plan-cbf84949 — RAM Role 分层整改与全真验收
结果类别：reported_integrated；证据级别：E1；机制：workflow_control, quality_gate, evolution_targeted
机制所指范围：{'workflow_control': 'run_report', 'quality_gate': 'run_report', 'evolution_targeted': 'run_report'}
fresh build错误触发修复—复验—集成；交付完成后旧effective阻塞仍需清理。
替代解释/限制：修复链有实质产物，代数高不应自动归为无效循环。
证据：01M0VVC1EH314M3GG1FP3HDHKE, 01M0VYSY3F7KEHHM1YWSM05WS8, 01M0VZ8HHDM0RPV5EE2K4FFMGN

## 27. plan-2a8bc24c — Plan 终态确定性收口：实现、迁移、验收、合并、运行时
结果类别：reported_deployed；证据级别：E1；机制：workflow_control, quality_gate, evolution_targeted
机制所指范围：{'workflow_control': 'run_report', 'quality_gate': 'run_report', 'evolution_targeted': 'run_report'}
证据未推送被拒绝，随后保留原证据恢复持久交付；最终仍有陈旧ack_wake投影。
替代解释/限制：agent介入恢复不等于真人介入；恢复的文件内容需原始日志验证。
证据：01M1613Z2Z1W3XVR6VXF4N24FE, 01M161DAXHW2WGDQBBQYQWMGTH, 01M164R20MGQW13GZAM4D95MQS

## 28. plan-d7ec8ee4 — Insight 态势中心 IA / Project 交付与 Plan Evolution 落地
结果类别：replaced_plan；证据级别：E1；机制：role_boundary, workflow_control, evolution_repeated
机制所指范围：{'role_boundary': 'run_report', 'workflow_control': 'run_report', 'evolution_repeated': 'run_report'}
工具发现误解、平台能力缺口与相似链反复生成，最后废弃并重建。
替代解释/限制：工具能力确实不可用与agent误解都有记录，不能一概归因模型。
证据：generation-d1b3a9e0, 01M1BG1RESSKDAZ6MG61VZ7C09, 01M1BN2404P5H55K9TCTB0H0WH

## 29. plan-d1680545 — Spark Manager 六领域并行开发与生产落地
结果类别：replacement_task_delivery；证据级别：E1；机制：role_boundary, workflow_control
机制所指范围：{'role_boundary': 'run_report', 'workflow_control': 'run_report'}
原任务误派外部Agent而stale，同Issue的新Task报告交付。
替代解释/限制：局部替代交付不证明六领域整Plan完成；paused不能自动计失败。
证据：01M1WZMK71XWDDDK56XQPSM1N7, 01M1X2SF9MWE1773M3Q1V655HQ

## 30. plan-33c168dc — P0 编排恢复与通知语义修复自动部署
结果类别：owner_cancelled；证据级别：E2；机制：workflow_control, quality_gate
机制所指范围：{'workflow_control': 'candidate_code', 'quality_gate': 'run_report'}
分类实现误伤被审查发现，owner明确废弃方案转讨论。
替代解释/限制：主动撤回方案不是自主执行卡死；源码支持缺陷存在但没有本轮重跑。
证据：01M0VVF3NEHW7F8T8SETJC1PBQ, 01M0W338J0NHP88M6NW6R75D2P, 01M0W34ZXBXK7E6M0XX8BNJBQB
