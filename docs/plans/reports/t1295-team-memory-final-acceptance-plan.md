# 测试计划 — T1295 Team Memory remediation 最终验收

## 范围与候选

- 验证 remediation SHA `db8faefb4cbf012f03a188161eebf7786eed4f13` 的谱系、构建、回归与受控写入关键门。
- 最终交付门要求修复进入 `origin/main`；分支候选通过不能替代 main 上闭环。
- T1294 真部署 PASS 仅作为输入，本轮独立核验发布门与最终 Git 状态。

## 用例

| # | 层 | 目标 | 出口标准 |
|---|---|---|---|
| 1 | Git | remediation 谱系 | 是 `76025b4f` 的严格后继且远端可达 |
| 2 | Unit/integration | Go 全量与 vet | 全绿 |
| 3 | Race | Team Memory 相关并发路径 | `-race -count=5` 全绿 |
| 4 | Web | SPA tests/lint/build | 186 files / 1709 tests、lint、build 全绿 |
| 5 | Deployed | 去重、CAS、权限、snapshot、restart backfill | 独立真部署全绿 |
| 6 | Delivery | main 集成 | `origin/main` 包含 review SHA |

## 判定

- 任一 P0 或交付门失败即 blocking REJECT。
- 只有 1–6 全部通过，才允许计划收口。
