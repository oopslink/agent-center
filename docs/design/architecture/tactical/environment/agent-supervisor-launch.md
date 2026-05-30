# Agent Supervisor 与启动模型（Environment BC）

> **状态（2026-05-30）**：**目录结构 / agent.yaml / 启动流程 = 定稿**；**auth & 隔离 & 权限模型 = 已验**（Tester ① 6/6 行为门全绿，commit `3ab158b`；见 §6）；**survive-reattach = GATE-7 Mode A（存活 + reattach）PASS**（真 (b) 统一-worker 路径）。本轮 auth 设计反复迭代三次、均以实测收敛。
>
> **关联**：[功能需求 F30 survive-reattach](../../../requirements/01-functional.md) · [survive-reattach 产品行为](../../../requirements/survive-reattach-product-behavior.md) · [v2.7 plan §2.5 Environment / §2.6 ClaudeCode headless](../../../../plans/v2.7-domain-refactor-plan.md) · [AgentInstance F24](../../decisions/0024-agent-instance-first-class.md)

## 1. 目的与范围

本文定义 Environment BC 中 **Agent 启动 + 存活（survive-reattach）** 的运行时设计：worker 如何启动 supervisor、supervisor 如何为 agent CLI 准备运行环境并拉起它、以及承载这些的**每-agent 目录结构**与 `agent.yaml` 交接契约。

核心约束（来自 F30 + @oopslink design intent）：

- **worker 重启/部署不打断在飞 agent**：agent 进程**存活**、worker 重新**接回**（reattach），不是杀+重起。这要求 supervisor 与 worker **生命周期独立**——supervisor **不以 worker 子进程方式启动**（detach，setsid），从而 worker 守护进程挂掉/重启时 agent 不随之退出。
- **worker 不碰 auth**：supervisor 为 agent **准备**启动环境；worker 守护进程的密钥**绝不**进 agent。
- **单机 v2.7、Darwin 优先**；跨机迁移不在本期。

## 2. 目录结构（每-agent 单树，supervisor 合并进 home）

参照 slock daemon 的实证布局（`~/.slock/agents/{id}/.slock/`），把 supervisor 控制面**合并进 agent home**，一棵树承载一个 agent 的全部（工作区 + 控制面 + 会话态）：

```
~/.agent-center/workers/{worker_id}/agents/{agent_id}/   ← agent home（v1 无可配 work_dir）
├── .supervisor/                  ← 启动 + 控制面（dir 0700）
│   ├── agent.yaml                worker 写 / supervisor 读：启动参数交接（§4）
│   ├── agent-env.json            worker 写（JIT 解析 secret）/ supervisor 注入 → agent env（中心 Profile.EnvVars，②）
│   ├── claude-mcp-config.json    MCP 配置（--mcp-config），cli 前缀命名
│   ├── claude-system-prompt.md   system prompt（--append-system-prompt-file），可选
│   ├── supervisor.sock           可重连 unix socket（reattach 接回点）
│   ├── supervisor.json           supervisor 身份：pid / instance_id / epoch / started_at（区分"真存活 reattach" vs "kill-relaunch"）
│   ├── session.epoch             reset-epoch {epoch, last_reset_version}
│   └── runtime-sessions/{cli}-{session_id}.jsonl   会话恢复态
├── config/  logs/  tmp/  memory/  运行支持目录
├── workspace/                    ← agent cwd（OQ7：活内容与支持目录物理分离）
└── skills/  mcp_config.json  instructions.md   AgentInstance 配置（F26/F27）
```

**为什么合并**：一棵树 = 一个 agent 全部 → 枚举 / GC / 排障简单（删 agent = rm 一棵树）；与 slock 实证一致；生命周期原子。supervisor 仍"有自己的工作目录"，即 `{home}/.supervisor/`。

**与 slock 的关键差异**：slock 的 daemon **直接**起 claude（启动参数在 daemon 内存），故其 `.slock/` **无启动参数文件**。我们 survive-reattach 要 **detached supervisor**（读不到 worker 内存）→ 故新增 **`agent.yaml`** 作为启动参数的落盘交接 —— 这是 slock 布局所无、而 detach 模型必需的一块。

## 3. 文件清单：owner / 权限 / 读写方

| 文件 | 权限 | writer | reader | 作用 |
|---|---|---|---|---|
| `.supervisor/`（dir） | 0700 | worker | supervisor | 控制面根 |
| `agent.yaml` | 0600 | worker | supervisor | 启动参数交接 |
| `agent-env.json` | 0600 | worker（JIT 解析） | supervisor（注入 agent env） | 解析后的中心 Profile.EnvVars（②） |
| `claude-mcp-config.json` | 0600 | worker（JIT） | agent CLI（`--mcp-config`） | MCP servers + 注入的 secret |
| `claude-system-prompt.md` | 0600 | worker | agent CLI | system / standing prompt |
| `supervisor.sock` | 0600 | supervisor（bind） | worker（reattach connect） | 重连控制通道 |
| `supervisor.json` | 0600 | supervisor（原子写） | worker（reattach 前读、判存活） | pid/instance_id/epoch/started_at |
| `events.jsonl` | 0600 | supervisor（drain stdout、append） | worker（reattach 后从 offset 读） | stdout offset-buffer（重连无缺口补发） |
| `session.epoch` | 0600 | supervisor（reset 时 bump） | supervisor（启动读） | {epoch, last_reset_version} |
| `runtime-sessions/{cli}-{sid}.jsonl` | 0600 | supervisor | supervisor（resume） | 会话恢复 |

