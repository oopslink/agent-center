# ① env-security fix 验收（C secret-leak + A config-isolation）— 真 claude 证据

| Field | Value |
|---|---|
| 验收对象 | ① 安全 env 修（commit `a7b32f5`，`internal/agentsupervisor/claudeenv.go` + `supervisor.go`） |
| 层 | 集成（机制/安全；真 claude 2.1.156；断言 = Tester 独立定） |
| 方法 | **直跑** `agent-center worker agent-supervisor`（与 daemon spawn 无关 → 不被 spawn-bug 阻），注入假 worker secret，查 child claude 真实 env + 隔离 + auth；+ dev claudeenv 单测交叉核 |
| 初判 | **(a) PASS / ⑤ PASS(单测) / A-隔离 PASS / GATE-0 = 隔离无 auth 回归（正向 authed-turn 受测试环境 keychain 限制，见下）** |
| 日期 | 2026-05-30 |

## 白名单口径（claudeenv.go，(a) 判据源）
- exact: `PATH HOME USER LOGNAME LANG TZ TMPDIR SHELL`
- prefix: `LC_ ANTHROPIC_ CLAUDE_`
- `CLAUDE_CONFIG_DIR` 显式排除（supervisor 设隔离值、继承值永不透）
- 其余（`AGENT_CENTER_*` + 任何未知）默认拒丢弃
- 层2 `AgentEnv`（② seam）原样叠加、当前空

## (a) 泄漏 secret 缺失 — PASS ✅
注入到 supervisor env：`AGENT_CENTER_ADMIN_TOKEN=LEAKTEST_ADMIN_123`、`AGENT_CENTER_WORKER_BEARER=LEAKTEST_BEARER_456`、`BOGUS_UNKNOWN_SECRET=SHOULD_DROP_789`。
child claude（pid 14927，`ps eww`）实测：
- ❎ 无 `AGENT_CENTER_ADMIN_TOKEN`/`AGENT_CENTER_WORKER_BEARER`/`BOGUS_UNKNOWN_SECRET`；**零 `AGENT_CENTER_*`**。
- ✅ 保留 `PATH`/`HOME` + `CLAUDE_CONFIG_DIR=<home>/claude-config`（隔离）。
- argv = GATE-5 形态 + 合法 UUID session-id（`bddb4658-…`）。
单测交叉核：`TestBuildClaudeEnv_AllowlistDropsWorkerSecretsKeepsClaudeAuth` PASS。

## ⑤ 纵深（daemon→supervisor 段）— PASS（单测）✅
`TestBuildSupervisorEnv_StripsWorkerSecrets` PASS（`BuildSupervisorEnv` 同 filter、剥 worker secret）。**运行时 ⑤（经真 daemon）待 spawn-bug 修**（daemon 起不来 supervisor，见 #110 d238f031）；单测覆盖该段逻辑。

## A 配置隔离 — PASS ✅
- `CLAUDE_CONFIG_DIR=/tmp/gate1-accept/home/claude-config`（每-agent 隔离 dir）。
- **正向**：claude **真用了**隔离 dir——它在该 dir 写入 `.claude.json`/`sessions/`/`telemetry/`/`backups/`（不是空壳）。
- **反向 marker**：operator `~/.claude` 的 SessionStart hooks（superpowers/slock）**未污染** events.jsonl（grep superpowers/slock/SessionStart = 0）；对比原始 GATE-0（非隔离）events.jsonl **含**这些注入 → 隔离生效、claude 不读 operator config。

## GATE-0（auth 是否被隔离打断）— 隔离无 auth 回归 ✅（正向 authed-turn 受环境限制）
- 隔离空 config dir → child claude `apiKeySource:none` + "Not logged in · Please run /login" + `authentication_failed`。
- **但 baseline（`CLAUDE_CONFIG_DIR=~/.claude`，非隔离）结果完全相同**（也 "Not logged in"）→ **auth 失败非隔离所致**。
- 本机 claude auth 在 **macOS Keychain**（`Claude Code-credentials`），非 config-dir 文件（`~/.claude/.credentials.json` 不存在）。child claude **非交互**下读不到 keychain（ACL/无 prompt）→ 隔离/非隔离都 "Not logged in"。
- **结论**：隔离修**不引入 auth 回归**（baseline==隔离）。`claudeAuthFiles={.credentials.json}` 在本机 moot（keychain auth、无该文件）且无害；keychain auth 与 CLAUDE_CONFIG_DIR 正交、隔离按构造不破它。**正向"隔离下完成 authed turn"** 在本测试机无法演示（child claude 非交互读不到 keychain，连非隔离都不行）——需 launchd-keychain 上下文 / 或 `ANTHROPIC_API_KEY`(env, 在白名单)/文件 auth 才能正向跑通。建议该正向确认放部署/CI 环境。

