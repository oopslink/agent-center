# 0030. AgentAdapter 矩阵扩展（v2 G3）

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-24 |
| Delivered | P9 § 3.3-3.6 — `009a6e6` (AgentAdapter v2 interface + claudecode / codex / opencode impls) |
| Related | v2 议题 G3（[v2-kickoff-2026-05-22 § Group 2 G3](../../drafts/v2-kickoff-2026-05-22.md)）；建立于 [implementation/05-agent-adapters](../../implementation/05-agent-adapters.md) v1 设计之上；接收 [ADR-0027 MCP per-agent](0027-mcp-per-agent-injection.md) + [ADR-0028 Skill File Mount](0028-skill-file-mount-lite.md) 注入到 adapter 层的职责 |

## Context

### v1 现状（[implementation/05-agent-adapters § 8](../../implementation/05-agent-adapters.md)）

| Adapter | v1 状态 |
|---|---|
| `claude-code` | ✅ 主力实装（stream-json + session-id 续接 + skill mount + kill 时序齐）|
| `codex` | 🔲 接口预留 stub，无 binary 接入 |
| `opencode` | 🔲 同上 stub |

竞品报告 § 4.3.1：Multica 支持 11 个 CLI；agent-center v1 只 1 个真正实装。差异化原因不在 CLI 多少，但**扩到 5-8** 是合理的「覆盖面 + 用户选择权」 投入。

### v2 决定

[ADR-0024 G1](0024-agent-instance-first-class.md) / [ADR-0027 G4](0027-mcp-per-agent-injection.md) / [ADR-0028 G5](0028-skill-file-mount-lite.md) 给 adapter 层加了新职责：

- **G1**：spawn 前解析 `home_dir/instructions.md` 加进 prompt 拼装
- **G4**：把 canonical `mcp_config.runtime.json`（MCP 标准 schema）**翻译**给 CLI（per-CLI flag / config 文件位置 / env 等）
- **G5**：把 `home_dir/skills/` **挂载**给 CLI（`--skill-path` 参数 / setenv HOME + symlink `~/.claude/skills` / 其他 per-CLI 约定）
- **Worker.capabilities**：自动探测 → 上报 detected；用户可 enabled=false 关掉某项

G3 要做的：

1. **更新 Adapter Go interface 契约**，纳入上面 3 类 per-CLI 翻译职责
2. **声明 v2 范围**：哪几个 CLI 在 v2 范围内真正写完代码
3. **Worker.capabilities 加 feature flags**：dispatch 校验链对位

## Decision

### 1. v2 实装范围（**3 个**）

| Adapter | v1 状态 | v2 期望状态 |
|---|---|---|
| `claude-code` | ✅ 实装 | ✅ 维持（按 G1/G4/G5 增量扩展契约方法）|
| `codex` | 🔲 stub | ✅ **v2 实装**（OpenAI Codex CLI）|
| `opencode` | 🔲 stub | ✅ **v2 实装**（SST OpenCode）|
| `gemini-cli` / `kimi` / 其他 | 未支持 | ❌ 不在 v2 范围；增量发布 / [roadmap](../../roadmap.md) |

> **「v2 实装」**：写完 Go subpackage `internal/agentadapter/<name>/` + 通过 [implementation/05 § 9 测试策略](../../implementation/05-agent-adapters.md)。**本 ADR 只定范围 + 契约；真正写代码在开发计划阶段**。

### 2. Adapter Go interface v2 扩展

**v1 现有**（[implementation/05 § 2](../../implementation/05-agent-adapters.md)）：

```go
type Adapter interface {
    Name() string
    BuildCommand(req SpawnRequest) (CmdSpec, error)
    ParseEvent(line []byte) (AgentTraceEvent, error)
    SupportsSession() bool
}
```

**v2 新增方法**（不破坏现有，纯加）：

