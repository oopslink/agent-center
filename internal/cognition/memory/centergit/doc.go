// Package centergit implements center-hosted git storage for agent and team
// memory — the "方案 A" of the Team 一等实体 design
// (docs/design/features/2026-07-12-team-entity-design.md §4.2/§4.3/§9).
//
// The center hosts one bare git repo per agent, per team, plus a single global
// repo. Runtimes clone/pull/push over git's smart-HTTP protocol, reusing the
// center's existing per-agent bearer token for auth. This package provides:
//
//   - Host (host.go): bare-repo provisioning on the center's disk
//     (git init --bare), per-agent / per-team / global (§4.2, §4.3 write path).
//   - Authorizer + TeamMembership (authz.go): the access-control decision —
//     "does this token's agent belong to the repo's team → rw; global repo is
//     readable by all; an agent may rw its own repo" (§9 访问控制映射).
//     TeamMembership is the seam onto S1's team service (center maintains the
//     agent→team mapping); a concurrency-safe in-memory MapMembership ships here
//     for bootstrap and tests, and models "实例化时给新 agent 授权其 team repo"
//     via Grant.
//   - Handler (httpbackend.go): a git smart-HTTP endpoint that authenticates the
//     caller, authorizes read (upload-pack) vs write (receive-pack) against the
//     requested repo, then bridges to git-http-backend via net/http/cgi.
//   - Store (store.go): the client-side memory store that keeps 每条经验一文件
//     (slug/uuid-named entry files) with a single MEMORY.md index DERIVED from
//     the entries (never hand-edited), and pushes with pull-rebase-retry to
//     absorb concurrent team writes (§5 渐进式加载 index, §9 并发写).
//
// Integration note (S2 stacks on S1): wiring Handler into the admin API's
// router and backing TeamMembership with S1's team sqlite tables is a single
// step performed once S1's Team entity lands; see Handler docs for the exact
// AgentResolver contract against the admin auth middleware.
package centergit
