# 本轮机制分析与主要结果

### 4.7 图视图未改变候选和合同选择，原动作评分需排除分类歧义

256次请求全部返回，原严格得分两模型均96/128。候选与合同字段却均128/128，全部64次不匹配集中在D5与D8的动作标签。复核发现，D5已说明新范围写入合同，却要求revise_scope，模型均选择补验证；D8已有有效回执但未说明是否挂接，模型均选择attach_evidence，并明确旧合同不会恢复。两者都是原材料与唯一答案之间的有效性问题，不能计作DAG或模型的错误。

排除D5/D8所有条件后，两模型在六个情境中均96/96；累计／活动视图、产物联表／分表与合同联表／分表的配对差均为0，每因素48对。局部修复、新检查、补回执、合理等待、恢复缺失能力与同树复用均获得正确处置。这些是明确协议下的正面证据，反对将图历史或关系分表本身预设为必然致错；短题天花板仍不能证明真实长期策略等价。

**表5。图视图实验的原评分与材料审计**

| 分析层 | Sonnet 5.5 | Haiku 5.5 | 解释 |
|---|---:|---:|---|
| 原严格评分 | 96/128 | 96/128 | D5/D8动作分类有歧义 |
| 原候选、合同均正确 | 128/128 | 128/128 | 不能由动作标签推断产物或范围混淆 |
| 排除D5/D8全部条件 | 96/96 | 96/96 | 六情境；三因素配对差均为0 |
| 材料修复后新调用 | 32/32 | 32/32 | 事后冻结；与原分母分开 |

为检验材料解释，另固定64次新调用：D5改为范围已获授权但尚未记入合同，D8补明回执已挂接但判定尚未登记，并区分动作含义。两模型分别32/32与32/32。它只说明明确待办状态后的分类行为，不能给原歧义题补上并不存在的初始信息，也不构成DAG演化提高交付率的证据。

![图6：图视图、关系呈现与有效性审计](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p4-dag-evolution/figures/p4-fig6-controlled-mechanisms.png)

**图6。**上左显示排除D5/D8后的全条件严格得分（每格12次），上右保留八情境原评分并标注缺陷题；下部是每因素48对的比较和另行材料修复结果。D5/D8的零分代表冻结答案标签不匹配，不能作为图或产物失败。没有执行真实图编辑、代码修复或静态／动态策略对照。



> **有效性审计更新：**原评分不覆盖。[材料审计](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/analysis/validity-audit.json)排除P2 W2/W8、P4 D5/D8的负面归因；13次格式不合规和6次引用用途歧义单列。事后修复128次全部匹配，详见[修复协议](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/validity-repair/protocol.md)和[结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/validity-repair/summary.json)。不能把原评分低分解释为模型架构失败。

# P4：受控机制实验完整结果

本附录是768次主调用中P4的256次；8固定情境×8条件×2顺序版本×2模型。不是256个独立任务。全部研究尚未人工复核；模型解释不是内部机制真值。主结果包含格式失败，后续纠正不回写。

## 协议、输入和运行

[事前协议](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/protocol.md) · [冻结哈希](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/design-lock.json) · [完整输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/design.json) · [方法实施边界](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/method-details.md) · [原始目录与复算](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/README.md)。模型使用固定Sonnet/Haiku 5.5、low effort，每调用fresh process，关闭工具、MCP与持久会话。当前说明一致，记录作用域明确；平台不可见上下文仍不能完整审计。

## 模型分层结果

| 模型 | 返回/计划 | 严格符合协议 | 格式失败 | 核心字段正确 | 直接解决（仅P3） |
|---|---|---|---|---|---|
| claude-sonnet-5-5 | 128/128 | 96 | 0 | 96 | 不适用 |
| claude-haiku-5-5 | 128/128 | 96 | 0 | 96 | 不适用 |

正确是动作、主体/候选、对象与合同等冻结字段同时匹配；P3还要求当前且获准的证据引用。JSON失败不作为语义错误或责任混淆。P3直接解决表示材料充分并符合协议，拒绝、请求已知缺少的授权或向有权者返回结果均可能合格；不表示产品已交付。

