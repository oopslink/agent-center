# P1图表与数据

当前5图、9张主文表。图1–4及原实验数值保持不变；014新增图5和表2，呈现多时点人工纠偏与9月恢复链条。原表2–8顺延为表3–9。

[014修改记录](REVISION-014.md) · [纵向分析](longitudinal-analysis.md) · [014派生证据包](longitudinal-014/README.md) · [原实验报告](controlled-experiments.md) · [原实验底表](experiment-data.json) · [原逐行评分](experiment-results.csv) · [原实验来源](experiment-provenance.json)。

图5来自[timeline.json](longitudinal-014/derived/timeline.json)，[figure.py](longitudinal-014/scripts/figure.py)生成同源PNG/SVG/PDF。左图是精选日期记录，不是发生率趋势；右图将原生状态、用户指令和Agent报告分开。新Task的创建领域事件发生在具体行动指令之后、身份提醒之前，后来仍失败，不能据此声称身份提醒单独造成恢复或最终交付。底表分别标明领域事件时间与Task.created_at，图与正文采用前者。

原图4仍可用[make_figure.py](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/make_figure.py)复算；图1–3的历史统计来源仍是figure-data.json及figure-data-provenance.json。合成情境响应不等于真实交付率，精选材料不等于全体错误或人工干预次数。全部研究尚未人工复核。

## 017真实执行

图6新增全部任务—条件—模型单元与全部配对总决策差，0差与未完成均保留。PNG为300dpi，另有矢量SVG/PDF；180×176mm，DejaVu Sans，正文／刻度约7–9pt。已逐图检查可读性和边界，没有总体置信区间。

[PNG](figures/p1-fig6-runtime-identity.png) · [矢量PDF](figures/p1-fig6-runtime-identity.pdf) · [SVG](figures/p1-fig6-runtime-identity.svg) · [完整底表](../runtime-experiments-017/figure-data.json) · [绘图代码](../runtime-experiments-017/make_figures.py)。

图7展示 16 个早期失败共同首答后的全部 48 条轨迹、实际动作及终态，另列可修候选的验收数。参见[图7 PDF](figures/p1-fig7-case-replay.pdf)、[原始底表](case-replay-019/outcomes.csv)与[绘图源](case-replay-019/source/figure.py)。补充历史等待组的 24 条轨迹另存，不与图中首答混计。
