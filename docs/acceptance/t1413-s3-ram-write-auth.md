# T1413 S3 RAM Write Authorization Matrix

All entries below are gated through `internal/authorization.Service` when the
authorization feature gate supplies an authorizer. Legacy, shadow, and enforce
semantics are owned by that shared service; handlers do not use dual-allow
fallbacks once enforce mode is active.

| Surface | Write operations | Permission | Resource scope | Focused coverage |
| --- | --- | --- | --- | --- |
| Admin HTTP bearer | admin-token scoped writes | mapped bearer permission | typed wildcard resource for token scope | `authz_delegate_test.go` |
| MCP issue tools | create/update/close/reopen/link issue | `issue.write` | `issue` with project/org context | `agent_tools_issues_test.go`, T1413 focused suite |
| MCP task tools | create/block/complete/update/reassign/fork task | `task.write` or task self permissions | `task` with project context | `TestT1413AgentCreateTaskEnforceUsesSharedAuthorization` |
| MCP plan tools | create/start/stop/topology/stage/task/dependency changes | `project.write` for creation, `plan.write` for existing plans | `project` for creation, `plan` for mutations | `TestT1413AgentPlanWriteEnforceRevokesPlanTools` |
| MCP project tools | project metadata/member/repo/stage mutations | `project.write`, `project.member.*`, `project.repo_ref.manage`, `project.stage.manage` | `project` | `agent_tools_passthrough_test.go`, PM web tests |
| MCP team tools | create/update/delete/template/import/instantiate | `team.create`, `team.write` | `org` for creation/template instantiation, `team` for mutation | `TestT1413AgentTeamCreateEnforceUsesOrgScope` |
| MCP team membership/tools | add/remove member, associate project, assign roles | `team.member.manage`, `team.project.link.manage`, `team.runtime_config.manage` | `team` | `TestT1413AgentTeamMemberManageEnforceRevokesTeamTools`, `agent_tools_team*_test.go` |
| MCP team memory | propose/review team memory | `team.memory.propose`, `team.memory.review` | current `team` | `agent_tools_team_memory_test.go` |
| MCP files | upload/attach scoped files | `file.upload`, `file.attach` | `file` with file URI and resolved owner refs | `agent_tools_files_test.go` |
| MCP messages | post message, reply, close/archive conversations | `conversation.post`, task/issue/plan write for owner-scoped actions | `conversation`, `task`, `issue`, `plan` | `agent_tools_write_test.go` |
| MCP orchestration | graph/node/edge/task binding mutations | `plan.write` | owning `plan` resolved from graph/node | orchestration focused suite |
| MCP orchestration templates | create/update/delete templates | `template.write` | `org` | orchestration focused suite |
| Web PM APIs | project/issue/task/plan mutations | same PM permissions as MCP | `project`, `issue`, `task`, `plan` | `handlers_pm*_test.go` |
| Web Team APIs | create/update/delete/instantiate/template save/import | `team.create`, `team.write` | `org` for creation/template writes, `team` for mutation | `TestT1413WebCreateTeamEnforceUsesSharedAuthorization`, `handlers_teams*_test.go` |
| Web Team links/members/RAM roles | project links, roster changes, RAM role mapping writes | `team.project.link.manage`, `team.member.manage`, `team.runtime_config.manage` | `team` | `handlers_teams*_test.go`, `handlers_team_ram_roles_test.go` |
| Background projectors/daemons | outbox projections and wake/reminder side effects | service-owned system path, not caller-originated API writes | source event scope already authorized at ingress | existing projector/reminder service tests |

Expected denial shape is a shared `403` response with `permission_denied` and
the authorization service decision reason, except resource non-existence and
authentication failures which retain `404`/`401` semantics.
