# Phase 9: Agent Runtime 扩展

> DDD TaskRuntime + Workforce (agent_cli adapter) · 依赖 Phase 8 · 解锁 Phase 11
> 纪律：按里程碑顺序 / 模块完备不半成品 / TDD + 单测 ≥ 90% + 集成 + e2e + 测试报告

覆盖 ADR：[ADR-0025 agent:create](../design/decisions/0025-agent-create-via-cli-not-protocol.md) · [ADR-0027 MCP per-agent 注入](../design/decisions/0027-mcp-per-agent-injection.md) · [ADR-0028 Skill File Mount](../design/decisions/0028-skill-file-mount-lite.md) · [ADR-0030 AgentAdapter 矩阵扩展](../design/decisions/0030-agentadapter-matrix-expansion.md)

## § 0. 目标

把 Phase 8 建好的 AgentInstance + SecretManagement 接入 worker 端 agent 运行时：

- **AgentInstance.config.mcp_config + skills home_dir** 实际生效（worker daemon spawn 时挂载）
- **3 个 agent CLI adapter（claude-code + codex + opencode）实装**（v1 仅 claude-code）
- **DispatchEnvelope.agent_instance_id 字段** 替代 v1 的 agent_kind；worker 端按 agent_instance_id join 出 agent_cli + home_dir
- **Worker.capabilities feature flags** (supports_mcp / supports_skills / supports_session) 由 adapter Probe + SupportedFeatures 上报；dispatch 校验链用之
- **G2 `agent:create` 协议** = G1 endpoint（Phase 8 已实装）+ supervisor Issue 流程对应 SOP（写进 supervisor.md skill）

## § 1. DDD 工件清单

### 1.1 Aggregate Roots（无新增）

- AgentInstance / Worker / UserSecret 等 Phase 8 已有
- TaskExecution 加字段（v1 已有）

### 1.2 Entities

- 无新增；Artifact / InputRequest 等 v1 已有

### 1.3 Value Objects

| VO | 来源 ADR | 备注 |
|---|---|---|
| **DispatchEnvelope**（重写）| 0027 | 删 `agent_cli` 字段；新 `agent_instance_id` 字段（per ADR-0024）|
| **MCPSetup** | 0030 | Adapter 返回；含 Args / Env / CopyTo |
| **SkillMountSetup** | 0030 | Adapter 返回；含 Mode (CLIArg \| SymlinkHomeClaude) + Args / Env / PreSpawn |
| **FeatureSet** | 0030 | SupportsMCP / SupportsSkills / SupportsSession bool |

### 1.4 Repositories

无新增；现有 TaskExecutionRepository / AgentInstanceRepository / UserSecretRepository 用 Phase 8 接口。

### 1.5 Domain Services

- **DispatchService**（[task-runtime/00 § 3.1](../design/architecture/tactical/task-runtime/00-overview.md)）：扩 feature 校验链（per ADR-0030 § 5）—— dispatch 前调用 Worker.capabilities[agent_cli] 看 supports_*，若 AgentInstance.config.mcp_config 非空但 capability supports_mcp=false → NACK reason=feature_unsupported
- **PromptAssemblyService**（worker 端 daemon 内部；[agent-harness/01](../design/architecture/tactical/agent-harness/01-prompt-assembly.md)）：扩 step 含 home_dir/instructions.md 拼装 + MCP 注入步骤（解析 SecretRef + 写 runtime.json + cleanup）
- **MCPInjectionService**（新；worker daemon 内）：负责调 SecretResolutionService + 写 runtime.json + 调 Adapter.BuildMCPConfigArg + spawn 后清理

### 1.6 Application Services（CLI handler）

- 现有 `agent-center dispatch` / `agent-center task new` 等 CLI 内部改：选 agent_instance_id 而非 agent_cli + worker_id
- G2 `agent-center agent create ...` Phase 8 已实装；本 phase 不动 CLI

### 1.7 Domain Events

| 事件 | 来源 ADR |
|---|---|
| `task_execution.failed { reason='feature_unsupported', ... }` | 0030（NACK reason 扩展）|
| `task_execution.failed { reason='secret_unresolvable', ... }` | 0027 |
| `task_execution.failed { reason='mcp_config_invalid', ... }` | 0027 |

### 1.8 Context Map 关系

- **TaskRuntime → SecretManagement**（Customer-Supplier；扩）：worker daemon spawn 前调 SecretResolutionService.resolve（per Phase 8 接口）
- **TaskRuntime → Workforce**（已有 Shared Kernel；扩）：dispatch envelope 加 agent_instance_id；worker 端 join AgentInstance

## § 2. 上游依赖（来自 Phase 8）

