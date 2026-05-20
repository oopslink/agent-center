# WorkerProjectProposal 聚合

> **DDD 战术层** · BC: Workforce · 聚合: WorkerProjectProposal（独立 AR）

WorkerProjectProposal 是 Worker 自动扫描发现的 "候选项目映射"。**需要用户飞书 / Web Console / CLI 确认才能升级成 WorkerProjectMapping**。

设计动机：避免 Worker 一发现 git repo 就建出无用 mapping —— 用户决策权前置，明示"哪些项目你打算在哪些 worker 上跑"。详见 [ADR-0008](../../../decisions/0008-worker-project-mapping-via-discovery-proposal.md)。

---

## § 1. 状态机（4 态）

```
pending ──┬──▶ accepted ─→ (终态)
          │
          ├──▶ ignored ─→ (可通过 unignore 回到 pending)
          │
          └──▶ superseded ─→ (终态)
```

| 转移 | 触发 |
|---|---|
| `pending → accepted` | 用户点 [✅ 加入] / [✏️ 改后加入]（ProposalReviewService accept）|
| `pending → ignored` | 用户点 [❌ 忽略] |
| `ignored → pending` | `agent-center worker proposal unignore <id>` 显式恢复 |
| `pending → superseded` | 新的 proposal 提议同 (worker_id, candidate_path) 时旧的标 superseded（v1 罕见，Worker 端 scan 已查重不重复 propose）|

**终态**：accepted / superseded 不可逆；ignored 可通过 unignore 回到 pending。

---

## § 2. 字段

```
worker_project_proposal (
  id                      ULID/UUID
  worker_id               FK → workers (强引用，不可变)
  candidate_path          TEXT  -- /Users/oopslink/code/agent-center
  suggested_project_id    TEXT  -- 'agent-center' (worker 的猜测，常用 dir name)
  suggested_kind          TEXT, nullable  -- 'coding' / 'writing' / null
  candidate_metadata      TEXT (JSON)  -- {git_remote_url, commit_count, recent_activity_at, detected_language}
  status                  pending | accepted | ignored | superseded
  proposed_at             ISO8601 TEXT
  reviewed_at             ISO8601 TEXT, nullable
  reviewed_by_identity_id TEXT, nullable
  resulting_mapping_id    ULID/UUID, nullable  -- 若 accepted, 指向生成的 mapping（反向血缘）
)
```

`candidate_metadata` 走 JSON 字段 / 不在 SQL 里查（[conventions § 9 dialect-agnostic](../../../../rules/conventions.md)）。

---

## § 3. 发现流程（ProposalDiscoveryService）

```
1. Worker 周期扫 scan_paths (启动后 + 每 scan_interval):
   找出所有 .git 目录
   按 exclude glob 过滤

2. 对每个候选, worker 先查 center:
   "(worker_id, candidate_path) 见过吗?"
   - accepted: 跳过 (mapping 已有)
   - ignored : 跳过 (用户已拒绝；除非用户 unignore)
   - pending : 跳过 (等用户审)
   - 未见过 : 走下一步

3. Worker emit `worker_project_proposal.proposed`:
   含 suggested_project_id (默认 = dir name)
   含 suggested_kind (启发式: go.mod → coding, manuscript/ → writing, ...)
   含 candidate_metadata (git remote, commit 统计等)

4. Center 入库 worker_project_proposals(status=pending) + 触发 supervisor 唤醒

5. Supervisor 决定如何呈现 (v1: 直接推飞书卡片):
   多条 proposal 可批量打包成一张卡片, 也可逐条

6. 飞书卡片:
   🔍 Worker mac-mini-1 发现候选项目:
       📁 /Users/oopslink/code/agent-center  (Go, 2.1k commits, github.com/.../agent-center)
       建议 project_id: agent-center
       建议 kind: coding
   [✅ 加入] [✏️ 改后加入] [❌ 忽略]
```

**启发式 suggested_kind**：

- `go.mod` / `package.json` / `Cargo.toml` / `pom.xml` / `pyproject.toml` 等 → `coding`
- `manuscript/` / 大量 `.md` / `.tex` → `writing`
- `Excel/` / 数据集 → `investing`（v1 启发不强）
- 默认 → null（让用户拍板）

---

## § 4. ProposalReviewService（决策）

### 4.1 用户点击 ✅ 加入

```
单事务内：
  a. 校验 suggested_project_id 不跟既有 project 冲突
     冲突 → 飞书卡片标红，让用户改 project_id 后再提交（不允许同名）
  b. 若 project 不存在: 自动创建 Project（用 suggested_project_id + suggested_kind）
     emit project.created
  c. 创建 WorkerProjectMapping(base_path=candidate_path, source_proposal_id=...)
     emit worker_project_mapping.added
  d. proposal.status=accepted; reviewed_at / reviewed_by 填值; resulting_mapping_id 回填
     emit worker_project_proposal.accepted
```