```go
type Adapter interface {
    // v1 维持
    Name() string
    BuildCommand(req SpawnRequest) (CmdSpec, error)
    ParseEvent(line []byte) (AgentTraceEvent, error)
    SupportsSession() bool   // 仍保留；同时进 SupportedFeatures().SupportsSession（兼容查询）

    // v2 G3 新增

    // Probe 用于 capability 探测：worker daemon 每次 online 调
    // 返回该 CLI 是否在本机可用 + 版本字符串
    Probe(ctx context.Context) (available bool, version string, err error)

    // SupportedFeatures 返回此 adapter 支持哪些 v2 高级特性
    // dispatch 校验链用之
    SupportedFeatures() FeatureSet

    // BuildMCPConfigArg 把 canonical mcp_config.runtime.json 路径
    // 翻译为该 CLI 接受的方式（cmd args / env / copy 到固定路径）
    // 见 ADR-0027 G4
    BuildMCPConfigArg(runtimeJSONPath string) (mcpSetup MCPSetup, err error)

    // BuildSkillMountSetup 把 home_dir/skills/ 挂载给 CLI
    // 见 ADR-0028 G5
    BuildSkillMountSetup(homeDirSkills, execDir string) (skillSetup SkillMountSetup, err error)
}

type FeatureSet struct {
    SupportsMCP      bool
    SupportsSkills   bool
    SupportsSession  bool   // --session-id 续接；冗余于 SupportsSession() 方法但语义一致
}

type MCPSetup struct {
    Args   []string            // 追加给 CmdSpec.Args 的项（如 ["--mcp-config", "/path"]）
    Env    map[string]string   // 追加给 CmdSpec.Env
    CopyTo string              // 若 CLI 只读固定路径，把 runtime.json copy 过去（绝对路径）
}

type SkillMountSetup struct {
    Mode      SkillMountMode    // CLIArg | SymlinkHomeClaude
    Args      []string          // mode=CLIArg 时追加给 CmdSpec.Args
    Env       map[string]string // mode=SymlinkHomeClaude 时追加给 CmdSpec.Env（如 HOME=<exec-dir>）
    PreSpawn  func() error      // mode=SymlinkHomeClaude 时创建 ~/.claude/skills 符号链接；nil 表无需 pre-action
}

type SkillMountMode int
const (
    SkillMountCLIArg SkillMountMode = iota
    SkillMountSymlinkHomeClaude
)
```

> Worker daemon 在拼装 CmdSpec 时把 `MCPSetup.Args` + `SkillMountSetup.Args` 追加到 `CmdSpec.Args`；env 同理；CopyTo + PreSpawn 在 spawn 前执行。

### 3. Adapter 注册：Compile-time closed list

`internal/agentadapter/registry.go`：

```go
var Registry = map[string]Adapter{
    "claude-code": claudecode.New(),
    "codex":       codex.New(),
    "opencode":    opencode.New(),
}
```

- v2 不引入 plugin / 动态加载
- 加新 CLI = 加 subpackage + 注册一行 + 发新 binary
- 跟 agent-center「单 user / 单 binary 部署」模型一致

### 4. Worker.capabilities 加 feature flags

[ADR-0023 § 3](0023-worker-enroll-lightweight.md) 定义 `Worker.capabilities: [{agent_cli, detected, enabled}]`。v2 G3 扩展每项：

```
capability {
  agent_cli         enum     -- claude-code | codex | opencode | ...
  detected          bool     -- worker daemon Probe 结果
  enabled           bool     -- 用户可关
  version           string?  -- Probe 上报的 CLI 版本
  supports_mcp      bool     -- Adapter.SupportedFeatures().SupportsMCP
  supports_skills   bool     -- Adapter.SupportedFeatures().SupportsSkills
  supports_session  bool     -- Adapter.SupportedFeatures().SupportsSession
}
```

每次 online 探测时 worker daemon 调 `Adapter.Probe()` + `Adapter.SupportedFeatures()`，把结果填进 capability 上报 center。

### 5. Dispatch 校验链扩展

[task-runtime/00-overview § 3.1 DispatchService](../../architecture/tactical/task-runtime/00-overview.md) + [ADR-0011](../0011-dispatch-reliability-protocol.md) 的派单校验链加 feature 检查：

```
dispatch (task → agent_instance_id) {
  ... 现有校验 (state, agent_cli ∈ capabilities[detected ∧ enabled], capacity) ...

  // v2 G3 新增：feature 校验
  cap = worker.capabilities.where(agent_cli=agent_instance.agent_cli)
  if agent_instance.config.mcp_config 非空 AND !cap.supports_mcp:
    → NACK reason=feature_unsupported, message="adapter '<n>' does not support MCP"
  if agent_instance has attached skills (home_dir/skills/ non-empty hint) AND !cap.supports_skills:
    → NACK reason=feature_unsupported (软警告，可降级；worker daemon spawn 时若 mount 失败再 NACK)
}
```

[ADR-0011](../0011-dispatch-reliability-protocol.md) NACK reasons 加：

| reason | 含义 |
|---|---|
| `feature_unsupported` | adapter 不支持本派单要求的 feature（如 agent_instance.mcp_config 非空但 capability supports_mcp=false）|

### 6. 不在 v2 G3 范围

- ❌ Gemini-CLI / Kimi / Cursor / 其他 CLI 的 adapter 写代码 → 增量发布 / [roadmap](../../roadmap.md)
- ❌ Plugin-style 动态加载 → v2 不做
- ❌ Out-of-process adapter（adapter 跑独立进程）→ v2 不做
- ❌ Adapter 自身的 version 管理 / hot reload → v2 不做

