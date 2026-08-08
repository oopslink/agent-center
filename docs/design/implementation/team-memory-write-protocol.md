# Team Memory Write Protocol

> 功能契约：[Team Memory 受控写入能力](../features/team-memory-controlled-writing.md)。
> 决策：[ADR-0057](../decisions/0057-controlled-team-memory-writes.md)。

## 1. 代码边界

- `internal/cognition/memory/teammemory`：TeamMemory aggregate、Proposal 状态机、Application Service ports。
- `internal/cognition/memory/centergit`：Git Repository adapter；Store 继续负责 markdown/index plumbing。
- `internal/team`：TeamMemoryPolicy value object（mode、curator_agent_refs）及 membership invariant。
- `internal/admin/api`：MCP admin adapter 与 human Web adapter；只解析/鉴权上下文，不直接操作 Store。
- `internal/mcphost`：四个 agent-facing tool proxy。
- `internal/webconsole/api`、`web/src`：proposal review/read model 与 UI。
- `internal/observability` adapter：从 Git proposal transition 幂等投影标准 event，不读取/复制正文。

Cognition service 通过 Team policy/membership port 查询授权；不直读 Team SQLite repository。Team BC
不直接写 Git，避免跨 BC 共享底层存储。

Git mutation 与 events DB 不具备同事务能力，因此 Git proposal transition 是 write authority；
observability projector 维护 durable checkpoint，按稳定 event id 重放到 events。Application Service
不能把 event emit 失败当作 Git 回滚，也不能静默丢弃，reconcile 必须最终补齐。

新增 MCP 工具必须同步更新 `AgentFacingToolNames`、server registration、admin route、tool parity test
与 `get_my_profile.my_capabilities` 的描述性投影；不能只写 handler 形成不可达能力。

## 2. Git Tree

```text
team/<team_id>.git (main)
├── MEMORY.md                         # derived
├── entries/<slug>-<uuid>.md          # canonical
├── rules/<slug>-<uuid>.md            # canonical
└── proposals/<proposal_id>.md         # workflow/audit, runtime readers ignore
```

Proposal frontmatter：

```yaml
---
proposal_id: tmprop-...
operation: update
target_kind: rule
target_source_path: rules/review-rigor-r1.md
target_uuid: r1
expected_blob_hash: <git-blob-sha>
author_ref: agent:...
created_at: 2026-08-09T00:00:00Z
idempotency_key: task-.../finding-...
status: pending
rationale: repeated review failure
evidence_refs: [task:task-..., issue:issue-...]
warnings: []
---

<candidate markdown encoded in fenced sections owned by the serializer>
```

Serializer/parser 必须是结构化、确定性的；不能通过拼接任意 frontmatter 写盘。Proposal 文件不被
`ReadTeam`、`ReadTeamRules`、`RegenerateIndex` 扫描。

Git commit author 使用经过净化的稳定系统 identity；真实 `actor_ref` 必须在 proposal frontmatter
和 event refs 中保存，不能把未校验的 display name/email 直接注入 Git config。

## 3. Git Mutation Algorithm

每次 command 使用临时 clone，读取 remote main HEAD 作为 aggregate version：

### Propose

1. Resolve current Agent → Team，校验 membership。
2. 以 `(team_id, author_ref, idempotency_key)` 查已有 proposal；同 payload 返回原结果，不同 payload 409。
3. 校验 candidate/target/content；生成唯一 proposal file。
4. commit + push；遇无关并发 commit 时 pull-rebase、重新查 idempotency 后重试。

### Promote/Reject

1. 校验 human owner/admin 或 Team policy curator grant。
2. clone 并要求 HEAD=`expected_repo_commit`；加载 pending proposal。
3. 对 update/disable/delete 校验 target UUID、source path、blob hash；add 校验 proposal 未被消费。
4. Promote：修改 canonical + proposal status + `RegenerateIndex`，一个 commit；Reject：只改 proposal status。
5. push 使用 ref lease/CAS。失败则重新读取；状态已由同一 command 完成时幂等返回，否则 409
   `team_memory_version_conflict`，绝不 last-write-wins。

