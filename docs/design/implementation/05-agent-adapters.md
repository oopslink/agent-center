# Agent CLI Adapters

> **实现层** · 把各家 agent CLI（Claude Code / Codex / OpenCode）的差异封进 `internal/agentadapter/` 包，给 worker daemon / per-execution shim 一个统一接口。
>
> **设计动机**：[ADR-0002](../decisions/0002-no-llm-sdk-use-cli-agents.md) 零 LLM SDK 依赖，所有 agent 走 CLI 子进程；[ADR-0018](../decisions/0018-detached-agent-via-per-execution-shim.md) per-execution shim 模式承载进程生命周期。
>
> 本文档 **元层规则 § 1-7 + Per-CLI 实现切片 § 8（claude-code 主力 + codex / opencode 接口预留）+ 测试策略 § 9 + ADR / Repository 对位 § 10**。

## § 1. 包定位

```
internal/
├── agentadapter/
│   ├── adapter.go            # Adapter interface（§ 2）
│   ├── event.go              # 统一 AgentTraceEvent schema（§ 3）
│   ├── lifecycle.go          # SpawnAgent / KillAgent 通用骨架（§ 4）
│   ├── claudecode/           # § 8.1
│   ├── codex/                # § 8.2 (TBD)
│   └── opencode/             # § 8.3 (TBD)
├── workerdaemon/             # 调 agentadapter
└── shim/                     # per-execution shim (ADR-0018)
```

**约束**（[ADR-0002 § 影响](../decisions/0002-no-llm-sdk-use-cli-agents.md)）：

- agentadapter 包**不依赖** anthropic / openai / 任何 LLM SDK（[NF1](../requirements/02-non-functional.md)）
- worker daemon / shim **不感知** CLI 差异，只调 Adapter interface
- 新增 agent CLI 走"加 subpackage + 注册"模式，不动 daemon / shim 代码

---

## § 2. Adapter Go Interface

> **v2.7+ UPDATE (2026-06-26)**: v2 G3 方法（Probe / SupportedFeatures / BuildMCPConfigArg / BuildSkillMountSetup）已从"新增扩展"晋升为核心接口方法——所有三个 adapter（claude-code / codex / opencode）均已实现完整接口。下述代码块反映实际 `internal/agentadapter/adapter.go` 的当前状态。

