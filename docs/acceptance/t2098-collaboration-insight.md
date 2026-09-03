# T2098 Collaboration Insight 独立验收

## 结论

**REJECT**。验收对象固定为
`origin/candidate/collaboration-insight-integrated@69babcc3765e947d1d9dc4da086e01860b662b23`，
基线为 `origin/main@4b8810577a6b747c59d6ba5a9dbda0e7002e0b92`。候选唯一、可达、领先基线 6 个提交，
但 dependency release 的生产语义方向反转，违反冻结验收合同第 ③、⑦ 项。

## 阻断项：依赖释放方向反转

生产 `AddPlanDependency(plan, B, A)` 的含义是 `B depends_on A`；审计事件按原样记录
`detail.from=B, detail.to=A`（`internal/projectmanager/service/audit_flow_test.go` 和
`plan_flow.go` 均明确这一合同）。候选的规则引擎却将 `from` 保存为 `UpstreamTaskID`、
`to` 保存为 `DownstreamTaskID`（`internal/observability/collaborationeffect/engine.go`）。
结果是完成真实 upstream A 时无法匹配依赖，页面缺少 A → B 的 `dependency_release` effect；
反而可能在完成 dependent B 时错误生成指向 A 的释放作用。

最小复现：向 `Projector` 依次投影
`pm.audit_recorded{change_type:dependency_added, detail:{from:B,to:A}}` 与
`pm.task.state_changed{task_id:A, running→completed}`，随后查询 P1/v1 effects 并断言存在
`relation_type=dependency_release,target_task_id=B`。实际仅返回 A 的 `complete` effect。

复现命令（临时测试，未留入候选）：

```text
go test ./internal/observability/collaborationeffect -run TestT2098ProductionDependencyDirection -count=1 -v
exit 1
missing A completion -> B release; effects=[... TargetTaskID:A ... RelationType:complete ...]
```

根因修复要求：对 `dependency_added` 使用 `UpstreamTaskID=detail.to`、
`DownstreamTaskID=detail.from`，并增加由真实 `AppService.AddPlanDependency` 产生审计、完成 A、
从 Web/API 读到指向 B 的 release effect 的 production-chain 回归测试。当前测试使用手造
`from=UP,to=DOWN` fixture，与生产合同相反，因而未捕获缺陷。

## 其余门禁证据

- 远端固定性：候选 SHA 仅由 `origin/candidate/collaboration-insight-integrated` 包含；
  `origin/main` 是其祖先，`rev-list --left-right --count` 为 `0 6`；隔离 worktree 初始 clean。
- `pnpm exec tsc --noEmit`：exit 0。
- `pnpm test`：exit 0，200 test files / 1910 tests passed。命令因参数透传方式实际执行了前端全量，
  覆盖 Collaboration 页面键盘按钮、文字 polarity、evidence drawer、scope/empty/403/500、truncated UI。
- `go test -race ./internal/observability/collaborationeffect/... ./internal/projectmanager/service/... ./internal/admin/api/... ./internal/webconsole/api/... ./internal/cli/...`：
  启动并通过 collaborationeffect 与 projectmanager/service；其余包长跑结果见任务回报。
- 首次 `npm test` 在仓库根目录退出 254（无根 package.json），随后隔离 worktree 通过
  `pnpm install --offline --frozen-lockfile` 使用本机缓存完成依赖准备；这不是产品失败。

## 合同判定

纯程式化、联系/作用分离、reject=mixed、evidence/rule version、幂等、授权 fail-closed、
显式截断、非颜色提示等已有单测或代码证据；但验收合同要求所有冻结项均可复验，且真实链路必须覆盖
`assign → upstream complete → downstream release`。该必过链路失败，因此不得写 PASS，也不得合入 main。
