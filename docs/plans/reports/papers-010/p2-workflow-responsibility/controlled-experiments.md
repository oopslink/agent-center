# 本轮机制分析与主要结果

### 4.7 对照未提供复杂度致错的有效证据，评分审计发现材料缺陷

256次请求均正常返回且模型标识一致，冻结规则原评分为Sonnet 107/128、Haiku 102/128（表7）。这一差距不能直接解释为责任推理差异：W2同时给出待集成目标与integration_ok=true，W8没有说明回执适用哪份合同，却预设release为唯一答案。两题共64次响应中42次未匹配，不能承担模型或流程负效应归因；全部原始响应与原评分保留，未仅删除不利输出。

排除这两题的全部条件后，Sonnet为91/96，Haiku为96/96。Sonnet其余五次均在正确JSON前附加解释，违反指定输出接口；事后从原文提取唯一内嵌JSON，六个情境的动作、主体、对象与合同两模型均为96/96。该诊断不把格式失败改写为合格响应。Sonnet原始collapsed减expanded差4.69个百分点，排除两题后为6.25个百分点，但全部来自格式；Haiku排除后各因素差均为0。联表的原始优势同样不能脱离题目缺陷解释。

**表7。原评分、有效性排除与另行复测（不合并分母）**

| 分析层 | Sonnet 5.5 | Haiku 5.5 | 含义 |
|---|---:|---:|---|
| 原冻结严格评分 | 107/128 | 102/128 | 含矛盾／缺信息题，保留审计用途 |
| 排除W2、W8全部条件 | 91/96 | 96/96 | 六情境；Sonnet剩五次为格式不合规 |
| 上行内嵌JSON字段诊断 | 96/96 | 96/96 | 事后提取，不覆盖严格得分 |
| W2、W8材料修复后新调用 | 32/32 | 32/32 | 事后冻结复测，未混入主分母 |
| 事前一次字段反馈 | 未抽到 | 0/24 | 全为缺陷题；不能估计真实纠错能力 |

事前反馈按固定排序取前24个未匹配输出，恰好全属Haiku的W2/W8，均未达到原判据。由于输入本身存在问题，该结果只说明此反馈没有消除原评分不匹配，不能称模型坚持错误。另冻结修复W2状态与W8合同信息后的64次新调用，两模型分别为32/32与32/32；这是材料修复诊断，不能证明架构简化有效，也不能补成历史生产事故的对照。

![图5：流程对照与材料有效性审计](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p2-workflow-responsibility/figures/p2-fig5-controlled-mechanisms.png)

**图5。**上左为排除缺陷题后的全条件严格得分（每格12次）；上右保留原八情境分数，星号标记不能承担负面归因的W2/W8；下部为六情境的配对差（每模型每因素48对）及另行材料修复结果。格式问题与语义字段分开解释，非团队停摆率。原128次／模型底表、全部反例和修复输入差分见实验附录。



> **有效性审计更新：**原评分不覆盖。[材料审计](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/analysis/validity-audit.json)排除P2 W2/W8、P4 D5/D8的负面归因；13次格式不合规和6次引用用途歧义单列。事后修复128次全部匹配，详见[修复协议](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/validity-repair/protocol.md)和[结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/validity-repair/summary.json)。不能把原评分低分解释为模型架构失败。

# P2：受控机制实验完整结果

本附录是768次主调用中P2的256次；8固定情境×8条件×2顺序版本×2模型。不是256个独立任务。全部研究尚未人工复核；模型解释不是内部机制真值。主结果包含格式失败，后续纠正不回写。

## 协议、输入和运行

[事前协议](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/protocol.md) · [冻结哈希](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/design-lock.json) · [完整输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/design.json) · [方法实施边界](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/method-details.md) · [原始目录与复算](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/README.md)。模型使用固定Sonnet/Haiku 5.5、low effort，每调用fresh process，关闭工具、MCP与持久会话。当前说明一致，记录作用域明确；平台不可见上下文仍不能完整审计。

## 模型分层结果

| 模型 | 返回/计划 | 严格符合协议 | 格式失败 | 核心字段正确 | 直接解决（仅P3） |
|---|---|---|---|---|---|
| claude-sonnet-5-5 | 128/128 | 107 | 5 | 107 | 不适用 |
| claude-haiku-5-5 | 128/128 | 102 | 0 | 102 | 不适用 |

