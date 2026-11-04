# P1 revision-014 派生证据包

对应[纵向分析](../longitudinal-analysis.md)与[论文](../paper.md)。单部署探索，未人工复核；不是新的模型实验。完整私有原件保存于本地`run-001/papers-010/revisions/014-p1-longitudinal/`。

| 文件 | 用途 |
|---|---|
| [cases.json](derived/cases.json) | 9组定向材料的分类、证据锚点和排除边界 |
| [record-locators.json](derived/record-locators.json) | 34条选定消息的原生ID、本地路径及哈希；原文不随包公开 |
| [timeline.json](derived/timeline.json) | 图5底表；包含创建Task早于身份提醒的反证；不是发生率数据 |
| [construct-audit.json](derived/construct-audit.json) | 512个实验输入均明示修复责任，64个S7均给具体授权修复；输入哈希已核 |
| [discovery.json](discovery.json) | 新检索范围、SQL与关键词；候选不等于故障数 |
| [protocol.md](protocol.md) | 本轮现象／因果／bug排除分层规则 |
| [validation.json](validation.json) | 记录定位、原实验不变、时间顺序、图表及局部链接检查 |
| [process.md](process.md) | 查询、选择、修改及技能工具缺失的处理记录 |

`scripts/`保存只读查询、原输入构念检查、绘图、导出和校验。图5可在本包目录使用已有绘图环境运行`scripts/figure.py`，输出到本包`figures/`；正式论文图位于上一级`figures/`。原始数据库查询及所有来源核验需要本地授权原件，聚合包不能替代完整复现。

保留7月22日不同主体的责任纠正、7月23日当时的操作者诊断及源码提示干预、8月后续正反材料、9月4日再度纠偏。源码修改时间不证明部署或知识内化。`failed`本身不是身份错误，需结合任务归属、明确表述和后续动作；新任务出现也不是最终交付成功。

原512动作＋32诊断＋3事后续步全部保持，不重新标注原正确输出为错误。新构念检查属于事后分析。本轮没有新增外部引用，没有运行缺失的官方ARS补丁／缩写门禁，不声称投稿认证或科学有效性认证。
