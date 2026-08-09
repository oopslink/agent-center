# 测试报告 — T1297 Team Memory main 最终复验

## 结论

**PASS（blocking=false）**，review SHA `db8faefb4cbf012f03a188161eebf7786eed4f13`。

## 结果

| # | 状态 | 证据 |
|---|---|---|
| 1 | pass | `origin/main` 与 `ls-remote refs/heads/main` 均精确为 `db8faefb`; `76025b4f` 为其父提交 |
| 2 | pass | `go test ./internal/cli ./internal/webconsole/api ./internal/cognition/memory/teammemory ./tests/integration ./tests/e2e` |
| 3 | pass | Team Memory service、startup reconcile/错误日志、Web proposal lifecycle 的 `-race -count=5` |
| 4 | pass | `go vet ./...`; `make build`，产物 buildCommit=`db8faefb` |
| 5 | pass | `make smoke`: Chromium deployed pipeline 1/1；真实 server + worker enroll + agent-tools dispatch；runtime-version e2e；23s |
| 6 | pass | T1294 在完全相同的不可变 commit 上独立真部署：Web pending/promotion 前后 proposal ID 精确一次；浏览器关闭后清空派生 events/checkpoint，仅重启 center 即恢复 proposed/promoted，二次重启仍各一；错误注入可观测，CAS/权限/snapshot 回归绿 |

## 分层清单

| 层 | 本轮计数/入口 |
|---|---|
| Unit/integration | 3 production packages + integration + e2e packages |
| Race | 3 related scopes, count=5 |
| Deployed-binary | 1 Playwright deployed pipeline + runtime-version e2e |
| Feature deployed cross-evidence | T1294 同 SHA 的 Web/bare-Git restart lifecycle |

## 说明

T1294 的 feature deployed 证据产生时修复尚在分支，但 Git commit 是不可变制品；T1296 将同一 commit 严格 fast-forward 到 main，T1297 又独立证明远端 main 精确等于该 SHA，并重新构建和运行 deployed-binary smoke。因此不存在“分支 PASS 冒充 main”的谱系缺口。

全部出口标准满足，允许计划按最终 PASS 收口；历史 REJECT/discarded generation 应保留审计，不应覆盖本次 verdict。