正确是动作、主体/候选、对象与合同等冻结字段同时匹配；P3还要求当前且获准的证据引用。JSON失败不作为语义错误或责任混淆。P3直接解决表示材料充分并符合协议，拒绝、请求已知缺少的授权或向有权者返回结果均可能合格；不表示产品已交付。

## 全条件

| 条件 | 因素 | Sonnet符合/16 | Haiku符合/16 |
|---|---|---|---|
| C01 | depth=collapsed, binding=bound, distractors=absent | 14 | 13 |
| C02 | depth=collapsed, binding=bound, distractors=present | 13 | 14 |
| C03 | depth=collapsed, binding=distributed, distractors=absent | 14 | 12 |
| C04 | depth=collapsed, binding=distributed, distractors=present | 14 | 12 |
| C05 | depth=expanded, binding=bound, distractors=absent | 14 | 12 |
| C06 | depth=expanded, binding=bound, distractors=present | 13 | 14 |
| C07 | depth=expanded, binding=distributed, distractors=absent | 12 | 13 |
| C08 | depth=expanded, binding=distributed, distractors=present | 13 | 12 |

## 全情境

| 情境 | Sonnet符合/16 | Haiku符合/16 |
|---|---|---|
| W1 | 16 | 16 |
| W2 | 16 | 1 |
| W3 | 15 | 16 |
| W4 | 16 | 16 |
| W5 | 13 | 16 |
| W6 | 16 | 16 |
| W7 | 15 | 16 |
| W8 | 0 | 5 |

## 全部配对差

差值为A减B；每一对固定情境、顺序版本和其它因素。报告有限固定设计的描述性差值，不计算总体显著性、等效性或生产失效率。

| 模型 | 指标 | A − B | 配对数 | 差（百分点） | 仅A正确 | 仅B正确 |
|---|---|---|---|---|---|---|
| claude-sonnet-5-5 | correct | collapsed − expanded | 64 | +4.688 | 4 | 1 |
| claude-sonnet-5-5 | correct | bound − distributed | 64 | +1.562 | 3 | 2 |
| claude-sonnet-5-5 | correct | absent − present | 64 | +1.562 | 2 | 1 |
| claude-haiku-5-5 | correct | collapsed − expanded | 64 | +0.000 | 2 | 2 |
| claude-haiku-5-5 | correct | bound − distributed | 64 | +6.250 | 5 | 1 |
| claude-haiku-5-5 | correct | absent − present | 64 | -3.125 | 1 | 3 |

## 全部未匹配冻结评分的输出

按ID排序完整列出，没有只保留符合预期的反例。truth是冻结评分答案，缺陷题不具有可识别的唯一答案；reason是模型可见文字，不作为因果解释。

### P2-haiku-C01-W2-v0

格式有效：True；不匹配字段：action, actor, object, contract。

```json
{
  "action": "integrate",
  "actor": "a-28",
  "object": "iteration-branch-12",
  "contract": "K-build-12"
}
```

```text
```json
{"action":"deliver_report","actor":"a-39","object":"report-12","contract":"K-report-12","reason":"Integration is already recorded as ok (integration_ok=true), and the build, runtime, and approval facts are also true. The goal is limited to the iteration branch, so no production release action is in scope. The tester, a-39, is the authorized owner of deliver_report, so the next step is to deliver the report."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C01-W2-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C01-W2-v0.json)

### P2-haiku-C01-W2-v1

格式有效：True；不匹配字段：action, actor, object, contract。

```json
{
  "action": "integrate",
  "actor": "a-28",
  "object": "iteration-branch-12",
  "contract": "K-build-12"
}
```

```text
{"action":"deliver_report","actor":"a-39","object":"report-12","contract":"K-report-12","reason":"Integration is already recorded as ok (integration_ok true) and the goal is iteration-branch integration only, not a production release. Build, runtime, and approval are also recorded as true, so the remaining authorized step is delivering the report under the report contract."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C01-W2-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C01-W2-v1.json)

### P2-haiku-C01-W8-v0

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
```json
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-report-18","reason":"The current goal repeats a completed check for the same candidate, environment and contract. Receipt-18 is still valid, and no new condition exists, so the existing verified evidence is reused instead of rerunning verification. Reuse is authorized for the tester role (a-39)."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C01-W8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C01-W8-v0.json)

### P2-haiku-C02-W2-v0

格式有效：True；不匹配字段：action, actor, object, contract。

```json
{
  "action": "integrate",
  "actor": "a-28",
  "object": "iteration-branch-12",
  "contract": "K-build-12"
}
```