```go
// internal/agentadapter/adapter.go
package agentadapter

import (
    "context"
    "io"
    "time"
)

// Adapter 封装一家 agent CLI 的所有差异。
// 实现位于 internal/agentadapter/<cli-name>/。
type Adapter interface {
    // Name 返回 CLI 标识符（"claude-code" / "codex" / "opencode"）。
    Name() string

    // BuildCommand 把 SpawnRequest 翻译为具体 CLI 命令行 + env。
    // shim 拿这个 cmdline 跑 fork+exec。
    BuildCommand(req SpawnRequest) (CmdSpec, error)

    // ParseEvent 把 agent stdout 一行 JSONL 翻译成 AgentTraceEvent。
    // CLI 私有字段保留在 raw 里供归档。
    ParseEvent(line []byte) (AgentTraceEvent, error)

    // SupportsSession 报告本 CLI 是否支持 `--session-id` 类幂等会话标识。
    SupportsSession() bool

    // Probe 用于 capability 探测：worker daemon 每次 online 调；
    // 返回该 CLI 是否在本机可用 + 版本字符串。
    // 实现 = exec `<cli> --version` + 解析 stdout。
    Probe(ctx context.Context) (available bool, version string, err error)

    // SupportedFeatures 返回此 adapter 支持的 v2 高级特性。
    // DispatchService 用之校验 agent instance 配置兼容性。
    SupportedFeatures() FeatureSet

    // BuildMCPConfigArg 把 canonical mcp_config.runtime.json 路径
    // 翻译为该 CLI 接受的方式（cmd args / env / copy 到固定路径）。
    BuildMCPConfigArg(runtimeJSONPath string) (MCPSetup, error)

    // BuildSkillMountSetup 把 home_dir/skills/ 挂载给 CLI。
    BuildSkillMountSetup(homeDirSkills, execDir string) (SkillMountSetup, error)
}

// FeatureSet 描述 adapter 对 v2 高级特性的支持情况。
type FeatureSet struct {
    SupportsMCP     bool
    SupportsSkills  bool
    SupportsSession bool
}

// MCPSetup describes how a particular CLI consumes the canonical mcp_config.runtime.json.
type MCPSetup struct {
    Args   []string            // 追加给 CmdSpec.Args（如 ["--mcp-config", "/path"]）
    Env    map[string]string   // 追加给 CmdSpec.Env
    CopyTo string              // 若 CLI 只从固定路径读，spawn 前 copy runtime.json 过去
}

// SkillMountSetup describes how a particular CLI sees the home_dir/skills/ directory.
type SkillMountSetup struct {
    Mode     SkillMountMode    // CLIArg | SymlinkHomeClaude
    Args     []string
    Env      map[string]string
    PreSpawn func() error      // nil = no pre-action
}

type SkillMountMode int
const (
    SkillMountCLIArg SkillMountMode = iota
    SkillMountSymlinkHomeClaude
)

type SpawnRequest struct {
    ExecutionID   string
    Prompt        string            // 最终 prompt（worker daemon 已拼装好，§ 6）
    SystemPrompt  string            // v2.8.1: 可选持久 system instruction（--append-system-prompt）
    WorkingDir    string            // agent CWD（per-execution worktree）
    SkillFiles    []string
    AgentLogPath  string
    Env           map[string]string
    Timeout       time.Duration
}

type CmdSpec struct {
    Binary   string
    Args     []string
    Env      []string       // KEY=VALUE
    Stdin    io.Reader      // nil = 不喂 stdin
}
```

注册表使用 `Registry` struct（thread-safe `sync.RWMutex`），提供 `Register` / `Get` / `Names` 包级函数。子包 `init()` 自注册（如 `agentadapter/codex/adapter.go: func init() { agentadapter.Register(New("")) }`）。

---

## § 3. JSONL 行格式规范化

各 CLI 输出 JSONL 行格式不一，统一翻译为内部 schema：

```go
// internal/agentadapter/event.go
type AgentTraceEvent struct {
    Type        EventType         // 见下表
    Seq         int64             // adapter 端单调递增（per-execution）
    OccurredAt  time.Time         // wall clock（解析时打）
    ToolName    string            // Type == ToolCall / ToolResult 时填
    ToolInput   json.RawMessage   // Type == ToolCall 时填
    ToolOutput  json.RawMessage   // Type == ToolResult 时填
    Thinking    string            // Type == Thinking 时填
    TokensIn    int               // Type == TokensReport 时填
    TokensOut   int               // 同上
    Raw         json.RawMessage   // 完整原始行；归档用
}

type EventType string
const (
    EventThinking      EventType = "thinking"
    EventToolCall      EventType = "tool_call"
    EventToolResult    EventType = "tool_result"
    EventTokensReport  EventType = "tokens_report"
    EventTurnEnd       EventType = "turn_end"
    EventError         EventType = "error"
    EventUnknown       EventType = "unknown"      // CLI 输出了新类型，归档但不解释
)
```

### 3.1 Unknown event 处理（observable degrade，不吞）

CLI 升级新增事件类型时 adapter 不认识。按 [conventions § 17](../../rules/conventions.md) **错误不许吞** —— 必须显式处理：

| 步骤 | 动作 |
|---|---|
| 1. 归类 | 行入 `EventUnknown`，`Raw` 保完整原文 |
| 2. 归档 | 跟其它 event 一样 append 到 events.jsonl + BlobStore 归档 |
| 3. **显式上报** | worker daemon emit `agent_adapter.unknown_event_seen { adapter_name, cli_type_field, sample_raw, first_seen_at, message }` 到 events 表（[NF9 同事务双写](../requirements/02-non-functional.md)）|
| 4. 去重 | 同一 `(adapter_name, cli_type_field)` 在单次 execution 内只 emit 一次，防刷屏 |
| 5. Escalate | events 表 query：某 type 在 24h 内出现 ≥ 阈值 → supervisor invocation 评估升 adapter 优先级 |
| 6. Per-execution 上限 | 单 execution 内 ≥ N 条 Unknown → emit `task_execution.warning { reason=adapter_significantly_behind, message }`，supervisor 决策是否 kill 重派 |

