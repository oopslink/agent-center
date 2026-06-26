# v2.3-7d Multi-host Deployment Documentation + Combined Verification

> Closes slock task #27 ST 7d (final). Pairs with
> [v23-7a](v23-7a-tcp-tls-listener-audit.md) (server TCP+TLS) +
> [v23-7b](v23-7b-client-fingerprint-pinning-audit.md) (client pinning) +
> [v23-7c](v23-7c-rate-limit-audit-ip-audit.md) (rate limit + audit IP).
>
> Hands operators an end-to-end "how to run server on host A + worker on
> host B" guide and verifies the full 7a+7b+7c stack works together in a
> single deployed-binary flow.

## § 0. 必答铁律答卷 (per conventions § 0.4)

| 必答问题 | 答 |
|---|---|
| 起点 = 领域模型要求？ | 是。7a/7b/7c 落了所有 capability；7d 是文档 + 完整路径验证；不改 BC / AR / Service / transport。 |
| 保留 BC invariants？ | N/A — 纯文档 + verification。 |
| 没省 transport？ | N/A。 |
| Mock-as-default 消除？ | N/A。 |
| 起点 = "previously didn't have deployment guide"？ | 否（per § 0.6）。v2.2 single-host guide 已经存在；v2.3 加了 multi-host capability 所以需要 multi-host guide。这是文档跟着 capability 增量。 |

## § 1. Scope

### F1 — `docs/deployment/v2.3-multi-host.md` (new)

Operator-facing guide：
- § 1 架构图（host A server + host B worker，TCP+TLS 中间）
- § 2 server side：config 三新字段 + 启动 + 读 fingerprint + firewall
- § 3 worker side：拿 binary + handoff fingerprint+token + 跑命令 + 验证
- § 4 token + fingerprint 维护：mint per-worker scoped token / 轮换 token / 轮换 cert（destructive）
- § 5 rate limit：默认值 + 用 observability events 找异常
- § 6 troubleshooting 表（6 个常见症状 + 修法）
- § 7 显式列出 v2.3 不做、留给 v3 / v2.4 的能力
- § 8 references 链回 7a/7b/7c audit + v2.4 deployment first-mile plan + v2.2 单机 guide

### F2 — 文案对齐修正

- 7c audit doc 之前用 `agent-center inspect / query` 描述查 events 的命令；实际 CLI 是 `agent-center query events --type=...`。已修正。
- multi-host guide 中所有 events 查询命令同步使用正确语法。

## § 2. Combined verification — 7a+7b+7c 完整路径手测

跑 fresh binary on host A + worker on simulated host B（loopback），验证所有 7x ST 落实的能力同时工作：

```
✅ 1. Server boot TCP+TLS listener
   admin tcp:  127.0.0.1:7312 (TLS, auto-generated)

✅ 2. Worker dial TCP+TLS with correct fingerprint (7a server cert + 7b client pinning)
   [worker] starting: worker_id=W-tcp-pos target=tcp://127.0.0.1:7312 poll=1s overrides=1
   [worker] enrolled as worker_id=W-tcp-pos

✅ 3. Health endpoint via TCP reports transport=tcp (7a healthHandler enhancement)
   $ curl -sk https://127.0.0.1:7312/admin/health
   {"endpoint":"admin","ok":true,"transport":"tcp"}

✅ 4. Rate limit kicks in on burst (7c per-token bucket)
   250 fast requests with valid token → 200=215, 429=35
   (= burst capacity 200 + 10/sec refill consumed during ~3s loop test)

✅ 5. Rate limit hits emit observability events (7c admin.rate_limit_hit)
   middleware adminRateLimitSink → EventSink.Emit → events DB row
   (queryable via `agent-center query events --type=admin.rate_limit_hit`)
```

**MITM defense** (7b) separately verified in 7b audit § 3.1 (wrong fingerprint = immediate fatal exit on worker daemon).

## § 3. Test gates final state

| Gate | Result |
|---|---|
| `go test ./...` 全量 | all green (across 7a unit/integration + 7b clienttransport + 7c rate limit) |
| `make lint` | green |
| `make smoke` | `smoke pass: 8 seconds` |
| 手动 e2e（7a+7b+7c combined）| § 2 全过 |

## § 4. v2.3-7 任务完整性 — #27 全 ST 收口

| ST | 落了什么 | 测试 | 手动验证 | 文档 |
|---|---|---|---|---|
| 7a | server TCP+TLS listener + cert auto-gen + fingerprint output + expiry warning | 13 tests | ✅ /admin/health over TLS + cert files at correct modes | v23-7a audit |
| 7b | clienttransport package + worker daemon flags + CLI env vars + fingerprint pinning | 10 tests | ✅ worker enroll + MITM defense | v23-7b audit |
| 7c | per-token rate limit + AuthContext.ClientIP + admin.rate_limit_hit event | 8 tests | ✅ 250 burst → 200+429 split + event landed | v23-7c audit |
| 7d | multi-host operator guide + combined e2e verification | N/A | ✅ § 2 | v23-7d audit (本) + v2.3-multi-host.md |

**总：31 new tests pass，4 audit docs，1 deployment guide，跨主机 transport 全闭环。**

## § 5. Deviation

无 vs 7a/7b/7c security frozen 决策。7d 没做 Playwright e2e —— 评估认为 7a/7b 各自的 unit + integration tests + 7b 已记录手动 e2e + 本 7d combined § 2 手动 e2e 三重覆盖足够（Playwright 在跨主机 binary 路径上的增量价值需要 ~500 行 spec 代码 + 维护成本，而且已经有 v22-deployed-pipeline 作 unix-socket 路径的 reference）。Playwright 跨主机 e2e 是 v3 deployment-as-product 的合理候选（同 CI 一并加）。

## § 6. § 0.6 layer discipline

- **Observation**: 7a/7b/7c capability 全部 ship，无操作员文档说明如何使用 + 无 combined e2e 证据
- **Capability**: 添加 multi-host deploy guide + 7a+7b+7c 三件套 combined 路径手动 e2e proof
- **Not claimed**: 「v2 之前对 multi-host 没意识」/ 「v2.2 设计假设单机」之类的设计意图推断 — 仅描述 v2.3 ship 的 capability 现在有了文档覆盖

## § 7. #27 收尾后续

- #27 整体（7a/7b/7c/7d 全部）→ 准备 mark done
- 7a/7b/7c/7d 5 个 commits 准备一并 push 到 origin/main
- v2.4-D first-mile deployment（@AgentCenterPD 已建 task #34）跟 #27 协同：multi-host 已 work，下一步是把"操作员手输 4 个 flag"简化为"WC 上 copy-paste 一行命令"
- 旁注：deployment-as-product v3 主题需要把 v2.3 multi-host 这层手动 ops 集成进 first-class 工具（Dockerfile / CI / one-command install / Prometheus / 自动 cert renew 等）

## § 8. 关联

- 上游 audits: v23-7a / v23-7b / v23-7c
- 操作员文档: `docs/deployment/v2.3-multi-host.md`（new in 7d）
- 单机参考: `docs/deployment/v2.2-mac-single-host.md`
- 后续: v2.4-D task #34 first-mile deployment / v3 deployment-as-product 主题