```text
```json
{"action":"deliver_report","actor":"a-39","object":"report-12","contract":"K-report-12","reason":"Build, integration, runtime, and approval are all true, so release_ready holds for the iteration branch. The remaining step is delivering the report, and the tester owns deliver_report. Production release is not part of this goal."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C02-W2-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C02-W2-v0.json)

### P2-haiku-C02-W8-v0

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-report-18","reason":"The current goal is a repeated check of the same candidate, environment and contract. The verified receipt is still valid and no new condition exists, so the existing evidence is reused instead of re-verifying."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C02-W8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C02-W8-v0.json)

### P2-haiku-C03-W2-v0

格式有效：True；不匹配字段：action, actor, object, contract。

```json
{
  "action": "integrate",
  "actor": "a-28",
  "object": "iteration-branch-12",
  "contract": "K-build-12"
}
```

```text
{"action":"deliver_report","actor":"a-39","object":"report-12","contract":"K-report-12","reason":"Build, integration, runtime, and approval facts are all true, so the integration goal is already satisfied on the iteration branch. The remaining authorized step is delivering the tester's report. This is an iteration-branch result only, not a production release, and does not authorize release to production-main-12."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C03-W2-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C03-W2-v0.json)

### P2-haiku-C03-W2-v1

格式有效：True；不匹配字段：action, actor, object, contract。

```json
{
  "action": "integrate",
  "actor": "a-28",
  "object": "iteration-branch-12",
  "contract": "K-build-12"
}
```

```text
{"action":"deliver_report","actor":"a-39","object":"report-12","contract":"K-report-12","reason":"Build, integration, runtime, and approval are all true, so the release_ready gate is satisfied. The goal is integration into the iteration branch, which is already complete, and this is not a production release. The remaining authorized step is delivering the report, which the tester role owns."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C03-W2-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C03-W2-v1.json)

### P2-haiku-C03-W8-v0

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"The current goal repeats an already-completed check for the same candidate, environment and contract. The receipt is still valid, and no new condition exists. The tester role owns reuse_evidence, and the existing receipt covers the build_ok, integration_ok and runtime_ok inputs for release_ready."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C03-W8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C03-W8-v0.json)

### P2-haiku-C03-W8-v1

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"The request repeats an already-completed check for the same candidate, environment and contract, and the verified receipt remains valid with no new condition. The tester owns reuse_evidence, so the existing receipt is reused instead of re-running verification."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C03-W8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C03-W8-v1.json)

### P2-haiku-C04-W2-v0

格式有效：True；不匹配字段：action, actor, object, contract。

```json
{
  "action": "integrate",
  "actor": "a-28",
  "object": "iteration-branch-12",
  "contract": "K-build-12"
}
```

```text
{"action":"deliver_report","actor":"a-39","object":"report-12","contract":"K-report-12","reason":"Build, integration, runtime, and approval facts are all true, so the release_ready gate is satisfied for this iteration. Integration is already recorded as ok, so the next authorized step is the tester's report delivery. This does not authorize a production release, which requires K-release-12 and production-main-12."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C04-W2-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C04-W2-v0.json)

### P2-haiku-C04-W2-v1

格式有效：True；不匹配字段：action, actor, object, contract。

```json
{
  "action": "integrate",
  "actor": "a-28",
  "object": "iteration-branch-12",
  "contract": "K-build-12"
}
```

```text
```json
{"action":"deliver_report","actor":"a-39","object":"report-12","contract":"K-report-12","reason":"Build, integration, runtime, and approval facts are all true, so the integration into iteration-branch-12 is complete. The goal is an iteration integration, not a production release, so the report is delivered under K-report-12 and K-release-12 is not invoked."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C04-W2-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C04-W2-v1.json)

### P2-haiku-C04-W8-v0

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
```json
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"The request repeats a completed check for the same candidate-18, environment, and contract. receipt-18 is still valid, and no new condition exists, so the verified receipt is reused instead of re-running verification. The tester role owns reuse_evidence. Unrelated-scope records are excluded."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C04-W8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C04-W8-v0.json)

### P2-haiku-C04-W8-v1

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-report-18","reason":"The current goal repeats a completed check for the same candidate, environment and contract. The verified receipt remains valid and no new condition exists, so the tester reuses the existing evidence instead of re-verifying."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C04-W8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C04-W8-v1.json)

### P2-haiku-C05-W2-v0

格式有效：True；不匹配字段：action, actor, object, contract。

```json
{
  "action": "integrate",
  "actor": "a-28",
  "object": "iteration-branch-12",
  "contract": "K-build-12"
}
```

