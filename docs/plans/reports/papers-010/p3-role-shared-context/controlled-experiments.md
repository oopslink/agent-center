# 本轮机制分析与主要结果

### 4.8 名字未改变直接处置，信息分区决定了哪些情境可以判定

两模型均完成128次主响应。四策略能够直接形成处置的比例首先由材料覆盖决定：个人历史3/8情境、同生产者角色2/8、全部与精确边界视图均8/8。两模型的直接解决分别为12/32、8/32、32/32、32/32，恰好达到各策略的充分信息上限（表7）。这说明此次差异主要落在信息是否到达；不能把按规则挡在输入之外的事实算成模型没有理解。个人策略有一题只凭当前失败即可拒绝，所以充分判定3/8不同于两项事实齐全2/8。

**表7。材料覆盖、严格接口与直接处置分别计量**

| 选择策略 | 两项事实齐全／情境 | 足以处置／情境 | Sonnet严格／直接（各32次） | Haiku严格／直接（各32次） |
|---|---:|---:|---:|---:|
| 个人holder | 2/8 | 3/8 | 27／12 | 32／12 |
| 同producer_role | 2/8 | 2/8 | 25／8 | 30／8 |
| 全部获准记录 | 8/8 | 8/8 | 32／32 | 32／32 |
| 当前范围bounded | 8/8 | 8/8 | 32／32 | 32／32 |

proper减opaque的直接解决差，两模型均为0（每模型64对）。严格得分差则分别为+6.25和0个百分点，不能写成所有命名效果均为零。全部14次严格不匹配中，八次Sonnet输出带JSON前解释；其余六次的处置、候选和合同都正确，仅证据列表包含用来解释排除的note或旧记录。回答明确说明这些材料不能证明当前通过，故不能把列表不符解释为使用旧PASS放行。评分要求只列当前检查／授权证据，而“IDs used”的提示允许对排除理由有另一种理解，需保留这一接口歧义。

事后抽取格式失败中的唯一JSON后，两模型的核心处置字段均128/128；此诊断不改写严格得分116/128与126/128，也不把引文不符说成已通过原规则。没有观察到可解析输出在证据不足时直接接受候选。全部与bounded分别提供7条与3条记录，但两模型都32/32严格符合；该短题未检出精确过滤相对全部材料的正确性优势，亦未测真实检索召回和维护成本。

![图5：信息覆盖与模型处置分解](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p3-role-shared-context/figures/p3-fig5-controlled-context.png)

**图5。**左上是由选择规则决定的当前事实覆盖与充分性，右上分别显示严格符合和直接解决；下部为显示名与策略的配对差。策略差有意包含材料差，名字对照保持稳定ID、角色、权限和记录不变。role-only仅按材料生产者角色过滤，bounded未使用角色分类；蓝色的直接解决包含合格拒绝、授权请求和向有权者返回结果，不是产品交付率。



> **有效性审计更新：**原评分不覆盖。[材料审计](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/analysis/validity-audit.json)排除P2 W2/W8、P4 D5/D8的负面归因；13次格式不合规和6次引用用途歧义单列。事后修复128次全部匹配，详见[修复协议](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/validity-repair/protocol.md)和[结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/validity-repair/summary.json)。不能把原评分低分解释为模型架构失败。

# P3：受控机制实验完整结果

本附录是768次主调用中P3的256次；8固定情境×8条件×2顺序版本×2模型。不是256个独立任务。全部研究尚未人工复核；模型解释不是内部机制真值。主结果包含格式失败，后续纠正不回写。

## 协议、输入和运行

[事前协议](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/protocol.md) · [冻结哈希](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/design-lock.json) · [完整输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/design.json) · [方法实施边界](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/method-details.md) · [原始目录与复算](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/README.md)。模型使用固定Sonnet/Haiku 5.5、low effort，每调用fresh process，关闭工具、MCP与持久会话。当前说明一致，记录作用域明确；平台不可见上下文仍不能完整审计。

## 模型分层结果

| 模型 | 返回/计划 | 严格符合协议 | 格式失败 | 核心字段正确 | 直接解决（仅P3） |
|---|---|---|---|---|---|
| claude-sonnet-5-5 | 128/128 | 116 | 8 | 120 | 84 |
| claude-haiku-5-5 | 128/128 | 126 | 0 | 128 | 84 |

