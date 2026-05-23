# 0038. CLI UX 增强（v2 W2）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-23 |
| Related | v2 议题 W2；触发自 [ADR-0031 v2 Drop Bridge](0031-v2-drop-bridge-vendor-integration.md)；跟 [ADR-0037 W1 Web Console](0037-web-console-as-main-user-ui.md) **兄弟前端**，CLI 与 Web Console 等价；v2 设计阶段最后一项 |

## Context

[ADR-0031](0031-v2-drop-bridge-vendor-integration.md) 撤回 Bridge / 飞书后，v2 用户接入面 = **Web Console + CLI**。[ADR-0037 W1](0037-web-console-as-main-user-ui.md) 升级 Web Console；W2 对位升级 CLI，让 **任何 Web Console 能做的事，CLI 同样能做**。

### 现有 CLI 已设计 / 已实装的命令

| 命令组 | 来源 ADR |
|---|---|
| `channel create / list / show / archive` | CV1 (ADR-0032) |
| `channel invite / leave / kick / participants` | CV2b (ADR-0034) |
| `conversation show / refs` | CV3 (ADR-0035) |
| `issue open / task new --from-conversation=...` | CV4 (ADR-0036) |
| `agent create / list / show / config set / archive` | G1 (ADR-0024) |
| `worker token issue/reissue/revoke/list` / `join` / `config set` / `capability enable/disable` / `proposal list/unignore` | E1 (ADR-0023) |
| `secret create / list / rotate / revoke / usage` | G4 (ADR-0026) |
| `inspect task / issue / event / etc.` | v1 现有 |
| `dispatch / kill-execution / abandon-task / suspend-task / resume-task` | v1 现有 |

### v2 缺什么（vendor 撤回后的新需求）

| 缺 | v1 时的入口 | v2 CLI 必须补 |
|---|---|---|
| 发消息到 conversation | 飞书 thread 发消息 | `conversation send` / 别名 `channel post` / `issue comment` / `task comment` |
| Tail conversation 实时跟随 | 飞书 thread 红点 / 卡片 | `conversation tail [-f]` |
| 回 InputRequest | 飞书卡片按钮 | `input-request respond` |
| 列 InputRequest 待办 | 飞书消息 | `input-request list [--pending]` |
| 看 InputRequest 详情 | 飞书卡片 | `input-request show` |
| 取消 InputRequest | 飞书取消按钮 | `input-request cancel` |
| 看 DM / agent 私聊 | 飞书 DM | `conversation show dm:<id>`（CV3 已设计）|

## Decision

### 1. 命令风格：按 entity 分组

CLI 命令按 entity 分组（跟现有惯例一致）：

```
agent-center
  ├── channel <subcommand>           # CV1 / CV2b
  ├── conversation <subcommand>      # CV3 通用
  ├── issue <subcommand>             # CV4 + v1
  ├── task <subcommand>              # CV4 + v1
  ├── input-request <subcommand>     # v2 新（vendor 替代）
  ├── agent <subcommand>             # G1 / G5
  ├── worker <subcommand>            # E1
  ├── secret <subcommand>            # G4
  ├── inspect <subcommand>           # v1 已有
  ├── dispatch / kill-execution / etc.  # v1 已有 verb-style
  └── server <subcommand>            # v1 已有 (server start / etc.)
```

### 2. 发消息：通用 + 别名

| 命令 | 用途 |
|---|---|
| **`agent-center conversation send <conv-id-or-name> <text>`** | canonical 通用 |
| `agent-center channel post <name> <text>` | alias → conversation send（限 kind=channel）|
| `agent-center issue comment <id> <text>` | alias → conversation send（限 kind=issue；同时校验 content_kind 默认 text）|
| `agent-center task comment <id> <text>` | alias → conversation send（限 kind=task）|

**`<text>`**：可用 `-` 表 stdin（pipe 输入），或 `--file=<path>`。

CLI 内部统一走 `conversation send` 实现；别名只是 ergonomic 包装 + kind 校验。

校验（per CV2b 严格 join）：调用 actor 必须是该 conversation 的 active participant。

### 3. Realtime tail

```
agent-center conversation tail <conv-id> [--follow|-f] [--since=<time>] [-n <count>]
```

| Flag | 用途 |
|---|---|
| `--follow`, `-f` | 类似 `tail -f`：保持 streaming，新 message 实时打印；Ctrl-C 停 |
| `--since=<time>` | 起始时间（ISO8601 / 相对如 `-1h`）|
| `-n <count>` | 显示最近 N 条（默认 50）|

**实现策略**：CLI 通过 **admin endpoint poll**（默认 1s 间隔；或 long-polling）—— 不通过 Web Console SSE。保持 CLI ↔ Web Console 兄弟独立。CLI 跟 server 的 admin endpoint 直连。

### 4. InputRequest 命令组

```
agent-center input-request list [--pending] [--task=<task-id>] [--format=table|json|yaml]
agent-center input-request show <ir-id> [--format=...]
agent-center input-request respond <ir-id> <answer-text>     # 可 -t / --text=<txt> / 或 stdin
agent-center input-request cancel <ir-id> [--reason=<r>]      # owner / supervisor 可用
```

**示例**：