```text
{"action":"deliver_report","actor":"a-39","object":"report-12","contract":"K-report-12","reason":"All gates are satisfied (g1 through g7 and release_ready are true), and integration_ok is already recorded, so no further integration is needed. The remaining step is to deliver the report for this iteration-branch integration. This is not a production release, so no release contract is invoked."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C05-W2-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C05-W2-v0.json)

### P2-haiku-C05-W2-v1

格式有效：True；不匹配字段：action, actor, object, contract。

```json
{
  "action": "integrate",
  "actor": "a-28",
  "object": "iteration-branch-12",
  "contract": "K-build-12"
}
```

```text
```json
{"action":"deliver_report","actor":"a-39","object":"report-12","contract":"K-report-12","reason":"Integration into iteration-branch-12 is already recorded as ok (integration_ok), and build, runtime, and approval are all satisfied. The goal is an iteration-branch integration, not a production release, so the remaining authorized step is to deliver the report under K-report-12. No release action is taken."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C05-W2-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C05-W2-v1.json)

### P2-haiku-C05-W8-v0

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"The current goal is a repeated check for the same candidate, environment and contract. receipt-18 is still valid and no new condition exists, so the tester reuses the existing receipt. Release is not requested and is not authorized by this action."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C05-W8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C05-W8-v0.json)

### P2-haiku-C05-W8-v1

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-report-18","reason":"The goal is a repeated check of the same candidate, environment and contract, and receipt-18 is still valid with no new condition. Reusing the verified receipt is the authorized action, and the tester owns it."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C05-W8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C05-W8-v1.json)

### P2-haiku-C06-W2-v0

格式有效：True；不匹配字段：action, actor, object, contract。

```json
{
  "action": "integrate",
  "actor": "a-28",
  "object": "iteration-branch-12",
  "contract": "K-build-12"
}
```

```text
{"action":"deliver_report","actor":"a-39","object":"report-12","contract":"K-report-12","reason":"Integration into iteration-branch-12 is already recorded as ok (integration_ok true), and build, runtime, and approval gates are all satisfied for candidate-12. The current goal is integration only, not production release, so no release action applies. The remaining authorized step is to deliver the report, which a-39 owns as tester."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C06-W2-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C06-W2-v0.json)

### P2-haiku-C06-W2-v1

格式有效：True；不匹配字段：action, actor, object, contract。

```json
{
  "action": "integrate",
  "actor": "a-28",
  "object": "iteration-branch-12",
  "contract": "K-build-12"
}
```

```text
{"action":"deliver_report","actor":"a-39","object":"report-12","contract":"K-report-12","reason":"Integration is already recorded as passed (integration_ok=true) and the build, runtime, and approval gates are satisfied. The remaining in-scope step is to deliver the report. This is an iteration-branch integration, not a production release, so no release action is taken."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C06-W2-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C06-W2-v1.json)

### P2-haiku-C07-W2-v0

格式有效：True；不匹配字段：action, actor, object, contract。

```json
{
  "action": "integrate",
  "actor": "a-28",
  "object": "iteration-branch-12",
  "contract": "K-build-12"
}
```

```text
{"action":"deliver_report","actor":"a-39","object":"report-12","contract":"K-report-12","reason":"Gates g1-g4 are satisfied (build, integration, runtime, approval), so the iteration-branch integration is complete. The remaining authorized step is to report it under the report contract. This is not a release action; release_ready is not requested."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C07-W2-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C07-W2-v0.json)

### P2-haiku-C07-W2-v1

格式有效：True；不匹配字段：action, actor, object, contract。

```json
{
  "action": "integrate",
  "actor": "a-28",
  "object": "iteration-branch-12",
  "contract": "K-build-12"
}
```

```text
{"action":"deliver_report","actor":"a-39","object":"report-12","contract":"K-report-12","reason":"Integration (integration_ok), build, runtime tests, and approval gates are all satisfied for the iteration branch. Delivering the report is the remaining step. This is a non-production report and does not authorize release."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C07-W2-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C07-W2-v1.json)

### P2-haiku-C07-W8-v1

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
```json
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-report-18","reason":"The request repeats a completed check for the same candidate, environment, and contract. Receipt-18 is still valid, no new condition exists, and reuse is the tester's authorized action for receipt evidence."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C07-W8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C07-W8-v1.json)

### P2-haiku-C08-W2-v0

格式有效：True；不匹配字段：action, actor, object, contract。

