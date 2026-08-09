# 测试计划 — T1285 Team Memory 最终验收

## 范围

- 被测对象：精确 `origin/main` 上 Team Memory 受控写入与学习闭环。
- 候选要求：T1292 的 blocking REJECT SHA `76025b4f` 的严格后继，且修复 Web proposal 去重与中心重启补投。
- 不在范围：修改生产实现；对未变化的已拒绝 SHA 重复执行全量门禁。

## 前置条件

- 从共享预克隆仓库执行 `git fetch origin --prune`。
- 以远端 `refs/heads/main` 为唯一候选事实源。
- 保留 T1292 真部署红端证据作为回归基线。

## 用例清单

| # | 类型 | 目标 | 输入 | 预期 |
|---|---|---|---|---|
| 1 | lineage | 候选谱系 | `origin/main` | 是 `76025b4f` 的严格后继 |
| 2 | static contract | Web 列表唯一性 | legacy index + controlled proposals | 同一 proposal ID 只出现一次 |
| 3 | static lifecycle | 重启补投入口 | center 启动 wiring | 启动后主动 reconcile Git proposals |
| 4 | deployed smoke | 全链闭环 | MCP propose → review → promotion → restart | 全链通过且事件补齐 |
| 5 | regression | 发布门 | Go/race/Web/build/lint | 全绿 |

## 出口标准

- 1–5 全部通过才可 PASS。
- 任一既有 P0 在当前候选仍存在，立即 REJECT；后续项目记为 blocked/skip，等待 remediation 后重跑。