## 全条件

| 条件 | 因素 | Sonnet符合/16 | Haiku符合/16 |
|---|---|---|---|
| C01 | view=cumulative, artifacts=bound, contracts=bound | 12 | 12 |
| C02 | view=cumulative, artifacts=bound, contracts=distributed | 12 | 12 |
| C03 | view=cumulative, artifacts=distributed, contracts=bound | 12 | 12 |
| C04 | view=cumulative, artifacts=distributed, contracts=distributed | 12 | 12 |
| C05 | view=active, artifacts=bound, contracts=bound | 12 | 12 |
| C06 | view=active, artifacts=bound, contracts=distributed | 12 | 12 |
| C07 | view=active, artifacts=distributed, contracts=bound | 12 | 12 |
| C08 | view=active, artifacts=distributed, contracts=distributed | 12 | 12 |

## 全情境

| 情境 | Sonnet符合/16 | Haiku符合/16 |
|---|---|---|
| D1 | 16 | 16 |
| D2 | 16 | 16 |
| D3 | 16 | 16 |
| D4 | 16 | 16 |
| D5 | 0 | 0 |
| D6 | 16 | 16 |
| D7 | 16 | 16 |
| D8 | 0 | 0 |

## 全部配对差

差值为A减B；每一对固定情境、顺序版本和其它因素。报告有限固定设计的描述性差值，不计算总体显著性、等效性或生产失效率。

| 模型 | 指标 | A − B | 配对数 | 差（百分点） | 仅A正确 | 仅B正确 |
|---|---|---|---|---|---|---|
| claude-sonnet-5-5 | correct | cumulative − active | 64 | +0.000 | 0 | 0 |
| claude-sonnet-5-5 | correct | bound − distributed | 64 | +0.000 | 0 | 0 |
| claude-sonnet-5-5 | correct | bound − distributed | 64 | +0.000 | 0 | 0 |
| claude-haiku-5-5 | correct | cumulative − active | 64 | +0.000 | 0 | 0 |
| claude-haiku-5-5 | correct | bound − distributed | 64 | +0.000 | 0 | 0 |
| claude-haiku-5-5 | correct | bound − distributed | 64 | +0.000 | 0 | 0 |

## 全部未匹配冻结评分的输出

按ID排序完整列出，没有只保留符合预期的反例。truth是冻结评分答案，缺陷题不具有可识别的唯一答案；reason是模型可见文字，不作为因果解释。

### P4-haiku-C01-D5-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract requires F-base and F-check, and the current candidate provides both. The only prior PASS was under the superseded contract-old-35, which covered F-base alone and is not reusable under the new contract. No existing valid receipt covers contract-active-35, and no work item covers the new requirement, so a new verification task is needed."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C01-D5-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C01-D5-v0.json)

### P4-haiku-C01-D5-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract adds F-check for the newly authorized device class, and no work item covers it. The prior PASS covered only the original device class, and no valid receipt exists for the new requirement. The current candidate already retains F-base and F-check, so it does not need restoration. A new verification task is required."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C01-D5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C01-D5-v1.json)

### P4-haiku-C01-D8-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"The current candidate (candidate-new-38) already has a PASS receipt against the active contract (contract-active-38), so the existing valid receipt can be attached without rerunning the check. The withdrawn contract-old-38 verdict is not restored by this receipt and is not used."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C01-D8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C01-D8-v0.json)

### P4-haiku-C01-D8-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"The existing PASS receipt matches the current candidate and the active replacement contract, so it can be attached without rerunning. The withdrawn contract-old-38 verdict is not restored, and no tree reuse is needed."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C01-D8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C01-D8-v1.json)

### P4-haiku-C02-D5-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract scope-1 requires F-base and F-check, and candidate-new-35 carries both. The prior PASS covered only scope-0 (F-base), and no work item covers the new requirement, so a new verification task is needed. Neither the old PASS nor the closed historical graph can be reused."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C02-D5-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C02-D5-v0.json)

### P4-haiku-C02-D5-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract scope-1 adds F-check, and current candidate-new-35 retains F-base and F-check. The prior PASS covered only scope-0 (F-base), and no existing work item verifies F-check, so a new verification task is required."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C02-D5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C02-D5-v1.json)

