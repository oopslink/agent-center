# Phase 12: Cleanup + Release

> 收尾阶段 · 依赖 Phase 8-11 全部完成 · v2.0 GA
> 纪律：所有 phase test report 闭环 / 端到端测试通过 / docs 同步 / release checklist

覆盖：v1 残留代码清理 / e2e suite / docs polish / release checklist。

## § 0. 目标

Phase 8-11 完成 = v2 所有功能 ready；本 phase 把 release 之前的「闭环动作」做完：

- **删 v1 残留代码 / docs**：Bridge BC code 已 P10 删；本 phase 扫剩余 vendor stained 代码（grep 验证）
- **端到端测试 suite**：跨 phase 大场景 e2e（不止单 phase 内的）
- **docs polish**：v2 ADR 17 个全状态对齐；roadmap 同步
- **Release checklist**：版本号 / changelog / deployment doc / migration guide

## § 1. DDD 工件清单（无新增）

本 phase 不引入新 AR / Service 等；纯收尾。

## § 2. 上游依赖

来自 Phase 8-11 全部产出。

## § 3. 工作项分解

### 3.1 跨 phase e2e suite

- **工件**：`tests/e2e/v2/*.go`
- **场景**：跨 phase 大流程，覆盖典型用户旅程
  - 用户冷启动：center start → identity add → worker join → agent create → secret create → channel create → invite agent → send message → agent reply → derive issue → conclude issue → spawn task → task done → archive
  - dispatch 失败：派 task → adapter NACK (e.g. capability_missing) → supervisor open Issue → user 回 Web Console / CLI
  - InputRequest 端到端：agent request-input → supervisor escalate → user 回 → task unblock
  - Secret rotate 期间 active execution：rotate → 新 spawn 用新 secret，旧 execution 仍持原值
  - **DM e2e**（per P11 Q3）: user 开 DM 跟 supervisor 1:1 私聊 → reply → SSE 实时
  - **Web/CLI 双向 e2e**: Web Console 开 conversation + CLI `conversation send` 注入 message → Web SSE 实时收到（验证两端等价）
  - **SSE 恢复 e2e**（per P11 Q5）: 客户端断连 → 服务端持续 push event → reconnect with Last-Event-ID → 漏掉的 events 续推
  - **Frontend bundle smoke**: `go:embed` 后 `agent-center server start` → 浏览器 fetch `/` 渲染成功（SPA 加载 + 首页可见）
- **DoD**：每场景 e2e 自动化 + 可重复跑

### 3.2 v1 vendor code purge 验证（扩展版）

- grep 验证 `飞书` / `feishu` / `lark` / `bridge` 不在 `internal/` + `assets/`（白名单：v3+ roadmap 文档 / ADR-0031 / 历史 ADR 0007/0016/0019 banner）
- **DB schema column drop migration 测**：
  - v1 schema 含 `messages.vendor_msg_ref` / `conversations.primary_channel_hint` / `primary_channel_thread_key` 等 column → 写 migration drop 这些 columns
  - 测两条路径：a) v1 → v2 migration upgrade DB; b) fresh v2 DB（无这些 columns 从一开始）—— 两条 path 都 pass schema check
- **CI lint rule**：repo root 加 `scripts/lint/no-vendor-refs.sh`，CI invoke：
  - regex `飞书|feishu|Feishu|lark|Lark` 命中 `internal/` 或 `assets/`（白名单除外）→ fail
  - 同样禁 `internal/bridge` / `internal/bridgeapp` 目录存在 → fail