正确是动作、主体/候选、对象与合同等冻结字段同时匹配；P3还要求当前且获准的证据引用。JSON失败不作为语义错误或责任混淆。P3直接解决表示材料充分并符合协议，拒绝、请求已知缺少的授权或向有权者返回结果均可能合格；不表示产品已交付。

## 全条件

| 条件 | 因素 | Sonnet符合/16 | Haiku符合/16 |
|---|---|---|---|
| C01 | names=proper, policy=personal | 15 | 16 |
| C02 | names=proper, policy=role | 13 | 15 |
| C03 | names=proper, policy=all | 16 | 16 |
| C04 | names=proper, policy=bounded | 16 | 16 |
| C05 | names=opaque, policy=personal | 12 | 16 |
| C06 | names=opaque, policy=role | 12 | 15 |
| C07 | names=opaque, policy=all | 16 | 16 |
| C08 | names=opaque, policy=bounded | 16 | 16 |

## 全情境

| 情境 | Sonnet符合/16 | Haiku符合/16 |
|---|---|---|
| N1 | 16 | 16 |
| N2 | 15 | 15 |
| N3 | 11 | 16 |
| N4 | 14 | 16 |
| N5 | 16 | 16 |
| N6 | 15 | 15 |
| N7 | 14 | 16 |
| N8 | 15 | 16 |

## 全部配对差

差值为A减B；每一对固定情境、顺序版本和其它因素。报告有限固定设计的描述性差值，不计算总体显著性、等效性或生产失效率。

| 模型 | 指标 | A − B | 配对数 | 差（百分点） | 仅A正确 | 仅B正确 |
|---|---|---|---|---|---|---|
| claude-sonnet-5-5 | correct | proper − opaque | 64 | +6.250 | 6 | 2 |
| claude-sonnet-5-5 | direct_resolution | proper − opaque | 64 | +0.000 | 0 | 0 |
| claude-sonnet-5-5 | correct | personal − role | 32 | +6.250 | 4 | 2 |
| claude-sonnet-5-5 | direct_resolution | personal − role | 32 | +12.500 | 4 | 0 |
| claude-sonnet-5-5 | correct | personal − all | 32 | -15.625 | 0 | 5 |
| claude-sonnet-5-5 | direct_resolution | personal − all | 32 | -62.500 | 0 | 20 |
| claude-sonnet-5-5 | correct | personal − bounded | 32 | -15.625 | 0 | 5 |
| claude-sonnet-5-5 | direct_resolution | personal − bounded | 32 | -62.500 | 0 | 20 |
| claude-sonnet-5-5 | correct | role − all | 32 | -21.875 | 0 | 7 |
| claude-sonnet-5-5 | direct_resolution | role − all | 32 | -75.000 | 0 | 24 |
| claude-sonnet-5-5 | correct | role − bounded | 32 | -21.875 | 0 | 7 |
| claude-sonnet-5-5 | direct_resolution | role − bounded | 32 | -75.000 | 0 | 24 |
| claude-sonnet-5-5 | correct | all − bounded | 32 | +0.000 | 0 | 0 |
| claude-sonnet-5-5 | direct_resolution | all − bounded | 32 | +0.000 | 0 | 0 |
| claude-haiku-5-5 | correct | proper − opaque | 64 | +0.000 | 1 | 1 |
| claude-haiku-5-5 | direct_resolution | proper − opaque | 64 | +0.000 | 0 | 0 |
| claude-haiku-5-5 | correct | personal − role | 32 | +6.250 | 2 | 0 |
| claude-haiku-5-5 | direct_resolution | personal − role | 32 | +12.500 | 4 | 0 |
| claude-haiku-5-5 | correct | personal − all | 32 | +0.000 | 0 | 0 |
| claude-haiku-5-5 | direct_resolution | personal − all | 32 | -62.500 | 0 | 20 |
| claude-haiku-5-5 | correct | personal − bounded | 32 | +0.000 | 0 | 0 |
| claude-haiku-5-5 | direct_resolution | personal − bounded | 32 | -62.500 | 0 | 20 |
| claude-haiku-5-5 | correct | role − all | 32 | -6.250 | 0 | 2 |
| claude-haiku-5-5 | direct_resolution | role − all | 32 | -75.000 | 0 | 24 |
| claude-haiku-5-5 | correct | role − bounded | 32 | -6.250 | 0 | 2 |
| claude-haiku-5-5 | direct_resolution | role − bounded | 32 | -75.000 | 0 | 24 |
| claude-haiku-5-5 | correct | all − bounded | 32 | +0.000 | 0 | 0 |
| claude-haiku-5-5 | direct_resolution | all − bounded | 32 | +0.000 | 0 | 0 |

