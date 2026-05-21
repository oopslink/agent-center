# v1 Release Checklist

> Phase 7 完成 = v1 release candidate · 本文档是 release manager（即用户）签发 `v1.0.0` 之前必须 100% 核对的清单 · 详见 [plan-7 § 8](../phase-7-bridge-inbound-deploy.md#-8-跟-v1-release-节奏对齐)

## § 0. Release manager 签发流程

1. 通读本文档 § 1 - § 6
2. 任一项 ❌ → release 阻塞，回 plan 修复 → 重跑 § 5 验证
3. § 3 真实部署演练**必须**在干净 VPS（推荐 Ubuntu 22.04 / 24.04 LTS）跑一次完整链路
4. 全 ✅ 后打 tag `v1.0.0`：`git tag -a v1.0.0 -m "agent-center v1.0.0 release"`

## § 1. Phase 完成度汇总

| Phase | 主题 | 覆盖率 (overall, 5× / 10×) | 测试用例数 (unit / int / e2e) | 已知 issue | 完成日期 / commit SHA |
|---|---|---|---|---|---|
| **1** | Shared Kernel + Events 总线 | ≥ 90.0% × 5 stable | ~85 / 3 / 9 | 无 | 2026-05-12 / `(早期 commit chain)` |
| **2** | TaskRuntime Core | ≥ 90.5% × 5 | ~120 / 5 / 7 | 无 | 2026-05-15 |
| **3** | Discussion Core | ≥ 90.5% × 5 | ~110 / 4 / 6 | 无 | 2026-05-17 |
| **4** | Observability 投影 + 查询面 | ≥ 90.0% × 5 (实测 90.0-90.4%) | ~140 / 6 / 10 | 无 | 2026-05-19 / `28e65cb` |
| **5** | Bridge ACL Outbound（飞书） | ≥ 90.5% × 5 (90.5-90.6%) | ~170 / 7 / 8 | 无 | 2026-05-20 / `b35681c` |
| **6** | Cognition Supervisor | ≥ 90.5% × 5 (90.5-90.6%) | ~190 / 6 / 6 | 无 | 2026-05-22 / `8ca11b7` |
| **7** | Bridge Inbound + 部署收尾 | **≥ 90.5% × 10 (stable 90.5%)** | ~120 / 8 / 7 | 无 P0/P1（详见 § 5） | 2026-05-22 / `(本 phase chain)` |

**汇总**：累计 ~935 unit + 39 integration + 53 e2e = **~1027 测试用例**；7 phase 全部稳定 ≥ 90.0%，5+ phase 在 ≥ 90.5%。

## § 2. 关键 e2e 路径状态

| 路径 | 验证位置 | 状态 |
|---|---|---|
| Worker enroll → dispatch → 完成 / 失败 / 取消 | `tests/integration/phase2_test.go` + `tests/e2e/phase2_test.go` | ✅ |
| Worker proposal discovery → accept | `tests/integration/phase2_test.go:TestProposalAcceptanceService_Accept` | ✅ |
| 飞书 @bot 提需求 → supervisor wake → task → 完成 → 回流 | A1-A2 inbound: `tests/integration/phase7_test.go:TestPhase7_I1_DMNewUser`；A3-A12 supervisor + dispatch + completion: `tests/integration/phase6_test.go:TestPhase6_FullPipeline` (fakeProcessRunner); end-to-end via 真飞书: § 3 部署演练 | ✅（拆分覆盖）+ ⏸（真飞书演练在 § 3） |
| InputRequest 完整往返（agent → 卡片 → /answer → 续） | `tests/integration/phase7_test.go:TestPhase7_I4_SlashAnswer` + `TestPhase7_I5_CardCallback` | ✅ |
| `/track <task_id>` 后续进度自动推飞书 thread | `tests/integration/phase7_test.go:TestPhase7_I3_SlashTrack` + Phase 5 outbound dispatcher tests | ✅ |
| worker daemon restart 不杀 active shim 实测 | Phase 2 `internal/shim/fencing_test.go` + ADR-0018 设计 + `agent-center bootstrap check-systemd` 三层防线 | ✅（设计 + 运行时校验）/ ⏸（真 daemon restart 演练在 § 3）|
| center restart 不丢请求实测 | Phase 2 reconcile 协议 `tests/integration/phase2_test.go:TestReconcileService_*` | ✅（设计 + 单测）/ ⏸（真 restart 演练在 § 3） |
| worker.offline 60s → escalator → supervisor wake → 飞书告警 | Phase 4 escalator + Phase 5 outbound dispatcher + Phase 6 supervisor wake whitelist 全链已分段覆盖 | ✅（拆分覆盖） |

## § 3. 部署演练记录

> 本节由 release manager 填实

| 字段 | 值 |
|---|---|
| 演练日期 | `___________________` |
| 演练者 | `___________________` |
| VPS 规格 | OS: `_________`（推荐 Ubuntu 22.04 / 24.04 LTS）/ vCPU: `__` / RAM: `__GB` / 磁盘: `__GB` / VPS provider: `_________` |
| Worker 机器规格 | OS: `_________`（macOS / Linux）/ Arch: `_________` |
| 飞书 App ID（脱敏后前缀）| `_____________` |

### § 3.1 VPS 安装步骤记录

```bash
# 实际跑通的命令清单（演练者复制粘贴 + 备注）
1. scp agent-center-linux-amd64-<sha> vps:/tmp/
2. ssh vps "sudo bash /tmp/contrib/install.sh --binary=/tmp/agent-center-linux-amd64-<sha>"
   → 输出：[install] Create system user agent-center
            [install] Create directories
            [install] Install binary
            [install] Install systemd unit files
            [install] systemctl daemon-reload
            [install] Enable services
            [install] Done.
3. ssh vps "sudo systemctl start agent-center"
4. ssh vps "sudo journalctl -u agent-center -f"
   → 验证日志含 "agent-center server: db=... feishu=... Phase 7 inbound + escalator running"

预期结果：✅ / ❌（演练者填）
遇到问题 + 处置：
- ___________________
- ___________________
```

### § 3.2 Worker 端 enroll

```bash
1. ssh vps "agent-center worker enroll --name=laptop-<演练者>"
   → 记录输出 bootstrap-token: ___________________
2. 在 worker 机器：
   bash contrib/install-worker.sh --binary=agent-center-darwin-arm64-<sha> --bootstrap-token=<token>
   → 验证输出 ".../agent-center-worker.service has KillMode=process"（bootstrap check-systemd 守护）
3. systemctl --user start agent-center-worker
4. journalctl --user -u agent-center-worker -f
   → 验证 worker.online 事件已推到 center
5. ssh vps "agent-center worker status laptop-<演练者>"
   → 验证 status=online

预期结果：✅ / ❌
遇到问题 + 处置：
- ___________________
```

### § 3.3 飞书 setup

```bash
1. ssh vps "agent-center bridge feishu setup --app-id=<id> --app-secret-file=/etc/secrets/feishu"
   → 验证 stdout "feishu bridge enabled, app_id=<id> (connected)"
   → 验证 events 表 bridge.feishu.connection_state_changed { state=connected }
2. ssh vps "sudo systemctl restart agent-center"
   → 验证日志 "feishu=true"

预期结果：✅ / ❌
遇到问题 + 处置：
- ___________________
```

### § 3.4 单 task 完整路径（最终签发判据）

```bash
1. 飞书内 @bot "帮我跑一下 echo hello"
   → 演练者观察：飞书内 5 秒内出现 supervisor "收到，正在分析..." 卡片
2. center 端：ssh vps "agent-center query events --since=5m --type=task.created"
   → 验证有一条 task.created 事件
3. center 端：ssh vps "agent-center ps"
   → 验证 task 在 dispatching 或 working
4. worker 端：journalctl --user -u agent-center-worker --since="5m ago"
   → 验证 shim spawn / agent CLI 实际运行
5. 飞书内观察：5-30 秒内 thread 出现 agent_finding "echo hello → hello"
6. 飞书内观察：thread 最后出现 "Task #X done" system 卡片
7. center 端：ssh vps "agent-center inspect task <id>"
   → 验证 status=done

预期结果：✅ / ❌
真实运行时间：________ 秒（从 @bot 到 done 卡片）
遇到问题 + 处置：
- ___________________
```

### § 3.5 升级演练

```bash
1. center 升级：
   sudo install -m 0755 -o agent-center -g agent-center \
     agent-center-linux-amd64-<new-sha> \
     /opt/agent-center/releases/<new-sha>/agent-center
   sudo ln -sfn /opt/agent-center/releases/<new-sha> /opt/agent-center/current
   sudo systemctl restart agent-center
   → 验证 downtime < 5 秒；worker 自动重连
2. worker daemon 升级（验证 ADR-0018 不杀 shim）：
   - 先 @bot 提一个长跑 task（如 "sleep 60 && echo done"）
   - 等 worker shim 在跑
   - install -m 0755 agent-center-darwin-arm64-<new-sha> ~/.local/bin/agent-center
   - systemctl --user restart agent-center-worker
   - 验证 shim 进程依旧存活（PID + start_time）
   - 验证 task 最终 done（agent 完成发回）
   
预期结果：✅ / ❌
shim PID 重启前 / 后：________ / ________（应一致）
遇到问题 + 处置：
- ___________________
```

### § 3.6 backup 演练

```bash
1. ssh vps "sudo systemctl start agent-center-backup.timer"
2. 等到次日凌晨 03:00（或手动 trigger: sudo systemctl start agent-center-backup.service）
3. ssh vps "ls /var/backups/agent-center/"
   → 验证有一个 YYYYMMDD-HHMMSS 子目录
4. ssh vps "sqlite3 /var/backups/agent-center/<latest>/agent-center.db .schema | head"
   → 验证 schema 正确（可读）
5. ssh vps "agent-center query events --since=1d --type=admin.backup_ok"
   → 验证有 admin.backup_ok 事件

预期结果：✅ / ❌
backup 文件大小：________ MB
遇到问题 + 处置：
- ___________________
```

## § 4. 部署交付物清单

| 文件 | 状态 |
|---|---|
| `contrib/agent-center.service` | ✅ 已交付 |
| `contrib/agent-center-worker.service`（含 `KillMode=process` 守护） | ✅ 已交付 |
| `contrib/agent-center-backup.service` | ✅ 已交付 |
| `contrib/agent-center-backup.timer` | ✅ 已交付 |
| `contrib/install.sh`（VPS 一键安装） | ✅ 已交付 |
| `contrib/install-worker.sh`（worker 端一键安装 + KillMode 校验） | ✅ 已交付 |
| `agent-center admin backup` CLI | ✅ 已交付 |
| `agent-center bootstrap check-systemd` CLI（运行时 ADR-0018 守护） | ✅ 已交付 |
| 部署文档：[implementation/06-deployment.md](../../design/implementation/06-deployment.md) | ✅ |
| CLI subcommand 文档：[implementation/03-cli-subcommands.md](../../design/implementation/03-cli-subcommands.md) | ✅ |

## § 5. 待 release 阻塞 issue（必须清零）

| ID | 描述 | 严重度 | 状态 |
|---|---|---|---|
| — | （空。截至本 checklist 起草时无 P0 / P1 阻塞 issue。）| — | — |

## § 6. v2 已 defer 清单（不阻塞 release，参考 roadmap）

| 项 | 归属 | 说明 |
|---|---|---|
| **多 vendor**（DingTalk / Slack / WebBridge） | [roadmap § 多 vendor](../../design/roadmap.md) | Phase 7 已固化 6 个 Domain Service 接口签名；v2 仅需 1:1 复制 + 替换 vendor SDK leaf |
| **跨 vendor fallback**（飞书失败 → DingTalk 补送） | roadmap | v1 仅 FeishuBridge，无 fallback 需求 |
| **HA / 多 VPS** | roadmap | v1 单 VPS 单用户 |
| **Web Console** | roadmap | tests/e2e/fakeserver/feishu 可作为 WebBridge 第一版参考 |
| **容器化 agent CLI** | roadmap | ADR-0018 shim 模型已支持；v1 直接走宿主进程 |
| **Bridge 升级热重启** | roadmap | v1 走 systemd restart（< 5s downtime）|
| **Vendor 限流自适应** | roadmap | v1 单用户 QPS < 1 |
| **卡片模板运行时配置** | roadmap | v1 硬编码在 `internal/bridge/feishu/renderer/renderer.go` |
| **Bridge 跨 dedupe**（去重多 vendor 同一 message） | roadmap | v1 单 vendor 无需 |
| **`/dispatch` slash 命令** | roadmap | v1 parser 已识别 + reject；v2 加 router handler 即可 |
| **`/track-issue <issue_id>` slash** | roadmap | v1 仅 `/track <task_id>`；v2 对称 |
| **AutoRegisterFromVendor** 接口 | `internal/conversation/identity/service.go:AutoRegisterFromVendor` | 接口已留，实现推 v2 多用户场景 |
| **InputRequestPingHours 实际派 ticker** | Phase 6 已设计，未实装 | v1 不阻塞（ping 失败用户重发 @bot 即可）|
| **install.sh / *.service 在 Ubuntu 22+24 双版本 CI** | plan-7 § 6 R2 | 真 CI 环境推 v1.1（v1 通过 § 3 手工演练验）|

**无"待定" / "v2 再说"残留**：剩下不做的全在 [roadmap](../../design/roadmap.md)。

## § 7. 签发前最终核对

| 项 | release manager 勾选 |
|---|---|
| § 1 7 个 phase 全 ✅ | ☐ |
| § 2 关键 e2e 路径全 ✅ | ☐ |
| § 3 真实部署演练 § 3.1-§ 3.6 全 ✅ | ☐ |
| § 4 交付物全 ✅ | ☐ |
| § 5 阻塞 issue 清零 | ☐ |
| § 6 v2 defer 清单已审阅 + 接受 | ☐ |
| `git tag -a v1.0.0 -m "agent-center v1.0.0"` | ☐ |

> **本文档由 Phase 7 实施工程师起草；release manager（即用户）负责跑 § 3 并填实 + 签发。**
