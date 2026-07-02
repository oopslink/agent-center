# Orchestration Engine Phase 2 — Integration & Migration

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Wire orchestration engine into the existing system: MCP tools, Task/Plan foreign keys, cycle flow template, and old DAG code cleanup.

**Architecture:** MCP tools follow the existing callAdmin pattern (args struct → admin handler → service call). Task gains a `nodeID` field, Plan gains a `graphID` field. The cycle flow template is a markdown document. Old cycle-specific code (CycleNodeRole, merge guard, scaffold, decision auto, unmerged board) is removed from the engine layer; mergecheck/gatecheck packages are retained as standalone HTTP-callable services.

**Tech Stack:** Go 1.23+, SQLite, MCP SDK (`github.com/mark3labs/mcp-go`)

## Global Constraints

- Module path: `github.com/oopslink/agent-center`
- MCP tool name = admin route segment = handler name base
- `agent_id` always from `cfg.AgentID`, never from args
- Table prefix: `pm_graph_` for orchestration tables
- Follow existing patterns in `internal/mcphost/tools.go` and `internal/admin/api/agent_tools_*.go`
- Run tests: `go test ./internal/projectmanager/... ./internal/mcphost/... ./internal/admin/... ./internal/persistence/...`

---

### Task 1: Orchestration Service Layer

Create a thin service layer that wraps the orchestration domain model for use by admin handlers. This service handles DB transactions, ID generation, and orchestrates the repo calls.

**Files:**
- Create: `internal/projectmanager/orchestration/service.go`
- Test: `internal/projectmanager/orchestration/service_test.go`

**Interfaces:**
- Consumes: `Graph`, `Node`, `Edge`, `ConditionConfig`, `EvaluateUpstream`, `ApplyConditionResult`, `ParseConditionConfig` from domain; `GraphRepository`, `NodeRepository`, `EdgeRepository` from repository.go
- Produces:
  - `type Service struct` with `NewService(deps ServiceDeps) *Service`
  - `func (s *Service) CreateGraph(ctx, planID string) (GraphID, error)` — creates graph + auto start/end nodes, persists all
  - `func (s *Service) GetGraph(ctx, graphID) (*Graph, error)` — loads graph with nodes + edges
  - `func (s *Service) StartGraph(ctx, graphID) error`
  - `func (s *Service) FinishGraph(ctx, graphID) error`
  - `func (s *Service) AddNode(ctx, graphID, category, controlKind, title string, metadata map[string]any) (NodeID, error)`
  - `func (s *Service) RemoveNode(ctx, nodeID) error`
  - `func (s *Service) UpdateNode(ctx, nodeID, title string, metadata map[string]any) error`
  - `func (s *Service) StartNode(ctx, nodeID) error`
  - `func (s *Service) CompleteNode(ctx, nodeID, outcome string) error`
  - `func (s *Service) DiscardNode(ctx, nodeID) error`
  - `func (s *Service) ResolveCondition(ctx, nodeID, result string) error`
  - `func (s *Service) AddEdge(ctx, graphID string, from, to NodeID) error`
  - `func (s *Service) RemoveEdge(ctx, graphID string, from, to NodeID) error`
  - `func (s *Service) ListNodes(ctx, graphID string, status, category string) ([]*Node, error)`
  - `func (s *Service) GetNode(ctx, nodeID) (*Node, error)`
  - `func (s *Service) GetReadyNodes(ctx, graphID) ([]*Node, error)`
  - `func (s *Service) BindTask(ctx, nodeID, taskID string) error`
  - `func (s *Service) UnbindTask(ctx, nodeID string) error`

---

### Task 2: MCP Tool Handlers + Admin Routes

Register orchestration tools in the MCP server and create admin handlers.

**Files:**
- Create: `internal/mcphost/orchestration_tools.go` — MCP handler factories
- Create: `internal/admin/api/agent_tools_orchestration.go` — admin HTTP handlers
- Modify: `internal/mcphost/server.go` — register tools in `registerAllTools()`
- Modify: `internal/mcphost/agent_facing_set.go` — add tool names
- Modify: `internal/mcphost/tiering.go` — add to secondary tools
- Modify: `internal/admin/api/server.go` — register admin routes

---

### Task 3: Task.nodeID and Plan.graphID Foreign Keys

Add `nodeID` to Task and `graphID` to Plan aggregates, with migration.

**Files:**
- Modify: `internal/projectmanager/task.go` — add `nodeID` field + getter
- Modify: `internal/projectmanager/plan.go` — add `graphID` field + getter
- Modify: `internal/projectmanager/sqlite/issue_task_repo.go` — persist nodeID
- Modify: `internal/projectmanager/sqlite/plan_repo.go` — persist graphID
- Create: `internal/persistence/migrations/0092_v228_task_node_plan_graph.up.sql`
- Create: `internal/persistence/migrations/0092_v228_task_node_plan_graph.down.sql`

---

### Task 4: Cycle Flow Template Document

Create the cycle development workflow as a markdown template.

**Files:**
- Create: `docs/flow-templates/cycle.md`

---

### Task 5: Remove Cycle-Specific Code from Engine Layer

Remove CycleNodeRole, merge guard, scaffold, decision auto, and unmerged board from the Task/Service layer. Keep mergecheck/ and gatecheck/ packages intact (they become standalone).

**Files:**
- Modify: `internal/projectmanager/task.go` — remove `role`, `branch`, `base`, `skipMergeCheck` fields
- Modify: `internal/projectmanager/plan_unmerged.go` — remove CycleNodeRole, CycleNodeMeta
- Modify: `internal/projectmanager/service/assign_flow.go` — remove guardIntegrateMerge
- Modify: `internal/projectmanager/service/appservices.go` — remove cycle fields from CreateTaskCommand
- Modify: `internal/projectmanager/service/plan_scaffold.go` — remove scaffold_cycle_plan
- Modify: `internal/projectmanager/service/decision_auto.go` — remove decision auto
- Modify: `internal/projectmanager/service/plan_unmerged.go` — remove unmerged board
- Modify: All affected tests
- Modify: `internal/mcphost/server.go` — remove `scaffold_cycle_plan` tool
- Modify: `internal/mcphost/agent_facing_set.go` — remove `scaffold_cycle_plan`, `set_task_skip_merge_check`, `list_unmerged_branches`
- Create: `internal/persistence/migrations/0093_v228_drop_task_cycle_fields.up.sql`
- Create: `internal/persistence/migrations/0093_v228_drop_task_cycle_fields.down.sql`