## 材料覆盖与指标分解

role-only仅按producer_role过滤；不是所有按职责相关性共享的方案。bounded只按项目、候选、合同和版本过滤，未使用角色。覆盖是构造事实而非模型测量。个人/同角色策略能直接判定3/8与2/8情境；全部和精确边界策略均8/8。

| 策略 | 模型 | 核心字段/32 | 证据引用/32 | 严格符合/32 | 直接解决/32 | JSON有效但材料不足仍不请求context |
|---|---|---|---|---|---|---|
| personal | claude-sonnet-5-5 | 29 | 27 | 27 | 12 | 0 |
| personal | claude-haiku-5-5 | 32 | 32 | 32 | 12 | 0 |
| role | claude-sonnet-5-5 | 27 | 25 | 25 | 8 | 0 |
| role | claude-haiku-5-5 | 32 | 30 | 30 | 8 | 0 |
| all | claude-sonnet-5-5 | 32 | 32 | 32 | 32 | 0 |
| all | claude-haiku-5-5 | 32 | 32 | 32 | 32 | 0 |
| bounded | claude-sonnet-5-5 | 32 | 32 | 32 | 32 | 0 |
| bounded | claude-haiku-5-5 | 32 | 32 | 32 | 32 | 0 |

raw score的unsupported_decision在JSON失败时也为真，因此表中另用format_valid限定，不能把无法解析误算为无依据决策。原始评分字段保留，没有改写。

## 全部未匹配冻结评分的输出

按ID排序完整列出，没有只保留符合预期的反例。truth是冻结评分答案，缺陷题不具有可识别的唯一答案；reason是模型可见文字，不作为因果解释。

### P3-haiku-C02-N2-v1

格式有效：True；不匹配字段：evidence_ids。

```json
{
  "decision": "request_context",
  "candidate": "C-22",
  "contract": "K-22",
  "needed_evidence": [],
  "visible_ids": [
    "R1-old-check",
    "R1-old-approval",
    "R1-other-contract",
    "R1-other-project",
    "R1-note"
  ],
  "full_coverage": false,
  "sufficient": false,
  "context_records": 5
}
```

```text
{"decision":"request_context","candidate":"C-22","contract":"K-22","evidence_ids":["R1-note"],"reason":"No visible check_passed record matches C-22, K-22, P-22, version 2. R1-other-project and R1-other-contract are out of scope, and R1-old-check is version 1, so none can be used as current evidence. R1-note states that it is not a check. The check is unknown, so context is needed before deciding."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P3-haiku-C02-N2-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P3-haiku-C02-N2-v1.json)

### P3-haiku-C06-N6-v0

格式有效：True；不匹配字段：evidence_ids。

```json
{
  "decision": "request_context",
  "candidate": "C-26",
  "contract": "K-26",
  "needed_evidence": [],
  "visible_ids": [
    "R5-old-check",
    "R5-old-approval",
    "R5-other-contract",
    "R5-other-project",
    "R5-note"
  ],
  "full_coverage": false,
  "sufficient": false,
  "context_records": 5
}
```

```text
```json
{"decision":"request_context","candidate":"C-26","contract":"K-26","evidence_ids":["R5-note"],"reason":"No current check record exists for C-26/K-26/P-26 at version 2, so the check status is unknown. R5-old-check (version 1) is an old record and is not used as current evidence. R5-note is in scope but is not a check or approval. Approval status is also not established by a current record."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P3-haiku-C06-N6-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P3-haiku-C06-N6-v0.json)

### P3-sonnet-C01-N3-v1

格式有效：False；不匹配字段：decision, candidate, contract, evidence_ids。

```json
{
  "decision": "request_context",
  "candidate": "C-23",
  "contract": "K-23",
  "needed_evidence": [],
  "visible_ids": [
    "R2-old-check",
    "R2-old-approval",
    "R2-other-contract",
    "R2-other-project",
    "R2-note"
  ],
  "full_coverage": false,
  "sufficient": false,
  "context_records": 5
}
```

```text
Target is C-23, K-23, P-23, version 2. The only records matching that scope at version 2 are R2-note, which is explicitly not a check or approval. The other check records are from another project (R2-other-project), another contract (R2-other-contract), or an old version (R2-old-check). The old approval is also version 1 and false. So the current check status is unknown, not failed, and the right action is to request context.

