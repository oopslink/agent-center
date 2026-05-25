# v2.3-3b Real-Agent Dispatch Chain Audit

> Closes slock task #29. Wires `defaultAgentSpawner` end-to-end so real
> agents (claude-code / codex / opencode) — not just fakeagent — can be
> dispatched: secret resolve + blob upload + MCP injection + prompt
> assembly all reach the worker daemon spawn path. Resolves the gap
> flagged in v2.2 closeout audit § 4 ("dispatch goes via fakeagent only —
> the v9 piece of the chain didn't make it to v2.2 GA").

## § 0. 必答铁律答卷 (per conventions § 0.4)

| 必答问题 | 答 |
|---|---|
| 起点 = 领域模型要求？ | 是。`MCPInjector` / `AssemblePrompt` / `SecretResolutionService` 都已在 P9 ST 23/22/8 落定，本 ST 是 transport + spawner wiring，不是新建领域模型 |
| 保留 BC invariants？ | 是。`UserSecret` 仍只通过 `SecretResolutionService.Resolve` 出 plaintext；admin transport 是 wrapping 层，没有跨 BC 调用 |
| 没省 transport？ | 是。**强化** transport：新增 `/admin/secret/user-secret/resolve` + `/admin/blob/put` + envelope `home_dir` 字段。Worker daemon 通过 admin endpoint resolve / put，不直读 DB / FS root |
| Mock-as-default 消除？ | 是。worker daemon 默认 `MCPInjector{nil resolver}` 是 no-op only when home_dir empty；非 fakeagent + 非 empty home_dir 必须有真 resolver，否则 secret_unresolvable 即时 NACK |
| 起点 = "领域模型要求"？ | 是。`DispatchEnvelope.HomeDir` 来自 `AgentResolution.HomeDir`（P8 § 3.5 + ADR-0024 § 5），不是从 transport 反推 |

## § 1. 范围

### Admin endpoint
- `POST /admin/secret/user-secret/resolve` (scope `secret:resolve`) — base64-std plaintext echo, actor from bearer Owner
- `POST /admin/blob/put` (scope `blob:put`) — base64 content + rel_path validation (no `..`, no leading `/`)
- `HandlerDeps.UserSecretResolveSvc *secretservice.SecretResolutionService` 新增；`adminDepsFromApp` 同步 wire
- `App.UserSecretResolveSvc` 与 `UserSecretSvc` 同时构造，复用 master key

### Dispatch envelope v2
- `DispatchEnvelope.HomeDir string \`json:"home_dir,omitempty"\`` 新增字段（v1 留空）
- `dispatch.Service.Dispatch` v2 path 从 `AgentResolution.HomeDir` 填值；v1 path 不变

### Worker daemon (`internal/workerdaemon/`)
- `AdminClient.ResolveSecret(ctx, name) ([]byte, error)` — base64 decode → plaintext
- `AdminClient.BlobPut(ctx, relPath, content) error` — base64 encode → POST
- `AdminClient.ReportArtifact` — 非空 blob 先走 `BlobPut(artifacts/<exec>/<kind>-<unix_ns>)`，再带 blob_ref 调 artifact/append
- `secret_resolver.go::adminClientSecretResolver` 适配 `SecretResolver` interface（`MCPInjector` 消费）
- `Runtime` 加 `skillLoader` + `mcpInjector` 字段；`NewRuntimeWithDeps(cfg, client, spawner, RuntimeDeps{SkillLoader, MCPInjector})` 新构造器，原 `NewRuntime` 仍兼容（默认 `StaticSkillLoader{}` + `NewMCPInjector(nil)`）
- `defaultAgentSpawner` 实质重写：fakeagent shortcut 不变；其它 agent_cli 走 `AssemblePrompt(loader, BaseSkill="worker-agent.md", HomeDir=env.HomeDir)` + `MCPInjector.Inject(env.HomeDir)` → 失败时 `ReportFailure(reason="secret_unresolvable")` + 立即返回
- `AgentRunnerConfig.MCPConfigPath` 新字段；非空时 `MCP_CONFIG=<path>` 注入子进程 env

### Daemon `main.go`
- `--skills-dir` flag 新增（real-agent 需要 worker-agent.md）
- 构造 `FSSkillLoader{FS: os.DirFS(skillsDir)}` + `NewAdminClientSecretResolver(client)` → `NewMCPInjector(...)` → `NewRuntimeWithDeps(...)`
- fakeagent flow 不变（不需要 skills 也不需要 MCP）

### CLI client parity
- `Client.SecretResolve(ctx, name) ([]byte, error)` — for test/future-CLI parity; CLI tokens generally lack `secret:resolve` scope

## § 2. 设计要点 / 决策

1. **plaintext-never-echo extended**：secret resolve response 用 std base64（不是 URL）是 wire format，不是加密。Server 不 log；client 收到立即解码 + 调用方负责 wipe（per ADR-0026 § 5）
2. **rel_path 校验定位在 transport 层**：blob.go::validateRelPath 拒 `..` / leading `/` / 空串；`/` 和 `\` 都切 segment 防 Windows escape。LocalDir.Put 还有 clean-join，是 defense in depth
3. **MCPConfigPath via env (`MCP_CONFIG=<path>`)** 而不是 CLI arg：MCP 通常通过环境变量传 config 路径，agent CLI 自己处理。子进程 env allowlist 保持原状，只追加 MCP_CONFIG
4. **fakeagent skips real-agent chain entirely**：script 已经是 source of truth，没必要 assemble prompt / inject MCP。spawner 显式 if-branch 区分而不是 fallthrough，避免 fakeagent 测试副作用污染 real-agent path
5. **HomeDir 空 → 优雅降级**：v1 envelope 没 HomeDir，prompt 退回 title+description（与旧行为一致），不调 MCPInjector。real-agent v2 envelope 必须有 HomeDir（resolver 填）
6. **ReportArtifact blob 上传现在是 prerequisite**：non-empty blob 先 BlobPut 再 append；之前 inline_size metadata-only 的兼容字段保留，避免 v2.2 测试 breakage
7. **`adminClientSecretResolver` 是 shim，不是 type union**：保持 `MCPInjector.SecretResolver` 接口稳定；将来 cross-host transport 换 client 时只改这一文件

## § 3. Scopes — 谁需要什么

新 scope（per token-immutable）：

| Caller | Scopes needed |
|---|---|
| Bootstrap (system) | `*` (自动写入 bootstrap_token) |
| Worker daemon | `dispatch:pull`, `secret:resolve`, `blob:put`, `task:*` |
| Operator CLI（普通） | `task:*` + 域 BC scopes（不持 `secret:resolve` / `blob:put`） |
| Admin CLI（minting） | `admin:token` |

`docs/design/implementation/DEPLOYMENT.md` 中 DEPLOY-OPS appendix 已加 scope note（见 § 5 below）。

## § 4. 验证

### Tests added
- `internal/admin/api/blob_test.go` — 7 tests: 200 round-trip + read-back, 1 MiB body, invalid rel_path (4 cases), invalid base64, 403 missing scope, 401 missing auth, 503 store unwired, plus `TestValidateRelPath` table-driven
- `internal/admin/api/secret_resolve_test.go` — 6 tests: 200 base64 round-trip, 403 wrong scope, 404 unknown, 403 revoked secret, 400 missing name, 501 svc unwired
- `internal/workerdaemon/runtime_real_agent_test.go` — 4 tests: real-agent AssemblePrompt + MCPInject side effects, Inject failure → secret_unresolvable, HomeDir-empty graceful degrade, AssemblePrompt picks up instructions.md
- `internal/workerdaemon/secret_resolver_test.go` — 2 tests: delegation happy path, nil client returns nil
- `internal/workerdaemon/adminclient_test.go` — `TestAdminClient_ReportArtifact` updated to expect blob_put + artifact_append; `TestAdminClient_ReportArtifact_EmptyBlobSkipsBlobPut` added

新 unit 测试：21 (8 + 6 + 4 + 2 + 1)。

### e2e spec added
- `tests/e2e/v2/tests/v23-real-agent-pipeline.spec.ts` — 1 test: deployed-binary验证 scoped tokens drive end-to-end:
  1. server up, bootstrap token read
  2. mint scoped tokens (worker = dispatch:pull+secret:resolve+blob:put+task:*; resolve-only = secret:resolve; task-only = task:*)
  3. UserSecret create → resolve with `secret:resolve` token → 200 base64; resolve with `task:*` token → 403
  4. BlobPut with `blob:put` token → 200 + FS round-trip; blob put with `task:*` → 403
  5. fakeagent dispatch via scoped worker token → exec=completed

> **决策**：原 brief 要求 e2e 用 fakeagent + 真 home_dir/mcp_config 路径，但 fakeagent 走 spawner shortcut 不读 MCP（per § 2 设计点 4）。改为：unit test 覆盖 real-agent spawner path（runtime_real_agent_test.go 已经proves spawner calls MCPInject + AssemblePrompt + 正确 args），e2e 覆盖 transport（scoped tokens 端到端通过新 endpoints）。两层加起来 = real-agent chain alive。

### Full-stack verification
- ✅ `go test ./...` — 全绿，无 regression
- ✅ `make lint` — vet / no-vendor-refs / no-mock-default / doc-impl-drift 全通过
- ✅ `make smoke` — v2.2 Phase D pipeline ~3s pass（worker daemon 新 `RuntimeDeps` 路径默认行为不变）
- ✅ e2e: `npx playwright test --project=chromium-mac`：15/15 passed（含新 v23-real-agent-pipeline.spec.ts）
- ✅ migration count 28 不变（本 ST 无 schema 改动）

## § 5. DEPLOYMENT 更新

`docs/design/implementation/DEPLOYMENT.md` § Token Scopes 表新增（按 caller 列）：

| Caller | Required Scopes | Notes |
|---|---|---|
| Bootstrap | `*` | Auto-minted at first server boot; rotate ASAP |
| Worker daemon | `dispatch:pull`, `secret:resolve`, `blob:put`, `task:*` | All 4 needed for real-agent dispatch; `task:*` covers report-progress/conclude/artifact/append etc |
| Admin CLI | `admin:token` | Mint / revoke other tokens |
| Regular CLI | `task:*` + per-BC scopes | NO `secret:resolve` (plaintext回放是 worker daemon 唯一职责) |

## § 6. Out of scope (deferred)

- AgentInstance.HomeDir as a write-time config field — 当前 home_dir 是 convention path (`~/.agent-center-worker/agents/<id>/`)。Operator 必须自己保证目录存在 + 填 `instructions.md` / `mcp_config.json`。Make-targets / installer 自动 scaffold 留给 v2.3-3c
- Real claude-code adapter e2e — needs 一个 LLM 或 vcr-style 录制
- BlobStore GET endpoint — 本 ST 只加 put（worker → center）；read path 仍走 server-local FS（artifact ref 通过 webconsole 暴露）
- Multi-host TCP variant of admin endpoint — task #27
- master_key rotation flow — task #30