### P4-haiku-C02-D8-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"The existing PASS receipt covers the current candidate (candidate-new-38) and the active contract (contract-active-38, scope-1, which requires F-base and F-check, both retained by the candidate). The withdrawn old contract's verdict does not need restoring, so the valid receipt can be attached without rerunning the check."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C02-D8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C02-D8-v0.json)

### P4-haiku-C02-D8-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"The existing receipt already reports PASS for the current candidate and the active replacement contract, so it can be attached without rerunning. Candidate-new-38's features (F-base, F-check) match scope-1 under the same environment (env-1). The withdrawn contract-old-38 verdict is not revived by this PASS and needs no new receipt."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C02-D8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C02-D8-v1.json)

### P4-haiku-C03-D5-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract adds F-check, and candidate-new-35 (tree-new-4) already contains F-base and F-check. The prior PASS covered only the superseded F-base contract, and no work item verifies F-check, so a new verification task is needed. Task-4-2 is blocked and does not cover the new requirement."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C03-D5-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C03-D5-v0.json)

### P4-haiku-C03-D5-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
```json
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The current candidate tree-new-4 already carries F-base and F-check, so no restore or repair is indicated. The prior PASS covered only the original device class, and the newly authorized device-class requirement has no work item, so a new verification task is needed for candidate-new-35 under the active contract."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C03-D5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C03-D5-v1.json)

### P4-haiku-C03-D8-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"The current candidate (candidate-new-38, tree-new-7) already has a PASS receipt under the active replacement contract (contract-active-38). Its features (F-base, F-check) match the contract's required features, so the existing valid receipt can be attached without rerunning the check. The withdrawn contract-old-38 verdict is not restored and is not needed."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C03-D8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C03-D8-v0.json)

### P4-haiku-C03-D8-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"A valid PASS receipt exists for the current candidate and active replacement contract, so it can be attached without rerunning. The candidate's tree (tree-new-7) carries F-base and F-check, which match the active contract's features. The withdrawn contract-old-38 verdict is not used."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C03-D8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C03-D8-v1.json)

### P4-haiku-C04-D5-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract's scope-1 requires F-base and F-check, and candidate-new-35 (tree-new-4) has both. The prior PASS covered only scope-0, and the graph has no work item for the new requirement. A new authorized independent verification task is needed."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C04-D5-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C04-D5-v0.json)

### P4-haiku-C04-D5-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
```json
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract (scope-1, env-1, F-base and F-check) is not yet covered by any PASS. The prior PASS was for superseded contract-old-35 (scope-0) and cannot be reused. The current candidate's tree already includes F-check, so no repair or restore is needed. Authorized new verification is required, and no work item covers it yet."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C04-D5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C04-D5-v1.json)

### P4-haiku-C04-D8-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"The existing PASS receipt covers the current candidate and active replacement contract. Its tree (tree-new-7) includes F-base and F-check, which matches the active scope-1 requirements. The withdrawn contract-old-38 verdict is not restored, and no new verification is needed."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C04-D8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C04-D8-v0.json)

### P4-haiku-C04-D8-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"The receipt already reports PASS for the current candidate and the active replacement contract, so the existing valid receipt can be attached without rerunning the check. The withdrawn contract-old-38 verdict is not restored by this PASS and needs no new verification."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C04-D8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C04-D8-v1.json)

### P4-haiku-C05-D5-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract requires F-check, and the prior PASS covered only the superseded F-base contract. No work item covers F-check, and no valid receipt exists for it under contract-active-35, so a new verification task is needed for the current candidate."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C05-D5-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C05-D5-v0.json)

### P4-haiku-C05-D5-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract adds a device-class requirement that the prior PASS did not cover, and no work item covers it. The current candidate already carries F-base and F-check, so no restore or reuse is needed. A new authorized verification task is required for the new device class."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C05-D5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C05-D5-v1.json)

### P4-haiku-C05-D8-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"The existing PASS receipt covers the current candidate and the active replacement contract, so it can be attached without rerunning the check. The withdrawn contract-old-38 verdict is not restored by this receipt, and no new verification is needed."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C05-D8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C05-D8-v0.json)

