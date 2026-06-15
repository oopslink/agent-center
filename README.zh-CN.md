<h1 align="center">agent-center</h1>

<p align="center">
  <a href="./README.md">English</a>
  &nbsp;·&nbsp;
  <strong>简体中文</strong>
  &nbsp;·&nbsp;
  <a href="./docs/index.md">设计文档</a>
  &nbsp;·&nbsp;
  <a href="./docs/design/roadmap.md">Roadmap</a>
  &nbsp;·&nbsp;
  <a href="./docs/deployment/v2.4-first-mile.md">部署指南</a>
  &nbsp;·&nbsp;
  <a href="./CHANGELOG.md">CHANGELOG</a>
</p>

<br/>

<h3 align="center">个人 AI agent 调度中心。</h3>
<p align="center">一台机器开 server，多台机器跑 worker；agent 在哪都行，对话和决策都收到 conversation 线程里。</p>

<br/>

> [!TIP]
> **Conversation 不是日志，是产品骨架。** Task / Issue / 决策 / 进度全挂在 Conversation 上 —— 这是 agent-center 跟"agent 是脚本"思路最大的区别。每一次 dispatch、每一个 InputRequest、每一份 artifact，都能在原线程里追溯。

> [!IMPORTANT]
> **v2.10.0 已发布（2026-06-15）** —— **三栏式桌面 UI/UX 重构。** Web Console 重构为四区骨架 —— 顶层模块**图标栏**(Workspace / Conversations / Members / System)→ 每模块**二级列表**(col②)→**内容**区(col③)→**按需上下文**面板(col④) —— 落在现有 IA 上(移除 Overview 页);col② 由每模块的**二级导航注册表**驱动 —— 模块加自己的 nav + 一行注册即接入(不动 app layout)。各模块全部在此骨架上重建:Conversations(col④ 参与者 + 共享文件)、Projects 与项目详情(col② 项目子导航)、Tasks / Issues(col④ 只读元信息面板)、**全局跨项目 Plan** 列表 + 详情(Chat / DAG / Task 列表 三 tab)、**Project Work Board**、Members(col② Humans / Agents + Agent col④ 上下文面板)、System。外加:**task & issue 附件**(列表 / 上传 / 下载,项目成员鉴权 fail-closed)、**消息引用 linkify**(`task-<id>` 与 `T<number>` org-ref 都渲染成任务链接,收发皆可)、**入站消息附件**透出给 agent、system / 调度消息作者正确显示 **System**。Message Threads、Plan 编排、Web Console、first-mile 部署从 v2.4–v2.9.2 延续。详见 [CHANGELOG](./CHANGELOG.md)。
>
> **v2.9.1 已发布（2026-06-14）** —— **消息 Thread**：从任一消息派生 thread、在弹出侧边栏里回复（Slack 式单层），Participants 侧栏有 thread 列表；**在 thread 内 @提及项目 agent 会唤醒它、且其回复落在该 thread 内**,"有无新活动"标记提示未读 —— 适用所有会话类型。外加一波任务模型/看板收尾:**claimable + 每项目内置指派池**(指派≠领取;三段 Work Board —— Backlog · 内置池 · 结构化 Plan)、更简的**任务状态机**(`blocked` 改为可恢复注解、删除 `verified`)、看板可见性修复(终态任务 & 归档项目条目默认不进列表;大 plan 任务列表带搜索/行内改派)、**频道归档**、`list_tasks` MCP 工具、任务恢复工具(`unblock_task`/`rerun_failed_node` + 自动重派)、以及全量设计 token 迁移。详见 [CHANGELOG](./CHANGELOG.md)。
>
> **v2.9 已发布（2026-06-12）** —— **Plan 编排（Plan Orchestration）**：把工作组织成任务的 DAG（一个 *Plan*）、`start` 它，center 随上游任务完成自动把每个就绪节点派发给对应 agent —— plan 一路跑到完成、无需手动推进。PM 型 agent 还能通过自己的 MCP 工具编程式建 plan 并跑起来。Plan 与 Project 有明确生命周期（draft / running / done，外加**不可逆归档**=只读；有 running task 的 plan 不能归档）。**org 路由显式化** —— 每个 org-scoped API 都挂在 `/api/orgs/{slug}/...` 下、消除隐含 org 状态。**plan 会话现在也纳入 @提及唤醒** —— 在 plan 会话里 @提及一个项目 agent 会唤醒它（issue / task 会话自 v2.7.1 起就支持；plan 会话与非-participant 的 project-member 广度是 v2.9 新增）。Web Console、拉取模型派发、first-mile 部署从 v2.4–v2.8.1 延续。详见 [CHANGELOG](./CHANGELOG.md)。

