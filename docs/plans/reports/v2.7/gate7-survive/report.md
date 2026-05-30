# GATE-7 Mode A (survive + re-attach) — 真 claude 证据（(b) 统一-worker, commit 27ac198）

| Field | Value |
|---|---|
| GATE | GATE-7 Mode A（killpg daemon 组→supervisor+claude 同 pid 存活 + 重启 re-attach）|
| 层 | 集成机制（真 claude 2.1.156；断言 Tester 独立定）|
| 环境 | 隔离 center /tmp/gate67 + `agent-center worker run`（统一-worker, (b)）, control-loop on |
| 初判 | **PASS** |
| 日期 | 2026-05-30 |

## 前置（同跑确认）
- **config-面 parity 修好（27ac198 resolveWorkerConfigPath）**：`worker run --config=/tmp/gate67/config.yaml` → `loaded long-term token from /tmp/gate67/var/worker-token`（operator config、非 /var/lib 默认）+ `control: connected`。
- **spawn-bug 在 (b) 下解**：统一 `agent-center worker run` daemon 真 spawn supervisor(53075)+claude(53080)，supervisor.sock/instance 建齐，ppid 链 claude(53080)→supervisor(53075)。
- **mcp-host 生产尾巴（结构闭）**：daemon 生成的 mcp_config.runtime.json server command = 统一 `bin/agent-center`（os.Executable，非退役的 worker-daemon）→ mcp-host 能路由；叠加 gate-6 已证该统一-binary mcp-config → MCP connected + `mcp__agent-center__get_my_work` is_error:false。全 runtime tool-exec 随 GATE-3 work-injection 复验。

## GATE-7 Mode A 取证
**baseline**：daemon pid=53066(pgid 53066) / supervisor pid=53075(**自成 pgid 53075=setsid 逃 daemon 组**) / claude pid=53080 / instance_id=01KSWR6YT1GDMKK4ZW8S1NN76H / ppid: claude 53080→parent 53075(supervisor、owner 对).
**killpg**：`kill -9 -53066`（杀 daemon 整组）→ daemon 死；**supervisor 53075 + claude 53080 仍活（同 pid）、instance_id 不变** = 同一进程从没退（非 kill+relaunch；PD 反假通过守卫满足）.
**re-attach**：重启 `worker run` → boot-reconcile `probe=reattachable desired=running → reattach` + `RE-ATTACHED from offset=29000 (no nudge — claude alive)`；supervisor 仍 53075（无新 spawn=非 relaunch、单一 supervisor）、instance_id 不变、**无 spurious nudge**.

## 断言（§A GATE-7 Mode A）
| 断言 | 结果 |
|---|---|
| supervisor setsid 逃 daemon 组（自成 pgid）| ✅ pgid 53075≠daemon 53066 |
| killpg daemon 组后 supervisor+claude **同 pid 存活** | ✅ 53075/53080 仍活 |
| instance_id 跨 killpg 不变（同一进程，非 relaunch）| ✅ 01KSWR6Y…不变 |
| 重启 daemon → reattach（probe=reattachable、非 relaunch）| ✅ |
| reattach 到**同 pid**（无新 supervisor spawn）| ✅ 仍 53075、单一 |
| reattach 从 offset 续（不丢）| ✅ offset=29000 |
| claude 活时**无 spurious nudge** | ✅ "no nudge — claude alive" |

## 结论
GATE-7 Mode A（survive+reattach）全断言过：killpg daemon→同 pid 存活→重启 reattach 同 pid 续、不丢、无双跑、无误 nudge。头号 cutover 风险（worker 重启 agent 不中断）在真 (b) 路径坐实。Mode B（真死→relaunch+resume）+ GATE-6 接缝 + GATE-1/2/3/4 待续。