**密钥边界**：`.supervisor/` 是控制面、agent（claude，bypassPermissions）与之同 home、能读它 → 故 `agent-env.json` 的 env **只放 agent 自己该有的**（中心下发的 Profile.EnvVars），**worker/admin 密钥绝不进**（由 ① env 白名单保证，§5）。最敏感的轮换密钥仿 slock 放**单独 0700 树、按路径引用**、不进 home（v2.7 暂无此类）。

**`agent-env.json` 生命周期**：**保留到 agent 终止**（0600）。survive-reattach 下 supervisor 在 crash/reset 重起 agent 时需**再次注入**，而 detached supervisor 够不到中心重解析 → 不像一次性 mcp runtime 那样 spawn 后即删。

## 4. `agent.yaml` schema

worker 在**启动 supervisor 前**写；reset / 重配时更新为权威态。

```yaml
schema_version: 1
ids:
  agent_id: "..."
  worker_id: "..."
  reconcile_version: 42          # 中心期望态版本，配 session.epoch 判 reset
cli:
  type: claude-code              # claude-code | codex | opencode
  binary_path: /abs/.../claude   # worker 自动发现解析出的绝对路径
  launch_args: [--model, sonnet] # adapter 生成，不含 secret
runtime:
  home_dir: "{home}"
  workspace_dir: "{home}/workspace"                    # agent cwd（OQ7）
  mcp_config_path: "{home}/.supervisor/claude-mcp-config.json"
  system_prompt_path: "{home}/.supervisor/claude-system-prompt.md"   # 可选
  env_file: "{home}/.supervisor/agent-env.json"        # 中心 Profile.EnvVars（②）
```

## 5. 启动流程（worker → supervisor → agent）

1. **worker** 准备 `.supervisor/`：写 `agent.yaml`（启动参数）+ `agent-env.json`（JIT 解析中心 Profile.EnvVars）+ `claude-mcp-config.json`（JIT 解析 MCP secret）+ system prompt。
2. **worker** 以**干净 env** 启动 supervisor（detached / setsid，**非子进程**），并把 **agent-cli 的路径注入 supervisor 的 `PATH`**。supervisor 自身 env = 系统 env 白名单过滤（无 worker 密钥，⑤ 防御）。
3. **supervisor** 读 `agent.yaml` → 据 `cli.type` 选 adapter → 用 `cli.binary_path` + `launch_args` + `runtime.*` 拉起 agent CLI（父子进程关系）；env = §5.1 构造；cwd = `workspace_dir`。
4. **supervisor** 持续 drain agent stdout 到 `events.jsonl`（offset-buffer），暴露 `supervisor.sock` 供 worker reattach；写 `supervisor.json` 身份。
5. **worker 重启**：读 `supervisor.json` 判 agent 仍存活 → 经 `supervisor.sock` **reattach**、从 `events.jsonl` 的 offset 续读（无缺口、不重复执行）。agent 进程从未退出。

### 5.1 agent 的运行环境（supervisor → agent CLI）

`BuildClaudeEnv` 两层 + 隔离（claudeenv.go）：

- **层 1 — 系统/继承 env 白名单过滤（default-deny）**：只放最小系统变量（`PATH/HOME/USER/...`）+ claude 自己的 auth 命名空间（`ANTHROPIC_*`、`CLAUDE_*`）。**剔掉**：`AGENT_CENTER_*` 等 worker 密钥（未白名单→默认丢）、`CLAUDE_CONFIG_DIR`、整个 `CLAUDE_CODE_*`（父 claude 的 SDK 嵌套标记 SESSION_ID/ENTRYPOINT/... + `CLAUDE_CODE_OAUTH_TOKEN`，见 §5.2 ③）、以及 `CLAUDECODE`（不匹配 `CLAUDE_` 前缀、默认丢）。
- **层 2 — 中心注入 env（`agent-env.json` / Profile.EnvVars，②）原样叠加**，**不过白名单**（中心有意注入的、可信的 per-agent env）。

## 5.2 Auth 与隔离模型 ✅（Tester 6/6 行为门已验 · commit `3ab158b`）

> 本节为本期 auth 设计的**结论**，结构经多轮实测纠偏（keychain 可用 → config-dir 是真凶 → HOME 覆盖也证伪 → 最终 `--setting-sources ""`）。**已经 Tester 6 点行为门 6/6 验证通过**（commit `3ab158b`、走 ① 真实路径；见 §6）。

