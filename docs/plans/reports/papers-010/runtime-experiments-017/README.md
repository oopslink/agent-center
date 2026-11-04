# P1/P2多轮执行与交接实验（017）

本包补上实际文件修改、模型worker子进程、测试与工件交接；不再只观察一次动作选择。主要分析使用H04仪器修复后的v2全54次复验。v1的34个完成结果与20个未启动项全部保留并退出主比较，没有只重跑失败项。v2不是新的未见任务确认；所有分析尚未人工复核。

- [研究发现与边界](report.md)；[协议](protocol.md)；[任务与全部条件](design.json)。
- [全部54次底表](analysis/episode-results.csv)；[条件汇总](analysis/condition-summary.json)；[全部配对](analysis/paired-comparisons.json)；[案例选择规则](analysis/case-selection.json)；[案例证据](analysis/selected-case-events.json)。
- [全阶段调用与成本](analysis/all-run-accounting.json)；[原始输入检查](analysis/researcher-prompt-audit.json)；[全部格式错误与恢复](analysis/all-error-cases.json)；[校验与来源](analysis/validation.json)；[终止产物独立重放](analysis/artifact-replay.json)。
- [缺陷与处理](bug-register.md)；[v1全轮排除账](v1-exclusion-ledger.json)；[v1负例复核](review/v1-outcome-audit.md)；[v2精确源码差分](review/v2-instrument.diff)；[仪器测试报告](test-report.md)。
- [v1冻结源](source-v1/runtime.py)与[v2冻结源](source-v2/runtime.py)；[绘图脚本](make_figures.py)；[图表底表](figure-data.json)。旧论文实验及图表没有覆盖。

## 原始数据与复现

完整原始档案保存在[本地实验目录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p12-runtime-003)：episodes-v1保留旧轮，episodes保留v2，pilot／pilot-v2／pilot-v3保留三轮开发试点。每次调用有request、原始stdout/stderr及metadata；每个episode保存角色事件、所有内容寻址文件版本、终止快照、真实检查与进程结果。[原始压缩包及SHA](raw-archives.json)另外保留三阶段完整副本。仓库保存完整SHA清单、派生数据及源码；不把本地可访问等同公开数据许可。

在原始实验目录运行 `python -B analyze.py`、`python -B validate_results.py`、`python -B audit_and_extract.py` 可复算，不调用模型。`make_figures.py` 需要matplotlib。`recheck_artifacts.py` 将终验在副本重放，要求新建的verification-replay目录尚不存在；已有结果见analysis/artifact-replay.json。校验脚本证明指定一致性，不能证明无任何仪器缺陷或科学结论充分。

新的模型复现须复制source-v2至空目录，保留sandbox子目录，在有CLI认证的环境使用Python3和原记录的Claude CLI／模型标识。先运行 `python -B -m unittest test_runtime -v`。`run_batch.py` 核对design-lock后按54个条件运行；模型服务和随机采样变化意味着不能保证逐字重现。不要在已有episodes目录覆盖或拼接原实验。主批40美元报告价仅是新episode派发阈值，未完成调用仍会结束；不是实际订阅扣费估计。

## 仍缺什么

每篇3个小函数任务、每条件一次；初始失败是脚本预置，天然新错误与长期多次恢复并非充分采样。没有人工提醒实验、真实外部权限阻塞对照、长期上下文、生产版本回放或同源训练干预。P1保持实际分离拓扑，只操纵身份表述；台账包含状态刷新。P2改变实际角色边界，但同时改变上下文分区与预算余量。后续应以新任务及重复运行区分信息缺口、交接成本和持续恢复机制，不能把本轮无差异写成普遍等价。
