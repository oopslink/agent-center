# v2.3-7b Client-side Fingerprint Pinning Audit

> Closes slock task #27 ST 7b (client side). Pairs with
> [v23-7a-tcp-tls-listener-audit](v23-7a-tcp-tls-listener-audit.md) (server side).
>
> Adds the client-side transport that worker daemon + CLI use to dial
> the v2.3-7a TCP+TLS admin listener. SSH-style fingerprint pinning is
> the trust anchor (rejected TLS-no-verify per @oopslink decision
> 2026-05-26 thread `#agent-center:b79f992f` — see § 2 MITM matrix in
> 7a audit). Both client packages share a new `clienttransport`
> sub-package so the pinning + format contract has one canonical
> implementation.

## § 0. 必答铁律答卷 (per conventions § 0.4)

| 必答问题 | 答 |
|---|---|
| 起点 = 领域模型要求？ | 是。Worker daemon AdminClient + CLI Client 之前都只能 dial unix socket，限制了跨机部署。本期添加 client-side 等价物：同一 transport abstraction（unix vs tcp+tls），同一鉴权（bearer token unchanged），无新 BC / AR / Service。 |
| 保留 BC invariants？ | 是。`NewAdminClient(sock, ...)` / `NewClient(sock, ...)` 旧入口完整保留，行为零变化（所有现有测试 / 调用站点不动）。新增 `NewAdminClientFromTarget` / `NewClientFromTarget` 作为新构造入口；CLI 通过 `AGENT_CENTER_ADMIN_TARGET` env 选 transport。 |
| 没省 transport？ | 是。**TCP 路径强制要求 fingerprint**（empty → error；malformed → error）。没有 "TLS no-verify" 静默退路 —— 安全决策已 frozen，client 端不接受 `InsecureSkipVerify: true` 作为正常路径（仅用于在 TLS handshake 后做 fingerprint pinning，handshake 后立即校验或断开）。 |
| Mock-as-default 消除？ | N/A — 新建模块，无 mock 切换路径。test 用 `httptest.NewTLSServer` 真起 TLS server 做 round-trip + mismatch 测试。 |
| 起点 = "previously didn't support cross-host CLI"？ | 否（per § 0.6）。事实是 v2.2 admin Client 只接 unix socket，物理上限制了 CLI / worker daemon 必须跟 server 同机同用户。v2.3-7a 在 server 加了 TCP 腿；v2.3-7b 这一期在 client 加上对应能力。没有"以前不打算支持"的隐含。 |

## § 1. Scope — what changed at the layer

### F1 — `internal/admin/clienttransport` package (new)

新建 sub-package 让 CLI Client 和 worker-daemon AdminClient **共享** transport 抽象（避免两份代码重复实现 fingerprint pinning，否则 v3 升级 SPKI mode 会要改两遍）。

| 文件 / 函数 | 责任 |
|---|---|
| `Target` struct + `Kind` enum | 表达 transport：unix:/path 或 tcp://host:port |
| `ParseTarget(spec)` | 解析 `unix:/path` / `tcp://host:port` / 裸路径（legacy 兼容）|
| `Target.BaseURL()` | unix → `http://unix`；tcp → `https://<addr>` |
| `NewHTTPTransport(target, fingerprint, timeout)` | 返回 `*http.Transport`；KindUnix → unix DialContext；KindTCP → DialTLSContext with **强制 fingerprint pinning** |
| `dialTLSPinned()` | 内部 TLS dial：`InsecureSkipVerify: true` + handshake 后立即校验 leaf cert SHA256(DER) ≡ expected fingerprint，不匹配立即关闭 + 返回 error |
| `computeFingerprint(derBytes)` | mirror of `api.FormatFingerprint`；duplicated on purpose to avoid cyclic dep |
| `looksLikeFingerprint(fp)` | 校验 `sha256:HH:HH:...` 32 段 hex 格式 |

**为什么不导入 `internal/admin/api` 来复用 `FormatFingerprint`？** 那会引入 client → server-side 包的依赖；admin/api 是 server transport，clienttransport 是 client transport，分层得保持干净。format 函数 10 行重复 + 双向 unit test 守住合约比一个 cycle 更便宜。

### F2 — `internal/cli/admin_client.go`（extended）

`Client` struct 加 `baseURL` 字段（保留 `socketPath` 字段以维持 `SocketPath()` 诊断 API 兼容性）。

新增构造：
```go
func NewClientFromTarget(target clienttransport.Target, fingerprint string, timeout time.Duration) (*Client, error)
```

`do()` URL 构造改用 `c.baseURL + path`，未设置时 fallback 到 `"http://unix"`（保持旧 zero-value 行为）。

### F3 — `internal/workerdaemon/adminclient.go`（extended）

镜像 F2：`AdminClient` struct 加 `baseURL`，新增 `NewAdminClientFromTarget`，request URL 用 baseURL。

### F4 — `internal/cli/build.go` lazyApp.build() wiring

新优先级：
1. `AGENT_CENTER_ADMIN_TARGET` env 非空 → 用它 + `AGENT_CENTER_SERVER_FINGERPRINT` env 构造 `NewClientFromTarget` Client（**跨主机 CLI 入口**）
2. `cfg.Server.AdminSocketPath` 存在 + socket file 存在 → 老 `NewClient` 路径（本机 CLI）
3. 都没有 → `app.Client = nil`，handler 走 legacy 直接 Service 路径（v2.2-B 过渡）

