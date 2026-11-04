# P5 round-002：历史、案例与定量描述数据包

对应[研究报告](../../agent-harness-memory-study-011.md)和[案例附录](../../agent-harness-memory-cases-011.md)。作者 oopslink，无资助；单 AI 探索，尚未人工复核。此包不包含原始私有记忆、对话全文、凭据或45GB数据库。

## 数据与图

| 文件 | 内容与单位 |
|---|---|
| [summary.json](derived/summary.json) | 全部描述统计及验证计数；不是交付效果统计 |
| [proposals.csv](derived/proposals.csv) | 30个提案的状态、审核类型、等待与右删失 |
| [event-git-validation.json](derived/event-git-validation.json) | 57条领域事件与Git commit／提案状态逐项比对 |
| [read-receipts.csv](derived/read-receipts.csv) | 230条去重正文回执及commit／blob／phase核验；首次回执不是全部调用 |
| [rules.csv](derived/rules.csv) | 10条canonical rule的尺寸、晋升、首次可见回执与覆盖 |
| [agent-rule-matrix.csv](derived/agent-rule-matrix.csv) | 图3矩阵的非零单元；人数不等于独立实验参与者 |
| [private-file-inventory.csv](derived/private-file-inventory.csv) | 现存私有／legacy文件路径、作用域、字节和哈希，不含正文 |
| [source-provenance.json](derived/source-provenance.json) | 9个文件×3个参考commit的内容哈希 |
| [source-milestones.json](derived/source-milestones.json) | 11个源码历史节点，非部署时间线 |
| [deployment-source-matrix.json](derived/deployment-source-matrix.json) | 56个留存产物与2个current指针；mtime不作激活时间 |
| [binary-identities.json](derived/binary-identities.json) | 两份current磁盘二进制的SHA-256及版本元数据绑定；不是历史运行进程证明 |
| [案例台账](evidence/case-ledger.json)／[排除片段](evidence/exclusions.csv) | 纳入、反证、混合原因与明显bug排除 |
| [源定位](evidence/source-locators.json) | 34个选定原生记录的本地原件路径、JSON Pointer及哈希；原件不随仓库提交 |
| [过程记录](process.md)／[校验](validation.json) | 选择方法、查询错误修正、图表检查及来源核对 |

[治理图 SVG](figures/01-governance.svg) · [PDF](figures/01-governance.pdf) · [PNG](figures/01-governance.png)
[访问图 SVG](figures/02-access.svg) · [PDF](figures/02-access.pdf) · [PNG](figures/02-access.png)

## 重建

图表可直接从本包CSV重建，使用已有研究绘图环境或等价的Matplotlib／NumPy环境：

```sh
/Users/oopslink/works/research/agent-harness-study/.venv-figures/bin/python scripts/figures.py
```

环境版本见[environment.json](figures/environment.json)。脚本以自身父目录作为数据根；不依赖当前工作目录。没有安装或修改产品依赖。

全部分析的重建需要**本地原件**，位于`/Users/oopslink/works/research/agent-harness-study/run-001/memory-011/round-002/`，以及原冻结SQLite快照；仓库中的aggregate包不能代替原件独立复现。依次运行`scripts/rebuild_auxiliary.py`（校验归档Git／DB）、`scripts/analyze.py`（派生统计）、`scripts/build_evidence.py`（定位）、`scripts/figures.py`（图表）、`scripts/validate.py`（一致性）。`case_extract.py`与`query_cases.py`记录只读查询和有目的选择；`source_capture.py`记录固定commit源码提取。

`capture.py`是首次现场采集脚本，会访问当前部署目录／日志，**不是重放命令**；不要在旧round目录重跑并覆盖原件。新的现场采集必须使用新round并记录新观测时间。原始Git bundle、镜像、私有文件、完整消息、过滤日志、版本信息与前后文档均在仓库外留存，并由本地manifest记录哈希。原SQLite哈希沿用首轮采集清单，本轮未重算。

## 版本与解释限制

Center current标签与binary revision一致但dirty；Worker标签与嵌入式revision冲突且dirty。历史部署的逐执行映射没有完成。Team全部57次变化与事件一致；230条回执全部可定位，不代表模型已经正确复用。案例目的抽取、缺完整输入与共同干预阻止净效果估计。三项pending按2026-11-02 06:41:08.656823 UTC右删失；现场Git采集稍晚，不能称跨存储原子快照。

旧轮次不覆盖；本轮新增报告、附录、两张统计图及结构图，更新计划与索引。未修改产品、未提交Git、未执行新模型实验。