```json
{"decision":"request_context","candidate":"C-23","contract":"K-23","evidence_ids":[],"reason":"No visible current-version (v2) check record for C-23/K-23/P-23. R2-note is not a check or approval; R2-other-project, R2-other-contract, and R2-old-check/R2-old-approval are out of scope or outdated. Check status is unknown, so more context is needed."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P3-sonnet-C01-N3-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P3-sonnet-C01-N3-v1.json)

### P3-sonnet-C02-N2-v0

格式有效：False；不匹配字段：decision, candidate, contract, evidence_ids。

```json
{
  "decision": "request_context",
  "candidate": "C-22",
  "contract": "K-22",
  "needed_evidence": [],
  "visible_ids": [
    "R1-old-check",
    "R1-old-approval",
    "R1-other-contract",
    "R1-other-project",
    "R1-note"
  ],
  "full_coverage": false,
  "sufficient": false,
  "context_records": 5
}
```

```text
Target is C-22, K-22, P-22, version 2. The only records matching the target's project and contract are version 1 (the old check and old approval) and a version 2 note. The old records are out of scope for the current version, and the note is neither a check nor an approval. The other-contract and other-project records don't count either. So there is no visible current check, and its status is unknown, which means request_context.

```json
{"decision":"request_context","candidate":"C-22","contract":"K-22","evidence_ids":[],"reason":"No current (version 2) check record for C-22 under K-22/P-22 is visible. The version 1 records are stale, the other-contract and other-project records are out of scope, and the note is not a check or approval. The check status is unknown, so more context is needed."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P3-sonnet-C02-N2-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P3-sonnet-C02-N2-v0.json)

### P3-sonnet-C02-N3-v1

格式有效：False；不匹配字段：decision, candidate, contract, evidence_ids。

```json
{
  "decision": "request_context",
  "candidate": "C-23",
  "contract": "K-23",
  "needed_evidence": [],
  "visible_ids": [
    "R2-old-check",
    "R2-old-approval",
    "R2-other-contract",
    "R2-other-project",
    "R2-note"
  ],
  "full_coverage": false,
  "sufficient": false,
  "context_records": 5
}
```

```text
Target is C-23, K-23, P-23, version 2. Visible records for the current target: only R2-note, which is not a check or approval. The other records are out of scope (other project, other contract, or old version 1). So the current check status is unknown, which means request_context.

{"decision":"request_context","candidate":"C-23","contract":"K-23","evidence_ids":["R2-note"],"reason":"No current (version 2, P-23, K-23) check or approval record is visible; R2-note is only a discussion note. Other records are from a different project, a different contract, or an old version, so the current check status is unknown."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P3-sonnet-C02-N3-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P3-sonnet-C02-N3-v1.json)

### P3-sonnet-C02-N4-v1

格式有效：True；不匹配字段：evidence_ids。

```json
{
  "decision": "request_context",
  "candidate": "C-24",
  "contract": "K-24",
  "needed_evidence": [],
  "visible_ids": [
    "R3-old-check",
    "R3-old-approval",
    "R3-other-contract",
    "R3-other-project",
    "R3-note"
  ],
  "full_coverage": false,
  "sufficient": false,
  "context_records": 5
}
```

```text
{"decision":"request_context","candidate":"C-24","contract":"K-24","evidence_ids":["R3-note"],"reason":"No visible current-version (2) check for C-24/K-24/P-24 exists. The passed checks are out of scope (other project, other contract) or old (version 1), and the note is neither a check nor approval. The check status is unknown, so more context is needed."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P3-sonnet-C02-N4-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P3-sonnet-C02-N4-v1.json)

### P3-sonnet-C05-N3-v0

格式有效：False；不匹配字段：decision, candidate, contract, evidence_ids。

```json
{
  "decision": "request_context",
  "candidate": "C-23",
  "contract": "K-23",
  "needed_evidence": [],
  "visible_ids": [
    "R2-old-check",
    "R2-old-approval",
    "R2-other-contract",
    "R2-other-project",
    "R2-note"
  ],
  "full_coverage": false,
  "sufficient": false,
  "context_records": 5
}
```

```text
Target is C-23, K-23, P-23, version 2. The visible records for that target:
- R2-old-check is version 1, so it is not current.
- R2-old-approval is version 1, so it is not current. It also has value false.
- R2-other-contract belongs to K-old-2, a different contract.
- R2-other-project belongs to P-other-2, a different project.
- R2-note is version 2 and in scope, but it is a note, not a check or approval.