| 上游工件 | 用在本 phase 哪步 |
|---|---|
| AgentInstance AR + Repository (P8 3.5) | dispatch service 校验链 + worker 端 join |
| AgentInstance.config JSON 字段（含 mcp_config? / skills? / instructions_ref?） | P9 实装解析逻辑 |
| SecretRef VO Parse (P8 3.8) | worker daemon prompt-assembly 阶段调用 |
| SecretResolutionService (P8 3.7) | worker daemon spawn 前调用拿明文 |
| Worker.capabilities 字段（detected / enabled / supports_session 等）(P8 3.1) | dispatch 校验链 + adapter Probe 填充 |
| TaskExecution.agent_instance_id 列 (P8 3.0 migration) | DispatchService 写入 |

## § 3. 工作项分解

### 3.1 DispatchEnvelope schema 改 + DispatchService 校验链

- **工件**：`internal/taskruntime/dispatch_envelope.go` + `internal/taskruntime/dispatch_service.go`
- **依赖**：Phase 8 全部
- **步骤**：
  1. DispatchEnvelope struct 删 `agent_cli` 字段；加 `agent_instance_id`（per ADR-0027）
  2. DispatchService.Dispatch 校验链加 feature 检查（per ADR-0030 § 5）
  3. emit feature_unsupported NACK 用 ADR-0011 reasons
  4. 单测覆盖 6+ feature 校验路径
- **DoD**：所有 dispatch 路径通过 agent_instance_id；feature 校验 NACK 单测覆盖

### 3.2 PromptAssemblyService 重写（worker daemon 端）

- **工件**：`internal/workerdaemon/prompt_assembly.go`
- **依赖**：3.1 + Phase 8 AgentInstance + SecretRef
- **步骤**：
  1. 拼装流程加 step：解析 agent_instance_id → 查本机 AgentInstance + home_dir
  2. 装载 home_dir/instructions.md 加进 prompt 层次
  3. (G5) 解析 home_dir/skills/ 元数据传给 adapter BuildSkillMountSetup
  4. (G4) 解析 home_dir/mcp_config.json + 替换 SecretRef + 写 mcp_config.runtime.json (mode 0600)
  5. 调 adapter.BuildMCPConfigArg + BuildSkillMountSetup → 拼最终 CmdSpec
  6. spawn agent；execution 结束 unlink runtime.json
- **DoD**：完整 prompt 拼装流程跑通；runtime.json 妥善清理（异常路径也清）；单测 + 集成测覆盖

### 3.3 Adapter 接口 v2 扩展

- **工件**：`internal/agentadapter/adapter.go`
- **依赖**：Phase 8 capabilities 字段
- **步骤**：
  1. Adapter interface 加 v2 方法 Probe / SupportedFeatures / BuildMCPConfigArg / BuildSkillMountSetup（per ADR-0030 § 2）
  2. 新 VO types: FeatureSet / MCPSetup / SkillMountSetup / SkillMountMode
  3. 单测 mock adapter 覆盖
- **DoD**：interface 编译；mock adapter 完整

### 3.4 ClaudeCode Adapter v2 实装

- **工件**：`internal/agentadapter/claudecode/`
- **依赖**：3.3
- **步骤**：
  1. Probe: exec `claude --version` 解析
  2. SupportedFeatures: { MCP: true, Skills: true, Session: true }
  3. BuildMCPConfigArg: 返回 Args=["--mcp-config", "<path>"]
  4. BuildSkillMountSetup: Mode=CLIArg, Args=["--skill-path", "<home_dir>/skills/"]
- **DoD**：单测 + 集成测真跑 `claude --version`（CI 跳过 if not installed）

### 3.5 Codex Adapter 实装

- **工件**：`internal/agentadapter/codex/`
- **依赖**：3.3
- **步骤**：
  1. Probe: exec `codex --version`
  2. BuildCommand: codex 等价 CLI 参数（headless mode + JSONL output）
  3. ParseEvent: codex JSONL 格式 → AgentTraceEvent
  4. SupportedFeatures: 按 codex 真实能力填（MCP 是否支持需调研；本 phase 实装根据真实情况）
  5. BuildMCPConfigArg: 按 codex 接受的方式（环境变量 / config 文件 / 等）
  6. BuildSkillMountSetup: 按 codex 接受（很可能 fallback symlink HOME）
- **DoD**：所有 method 实装；JSONL parse 兼容 codex 真实 output；单测覆盖

### 3.6 OpenCode Adapter 实装

- **工件**：`internal/agentadapter/opencode/`
- **依赖**：3.3
- 同 3.5 结构；按 opencode 真实 CLI 实装
- **DoD**：同 3.5

### 3.7 Worker daemon Probe 流程（online 时）

- **工件**：`internal/workerdaemon/probe.go`
- **依赖**：3.4 - 3.6
- **步骤**：
  1. Worker daemon online 时遍历 registered adapters（compile-time map）
  2. 调每个 adapter.Probe + SupportedFeatures
  3. 把结果（含 features）整合成 Capability 数组上报 center
  4. center 端 WorkerRepository.UpdateCapabilities 写入
