# round-002 过程与判断变更

本轮用户授权：补齐可得记忆Git／部署版本，分析真实写入、共享、读取和复用，排除明显系统bug，保存过程及更新报告。仅只读研究；不含产品修复、生产触发、付费模型实验、Git提交或发布。

## 执行过程

1. 保存协议；沿用冻结SQLite快照，盘点现存记忆目录与部署产物。采集4个非空Git的bundle与只读mirror；另3个空bare仓库记录无法创建empty bundle的正常结果，保留refs前后比较。遗留Worker文件按字节复制和校验。Center的`var/backups`为空；`var/team-git`仅global与team目录，未发现额外agent bare库。
2. 分别记录current symlink、COMMIT/VERSION、Go binary build info、DB Worker心跳自报、历史产物目录、限定关键词日志片段。没有把mtime当部署时间；PID文件对应进程查询无返回，没有重启服务。日志英文正则不覆盖所有中文“记忆”文本，命中日志不是全量运行轨迹。
3. 查询词法候选及提案来源，扩展必要对话、Task action和领域审计。全量检查Team提案／事件／Git与230条正文审计；585条消息只是候选全集，深读为定向选择，不声称全部人工或AI双重标注。
4. 对存储归属、发布状态、服务端回执、行为采用和结果分别编码；将明显故障片段与缺失证据单列。保留M02/M03关联正反链、M04当次context反证、M05–M07弱证据或排除。保留34条选定原生记录定位；其余全量Git／audit／源码由独立数据表定位。
5. 重建派生CSV、统计与两张双面板科学图，另在报告给出路径结构图；已逐图视觉查看，无文字重叠或裁切。报告正文压缩日志细节，时间、ID和局限放入案例附录。
6. 独立从归档mirror重新抽取47个HEAD文件元数据，从只读DB重新抽取57个事件和230个audit，与采集结果一致。核对源定位哈希、bundle、文档链接与变更范围；保存修改前后和diff。只同步研究文件到主工作区，保留其已有cherry-pick冲突及产品改动。

## 纠正与失败记录

- 初始事件查询用了`team.memory.%`／`memory.%`，得到0行。事件类型盘点后改成实际的`team_memory.%`，取得57行；保留原零结果、修正SQL与两份原件。原零值不得作为事件缺失。
- `agent_activity_events`确实0行，但另有专门的`team_rule_load_audits`230行。通用activity无记录不等于没有读取证据。
- `analyze.py`首次因系统Python对五位小数的ISO时间解析报`ValueError: Invalid isoformat string`；将小数统一到微秒精度后通过，原时间字符串不修改。
- 系统Python未安装matplotlib（ModuleNotFoundError）；改用研究目录已有`.venv-figures`，未改产品依赖。图脚本首次单点分组的jitter为list，触发`int + list` TypeError；改numpy零数组后两图生成成功。
- zsh未匹配的隐藏路径glob造成一次目录发现失败；改为pathlib枚举找到现有venv。没有把shell错误当目录不存在。
- 首次私有scope统计把issue路径粗归project；在报告前按issue独立重算：legacy root 2、project 3、issue 3、task 15。总23文件／129938bytes不变。
- 将结构图中的“祖先路径组装”改成“启动披露根正文／其余按需索引与读取”，避免把可用scope函数误写成所有正文自动注入。
- 首次文本patch定位失败后按实际整行重新应用，未产生半次修改。

## 对首轮判断的增量

| 原问题 | 本轮新判断 | 仍不能回答 |
|---|---|---|
| 记忆Git是否可得 | 2个Team完整可达历史可恢复；2个新agent home只有骨架；legacy有文件无Git | 旧private home是否写过、何时读过 |
| 部署源码是否明确 | 56个留存目录+current标签可恢复，9文件在3个参考commit相同 | dirty binary精确树、Worker冲突revision、逐执行激活时间 |
| 是否真有共享读取 | 230个成功服务端正文查找回执，可还原10规则，9规则跨agent | HTTP完整接收、LLM理解、entries阅读和正确复用机会分母 |
| 冻结是否造成旧知识误用 | 可见回执均等于读取前最新已入库Team变更commit；是反证 | 无回执访问／长会话后续采用／未发布知识 |
| 审核有没有实际作用 | canonical整理与拒绝边界可见，且16次作者自审 | 独立质量、净收益或人时 |
| 记忆是否促进交付 | 有规则明确采用和局部接力；宽规则也有合同冲突 | 记忆的单独因果效应、整体成功率 |

本轮研究记录不等于人工复核或投稿定稿。下一轮先验证fixture，再决定实验规模；无付费实验已执行的暗示。
