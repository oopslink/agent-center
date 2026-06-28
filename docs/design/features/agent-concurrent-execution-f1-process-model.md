# F1 实现说明 — 监工 / Executor 进程模型与拓扑

> 父设计：[agent-concurrent-execution.md](agent-concurrent-execution.md)（§3 串行/并发、§4
> 进程与连接拓扑、§10 配置项 max_concurrent_tasks、§11.2 executor 状态机、§12 崩溃恢复）。
> 本文档记录 F1（cycle v2.17.0 plan-9c606650 / 任务 T505）的**实现层**落点。F1 在 F2
> （`internal/workerdaemon/executor` 文件协议）之上加**进程模型**：spawn、环境隔离、并发闸门、
> executor 入口。模型路由（F3）、watchdog/完成信号编排/崩溃恢复 reaper（F5）、监工事件主循环
> **不在本范围**——它们消费本层。

## 1. 范围

只做**进程模型与拓扑**——「一个 agent = 一个监工 + N 个 executor」里 executor 一侧的
*进程* 落地（design §4 / §11.2）：

1. **环境隔离**：executor 进程无 mcp / 无中心凭据可达（design §4「executor 不持凭据 / 不碰 mcp」）。
2. **进程组隔离**：每个 executor fork 进自己的 process group（killpg 不串味）。
3. **并发闸门**：≤ `max_concurrent_tasks`（默认 3）并发，超额排队不硬起（design §3 / §10）。
4. **executor 入口**：forked 进程跑的状态机（读 input → status running → 跑 → output/status → exit code）。

**不做**（划给 F3/F5，本层留 seam）：选哪个模型 / 哪条 agent CLI（F3，model 从 input.json 来）；
watchdog / 完成信号判定 / 崩溃恢复扫目录重建 / 监工事件唤醒主循环（F5）。**监工**本体 = 既有
`internal/agentsupervisor` + `internal/supervisormanager`（常驻、唯一连 mcp、收发 chat、survive
daemon），F1 不重建它，只提供它 fork executor 所需的进程模型件。

## 2. 落点（均在 `internal/workerdaemon/executor`，consume F2）

| 文件 | 职责 | design |
|---|---|---|
| `executorenv.go` | `BuildExecutorEnv` — executor 启动环境：默认 deny allowlist（无 worker/mcp 密钥、无 admin endpoint）+ ② per-agent overlay；overlay 里的中心凭据形 key 再 scrub 一遍（防御纵深）| §4 / §6.A |
| `spawn.go` | `Spawner.Spawn` — fork `worker executor` 子进程，`SysProcAttr{Setpgid:true}` 独立进程组，**不**给 --mcp-config；`Handle` 经负 PID killpg 信号整个组 | §4 / §11.2 |
| `pool.go` | `Pool` — per-agent ≤N 并发闸门：reserve→Provision→worktree→WriteInput→Spawn→track；满则 `ErrAtCapacity`（监工排队，非失败）| §3 / §10 |
| `run.go` | `RunExecutor` — executor 侧入口状态机 + `Runner` 端口 + 默认 `CommandRunner`（在 workspace 内跑监工选定的 model-routed 命令）| §11.2 / §9 |
| `internal/cli/handlers_executor.go` | `worker executor` 子命令 → `RunExecutor`；进程 exit code 即完成信号 | §4 / §9 |

加固自检参见 §6。

## 3. 环境隔离（`executorenv.go`，design §4）

`BuildExecutorEnv(source, agentEnv)` 两层，与 supervisor 的 `BuildClaudeEnv` 同策略但**独立实现**
（executor 包保持零内部依赖——daemon import 本包做 spawn，agentsupervisor import daemon，若本包再
import agentsupervisor 一旦接线就成环）：

- **layer 1**：对系统 env 走 default-deny allowlist（`PATH/HOME/...` + 代理 + locale + claude 自有
  `ANTHROPIC_*/CLAUDE_*` auth，排除 `CLAUDE_CONFIG_DIR` 与整段 `CLAUDE_CODE_*`）。`AGENT_CENTER_*` /
  `AC_MCP_*` 不在 allowlist → 结构性丢弃。