```json
{
  "action": "integrate",
  "actor": "a-28",
  "object": "iteration-branch-12",
  "contract": "K-build-12"
}
```

```text
{"action":"deliver_report","actor":"a-39","object":"report-12","contract":"K-report-12","reason":"Current facts show build_ok, integration_ok, runtime_ok, and approval are all true, so the integration goal's gates are satisfied and integration need not be repeated. The tester is authorized to deliver the report for this goal. This is a report for the iteration branch only and does not authorize release."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C08-W2-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C08-W2-v0.json)

### P2-haiku-C08-W2-v1

格式有效：True；不匹配字段：action, actor, object, contract。

```json
{
  "action": "integrate",
  "actor": "a-28",
  "object": "iteration-branch-12",
  "contract": "K-build-12"
}
```

```text
```json
{"action":"deliver_report","actor":"a-39","object":"report-12","contract":"K-report-12","reason":"All gates are satisfied (build_ok, integration_ok, runtime_ok, approval), so the candidate is verified and approved for the iteration branch. The remaining step is to deliver the report through the tester role. This is not a production release, and the other-scope records are unrelated to this goal."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C08-W2-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C08-W2-v1.json)

### P2-haiku-C08-W8-v0

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
```json
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"The goal is a repeat of an already-completed check for the same candidate, environment and contract. receipt-18 is valid, and no new condition exists. The tester role owns reuse_evidence. This does not authorize release or delivery."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C08-W8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C08-W8-v0.json)

### P2-haiku-C08-W8-v1

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
```json
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"The requested check repeats a completed build verification for the same candidate, environment and contract. The receipt is still valid and no new condition exists, so the existing receipt is reused instead of re-verifying."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-haiku-C08-W8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-haiku-C08-W8-v1.json)

### P2-sonnet-C01-W8-v0

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"Repeated check for identical candidate, environment and contract; verified receipt remains valid and no new condition exists, so reuse it instead of re-verifying."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C01-W8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C01-W8-v0.json)

### P2-sonnet-C01-W8-v1

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"The request repeats an already-completed check for the same candidate, environment and contract. The verified receipt is still valid and no new condition exists, so the tester reuses it instead of re-verifying."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C01-W8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C01-W8-v1.json)

### P2-sonnet-C02-W7-v0

格式有效：False；不匹配字段：action, actor, object, contract。

```json
{
  "action": "deliver_report",
  "actor": "a-39",
  "object": "report-17",
  "contract": "K-report-17"
}
```

```text
The goal is a NO-GO evidence report, and the report is complete and accurately lists the unmet release conditions (runtime_ok is false). Delivering it is the next authorized action. The tester (a-39) owns deliver_report, and the report contract is K-report-17. Delivery does not authorize release.

{"action":"deliver_report","actor":"a-39","object":"report-17","contract":"K-report-17","reason":"The NO-GO report is complete and accurately lists the unmet release conditions (runtime_ok false, so release_ready fails). Delivering it satisfies the current goal and does not imply release authorization."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C02-W7-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C02-W7-v0.json)

### P2-sonnet-C02-W8-v0

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"Repeated check for identical candidate-18, scope and environment; verified receipt-18 remains valid and no new condition exists, so reuse it instead of re-verifying. Build contract applies since this is a build check, not release or report."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C02-W8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C02-W8-v0.json)

### P2-sonnet-C02-W8-v1

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"The request repeats a completed check for the identical candidate, environment and contract. The verified receipt is still valid and nothing new has come up, so the existing evidence is reused rather than verifying again. a-39 (tester) owns reuse_evidence."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C02-W8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C02-W8-v1.json)

### P2-sonnet-C03-W8-v0

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"The check is a repeat for the identical candidate, environment and contract. The verified receipt is still valid and no new condition exists, so the tester reuses it instead of re-verifying."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C03-W8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C03-W8-v0.json)

### P2-sonnet-C03-W8-v1

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"The check was already completed for the identical candidate, environment and contract. The verified receipt is still valid and nothing new has come up, so the existing evidence is reused instead of verifying again. The tester (a-39) is responsible for reuse_evidence, and the contract is the build contract, since the repeated check was a build check."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C03-W8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C03-W8-v1.json)

### P2-sonnet-C04-W8-v0

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"The check is a repeat for the identical candidate, environment and contract, the verified receipt is still valid, and no new condition exists. The tester owns reuse_evidence, so the existing receipt is reused instead of re-verifying."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C04-W8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C04-W8-v0.json)

