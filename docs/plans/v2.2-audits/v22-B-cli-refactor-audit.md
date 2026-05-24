# v2.2-B — CLI refactor: client over admin transport

> Phase B. Per-ST P12 cadence. Builds on Phase A (admin endpoint live
> with 93 routes + dispatchq + real spawner).

## § 0. 必答铁律答卷 (per conventions § 0.4)

| 必答问题 | 答 |
|---|---|
| 起点 = 领域模型要求？ | 是。CLI 不能绕过 AppService — 必须经 admin transport |
| 保留 BC invariants？ | 是 — 调用的还是同一组 Service method，只是过 unix socket |
| 没省 transport？ | 是 |
| Mock-as-default 消除？ | A3 已做；本 ST 不引入新的 stub |
| 起点 = 领域模型？ | 是 |

## § 1. 范围

### in scope

把所有 36 个 CLI subcommand handler **完全去掉对 cli.App 内 Service / Repo 字段的直接访问**，改成调一个 `Client` 类型——`Client` 内部 HTTP/JSON over unix socket 调 admin endpoint。

- 新增 `internal/cli/admin_client.go` —— Client 类型 + 所有 79 个 method 的 thin wrapper
- 改造 `internal/cli/build.go` 的 `lazyApp.build()`：对非 server 命令，**不开 DB**，构造一个 client-only App；对 `server` 命令保持原有 DB-opening path
- 36 个 handler 文件每一个 `*App` 引用改成调 `*Client`
- 测试重写：原 CLI test 直接 setup DB → 改为起 in-process server + 用 Client

### out of scope

- 不引入 streaming / SSE 这条 transport（C 阶段）
- 不动 `worker run` 占位（C 阶段才填）
- WebConsole 不动（它本来就走 AppService）
- 不删 webconsole/api，admin 和它平行存在

## § 2. 设计选择

### 2.1 Transparent proxy vs explicit Client

两种路径：
- **A. Proxy**：App.EnrollSvc 等字段类型不变（interface），换成 proxy 实现 → handler 代码完全不动
- **B. Explicit Client**：handler 改成 `c.WorkerEnroll(ctx, ...)` 直接调 Client

选 **B**。原因：
- Service 类型大量是 `*concrete` struct 不是 interface，proxy 不直接适用
- handler 改动更可见，arch_test 容易抓未迁移点
- Client 单一入口便于 metrics / 日志 / retry 后续加

### 2.2 Connection management

- `Client` 持一个 `*http.Client`，`Transport` 自定义 Dial unix socket
- `cfg.Server.AdminSocketPath` 决定 socket 路径
- "server 未启动" 错误：socket 不存在 / connect refused → 友好提示 `agent-center server &` 起服

### 2.3 命令分类

3 类 CLI command，分别处理：
1. **走 admin transport**（绝大多数 ~33 个）：通过 Client
2. **本地工具，无需 server**（≤3 个）：migrate, server, version, help
3. **agent CLI 钩子**（read-task-context / report-progress / 等 5-7 个，runtime-only — agent 通过 bash 调）：通过 admin transport（因为它们也要写 events / states）

## § 3. 文件影响清单

新增：
- `internal/cli/admin_client.go` — Client + 79 methods（large file，可拆 sub-file 按 BC 分组）
- `internal/cli/admin_client_test.go` — unit tests

改：
- `internal/cli/build.go` — `lazyApp` 改造，区分 server / non-server 路径
- `internal/cli/app.go` — App 加 `Client *Client` 字段；旧 Service 字段保留但 server 模式才填
- `internal/cli/handlers_*.go` — 18 个 handler 文件每个改若干处 `a.XSvc.Y(...)` → `a.Client.Y(...)` 或更直接 `c.Y(...)`
- 各 `handlers_*_test.go` — 改为起 in-process admin server + Client 测试

## § 4. 验收

- `go build ./...` clean
- `go test ./...` green（CLI test 全 rewire 后）
- arch test 通过（A5 装的那个仍然 green —— 没新增直读 sqlite 的 handler）
- **deploy smoke**：起 server → 用 CLI 命令（不直接 sqlite）做完整流程：
  - `agent-center worker enroll` → CLI 通过 admin endpoint 走
  - `agent-center worker list` → 同上读出
  - `agent-center conversation send` → 同上
  - 全程不 touch sqlite 文件除了 server 进程

## § 5. Execution log

由 impl commits 填写。
