# 0028. Skill File Mount（v2 lite，G5）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-22 |
| Related | v2 议题 G5（[v2-kickoff-2026-05-22 § Group 2 G5](../../drafts/v2-kickoff-2026-05-22.md)）；建立于 [ADR-0024 AgentInstance 一等公民化](0024-agent-instance-first-class.md)（home_dir）之上；跟 [ADR-0029 Supervisor as Built-in AgentInstance](0029-supervisor-as-builtin-agent-instance.md) 协同 |

## Context

### v1 现状

worker agent prompt 注入是「全塞 prompt」（[agent-harness/01-prompt-assembly](../../architecture/tactical/agent-harness/01-prompt-assembly.md)）：

- `worker-agent.md` skill（agent-center 项目 bundled with binary，拼进 prompt）
- `extra_skill_files`（supervisor 派单时附加，也是拼进 prompt）
- `task.prompt`（supervisor 加工后指令，拼进 prompt）

「全塞」的代价：

- prompt 越涨越大，token cost 涨
- 没有「per-AgentInstance skill 集」概念，无法 per-agent 配 skill
- 没用 [Anthropic Skills 标准](https://anthropic.com/news/skills)（SKILL.md 文件 + 按需加载）

### G5 想加什么

让用户为 AgentInstance 配一组 **skill 文件**，由 agent CLI 按 Anthropic Skills 标准**按需加载**（不全塞 prompt）。

### 用户拍板的方向（2026-05-22 #agent-center 讨论）

> **长期方向**（v3+）：agent **本体**（cli + harness + bundled skill）打成不可变 **image**，**数据**（memory 等）走 git；本体 vs 数据分开管理 + 独立版本化。
>
> **v2 范围**：
> - 用户自配 skill = **文件**摆 worker / center 机；自己版本管理（git / copy / 等）
> - 系统**内置** skill（`worker-agent.md` / `supervisor.md` / 未来内置工具 skill）= **跟 binary 一起部署**
> - **不**在 center DB 管理 / **不**做分发 / **不**引入中心化 skill 库

这跟 v3+ image 模型方向一致 —— v2 文件结构直接是 v3 image build 的 source，无 migration 痛苦。

## Decision

### 1. 用户自配 skill 在 AgentInstance home_dir 内

路径约定：

```
{AgentInstance.home_dir}/skills/<skill-name>/SKILL.md
```

其中 `home_dir` 按 [ADR-0024](0024-agent-instance-first-class.md) + [ADR-0029](0029-supervisor-as-builtin-agent-instance.md):

| Actor | home_dir |
|---|---|
| Worker AgentInstance | `~/.agent-center-worker/agents/<agent_instance_id>/` |
| Built-in supervisor AgentInstance | `~/.agent-center/agents/supervisor/` |

用户**自己**在该路径下创建 / 编辑 skill 文件；可用 git 管理目录；可手动 copy 跨 agent 复用。

### 2. Schema = Anthropic Skills 标准

直接吃 [Anthropic Skills](https://anthropic.com/news/skills) 格式（SKILL.md + frontmatter 元数据）。center 不重新定义 schema。

### 3. 内置 skill 跟 binary 部署

| Skill | 部署方式 |
|---|---|
| `worker-agent.md` | 跟 worker binary bundle，安装路径 `${install_dir}/skills/worker-agent.md` |
| `supervisor.md` | 跟 center binary bundle，同上 |
| 其他未来内置 skill（如系统级任务管理 skill）| 同上 |

[agent-harness/01-prompt-assembly](../../architecture/tactical/agent-harness/01-prompt-assembly.md) 的 bundled skill 装载机制**不变**。

### 4. Worker daemon spawn 时挂载机制（Q4 c+a）

agent_cli adapter 决定：

| Adapter | 挂载方式 |
|---|---|
| claude-code | `claude --skill-path=<home_dir>/skills/ ...`（CLI 标志支持，按需扫该路径加载 SKILL.md）|
| codex / opencode / gemini | (a) fallback：spawn 前 `setenv HOME=<exec-dir>` + symlink `~/.claude/skills` → `home_dir/skills/`（让 CLI 从约定路径自动加载） |

详见 [implementation/05-agent-adapters](../../implementation/05-agent-adapters.md)（adapter 实装在 G3 议题后扩展）。

### 5. home_dir/skills/ 期间只读约束

跟 [ADR-0024 § 5](0024-agent-instance-first-class.md) 一致：execution 期间 agent 进程对 home_dir 只读；改 skill = between-execution 行为。

### 6. v2 不引入

| 项 | 不引入理由 |
|---|---|
| Center DB Skill AR / `skills` 表 | 用户自管文件；不需中心化 |
| AgentSkillAttachment M:N 表 | 同上 |
| Skill 内容走 BlobStore | 同上 |
| `agent-center skill create / list / update / delete / usage` CLI | 同上 |
| `agent-center agent skill attach / detach` CLI | 同上 |
| `skill.*` / `agent_skill.*` 事件 | 同上 |
| Skill 跨 worker / 跨 agent 自动 sync | 同上；用户手动 copy |
| Skill 版本管理（agent-center 内）| 用户用 git 管理 home_dir |

→ G5 ADR 是「**约定 + 现状声明**」性质，零新增模型。

## Consequences

**正面**：

- 实施范围极小（约定 + adapter mount + 文档）
- 跟 v3+ image 模型方向一致；不引入会被 image 模型取代的 technical debt
- 用户体验直接（编辑文件 → next execution 生效），无额外 CLI 学习成本
- 内置 skill 仍随 binary 走，部署模型不变

**负面 / 待跟进**：

- 没有 center 端 skill audit / 反查 / 多 agent 复用便利 —— 用户在多个 home_dir 间手动管理
- 跨 worker 复用 skill 要手动 copy
- 期望 v3+ image 模型给齐这些便利

## Alternatives Considered

### A. V-full：Center 管理 skill 库 + M:N attach + BlobStore content

- 早期讨论中推荐过；引入 `Skill` AR + `AgentSkillAttachment` 表 + skill CRUD CLI + 跨 worker sync
- ✅ 集中 audit / 跨 agent 复用 / 更新 propagation
- ❌ 跟用户长期方向（agent-as-image）矛盾 —— 是 image 模型的 technical debt
- ❌ v2 工程量大，跟「skill 是用户自配」的简单心智不符
- 否决（见 [ADR-0028 § Context](#context) 用户表态）

### B. 仅 per-task `extra_skill_files`（envelope 字段）

- 维持现状；不加 per-AgentInstance skill 概念
- ❌ 每次派单都要重写 envelope；agent 没有持久能力沉淀
- 否决：跟 G1 「agent 一等公民 + 持久身份」 不一致

### C. Skill 跟 SecretManagement 一样建 BC

- 引入 `SkillManagement` BC（对称 SecretManagement）
- ❌ Skill ≠ secret；不需要加密 / 解析 / 授权矩阵 / 跨 BC 共享
- ❌ image 模型来时整套 BC 被取代
- 否决

## References

### 相关 ADR

- [ADR-0024 AgentInstance 一等公民化](0024-agent-instance-first-class.md)（home_dir 父结构）
- [ADR-0029 Supervisor as Built-in AgentInstance](0029-supervisor-as-builtin-agent-instance.md)（home_dir 路径分支）
- [ADR-0005 Charter 留在项目仓库](../0005-project-charter-stays-in-project-repo.md)（项目本地 CLAUDE.md 跟本 ADR 的 home_dir/skills/ 是不同层次）

### 跨 BC

- [agent-harness/01-prompt-assembly](../../architecture/tactical/agent-harness/01-prompt-assembly.md) — 挂载机制位置
- [implementation/05-agent-adapters](../../implementation/05-agent-adapters.md) — adapter 翻译细节

### 长期方向

- [roadmap.md v3+ AgentImage 模型 + Memory git 化](../../roadmap.md)

### 来源

- [docs/design/drafts/v2-kickoff-2026-05-22.md § Group 2 G5](../../drafts/v2-kickoff-2026-05-22.md)
- 2026-05-22 #agent-center 讨论（用户提出 agent-as-image 长期方向后简化 G5）