No current check record exists for the target, so the check status is unknown. That means request_context.

{"decision":"request_context","candidate":"C-23","contract":"K-23","evidence_ids":["R2-note"],"reason":"No current (version 2, P-23, K-23) check record is visible. The only records for this target are a version 1 check and approval (stale), records from another contract and another project (out of scope), and a note that is neither a check nor an approval. The check status is unknown, so more context is needed."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P3-sonnet-C05-N3-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P3-sonnet-C05-N3-v0.json)

### P3-sonnet-C05-N3-v1

格式有效：True；不匹配字段：evidence_ids。

```json
{
  "decision": "request_context",
  "candidate": "C-23",
  "contract": "K-23",
  "needed_evidence": [],
  "visible_ids": [
    "R2-old-check",
    "R2-old-approval",
    "R2-other-contract",
    "R2-other-project",
    "R2-note"
  ],
  "full_coverage": false,
  "sufficient": false,
  "context_records": 5
}
```

```text
{"decision":"request_context","candidate":"C-23","contract":"K-23","evidence_ids":["R2-note"],"reason":"No current check (project P-23, contract K-23, version 2) is visible. The version 2 passed checks belong to another project or contract, and the version 1 check and approval are old. The note is neither a check nor an approval. The check status is unknown, so more context is needed."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P3-sonnet-C05-N3-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P3-sonnet-C05-N3-v1.json)

### P3-sonnet-C05-N7-v1

格式有效：True；不匹配字段：evidence_ids。

```json
{
  "decision": "request_context",
  "candidate": "C-27",
  "contract": "K-27",
  "needed_evidence": [],
  "visible_ids": [
    "R6-old-check",
    "R6-old-approval",
    "R6-other-contract",
    "R6-other-project",
    "R6-note"
  ],
  "full_coverage": false,
  "sufficient": false,
  "context_records": 5
}
```

```text
{"decision":"request_context","candidate":"C-27","contract":"K-27","evidence_ids":["R6-note","R6-old-check","R6-old-approval","R6-other-project","R6-other-contract"],"reason":"No current check (project P-27, contract K-27, version 2) is visible. The only passed checks are version 1 (stale), from another project, or from another contract, so none count as current evidence. The note is neither a check nor an approval. The current check status is unknown, so more context is needed."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P3-sonnet-C05-N7-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P3-sonnet-C05-N7-v1.json)

### P3-sonnet-C05-N8-v0

格式有效：False；不匹配字段：decision, candidate, contract, evidence_ids。

```json
{
  "decision": "request_context",
  "candidate": "C-28",
  "contract": "K-28",
  "needed_evidence": [
    "R7-check"
  ],
  "visible_ids": [
    "R7-check",
    "R7-old-check",
    "R7-old-approval",
    "R7-other-contract",
    "R7-other-project",
    "R7-note"
  ],
  "full_coverage": false,
  "sufficient": false,
  "context_records": 6
}
```

```text
Current check R7-check (version 2, K-28, P-28) passed. No current approval record is visible; the only approval record is version 1 and out of scope. Approval is therefore unknown, so context is needed.

{"decision":"request_context","candidate":"C-28","contract":"K-28","evidence_ids":["R7-check"],"reason":"Current check passed (v2), but no current approval record is visible; the only approval is an old v1 record, so approval status is unknown."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P3-sonnet-C05-N8-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P3-sonnet-C05-N8-v0.json)

### P3-sonnet-C06-N3-v0

格式有效：False；不匹配字段：decision, candidate, contract, evidence_ids。

```json
{
  "decision": "request_context",
  "candidate": "C-23",
  "contract": "K-23",
  "needed_evidence": [],
  "visible_ids": [
    "R2-old-check",
    "R2-old-approval",
    "R2-other-contract",
    "R2-other-project",
    "R2-note"
  ],
  "full_coverage": false,
  "sufficient": false,
  "context_records": 5
}
```

```text
Target is C-23, K-23, P-23, version 2. The visible records for the current target:
- R2-old-check: version 1, so it is not current.
- R2-old-approval: version 1, so it is not current.
- R2-other-contract: contract K-old-2, so it is out of scope.
- R2-other-project: project P-other-2, so it is out of scope.
- R2-note: a note, not a check or approval.