- **assets/skills 扫**: 检查 `assets/skills/` 下有无 vendor-specific skill (e.g. feishu_sender / lark_*）→ 全删
- 配置文件 `bridge.feishu.*` 等彻底删（含 systemd unit / docker compose / .env.example）
- 确认 ADR 状态对齐（superseded / draft / accepted）
- **DoD**：grep clean；migration up + down pass；CI lint rule 在；测试 compile pass

### 3.3 ADR drafts → Accepted 状态转换（一次性 script 自动化）

- 17 个 v2 ADR 当前在 `decisions/drafts/`；v2 release 时统一 promote 到 `decisions/`（移动文件 + 更新 status + 更新 cross-refs）
- 0023-0030 + 0032-0038 + 0039：drafts → decisions/
- 0026 + 0031 + 0039 已是 Accepted；其他 promote 时改 Status: Accepted
- **工件**：`scripts/v2-promote-adrs.sh`（一次性脚本）：
  ```
  1. for f in decisions/drafts/{0023,0024,0025,0026,0027,0028,0029,0030,0032,0033,0034,0035,0036,0037,0038,0039}-*.md:
       git mv $f decisions/
  2. for adr_id in 0023..0030 0032..0039:
       find docs/ -name '*.md' | xargs sed -i.bak \
         -e "s|drafts/${adr_id}-|${adr_id}-|g"
  3. for f in decisions/{0023..0030,0032..0038}-*.md:
       sed -i.bak 's|^| Status .* Draft.*|Status: Accepted|' $f
  4. rm **/*.bak
  5. verify:
       - grep -r 'drafts/0023' docs/  → 0 hits
       - grep -r 'drafts/002[3-9]' docs/  → 0 hits
       - grep -r 'drafts/003[2-9]' docs/  → 0 hits
       - grep '^| Status .* Draft' decisions/{0023..0030,0032..0038}-*.md  → 0 hits
  ```
- 跑前 dry-run 模式（`--dry-run` flag），跑后 git diff review
- 0029 amends-but-not-supersedes 0024 关系特殊：promote 时验证 0024 metadata 保留 amended-by 0029 行
- **DoD**：脚本 idempotent；所有 v2 ADR 在 `decisions/` 主目录；状态 Accepted；cross-ref 路径 updated；脚本 review by user

### 3.4 决策 README 同步

- `decisions/README.md` 全表 status update
- v1 ADR superseded 行已 P10 处理；本 phase 验证

### 3.5 roadmap.md polish

- v2 已实施的（如 SecretManagement BC8）从 roadmap 移除（如果 v1 roadmap 有列）
- v3+ 项整理：AgentImage + Memory git / Cloud Computer 节点 / Bridge 重设计 / etc.

### 3.6 v2-kickoff archive 验证

- v2-kickoff-2026-05-22.md (已 P10 Step D 加 archive header) 验证全完整

### 3.7 Documentation final pass（三组拆解）

**Group A — Wave 2 加 banner 的 v1-era docs**（P10/P11 真正 rewrite 后清 banner + 删 strikethrough）：

| 文件 | 何时清理 |
|---|---|
| tactical/conversation/00 + 01 + 02-identity | P10 完成后；本 phase 验证 |
| tactical/task-runtime/00 + 01 + 02 + 03 | P10 完成后；本 phase 验证 |
| tactical/discussion/00 | P10 完成后；本 phase 验证 |
| tactical/observability/00 | P10 完成后；本 phase 验证 |
| tactical/cognition/00 | P10 完成后；本 phase 验证 |
| tactical/workforce/00 + 03 | P8 / P9 完成后；本 phase 验证 |
| tactical/agent-harness/02-skill-cli-tooling | P11 完成后；本 phase 验证 |
| tactical/presentation/01-web-console | P11 完成后；本 phase 验证 |
| implementation/02 + 03 + 04 + 06 | P10/P11 后；本 phase 验证 |
| strategic/00 + 01 + 02 | P12 内 polish |
| ADR-0007 / 0016 / 0019 | 改 metadata 形式保留（"v1-era ADR，超越部分见 ADR-0039"），不再「待 P10/P11 rewrite」语 |

**Group B — Wave 1 已清的，touch-up only**：

| 文件 | 校验内容 |
|---|---|
| strategic/03-bounded-contexts | verify 全 v2 形态；BC7 移除；Context Map mermaid 正确 |
| requirements/00+01+02+04 | 跟 v2 最终形态对齐 |
| rules/conventions | § 9.y / § 13 跟 v2 形态对齐 |
| ddd-blueprint | v2 BC 数对齐；删 Bridge 段 |
| roadmap | v3+ 项整理 |
| design/decisions/README | v2 ADR 表完整 + status accurate |

**Group C — 新增 v2 文档**（P11 实施时 / 本 phase 验证）：

| 文件 | 说明 |
|---|---|
| tactical/presentation/02-web-console-architecture.md | React + Vite + Zustand + Router 选型 + 组件层 + state 流（per P11 § 3.10）|
| tactical/secret-management/00-overview + 01-user-secret | P8 已建；本 phase 验证一致性 |
| tactical/workforce/04-agent-instance | P8 已建；本 phase 验证 |

**DoD**:
- Group A: 所有 banner / strikethrough / `(v2 删)` 注释 全清；段落实际删除（不留死代码）
- Group B: 字面校验 + 无 stale refs
- Group C: 文件存在 + 内容跟实装一致

### 3.8 release checklist

参 v1 phase-7 (已删) 风格：

- [ ] 版本号 bump (v1.x → v2.0)
- [ ] CHANGELOG.md v2 entry 完整
- [ ] DEPLOYMENT.md migration guide (v1 → v2 升级路径；提醒「不考虑 back-compat」 = fresh DB / 重新 enroll workers / 重新 create agents / 等)
- [ ] systemd / docker compose 配置文件 v2 适配
- [ ] master_key 备份指引 (per ADR-0026)
- [ ] CI 通过 + 整体 coverage 报告
- [ ] e2e suite 全过

### 3.9 v2 release tag

- git tag `v2.0.0` + GitHub Release notes
- 标 Status of all v2 ADRs → Accepted

## § 4. Definition of Done

- [ ] § 3.1 e2e suite 8 跨 phase 场景全过（含 DM / Web↔CLI 双向 / SSE 恢复 / bundle smoke）
- [ ] § 3.2 grep clean for v1 vendor stained code + DB column drop migration + CI lint rule
- [ ] § 3.3 17 个 ADR 全 Accepted + decisions/ 主目录（脚本 idempotent + user review）
- [ ] § 3.4-3.7 docs polish 全完成（Group A banner+strikethrough 全清；Group B touch-up；Group C 文件存在）
- [ ] § 3.8 release checklist 100%
- [ ] § 3.9 git tag + release notes 发布
- [ ] phase-12-test-report.md 归档（含跨 phase coverage 汇总）

## § 5. 测试计划

### 5.1 单测

无新增；P8-P11 单测继承。

### 5.2 集成

无新增。

### 5.3 e2e（核心）

每条场景至少 1 个自动化 e2e（per 3.1）：

| 场景 | 入口 | 关键断言 |
|---|---|---|
| 冷启动用户旅程 | CLI sequence | 所有 entity / event / state 全链路 |
| Dispatch NACK → Issue surface | CLI + Web Console | supervisor 开 Issue；不调 agent create |
| InputRequest 端到端 | CLI 双终端 / Web Console | 卡片显示 + 回复 → task unblock |
| Secret rotate 中 execution | CLI 跨进程 | 新 spawn 用新值 |
| Multi-agent 并行 (同 AgentInstance 并跑 N tasks) | dispatch + execution | concurrent execution count 正确 |
| Carry-over derived Issue | Web Console / CLI | child conv 渲染分段 + reference 反查 |

### 5.4 性能 / 烟雾测试（不强制 v2 GA；记 v3 roadmap）

v2 GA 不卡性能 baseline（"能跑"优先）。性能优化推到 v3：

- center 启动时间 (target < 5s)
- worker enroll (target < 1s)
- channel send → SSE 投到 browser (target < 500ms)
- 单 SQLite DB 万级 events 性能 baseline

详 [roadmap.md v3 性能优化](../design/roadmap.md)。本 phase 可跑一次 ad-hoc 烟雾测试存档 baseline 数字，但**不阻塞 GA**。

## § 6. 风险

| 风险 | 缓解 |
|---|---|
| Cross-phase e2e 发现 phase 内未覆盖的 corner case | 修 + 加 unit test；如果是 phase N 漏，回头补 phase N test report |
| v1 数据库不兼容 v2 schema | Migration guide 明示 v2 fresh DB；用户须 backup v1 然后 destroy |
| Master_key 用户丢失 | DEPLOYMENT.md / 启动报错信息明确指引；同步 ADR-0026 § 4 |
| ADR draft → Accepted 时 cross-ref path 更新（drafts/ → decisions/）| 脚本自动化 update；grep + sed 批 |
| v2.0 release 前用户期望「演示场景」 | Demo scripts 准备；e2e 场景 (3.1) 可作 demo 蓝本 |

## § 7. 下游解锁

本 phase 完成 = **v2.0 GA**。

下游 v2.x 增量：

- agent / worker / secret 管理 UI（W1 推迟项）
- gemini / kimi / 其他 adapter
- 其他 bonus features

下游 v3+：

- AgentImage 模型 + Memory git 化
- Cloud Computer 节点支持
- Bridge / vendor 重新接入
- Multi-user / 权限模型
- Supervisor 增强 (Reminder / Cron / Emoji)