**不阻断业务路径**（避免 CLI 升级 = worker 全停摆），**但显式可见** —— 运营 / supervisor 能看见，能 react，不是黑洞。

### 3.2 真正抛错的情况

| 情况 | 处理 |
|---|---|
| JSON 语法非法（缺括号 / 引号没闭等） | adapter 累计计数；单次 execution 内 ≥ N 条 → emit `task_execution.failed(reason=jsonl_parse_error)` |
| JSON 合法但 `type` 已知字段缺失到无法构造（如 `tool_use` 缺 `name`）| 降级为 `EventUnknown` + 走 § 3.1 路径；**不**直接 fail（结构 noisy 但有损信号有救） |
| Adapter 内部 panic（如类型断言失败）| `recover` + emit `task_execution.failed(reason=adapter_internal_error)` |

**TaskExecution projection** 由 worker daemon 边读规范化 event 边维护（[02-persistence-schema § 8.2](02-persistence-schema.md)）；agent trace event 本身**不进 events 表**（[ADR-0015](../decisions/0015-agent-trace-not-in-events-table.md)），但 § 3.1 的 `agent_adapter.unknown_event_seen` 是 worker daemon 自己的 domain event，**进 events 表**（它是 BC-level 信号，不是 agent trace）。

---

## § 4. 进程生命周期

实际 spawn / wait / kill 不由 Adapter 直接做 —— Adapter 只产 `CmdSpec`，由 **per-execution shim**（[ADR-0018](../decisions/0018-detached-agent-via-per-execution-shim.md)）执行：

```
worker daemon
    │ adapter.BuildCommand(req) → CmdSpec
    │
    ▼
spawn shim
    ./agent-center worker shim
      --execution-id=<ulid>
      --shim-token=<nonce>
      --cmd=claude
      --                              # 后续 args 为 CmdSpec.Args
      --output-format stream-json
      --session-id=<ulid>
      -p "<prompt>"
    │
    ▼
shim setsid + fork + exec agent CLI
    │
    ▼
agent 子进程独立于 worker daemon（detached）
shim 全职：
  - 监听 unix socket（接 worker daemon RPC）
  - 把 agent stdout 一行一行 append 到 events.jsonl
  - 跑 adapter.ParseEvent(line) 解析规范化
  - 收 daemon SIGTERM → 转发 agent SIGTERM，5s grace 后 SIGKILL
```

### 4.1 Per-execution 目录布局（[ADR-0018](../decisions/0018-detached-agent-via-per-execution-shim.md)）

```
/var/lib/agent-center/executions/<execution_id>/
├── envelope.json       # daemon 写：dispatch envelope 快照
├── status.json         # shim 边跑边写：current pid + start_time + status
├── events.jsonl        # shim append：原始 JSONL 行（带 seq）
├── agent.log           # agent stdout 完整原文（debug 兜底）
├── stderr.log          # agent stderr
└── shim.sock           # shim unix socket（daemon 重连入口）
```

execution 结束后归档：
- `events.jsonl` → BlobStore `tasks/<task_id>/<execution_id>/trace.jsonl.gz`
- `agent.log` → BlobStore `tasks/<task_id>/<execution_id>/log.log.gz`
- 24h 后整目录清理（保 disk）

### 4.2 Kill 时序

| 阶段 | 动作 |
|---|---|
| t=0 | center emit `task_execution.kill_requested` |
| t=0+ε | worker daemon 收事件 → 调 shim RPC `Kill(reason, message)` |
| t=0+2ε | shim 发 SIGTERM 给 agent |
| t≤5s | agent 正常退出 → shim wait → exit code 0 + 优雅终态 |
| t=5s | shim 发 SIGKILL（hard kill）|
| t≤10s | shim wait → exit code != 0 + reason=`killed` |
| 终态 | shim emit `task_execution.killed { reason, message }` → daemon 转 center |

