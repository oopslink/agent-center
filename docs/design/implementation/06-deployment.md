> ⚠ **v1-era doc** — pending rewrite in Phase 10 / 11 / 12. v2 撤回了 Bridge BC + 飞书集成 (per [ADR-0031](../decisions/drafts/0031-v2-drop-bridge-vendor-integration.md))；本文中 Bridge / vendor / 飞书 / 已删 ADR 引用是 v1 残留。

# 部署 / systemd / 升级

> **实现层** · v1 单 VPS 部署的 systemd unit / 文件系统布局 / 升级流程 / 备份恢复 / 日志 rotation。
>
> 本文档 **元层规则 § 1-3（拓扑 / 交付 / 文件布局）+ systemd 集成 § 4-5 + 备份 / 日志 § 6-7 + 监控 / 网络 § 8-9 + 首次安装 § 10 + 设计层对位 § 11**。具体路径与脚本以本文档为准。

## § 1. 部署拓扑

### 1.1 v1 拓扑（[domain-vision § B2](../architecture/strategic/00-domain-vision.md) + [system-overview § 部署形态](../architecture/strategic/02-system-overview.md)）

```
┌─────────────────────────────────────────┐
│ VPS（单台）                              │
│                                          │
│  agent-center server （常驻 / systemd） │
│  agent-center supervisor （事件触发 spawn，短生命周期）
│                                          │
└─────────────────────────────────────────┘
              ▲                ▼
              │            ~~飞书 WebSocket~~ (v2 删 per ADR-0031)
              │           ~~（Center 主动出站，无入站）~~
              │
              │ gRPC 长连接 (Worker 主动出站)
              │
┌─────────────┴───────────────────────────┐
│ Worker 机器（N 台，用户开发机）           │
│                                          │
│  agent-center worker （常驻 / user systemd）
│    └─ per-execution shim 进程 (detached / setsid)
│         └─ agent CLI 子进程 (claude / codex / ...)
└─────────────────────────────────────────┘
```

| 进程 | 部署位置 | 生命周期 | 由谁起 |
|---|---|---|---|
| `agent-center server` | VPS | 常驻 | systemd（root / 系统级 unit） |
| `agent-center supervisor` | VPS | 短生命周期 spawn | Center 内部 events 触发 fork+exec；不需 unit |
| `agent-center worker` | Worker 机器 | 常驻 | user systemd（`--user`） |
| per-execution shim | Worker 机器 | 跟随 execution 生命周期 | Worker daemon spawn（detached）|
| agent CLI | Worker 机器 | 跟随 execution | shim fork+exec |

### 1.2 v1 不做（[domain-vision § B2](../architecture/strategic/00-domain-vision.md)）

- 多 Center / 多 VPS（多用户 SaaS 是"重做项目"）
- HA / failover
- 容器化 agent CLI（[roadmap § 容器化 agent 执行](../roadmap.md)）

---

## § 2. 二进制交付

### 2.1 构建

`agent-center` 是单一 Go binary（[conventions § 10](../../rules/conventions.md)）。Web Console React SPA 通过 `go:embed` 进 binary，不需单独分发。

```bash
# 完整构建（frontend + backend，单 binary）
make build                  # = build-frontend + build-backend

# 子目标
make build-frontend         # pnpm install + vite build → internal/webconsole/spa/dist/
make build-backend          # go build → ./bin/agent-center
```

CI build artifact 命名：

```
agent-center-<os>-<arch>-<git-sha>
  例：agent-center-linux-amd64-c0a1771
```

每次 main 分支推送都触发 build；产物上传 GitHub Release。Binary 含嵌入 SPA bundle (~0.5 MB compressed)，总大小 ~17-18 MB（Go runtime + sqlite3 + SPA）。

**SPA dev flow**（不需 rebuild binary）：在 `web/` 跑 `pnpm run dev`（vite :5173 + proxy `/api` 到 :7100），binary 端正常起 server；vite dev server 优先 hot-reload，binary 嵌的 SPA 不被消费。