<br/>

## 安装

`agent-center` 现在是**装一次跑全套**。解压官方 tarball 后，两条命令分别装 center 和 worker：

```bash
# 装 center（本机）
cd agent-center-v2.4.0-<os>-<arch>/
./install center
# 输出末尾给你 Web Console URL，浏览器打开继续

# 装 worker（同机或任意机器）
# 在 Web Console 点 "+ Add Worker"，填一个 friendly name → 复制命令 → 粘贴到 worker 机器执行
./install worker --bootstrap=... --worker-id=... --worker-name=... --token=...
```

支持 macOS（Mac 是本期 acceptance 平台）+ Linux（systemd 自动注册，代码就位但本期未验）。**升级也是同一条命令** —— 解压新版本 tarball → `./install center` → 原子切 symlink + 自动回滚兜底。

| 命令 | 干嘛 |
|---|---|
| `agent-center install center` | 装 / 升级 center（idempotent） |
| `agent-center install worker` | 装 / 升级 worker daemon |
| `agent-center server` | 直接前台启动 server（开发用） |
| `agent-center help` | 全命令树（subject-verb 分组） |
| `agent-center task create <proj> <title>` | 建任务 |
| `agent-center issue open <proj> <title>` | 开 Issue 走讨论 |
| `agent-center ps [--watch]` | 实时 fleet 视图（worker × execution） |
| `agent-center inspect <kind> <id>` | 看单个实体（task / issue / worker / ...） |

详细 CLI 全签名见 [CLI 子命令](./docs/design/implementation/03-cli-subcommands.md)；部署完整 journey 见 [v2.4 first-mile 指南](./docs/deployment/v2.4-first-mile.md)。

<br/>

## 它解决什么

| 痛点 | agent-center 怎么处理 |
|---|---|
| 多 agent / 多机器并行做事，状态散在 N 个终端里 | 单 server 收口；`/fleet` 实时看所有 worker × execution × IR |
| Agent 跑一半要你拍板（"要不要 commit？"） | InputRequest 一等公民 —— WC 卡片回 yes/no/choice，agent 续跑 |
| Agent 干了啥、为啥这么干、谁让它干的，事后查不到 | 每个 task / issue 都挂一个 Conversation 线程，dispatch / decision / progress / artifact 全在里面 |
| Skill / MCP 配置散在每个 agent 项目里 | AgentInstance 一等 AR：instructions + MCP + skill mount 跟 agent 绑，集中可见 |
| 凭证管理 | UserSecret BC，AES-256，plaintext-never-echo，agent 引用 `secret:<name>` |
| 多机部署难 | v2.3 multi-host TCP+TLS 落地（SSH-style fingerprint pinning）+ v2.4 一条命令 first-mile |

<br/>

## 核心抽象

每一个都是用户要学的名词，对应 DDD 的 AR / VO / 事件 / 服务：