**5s grace 时间硬编码**（写死，不做 per-CLI 配置）—— 大部分 agent CLI 收 SIGTERM 后保存 transcript 几百 ms 内完成，5s 足够；超时直接 SIGKILL 避免拖跨 worker。

---

## § 5. Session 管理多态

不同 CLI 对幂等会话标识支持不一：

| CLI | 支持 session-id? | 路径 |
|---|---|---|
| Claude Code | ✅ `--session-id=<ulid>` | 用 `execution_id` 当 session-id（execution 即一次会话）|
| Codex | ⏳ 待验证 | 若不支持，session resume 不可用，fallback 走"新 execution" |
| OpenCode | ⏳ 待验证 | 同上 |

`Adapter.SupportsSession() bool` 让 caller 知道：

```go
// internal/workerdaemon/dispatch.go
if !adapter.SupportsSession() {
    // shim 重启 / RPC 失联后无法 resume；走 task fail + new execution 路径
    log.Info("adapter does not support session resume; failover only via new execution")
}
```

**关于 retry / resume**：v1 不做 agent 内 retry；agent crash → shim → daemon → emit `task_execution.failed(reason=agent_crashed)` → supervisor 决策起新 execution（[ADR-0018](../decisions/0018-detached-agent-via-per-execution-shim.md)）。Session resume 仅用于 **shim ↔ daemon 失联重连**场景（[ADR-0018 § 4](../decisions/0018-detached-agent-via-per-execution-shim.md)），不是 agent retry。

---

## § 6. Prompt 拼装职责

Adapter **不**拼 prompt。prompt 由 worker daemon 拼好后通过 `SpawnRequest.Prompt` 传入。

拼装路径（[agent-harness/01-prompt-assembly § ...](../architecture/tactical/agent-harness/01-prompt-assembly.md)）：

1. worker daemon 装载 binary 携带的 `worker-agent.md` skill
2. 追加 `extra_skill_files`（envelope 携带）
3. 拼上 `constraints_extra` + `task_description`
4. 完整 string 作为 `SpawnRequest.Prompt` 喂 Adapter
5. Adapter 决定**怎么传给 CLI**（claude-code 用 `-p "$prompt"` flag；codex 可能用 stdin）

> **不在 prompt 里塞 project 本地 CLAUDE.md / AGENTS.md**：[conventions § 7](../../rules/conventions.md) 项目本地约定不进 agent-center，agent CLI 自己读项目目录 `AGENTS.md` / `CLAUDE.md`。

---

## § 7. 错误 reason 编码

`task_execution.failed` 的 reason 枚举（adapter 路径相关）：

| reason | 触发场景 |
|---|---|
| `agent_crashed` | agent 进程 abnormal exit（非 0 非 SIGKILL）|
| `agent_killed` | 收 SIGTERM/SIGKILL 后 exit |
| `shim_no_hello` | shim 启动后 N 秒内没 ShimHello（[ADR-0018](../decisions/0018-detached-agent-via-per-execution-shim.md)）|
| `shim_crashed` | daemon 探活发现 shim 进程已死 |
| `agent_cli_not_found` | binary 路径不存在 / exec 失败 |
| `prompt_too_long` | 拼装后 prompt 超 CLI 上限（adapter 检测） |
| `jsonl_parse_error` | adapter.ParseEvent 多次失败（非 Unknown，是非法 JSON）|

`message` 字段（[conventions § 16](../../rules/conventions.md)）携带具体进程 ID / exit code / 错误片段。

---

## § 8. Per-CLI 实现切片

### 8.1 Claude Code（v1 主力）

**Binary**：`claude`（PATH 上找 / config `agent_cli.claude_code.binary` 覆盖）。

**BuildCommand**：