### F5 — `cmd/worker-daemon/main.go` 命令行

新增 2 flag：
- `--admin-target` (e.g. `unix:/run/admin.sock` or `tcp://host:7300`) — 覆盖 `cfg.Server.AdminSocketPath`，**这就是 worker 跨机部署的入口**
- `--server-fingerprint` (`sha256:HH:HH:...`) — tcp 模式必须；fallback to `AGENT_CENTER_SERVER_FINGERPRINT` env

旧 `cfg.Server.AdminSocketPath` 仍兜底（当 --admin-target 不给时）。

Boot log 改 `socket=X` → `target=X` 以反映 transport 一般化。

## § 2. Security gate（FROZEN per @oopslink）

| 安全决策 | 实施位置 | 测试覆盖 |
|---|---|---|
| **空 fingerprint → reject**（不容许"加密但不验"）| `clienttransport.NewHTTPTransport` 早 return error | `TestNewHTTPTransport_TCPRequiresFingerprint` |
| **Malformed fingerprint → reject** | `looksLikeFingerprint` 检查 32 段 hex | `TestNewHTTPTransport_TCPRequiresValidFingerprint` |
| **Fingerprint mismatch → fail immediately, no fallback** | `dialTLSPinned` close connection + return descriptive error containing got vs expected + MITM warning | `TestTLSPinning_MismatchFails` |
| **Compare case-insensitive** | `equalFingerprint` via `strings.EqualFold` | 隐式（手动 e2e 时尝试小写也接受）|

## § 3. Verification

| Gate | Result |
|---|---|
| `go test ./internal/admin/clienttransport/ -v` | **10/10 pass**（ParseTarget x4 + transport require fingerprint x3 + BaseURL + round-trip + mismatch）|
| `go test ./...`（全量）| all green |
| `make lint` | green |
| `make smoke` | `smoke pass: 8 seconds`（v2.2 unix-only pipeline 不受影响）|
| 手动 e2e（real binaries）| 见 § 3.1 |

### § 3.1 手动 e2e — full client→server TCP+TLS+fingerprint path

**Positive path**:
```
$ ./bin/agent-center server --config=/tmp/v237b-server.yaml &
agent-center server: ...
  admin tcp:  127.0.0.1:7311 (TLS, auto-generated)
              fingerprint: sha256:97:3C:8F:...

$ ./bin/agent-center-worker-daemon \
    --worker-id=W-tcp-1 \
    --admin-target=tcp://127.0.0.1:7311 \
    --server-fingerprint="sha256:97:3C:8F:..." \
    --admin-token="$(cat /tmp/bootstrap_token)" \
    --capabilities=fakeagent --fake-agent=./bin/fakeagent
[worker] starting: worker_id=W-tcp-1 target=tcp://127.0.0.1:7311 poll=1s overrides=1
[worker] enrolled as worker_id=W-tcp-1               # ← TCP+TLS+fingerprint pinning succeeded

$ ./bin/agent-center worker list --config=/tmp/v237b-server.yaml
WORKER_ID    STATUS    CAPABILITIES
W-tcp-1      offline   fakeagent                    # ← server-side state confirms enroll landed
```

**Negative path (MITM defense)**:
```
$ ./bin/agent-center-worker-daemon \
    --worker-id=W-mitm-1 \
    --admin-target=tcp://127.0.0.1:7311 \
    --server-fingerprint="sha256:00:00:00:...00" \   # ← wrong fingerprint
    --admin-token="..." ...
[worker] starting: worker_id=W-mitm-1 target=tcp://127.0.0.1:7311 poll=1s overrides=1
[worker] fatal: runtime: initial enroll: adminclient: do POST /admin/workforce/worker/enroll: Post "https://127.0.0.1:7311/admin/workforce/worker/enroll": clienttransport: fingerprint mismatch dialing 127.0.0.1:7311: got sha256:97:3C:8F:..., expected sha256:00:00:00:...00 — the server cert changed (rotated?) OR you're being MITM'd; do NOT proceed until verified
```

Worker exits 1 immediately, **no enroll, no silent fallback** — MITM defended.

## § 4. Deviation

None vs the 4 frozen defaults. One additional choice (within scope): made the fingerprint comparison **case-insensitive** so operators pasting from a lowercase source (some terminal copy/paste workflows lowercase hex) still work. Format contract still requires the SSH-style colons.

## § 5. § 0.6 layer discipline

- **Observation**: pre-7b CLI / worker daemon only dialed unix socket
- **Capability**: clienttransport package + new `NewXxxFromTarget` constructors now support TCP+TLS with strict fingerprint pinning
- **Not claimed**: 「v2.2 设计假设了 client 和 server 同机」/ 「mTLS 是错的」 etc. — 仅描述 transport 之前不支持 TCP，现在支持，安全模式由 7a 已确定的 fingerprint pinning 落地

## § 6. v2.3-7c 衔接

7c 任务：per-token rate limit middleware + audit log 加 client IP 字段。两个能力在 TCP 暴露后才有意义（unix socket 信任本地，TCP 暴露要防 token 偷后无脑刷 + 要审计 IP）。

## § 7. 关联

- 上游：v2.3-7a server side TCP+TLS listener
- 上游：@oopslink 2026-05-26 fingerprint pinning 决策（thread `#agent-center:b79f992f`）
- 下游：v2.3-7c per-token rate limit + audit log client IP
- 下游：v2.3-7d 跨机 e2e + DEPLOYMENT 文档