### P2-sonnet-C04-W8-v1

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"The check is a repeat for the identical candidate, environment and contract. The verified receipt is still valid and no new condition exists, so the tester reuses the existing evidence instead of re-verifying."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C04-W8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C04-W8-v1.json)

### P2-sonnet-C05-W8-v0

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"Repeated check for identical candidate, environment and contract; verified receipt remains valid and no new condition exists, so reuse it instead of re-verifying."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C05-W8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C05-W8-v0.json)

### P2-sonnet-C05-W8-v1

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"The request repeats a completed check for the identical candidate, environment and contract. The verified receipt is still valid and no new condition exists, so the existing evidence is reused instead of re-verifying."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C05-W8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C05-W8-v1.json)

### P2-sonnet-C06-W3-v0

格式有效：False；不匹配字段：action, actor, object, contract。

```json
{
  "action": "verify",
  "actor": "a-39",
  "object": "candidate-13",
  "contract": "K-release-13"
}
```

```text
Runtime check (g3) is missing, and release_ready requires it. The tester (a-39) owns verification, and the candidate is candidate-13. Release contract applies since the runtime check belongs to the release contract.

{"action":"verify","actor":"a-39","object":"candidate-13","contract":"K-release-13","reason":"runtime_ok is false, so g3 and g6 fail and release_ready is blocked. Build and integration already pass and approval is true, so the next step is runtime verification of candidate-13 by the tester under the release contract."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C06-W3-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C06-W3-v0.json)

### P2-sonnet-C06-W8-v0

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"Repeated check for identical candidate, environment and contract; verified receipt remains valid and no new condition exists, so reuse it instead of re-verifying."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C06-W8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C06-W8-v0.json)

### P2-sonnet-C06-W8-v1

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"The request repeats a completed check for the same candidate, environment and contract. The verified receipt is still valid and nothing new has come up, so the existing evidence is reused instead of re-verifying. The reuse_evidence action belongs to a-39 (tester) and applies to receipt-18."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C06-W8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C06-W8-v1.json)

### P2-sonnet-C07-W5-v0

格式有效：False；不匹配字段：action, actor, object, contract。

```json
{
  "action": "wait",
  "actor": "a-41",
  "object": "approval-ticket-15",
  "contract": "K-release-15"
}
```

```text
Approval is requested but still pending (approval=false), and no other work is authorized. The next action is to wait on the approval ticket, owned by the release-owner role (a-41).

```json
{"action":"wait","actor":"a-41","object":"approval-ticket-15","contract":"K-release-15","reason":"Build, integration and runtime checks passed; external approval was requested and is still pending, so release_ready is false. No other work is authorized, so wait on the approval ticket."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C07-W5-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C07-W5-v0.json)

### P2-sonnet-C07-W5-v1

格式有效：False；不匹配字段：action, actor, object, contract。

```json
{
  "action": "wait",
  "actor": "a-41",
  "object": "approval-ticket-15",
  "contract": "K-release-15"
}
```

```text
The approval has been requested but not granted, and no other work is authorized. The next action is to wait on the approval ticket, which the release-owner (a-41) is responsible for. The release contract applies because approval gates the release.

{"action":"wait","actor":"a-41","object":"approval-ticket-15","contract":"K-release-15","reason":"Build, integration and runtime checks pass, but the external approval (g4) is still pending. release_ready stays false until it is granted, and no other work is authorized, so the release-owner waits on the ticket."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C07-W5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C07-W5-v1.json)

### P2-sonnet-C07-W8-v0

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"Repeated check for identical candidate, environment and contract; verified receipt remains valid and no new condition exists, so reuse the receipt instead of re-verifying. Tester owns reuse_evidence."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C07-W8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C07-W8-v0.json)

### P2-sonnet-C07-W8-v1

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"Repeated check for the identical candidate, environment and contract; the verified receipt is still valid and no new condition exists, so the evidence is reused instead of re-verifying."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C07-W8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C07-W8-v1.json)

### P2-sonnet-C08-W5-v1

格式有效：False；不匹配字段：action, actor, object, contract。

```json
{
  "action": "wait",
  "actor": "a-41",
  "object": "approval-ticket-15",
  "contract": "K-release-15"
}
```

