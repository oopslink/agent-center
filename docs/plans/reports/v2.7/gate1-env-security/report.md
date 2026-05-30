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
