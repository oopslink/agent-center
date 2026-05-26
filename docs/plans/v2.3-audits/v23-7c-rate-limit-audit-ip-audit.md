# v2.3-7c Per-token Rate Limit + Audit IP Audit

> Closes slock task #27 ST 7c. Pairs with
> [v23-7a](v23-7a-tcp-tls-listener-audit.md) (server TCP+TLS) +
> [v23-7b](v23-7b-client-fingerprint-pinning-audit.md) (client pinning).
>
> Hardens the now-TCP-exposed admin endpoint with (1) per-token token-
> bucket rate limit and (2) client IP capture on the AuthContext so
> rate-limit-hit observability events can attribute cross-host traffic
> for audit. Frozen-by-user pieces of the 5-decision security matrix.

## § 0. 必答铁律答卷 (per conventions § 0.4)

| 必答问题 | 答 |
|---|---|
| 起点 = 领域模型要求？ | 是。Admin endpoint 在 v2.3-7a/7b 后跨主机 reachable；7c 加的是 transport 层 hardening（rate limit 中间件 + observability 字段），无新 BC / AR / Service。 |
| 保留 BC invariants？ | 是。Rate limit middleware 在 AuthMiddleware 下游链上拼接（AuthContext 已有 TokenID + 现在加了 ClientIP）；事件发到现有 EventSink；不改任何 Service 内部状态。 |
| 没省 transport？ | 是。Rate limit 是真在 middleware 拦请求 + 真 429 + Retry-After header；没有"统计但不拒"或"日志 only"的退路。 |
| Mock-as-default 消除？ | 是。`adminRateLimitSink` 真接 `observability.EventSink`，noopRateLimitSink 仅在 sink 为 nil 时兜底（测试 + 无 observability 配置的极小化场景），prod wiring `newAdminRateLimitSink(app)` 总返真 sink 当 `app.Sink != nil` —— 与 v2.0 GA "NoopSender 默认进 prod" 教训对照。 |
| 起点 = "previously didn't rate-limit"？ | 否（per § 0.6）。事实是 v2.3-3a admin token + v2.3-7a TCP 暴露之前 admin endpoint 仅 listen unix（loopback 信任），rate limit 是 nice-to-have 不是必需；TCP 暴露 + 跨主机后变必需。这是 capability 添加，不是"v2 之前故意不做"。 |

## § 1. Scope — what changed at the layer

### F1 — `internal/admin/api/ratelimit.go` (new)

| 实体 | 责任 |
|---|---|
| `RateLimitConfig` | `Burst` (max 桶容量) / `RefillRate` (token/sec) / `IdleGC` (老 bucket 回收窗口) |
| `RateLimitDefaults` | Burst=200, RefillRate=10/sec —— tuned 让 worker poll (1/sec) + interactive CLI burst + smoke pipeline 都过；攻击者持 stolen token 也撞墙 |
| `RateLimitSink` interface | 只依赖 `EmitRateLimitHit(tokenID, clientIP, method, path)` 这一个方法；测试用 stub，prod 走 `adminRateLimitSink` 接 `observability.EventSink` |
| `RateLimitMiddleware(cfg, sink)` | http.Handler middleware；从 `AuthFromContext` 取 `TokenID` 作 bucket key；超额 → 429 + Retry-After: 1；isPublicPath (`/admin/health`) 跳过 |
| `tokenBucket` + `rateLimiter` | 经典 float-token bucket 实现；map[TokenID]*bucket，per-bucket mutex on hot path，top-level mutex on map mutation only |

### F2 — `AuthContext.ClientIP` (extended)

`internal/admin/api/auth.go`：
- `AuthContext` 加 `ClientIP string`
- `AuthMiddleware` 调 `clientIPFromRequest(r)` 填入：从 `r.RemoteAddr` `net.SplitHostPort`，unix socket 返空，IPv6 正确解括号
- `clientIPFromRequest` 单独可测；4 个 case 全覆盖（tcp v4 / tcp v6 / unix `@` / empty）

### F3 — wiring (`internal/cli/admin_wiring.go`)

