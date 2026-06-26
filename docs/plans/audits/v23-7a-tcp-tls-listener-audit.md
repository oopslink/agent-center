# v2.3-7a TCP+TLS Admin Listener Audit

> Closes slock task #27 ST 7a (server-side TCP+TLS). v2.3-7b will land
> the client-side fingerprint pinning (worker daemon + CLI
> `--admin-target=tcp://<fingerprint>@<host>:<port>`).
>
> Triggered by @oopslink's 2026-05-26 directive: enable cross-host
> server / worker / CLI by adding a TCP+TLS leg to the admin endpoint.
> The 4 engineering defaults below were locked in thread
> `#agent-center:b79f992f` before code; @oopslink picked fingerprint
> pinning over TLS-no-verify after the MITM threat-matrix discussion.

## § 0. 必答铁律答卷 (per conventions § 0.4)

| 必答问题 | 答 |
|---|---|
| 起点 = 领域模型要求？ | 是。Admin endpoint 既有 5+ BC 的 49+ AppService 入口；新增 TCP+TLS 是 **transport 选项**，不动任何 AR / Service / Repo。配置层加 3 个 ServerConfig 字段（`AdminTCPListen` / `AdminTLSCertPath` / `AdminTLSKeyPath`），传输层加 1 个新 module（`internal/admin/api/tls.go`）+ Server 扩展支持双 listener。**没有新 BC**，**没有新 AR**。 |
| 保留 BC invariants？ | 是。AuthMiddleware（v2.3-3a）+ WithDeps 中间件链 unchanged；同一 mux 通过 `SetHandler` 同时挂在 unix + tcp 两个 `http.Server`，两条腿用 **同一 token + 同一 scope** 鉴权。AppService 单入口规则不破。 |
| 没省 transport？ | 是。TCP leg 必须走 TLS（`http.Server.ServeTLS`），明文 HTTP 不暴露在 TCP；client 端拿到的 cert 通过 fingerprint pinning 校验（v2.3-7b 实施 client，本期 server 给出 fingerprint）。bearer token 仍是必须，TLS 是 + 不是 - 替代 token。 |
| Mock-as-default 消除？ | N/A — 这是新建 transport 模块，没有从 mock 切换 prod 的迁移路径。`generateAndPersist` 使用 `crypto/rand` 真随机；test 助手 `writeExpiredCertPair` 也走真 ECDSA。 |
| 起点 = "previously didn't support multi-host"？ | 否（per § 0.6）。事实是 **admin endpoint 之前只 listen unix socket，物理上限制了 server / worker / CLI 必须同机同用户**。文档注释 `02-deployment § 2.2` 留了"多机 TCP transport 在 v2.3" 的占位，但 v2.2 没有 unix 之外的 listener 代码；v2.3-7a 才把 TCP+TLS 这条腿落地。这是 capability 添加，不是改设计意图。 |

## § 1. Scope — what changed at the layer

### F1 — Config (3 个新字段)

`internal/config.ServerConfig`：
- `AdminTCPListen string \`yaml:"admin_tcp_listen"\`` — empty = TCP disabled（默认行为）
- `AdminTLSCertPath string \`yaml:"admin_tls_cert_path"\`` — empty → `filepath.Dir(SqlitePath) + "/admin-tls.crt"`
- `AdminTLSKeyPath string \`yaml:"admin_tls_key_path"\`` — empty → `filepath.Dir(SqlitePath) + "/admin-tls.key"`

YAML loader 的已知 key 集合 + env-var overrides 都同步扩展（`AGENT_CENTER_SERVER_ADMIN_TCP_LISTEN` 等）。fingerprint 文件路径走默认 `filepath.Dir(SqlitePath) + "/admin-tls.fingerprint"`，未对外暴露 config 字段（操作员通过 boot banner + 文件路径都能拿到）。

### F2 — Cert auto-gen + fingerprint module (`internal/admin/api/tls.go`)

| 函数 | 责任 |
|---|---|
| `LoadOrGenerateCert(certPath, keyPath, hostname)` | 主入口。Both exist → load + validate not-expired；both missing → generate fresh；一边在另一边缺 → 报错"delete both to regenerate"；expired → 报错"delete to regenerate"。 |
| `FormatFingerprint(derBytes)` | SHA256(DER) → `sha256:HH:HH:...:HH` 32-byte SSH-style format。 |
| `WriteFingerprintFile(path, fp)` | mode 0644 写入 + trailing newline。 |
| `CertExpiryWarning(cert)` | `(bool, daysRemaining)` —— ≤ 30 天到期 → warn=true，boot 代码用来决定是否 emit observability event。 |
| `CertNotAfter(cert)` | 抽 NotAfter，banner 用。 |