```go
// internal/agentadapter/claudecode/adapter.go
func (a *Adapter) BuildCommand(req SpawnRequest) (CmdSpec, error) {
    args := []string{
        "--output-format", "stream-json",
        "--session-id", req.ExecutionID,
    }
    for _, p := range req.SkillFiles {
        args = append(args, "--skill", p)   // 假设 flag 名；落代码时跟 claude --help 校准
    }
    args = append(args, "-p", req.Prompt)

    env := os.Environ()
    for k, v := range req.Env {
        env = append(env, k+"="+v)
    }

    return CmdSpec{
        Binary: a.binaryPath,
        Args:   args,
        Env:    env,
        Stdin:  nil,                          // prompt 走 -p flag，不用 stdin
    }, nil
}

func (a *Adapter) SupportsSession() bool { return true }
```

**ParseEvent**（claude code stream-json 行格式 — 落代码时按 `claude --output-format stream-json` 实际输出对齐）：

```jsonl
{"type":"thinking","text":"..."}
{"type":"tool_use","name":"Bash","input":{"command":"..."}}
{"type":"tool_result","tool_use_id":"...","content":"..."}
{"type":"usage","input_tokens":1234,"output_tokens":567}
{"type":"end_turn"}
```

映射到 `AgentTraceEvent`：

| Claude type | EventType | 字段映射 |
|---|---|---|
| `thinking` | `EventThinking` | `Thinking = text` |
| `tool_use` | `EventToolCall` | `ToolName = name`, `ToolInput = input` |
| `tool_result` | `EventToolResult` | `ToolOutput = content` |
| `usage` | `EventTokensReport` | `TokensIn / TokensOut` |
| `end_turn` | `EventTurnEnd` | - |
| 其它 | `EventUnknown` | `Raw = 完整行` |

### 8.2 Codex（已实现 — `internal/agentadapter/codex/`）

> **v2.7+ UPDATE**: Codex adapter 已完整实现（validated on codex-cli 0.137.0）。BuildCommand / ParseEvent 对接 `codex exec --json` 行为。但 codex agents 仍被 `agent.cli` 白名单拒绝创建——adapter 目前仅在 tests 和 probe/discovery 中运行，未进入 runtime spawn 路径。

**Binary**：`codex`（PATH 上找）。

**BuildCommand**：

```go
args := []string{
    "exec",
    "--json",
    "--skip-git-repo-check",
    "--dangerously-bypass-approvals-and-sandbox",
}
if req.WorkingDir != "" {
    args = append(args, "-C", req.WorkingDir)
}
args = append(args, prompt) // prompt 是 positional argument
```

**ParseEvent**（codex exec --json JSONL 行格式）：

| codex type | EventType | 字段映射 |
|---|---|---|
| `turn.completed` | `EventTurnEnd` | `TokensIn / TokensOut`（usage 内嵌在 turn 边界）|
| `turn.failed` / `error` | `EventError` | - |
| `item.started` + `command_execution` | `EventToolCall` | `ToolName="shell"`, `ToolInput={command}` |
| `item.completed` + `command_execution` | `EventToolResult` | `ToolOutput=aggregated_output` |
| `item.completed` + `reasoning` | `EventThinking` | `Thinking=text` |
| 其它 | `EventUnknown` | `Raw` 保完整行 |

**FeatureSet**：`SupportsMCP=false`, `SupportsSkills=false`, `SupportsSession=false`（codex 用自己的 thread_id，不接受外部 session-id 设定）。

**Session 差异**：codex 自行 mint `thread_id`（thread.started event 返回），resume 走 `codex exec resume <thread_id>`——与 claude 的 `--session-id` 模式不同。捕获 thread_id + 发起 resume 属于 runtime wiring stage，不在 adapter。

### 8.3 OpenCode（stub — `internal/agentadapter/opencode/`）

> **v2.7+ UPDATE**: OpenCode adapter 已注册，Probe 已实现（`exec LookPath + --version`）。BuildCommand / ParseEvent 仍返回 `ErrNotImplemented`（stub）。FeatureSet 全 false。