| 概念 | 一句话理解 |
|---|---|
| **Task** | 你或 Supervisor 派的一次工作；可重试（每次 retry 是新 TaskExecution，task identity 不变） |
| **Issue** | 一个待讨论的题目（"该用 X 还是 Y？"），结论可以是 0/1/N 个 Task |
| **Conversation** | 一条消息线索，挂在 Task / Issue / Channel / DM 上；产品骨架 |
| **Worker** | 跑 agent 的机器（本机或远程）；一台机器可装多个 worker（v2.4） |
| **AgentInstance** | 命名 + 持久化的 agent 身份（"我的 mbp 上的 coder"）；含 instructions / MCP / skill |
| **Supervisor** | 内置 agent，看 Conversation 上下文做调度决策；不是"大脑"，跟其他 agent 一样有日志 |
| **InputRequest** | Agent 卡住向你请示；你在 WC 卡片回，agent 续跑 |
| **Project** | Task 的归属容器；一个 worker 可以 mapped 到多个 Project |
| **Artifact** | Agent 跑完的产物（PR URL / 文件 / 报告） |
| **Memory** | Supervisor 的持久笔记（markdown 文件，按 scope 分） |

完整术语表见 [bounded contexts § 1](./docs/design/architecture/strategic/03-bounded-contexts.md#-1-通用语言ubiquitous-language)。

<br/>

## 设计

`agent-center` 以 [DDD](https://en.wikipedia.org/wiki/Domain-driven_design) 为方法论，**7 个 Bounded Context**：

- **TaskRuntime** —— Task / Execution / Dispatch / Kill
- **Discussion** —— Issue / IssueComment / Conclude
- **Workforce** —— Worker / AgentInstance / Project / Mapping
- **Cognition** —— Supervisor / Invocation / Memory
- **Observability** —— Event / Trace / Stats
- **Conversation** —— Channel / DM / Thread / Message
- **SecretManagement** —— UserSecret + master key

跨 BC 走 events / RPC，不共享物理表（[§ 9.z](./docs/rules/conventions.md)）。所有持久化只走对应 BC 的 AppService —— transport 层（unix socket / TCP+TLS）是工件，**领域不变量永远在 AppService 后面**。

文档入口：
- [设计入口](./docs/design/README.md)
- [DDD 蓝图（推进 plan + status）](./docs/design/ddd-blueprint.md)
- [战略层 / 领域愿景](./docs/design/architecture/strategic/00-domain-vision.md)
- [战术层 / 各 BC overview](./docs/design/architecture/tactical/)
- [ADR 索引](./docs/design/decisions/)
- [项目规约（必读）](./docs/rules/conventions.md)
- [Roadmap（v3 推迟项）](./docs/design/roadmap.md)

<br/>

## 开发

### 前置

- **Go** 1.22+
- **Node.js** 20+ + **pnpm**（Web Console SPA 用）
- **macOS** 或 **Linux**（Windows 未测）

### 构建

```bash
make build                  # 前端（vite）+ 后端（go）+ worker-daemon + fakeagent
                            # 产物 ./bin/{agent-center, agent-center-worker-daemon, fakeagent}

VERSION=v2.4.1 make build   # 指定版本号（默认 v2.4.0）
```

前端先打包（`web/` → `internal/webconsole/spa/dist/`），再通过 `go:embed` 嵌进 Go binary —— 单 binary 自带完整 Web Console。

SPA 开发时另起 vite dev server，`/api` 代理到本机 Go server：

```bash
pnpm --dir web install      # 一次性
pnpm --dir web run dev      # http://localhost:5173 → 127.0.0.1:7100
```

### 测试、lint、smoke

```bash
make test            # go test ./...
make cover           # 带 coverage 报告
make cover-html      # 渲染到 ./coverage.html
make vet             # go vet ./...
make lint            # vet + lint-vendor + lint-mock-default + lint-doc-impl-drift
                     # （执行 conventions § 0.4 架构规则）
make smoke           # fresh-binary 部署 + 跑通一个 task 到 done —— § 0.4 #4 gate
```

E2E 测试（Playwright）：

```bash
make e2e-install     # 一次性：pnpm install + chromium 下载
make e2e             # 跑全套 E2E，含 deployed-pipeline spec
```

### 仓库结构

```
agent-center/
├── cmd/
│   ├── agent-center/               # 主 binary（server + CLI + install 命令）
│   ├── worker-daemon/              # worker daemon（独立 binary）
│   └── fakeagent/                  # smoke 测试用 agent（不调 LLM）
├── internal/                       # 一个 Bounded Context 一个子包
│   ├── taskruntime/  discussion/   # 还有 admin transport / webconsole / cli ...
│   ├── workforce/    cognition/
│   ├── observability/ conversation/
│   ├── secret/       admintoken/
│   └── ...
├── web/                            # React SPA（vite + TS + Tailwind）
│   └── src/                        # 编译到 internal/webconsole/spa/dist
├── docs/
│   ├── design/                     # DDD 架构 / ADR / 需求
│   ├── plans/                      # phase / cycle 计划 + audit
│   ├── deployment/                 # 各版本部署指南
│   ├── operations/                 # 运维 runbook
│   └── rules/conventions.md        # 跨切设计规则 —— 必读
├── sites/                          # 手写静态文档站（零构建，GitHub Pages 发布）
├── tests/                          # E2E 套件
├── contrib/                        # 老版 install 脚本（保留 reference）
└── Makefile
```

### 规约

贡献之前先读 [`docs/rules/conventions.md`](./docs/rules/conventions.md)。两条最容易踩的：

- **§ 0.4 —— AppService 是访问领域状态的唯一入口。** server 之外没有任何进程直读 SQLite；CLI / worker / web 全走 admin transport
- **§ 0.6 —— 没证据不要推断设计意图。** 只描述「现状」（观察）+「能力」（模型）。不要越界到"系统设计上假设了 X"，除非你能 `grep` 到证据

### 打包（release tarball）

`make release` 一条命令打出当前平台可直接喂给 `./install` 的 tarball：

```bash
make clean-dist     # 可选：清旧 tarball
make release        # → dist/agent-center-v<ver>-<os>-<arch>.tar.gz + sha256

# 它做的事：
#   1. make build（前端 + 后端 + worker-daemon）
#   2. 在 dist/agent-center-v<ver>-<os>-<arch>/ 拼好 bin/ + install
#      wrapper + LICENSE + README.md
#   3. tar -czf 打包 + 输出 sha256 + 解压验证命令提示
```

跨平台 tarball（Mac 上交叉编译 Linux × amd64/arm64 等）、签名、GitHub Releases publish、CI 都推迟到 v3 "Deployment as Product" 主题。当前 `make release` 覆盖当前平台 —— 是端到端测 install 流程的最小所需。

### 本地文档站点

仓内 [`sites/`](./sites/) 是**手写静态站**（纯 HTML + 一份共享 `assets/site.css` /
`site.js`，**无构建步骤**），是 `docs/` 的对外**精选门面**——`docs/` 始终是权威源。
站点结构与「页 ↔ 源」对照见 [`sites/README.md`](./sites/README.md)。

```bash
# 本地预览 —— 直接开文件，或起个静态服务：
open sites/index.html                 # 或：python3 -m http.server -d sites 5173
```

部署是自动的：`.github/workflows/pages.yml` 在每次 push 到 `main` 时把 `sites/**`
发布到 GitHub Pages（项目子路径 `/agent-center/`，全相对链接）。没有任何要构建的东西。

<br/>

## 贡献 & 反馈

本仓库当前是单人项目。想贡献的话：

- **Bug / 设计讨论** —— 开 GitHub Issue
- **代码贡献** —— 先扫一遍 [`docs/rules/conventions.md`](./docs/rules/conventions.md)（§ 0.4 AppService 唯一入口 + § 0.6 不越界推断设计意图最容易踩）
- **路线图反馈** —— 在 [Roadmap](./docs/design/roadmap.md) 找对应条目或开 Discussion

静态站点 `sites/` 是对外入口（已发布到 GitHub Pages）；完整细节请直接看仓内 `docs/`。