### 2.2 安装路径

| 位置 | 说明 |
|---|---|
| `/usr/local/bin/agent-center` | 实际二进制（symlink → `/opt/agent-center/releases/<sha>/agent-center`）|
| `/opt/agent-center/releases/<sha>/` | 每次 release 的归档目录；保留最近 N 个回滚用 |
| `/opt/agent-center/current` | symlink → `/opt/agent-center/releases/<active-sha>` |

升级时换 `/opt/agent-center/current` 软链 + `systemctl restart`（[§ 5](#-5-升级流程)）。

### 2.3 用户 / 权限

| 用户 | 用途 |
|---|---|
| `agent-center`（system user，UID auto） | `agent-center server` 运行身份；owner of `/var/lib/agent-center/` |
| 实际人类用户（如 `hayang`） | SSH 登录跑 admin CLI（`agent-center query` / `worker proposal accept` / 等） |

人类用户加入 `agent-center` group → 能 `read /var/lib/agent-center/`；admin socket `/run/agent-center/admin.sock` 0660 group=`agent-center` 允许调用。

Worker 机器：`agent-center worker` 跑在**用户自己的账号**下（非 root），用 `user systemd`。

---

## § 3. 文件系统布局

### 3.1 VPS 端（center）

```
/etc/agent-center/
├── config.yaml                        # 主配置文件（mode=server）
└── feishu.env                         # ~~凭据 env 文件（systemd EnvironmentFile）~~ (v2 删 per ADR-0031)

/var/lib/agent-center/
├── agent-center.db                    # SQLite（02-persistence § 1.2）
├── agent-center.db-wal                # WAL 副本
├── agent-center.db-shm                # shared memory
├── blobs/                             # BlobStore root（01-blob-store）
│   ├── tasks/<task_id>/log.log.gz
│   ├── tasks/<task_id>/trace.jsonl.gz
│   └── supervisor_invocations/<inv_id>/session.jsonl.gz
└── memory/                            # Memory git repo（ADR-0012）
    ├── .git/
    ├── global.md
    ├── projects/<project_id>.md
    └── workers/<worker_id>.md

/run/agent-center/
└── admin.sock                         # 本机 admin CLI 接入（NF5）

/var/log/agent-center/
└── server.log                         # 启动 / panic 日志（journald 之外的备份）
```

### 3.2 Worker 端

```
~/.agent-center-worker/
├── config.yaml                        # mode=worker 配置
├── bootstrap-token                    # 一次性 enroll token（用完删）
├── session-token                      # 长期 session token（worker 跟 center 握手后写）
├── daemon.sock                        # worker daemon unix socket（NF11；agent CLI 入口）
├── exec/                              # per-execution 目录（ADR-0018）
│   └── <execution_id>/
│       ├── envelope.json
│       ├── status.json
│       ├── events.jsonl
│       ├── agent.log
│       ├── stderr.log
│       └── shim.sock
└── blobs-staging/                     # 上传 BlobStore 前的暂存
```

---

## § 4. systemd unit

### 4.1 Center server（系统级）

`/etc/systemd/system/agent-center.service`：

```ini
[Unit]
Description=agent-center server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=agent-center
Group=agent-center
ExecStart=/usr/local/bin/agent-center server --config=/etc/agent-center/config.yaml
# EnvironmentFile=-/etc/agent-center/feishu.env  # v2 删 per ADR-0031 — 飞书集成已撤回
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=agent-center

# 安全 hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/agent-center /run/agent-center /var/log/agent-center
RuntimeDirectory=agent-center
RuntimeDirectoryMode=0750

[Install]
WantedBy=multi-user.target
```

启停：

```
sudo systemctl enable --now agent-center
sudo systemctl restart agent-center
sudo journalctl -u agent-center -f
```

### 4.2 Worker daemon（用户级）

`~/.config/systemd/user/agent-center-worker.service`：

```ini
[Unit]
Description=agent-center worker daemon
After=network-online.target

[Service]
Type=simple
ExecStart=%h/.local/bin/agent-center worker --config=%h/.agent-center-worker/config.yaml
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal

# 注意：不能加 KillMode=control-group 之类的限制 ——
# per-execution shim 必须能脱离 daemon process group 存活（ADR-0018）
KillMode=process

[Install]
WantedBy=default.target
```

启停：

```
systemctl --user enable --now agent-center-worker
systemctl --user restart agent-center-worker     # ✓ 不杀 shim（KillMode=process + setsid）
journalctl --user -u agent-center-worker -f
```

> **`KillMode=process` 是必需的**（[ADR-0018 § 2](../decisions/0018-detached-agent-via-per-execution-shim.md)）。默认 `KillMode=control-group` 会把 daemon 的子孙进程一起 SIGTERM —— 而 shim 应该跟 daemon 生命周期解耦。结合 shim 内 `setsid()` 脱 cgroup，二者必须**同时**做对，单一保险层不够。

---

## § 5. 升级流程

### 5.1 Center server 升级

```bash
# 1. 拉新 binary
sudo install -m 0755 -o agent-center -g agent-center \
  agent-center-linux-amd64-<new-sha> \
  /opt/agent-center/releases/<new-sha>/agent-center

# 2. 切 symlink
sudo ln -sfn /opt/agent-center/releases/<new-sha> /opt/agent-center/current

# 3. 重启服务
sudo systemctl restart agent-center

# 4. 跟踪
sudo journalctl -u agent-center -f
```

启动时自动跑 migration（[02-persistence § 6](02-persistence-schema.md)）。失败 → exit 2 + journalctl 看错误；systemd 会 Restart=on-failure 重试 5s 一次（持续失败需要人工介入）。

**Downtime**：~2-5 秒（systemd restart + migrate + ready），worker 会自动重连。

### 5.2 Worker daemon 升级 — 不中断 agent

[ADR-0018](../decisions/0018-detached-agent-via-per-execution-shim.md) 设计目标：升级 daemon 不能杀 active agent。

```bash
# 1. 拉新 binary
install -m 0755 agent-center-linux-amd64-<new-sha> ~/.local/bin/agent-center

# 2. 重启 user systemd
systemctl --user restart agent-center-worker
```

发生什么：

1. systemd SIGTERM daemon → daemon 优雅退出（关 unix socket，刷盘 events.jsonl）
2. **shim 进程不受影响**（`setsid` 脱 cgroup + `KillMode=process`）
3. daemon 重启后 reconcile：扫 `~/.agent-center-worker/exec/*/status.json` 找到所有活 shim
4. daemon 主动连 shim.sock → catchup events.jsonl 未 ACK 的事件 → 续接长连

Agent CLI 子进程**完全不知道** daemon 重启过。

### 5.3 回滚

```bash
sudo ln -sfn /opt/agent-center/releases/<previous-sha> /opt/agent-center/current
sudo systemctl restart agent-center
```

migration 不可逆 / 强 schema 改动情况下需要先恢复 SQLite snapshot（[§ 6](#-6-备份--恢复)）。建议升级前打 snapshot。

---

## § 6. 备份 / 恢复

### 6.1 备份对象

| 对象 | 路径 | 体量 | 频次 |
|---|---|---|---|
| SQLite | `/var/lib/agent-center/agent-center.db` | 数百 MB（v1 单用户）| 日级（拷贝 + WAL checkpoint） |
| BlobStore | `/var/lib/agent-center/blobs/` | GB 级（含历史 trace 归档）| 周级（增量 rsync）|
| Memory git | `/var/lib/agent-center/memory/` | KB-MB 级 | 日级（push 到远端 git remote） |
| /etc/agent-center | 配置 ~~+ feishu.env~~ (v2 删 per ADR-0031) | KB | 配置改动时（手动 git）|

### 6.2 SQLite 备份脚本

```bash
#!/bin/bash
# /usr/local/bin/agent-center-backup
set -euo pipefail
DEST=/var/backups/agent-center/$(date +%Y%m%d-%H%M%S)
mkdir -p "$DEST"

# WAL checkpoint 后再拷贝，保 consistency
sqlite3 /var/lib/agent-center/agent-center.db "PRAGMA wal_checkpoint(FULL);"
cp /var/lib/agent-center/agent-center.db "$DEST/agent-center.db"

# 同步保留 30 天
find /var/backups/agent-center -mindepth 1 -maxdepth 1 -type d -mtime +30 -exec rm -rf {} +
```

systemd timer 跑：

```ini
# /etc/systemd/system/agent-center-backup.timer
[Timer]
OnCalendar=daily
Persistent=true

[Install]
WantedBy=timers.target
```

### 6.3 恢复

```bash
sudo systemctl stop agent-center
sudo cp /var/backups/agent-center/<date>/agent-center.db /var/lib/agent-center/
sudo chown agent-center:agent-center /var/lib/agent-center/agent-center.db
sudo systemctl start agent-center
```

BlobStore 走 `rsync -a` 全量 / 增量恢复；blob 路径自描述（[01-blob-store § 路径约定](01-blob-store.md)），可独立部分恢复。

### 6.4 磁盘预估（v1 单用户）

| 项 | 体量 | 增长率 |
|---|---|---|
| SQLite（含 events 表）| 100 MB - 1 GB | events 365 天 retention（[NF9](../requirements/02-non-functional.md) + [04 § 7.8](04-configuration.md)）|
| BlobStore | 5 GB - 50 GB | 视 task 数量 × trace 体积；blob retention 90 天 |
| Memory git | < 100 MB | 文件 commit 累积，git pack 后压缩 |
| 每 task execution 占用 | ~1-10 MB（trace 归档）| GC 90 天后 BlobStore 自动清 |

推荐 VPS：50 GB 系统盘 / 200 GB 数据盘。

---

## § 7. 日志 rotation

### 7.1 systemd journal

`agent-center server` / `worker daemon` 走 stdout → journald → 自动 rotation（journald 配置）：

```
# /etc/systemd/journald.conf （示例）
SystemMaxUse=2G
SystemKeepFree=10G
MaxRetentionSec=30day
```

`journalctl -u agent-center --since="1 hour ago"` 是 v1 主排查入口。

### 7.2 Worker per-execution 目录

不走 logrotate；走 ADR-0018 的 GC：

- execution 结束 → `events.jsonl` / `agent.log` 上传 BlobStore + 留 24h 本地副本
- 24h 后整目录删除（[ADR-0018 § 9](../decisions/0018-detached-agent-via-per-execution-shim.md)）

不需要 logrotate.d 配置 — 是 agent-center daemon 自己管的。

### 7.3 BlobStore 归档

按 [04 § 7.2](04-configuration.md) `blob_store.retention_days: 90` —— agent-center server 周期 GC 扫过期 blob 删除。

---

## § 8. 监控 / 告警

### 8.1 现有 event-driven 信号（无需额外组件）

worker daemon / center server 自带的 domain event（[observability/00-overview § 2.1](../architecture/tactical/observability/00-overview.md)）已覆盖主要场景：

| 事件 | 触发条件 | 路由 |
|---|---|---|
| `worker.offline { reason, message }` | 心跳超时 / 主动 disconnect | ~~Bridge 推飞书~~ (v2 删 per ADR-0031) + Web Console |
| `task_execution.failed { reason, message }` | execution 终态 failed | 同上 |
| `supervisor.invocation_failed_alert` | supervisor 调用失败 / 超时（[ADR-0013](../decisions/0013-supervisor-invocation-concurrency.md)）| 同上 |
| `agent_adapter.unknown_event_seen` | adapter 见不认识的 JSONL type（[05 § 3.1](05-agent-adapters.md)）| 累计阈值后 escalate |

CLI 查看：`agent-center query events --since=1h --type=*.failed` / `agent-center stats`。

### 8.2 系统层信号（agent-center 之外）

| 维度 | 工具 |
|---|---|
| CPU / 内存 / 磁盘 | systemd + node_exporter（可选）/ 简单 `df` cron 告警 |
| systemd unit 状态 | `systemctl is-active agent-center` cron 告警 |
| SQLite 文件大小增长 | 每日 cron 比对 `du -sh /var/lib/agent-center` |
| Backup 是否成功 | systemd timer 状态 `systemctl status agent-center-backup.timer` |

v1 不上 Prometheus / Grafana —— 单 VPS 单用户场景**直接 SSH + journalctl + CLI** 排查即可，详见 [conventions § 2](../../rules/conventions.md)（观测层是 events 表 + CLI 而非外部 metrics stack）。

---

## § 9. 网络 / firewall

### 9.1 端口规划（[system-overview § 部署形态](../architecture/strategic/02-system-overview.md)）

| 端口 | 协议 | 方向 | 暴露给 |
|---|---|---|---|
| 7000/tcp | gRPC | 入站 | Worker 机器（白名单） |
| 7100/tcp | HTTP | 入站 (loopback only) | 本机 — Web Console 浏览器 / SSH tunnel |
| ~~443/tcp~~ | ~~HTTPS（飞书 WebSocket）~~ | ~~出站~~ | ~~open.feishu.cn~~ (v2 删 per ADR-0031) |
| 22/tcp | SSH | 入站 | 运维人员（白名单 IP） |
| 其它 | - | 关 | 所有 |

### 9.1.1 Web Console（P11）

```yaml
# server.yml
web_console:
  enabled: true                    # 默认 false
  listen_addr: 127.0.0.1:7100      # server 主动 reject 非 127.0.0.1 / localhost bind
```

Web Console 跟 API / SSE / SPA 全在同一 binary 同一 port (`7100`)：
- `/api/*` — 17 个 JSON endpoint（conversation / agent / secret / IR / fleet / trace）
- `/api/sse` — 单连接 SSE Bus（per-user 长连接）
- `/`, `/assets/*`, `/<react-router-path>` — embedded React SPA + client-routing fallback

**远程访问**通过 SSH 隧道：
```bash
# 用户笔记本
ssh -L 7100:127.0.0.1:7100 user@vps-host
# 然后浏览器开 http://127.0.0.1:7100/
```

不要 firewall 开 7100 — Web Console 设计前提就是 loopback-only (ADR-0037)。需要远程访问就用 SSH tunnel。

~~**无 HTTP webhook / 无入站飞书连接** —— Center 主动出站连飞书 WebSocket，不需要域名 / TLS cert / Reverse proxy。~~ (v2 删 per ADR-0031；v2 仅 gRPC 入站 / 无 vendor 出站)

### 9.2 firewalld 配置示例

```bash
sudo firewall-cmd --permanent --add-port=7000/tcp --zone=public
sudo firewall-cmd --permanent --add-source=<worker-ip-or-range> --zone=trusted
sudo firewall-cmd --reload
```

Worker IP 白名单需要管理时，结合 enroll 时记录 worker 来源 IP 自动加白（v1 手动可控，无需自动化）。

---

## § 10. 首次安装 bootstrap

按顺序：

### 10.1 VPS 端

```bash
# 1. 系统用户
sudo useradd --system --shell /usr/sbin/nologin --home-dir /var/lib/agent-center agent-center

# 2. 目录
sudo mkdir -p /var/lib/agent-center/{blobs,memory} /etc/agent-center /var/log/agent-center
sudo chown -R agent-center:agent-center /var/lib/agent-center /var/log/agent-center

# 3. 配置 + 凭据
sudo tee /etc/agent-center/config.yaml > /dev/null <<'YAML'
# 复制 04-configuration § 8.1 范例
# ~~+ 改 feishu.app_id 等~~ (v2 删 per ADR-0031)
YAML
# ~~v2 删 per ADR-0031 — 飞书凭据无需安装~~
# sudo install -m 0600 -o agent-center -g agent-center \
#   /path/to/feishu.env /etc/agent-center/feishu.env

# 4. 二进制 + systemd
sudo install -m 0755 agent-center-linux-amd64-<sha> /usr/local/bin/agent-center
sudo install -m 0644 contrib/agent-center.service /etc/systemd/system/agent-center.service
sudo systemctl daemon-reload
sudo systemctl enable --now agent-center

# 5. 验证
sudo journalctl -u agent-center -f
agent-center version  # 用 admin socket 查
```

### 10.2 Worker 端（每台开发机一次）

```bash
# 1. VPS 端签发 token
ssh vps "agent-center worker enroll --name=laptop-hayang"
# → 输出 worker-id + bootstrap-token

# 2. Worker 机器
mkdir -p ~/.agent-center-worker
echo "<token>" > ~/.agent-center-worker/bootstrap-token
chmod 0600 ~/.agent-center-worker/bootstrap-token

cat > ~/.agent-center-worker/config.yaml <<'YAML'
# 复制 04-configuration § 8.2 范例 + 改 worker_config.id / center_endpoint / scan_paths
YAML

install -m 0755 agent-center-darwin-arm64-<sha> ~/.local/bin/agent-center

# 3. user systemd
install -m 0644 contrib/agent-center-worker.service \
  ~/.config/systemd/user/agent-center-worker.service
systemctl --user daemon-reload
systemctl --user enable --now agent-center-worker

# 4. 验证
journalctl --user -u agent-center-worker -f
agent-center worker status <worker-id>  # VPS 端查应该是 online
```

### 10.3 ~~飞书集成（一次性，可推迟）~~ (v2 删 per ADR-0031)

~~`/etc/agent-center/feishu.env`：~~

```
# ~~v2 删 per ADR-0031 — 飞书集成已撤回~~
# AGENT_CENTER_BRIDGE_FEISHU_APP_ID=cli_abc123
# AGENT_CENTER_BRIDGE_FEISHU_APP_SECRET=<secret>
```

~~或走 `bridge.feishu.app_secret_file` 指向 `/etc/secrets/agent-center/feishu`（[04 § 3](04-configuration.md)）。~~

~~Worker enroll + Feishu setup 都做完 → 第一个 task 可以派单。~~ (v2: Worker enroll 完即可派单；无 Feishu 步骤)

---

## § 11. 与设计层对位

| 设计层来源 | 落到 06 哪节 |
|---|---|
| [domain-vision § B2](../architecture/strategic/00-domain-vision.md) v1 单 VPS | § 1 拓扑 |
| [system-overview § 部署形态](../architecture/strategic/02-system-overview.md) | § 1 / § 9 |
| [ADR-0018](../decisions/0018-detached-agent-via-per-execution-shim.md) shim detached | § 4.2 / § 5.2（`KillMode=process` 强制）|
| [ADR-0012](../decisions/0012-memory-file-based.md) memory git | § 3.1 / § 6 |
| [01-blob-store](01-blob-store.md) 路径约定 | § 3.1 / § 6 / § 7.3 |
| [02-persistence § 6](02-persistence-schema.md) migration | § 5.1 |
| [04-configuration § 1 / § 7.1](04-configuration.md) 路径 | § 3 / § 4 |
| [05-agent-adapters § 4](05-agent-adapters.md) 进程生命周期 | § 4.2 `KillMode=process` |
| [conventions § 10](../../rules/conventions.md) 单一二进制 | § 2 |
| [conventions § 13](../../rules/conventions.md) 安全 / 凭据 | § 2.3 / § 4 / § 10 |
| [NF2](../requirements/02-non-functional.md) Center 出站不入站 | § 9 |
| [NF5](../requirements/02-non-functional.md) 本机 admin socket | § 3.1 / § 4 |

---

> **本文档 scope**：v1 单 VPS 部署完整流程 — 拓扑 / 二进制 / 文件布局 / systemd unit / 升级 / 备份 / 日志 / 监控 / 网络 / bootstrap。多 VPS / HA / 容器化 推 [roadmap](../roadmap.md)。