### 4.2 用户点击 ✏️ 改后加入

```
1. Bridge 弹卡片让用户编辑 project_id / name / kind / default_agent_cli
2. 用户提交后走 § 4.1 流程，用编辑后的字段
```

### 4.3 用户点击 ❌ 忽略

```
proposal.status = ignored
reviewed_at / reviewed_by 填值
emit worker_project_proposal.ignored
Worker 下次扫 (worker_id, candidate_path) 跳过 (因为 status=ignored)
```

### 4.4 用户后悔忽略

```
agent-center worker proposal unignore <proposal_id>
→ proposal.status=pending
→ emit worker_project_proposal.unignored
→ Worker 下次扫该路径会再次见到（不视为新 proposal；同条 proposal 复活）
```

---

## § 5. 同一 project 被多 worker 发现

Worker A 已 accepted `agent-center → /Users/.../code/agent-center`。
Worker B 扫到自己本地 `/home/.../code/agent-center`，suggested_project_id 也是 `agent-center`。

Center 检测到 Project `agent-center` 已存在 → 仍然走 propose + 飞书路径：

```
Worker home-server 也发现 agent-center 项目:
  📁 /home/oopslink/code/agent-center

是否在该 worker 上也启用?
[✅ 启用 (默认)] [❌ 不启用]
```

默认选项是 ✅ —— 一键即可，避免无意义的二次确认。

Accept 时不再建 Project（已存在），只建 Mapping。

---

## § 6. WorkerProjectProposal Invariants

1. **worker_id / candidate_path 唯一对**：同 (worker_id, candidate_path) 至多一条 active proposal（非终态）
2. **terminal 状态 accepted / superseded 不可逆**；ignored 可通过 unignore 回到 pending
3. **accept 时必须连带建 Mapping**（[ADR-0014 § 2](../../../decisions/0014-event-sourcing-level.md) 同事务）；spawn 失败 → 不进 accepted 终态
4. **suggested_project_id 命名冲突时不允许直接 accept**：必须走 ✏️ 改后加入路径
5. **reviewed_at / reviewed_by 必须跟终态共填**：状态 != pending 时这两字段非空（v1 ignored / accepted）

---

## § 7. 事件

| 事件 | 触发 | payload |
|---|---|---|
| `worker_project_proposal.proposed` | Worker 扫到新候选 | proposal_id, worker_id, candidate_path, suggested_* |
| `worker_project_proposal.accepted` | 用户 accept | proposal_id, resulting_mapping_id |
| `worker_project_proposal.ignored` | 用户 ignore | proposal_id |
| `worker_project_proposal.unignored` | CLI unignore | proposal_id |
| `worker_project_proposal.superseded` | 极少；Worker 重新提议时旧的标 superseded | proposal_id, superseded_by_proposal_id |

---

## § 8. CLI

| 命令 | 用途 |
|---|---|
| `agent-center worker proposal list [--worker-id=...] [--status=pending|ignored|accepted]` | 列 |
| `agent-center worker proposal show <proposal_id>` | 详情（含 candidate_metadata）|
| `agent-center worker proposal accept <proposal_id> [--project-id=...] [--kind=...]` | 同飞书 ✅；--project-id / --kind 覆盖 suggested |
| `agent-center worker proposal ignore <proposal_id>` | 同飞书 ❌ |
| `agent-center worker proposal unignore <proposal_id>` | 复活 |

详见 [agent-harness/02-skill-cli-tooling.md](../agent-harness/02-skill-cli-tooling.md)。

---

## § 9. References

- [ADR-0008 WorkerProjectMapping discovery proposal](../../../decisions/0008-worker-project-mapping-via-discovery-proposal.md)
- [ADR-0014 事件溯源走 L1](../../../decisions/0014-event-sourcing-level.md)（accept 同事务双写）
- [00-overview.md § 3.2-3.3](00-overview.md) — ProposalDiscoveryService / ProposalReviewService
- [01-worker.md § 4 WorkerProjectMapping](01-worker.md) — accept 产物
- [02-project.md § 3.1](02-project.md) — Project accept 时自动创建路径
- [bridge/01-feishu-integration.md](../bridge/01-feishu-integration.md) — 飞书 Proposal 卡片渲染
- [cognition/00-overview.md](../cognition/00-overview.md) — Supervisor 在 proposal 决策中的角色（提醒用户，不替决策）
