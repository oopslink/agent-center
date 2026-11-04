# 终检记录（Task 14）

两篇稿件各过两道检查：`paper-audit` 的脚本层，以及两名独立派发的评审。
本文件记录**每道检查实际做了什么、没做什么**，以免把工具的回退输出当成审稿结论。

## 1 paper-audit 的脚本层：实际产出两条，都不是论文缺陷

命令（两篇各一次）：

```
pandoc <paper>.md -o <paper>.tex --standalone
uv run python -B $SKILL/scripts/prepare_review_workspace.py <paper>.tex --output-dir ./review_results
uv run python -B $SKILL/scripts/audit.py <paper>.tex --mode deep-review --lang zh --focus full --review-dir ./review_results/<paper>
```

环境限制：本机没有任何 PDF 引擎（xelatex / lualatex / tectonic / weasyprint / wkhtmltopdf / typst 均无），也没有 pymupdf，因此走 `.tex` 路径而非 PDF 路径。`paper-audit` 为独立安装，缺少同级写作技能的脚本，运行日志显示 **figures / consistency / spec / blind / abstract / conclusion / literature / tables 等 lane 被 SKIP（script not found）**。这正是该技能文档所说的"单独安装为有限覆盖"。

两篇的结构化问题包各含两条，逐条处置如下：

| 严重度 | 发现 | 处置 | 理由 |
|---|---|---|---|
| moderate | Abstract was not detected; submission packages need a visible abstract | **不修** | pandoc 把 Markdown 的 `## 摘要` 转成普通 section，没有生成 `\begin{abstract}` 环境。两篇的 Markdown 原文都有摘要。这是转换产物的假阳性，不是稿件缺陷。排版阶段生成真正的投稿包时需要确认摘要环境，已记入后续阶段。 |
| minor | Deep review requires manual reviewer judgment | **按其要求执行** | 这不是对稿件的发现，而是工具自述："确定性回退无法从抽取的文本得出 lane 发现，应由人或 agent 评审直接检查工作区。"全部五个 committee lane（editor / theory / literature / methodology / logic）与 claims_vs_evidence lane 返回的都是这同一句占位文本。 |

`references` 检查两篇均为 clean。

**必须写明的一点：** 运行摘要给出的"委员会评分 8.1/10、主编裁定 Pass to Review、审稿推荐 小修"是**确定性回退的输出**，不是评审团的判断——因为上一段说明了判断层没有产生任何发现。把它当成审稿结论会是虚假陈述。本记录不采用该评分。

## 2 两名独立派发的评审：真正的审稿意见来源

因为脚本层的判断部分为空，终检的实质内容来自两名独立派发的评审，各读一篇，各自有自己的上下文，**不知道执行者的结论**。给它们的指令要求：

- 每条发现锚定到精确引文或小节号；
- 区分 `[已验]`（实际运行了复现包的 `verify.py` 或核对了数据）与 `[判断]`（阅读判断）；
- **不得建议加强主张**；若某主张需要补实验才能支撑，正确处置是削弱该主张；
- 不提格式与文献风格琐碎项；
- 某类检查无问题须明确说"未发现"，不得凑数。

A 篇的检查重点：主张强度是否越过"只做描述与机制解释"这条自设边界；数字在摘要／正文／表格／证据边界之间是否一致；证据级别与表述是否匹配（五级证据各自允许的说法）；自述的方法限制是否足以支撑它仍然做出的主张。

B 篇的检查重点：两个因子是否只改变了各自声称改变的东西；效应表述与 n=5／n=4 的样本量是否匹配；事后决定的披露是否完整（尤其试跑与正式批次之间改了什么，是否构成"迭代到出现效应"）；仪器可信度（作者自改运行器、桥接重跑 n=1、oracle 的保护是否有效）；结论是否恰当限定了"交付差异只出现在较弱模型"。

两名评审的发现与逐条处置见 `findings.md`。

## 3 这道终检没有覆盖什么

- **没有人类审稿人。** 两名评审是独立派发的 agent，上下文独立但不是人，也不是独立的评审团。
- **没有同行评审意义上的一致性。** 两篇各一名评审，发现没有交叉验证。
- **没有排版级检查。** 未生成 PDF，未检查图表编号、分栏、字体与投稿模板合规；这些属于后续的翻译与排版阶段。
- **没有覆盖被 SKIP 的 lane。** figures / consistency / spec / blind / tables 等脚本缺失，相应检查未执行。