Cert spec（FROZEN per @oopslink）：
- ECDSA P-256（比 RSA-2048 小 5x，签速快 10x，足够安全到 2030+）
- 1 年有效期
- CN = `agent-center`
- SAN = `127.0.0.1`, `::1`, 所有 `net.Interfaces()` 上的非 link-local IPv4/IPv6, `localhost` DNS, 配置 hostname（如 non-empty）

### F3 — Server 扩展（`internal/admin/api/server.go`）

`Server` struct 新增 5 个字段：`tcpListenAddr`, `tlsCert`, `tlsFingerprint`, `tcpSrv`, `tcpListener`。

新构造器：
```go
func NewServerWithTransports(socketPath, tcpListenAddr string,
    tlsCert *tls.Certificate, tlsFingerprint string,
    deps ServerDeps) *Server
```

`ListenAndServe` 重构：
- 两个 listener 都未配 → `errors.New("admin api: at least one of socket_path or tcp_listen required")`
- `tcpListenAddr != ""` 但 `tlsCert == nil` → 立即报错（boot 必须先 LoadOrGenerateCert）
- 任一配置 → 起对应 listener；都配 → 起两个 goroutine 并发 Serve，等任一返回非 `ErrServerClosed` 错误。
- TCP leg 走 `http.Server.ServeTLS(ln, "", "")`，TLSConfig 含 `Certificates + MinVersion=TLS12`。

`SetHandler` 同时改 unix + tcp 两个 `http.Server.Handler`，保证 AuthMiddleware 链对两条腿一视同仁。

`Shutdown` 并发关两个 listener + 清理 unix socket file（保持原有行为）。

### F4 — `healthHandler` 改造

`/admin/health` 根据 `r.TLS != nil` 报告 `transport: "tcp"` vs `"unix"`，方便操作员确认实际打到的是哪条腿。其它字段不变。

### F5 — CLI wiring（`internal/cli/admin_wiring.go` + `handlers_system.go`）

- 新 types：`AdminTransportConfig` + `AdminTransportInfo`
- `runAdminEndpoint` 签名从 `(ctx, app, socketPath, logger)` 改为 `(ctx, app, AdminTransportConfig, logger)`，返回 `(AdminTransportInfo, cleanup, err)`
- 新 helper：`adminTransportFromCfg(cfg)` — 把 ServerConfig 翻译成 transport config（处理路径默认值）
- 新 helper：`hostnameForCertSAN()` — `os.Hostname()` 包装
- ServerCommand 的 server boot：
  - 任一 admin 字段非空 → 启动 admin endpoint
  - TCP 启用 → boot banner 追加 cert path、validity、fingerprint
  - cert 在 30 天到期窗口 → `app.Sink.Emit("admin.tcp_cert_expiring", ...)` observability event

## § 2. 4 个工程默认（@oopslink frozen）

| # | 决策 | 取值 | 为什么这个值 |
|---|---|---|---|
| 1 | Cert + SAN | CN=`agent-center`, SAN=`127.0.0.1`+`::1`+所有 interface IP+config hostname+`localhost`, ECDSA P-256, 1 年 | 标准 self-signed 模式；ECDSA 在 modern stack 上 default；1 年比 90 天更适合"操作员一年记得换"心智模型，又比 5 年安全。`localhost` DNS 始终加进去以兼容 `curl localhost:7300`。 |
| 2 | Fingerprint 格式 | `sha256:HH:HH:...` (SSH-style, 32 段) | SPKI 模式能让 cert renew 不变 fingerprint，但 v2.3 destructive rotation = delete + regenerate，cert hash 就够；SSH 操作员都熟，pinning 文件 / SCP / 邮件粘贴体验顺。SPKI 是 v3 候选。 |
| 3 | Cert expiry warning 窗口 | 30 天 | 标准；操作员有 1 个月反应时间。事件 `admin.tcp_cert_expiring` 落 observability，运维可以从 inspect/query 拿到。 |
| 4 | TCP-only 模式 | 支持 `admin_socket_path: ""` + `admin_tcp_listen` 非空 | flexibility for "纯远程 server" 场景（VPS only，本机无人 SSH）。Both empty = boot fail-fast（server 没 admin endpoint = broken）。 |

## § 3. Failure modes — 6 条全覆盖 + 测试

| 情况 | 行为 | 测试 |
|---|---|---|
| 两个 admin 字段都空 | boot error `"admin api: at least one of socket_path or tcp_listen required"` | `TestAdminServer_BothEmpty_BootError` |
| `tcp_listen` set + cert 文件存在但 key 缺失 | boot error `"cert exists at X but key missing at Y — delete both to regenerate"` | `TestLoadOrGenerateCert_CertExistsKeyMissing` |
| `tcp_listen` set + key 存在但 cert 缺失 | boot error `"key exists at X but cert missing at Y — delete both to regenerate"` | `TestLoadOrGenerateCert_KeyExistsCertMissing` |
| `tcp_listen` set + cert expired | boot error `"cert at X expired YYYY-MM-DD — delete cert + key to regenerate"` | `TestLoadOrGenerateCert_Expired` |
| `tcp_listen` set + `tlsCert == nil` 进 Server | boot error `"tcp_listen set but tlsCert is nil"` | `TestAdminServer_TCPSetButCertNil_BootError` |
| Fingerprint 文件写失败（dir 不可写）| warning logged via passed-in `logger` func, server still boots (fingerprint 也打到 boot banner) | 手动测：parent dir 权限 0500，admin 起来但 logger 记 warning |