**Binary**：`opencode`（PATH 上找）。接口完整实现但 BuildCommand / ParseEvent 返回 `ErrNotImplemented`，待 CLI 行为验证后填充具体翻译逻辑。

---

## § 9. 测试策略

### 9.1 Mock agent CLI

`testdata/mock-agents/<scenario>.sh` 是一个可执行脚本，模拟 agent CLI：

```bash
#!/usr/bin/env bash
# testdata/mock-agents/thinking_then_tool_call.sh
# 按 --session-id 等 flag 解析后输出 JSONL，sleep 模拟时延。
echo '{"type":"thinking","text":"let me check the file"}'
sleep 0.1
echo '{"type":"tool_use","name":"Read","input":{"path":"/tmp/foo"}}'
sleep 0.2
echo '{"type":"tool_result","tool_use_id":"x","content":"file contents"}'
echo '{"type":"end_turn"}'
```

Adapter 单测：

- `BuildCommand` 单测断言 args 顺序 / flag 正确
- `ParseEvent` 单测喂行→断言映射
- 集成测：spawn mock CLI，断言 events.jsonl 内容 + AgentTraceEvent stream

### 9.2 Per-CLI 兼容性矩阵

CI 跑 `tests/agent_cli_compat/` 套件，**每家真实 CLI** 在隔离 runner 上跑一组固定 prompt → 断言 JSONL schema：

| Scenario | claude-code | codex | opencode |
|---|---|---|---|
| simple thinking + end_turn | pass | pass (item.completed reasoning) | stub |
| single tool_call + result | pass | pass (command_execution) | stub |
| usage 字段存在 | pass | pass (turn.completed.usage) | stub |
| session_id 可重用 | pass | N/A (codex mints own thread_id) | stub |
| 超时 + SIGTERM 5s 内 exit | pass | TBD | stub |
| Probe version detection | pass | pass | pass |

兼容性矩阵 fail → 该 CLI 不计入支持列表。

---

## § 10. 与 ADR / Repository 对位

| 设计层来源 | 实现层落点 |
|---|---|
| [ADR-0002](../decisions/0002-no-llm-sdk-use-cli-agents.md) 零 LLM SDK | § 1 包定位 / 依赖约束 |
| [ADR-0018](../decisions/0018-detached-agent-via-per-execution-shim.md) per-execution shim | § 4 进程生命周期 + 目录布局 |
| [ADR-0015](../decisions/0015-agent-trace-not-in-events-table.md) trace 不进 events 表 | § 3 规范化 event → 走 BlobStore 归档 + projection 推送（[02-persistence § 8.2](02-persistence-schema.md)） |
| [NF8](../requirements/02-non-functional.md) 结构化 JSONL | § 2 / § 3 / § 9 |
| [agent-harness/01-prompt-assembly](../architecture/tactical/agent-harness/01-prompt-assembly.md) | § 6 prompt 拼装职责（在 daemon 不在 adapter）|
| TaskRuntime TaskExecutionRepository | adapter 不直接调；daemon 调 Repository 写状态（[01-task.md § 4](../architecture/tactical/task-runtime/01-task.md)） |
| Observability TaskExecutionProjectionRepository | adapter 解析的 event → daemon 推 projection（[observability § 5.2](../architecture/tactical/observability/00-overview.md)） |
| Observability TraceArchiveRepository | events.jsonl + agent.log 归档 BlobStore（[01-blob-store § 路径约定](01-blob-store.md)）|

---

> **本文档 scope**：实现层 Adapter 包定位 + 接口契约 + 进程生命周期 + 错误编码 + 三个 adapter 实现切片 + 测试策略。v2.7+ sweep (2026-06-26): § 2 接口更新为实际代码（含 SystemPrompt 字段 + Registry struct）；§ 8.2 codex 从 TBD 更新为已实现状态（validated codex-cli 0.137.0）；§ 8.3 opencode 从 TBD 更新为 stub 状态（Probe 已实现）。Per-CLI flag 真实细节以**该 CLI 当时的 `--help` 输出**为准；新增 CLI 走 § 1 "加 subpackage + 注册"模式。
