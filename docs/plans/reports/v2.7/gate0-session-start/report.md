# GATE-0 — 真 claude 用组装 argv 真起会话（集成机制层）证据

| Field | Value |
|---|---|
| GATE | GATE-0「会话能真启动」（§A.5；集成机制层；GATE-5 的运行时对应） |
| 层 | 集成（机制；真 claude 2.1.156；断言 = Tester 独立定） |
| 方法 | 跑 `agent-center worker agent-supervisor`（v2.7 worktree build），真 claude 子进程 |
| 初判 | **PASS** |
| 日期 | 2026-05-30 |

## 执行
```
/tmp/agent-center-gate worker agent-supervisor \
  --agent-id 01KSGATE0TEST0001 --home-dir /tmp/gate0-home \
  --claude-bin /Users/oopslink/.local/bin/claude --model claude-haiku-4-5-20251001
```
（无 --mcp-config：GATE-0 只验"会话起"；MCP 是 GATE-3）

## 观测（真进程）
- **supervisor 起**：`agent=01KSGATE0TEST0001 instance=01KSW7HMNWFC0R0MY8JQVHPG7C child_pid=96712`；supervisor pid 96708 **STAT=Ss（session leader = setsid 自成会话/组，detach 成立）**。
- **claude 真起 + 存活**：pid 96712 **STAT=S（活、未退）**，实际命令行：
  ```
  claude --output-format stream-json --session-id d0253330-be86-5eb2-87f8-54ca2f2d76e3 --print --input-format stream-json --verbose --model claude-haiku-4-5-20251001
  ```
  → 与 GATE-5 断言的 argv 形态一致（--print / stream-json in+out / --verbose / --session-id=UUID / 无位置 prompt）。
- **session-id 被 claude 接受**：claude 用 `--session-id d0253330-…`（= `SessionUUID(01KSGATE0TEST0001,0)` 的合法 UUID）**正常启动、未报 "Invalid session ID. Must be a valid UUID."** → 当初 ULID 被拒的回归点坐实修好。
- **claude 真产 stream-json**：`events.jsonl` 4 行、均 `type=system`（真 claude 2.1.156 启动 SessionStart hook 事件），supervisor 持续 drain 落盘。
- **工件齐**：`claude.pid`(96712) / `supervisor.instance`(instance_id+agent_id+supervisor_pid+child_pid+started_at) / `supervisor.sock`（可重连 socket）/ `events.jsonl`（offset 缓冲）。

## 断言（§A.0 / GATE-0）
| 断言 | 结果 |
|---|---|
| 真 AgentController/supervisor 用组装 argv **真把 claude 进程起起来**（非 stub） | ✅ 96712 活 |
| argv 形态 = GATE-5 断言（--print/stream-json/--verbose/UUID session-id/无位置 prompt） | ✅ |
| **合法 UUID session-id 被 claude 接受、不拒启**（ULID 回归点） | ✅ |
| claude 真产 stream-json（supervisor drain→events.jsonl） | ✅ 4 system 行 |
| supervisor setsid-detach（STAT Ss，为存活逃 killpg 打基础） | ✅ |
| 进程不立即退（长驻、未 EOF 退） | ✅ STAT=S 持续 |

## 清理
杀 supervisor(96708)+claude(96712)，已确认无残留 gate-0 进程。

## 注（GATE 环境，后续 parse 类 GATE 需隔离）
本次 claude 继承了**本机 `~/.claude` 的 SessionStart hooks**（events.jsonl 里出现本用户的 superpowers/slock 注入内容）——这是测试 claude 跑在我的用户级 claude 配置下的环境产物。**对 GATE-0（进程起+产 stream-json+接受 UUID）无影响**；但 GATE-2（解析 0-unknown）/GATE-1（注入回显）等需要**干净可控的 claude 输出**时，应隔离 claude 配置（如独立 HOME / 关用户 hooks），避免我的 hooks 污染待解析输出。已记入后续 GATE 环境准备。