### P4-haiku-C05-D8-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"The existing receipt is a PASS for the current candidate and active replacement contract, so it can be attached without rerunning the check. The withdrawn contract-old-38 verdict is not restored by this PASS, and no new verification is needed."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C05-D8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C05-D8-v1.json)

### P4-haiku-C06-D5-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract (scope-1) adds F-check, and candidate-new-35 already carries F-base and F-check in the same env-1. No work item covers the new requirement, and the prior PASS was for scope-0 only, so it cannot be reused. A new verification task is needed for the current candidate under the active contract."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C06-D5-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C06-D5-v0.json)

### P4-haiku-C06-D5-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
```json
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract scope-1 requires F-base and F-check, but the prior PASS covered only scope-0 (F-base). No work item covers the new F-check requirement, and task-4-2 is blocked. The current candidate already includes F-check, so it needs a new verification task under the active contract. The old PASS cannot be reused or attached."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C06-D5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C06-D5-v1.json)

### P4-haiku-C06-D8-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"The existing receipt reports PASS for the current candidate and the active replacement contract, so it can be attached without rerunning the check. The withdrawn contract-old-38 verdict is not restored by this."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C06-D8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C06-D8-v0.json)

### P4-haiku-C06-D8-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"The existing PASS receipt already covers the current candidate and the active replacement contract, so it can be attached without rerunning the check. The withdrawn contract-old-38 verdict is not restored by this."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C06-D8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C06-D8-v1.json)

### P4-haiku-C07-D5-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract requires F-check, which candidate-new-35 (tree-new-4) already contains, so no repair or restore is needed. The only prior PASS was under superseded contract-old-35 (F-base only), and no valid receipt exists for the active contract. No work item covers the new requirement, so a new verification task is needed."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C07-D5-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C07-D5-v0.json)

### P4-haiku-C07-D5-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
```json
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract now requires F-check, and the prior PASS covered only the original device class. No work item covers the new device-class requirement, and no valid receipt for it exists. candidate-new-35 already has F-base and F-check in tree-new-4, so no restore or repair is needed. A new authorized verification task is required."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C07-D5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C07-D5-v1.json)

### P4-haiku-C07-D8-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
```json
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"A PASS receipt already covers the current candidate and active replacement contract. Attach it without rerunning the check. The withdrawn contract-old-38 verdict is not restored by this receipt."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C07-D8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C07-D8-v0.json)

### P4-haiku-C07-D8-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"A valid PASS receipt already exists for the current candidate and the active replacement contract, so it can be attached without rerunning the check. The withdrawn contract-old-38 verdict is not restored by this PASS."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C07-D8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C07-D8-v1.json)

### P4-haiku-C08-D5-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
```json
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The current candidate's tree (tree-new-4) already contains both F-base and F-check, which the active scope-1 requires. The prior PASS was for the superseded scope-0 contract, so it cannot be reused. No work item covers the new device-class requirement, so a new verification task is needed. Task-4-2 is blocked and the records do not show it covers this requirement."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C08-D5-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C08-D5-v0.json)

### P4-haiku-C08-D5-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract (scope-1, env-1) requires F-base and F-check. The only PASS covers the superseded scope-0 contract, which had only F-base, so it cannot satisfy the new requirement. No verification exists for the active contract, and no work item covers F-check. The current candidate candidate-new-35 (sha-new-4, tree-new-4) already includes F-check, so it needs a new independent verification task. Reusing tree-base-4 is not valid because the contract changed."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C08-D5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C08-D5-v1.json)

### P4-haiku-C08-D8-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"The current candidate (candidate-new-38) already has a PASS receipt for the active replacement contract (contract-active-38). Its tree (tree-new-7) carries features F-base and F-check, matching scope-1 in env-1, so the receipt can be attached without rerunning. The withdrawn contract-old-38 verdict is not restored by this."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C08-D8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C08-D8-v0.json)