## § 4. Verification

| Gate | Result |
|---|---|
| `go test ./internal/admin/api/ -run "Cert\|TLS\|LoadOr\|AdminServer_TCP\|AdminServer_Both"` | 13/13 pass |
| `go test ./...`（全量） | all green |
| `make lint` | green (vet + no-vendor-refs + no-mock-default + doc-impl-drift) |
| `make smoke` | `smoke pass: 10 seconds`（deployed-binary pipeline 不受影响） |
| 手动 e2e | 见 § 4.1 |

### § 4.1 手动 e2e（fresh binary）

```yaml
# /tmp/v237a-test.yaml
server:
  listen_addr: "127.0.0.1:7090"
  sqlite_path: "/tmp/v237a-test.db"
  admin_socket_path: "/tmp/v237a-admin.sock"
  admin_tcp_listen: "127.0.0.1:7310"
identity:
  default_user: hayang
web_console:
  enabled: false
```

Boot banner:
```
agent-center server: db=/tmp/v237a-test.db listen=127.0.0.1:7090 web=disabled admin=/tmp/v237a-admin.sock (escalator running)
  admin tcp:  127.0.0.1:7310 (TLS, auto-generated)
              cert valid until 2027-05-26 (364 days)
              fingerprint: sha256:17:32:F2:59:73:E0:11:11:60:56:7E:06:A4:DB:3C:53:EF:53:93:E2:6B:76:94:10:DA:9E:65:F8:FE:DD:01:7B
```

Files created next to sqlite:
- `/tmp/admin-tls.crt`（mode 0644，769 bytes，PEM）
- `/tmp/admin-tls.key`（mode 0600，227 bytes，ECDSA P-256 PEM）
- `/tmp/admin-tls.fingerprint`（mode 0644，含 banner 同款字符串 + newline）

Curl 验：
```bash
# 公共健康
$ curl -sk https://127.0.0.1:7310/admin/health
{"endpoint":"admin","ok":true,"transport":"tcp"}   # transport 字段正确报告 tcp

# 带 token 调真 endpoint
$ TOKEN=$(cat /tmp/bootstrap_token)
$ curl -sk -H "Authorization: Bearer $TOKEN" https://127.0.0.1:7310/admin/workforce/worker/find-all
[]                                                  # auth pass，shared mux 工作
```

## § 5. Deviation

None against the frozen 4 defaults. 一处 v2.3-7a 之外的顺手补：`healthHandler` 从硬编码 `transport: "unix"` 改为按 `r.TLS != nil` 上报真 transport — 这是 § 3 失败模式表里"操作员怎么确认打到哪条腿"的最便宜实现，逻辑改动 3 行，单元测试覆盖未变（health 测试仍走 unix）。

## § 6. § 0.6 layer discipline

- **Observation**：v2.2 admin endpoint 只 listen unix socket。
- **Capability**：transport 层之前不支持 TCP；BC / AppService / 鉴权 / scope 都不影响 transport 选择。
- **Not claimed**：「v2.2 design 假设了单机」/ 「v2 是 personal tool」之类的 design-intent 推断。仅描述 transport 之前没做、现在做了。

## § 7. v2.3-7b 衔接（不在本期 scope）

7b 的 client 侧任务（worker daemon + CLI）需要：
- 接受 `--admin-target=tcp://<fingerprint>@<host>:<port>` 或拆字段
- 用 `crypto/tls` Dial 时 InsecureSkipVerify + 在 connection-state 校验 leaf cert SHA256(DER) 匹配 fingerprint，不匹配立即 fail（不沉默 fallback）
- bearer token 在 `Authorization: Bearer` header（沿用 v2.3-3a）
- 同 admin Client 抽象（同 unix path），只是 transport 切换

`FormatFingerprint` 已 export，client 侧直接复用 —— 这是本期为 7b 留的 hook。

## § 8. 关联

- 上游：v2.3-3a admin token auth（`docs/plans/v2.3-audits/v23-3a-admin-token-auth-audit.md`）—— 鉴权中间件已经 transport-agnostic，本期直接复用
- 上游：@oopslink 2026-05-26 fingerprint pinning 决策（thread `#agent-center:b79f992f`）
- 下游：v2.3-7b client-side fingerprint pinning
- 下游：v2.3-7c per-token rate limit + audit log 加 client IP（TCP 暴露后的强化）
- 下游：v2.3-7d 跨机 e2e 测试 + DEPLOYMENT 文档更新
