# v2.2-A1 — admin endpoint scaffolding (unix socket binding) audit

> Phase A sub-ST 1 of 5. Per-ST P12 cadence: audit-first commit, then
> impl commit, then deploy smoke proof. See `v2.2-transport-layer.md`
> for the umbrella plan.

## § 0. 必答铁律答卷 (per conventions § 0.4)

| 必答问题 | 答 |
|---|---|
| 起点 = 领域模型要求？ | 是。AppService 必须有 transport；本 ST 不引入领域逻辑，只装 transport adapter |
| 保留 BC invariants？ | 是。Server 进程仍是唯一 sqlite writer；admin endpoint 只是新的 inbound 端口 |
| 没省 transport？ | 是。unix socket = 真 transport，不是"共享文件简化" |
| Mock-as-default 消除？ | A3 才处理 NoopSender，本 ST 不动 |

## § 1. 范围 (in)

新增 `internal/admin/api/` 包，平行于 `internal/webconsole/api/`：

1. `Server` 类型 — 复用 webconsole pattern，但 listener 不同（unix socket vs TCP）
2. **复用 `HandlerDeps` + handler 函数**：admin endpoint 本质是"另一组 routes 跑同样的 dep bag"。新增 `internal/admin/api/server.go` 注册自己的 mux + WithDeps middleware；handler 函数从 webconsole 借（同 package 跨 file split 时 OK，跨 package 就 export 复用——选择借 export）
3. **Unix socket listener** with mode 0600 + owner = current uid（同机 trust，无 auth token）
4. **ServerCommand 同时起两个 listener**：
   - WebConsole (existing): `127.0.0.1:7100` TCP
   - Admin (new): `/tmp/agent-center.sock` Unix socket
5. **config 字段**：`admin.socket_path` (default `/tmp/agent-center.sock`)，`admin.enabled` (default true)
6. **graceful shutdown** — server stops 两个 listener 都得 clean 关闭，socket file 删除

## § 2. 范围 (out)

- A1 **不**把所有 CLI handler 都映射到 admin endpoint — 那是 A2
- A1 **不**实现新 transport — A3
- A1 **不**改 CLI handlers — B 阶段
- A1 **只**装 framework + 1 个 health endpoint 作为 smoke 验证

A1 只是 scaffolding——**unix socket 起来，`/health` 端点能响应**，就 A1 done。

## § 3. 设计选择

### 3.1 复用还是新建 handler 包？

WebConsole api 包的 handler 函数与 admin endpoint 想要的 surface **高度重合**——大部分 CLI 命令对应的 Service method 已被 webconsole/api 暴露。

选择：**复用**。把 `internal/webconsole/api/` 重命名为 `internal/server/api/`（中性名），或者保持 webconsole/api 不动但 admin 直接 import 它的 handlers。先用后者（最小侵入）；如果发现 admin 需要的 endpoint 大量是 CLI 独有的，A2 再考虑包重组。

### 3.2 Unix socket vs Loopback TCP

为什么 unix socket 而不是 loopback TCP？
- **权限隔离**: socket file 0600 mode + owner check 比"port 监听 loopback"更强（端口本机可达所有用户）
- **同机零配置**: CLI 不用知道端口号，固定 socket path
- **未来 multi-host**: 加 TCP listener 是 additive，不动 unix socket

### 3.3 路径选择

默认 `/tmp/agent-center.sock`——但 `/tmp` 在 macOS 下重启清理。生产环境应该用 runtime dir。
- macOS 推荐: `$XDG_RUNTIME_DIR/agent-center.sock`（不存在则 fallback `/tmp/agent-center.sock`）
- Linux: `/run/user/<uid>/agent-center.sock`（同上 fallback）
- v2.2 选 fallback 路径（`/tmp/`）作为 default + config 可改；docs 推荐生产用 runtime dir

### 3.4 双 listener vs 同一 listener 多协议

不同 listener。简单、独立 lifecycle、graceful shutdown 一个不影响另一个。

## § 4. 文件计划

新增：
- `internal/admin/api/server.go` — `Server` + `ListenAndServe` + `Shutdown`，binds unix socket
- `internal/admin/api/health.go` — `GET /admin/health` handler（最小可验证 endpoint）
- `internal/admin/api/server_test.go` — unit test (起 socket + curl health)
- `internal/cli/admin_wiring.go` — 把 admin server 接到 ServerCommand 的生命周期（类似 `webconsole_wiring.go`）

修改：
- `internal/config/config.go` — 加 `AdminConfig{ Enabled bool, SocketPath string }`，default enabled=true, socket_path=`/tmp/agent-center.sock`
- `internal/cli/handlers_system.go` — `ServerCommand` 起 admin listener (类似现有 webconsole 起法)
- `docs/design/implementation/04-configuration.md` — admin 配置段
- `docs/design/architecture/tactical/presentation/01-web-console.md` — 加 "see also: admin endpoint"

## § 5. 验收

- `go build ./...` clean
- `go test ./internal/admin/...` green（unit test）
- `go test ./...` green（regression）
- **临时 deploy smoke**: 起 `./bin/agent-center server` → `curl --unix-socket /tmp/agent-center.sock http://localhost/admin/health` 返回 `{"ok":true}`
- conventions § 0.4 自检过（这是新增 transport adapter，不是绕过；没有 mock-as-default）

## § 6. Execution log

**Files landed:**
- `internal/admin/api/server.go` — `Server` (unix socket listener +
  graceful shutdown that removes socket file; 0600 perm + stale-file
  cleanup).
- `internal/admin/api/health.go` — `GET /admin/health` returning
  `{"ok": true, "transport": "unix", "endpoint": "admin"}`.
- `internal/admin/api/server_test.go` — 5 cases: health round-trip,
  socket permissions (mode & 077 == 0), stale-file removal on start,
  socket cleanup on shutdown, empty-path rejection. Helper
  `shortSocketPath` works around the macOS 104-byte socket path limit
  (t.TempDir's /var/folders/... is too long).
- `internal/cli/admin_wiring.go` — `runAdminEndpoint` (parallel to
  `runWebConsole`); fires the server goroutine + returns cleanup.
- `internal/cli/handlers_system.go` — ServerCommand now binds admin
  socket if `cfg.Server.AdminSocketPath != ""`; banner shows
  `admin=<path>`; defer-cleanup pattern matches WebConsole.

**Verification:**
- `go test ./internal/admin/api/` — 5/5 green.
- `go test ./...` — full regression green (no other packages
  affected; new package is additive).
- `make build` — binary built.
- **Deploy smoke** (firsthand, per conventions § 0.4):
  ```
  $ ./bin/agent-center server --config=/tmp/v22-smoke.yaml
  agent-center server: db=/tmp/v22-smoke.db listen=:7099 web=127.0.0.1:7199 admin=/tmp/v22-admin.sock (escalator running)

  $ curl --unix-socket /tmp/v22-admin.sock http://localhost/admin/health
  {"endpoint":"admin","ok":true,"transport":"unix"}

  $ ls -la /tmp/v22-admin.sock
  srw-------@ 1 oopslink  wheel  0  5月 24 21:43 /tmp/v22-admin.sock

  $ kill $pid  &&  ls /tmp/v22-admin.sock
  ls: /tmp/v22-admin.sock: No such file or directory
  ```
  - Banner now lists admin path ✓
  - Health endpoint returns JSON over unix socket ✓
  - File permissions are 0600, owner-only ✓
  - Graceful shutdown removes the socket file ✓

A1 complete. Ready to proceed with A2 (expose CLI-consumed AppService
methods through admin endpoint).