- **layer 2**：② per-agent overlay（git identity / Profile.EnvVars）AS-IS 叠加，**但** `centerCredentialKey`
  （`AC_MCP_*` / `AGENT_CENTER_*`）即便被误配进 profile 也一律 scrub——executor 永不可能拿到中心凭据。

与 supervisor 的关键差：executor **从不**收 `--mcp-config`（Spawner 不传），连 supervisor 那条
「token 经文件路径间接给 mcp-host」的链路都不存在。

## 4. 进程组隔离与 spawn（`spawn.go`，design §4 / §11.2）

`buildExecutorCommand`（纯函数，可测）拼 `worker executor --executor-id <id> --agent-root <root>
[--runner-cmd <json-array>]`，`cmd.Env = BuildExecutorEnv(...)`，`SysProcAttr{Setpgid:true}`。

- **独立 process group**：Setpgid（不 Setsid——executor 是监工 reap 的瞬态子进程，非 daemon survivor，
  与 supervisor 自身 setsid 的角色相反）。child 无显式 Pgid ⇒ pgid==pid ⇒ `Handle.Signal` 用 `-pid`
  killpg 整个 executor 子树（监工 stop / watchdog 走这里，F5 消费）。
- **runner 命令走 JSON flag** 而非裸尾 argv：CLI 的 permissive 解析器会把 runner 自带的 `-p` 当本命令的
  未知 flag 报错，故整条 argv 用 `--runner-cmd '["claude","-p","..."]'` 包成不透明 JSON。
- os/exec 经 `commandStarter` / `groupSignaler` seam 注入，argv/env/进程组不变量纯单测，生产接真 syscall。

## 5. 并发闸门（`pool.go`，design §3 / §10）

`Pool` 是 per-agent「≤N executor」原语：`Launch` 先在锁内 **reserve** 一个槽（原子地兑现上限），慢 I/O
（`git worktree add`、spawn）在锁**外**跑，所以并发 launch 落到不同空槽不会串行在 git 上，而上限仍精确。
满 → `ErrAtCapacity`（监工*排队重试*，非工作失败，design §3「超额排队，不硬起」）；重复 id → `ErrAlreadyActive`。
失败的 launch 释放其 reserved 槽。`Max()/Active()/Available()/Handles()/Release()` 给监工循环（F5）驱动。
每 executor 一条 `executor/<id>` 分支（`AddNewBranch`），两并发 executor 端到端不共享 checkout（接 F2 §6.D）。

## 6. executor 入口状态机（`run.go`，design §11.2 / §9）

`RunExecutor`：`ReadInput → WriteStatus(running) → Runner.Run（边跑边 AppendProgress 并刷
last_progress_at 喂 watchdog）→ 成功 WriteOutput+status=done 返回 nil（exit 0）/ 失败 WriteOutput(error)
+status=failed 返回 err（exit !=0）`。即便失败也尽力落 output.json + status=failed，让监工的**双信号**
判定（design §9：exit code + status/output）有细节。input 缺失 → 直接报错退出（无 running status 的进程没了
= 监工按崩溃处理）。

`Runner` 端口把「跑什么模型 / 哪条 CLI」与进程模型解耦：默认 `CommandRunner` 在 executor 的隔离 workspace
内跑监工经 `--runner-cmd` 传入的 model-routed 命令（F3 供给具体 argv），继承 executor 自己已净化、无 mcp 的
环境——没有中心凭据可往下传。结构上 executor **不**构造任何中心 / mcp 连接。

## 7. 规约自检（conventions §15）

- **§0.4 AppService**：本层是 worker 本机进程编排 + 文件状态（同 F2 / taskexec / sessioninstance），
  **无**中心 DB / 跨进程 AppService 绕过，不触发该铁律。executor 结构上不连中心。
- **§2 可观测性**：executor 不连中心、不发 domain event；其可观测面是 F2 的 `status`（state/last_progress_at/
  error）+ `progress.jsonl`。**监工侧**读 output 后回写中心 / emit event 属 F5，本层不发事件。
- **§4 零 LLM SDK / §10 单二进制**：无新依赖；`worker executor` 是既有单二进制的新子命令，非新 binary。
- **§9 持久化**：纯进程 + F2 文件，无 SQL / schema migration / FK。
- **§12 命名**：executor / orchestrator(监工) / pool 用统一语言；workspace 沿用 F2 wire 名。
- **§13 安全**：本 slice 的核心即安全——executor 环境 default-deny + 中心凭据双重 scrub + 无 --mcp-config；
  独立进程组防 killpg 串味。
