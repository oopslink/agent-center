# 实验仪器缺陷与排除

H01–H03在主设计冻结前发现；H04在v1正式执行期间发现。均不能作为Agent Center架构负例。

| ID | 发现 | 修复/处理 | 原证据 |
|---|---|---|---|
| H01 | 已完成未收取的worker句柄被误报仍active | 每回合、spawn及终止时poll；真实完成事件回流；合格快照不因清理中的进程而失败 | review/runtime-method-findings.md |
| H02 | 多个检查可能读取不同工件版本，晚到回执可能倒填review | 跨进程文件锁、原子写入、一致终止副本；回执须早于同版本review | test_runtime T03/T05；review记录 |
| H03 | worker把工具参数放args中时被误报无权限；pilot-v1 P2合理blocked | flat/nested参数等价正规化；缺参数单列；保存v1，不计任何主效果；同开发族v2复测 | pilot/dev-p2-long；pilot-v2/dev-p2-long |

C03身份词汇混杂、C04仅初始坏代码而缺执行交接、C05台账可观测性范围、C06预算指标等设计修订见分工审阅。当前失败起点来自真实脚本化executor进程，但不是自然LLM失败。所有主输出应使用冻结后的实现；如果正式批次发现新仪器bug，保留原输出并按任务×模型整块排除，另版重跑。

## H04：正式轮的测试元数据误拒绝

v1合法测试附加name被未说明的精确键校验误拒绝；5/5输入及预期实际正确。修复接受描述字段，给出逐项诊断，新增T10。v1全轮34已执行/20未启动退出主要比较，v2原54条件全部重跑。证据：review/h04-adjudication.md/json、v1-exclusion-ledger.json、raw-manifest-v1.json。

v2只改变H04相关测试合同、校验诊断及运行ID。没有根据主结果改任务、因素、预算、排序或主要分析算法。修复前已确认全部旧进程退出，见setup/v1-shutdown-confirmation.json。
