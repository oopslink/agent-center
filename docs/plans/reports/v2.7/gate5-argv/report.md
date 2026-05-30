# GATE-5 — argv 静态断言 + session-id（集成机制层）证据

| Field | Value |
|---|---|
| GATE | GATE-5（argv 回归静态断言；§A.0；集成机制层） |
| 层 | 集成（机制；断言 = Tester 独立定；不需真 claude/手册） |
| 被验 | `internal/claudestream` `BuildStreamingArgv` / `SessionUUID`（v2.7 worktree `agent-center`，commit 9887db7） |
| 方法 | Tester 独立断言 test `gate5_argv_tester_test.go`（不属 dev 套件）→ `go test ./internal/claudestream/ -run GATE5 -v` |
| 初判 | **PASS** |
| 日期 | 2026-05-30 |

## 实际产出 argv（捕获）
```
claude --output-format stream-json --session-id 9ae50ddf-cb72-51b5-b10a-5bafbdccbb1c --print --input-format stream-json --verbose --mcp-config /tmp/agents/01KS/mcp-config.json
```
（agentID=`01KSVNCZEXAMPLEAGENTID0001`, epoch=0, mcp-config 路径给定）

## 断言 vs §A.0（逐条）
| # | 断言（§A.0） | 结果 |
|---|---|---|
| 1 | `--print` 在（D2-c-ii-C 抓的回归：曾错删） | ✅ |
| 2 | `--input-format stream-json` 在 | ✅ |
| 3 | `--output-format stream-json` 在 | ✅ |
| 4 | `--verbose` 在 | ✅ |
| 5 | 无 `-p`、无位置 prompt（sentinel `__ac_streaming_input__` 被剥） | ✅ |
| 6 | `--session-id` 在 且匹配 UUID 正则 `^[0-9a-f]{8}-…{12}$` | ✅ `9ae50ddf-cb72-51b5-b10a-5bafbdccbb1c` |
| 7 | `--mcp-config <path>` 在（给路径时） | ✅ |
| 8 | argv 无多余位置参数 | ✅ |

## session-id UUID/稳定性（§A.0 ②）
| 断言 | 结果 |
|---|---|
| 同 (agent,epoch) → 同 UUID（re-attach / crash-relaunch 续同会话） | ✅ |
| 全为合法 v5 UUID | ✅ |
| epoch++ → 不同 UUID（reset = clean-slate） | ✅ `a1/e0=9ae50ddf…` vs `a1/e1=5e89dc61…` |
| 不同 agent → 不同 UUID（无"已在用"冲突） | ✅ `a2/e0=632e6b03…` |

## 结论
GATE-5（argv 静态 + session-id 合法/稳定）**全部断言通过**。即"组装出的 claude 调用 argv 符合 claude 2.1.156 契约（保 --print、流式格式、合法 UUID session-id、无一次性位置 prompt），且 session-id 在 (agent,epoch) 上确定、reset 跨 epoch 变、跨 agent 不撞"。

> 注：本条是**静态/单元级机制断言**（不跑真 claude）。GATE-0（真 claude 用此 argv 真能起进程）是其运行时验证、待真 claude 环境。
> 测试源：`<v2.7 worktree>/internal/claudestream/gate5_argv_tester_test.go`（Tester 独立断言，复用生产 `BuildStreamingArgv`）。
