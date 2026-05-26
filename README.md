<h1 align="center">agent-center</h1>

<p align="center">
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
> **v2.4.0 已发布（2026-05-26）** —— first-mile deployment 完成：单条命令装好 center 和 worker，Web Console 加 worker 一键复制命令；同机多 worker 隔离；atomic 升级 + 失败自动回滚。详见 [CHANGELOG](./CHANGELOG.md)。

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

- **TaskRuntime**（Task / Execution / Dispatch / Kill）
- **Discussion**（Issue / IssueComment / Conclude）
- **Workforce**（Worker / AgentInstance / Project / Mapping）
- **Cognition**（Supervisor / Invocation / Memory）
- **Observability**（Event / Trace / Stats）
- **Conversation**（Channel / DM / Thread / Message）
- **SecretManagement**（UserSecret + master key）

跨 BC 走 events / RPC，不共享物理表（[§ 9.z](./docs/rules/conventions.md)）。所有持久化只走对应 BC 的 AppService —— transport 层（unix socket / TCP+TLS）是工件，**领域不变量永远在 AppService 后面**。

```bash
# 跑一次 smoke：build 三 binary + enroll worker + 派 task + 看它 done
make smoke
```

文档入口：
- [设计入口](./docs/design/README.md)
- [DDD 蓝图（推进 plan + status）](./docs/design/ddd-blueprint.md)
- [战略层 / 领域愿景](./docs/design/architecture/strategic/00-domain-vision.md)
- [战术层 / 各 BC overview](./docs/design/architecture/tactical/)
- [ADR 索引](./docs/design/decisions/)
- [项目规约（必读）](./docs/rules/conventions.md)
- [Roadmap（v3 推迟项）](./docs/design/roadmap.md)

<br/>

## 现状

| Cycle | 状态 |
|---|---|
| v1 | 飞书 / 钉钉 / Bridge BC 路线（已撤回，见 [ADR-0031](./docs/design/decisions/0031-v2-drop-bridge-vendor-integration.md)） |
| **v2.0 – v2.4** | **已 ship** —— 单 binary + Web Console 主入口 + 7 BC 全套 + Multi-host TCP+TLS + first-mile deployment + 同机多 worker |
| v3+ | Deployment-as-Product 主题（容器化 / Dockerfile / CI / metrics / KMS master key）、外部 IM 渠道重新设计、云 computer 节点、多用户/SaaS 等 —— 见 [Roadmap](./docs/design/roadmap.md) |

<br/>

## 本地文档站点

仓内 [`sites/`](./sites/) 是 VitePress 静态站点脚手架，源 markdown 直接来自 [`docs/`](./docs/)：

```bash
cd sites/
npm install
npm run dev      # http://localhost:5173，markdown 热重载
npm run build    # → sites/.vitepress/dist/ 静态产物，拷哪都能跑
```

<br/>

## 贡献 & 反馈

本仓库当前是单人项目（[设计前提](./docs/plans/v2.4-deployment-first-mile.md#-1-为什么这件事是-v24-主线)）。
- Bug / 设计讨论：开 GitHub Issue
- 代码贡献：先扫一遍 [conventions](./docs/rules/conventions.md)（特别是 § 0.4 AppService 唯一入口 + § 0.6 不越界推断设计意图）
- 路线图反馈：在 [Roadmap](./docs/design/roadmap.md) 讨论

文档站点（如部署到 GitHub Pages 后）将作为统一的入口；当前请直接看仓内 `docs/`。