### P4-haiku-C08-D8-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"An existing PASS receipt covers the current candidate and the active replacement contract, so it can be attached without rerunning the check. The withdrawn contract-old-38 verdict is not restored by this."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-haiku-C08-D8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-haiku-C08-D8-v1.json)

### P4-sonnet-C01-D5-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The current candidate keeps both F-base and F-check, so no restore or repair is needed. The active contract now requires the new device class, and the earlier PASS covered only the original class. No existing work item covers the new requirement, and the blocked task-4-2 is not assigned to it. A new independent verification task is therefore needed. Closed history has no bearing on this."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C01-D5-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C01-D5-v0.json)

### P4-sonnet-C01-D5-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract now requires F-check, and the earlier PASS covered only the original device class under the superseded contract. No work item covers the new requirement, so a new verification task is needed against the current candidate. The candidate already has F-check, so no repair or restore is needed. Closed history has no active dependencies."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C01-D5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C01-D5-v1.json)

### P4-sonnet-C01-D8-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"A valid PASS receipt already exists for the current candidate and active contract, so it can be attached without rerunning. The withdrawn contract's verdict is not restored by it, and there is no need to record it. The candidate retains F-base and F-check, so no repair or restore is needed. Closed history has no active dependencies."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C01-D8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C01-D8-v0.json)

### P4-sonnet-C01-D8-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"The existing receipt reports PASS for the current candidate and the active replacement contract, so it can be attached without rerunning the check. The withdrawn old contract's verdict is not restored and needs no action. The blocked task-7-2 is not tied to any missing prerequisite in the records, and the closed history has no current execution dependency."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C01-D8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C01-D8-v1.json)

### P4-sonnet-C02-D5-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract (scope-1) now requires F-check for the new device class, and the earlier PASS covered only scope-0. The current candidate retains both features, so no repair or restore is needed. No work item covers the new requirement, so a new verification task is needed. The archived history has no active dependencies."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C02-D5-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C02-D5-v0.json)

### P4-sonnet-C02-D5-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract now requires F-check, and the current candidate has both F-base and F-check. The earlier PASS covered only the original scope, and no work item covers the new requirement, so a new verification task is needed. The blocked task-4-2 is not tied to this requirement."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C02-D5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C02-D5-v1.json)

### P4-sonnet-C02-D8-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"A valid PASS receipt already exists for the current candidate and the active replacement contract, so it can be attached without rerunning the check. The withdrawn contract's verdict is not restored and needs no action. Closed history has no active dependencies."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C02-D8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C02-D8-v0.json)

### P4-sonnet-C02-D8-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"A valid PASS receipt already exists for the current candidate and the active replacement contract, so it can be attached without rerunning. The withdrawn old contract's verdict is not restored, and closed history has no current execution dependency."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C02-D8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C02-D8-v1.json)

### P4-sonnet-C03-D5-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The current candidate's tree has F-base and F-check, so it meets the active contract's features and nothing needs repair or restoring. The earlier PASS covered only the original device class, and no work item covers the new requirement, so a new verification task is needed. Closed history has no active dependencies."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C03-D5-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C03-D5-v0.json)

### P4-sonnet-C03-D5-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The current candidate's tree has both F-base and F-check, so no repair or restore is needed. The active contract adds the new device class requirement, and the prior PASS covered only the original class. No existing work item or receipt covers the new requirement, so it needs a new verification task. Archived history has no active dependencies."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C03-D5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C03-D5-v1.json)

### P4-sonnet-C03-D8-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"The existing receipt reports PASS for the current candidate and the active replacement contract, so it can be attached without rerunning the check. The withdrawn contract-old-38 verdict is not needed or restored. The graph's blocked task-7-2 does not change this, since no new verification, repair or wait is required."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C03-D8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C03-D8-v0.json)

### P4-sonnet-C03-D8-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"A valid PASS receipt already exists for the current candidate and active replacement contract, so it can be attached without rerunning. The withdrawn contract's verdict is not restored and is not needed. Closed history has no active dependencies, and the blocked task-7-2 does not call for repair, new verification, or waiting."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C03-D8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C03-D8-v1.json)