## (b) 中心注入 env round-trip — 待 ② 管道落
② `Profile.EnvVars` 管道未建（dev 排在 spawn-bug 后）。届时埋非-secret marker（如 `AC_TEST_INJECT`）证 claude env 真拿到、且不被白名单吃。

## 给 dev 的一条纠正（CLAUDE_CODE_* 透传）
dev 称"`CLAUDE_CODE_SESSION_ID` 泄漏经 ① 修自动消"——**不准**。`CLAUDE_CODE_*` 匹配白名单 `CLAUDE_` 前缀 → **透传**（实测 child claude env 仍含 `CLAUDE_CODE_SESSION_ID`）。仅 `CLAUDECODE`（无下划线）被丢。生产无害（daemon 不在 claude 会话下、无此 env）+ 合"继承 claude-cli env"意图；但若要专门排除 claude-code SDK 会话变量是另一处改。

## 结论 / go-no-go 建议
① 安全修在 **(a) worker-secret 过滤 + ⑤(单测) + A 配置隔离（含正反 marker）** 三面 **PASS**、**不引入 auth 回归**。唯一未在本机正向演示的是"隔离下 claude 完成 authed turn"（受测试机 keychain 非交互限制、与隔离无关）——建议 PD 据此判：要么接受"无回归 + 单测 + 隔离 dir 真用 + 反向 marker 干净"为 ① 充分证据，要么把正向 authed-turn 放到 keychain-capable（launchd/CI/API-key）环境补一次。运行时 ⑤ + (b) 随 spawn-bug 修 / ② 管道落补。

## (a) CLAUDE_CODE_ deny-prefix 重验 — PASS ✅ (commit 3c2c55c, 2026-05-30)
dev 折进 ① 的 deny-prefix（`envAllowed`: `name=="CLAUDE_CONFIG_DIR" || HasPrefix(name,"CLAUDE_CODE_") → deny`）。Tester rebuild 3c2c55c + 直跑 supervisor 重抓 child claude env（`ps eww`）：
- source(supervisor) env 有 **4 个 `CLAUDE_CODE_*`** → child claude env **0 个** ✅（整段前缀 deny 生效，覆盖 SESSION_ID/ENTRYPOINT/EXECPATH/TMPDIR…）
- `AGENT_CENTER_*` 注入 `AGENT_CENTER_ADMIN_TOKEN=LEAK_REGRESS_111` → child env **0 个** ✅（(a) regression 守住）
- 注入 `CLAUDE_FOO`/`ANTHROPIC_TESTVAR` → child env **均在** ✅（其余 `CLAUDE_*` + claude auth 命名空间保留）
- `CLAUDE_CONFIG_DIR`=隔离 dir ✅
→ (a) 含 CLAUDE_CODE_ deny 全过。**① 仅剩 authed-turn 正向演示**（ship 前 authed/`ANTHROPIC_API_KEY` 环境，§A.5 红线）；runtime ⑤ + (b) 随 spawn-fix / ② 补。

## (a) CLAUDE_CODE_OAUTH_TOKEN carve-out 重验 — PASS ✅ (commit 5b07a22, 2026-05-30)
dev 为支持订阅-token 非交互路 (b) 在 `CLAUDE_CODE_` deny 前加专项放行（`envAllowed`: `if name=="CLAUDE_CODE_OAUTH_TOKEN" {return true}`）。Tester rebuild 5b07a22 + 直跑 supervisor + `ps eww` child claude env：
- `CLAUDE_CODE_OAUTH_TOKEN` → child env **在（1）** ✅（carve-out 生效、订阅 token 路可透）
- `CLAUDE_CODE_SESSION_ID` → **剔（0）** ✅；其余 `CLAUDE_CODE_*` → **0 泄漏** ✅（session 标记 deny 仍守）
- `AGENT_CENTER_*` → **剔（0）** ✅（worker secret regression 守住）
- `CLAUDE_FOO`/`ANTHROPIC_TESTVAR` → **均在（2）** ✅
→ **① env 构造三条 auth 路径全 code-ready**：env `ANTHROPIC_API_KEY` / 文件凭据(`claudeAuthFiles`) / 订阅 `CLAUDE_CODE_OAUTH_TOKEN`(carve-out)。**① 代码侧 (a)+⑤+A+CLAUDE_CODE_ deny+OAUTH carve-out 全 PASS**；仅剩 **authed-turn 正向演示**（🔴 需 @oopslink 给凭据：API key 或铸订阅 token；本机无 key、订阅 keychain 非交互不可读、setup-token 交互式我铸不了）。

## 🔴 GATE-0/A-isolation × /login auth — 关键发现（2026-05-30，控变量实测）
@oopslink 定 **v2.7 只支持 /login（keychain 订阅）**。PD/dev 假设"① 剔了 markers → keychain 能认"。控变量实测（markers 全剔、不给 token、pipe 一条消息、看认进+跑完）：

