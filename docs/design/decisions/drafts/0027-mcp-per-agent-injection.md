# 0027. MCP per-agent 注入（G4）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-22 |
| Related | v2 议题 G4（[v2-kickoff-2026-05-22 § Group 2 G4](../../drafts/v2-kickoff-2026-05-22.md)）；建立于 [ADR-0024 AgentInstance 一等公民化](0024-agent-instance-first-class.md)（home_dir + config）+ [ADR-0026 SecretManagement BC](0026-user-secret-management-bc.md)（secret resolve）之上；与 [ADR-0001 不引入 MCP](../0001-no-mcp.md) **不矛盾**（详见 § Context）|

## Context

### ADR-0001 vs G4：两层 MCP 区分

| 层次 | 谁的 MCP | 立场 |
|---|---|---|
| **agent-center 暴露自己工具给 agent**（dispatch / query / open-issue / 等） | 把内部 CLI 包成 MCP server | [ADR-0001](../0001-no-mcp.md) ❌ 不引入 —— 走 Bash + CLI |
| **worker agent CLI 自己用的外部 MCP**（user 的 GitHub server / Filesystem server / Puppeteer 等） | claude-code / codex 等 CLI 内嵌的 MCP 客户端 | **本 ADR**：v2 允许用户 per-agent 配置 |

简言之：ADR-0001 不动；G4 加的是「让用户给他的 agent 配外部 MCP server」。

### 现状

[ADR-0024 AgentInstance 一等公民化](0024-agent-instance-first-class.md) 已经预留挂点：

- `AgentInstance.config.mcp_config` JSON 字段
- `home_dir/mcp_config.json` 路径
- 「细节归 G4 后续讨论」

G4 = 把这个挂点设计完整 + 集成 [ADR-0026 SecretManagement](0026-user-secret-management-bc.md) 的 secret 解析。

## Decision

### 1. mcp_config 存储模型