No current check record exists for version 2 of the target, so the check status is unknown. That means request_context.

{"decision":"request_context","candidate":"C-23","contract":"K-23","evidence_ids":[],"reason":"No visible current (version 2, P-23, K-23) check or approval record exists. The version 1 records are stale, the other-contract and other-project records are out of scope, and the note is not a check. Check status is unknown, so more context is needed."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P3-sonnet-C06-N3-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P3-sonnet-C06-N3-v0.json)

### P3-sonnet-C06-N4-v1

格式有效：False；不匹配字段：decision, candidate, contract, evidence_ids。

```json
{
  "decision": "request_context",
  "candidate": "C-24",
  "contract": "K-24",
  "needed_evidence": [],
  "visible_ids": [
    "R3-old-check",
    "R3-old-approval",
    "R3-other-contract",
    "R3-other-project",
    "R3-note"
  ],
  "full_coverage": false,
  "sufficient": false,
  "context_records": 5
}
```

```text
Target is C-24, K-24, P-24, version 2. Visible records for the current target:
- R3-note (version 2, matching): a note only, not a check or approval.
- R3-other-project: wrong project, so it is not evidence.
- R3-other-contract: wrong contract, so it is not evidence.
- R3-old-check and R3-old-approval: version 1, so they are not current. The old approval is also false.

There is no current check result for version 2, so the check status is unknown. Unknown means request_context.

{"decision":"request_context","candidate":"C-24","contract":"K-24","evidence_ids":["R3-note"],"reason":"No visible current (version 2) check_passed or approval record for C-24/K-24/P-24. The only matching current record is a note, which is neither a check nor an approval. The other check records are out of scope by project, contract, or version, so the check status is unknown."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P3-sonnet-C06-N4-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P3-sonnet-C06-N4-v1.json)

### P3-sonnet-C06-N6-v0

格式有效：False；不匹配字段：decision, candidate, contract, evidence_ids。

```json
{
  "decision": "request_context",
  "candidate": "C-26",
  "contract": "K-26",
  "needed_evidence": [],
  "visible_ids": [
    "R5-old-check",
    "R5-old-approval",
    "R5-other-contract",
    "R5-other-project",
    "R5-note"
  ],
  "full_coverage": false,
  "sufficient": false,
  "context_records": 5
}
```

```text
Target is C-26, K-26, P-26, version 2. The only check record for that exact scope is R5-old-check, which is version 1, so it is not current. The other check records belong to a different contract (K-old-5) or a different project (P-other-5), so they are not evidence for the target. The note is not a check or approval. There is no visible current check, so its status is unknown. Per protocol, an unknown check means request_context.

```json
{"decision":"request_context","candidate":"C-26","contract":"K-26","evidence_ids":[],"reason":"No visible current (version 2) check for C-26/K-26/P-26. Visible check and approval records are old (version 1), from another contract or project, or are a non-evidence note. The current check status is unknown, so more context is needed."}
```
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P3-sonnet-C06-N6-v0/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P3-sonnet-C06-N6-v0.json)

### P3-sonnet-C06-N7-v1

格式有效：True；不匹配字段：evidence_ids。

```json
{
  "decision": "request_context",
  "candidate": "C-27",
  "contract": "K-27",
  "needed_evidence": [],
  "visible_ids": [
    "R6-old-check",
    "R6-old-approval",
    "R6-other-contract",
    "R6-other-project",
    "R6-note"
  ],
  "full_coverage": false,
  "sufficient": false,
  "context_records": 5
}
```

```text
{"decision":"request_context","candidate":"C-27","contract":"K-27","evidence_ids":["R6-note","R6-old-check","R6-old-approval","R6-other-project","R6-other-contract"],"reason":"No current check (project P-27, contract K-27, version 2) is visible. The only matching-scope check (R6-old-check) is version 1, so it is stale. R6-other-project and R6-other-contract are from other scopes, and R6-note is not a check. The check status for the target is therefore unknown, so more context is needed. Approval is also not established, since the only approval record is version 1 and its value is false."}
```

