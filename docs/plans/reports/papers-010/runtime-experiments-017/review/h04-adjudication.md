# H04：测试元数据被未声明的封闭schema拒绝

**定性：实验harness与已公布工件合同不一致，应排除为仪器bug。** 本次只查看指定事件的test_cases、module及失败/请求外部事件，没有展开其它条件的效果分析。

原`ARTIFACT_CONTRACT`要求input与output或error，未禁止name等元数据。指定builder的5个case均有不同input，核心预期全部符合冻结expiry参考实现；但原`set(c)`只接受精确两键，后续整非input字典比较也把name算入了预期。两条校验都会无依据拒绝这5个case。逐项机械结果和源SHA见同名JSON。该核对未调用模型，也未修改原数据。

worker与controller均收到test_contract_invalid / AssertionError，公开检查通过，随后controller请求外部帮助。因此这条轨迹不能用作工作流或模型的干净失败样本。它也不能自动变成模型判断完全正确的正例：controller把可见用户测试称为hidden等个别解释仍需保留原文边界。

## 修复必须保持的边界

- 只用input和恰好一个output/error进行执行语义核对；name/notes等额外字段不导致拒绝，原metadata仍保存。
- 仍拒绝缺input、output/error同时存在或同时缺失、错误预期、少于4个不同input；元数据不能掩盖任何核心错误。
- 审查/测试/manifest的原文件哈希仍绑定完整工件，不因语义校验忽略metadata而忽略版本变化。
- 保留全部v1成功与失败，另锁v2并按原54行设计全批重跑，不拼接最佳结果。v2是已看过v1输出后的仪器修复与重跑，不是新的未见留出确认；不因此删除或放大真实负结果。

待核：元数据兼容代码、回归测试原始日志、v1归档与v2源锁。