### P4-sonnet-C04-D5-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The current candidate already has F-base and F-check, so no repair or restore is needed. The active contract's scope-1 adds the new device class, and the earlier PASS covered only scope-0. No work item covers the new requirement, so it needs a new verification task. Closed history has no active dependencies."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C04-D5-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C04-D5-v0.json)

### P4-sonnet-C04-D5-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract now requires F-check, and the current candidate's tree has both F-base and F-check. The earlier PASS covered only the original scope (scope-0), and no work item covers the new requirement, so a new verification task is needed against the current candidate and active contract."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C04-D5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C04-D5-v1.json)

### P4-sonnet-C04-D8-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"A valid PASS receipt already exists for the current candidate and active contract, so it can be attached without rerunning. The withdrawn contract's verdict is not restored by it, and the new candidate retains F-base and F-check, so no repair or restore is needed. The blocked task-7-2 is not tied to any requirement in the records, and closed history has no active dependencies."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C04-D8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C04-D8-v0.json)

### P4-sonnet-C04-D8-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"A valid PASS receipt already exists for the current candidate and the active replacement contract, so it can be attached without rerunning the check. The old contract's withdrawn verdict is not restored and needs no action. Closed history has no current execution dependency."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C04-D8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C04-D8-v1.json)

### P4-sonnet-C05-D5-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The owner-authorized device class is already in the active contract, but the prior PASS covered only the original class and no work item covers the new requirement. The current candidate keeps F-base and F-check, so no repair or restore is needed. The new requirement needs its own verification task against the current candidate."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C05-D5-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C05-D5-v0.json)

### P4-sonnet-C05-D5-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract now includes F-check for the new device class, and candidate-new-35 retains F-base and F-check. The earlier PASS covered only the original device class, and no work item covers the new requirement. A new verification task is needed against the current candidate and active contract."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C05-D5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C05-D5-v1.json)

### P4-sonnet-C05-D8-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"A valid PASS receipt already exists for the current candidate and active contract, so it can be attached without rerunning. The withdrawn contract's verdict is not restored, and the blocked task-7-2 is not an external prerequisite named in the records. Candidate retains F-base and F-check, so no repair or restoration is needed."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C05-D8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C05-D8-v0.json)

### P4-sonnet-C05-D8-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"A valid PASS receipt already exists for the current candidate and active contract, so it can be attached without rerunning. The withdrawn old contract's verdict isn't restored and needs no action; the blocked node is addressed by attaching the receipt."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C05-D8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C05-D8-v1.json)

### P4-sonnet-C06-D5-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The current candidate already retains F-base and F-check, so no repair or restore is needed. The active contract scope-1 adds the new device class requirement, and the earlier PASS covered only scope-0. No work item covers the new requirement, so a new verification task is needed."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C06-D5-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C06-D5-v0.json)

### P4-sonnet-C06-D5-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract now requires F-check, and the earlier PASS covered only scope-0 (F-base). No work item covers the new requirement. The current candidate already has both features, so it needs no repair or restore. A new verification task is needed against the active contract."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C06-D5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C06-D5-v1.json)

### P4-sonnet-C06-D8-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"A valid PASS receipt already exists for the current candidate and active replacement contract, so it can be attached without rerunning. The withdrawn old contract is not restored by this PASS and needs no action. Both candidates retain F-check, and closed history has no active dependency."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C06-D8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C06-D8-v0.json)

### P4-sonnet-C06-D8-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"A valid PASS receipt already exists for the current candidate and active contract, so it can be attached without rerunning. The withdrawn old contract is not relevant, and the candidate retains both required features."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C06-D8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C06-D8-v1.json)

### P4-sonnet-C07-D5-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract now requires F-check, and the current candidate's tree has it. The prior PASS covered only the original device class, and no work item covers the new requirement. A new verification task is needed for the authorized scope."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C07-D5-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C07-D5-v0.json)

### P4-sonnet-C07-D5-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract now requires F-check, and the prior PASS covered only the original device class. No work item covers the new requirement. The current candidate's tree already has F-base and F-check, so no restore or repair is needed. The new authorized scope needs a new independent verification task on candidate-new-35."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C07-D5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C07-D5-v1.json)