- **① 认证 = keychain `/login`（订阅）**。v2.7 **只支持 `/login`**（token-via-中心env 路推迟）。关键实测事实：
  - claude-code 订阅 token 存 **macOS Keychain**（非文件）；**绑原始 HOME 路径**。
  - **任何对 `CLAUDE_CONFIG_DIR` 的设置、或对 `HOME` 的覆盖，都会切断 keychain `/login`**（claude 改走文件凭据模式 / 找不到绑定路径）。→ 故 **agent env 一律不重定位 `HOME`/`CONFIG_DIR`**（保住 keychain 前提）。
  - worker **不碰 auth**：operator 在 worker 机上 `/login` 一次（建立 keychain 订阅），非交互 agent 直接用。
- **② A-隔离（防 operator 个人 `~/.claude` 的 hooks/settings/SessionStart 污染 agent）= claude argv 加 `--setting-sources ""`**（就地不加载任何 settings 源）。**取代**早先"重定位 `CONFIG_DIR`/`HOME`"的老路——后者会断 keychain。隔离系于单一 argv flag，故配**双层 drift 守卫**：静态（argv 含 flag 恰一次，单元）+ 运行期（operator 反向 marker = 0，常驻 contract，plan §2.6）。
- **③ env 白名单**：`CLAUDE_CODE_OAUTH_TOKEN` 在层 1 **deny**（同其余 `CLAUDE_CODE_*`）。/login-only 下它无用，且留着会让 worker/运维 env 里的 ambient token 误透给 agent、覆盖 keychain（非确定）。**中心若要给 agent 发 token，走层 2（Profile.EnvVars）**，与层 1 deny 无关。
- **④ 权限姿态 = `bypassPermissions`（显式进 argv）**。理由（按威胁模型、非照搬 slock）：claude 权限系统不是我们的安全边界、headless 弹不了授权框。真边界在别处：
  - **本地工具**（file/bash）= worker 的**受限专用 OS 用户** + workspace cwd；
  - **MCP 工具** = `--strict-mcp-config` 限定 + 每次调用经 Center AppService、由 **§10 OQ6 域隔离服务端授权**（GATE-3）；
  - **env** = 层 1 白名单。
  - 分层：**claude 侧 bypass** 只保证"不被 claude 拦"（门6 验）；**MCP 的服务端域授权（OQ6）由 GATE-3 管**——两条独立。
  - **部署前提（进产品手册）**：bypass 安全 ⟺ worker 跑在权限受限专用 OS 用户（非 root/admin）。
- **简化**：不重定位 + keychain → **删除** `PrepareIsolatedClaudeConfig` / auth 文件拷贝那套（隔离 config dir、symlink 凭据）——keychain 处理 /login、不靠文件。

## 6. 验证状态 ✅（Tester 6/6 行为门全绿 · commit `3ab158b`）

走 ① 真实路径（`agent-center worker agent-supervisor`、env 由 `BuildClaudeEnv` / argv 由 `BuildStreamingArgv` 真构造、不手搭）：

1. ✅ keychain `/login` 认进 + 跑完一轮（authed-turn 正向，PONG / `is_error:false`；本机 keychain、无需额外凭据）；
2. ✅ 隔离两类（常驻反向 marker contract）：(settings/hooks) operator superpowers/slock SessionStart = 0；(MCP 源) operator `~/.claude.json` 实有 33 个 mcpServers，`--strict-mcp-config` 全排除、agent `mcp_servers=[]`；
3. ✅ 层 1 secret 过滤（注入 `AGENT_CENTER_ADMIN_TOKEN` → child env 0 个 `AGENT_CENTER_*`）；
4. ✅ `CLAUDE_CODE_OAUTH_TOKEN` deny（注入 → child env 0；其余 `CLAUDE_CODE_*` 亦 0）；
5. ✅ env 无 `CLAUDE_CONFIG_DIR`、`HOME` 原值透传；F27 skills 走 `--skill-path`、独立于 settings 源（前向）；
6. ✅ agent 真调通一个 MCP 工具：claude 发起 `mcp__agent-center__get_my_work` → tool_result `is_error:false`（`--setting-sources ""` + bypassPermissions + `--strict-mcp-config` 不破坏 MCP 执行）。

→ **①（env 安全 + A 隔离 + 权限姿态）= 无条件 GO。**

> **生产尾巴（不在 ① 范围）→ 已解**：Tester gate-6 发现 spawn-bug 两段同根因（`worker agent-supervisor` + `worker mcp-host`，standalone 二进制路由不了子命令）。@oopslink 已拍 **(b) 统一 `agent-center` 当 worker**（`worker run` 真 daemon 入口，`os.Executable()`=统一版 → 两段子命令都路由）；spawn-fix 已落（slice-1 `a81428a` + `--config` parity 修 `27ac198` + slice-3 退役 standalone `87e5988`），**GATE-7 Mode A（存活+reattach）PASS**。mcp-host 生产 tool-exec 复验随剩余 cutover 门（GATE-3）进行。① 的 argv/env/隔离/权限设计本身已 6/6 验证为对。