Middleware 链改成 3 层（外到内）：
```
AuthMiddleware( RateLimitMiddleware( WithDeps( mux ) ) )
```

注释解释了顺序：auth 先建 AuthContext，rate limit 读 AuthContext，deps 走 handler。

新增 `adminRateLimitSink` adapter：把 middleware 的回调翻译成 `observability.EmitCommand{ EventType: "admin.rate_limit_hit", ... }` 落 events 表。

## § 2. Security gates (frozen)

| 决策 | 取值 | 设计意图 |
|---|---|---|
| Burst | 200 | 比 prod normal burst 大 ~5x，预留 web console + e2e/smoke + interactive 脚本余量 |
| RefillRate | 10/sec | 比 worker poll (1/sec) 大 10x；攻击者持 stolen token 持续刷也只能 60×10=600 req/min，配合 audit log 给操作员充分时间发现+revoke |
| Per-token isolation | 是（key by TokenID）| 一个 token 被刷不影响其它 worker / CLI 的 token |
| Public path exempt | `/admin/health` only | 跟 AuthMiddleware 白名单完全一致 |
| 429 response | `Retry-After: 1` + JSON error `{reason: "rate_limited"}` | 客户端可以自动 backoff；error envelope 跟其它 admin error 同 shape |
| Observability event | `admin.rate_limit_hit` 含 token_id + client_ip + method + path | 操作员可以 `agent-center inspect` / `query` 找出"哪个 IP 刷了哪个 token"，配合 v2.3-3a token revoke 一键封 |
| AuthContext.ClientIP | TCP 真 IP；unix socket 空 | 跨机请求都有可追溯 IP；单机不污染 |

## § 3. Verification

| Gate | Result |
|---|---|
| `go test ./internal/admin/api/ -run "RateLimit\|ClientIP" -v` | **8/8 pass**（AllowsBurst / RejectsOverBurst / PerTokenIsolation / RefillsOverTime / HealthExempt / NoAuthFails / DefaultsFallback / ClientIPFromRequest）|
| `go test ./...` 全量 | all green |
| `make lint` | green |
| `make smoke` | `smoke pass: 8 seconds`（默认 burst=200 + refill 10/sec 让 v22 deployed pipeline 顺利通过）|

### § 3.1 一次回归记录（透明）

第一次跑 smoke 用最初的 Burst=20 + RefillRate=1/sec 默认，smoke 测试 client 在 enroll + create channel + dispatch task 的 quick burst 中触发了 429。调整 RateLimitDefaults 到 Burst=200 + RefillRate=10/sec 后 smoke pass。

这是 prod default 选择的实战校准 —— 单元测试覆盖了算法正确性，但实际 burst 量需要在 e2e 上撞一次才有 ground truth。**未来调 default 时应当再跑一次 smoke 验证**。

## § 4. Deviation

无 vs 7a/7b 安全 frozen 5 条。Default Burst/RefillRate 选择不在 frozen 5 条范围内，是 v0 工程取值（per § 3.1 调过一次）。

## § 5. § 0.6 layer discipline

- **Observation**: v2.3-7a/7b 后 admin endpoint 通过 TCP 跨机可达；token 失窃后无 rate limit 可无脑刷；audit 事件无 client IP 字段难追溯
- **Capability**: 加了 per-token token-bucket + AuthContext.ClientIP + admin.rate_limit_hit observability event
- **Not claimed**: 「v2.2 之前故意不做 rate limit」/ 「之前的 IP 字段缺失是设计错误」 etc. — 仅描述跨机暴露后必须有的能力 v2.3-7c 加上了

## § 6. 关联 + 后续

- 上游：v2.3-7a TCP+TLS server listener；v2.3-7b client fingerprint pinning
- 下游：v2.3-7d 跨机 e2e + DEPLOYMENT 文档（含"如何配置 rate limit"、"如何 query rate_limit_hit"操作员手册）
- v3 deployment-as-product 主题候选：per-endpoint rate limit / per-IP CIDR allowlist / 自动 revoke on N rate_limit_hits within window