### P4-sonnet-C07-D8-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"An existing valid PASS receipt covers the current candidate and the active replacement contract, so it can be attached without rerunning the check. The withdrawn contract-old-38 verdict is not restored and needs no action. The candidate keeps both F-base and F-check, so no repair or restore is needed."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C07-D8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C07-D8-v0.json)

### P4-sonnet-C07-D8-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"A valid PASS receipt already exists for the current candidate and the active replacement contract, so it can be attached without rerunning the check. The withdrawn contract's verdict is not restored, and closed history creates no execution dependency."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C07-D8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C07-D8-v1.json)

### P4-sonnet-C08-D5-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The current candidate's tree has both F-base and F-check, so no artifact repair or restore is needed. The active contract's scope-1 adds F-check, which the earlier PASS did not cover, and no work item covers it. The new authorized scope needs its own verification task."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C08-D5-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C08-D5-v0.json)

### P4-sonnet-C08-D5-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "revise_scope",
  "candidate": "candidate-new-35",
  "contract": "contract-active-35"
}
```

```text
{"action":"add_verification","candidate":"candidate-new-35","contract":"contract-active-35","reason":"The active contract now requires F-check, and the current candidate's tree includes both F-base and F-check, so no artifact repair or restoration is needed. The earlier PASS covered only the original scope-0, and no work item covers F-check. Independent verification of the new requirement needs a new verification task against candidate-new-35 under contract-active-35."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C08-D5-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C08-D5-v1.json)

### P4-sonnet-C08-D8-v0

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"A valid PASS receipt already exists for the current candidate and active replacement contract, so it can be attached without rerunning. The withdrawn contract's verdict is not restored by it and needs no action. The candidate retains F-base and F-check, so no repair or restore is needed. Closed history has no active dependencies."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C08-D8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C08-D8-v0.json)

### P4-sonnet-C08-D8-v1

格式有效：True；不匹配字段：action。

```json
{
  "action": "record_scoped_result",
  "candidate": "candidate-new-38",
  "contract": "contract-active-38"
}
```

