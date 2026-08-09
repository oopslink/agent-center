# 测试报告 — T1295 Team Memory remediation 最终验收

## 结论

**REJECT（blocking）**。Review SHA `db8faefb4cbf012f03a188161eebf7786eed4f13` 的代码与候选验证为绿，但它尚未进入 `origin/main`；main 仍精确指向已被 T1292/T1285 拒绝的 `76025b4f375b210bf10a9a840bb014f4f95cc13d`。因此不能以 feature-branch PASS 收口计划。

## 计划项结果

| # | 状态 | 证据 |
|---|---|---|
| 1 | pass | `db8faefb` 是 `76025b4f` 的单提交严格后继；远端 `fix/t1293-team-memory-reconcile` 可达 |
| 2 | pass | `go test ./...`; `go vet ./...` |
| 3 | pass with scope | Team Memory service、新增 startup tests、Web proposal lifecycle 相关 `-race -count=5` 通过；联合 `webconsole/api` 全包 race 210s 未完成后人工中止，未见 DATA RACE |
| 4 | pass | Web 186 files / 1709 tests；`make lint`; `make build` |
| 5 | inherited pass / not rerun | T1294 已在同一 SHA 独立真部署验证 Web 去重、restart 主动补投及幂等、错误可观测、CAS/单赢家、权限与 snapshot；本轮因交付门先失败，不把继承证据计作新 deployed smoke |
| 6 | **fail** | fetch 后 `origin/main=76025b4f`，不包含 `db8faefb` |

## 分层清单

| 层 | 本轮结果 |
|---|---|
| Unit / integration | Go 全量 PASS；Web 1709 PASS |
| Race | 变更相关路径 `-count=5` PASS；联合大包未完成并明确披露 |
| Deployed-binary smoke | 0 new；T1294 同 SHA 红→绿证据仅作输入 |
| Build / lint | PASS |

## 阻断与下一步

必须先将 `db8faefb`（或其严格后继）通过正常集成流程推进到 `origin/main`，回读远端 SHA 与 ancestor 关系；随后在精确 main 上重跑 deployed-binary 最终 smoke 并提交新的结构化 verdict。在此之前，T1295 不得 PASS，计划不得收口。