| 维度 | 内容 |
|---|---|
| **存储位置** | `AgentInstance.config.mcp_config`（JSON，center DB 主导，[ADR-0024](0024-agent-instance-first-class.md)）|
| **Schema** | 直接吃 [MCP 标准 schema](https://modelcontextprotocol.io/)（也是 claude-code / codex / opencode 大致共通）|
| **Secret 引用** | `env` 字段内值如 `secret:<name>`（[ADR-0026](0026-user-secret-management-bc.md) SecretRef 语法）|

```json
{
  "mcpServers": {
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": { "GITHUB_TOKEN": "secret:github-pat" }
    },
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/Users/oopslink/notes"]
    }
  }
}
```

### 2. 修改 CLI

```bash
# 改 mcp_config（整段替换；JSON 校验）
agent-center agent config set <name> mcp_config=<json>

# 列示当前 mcp_config（**不解析 SecretRef**；输出含 secret:xxx 占位）
agent-center agent show <name>
```

### 3. 同步协议（沿用 ADR-0024）

- 用户改 config → center 写 AgentInstance.config 字段 + emit `agent_instance.config_updated`
- 在线 worker 收长连接事件 → 重拉 config → 重写本机 `home_dir/mcp_config.json`（模板版，**含 SecretRef，不含明文**）
- Offline worker：不推；重连 reconcile 拉
- **变更不影响当前正在跑的 execution**（home_dir 在 execution 期间只读，[ADR-0024](0024-agent-instance-first-class.md) § 5）

### 4. dispatch envelope 不携带 mcp_config

DispatchEnvelope **不加** `mcp_config` 字段：

- 模板已落 worker 本地 `home_dir/mcp_config.json`（同步好的）
- envelope 保持轻
- 没有 per-task override 的设计冗余（v2 不要）

### 5. Worker daemon 注入流程

```
worker daemon 收到 DispatchEnvelope:
  ... (现有 ADR-0018 shim 流程) ...

  Prompt 组装阶段（agent-harness/01-prompt-assembly.md）:
    1. 解析 home_dir/mcp_config.json (template, with SecretRef)
    2. 对每个 secret:<name> 引用:
       a. 调 center API SecretResolutionService.resolve(name, agent_instance_id)
          → 返回明文 plaintext
       b. 内存里替换 env value 的 SecretRef → plaintext
    3. 写 home_dir/mcp_config.runtime.json (含明文, mode 0600)
    4. 调对应 agent adapter:
       - claude-code: 用 --mcp-config 参数指向 mcp_config.runtime.json
                     （或 claude-code 自己读约定路径）
       - codex: 翻译为 codex 的 MCP 配置格式（adapter 层处理）
       - opencode: 同上
       - gemini: 同上
    5. spawn agent CLI

  execution 结束（shim goodbye / 进程死）:
    6. daemon unlink home_dir/mcp_config.runtime.json

  异常路径（worker 重启 / crash）:
    7. daemon 启动时扫 home_dir/agents/*/mcp_config.runtime.json 主动清理（除非该 execution 仍 active）
```

### 6. Adapter 层翻译

各 agent CLI 的 MCP 配置约定不同；canonical schema = MCP 标准；adapter 层做 per-CLI 翻译：

| CLI | 翻译 |
|---|---|
| claude-code | 直接吃 MCP 标准 JSON；通过 `--mcp-config <path>` 或约定路径 |
| codex | （待 adapter 实装时定）|
| opencode | （待 adapter 实装时定）|
| gemini | （待 adapter 实装时定）|

详见 [implementation/05-agent-adapters.md](../../implementation/05-agent-adapters.md)（adapter 实装在 G3 议题后扩展）。

### 7. NACK reasons 扩展

dispatch 时 worker daemon 在 prompt-assembly 阶段调 SecretResolutionService.resolve 可能失败：

| reason | 含义 |
|---|---|
| `secret_unresolvable` | 至少一个 `secret:<name>` 解析失败（not_found / revoked / not_authorized）|
| `mcp_config_invalid` | mcp_config JSON schema 不合法（非 MCP 标准格式）|

加入 [ADR-0011](../0011-dispatch-reliability-protocol.md) NACK reasons 表。

### 8. 安全约束

- worker daemon 写 `mcp_config.runtime.json` 用 mode 0600
- 明文跨进程边界：worker daemon → agent CLI 子进程（通过环境变量或文件，由 adapter 决定）
- agent 进程**不需要也不应该**通过其他途径直接读 secret 明文（明文进 agent CLI 是必要的，因为 MCP server 需要它）
- worker daemon **不**把 mcp_config.runtime.json 上报 center / 不写 trace / 不写 artifact

## Consequences

**正面**：

- 用户可 per-agent 配 MCP server，agent 用 GitHub / Filesystem / 等外部能力扩展
- mcp_config 在 center DB **不含明文**（仅 SecretRef）—— 用户可以放心 `agent show` / git diff / 截图分享
- 明文仅 worker daemon 短暂持有；execution 后清理
- 跟 G1/G2/G3/G5 完全兼容
- adapter 层吸收 per-CLI 差异，center / agent-harness 不动

**负面 / 待跟进**：

- worker daemon spawn 前增加 N 次 secret resolve RPC（N = 引用的 secret 数量）；性能影响微小（毫秒级 + DB 一次解密）
- mcp_config.runtime.json 短暂落盘 = 攻击面（mode 0600 + cleanup 缓解；不彻底无解，得接受）
- adapter 层 per-CLI 翻译要实装；G3 议题（adapter 矩阵扩展）时一起做

## Alternatives Considered

### A. envelope 直带 mcp_config（v2-kickoff 原想法）

- DispatchEnvelope 加 mcp_config 字段；supervisor 派单时打包
- ❌ Supervisor 不应触碰 secret（[ADR-0026](0026-user-secret-management-bc.md) 授权矩阵）
- ❌ Per-task override 是 over-engineering
- 否决：home_dir-based 自然分层

### B. agent-center 自己定义 schema（不直接吃 MCP 标准）

- ✅ 可控
- ❌ 重新发明轮子；MCP 标准已稳定；CLI 升级时翻译双轨
- 否决：吃标准

### C. mcp_config 明文存 DB（不引入 SecretManagement BC）

- ✅ 工程小
- ❌ git diff / 截图分享会泄露
- ❌ rotate 困难
- 否决：用户明确要中心化 secret

### D. 永久落盘 mcp_config.runtime.json（含明文，execution 后不清理）

- ✅ 性能更好（不用每次 spawn 都 resolve）
- ❌ 攻击面大；worker 文件系统长期持有明文
- 否决：just-in-time 清理是默认安全姿态

## References

### 相关 ADR

- [ADR-0001 不引入 MCP](../0001-no-mcp.md)（agent-center 自身工具不走 MCP，两层不冲突）
- [ADR-0011 Dispatch Reliability Protocol](../0011-dispatch-reliability-protocol.md)（NACK reasons 表扩展）
- [ADR-0018 detached agent via per-execution shim](../0018-detached-agent-via-per-execution-shim.md)（spawn 流程沿用）
- [ADR-0024 AgentInstance 一等公民化](0024-agent-instance-first-class.md)（home_dir + config）
- [ADR-0026 SecretManagement BC](0026-user-secret-management-bc.md)（SecretRef + resolve 协议）

### 跨 BC

- [workforce/04-agent-instance.md § 2](../../architecture/tactical/workforce/04-agent-instance.md) — AgentInstance.config.mcp_config 字段
- [agent-harness/01-prompt-assembly.md](../../architecture/tactical/agent-harness/01-prompt-assembly.md) — worker daemon 注入流程
- [implementation/05-agent-adapters.md](../../implementation/05-agent-adapters.md) — adapter 层 per-CLI MCP 翻译

### 来源

- [docs/design/drafts/v2-kickoff-2026-05-22.md § Group 2 G4](../../drafts/v2-kickoff-2026-05-22.md)
- [docs/research/competitive-analysis-2026-05-21.md § 4.3.2](../../../research/competitive-analysis-2026-05-21.md)（Multica 的 MCP 服务器注入对照）