```
$ agent-center input-request list --pending
ID                              TASK    AGENT              QUESTION                        AGE
ir-01HE8ABC...                  T-42    agent:coder-mbp    "需要 confirm dispatch 重试?"   5min
ir-01HE8DEF...                  T-43    agent:reviewer     "PR description 要不要..."      12min

$ agent-center input-request respond ir-01HE8ABC... "yes"
✓ Responded to ir-01HE8ABC...; task T-42 unblocked.
```

### 5. 输出格式：`--format` flag

| 格式 | 默认 / 用途 |
|---|---|
| `table` | 默认；人类可读；列名 + 行分隔 |
| `json` | 脚本化 / pipe |
| `yaml` | 跟 config 兼容 |

适用于：`list` / `show` / `participants` / `refs` / `inspect` 等 **查询类命令**。**操作类命令**（create / dispatch / send / etc.）仍输出简单 status line。

### 6. Stdin / 文件输入约定

跟 v2 ergonomic 通用：

| 命令 | stdin / 文件 输入支持 |
|---|---|
| `conversation send <conv> <text>` | `<text>` = `-` 表 stdin；`--file=<path>` |
| `issue open --description=<text>` | 同上 |
| `task new --description=<text>` | 同上 |
| `agent config set <name> <key>=<value>` | `=@<file>` 表从文件读 |
| `channel create --description=<text>` | 同上 |

→ 减少 CLI 长 string 在 shell 里逃逸字符的痛苦。

### 7. CLI / Web Console naming consistency

设计原则：CLI 命令名跟 Web Console UI 标签一致。

| Web Console UI 标签 | CLI 命令 |
|---|---|
| 「Channel」「Create channel」 | `channel`, `channel create` |
| 「Send message」 | `conversation send` / `channel post` |
| 「Invite」 | `channel invite` |
| 「Reply」 | `input-request respond` |
| 「派生 Issue from messages」 | `issue open --from-conversation=... --select-messages=...` |
| 「Archive」 | `channel archive` |
| 「Live」 | `conversation tail -f` |

### 8. 跟 W1 SSE 的分工

| 通道 | 实时机制 |
|---|---|
| Web Console（W1）| SSE（HTML5 标准；HTMX/SPA 原生支持）|
| CLI（W2）| Admin endpoint polling（默认 1s）或 long-polling |

→ 两个通道实现各自最适合的机制；不互相依赖。

### 9. CLI Help / Discoverability

| 命令 | 用途 |
|---|---|
| `agent-center --help` | 全命令树 |
| `agent-center channel --help` | channel 组子命令 |
| `agent-center input-request --help` | InputRequest 组子命令 |
| `agent-center help <command>` | 同 `<command> --help` |
| `agent-center version` | 版本 |

### 10. Bash / Zsh completion（推迟）

> 推迟到开发计划阶段；ADR 范围声明「v2 不必发 bash completion 脚本；v3+ 加」。

## Consequences

**正面**：

- CLI 完全 cover v2 用户日常操作；vendor 撤回后零依赖
- 命令风格统一（entity 分组）；用户跨命令组体验一致
- 通用 + 别名设计减少学习成本（`channel post` / `issue comment` 都是 `conversation send` 的别名）
- 跟 W1 Web Console 命名一致；用户在 GUI / CLI 之间切换自然
- 输出格式 `--format` 让 CLI 脚本化友好（infra-as-code 场景）

**负面 / 待跟进**：

- CLI 命令树规模扩大；docs / help / completion 维护成本 ↑
- `conversation tail -f` polling 实现性能 v2 OK；高频场景 v3+ 可换 long-polling / WebSocket
- 各命令的 `--format` 实现要测；JSON / YAML serialization 要保 schema 稳定

## Alternatives Considered

### A. 命令按 action 分组（`agent-center send / list / show`）

- ❌ `send` 跨 entity 含义不清；user 必须额外指定 entity kind
- ❌ 不跟现有 ADR 设计的命令风格（按 entity）一致
- 否决

### B. 发消息只在 per-kind 命令（无通用 conversation send）

- ❌ DM / adhoc / notification 等 kind 没专门别名；命令缺
- ❌ 通用 send 是 canonical；alias 是 ergonomic
- 否决

### C. CLI 通过 Web Console SSE 实现 tail

- CLI start subscribes Web Console SSE endpoint
- ❌ CLI 依赖 Web Console；如果用户不启 Web Console 则 CLI 不可用
- ❌ 兄弟前端原则违和
- 否决（直接 admin endpoint）

### D. 不补 InputRequest CLI（用户必须通过 Web Console 回）

- ❌ vendor 撤回后没有 CLI 入口 = SSH 远程用户无法回；不能接受
- 否决

## References

### v2 ADRs

- [ADR-0031 v2 Drop Bridge](0031-v2-drop-bridge-vendor-integration.md) - 触发本议题
- [ADR-0037 W1 Web Console](0037-web-console-as-main-user-ui.md) - 兄弟前端
- [ADR-0032 / 0034 / 0035 / 0036](0032-conversation-channel-as-first-class.md) - CV1-CV4 中已设计的 CLI 命令均吸纳
- [ADR-0023 / 0024 / 0026](0023-worker-enroll-lightweight.md) - E1 / G1 / G4 中已设计的 CLI 命令均吸纳

### 已有

- [agent-harness/02-skill-cli-tooling.md](../../architecture/tactical/agent-harness/02-skill-cli-tooling.md) - v1 CLI 设计；v2 阶段实施时增补本 ADR 新命令

### 来源

- 2026-05-23 #agent-center 讨论