```text
Approval has been requested and is pending, so the next authorized action is to wait on the approval ticket. The release-owner (a-41) is responsible for wait. Release contract applies.

{"action":"wait","actor":"a-41","object":"approval-ticket-15","contract":"K-release-15","reason":"All checks pass and approval was already requested but is still pending (approval=false, so g4/g6/release_ready are unmet). Waiting preserves the missing external authorization; no other work is authorized."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C08-W5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C08-W5-v1.json)

### P2-sonnet-C08-W8-v0

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"Repeated check for identical candidate, environment and contract; verified receipt remains valid and no new condition exists, so reuse the tester's receipt instead of re-verifying."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C08-W8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C08-W8-v0.json)

### P2-sonnet-C08-W8-v1

格式有效：True；不匹配字段：contract。

```json
{
  "action": "reuse_evidence",
  "actor": "a-39",
  "object": "receipt-18",
  "contract": "K-release-18"
}
```

```text
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-build-18","reason":"The repeated check targets the same candidate-18, environment and contract, the verified receipt-18 is still valid and no new condition exists. The tester role owns reuse_evidence, so the existing evidence is reused instead of re-verifying."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P2-sonnet-C08-W8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P2-sonnet-C08-W8-v1.json)

## 规则选取的正例

每模型每情境取字典序第一个匹配评分的ID；没有匹配输出的情境不补选。全实验共43条，选择规则早于结果读取。这些不是独立重复实验。

### P2-sonnet-C01-W1-v0

```text
{"action":"assign","actor":"a-17","object":"task-bundle-11","contract":"K-build-11","reason":"Tasks have valid inputs but no assignee (assigned=false); coordinator a-17 owns assign and is authorized. Scope is the current goal only, so assignment is the next action, not release."}
```

### P2-sonnet-C01-W2-v0

```text
{"action":"integrate","actor":"a-28","object":"iteration-branch-12","contract":"K-build-12","reason":"The goal is to integrate the approved candidate into the iteration branch, which is a build-contract action owned by the integrator a-28. This is not a production release, so the release contract and production-main-12 are out of scope."}
```

### P2-sonnet-C01-W3-v0

```text
{"action":"verify","actor":"a-39","object":"candidate-13","contract":"K-release-13","reason":"Build and integration pass, approval is already held, but runtime_ok is false. The release gate needs a runtime check, and verify on the candidate is the tester's action."}
```

### P2-sonnet-C01-W4-v0

```text
{"action":"request_approval","actor":"a-41","object":"approval-ticket-14","contract":"K-release-14","reason":"Build, integration and runtime checks pass, but release_ready also requires approval, which has not been requested. The release owner a-41 is the authorized actor to request it and cannot grant it."}
```

### P2-sonnet-C01-W5-v0

```text
{"action":"wait","actor":"a-41","object":"approval-ticket-15","contract":"K-release-15","reason":"Build, integration and runtime checks passed, but release_ready also requires external approval, which was requested and is still pending. The release-owner owns wait, and there is no other authorized work."}
```

### P2-sonnet-C01-W6-v0

```text
{"action":"repair","actor":"a-52","object":"candidate-16","contract":"K-build-16","reason":"Candidate was rejected for a specific implementation defect and build_ok is false; repair is authorized under the existing build contract. Integration, runtime and approval are already satisfied, so repair by the developer is the next action."}
```

### P2-sonnet-C01-W7-v0

```text
{"action":"deliver_report","actor":"a-39","object":"report-17","contract":"K-report-17","reason":"The goal is a NO-GO evidence report, which is complete and accurately lists the unmet release condition (runtime_ok is false, so release_ready is false). Delivering it is the tester's action under the report contract and does not authorize release."}
```

### P2-haiku-C01-W1-v0

```text
{"action":"assign","actor":"a-17","object":"task-bundle-11","contract":"K-build-11","reason":"Current goal is unassigned tasks with valid inputs, and a-17 is the coord authorized to assign. Assignment is the next authorized step; no other gate is needed for it."}
```

### P2-haiku-C02-W2-v1

```text
{"action":"integrate","actor":"a-28","object":"iteration-branch-12","contract":"K-build-12","reason":"Implementation and independent tests passed (build_ok, runtime_ok, integration_ok, approval all true). The current goal is integrating the approved candidate into the iteration branch, not a production release, so the release gate is not needed and K-release-12 does not apply."}
```

### P2-haiku-C01-W3-v0

```text
{"action":"verify","actor":"a-39","object":"candidate-13","contract":"K-release-13","reason":"Build and integration passed and approval is true, but runtime_ok is false, so release_ready is not satisfied. The release contract still lacks its runtime check. The tester (a-39) owns verify, and verification targets the candidate."}
```

### P2-haiku-C01-W4-v0