### 7. 跟开发计划的关系

> 本 ADR 是**设计文档**。**真正写 codex / opencode adapter 代码**是开发计划阶段（v2 设计 6/6 闭环后开议题谈节奏 + 责任分工）。
>
> v2 范围承诺 = 「**在 v2 release 前**，`internal/agentadapter/codex/` + `opencode/` 完成代码实装 + 单元测试 + 集成测试（[implementation/05 § 9](../../implementation/05-agent-adapters.md)）」。

## Consequences

**正面**：

- Adapter 矩阵从 1 个实装扩到 3 个，给用户 CLI 选择权
- Adapter interface 统一吸收 G1/G4/G5 的 per-CLI 职责，center / agent-harness 不动
- Worker.capabilities 加 feature flags 让 dispatch 校验更精确（避免 spawn 后失败的 surprise）
- 跟 [v3+ AgentImage 模型](../../roadmap.md) 不冲突 —— image 模型会进一步把 adapter 选择内嵌到 image build；v2 adapter 矩阵是过渡形态

**负面 / 待跟进**：

- Adapter interface 增加 5 个方法，现有 claude-code 实装要补全（**实装工作量在开发计划**）
- 各 CLI 的 MCP / skill 支持方式各异，per-CLI 翻译细节占工程量大头
- Worker.capabilities schema migration（加 supports_* 字段）；v2 不考虑向后兼容直接重建
- gemini / kimi / 其他 CLI 用户可能在 v2 阶段觉得选项不够 —— 用 roadmap 列项缓解

## Alternatives Considered

### A. v2 直接实装 5 个（claude / codex / opencode / gemini / kimi）

- ✅ 一次覆盖广
- ❌ 工程量翻番；per-CLI 调研 + 翻译细节调通成本高
- ❌ 风险：实装质量被工程量拖垮；不如先稳 3 个再增量
- 否决（Q1=Q5=(a)）

### B. Plugin-style 动态加载 adapter

- ✅ 用户 / 第三方加 adapter 不用 patch 主仓
- ❌ Go plugin 跨平台坑多（Mach-O / ELF 各异）；v1 vision「单 binary 简洁」违和
- ❌ 安全：动态加载未审 adapter 风险大
- 否决

### C. Out-of-process adapter（adapter 独立进程，protobuf / JSON RPC 跟 center 交互）

- ✅ adapter 跟 center binary 解耦；可独立升级
- ❌ 工程量大（协议设计 + 进程管理 + 通信开销）；v2 个人工具不需要这层
- 否决（推迟到 v3+ AgentImage 模型时一起考虑）

### D. 不动 Adapter interface，新 feature 用 sidecar 函数

- 用 helper functions 处理 MCP / skill 翻译，不进 Adapter interface
- ✅ 不改契约
- ❌ per-CLI 差异散在多个 helper，没集中点；新 adapter 加进来要找所有 helper
- 否决：interface 显式更清晰

## References

### v1 设计

- [implementation/05-agent-adapters.md](../../implementation/05-agent-adapters.md) — Adapter 现有契约 + 测试 + per-CLI 切片

### 接收的 v2 ADR

- [ADR-0023 Worker Enroll 轻量化](0023-worker-enroll-lightweight.md) — capabilities 字段父结构
- [ADR-0024 AgentInstance 一等公民化](0024-agent-instance-first-class.md) — agent_cli 字段
- [ADR-0027 MCP per-agent 注入](0027-mcp-per-agent-injection.md) — adapter 层 BuildMCPConfigArg 职责
- [ADR-0028 Skill File Mount](0028-skill-file-mount-lite.md) — adapter 层 BuildSkillMountSetup 职责
- [ADR-0011 Dispatch Reliability Protocol](../0011-dispatch-reliability-protocol.md) — NACK reasons 表加 feature_unsupported

### 跨 BC

- [workforce/01-worker.md](../../architecture/tactical/workforce/01-worker.md) — Worker.capabilities 字段扩展
- [task-runtime/00-overview.md](../../architecture/tactical/task-runtime/00-overview.md) — DispatchService 校验链

### Roadmap

- [roadmap.md](../../roadmap.md) — gemini / kimi / 其他 CLI；AgentImage 模型（v3+ 把 adapter 选择内嵌 image）

### 来源

- [docs/design/drafts/v2-kickoff-2026-05-22.md § Group 2 G3](../../drafts/v2-kickoff-2026-05-22.md)
- [docs/research/competitive-analysis-2026-05-21.md § 4.3.1](../../../research/competitive-analysis-2026-05-21.md)（Multica 11 个 CLI matrix 对照）
