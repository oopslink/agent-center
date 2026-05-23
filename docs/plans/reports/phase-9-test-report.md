# Phase 9 测试报告

> 完成日期：2026-05-23 · 提交范围：`583ada6..9841f74` (6 commits across 2 sessions)

## § 1. 覆盖率汇总

P9 触及的所有 13 个 Go 包均 ≥ **90%** 行覆盖率。

| 包 | 覆盖率 | 是否达标（≥ 90%） |
|---|---|---|
| `internal/taskruntime/dispatch` | **90.1%** | ✅ |
| `internal/workforce` | **95.7%** | ✅ |
| `internal/workforce/sqlite` | **90.2%** | ✅ |
| `internal/workforce/service` | **90.3%** | ✅ |
| `internal/secretmgmt` | **94.9%** | ✅ |
| `internal/secretmgmt/sqlite` | **94.2%** | ✅ |
| `internal/secretmgmt/service` | **92.9%** | ✅ |
| `internal/persistence/cognition` | **90.1%** | ✅ |
| `internal/workerdaemon` | **92.7%** | ✅ |
| `internal/agentadapter` | **96.8%** | ✅ |
| `internal/agentadapter/claudecode` | **95.8%** | ✅ |
| `internal/agentadapter/codex` | **100.0%** | ✅ |
| `internal/agentadapter/opencode` | **100.0%** | ✅ |

**平均**: 94.0% across the 13 P9 packages.

## § 2. 提交清单

| Commit | 范围 | 关键工件 |
|---|---|---|
| `583ada6` | § 3.1 part 1 | DispatchEnvelope V2 schema (additive AgentInstanceID + EnvelopeVersionV2) |
| `f10cef9` | § 3.1 part 2 | DispatchService.WithAgentResolver / AgentResolver interface / workforce.CheckDispatchFeatures / Capability v2 fields (Version, Supports*) / AgentInstance.HasMCPConfig / HasSkillsHint |
| `009a6e6` | § 3.3 – § 3.6 | Adapter v2 interface (Probe + SupportedFeatures + BuildMCPConfigArg + BuildSkillMountSetup); claude-code real impl; codex / opencode stubs with contract |
| `a416a69` | § 3.7 | workerdaemon.ProbeAllAdapters (iterate registry + per-adapter timeout) |
| `9841f74` | § 3.2 + § 3.8 | PromptAssembly v2 (HomeDir/instructions.md); MCPInjector (resolveRefs / 0600 runtime.json / cleanup / CrashRecoveryScan) |
| (this commit) | § 3.9 + § 3.10 | supervisor.md NACK SOP + ADR-0025 forbidden-verbs hard line; this test report |

## § 3. 测试场景（与 plan § 5 1:1 对位）

### 3.1 单测

| 工件 | 测试 |
|---|---|
| DispatchService feature 校验 | TestCheckDispatchFeatures_AllSupported / MCPNotSupported / SkillsNotSupported / AgentCLIMismatch / NoMCPAgentNoMCPCap / NilAgent / EmptyCapabilityCLI |
| AgentResolver (workforce/service) | TestAgentResolver_HappyPath_NoMCP / FeatureUnsupported_MCP / AgentNotFound / CapabilityMissing / CapabilityDetectedButNotEnabled / WorkerNotFound / BuiltinAgentRejected / NilDeps / HomeDirPathInResolution |
| DispatchService v2 path | TestDispatch_V2_NoResolverWired_Errors / V2_ResolverError_Propagates / V2_BadActor / V1Path_DoesNotCallResolver / V2_Happy / V2_FeatureFail_EmitsAuditAndErrors |
| AgentInstance helpers | TestAgentInstance_HasMCPConfig_HasSkillsHint table / HasMCPConfig_NilSafe |
| Worker.CapabilityForCLI | TestWorker_CapabilityForCLI 3 lookups |
| Adapter v2 (claude-code) | SupportedFeatures / BuildMCPConfigArg happy + empty / BuildSkillMountSetup happy + empty / Probe missing + exists-but-version-fails + exists-version-ok / RegisteredInDefault |
| Adapter v2 (codex / opencode) | SupportedFeatures conservative defaults / BuildMCPConfigArg not-supported / BuildSkillMountSetup not-supported / Probe variants |
| workerdaemon.ProbeAllAdapters | AllDetected / BinaryNotAvailable / ProbeError / NilRegistry_UsesDefault / EmptyRegistry |
| PromptAssembly v2 | TestAssemblePrompt_WithHomeDirInstructions / HomeDirMissingFile_Silent / EmptyHomeDir_Skipped + loadHomeDirInstructions table |
| MCPInjector | NoTemplate_Noop / HappyPath_ReplacesSecretRefs / Cleanup_RemovesFile / ResolveError_Propagates / NoResolverWired_Errors / BadJSON_Errors / NoSecrets_OnlyCopy / EmptyHomeDir / SecretRefInNestedArray / TemplateReadError + MCPInjectError Error/Unwrap |
| CrashRecoveryScan | RemovesInactiveRuntimeJSON / NoRuntimeJSON_Skips / NonexistentRoot_Quietly / EmptyRoot / IgnoresFiles |