不得复用当前 `SyncPush --strategy-option=theirs` 处理同 proposal promotion；该策略仅适合唯一文件的
append seed。通用 mutation 需要识别 canonical/proposal 冲突并 fail-loud。

## 4. Service Commands

```go
type ProposeCommand struct {
    ActorRef, IdempotencyKey string
    Operation, TargetKind    string
    Target                   *TargetRef
    Candidate                *Candidate
    Rationale                string
    EvidenceRefs             []string
}

type ReviewCommand struct {
    ActorRef, ProposalID, Action string
    ExpectedRepoCommit           string
    Comment                      string
    AcknowledgeWarnings          []string
}
```

返回统一包含 `team_id/proposal_id/status/repo_commit/source_path/warnings/effective_for`。

错误 taxonomy：`not_team_member`、`not_memory_curator`、`invalid_candidate`、`secret_detected`、
`warning_unacknowledged`、`target_changed`、`proposal_not_pending`、`idempotency_conflict`、
`team_memory_version_conflict`、`git_unavailable`。reason 与 message 分离。

## 5. HTTP/MCP

Admin agent tools：

- `POST /admin/agent-tools/propose_team_memory_change`
- `POST /admin/agent-tools/list_team_memory_proposals`
- `POST /admin/agent-tools/get_team_memory_proposal`
- `POST /admin/agent-tools/review_team_memory_proposal`

Agent 请求只带 `agent_id`，Team 由 membership 解析；list/get 不能借 proposal id 越权读取其它 Team。

Web Console 增加 org-scoped endpoints，session identity 必须为 owner/admin 才可 mutate；普通 member
只有 read。两类 endpoint 组装同一 service command，不能复制 Git mutation。

## 6. Team Policy Persistence

Team aggregate 增加：

```go
type TeamMemoryPolicy struct {
    Mode              string   // proposal_only | curator_auto
    CuratorAgentRefs  []string
}
```

SQLite 以普通列/子表持久化（dialect-agnostic）；grant 必须引用本 Team 的 Agent member。成员移除和
Team 删除与 policy 更新遵守 Team aggregate invariant。Owner/admin Web mutation 是 policy 唯一写入口；
Agent MCP 不能自授 Curator。

## 7. Rollout

1. 先部署 schema/service，policy 默认 proposal_only，现有行为不变。
2. 上线只读 proposal list/get 与 propose；观察 Git/idempotency。
3. 上线 human owner/admin review。
4. 最后开放显式 Curator Agent MCP review；未配置 Team 不获得自动 promotion。
5. 旧 worker/MCP host 不受影响；新工具只在新 server catalog 出现，`get_team_rules` 契约不变。

`TeamMemoryProducer.SeedTeam` 与 legacy migration 在 service 上线后必须改走 service 的受信 bootstrap
command；不得继续作为绕过 proposal/policy 的长期第二写入口。Bootstrap commit 明确标记 actor/source，
仅用于 Team instantiate/migration，不暴露给 MCP/Web。

回滚时禁用新 mutation routes；已 promoted canonical commit 保留，可通过反向 proposal 或 Git revert
恢复，不能删除审计 proposal。

## 8. 测试

- Domain：proposal 状态机、policy membership/grant/revoke、终态不可重开。
- Repository：真实 bare repo add/update/disable/delete、index、CAS、并发 proposal、promotion 单赢家。
- Auth：same-team、non-team、cross-org、curator、revoked curator、human owner/admin/member。
- Security：path traversal、frontmatter injection、NUL、size、secret hard reject、warning acknowledgement、HTML escape。
- Contract：MCP parity、admin/Web 同 service、错误 taxonomy、pagination、旧 get_team_rules 无漂移。
- Runtime：promotion 前后 commit freeze；同 generation/in-flight 不变，新 generation/fork 生效。
- Gates：`go test ./...`、`make test-race`、`go vet ./...`、`go build ./...`、Web test/build/lint、
  `git diff --check`，以及 deployed binary + real bare repo smoke。