[原始结果](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/raw/P3-sonnet-C06-N7-v1/result.json) · [输入](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/inputs/P3-sonnet-C06-N7-v1.json)

## 规则选取的正例

每模型每情境取字典序第一个匹配评分的ID；没有匹配输出的情境不补选。全实验共43条，选择规则早于结果读取。这些不是独立重复实验。

### P3-sonnet-C01-N1-v0

```text
{"decision":"accept","candidate":"C-21","contract":"K-21","evidence_ids":["R0-check","R0-approval"],"reason":"Current version 2 check passed and approval granted for C-21 under K-21 in P-21, both readable by agent-a; can_accept is true. Older version, other-contract, other-project records and the note were not used as current evidence."}
```

### P3-sonnet-C01-N2-v0

```text
{"decision":"request_context","candidate":"C-22","contract":"K-22","evidence_ids":[],"reason":"No visible check record exists for the target (P-22, K-22, version 2). The only K-22/P-22 check and approval records are version 1 (old), and the other check records belong to a different contract or project. The note is not a check or approval. The current check is unknown, so more context is needed."}
```

### P3-sonnet-C01-N3-v0

```text
{"decision":"request_context","candidate":"C-23","contract":"K-23","evidence_ids":[],"reason":"No visible current check for target version 2 of C-23/K-23/P-23. The version 1 check and approval are stale, the other-contract and other-project records are out of scope, and the note is not a check or approval. The current check status is unknown, so more context is needed."}
```

### P3-sonnet-C01-N4-v0

```text
{"decision":"reject","candidate":"C-24","contract":"K-24","evidence_ids":["R3-check"],"reason":"The current check for C-24 under project P-24, contract K-24, version 2 (R3-check) has value false, so the candidate fails. The other records are old-version, other-contract, other-project, or non-check evidence and do not change this."}
```

### P3-sonnet-C01-N5-v0

```text
{"decision":"return_result","candidate":"C-25","contract":"K-25","evidence_ids":["R4-check","R4-approval"],"reason":"Current version 2 check passed and approval granted for C-25 under K-25 in P-25, but can_accept is false, so the result is returned to the authorized owner rather than accepted. Older, other-contract, other-project records and the note are not used as current evidence."}
```

### P3-sonnet-C01-N6-v0

```text
{"decision":"request_context","candidate":"C-26","contract":"K-26","evidence_ids":[],"reason":"No visible check_passed record exists for C-26 under P-26, K-26, version 2. The only in-scope check (R5-old-check) is version 1, and the other checks belong to a different contract (K-old-5) or project (P-other-5). R5-note is not a check. The current check is therefore unknown, so context is needed before any other disposition."}
```

### P3-sonnet-C01-N7-v0

```text
{"decision":"request_context","candidate":"C-27","contract":"K-27","evidence_ids":[],"reason":"No visible check record exists for target version 2 of C-27 under K-27 in P-27. The only matching check (R6-old-check) is version 1 and so is outdated. R6-other-contract and R6-other-project belong to a different contract and project, and R6-note is not a check. The current check is therefore unknown, so more context is needed."}
```

### P3-sonnet-C01-N8-v0

```text
{"decision":"request_context","candidate":"C-28","contract":"K-28","evidence_ids":["R7-check"],"reason":"The current check R7-check (P-28, K-28, v2) passed. No current approval record is visible, because R7-old-approval is version 1 and out of scope. Approval is therefore unknown, so more context is needed."}
```

### P3-haiku-C01-N1-v0

```text
{"decision":"accept","candidate":"C-21","contract":"K-21","evidence_ids":["R0-check","R0-approval"],"reason":"The current check R0-check (check_passed, value true, version 2) and approval R0-approval (approval_granted, value true, version 2) match candidate C-21, contract K-21, and project P-21. The target has can_accept true, so the candidate is accepted."}
```

### P3-haiku-C01-N2-v0

```text
{"decision":"request_context","candidate":"C-22","contract":"K-22","evidence_ids":[],"reason":"No visible check_passed record exists for C-22, K-22, P-22 at version 2. The only same-contract, same-project check (R1-old-check) is version 1, so it is not current. The check is therefore unknown, and approval status cannot be relied on. Records from K-old-1 and P-other-1 are out of scope."}
```