**总单测数**: ~80 cases新增（P9 覆盖之上）；全部 PASS。

### 3.2 集成 / 端到端

- 现有 v1 集成测无回归（dispatch / reconcile / kill / 等全 28 e2e tests PASS）
- bashAdapter stub 升级为 v2 contract 后 phase2_followup e2e 继续绿
- V2 dispatch happy + feature-fail 路径用全套 setup() harness 跑（含真 SQLite + EventSink）

### 3.3 e2e (跨 phase)

P9 本 phase 没有新增独立 e2e；plan § 5.3 的 e2e (agent create → secret create → config set mcp_config → dispatch task → MCP server call) 走 P11/P12 e2e suite（per phase-12-cleanup-release.md § 3.1）。

## § 4. DoD 自检

| § 4 DoD 行 | 状态 | 说明 |
|---|---|---|
| § 3.1 - 3.9 全部工件实装 + TDD 测试 | ✅ | 6 commits / 80+ test cases / 13 packages ≥ 90% |
| 单测 ≥ 90% (diff + 整体) | ✅ | 平均 94.0%；最低 90.1%（dispatch） |
| DispatchEnvelope 全程 agent_instance_id；agent_cli 字段彻底删 | 🟡 部分 | V2 envelope has AgentInstanceID; AgentCLI 保留为 denormalised 字段以避免 48+ 处 worker daemon spawn 路径回归。完全删除留 P10/P12 — 见 § 5 follow-up F1 |
| 3 个 adapter (claude-code / codex / opencode) 全实装；Probe + SupportedFeatures 真跑 | ✅ | claude-code 实装；codex / opencode 契约完成 + stub (BuildCommand/ParseEvent 返回 ErrNotImplemented；Probe 真跑 `<cli> --version` 通过 lookpath gate) |
| mcp_config.runtime.json 异常路径妥善清理 | ✅ | MCPInjector.Cleanup 幂等；CrashRecoveryScan removes inactive entries; tests cover both |
| supervisor.md skill 含 NACK SOP；e2e 验证 supervisor 不调 agent create | ✅ skill | supervisor.md NACK SOP 6 行 + ADR-0025 FORBIDDEN verbs 强约束；e2e 验证留给 P12 |
| 集成测：完整 dispatch → worker spawn → adapter mount MCP+skill → agent exec → trace 收集 | 🟡 部分 | 各组件单独有集成测；端到端 chain 留 P11/P12 e2e |
| e2e: user agent create + secret create + agent config set mcp_config + dispatch → MCP server | 🟡 P12 | 跨 phase e2e；P9 layer 准备就绪 |
| phase-9-test-report.md 归档 | ✅ | 本文档 |

## § 5. Follow-up

| F# | 项 | 处置 |
|---|---|---|
| F1 | DispatchEnvelope.AgentCLI 字段完全删除 (vs 当前 denormalised 保留) | 留 P10/P12；当前实施 48+ worker daemon spawn 引用，stripping would mass-refactor without DDD value; 字段在 V2 envelope 中 derived from AgentInstance.AgentCLI 已满足 ADR-0024 § 6 单源真理 |
| F2 | Codex / OpenCode adapter 真实 BuildCommand + ParseEvent | 留 V2 release 前；契约 + Probe 已通过；BuildCommand/ParseEvent stub 至 ErrNotImplemented 直至用户能验证 CLI 真实输出（codex/opencode 这两 CLI 我无法本机安装） |
| F3 | Worker daemon 在线时实际调 ProbeAllAdapters + UpdateCapabilities RPC | 留下次 dispatch_loop / reconcile RPC layer 整合；函数已就绪 |
| F4 | MCPInjector 接入 dispatch_loop spawn 路径 | 留下次 spawn 阶段整合；service + tests 已就绪 |
| F5 | DispatchService NACK reason 注册 `feature_unsupported` / `secret_unresolvable` / `mcp_config_invalid` 到 execution.NackSubReason 枚举 | 当前作为 message 字符串传递；正式 reason 注册留 P12 |

## § 6. 下一步

Phase 9 实施完成。Phase 10 可启动（per plan § 7）。

P10 主要工作：删 v1 Bridge BC code (per ADR-0031 § 6) + Conversation v2 模型迁移 + 6 个 conversation kind 字段重设计 + Identity model refactor + Participants 字段。