- **DoD**：online 时 capabilities 自动填充；单测 + 集成测覆盖

### 3.8 MCPInjectionService（worker daemon 端）

- **工件**：`internal/workerdaemon/mcp_injection.go`
- **依赖**：3.2 + Phase 8 SecretResolutionService
- **步骤**：
  1. Resolve all SecretRef in mcp_config.json
  2. Write mcp_config.runtime.json (mode 0600)
  3. PreSpawn hook
  4. Post-spawn cleanup hook
  5. Crash recovery: daemon 启动扫 home_dir/agents/*/mcp_config.runtime.json，对应 execution 已 inactive 的 unlink
- **DoD**：runtime.json 不留 dangling；secret resolve 错误 → NACK secret_unresolvable

### 3.9 supervisor.md skill 更新

- **工件**：`assets/skills/supervisor.md`（per ADR-0025 + 0030）
- **依赖**：所有 P9 完成
- **步骤**：
  1. 加 SOP「派单 NACK reason ∈ {agent_unavailable / capability_missing / agent_at_capacity / feature_unsupported / secret_unresolvable / mcp_config_invalid} 时的标准 Issue 流程」
  2. 加 SOP「严格禁止 supervisor 调 `agent create / config set / archive / secret create / 等`」（per ADR-0025）
- **DoD**：skill 文档 review；reflect ADR-0025 + 0030 完整

### 3.10 G2 验证（接口测试）

- G2 协议 = G1 endpoint，Phase 8 已实装。本 phase 验证 supervisor 派单失败时按 supervisor.md skill 行为开 Issue（不直接 create agent）。
- e2e 测试：模拟 dispatch 失败 → supervisor wake → open Issue（验证 NOT calling agent create）

## § 4. Definition of Done

- [ ] § 3.1 - 3.9 全部工件实装 + TDD 测试
- [ ] 单测 ≥ 90% (diff + 整体)
- [ ] DispatchEnvelope 全程 agent_instance_id；agent_cli 字段彻底删
- [ ] 3 个 adapter (claude-code / codex / opencode) 全实装；Probe + SupportedFeatures 真跑
- [ ] mcp_config.runtime.json 异常路径妥善清理（daemon crash recovery test）
- [ ] supervisor.md skill 含 NACK SOP；e2e 验证 supervisor 不调 agent create
- [ ] 集成测：完整 dispatch → worker spawn → adapter mount MCP+skill → agent exec → trace 收集
- [ ] e2e: user `agent create` + `secret create` + `agent config set mcp_config=...` + dispatch task → agent 跑通含 MCP server
- [ ] phase-9-test-report.md 归档

## § 5. 测试计划

### 5.1 单测场景

| 工件 | 场景 |
|---|---|
| DispatchService feature 校验 | agent_unavailable / capability_missing / agent_at_capacity / feature_unsupported 各 NACK reason |
| PromptAssemblyService | 完整 prompt 拼装；mcp_config 解析失败 → NACK；skills 目录空跳过 |
| ClaudeCode Adapter | Probe / SupportedFeatures / BuildMCPConfigArg / BuildSkillMountSetup |
| Codex Adapter | 同上 |
| OpenCode Adapter | 同上 |
| MCPInjectionService | resolve 成功 + 失败；runtime.json 写 + cleanup |

### 5.2 集成测试

| 场景 |
|---|
| Worker daemon online → Probe → capabilities 上报 → center 入 DB |
| Worker.config push (feature flag 改) → worker 重拉 |
| Dispatch 全链路（含 secret resolve）|
| MCP injection cleanup（execution 异常退 + daemon crash recovery）|

### 5.3 e2e 测试

| 场景 |
|---|
| `agent create` → `secret create` → `agent config set mcp_config=...` → `task new --agent=...` → agent 跑 MCP server 调成功 |
| dispatch 失败 capability_missing → supervisor 开 Issue（不 create agent）|
| codex / opencode 替代 claude-code 跑同一 task |

## § 6. 风险

| 风险 | 缓解 |
|---|---|
| codex / opencode 真实 MCP / skill 支持调研未明 | P9 启动时先做调研 spike；按真实情况填 SupportedFeatures；若不支持先 false + 后续 adapter 升级 |
| Adapter 间 JSONL 格式差异大 | ParseEvent 实现按 CLI 真实输出 fixture 覆盖；测试用 record-replay |
| MCP server 子进程管理（agent CLI 起 MCP server）| Adapter 不管 MCP server lifecycle，由 CLI 自管 |
| runtime.json 明文在 worker disk | mode 0600 + 短期落盘 + cleanup；workspace 加密 vault 是 v3+ |

## § 7. 下游解锁

本 phase 完成 → **Phase 10 启动**（phase 间严格串行）。

提供给下游：
- 完整 dispatch with agent_instance_id 接口
- MCP injection + Secret resolve 全链路
- 3 个生产可用 adapter
- supervisor 派单失败标准 SOP
