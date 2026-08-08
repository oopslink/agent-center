# T1269 Team Memory rules 独立验收

## 测试计划

- 基线：`origin/main` / `851222c25c6faa458fa6fc7480b0d7da75ef12a2`
- 原则：不依赖开发者口述；不修改生产代码；用临时 Git host、真 HTTP、构建产物和隔离部署实例取证。

| # | 层 | 验收目标 | 出口标准 |
|---|---|---|---|
| 1 | integration | Team repo 同时生成 `entries/`、`rules/` 与派生 `MEMORY.md` | 两类内容可读取；rule 类型仅来自路径，无 `kind/type` 双标 |
| 2 | integration | `get_team_rules` 的 phase、enabled、applies_to、commit 与空/错路径 | 仅返回当前阶段已启用规则；响应含 commit/source_path；非法 phase 显式失败 |
| 3 | integration | fork 输入快照与刷新语义 | input 固化 rule snapshot；同一 execution/recovery 不隐式刷新 |
| 4 | integration | 旧 workflow template 安全迁移 | 仅唯一同组织 owner 归属被 claim；builtin/缺 owner/跨 org/歧义不广播 |
| 5 | UI/unit | Templates 页面、路由、导航下线；Team Memory rules 分组/筛选 | 目标 Web 回归全绿；普通 entry 不被启发式误标 |
| 6 | static audit | `template_ref`、MCP/API/UI 消费者扫描 | 无活动 Workspace Template UI 消费者；残留只属于明确 deprecated 后端兼容或 Team Template 领域 |
| 7 | gates | 全量回归、lint、race、build、真部署 smoke | 关键门禁通过；任何失败必须复跑定性并如实记录 |

## 测试报告

| # | 状态 | 独立证据 |
|---|---|---|
| 1 | PASS | `TestTeamMemoryProducer_SeedTeamRulesAndConsumerSnapshot` 在临时 bare Git host 中真实 clone/commit/read；`TestTeamMemoryProducer_SeedTeam` 校验 entries 与 `MEMORY.md`。fixture 与 Web 断言均不含 `type: rule`。 |
| 2 | PASS | centergit consumer、admin 真 HTTP fixture 与 MCP host 测试通过；execute snapshot 只含 enabled+execute rule，包含非空 commit/source_path。phase 枚举为 plan/execute/review/recovery，非法 phase 返回 `invalid_phase`。 |
| 3 | PASS | agentruntime/executor 测试验证 fork 前调用 `get_team_rules`，完整 snapshot 写入 `input.json`；protocol 保存 commit 与 refresh semantics；`make test-race` 以 `-race -count=10` 通过。 |
| 4 | PASS | `TestPlanLegacyWorkflowTemplateMigrationClaimsOnlyUniqueTeamOwners` 与 `TestApplyLegacyWorkflowTemplateMigrationWritesOnlyClaimedTeamRules` 通过：唯一 owner 写入 plan rule；4 类未知归属保持 unclaimed；无关 Team 未建仓；旧 `pm_templates` 不删除。 |
| 5 | PASS | `TeamDetail`、`App`、Workspace/Team nav 共 46/46 通过。真实 build 后隔离实例中 `/organizations/<org>/templates` 与 `/organizations/<org>/teams/templates` 均显示 `404 — Not found`；Workspace/Teams 导航无 Templates。 |
| 6 | PASS | 活动 Workspace Template hook/type/query key 已删除。`list_templates`/旧 CRUD 仍在后端并显式标记 LEGACY/DEPRECATED，满足迁移回滚兼容；`workflow_template_ref` 残留属于 Team Template 兼容字段，不是已下线的 Workspace Templates UI 消费者。 |
| 7 | PASS with recorded flake | 目标 Go 包全绿；`make lint`、`make build`、`make test-race` 全绿。`go test ./...` 首轮唯一失败为无关 `TestSupervisorSession_DetachSurvives`，单测立即独立复跑通过；rules/team/admin/agentruntime/mcphost、integration、e2e 均通过。 |

## 测试分层

| 层 | 结果 | 入口 |
|---|---|---|
| Unit / in-package | PASS | centergit、team/migration、admin/api、agentruntime、mcphost 目标包；Web 4 files / 46 cases |
| Integration with mocks/real local IO | PASS | 临时 bare Git repo + 真 git clone/commit；admin server 真 HTTP；`tests/integration` 与 `tests/e2e` 在全量 Go 中通过 |
| Deployed-binary smoke | PASS (1 sandbox, 2 retired routes) | 本分支 `make build` 后 `install test-instance --id t1269-rules --with-seed`；真登录后验证两个 Templates 路由均 404 且导航无入口 |

## 门禁命令

- `go test ./internal/cognition/memory/centergit ./internal/team/migration ./internal/admin/api ./internal/agentruntime/... ./internal/mcphost/...` — PASS
- `pnpm exec vitest run src/pages/TeamDetail.test.tsx src/App.test.tsx src/shell/nav/WorkspaceSecondaryNav.test.tsx src/shell/nav/TeamUISecondaryNav.test.tsx` — 46/46 PASS
- `make lint` — PASS
- `make build` — PASS（Vite 有既存 CSS/chunk size warning，无错误）
- `make test-race` — PASS (`-race -count=10`)
- `go test ./...` — rules 相关与 integration/e2e PASS；workerdaemon detach 单例首轮 timeout，独立复跑 PASS

## 结论

**PASS / ACCEPT。** T1269 所列七个验收域均有可执行证据，未发现 Team Memory rules 收口的阻断缺陷。记录一项与本变更无关、可独立复跑通过的 workerdaemon 时序 flake，不据此拒绝本次收口。