- **§14 / §14.x 可测**：os/exec 与 git 全经 seam 注入，进程模型不变量纯单测；真进程 / 真 CLI 的集成用例
  缺二进制则 `t.Skip`（沿用 F2 真 git 策略）。
- **§16 reason+message**：失败经 F2 `ErrorDetail{Kind,Message}`（`kind=runner_failed`）。
- **§17 错误不吞**：每个 err 分支显式 return；progress/status 的 best-effort 写明确标注为非致命 relay。
- **§19 测试 ownership**：单模块（`internal/workerdaemon/executor` + `internal/cli` 入口）单测，Dev scope。

未触发 / 不适用：§1（不造 task）、§8 BlobStore（协议文件均小 JSON）、§9.4 schema 变更（无）、§11 渠道（无新交互渠道）、§5 ADR（落既有 design 范围内，无新架构决策）。

## 8. 测试计划 / 报告

### 范围
`internal/workerdaemon/executor` 新增 4 文件全部公开 API + `internal/cli` 的 `worker executor` 入口。
真进程 / 真命令的集成用例缺二进制则 `t.Skip`。

### 用例清单与结果

| # | 用例 | 文件 | 结果 |
|---|---|---|---|
| 1 | `BuildExecutorEnv`：放行系统/auth、丢中心凭据与未知密钥与 CLAUDE_CODE_*/CONFIG_DIR、overlay 应用且 scrub 中心凭据、确定序+畸形跳过 | executorenv_test.go | PASS |
| 2 | `buildExecutorCommand`：argv/env/Setpgid、无 mcp-config、JSON runner-cmd、默认 binary=os.Executable | spawn_test.go / integration_test.go | PASS |
| 3 | `Spawn`：fake start 赋 handle + killpg 经负 pid、start 错传播、handle 信号/Wait/Release 守卫、非法 spec 拒绝 | spawn_test.go | PASS |
| 4 | `Pool`：≤max 后 ErrAtCapacity、默认 3、重复 id、Release 放槽、provision 失败放槽、**并发 launch 精确守上限**、Launch 校验 input、NewPool 校验 | pool_test.go | PASS |
| 5 | `RunExecutor`：成功落 output+done、失败落 error+failed 且 exit!=0、input 缺失=崩溃；`CommandRunner` 跑命令于 workspace / 空命令报错 / 命令失败带输出；summarize | run_test.go | PASS |
| 6 | **集成**：真 Spawner 起真进程（realStart/Wait/Release）、真 CommandRunner 跑 `true`（execRun） | integration_test.go | PASS |
| 7 | CLI `worker executor`：usage 错误、bad JSON、真命令端到端落 output/status、元数据声明无中心/mcp | handlers_executor_test.go | PASS |

### 覆盖率
- `internal/workerdaemon/executor` 包行覆盖：**92.0%**（含 F2 既有 91.7%；本 slice 新增 4 文件同档）。
- 未覆盖余项：`realGroupSignal`（killpg 一行 syscall 包装，安全地无法在测试内对真实进程组发信号）、
  少量深层写失败防御分支（同 F2「无法确定性触发」策略）。

### 出口标准核对
- [x] executor 环境无 mcp / 凭据可达（§3，用例 1/2，含 overlay 双重 scrub）
- [x] 并发 fork ≤N 个 executor 独立进程组（§4/§5，用例 4 并发精确守上限 + Setpgid + killpg）
- [x] executor 入口跑通：读 input → status → output（用例 5/6/7，真命令端到端）
- [x] 行覆盖 ≥ 90%（92.0%）
- [x] `go build ./...` / `go vet ./...` / `gofmt` 干净

### 结论
F1 验收达成：监工（既有 supervisor）常驻收发 chat 不变；新增进程模型层使监工能并发 fork ≤N 个
**独立进程组**的 executor，每个 executor 运行环境内**无 mcp / 无中心凭据**可达，且经 F2 文件协议读 input、
写 output/status——「一个 agent = 一个监工 + N 个 executor」拓扑的进程一侧落地完成。
