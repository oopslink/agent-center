# 测试报告 — T1285 Team Memory 最终验收

## 候选

- 验收时间：2026-08-09（Asia/Shanghai）
- `origin/main`: `76025b4f375b210bf10a9a840bb014f4f95cc13d`
- 结论：**REJECT（blocking）**

## 计划项执行结果

| # | 用例 | 状态 | 证据 / 备注 |
|---|---|---|---|
| 1 | 候选谱系 | fail | fetch 后 `origin/main` 仍精确等于 T1292 已拒绝 SHA `76025b4f`，不存在严格后继 |
| 2 | Web 列表唯一性 | fail | `teamMemoryIndexHandler` 先用 `TeamMemory.List` 形成 `out`，再遍历 `TeamMemoryWrite.List` 无去重 append；与 T1292 真部署重复 ID 现象一致 |
| 3 | 重启补投入口 | fail | production wiring 只构造 `TeamMemoryProjector`；`ReconcileTeam` 仅在 Web/MCP 请求 handler 内调用，未发现 center 启动时 reconcile |
| 4 | deployed smoke 全链 | blocked | 当前 SHA 已由 T1292 真部署复现两项 P0，且候选无变化；等待 remediation 后必须重跑 |
| 5 | 全量回归门 | blocked | P0 与候选谱系已触发 fail-fast；不得用全量绿掩盖功能阻断 |

## 分层测试清单

| 层 | 本轮计数 | 说明 |
|---|---:|---|
| Unit (in-package) | 0 | 候选未变化，fail-fast |
| Integration with mocks | 0 | 候选未变化，fail-fast |
| Deployed-binary smoke | 0 new / 1 inherited red baseline | T1292 已在同一 SHA 真部署复现；本轮不把继承证据计作新执行 |

## 阻断详情

1. **Web proposal 重复仍未修复。** `internal/webconsole/api/handlers_teams_p2.go` 的索引读取混合 legacy read adapter 与 controlled-write Service，并无 proposal ID 去重或单一 AppService read model。
2. **restart observability backfill 仍未实现。** `internal/cli/webconsole_wiring.go` 只注入 projector；源码中的 `ReconcileTeam` 调用点均属于 Web/MCP 请求路径，center restart 本身不会从 Git 主动补投 proposal transitions。

## 出口标准核对

- [ ] 当前 main 是已拒绝 SHA 的严格后继
- [ ] Web 同 proposal ID 唯一
- [ ] center restart 主动补投 events/checkpoint
- [ ] remediation 后 deployed-binary 全链通过
- [ ] Go/race/Web/build/lint 全绿

## 结论与后续

当前 `main` 没有吸收 T1292 两项 P0 的任何修复，不能进入 PASS。应先追加 remediation 节点修复唯一 read model/去重与启动 reconcile（错误必须可观测，不得忽略），推送 `76025b4f` 的严格后继；随后重新执行 T1285 完整 deployed-binary、权限、CAS、snapshot、restart backfill 与发布门。