### P3-haiku-C01-N3-v0

```text
{"decision":"request_context","candidate":"C-23","contract":"K-23","evidence_ids":[],"reason":"No current check is visible for C-23 under K-23/P-23 at target version 2. The only in-scope check (R2-old-check) is version 1 and labeled old, so the check status is unknown. Approval R2-old-approval is also version 1 and not current. Other-contract and other-project checks are out of scope, and the note is not a check or approval."}
```

### P3-haiku-C01-N4-v0

```text
{"decision":"reject","candidate":"C-24","contract":"K-24","evidence_ids":["R3-check"],"reason":"The current check for C-24 under K-24 in P-24 (version 2) has value false, so it failed. A failed current check is sufficient to reject. The older passing check (R3-old-check, version 1), the out-of-scope contract and project records, and the note were not used as current evidence."}
```

### P3-haiku-C01-N5-v0

```text
{"decision":"return_result","candidate":"C-25","contract":"K-25","evidence_ids":["R4-check","R4-approval"],"reason":"The current check (R4-check, check_passed, value true, version 2, P-25/K-25) passed and the current approval (R4-approval, approval_granted, value true, version 2) is granted. Because can_accept is false, the candidate cannot be accepted, so it is returned to the authorized owner, agent-a (Nora). Superseded version 1 records and the other-contract and other-project records were excluded."}
```

### P3-haiku-C01-N6-v0

```text
```json
{"decision":"request_context","candidate":"C-26","contract":"K-26","evidence_ids":[],"reason":"No visible current check_passed record exists for C-26 under K-26, P-26, version 2. The only check record in scope, R5-old-check, is version 1, so it is not current evidence. The other check records are from another contract or project. The check is therefore unknown, and the approval record is not needed to decide."}
```
```

### P3-haiku-C01-N7-v0

```text
{"decision":"request_context","candidate":"C-27","contract":"K-27","evidence_ids":[],"reason":"No visible check record for candidate C-27, contract K-27, project P-27 at target version 2, so the check is unknown. The only matching check (R6-old-check) is version 1 and is not current. R6-other-contract and R6-other-project are out of scope, and R6-note is not a check. The approval record R6-old-approval is also version 1 and not current."}
```

### P3-haiku-C01-N8-v0

```text
{"decision":"request_context","candidate":"C-28","contract":"K-28","evidence_ids":["R7-check"],"reason":"The current check R7-check (check_passed, value true, version 2, candidate C-28, contract K-28, project P-28) passed. No current approval record exists for version 2. The only approval record, R7-old-approval, is version 1 and is not current evidence, so approval status is unknown rather than known absent."}
```

## 一次字段反馈

全实验按预定字典序最多24例，不按论文均分。反馈只指出未通过字段，不补新事实或正确答案。该环节是一次回答修订，不是运行恢复。

| 主调用ID | 反馈可用 | 纠正 | 响应 |
|---|---|---|---|

## 结构性反例

[十二对投影反例](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/analysis/structural-witnesses.json)中的本篇四对是分析构造，0次模型观测。相同受限视图对应不同合法处置，说明视图不足；不证明生产输入曾丢失这些信息。


## 评分范围补记：接口、引用目的与未评分说明

125次原评分不匹配采用互斥主分类，先记材料缺陷，再记格式，最后记引用：106＋13＋6。格式失败中的P3响应另有3条在提取内嵌JSON后仍不符合引用列表规则；所以P3事后列表不匹配共有9条（Sonnet7、Haiku2），不是只有6条。原评分与正文的14次不匹配分解均保持，不能把这项诊断当预定再评分。

评分未覆盖reason中每一个自然语言断言。所选字段正确正例`P3-haiku-C01-N5-v0`把return_result的“有权接收者”说成agent-a/Nora，而给定材料只有当前actor的can_accept=false，没有指定该接收者；这是未获得材料支持的附加说法。主字段选择return_result及候选、合同、证据仍匹配，不因此回写冻结分数。该反例说明“核心字段全部正确”不能扩写为全部解释正确或权限关系已完整理解。本轮没有逐条编码所有reason的此类错误率，也不把这一条归因于人名。