```text
{"action":"attach_evidence","candidate":"candidate-new-38","contract":"contract-active-38","reason":"A valid PASS receipt already exists for the current candidate and active contract, so it can be attached without rerunning. The withdrawn old contract is not restored by it and is not relevant to the active scope. The candidate retains F-base and F-check, so no repair or restore is needed. Closed history has no active dependencies."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P4-sonnet-C08-D8-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P4-sonnet-C08-D8-v1.json)

## 规则选取的正例

每模型每情境取字典序第一个匹配评分的ID；没有匹配输出的情境不补选。全实验共43条，选择规则早于结果读取。这些不是独立重复实验。

### P4-sonnet-C01-D1-v0

```text
{"action":"repair_existing","candidate":"candidate-new-31","contract":"contract-active-31","reason":"The rendering check returned a mismatched frame, which is a local defect in the current candidate. The candidate retains both F-base and F-check, the contract is unchanged, and implementation authorization is still active, so the defect can be repaired in place. All verification responsibilities already have tasks, so no new verification task is needed. Closed graph history has no active dependencies and does not affect this."}
```

### P4-sonnet-C01-D2-v0

```text
{"action":"add_verification","candidate":"candidate-new-32","contract":"contract-active-32","reason":"The owner authorized a new independent cross-project check that no existing task covers, so it needs a new verification task against the current candidate and active contract. Existing checks found no defect, so no repair is needed. No valid receipt exists to attach. The candidate retains F-base and F-check, so no restore is needed. Closed history has no active dependencies."}
```

### P4-sonnet-C01-D3-v0

```text
{"action":"attach_evidence","candidate":"candidate-new-33","contract":"contract-active-33","reason":"A valid receipt for the current candidate, environment and active contract already exists in the ledger. The bundle only lacks the attachment, so attach it without rerunning the check. The new candidate retains F-base and F-check, so no restore or repair is needed. Closed history has no active dependencies."}
```

### P4-sonnet-C01-D4-v0

```text
{"action":"wait_external","candidate":"candidate-new-34","contract":"contract-active-34","reason":"All local work is done. The remaining check needs a real external environment, and access request R-access is still pending. The owner prohibited simulated substitutes, so the only valid step is to wait. The current candidate keeps F-base and F-check, so no restore or repair is needed. Closed history has no bearing on this."}
```

### P4-sonnet-C01-D6-v0

```text
{"action":"restore_artifact","candidate":"candidate-old-36","contract":"contract-active-36","reason":"The current candidate candidate-new-36 lost required feature F-base, which candidate-old-36 retains (F-base and F-check). The protocol says to restore the retaining predecessor as the repair base. The blocked task and the closed history do not change this, and no new scope was authorized."}
```

### P4-sonnet-C01-D7-v0

```text
{"action":"reuse_candidate","candidate":"candidate-new-37","contract":"contract-active-37","reason":"The new candidate has the same verified full tree (tree-base-6) and features (F-base, F-check) as its predecessor. The predecessor's receipt was issued under the same active contract and environment, so it can be reused without rerunning the check. The closed graph history has no active dependencies."}
```

### P4-haiku-C01-D1-v0

```text
{"action":"repair_existing","candidate":"candidate-new-31","contract":"contract-active-31","reason":"The rendering check mismatch is a local defect in the current candidate, which retains both F-base and F-check required by the unchanged active contract. Implementation authorization remains active, so the defect can be repaired in place. No new verification, lost feature restoration, or scope revision is indicated by the records."}
```

### P4-haiku-C01-D2-v0

```text
{"action":"add_verification","candidate":"candidate-new-32","contract":"contract-active-32","reason":"The owner authorized a new independent cross-project check, and no existing graph task covers it. Existing checks found no defect, so no repair is needed. No valid receipt exists for this obligation under the active contract, so a new verification task is required against the current candidate."}
```

### P4-haiku-C01-D3-v0

```text
{"action":"attach_evidence","candidate":"candidate-new-33","contract":"contract-active-33","reason":"The bundle is missing only its receipt attachment. The ledger holds a valid receipt for the current candidate, environment (env-1), and active contract, and all required tests are complete. Attaching the existing receipt does not require rerunning the check."}
```

### P4-haiku-C01-D4-v0

```text
```json
{"action":"wait_external","candidate":"candidate-new-34","contract":"contract-active-34","reason":"candidate-new-34 is the current candidate and contract-active-34 is the active contract, and candidate-new-34 retains both required features (F-base, F-check). The outstanding check needs a real external environment, which is pending under R-access. No valid receipt for the active contract is recorded, and simulated substitutes are prohibited, so the check must wait."}
```
```

### P4-haiku-C01-D6-v0

```text
{"action":"restore_artifact","candidate":"candidate-old-36","contract":"contract-active-36","reason":"candidate-new-36 failed the feature-presence audit because it dropped the required retained feature F-base. Its parent, candidate-old-36, retains F-base and F-check, so it is the restore base under the active contract."}
```

### P4-haiku-C01-D7-v0

```text
{"action":"reuse_candidate","candidate":"candidate-new-37","contract":"contract-active-37","reason":"candidate-new-37 (sha-new-6) has the same full tree as its predecessor candidate-old-37 (tree-base-6). Both list features F-base and F-check, and the active contract and environment (env-1) are unchanged, so the predecessor's receipt applies."}
```

## 一次字段反馈

全实验按预定字典序最多24例，不按论文均分。反馈只指出未通过字段，不补新事实或正确答案。该环节是一次回答修订，不是运行恢复。

| 主调用ID | 反馈可用 | 纠正 | 响应 |
|---|---|---|---|

## 结构性反例

[十二对投影反例](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/analysis/structural-witnesses.json)中的本篇四对是分析构造，0次模型观测。相同受限视图对应不同合法处置，说明视图不足；不证明生产输入曾丢失这些信息。

## 事后材料修复

本篇64次新调用，两模型各32/32。修复了模型实际可见的状态、合同或动作区分，未改写原768次结果。所有输入、差分及原始输出见[修复设计](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/validity-repair/design.json)与[输入差分](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/validity-repair/input-diffs.diff)。这一修复不是长期流程或架构干预。