```text
{"action":"request_approval","actor":"a-41","object":"approval-ticket-14","contract":"K-release-14","reason":"Build, integration, and runtime checks pass, but approval is false and not yet requested. release_ready needs approval, so the release owner should request it. Approval is not granted by this action."}
```

### P2-haiku-C01-W5-v0

```text
{"action":"wait","actor":"a-41","object":"approval-ticket-15","contract":"K-release-15","reason":"Build, integration, and runtime checks pass and approval has already been requested, so the release gate is blocked only on the pending external approval. Preserve that approval and wait on the approval ticket; do not re-request it or claim release readiness."}
```

### P2-haiku-C01-W6-v0

```text
{"action":"repair","actor":"a-52","object":"candidate-16","contract":"K-build-16","reason":"build_ok is false, so release_ready fails. The candidate was rejected for a specific implementation defect, and repair is authorized under the existing contract. Integration, runtime, and approval are already satisfied, so the next authorized step is repair of the candidate."}
```

### P2-haiku-C01-W7-v0

```text
{"action":"deliver_report","actor":"a-39","object":"report-17","contract":"K-report-17","reason":"The complete NO-GO evidence report is ready and accurately lists unmet release conditions. release_ready is false because runtime_ok is false, so the report is delivered as NO-GO evidence and does not authorize release."}
```

### P2-haiku-C01-W8-v1

```text
```json
{"action":"reuse_evidence","actor":"a-39","object":"receipt-18","contract":"K-release-18","reason":"The goal is a repeated check for the same candidate, environment and contract, and the receipt is still valid with no new condition. The release_ready gate (build_ok, integration_ok, runtime_ok, approval) is already satisfied, so the existing receipt can be reused."}
```
```

## 一次字段反馈

全实验按预定字典序最多24例，不按论文均分。反馈只指出未通过字段，不补新事实或正确答案。该环节是一次回答修订，不是运行恢复。

| 主调用ID | 反馈可用 | 纠正 | 响应 |
|---|---|---|---|
| P2-haiku-C01-W2-v0 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C01-W2-v0.json) |
| P2-haiku-C01-W2-v1 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C01-W2-v1.json) |
| P2-haiku-C02-W2-v0 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C02-W2-v0.json) |
| P2-haiku-C03-W2-v0 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C03-W2-v0.json) |
| P2-haiku-C03-W2-v1 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C03-W2-v1.json) |
| P2-haiku-C04-W2-v0 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C04-W2-v0.json) |
| P2-haiku-C04-W2-v1 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C04-W2-v1.json) |
| P2-haiku-C05-W2-v0 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C05-W2-v0.json) |
| P2-haiku-C05-W2-v1 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C05-W2-v1.json) |
| P2-haiku-C06-W2-v0 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C06-W2-v0.json) |
| P2-haiku-C06-W2-v1 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C06-W2-v1.json) |
| P2-haiku-C07-W2-v0 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C07-W2-v0.json) |
| P2-haiku-C07-W2-v1 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C07-W2-v1.json) |
| P2-haiku-C08-W2-v0 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C08-W2-v0.json) |
| P2-haiku-C08-W2-v1 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C08-W2-v1.json) |
| P2-haiku-C01-W8-v0 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C01-W8-v0.json) |
| P2-haiku-C02-W8-v0 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C02-W8-v0.json) |
| P2-haiku-C03-W8-v0 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C03-W8-v0.json) |
| P2-haiku-C03-W8-v1 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C03-W8-v1.json) |
| P2-haiku-C04-W8-v0 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C04-W8-v0.json) |
| P2-haiku-C04-W8-v1 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C04-W8-v1.json) |
| P2-haiku-C05-W8-v0 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C05-W8-v0.json) |
| P2-haiku-C05-W8-v1 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C05-W8-v1.json) |
| P2-haiku-C07-W8-v1 | True | False | [记录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/feedback/P2-haiku-C07-W8-v1.json) |

## 结构性反例

[十二对投影反例](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/analysis/structural-witnesses.json)中的本篇四对是分析构造，0次模型观测。相同受限视图对应不同合法处置，说明视图不足；不证明生产输入曾丢失这些信息。

## 事后材料修复

本篇64次新调用，两模型各32/32。修复了模型实际可见的状态、合同或动作区分，未改写原768次结果。所有输入、差分及原始输出见[修复设计](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/validity-repair/design.json)与[输入差分](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/validity-repair/input-diffs.diff)。这一修复不是长期流程或架构干预。