| 变体 | CLAUDE_CONFIG_DIR | 结果 |
|---|---|---|
| A | unset（默认 ~/.claude） | ✅ PONG / is_error:false（keychain /login 认进+跑完） |
| G | =`~/.claude`（显式设成同一默认路径） | ❌ Not logged in / authentication_failed |
| D | =隔离 dir（① 真实做法） | ❌ Not logged in |
| E/F | 隔离 dir + 拷 `.claude.json`(258B config级 / 118KB HOME级) | ❌ 仍 Not logged in |

**结论1（翻案，好消息半边）**：keychain /login 非交互**能用**——条件 = markers(`CLAUDECODE`/`CLAUDE_CODE_*`)剔 + **CLAUDE_CONFIG_DIR unset**（变体 A）。我之前"keychain 非交互不可用"是**测试假象**（repro env 带了 markers）。slock 正是这样跑（默认 config + delete CLAUDECODE）。
**结论2（关键阻塞）**：**显式设 `CLAUDE_CONFIG_DIR`（设成任何值、连同一默认路径 G 都算）→ 打断 keychain /login**。单变量 = A(unset) vs G(设)。最可能机制：CLAUDE_CONFIG_DIR 一旦显式设 → claude 改走该 dir 的文件凭据(.credentials.json) → 不查 keychain；拷 .claude.json(两级)救不回 = 切了 auth 模式、非缺文件。
**机制核（PD #3）**：直跑真 `agent-center worker agent-supervisor` → child claude env **0 markers**（剔干净坐实）+ `CLAUDE_CONFIG_DIR=<home>/claude-config`（= 失败的 D）。

**影响**：① 的 **A-config-隔离靠设 `CLAUDE_CONFIG_DIR`**，与 **/login keychain 互斥**。/login-only 下 ① 现状 → agent claude 认不进。**非"没 auth 路"**——/login 本身能用（前提不设 CLAUDE_CONFIG_DIR）；冲突专在 ① 的 config-dir 隔离。**取舍待 PD/dev/@oopslink 拍**（报 #110 c669404f）：(a) ① 不设 CLAUDE_CONFIG_DIR（用默认、另法防 operator hooks 污染：worker 专用干净 HOME/.claude 或关-hooks flag）；(b) 别的不设-CONFIG_DIR 的隔离法。**A 的 secret-泄漏过滤(层1 worker-secret)那半不受影响、仍 PASS**；受影响只有 config-dir 隔离这处。authed-turn 正向待此取舍定后用拍定那条闭。

## 🟢 SOLUTION (2026-05-30): A-isolation via `--setting-sources ""` (NOT HOME/CONFIG_DIR relocation)
dev proposed HOME-override (variant H) as the isolation fix. Tested + REJECTED, then found the working solution.

**H-series (HOME override) — ALL FAIL** (markers stripped, CONFIG_DIR unset, pipe a turn):
| variant | HOME | auth |
|---|---|---|
| A | `/Users/oopslink` (real original) | ✅ PONG |
| H | `/tmp/.../iso-home` (clean) | ❌ Not logged in |
| H2 | `/tmp/...` + copied account-linkage `.claude.json` | ❌ Not logged in |
| H3 | `/Users/oopslink/.ac-home-test-h3` (subdir under real home) | ❌ Not logged in |
→ ANY HOME ≠ original breaks keychain /login (H3 rules out a /tmp-specific cause). keychain cred is **bound to the original HOME/config path** (`Claude Code-credentials-48d98f35` = likely path hash). "keychain per-user/HOME-independent" assumption is FALSE. `SkillMountSymlinkHomeClaude` auths only because it symlinks operator's real `~/.claude` (hooks included = not isolated). **Both CONFIG_DIR and HOME relocation break /login.**

**WORKING SOLUTION (variant I2, verified)**: no relocation (default HOME + CONFIG_DIR unset → keychain auth premise preserved) + add claude argv **`--setting-sources ""`** (load NO setting sources → operator hooks/settings don't run):
- Result: **PONG ×3 / is_error:false (keychain /login auths)** + **operator hooks (superpowers/slock) = 0 (isolation holds)**. Both achieved.
- Rejected: `--bare` (auth FAIL + didn't isolate hooks).

**Implication for ①**: change A-isolation from CONFIG_DIR/HOME relocation → **`--setting-sources ""` in argv (in-place)**, keep default HOME/config (keychain /login intact). Layer-1 secret filter + marker strip + carve-out withdrawal unchanged. `PrepareIsolatedClaudeConfig` (auth-copy) becomes obsolete (keychain, no files). ⚠️ dev to confirm `--setting-sources ""` coexists with `--mcp-config` (MCP via flag, not setting source) + agent needs no settings-source. Tester re-verifies whole new ① after dev's change.
