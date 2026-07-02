# Orchestration Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a generic orchestration engine as a core subdomain within ProjectManager BC, providing DAG + state machine + condition node primitives via MCP tools.

**Architecture:** The engine lives in `internal/projectmanager/orchestration/` as a sub-package. It defines three domain types — Graph (aggregate root), Node (entity), Edge (value object) — with SQLite persistence and a repository interface. The engine is business-agnostic: no guard, no role, no cycle concepts. Condition nodes support three evaluation modes (upstream outcome, external hook, manual) with goto semantics for success/failure routing.

**Tech Stack:** Go 1.23+, SQLite (via `database/sql`), existing `internal/persistence` migration framework, existing `internal/clock` and `internal/idgen` packages.

## Global Constraints

- Module path: `github.com/oopslink/agent-center`
- Follow existing patterns: private fields + public getters, `NewXxxInput` / `RehydrateXxxInput` constructors, `touch(at)` for version bumping
- Table prefix: `pm_graph_` for all new tables
- Migration numbering starts at `0091`
- All times stored as RFC3339Nano UTC strings in SQLite
- Tests use `persistence.MemoryDSN()` + `persistence.NewMigrator(db).Up(ctx)`
- Run tests with `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/projectmanager/orchestration/...`

## File Structure

```
internal/projectmanager/orchestration/
  types.go              — GraphID/NodeID type aliases, sentinel errors, NodeCategory/ControlKind enums
  graph.go              — Graph aggregate root (status machine, auto-done detection)
  node.go               — Node entity (status machine, outcome, action log)
  edge.go               — Edge value object, cycle detection (ValidateNoCycle)
  condition.go          — ConditionConfig, evaluation logic (upstream/hook/manual), failure reopen chain
  repository.go         — GraphRepository + NodeRepository interfaces
  sqlite/
    helpers.go          — shared SQL helpers (ts, parseTime, nullString, etc.)
    graph_repo.go       — GraphRepo implementation
    node_repo.go        — NodeRepo implementation
    edge_repo.go        — edge persistence (part of GraphRepo, separate file for clarity)
    repos_test.go       — round-trip tests for all repos

internal/persistence/migrations/
  0091_v228_orchestration_graphs.up.sql
  0091_v228_orchestration_graphs.down.sql
```

---

### Task 1: Domain Types and Node Status Machine

**Files:**
- Create: `internal/projectmanager/orchestration/types.go`
- Create: `internal/projectmanager/orchestration/node.go`
- Test: `internal/projectmanager/orchestration/node_test.go`

**Interfaces:**
- Consumes: nothing (foundational task)
- Produces:
  - `type GraphID string`
  - `type NodeID string`
  - `type NodeCategory string` — `NodeCategoryBusiness`, `NodeCategoryControl`
  - `type ControlKind string` — `ControlKindStart`, `ControlKindEnd`, `ControlKindCondition`
  - `type NodeStatus string` — `NodeOpen`, `NodeRunning`, `NodeCompleted`, `NodeReopen`, `NodeDiscarded`
  - `func (s NodeStatus) CanTransitionTo(to NodeStatus) bool`
  - `type ActionLog struct { OccurredAt time.Time; Action string; Detail string }`
  - `func NewNode(in NewNodeInput) (*Node, error)` — constructs with validation
  - `func RehydrateNode(in RehydrateNodeInput) (*Node, error)` — persistence round-trip
  - `func (n *Node) Start(at time.Time) error`
  - `func (n *Node) Complete(outcome string, at time.Time) error`
  - `func (n *Node) Reopen(reason string, at time.Time) error`
  - `func (n *Node) Discard(at time.Time) error`
  - Getters: `ID()`, `GraphID()`, `Category()`, `ControlKind()`, `Title()`, `Status()`, `Outcome()`, `Metadata()`, `ActionLogs()`, `CreatedAt()`, `UpdatedAt()`, `Version()`

- [ ] **Step 1: Write the failing test for node status transitions**

Create `internal/projectmanager/orchestration/node_test.go`:

```go
package orchestration

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)

func TestNodeStatus_Transitions(t *testing.T) {
	allowed := []struct{ from, to NodeStatus }{
		{NodeOpen, NodeRunning},
		{NodeRunning, NodeCompleted},
		{NodeCompleted, NodeReopen},
		{NodeReopen, NodeRunning},
		{NodeOpen, NodeDiscarded},
		{NodeRunning, NodeDiscarded},
	}
	for _, tt := range allowed {
		if !tt.from.CanTransitionTo(tt.to) {
			t.Errorf("%s -> %s should be allowed", tt.from, tt.to)
		}
	}

	denied := []struct{ from, to NodeStatus }{
		{NodeOpen, NodeCompleted},
		{NodeCompleted, NodeRunning},
		{NodeDiscarded, NodeOpen},
		{NodeDiscarded, NodeRunning},
		{NodeReopen, NodeCompleted},
	}
	for _, tt := range denied {
		if tt.from.CanTransitionTo(tt.to) {
			t.Errorf("%s -> %s should be denied", tt.from, tt.to)
		}
	}
}

func TestNewNode_BusinessNode(t *testing.T) {
	n, err := NewNode(NewNodeInput{
		ID:       "n1",
		GraphID:  "g1",
		Category: NodeCategoryBusiness,
		Title:    "Dev task",
		Metadata: map[string]any{"branch": "feature-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n.ID() != "n1" || n.GraphID() != "g1" || n.Category() != NodeCategoryBusiness {
		t.Fatalf("unexpected fields: id=%s graph=%s cat=%s", n.ID(), n.GraphID(), n.Category())
	}
	if n.Status() != NodeOpen {
		t.Fatalf("status = %s, want open", n.Status())
	}
	if n.ControlKind() != "" {
		t.Fatalf("business node should have empty controlKind, got %s", n.ControlKind())
	}
	if n.Metadata()["branch"] != "feature-1" {
		t.Fatal("metadata not preserved")
	}
}

func TestNewNode_ControlNode(t *testing.T) {
	n, err := NewNode(NewNodeInput{
		ID:          "n2",
		GraphID:     "g1",
		Category:    NodeCategoryControl,
		ControlKind: ControlKindCondition,
		Title:       "Review gate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if n.ControlKind() != ControlKindCondition {
		t.Fatalf("controlKind = %s, want condition", n.ControlKind())
	}
}

func TestNewNode_Validation(t *testing.T) {
	// missing id
	if _, err := NewNode(NewNodeInput{GraphID: "g1", Category: NodeCategoryBusiness, Title: "x"}); err == nil {
		t.Fatal("expected error for missing id")
	}
	// missing graphID
	if _, err := NewNode(NewNodeInput{ID: "n1", Category: NodeCategoryBusiness, Title: "x"}); err == nil {
		t.Fatal("expected error for missing graphID")
	}
	// missing title
	if _, err := NewNode(NewNodeInput{ID: "n1", GraphID: "g1", Category: NodeCategoryBusiness}); err == nil {
		t.Fatal("expected error for missing title")
	}
	// invalid category
	if _, err := NewNode(NewNodeInput{ID: "n1", GraphID: "g1", Category: "bad", Title: "x"}); err == nil {
		t.Fatal("expected error for invalid category")
	}
	// control node without controlKind
	if _, err := NewNode(NewNodeInput{ID: "n1", GraphID: "g1", Category: NodeCategoryControl, Title: "x"}); err == nil {
		t.Fatal("expected error for control node without controlKind")
	}
}

func TestNode_Start_Complete_Reopen(t *testing.T) {
	n, _ := NewNode(NewNodeInput{ID: "n1", GraphID: "g1", Category: NodeCategoryBusiness, Title: "x"})

	if err := n.Start(t0); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if n.Status() != NodeRunning {
		t.Fatalf("status = %s, want running", n.Status())
	}

	if err := n.Complete("success", t0.Add(time.Hour)); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if n.Status() != NodeCompleted || n.Outcome() != "success" {
		t.Fatalf("status=%s outcome=%s, want completed/success", n.Status(), n.Outcome())
	}

	if err := n.Reopen("condition failed", t0.Add(2*time.Hour)); err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	if n.Status() != NodeReopen {
		t.Fatalf("status = %s, want reopen", n.Status())
	}
	if n.Outcome() != "" {
		t.Fatalf("outcome should be cleared on reopen, got %s", n.Outcome())
	}
	if len(n.ActionLogs()) != 3 {
		t.Fatalf("action logs = %d, want 3", len(n.ActionLogs()))
	}

	// reopen -> running again
	if err := n.Start(t0.Add(3 * time.Hour)); err != nil {
		t.Fatalf("Start after reopen: %v", err)
	}
	if n.Status() != NodeRunning {
		t.Fatalf("status = %s, want running", n.Status())
	}
}

func TestNode_Discard(t *testing.T) {
	n, _ := NewNode(NewNodeInput{ID: "n1", GraphID: "g1", Category: NodeCategoryBusiness, Title: "x"})
	if err := n.Discard(t0); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if n.Status() != NodeDiscarded {
		t.Fatalf("status = %s, want discarded", n.Status())
	}
	// discarded is terminal
	if err := n.Start(t0); err == nil {
		t.Fatal("expected error starting discarded node")
	}
}

func TestNode_IllegalTransition(t *testing.T) {
	n, _ := NewNode(NewNodeInput{ID: "n1", GraphID: "g1", Category: NodeCategoryBusiness, Title: "x"})
	// open -> completed is illegal
	if err := n.Complete("ok", t0); err == nil {
		t.Fatal("expected error for open -> completed")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/projectmanager/orchestration/... -v -run TestNode`
Expected: compilation error — package and types don't exist

- [ ] **Step 3: Implement types.go**

Create `internal/projectmanager/orchestration/types.go`:

```go
package orchestration

import "errors"

// Typed identifiers.
type (
	GraphID string
	NodeID  string
)

// NodeCategory distinguishes business nodes from control nodes.
type NodeCategory string

const (
	NodeCategoryBusiness NodeCategory = "business"
	NodeCategoryControl  NodeCategory = "control"
)

func (c NodeCategory) IsValid() bool {
	switch c {
	case NodeCategoryBusiness, NodeCategoryControl:
		return true
	}
	return false
}

// ControlKind sub-classifies control nodes.
type ControlKind string

const (
	ControlKindStart     ControlKind = "start"
	ControlKindEnd       ControlKind = "end"
	ControlKindCondition ControlKind = "condition"
)

func (k ControlKind) IsValid() bool {
	switch k {
	case ControlKindStart, ControlKindEnd, ControlKindCondition:
		return true
	}
	return false
}

// NodeStatus enum + state machine.
type NodeStatus string

const (
	NodeOpen      NodeStatus = "open"
	NodeRunning   NodeStatus = "running"
	NodeCompleted NodeStatus = "completed"
	NodeReopen    NodeStatus = "reopen"
	NodeDiscarded NodeStatus = "discarded"
)

func (s NodeStatus) IsValid() bool {
	switch s {
	case NodeOpen, NodeRunning, NodeCompleted, NodeReopen, NodeDiscarded:
		return true
	}
	return false
}

var nodeTransitions = map[NodeStatus][]NodeStatus{
	NodeOpen:      {NodeRunning, NodeDiscarded},
	NodeRunning:   {NodeCompleted, NodeDiscarded},
	NodeCompleted: {NodeReopen},
	NodeReopen:    {NodeRunning, NodeDiscarded},
	NodeDiscarded: {}, // terminal
}

func (s NodeStatus) CanTransitionTo(to NodeStatus) bool {
	for _, n := range nodeTransitions[s] {
		if n == to {
			return true
		}
	}
	return false
}

func (s NodeStatus) IsTerminal() bool {
	return s == NodeDiscarded
}

// GraphStatus enum.
type GraphStatus string

const (
	GraphDraft    GraphStatus = "draft"
	GraphRunning  GraphStatus = "running"
	GraphDone     GraphStatus = "done"
	GraphArchived GraphStatus = "archived"
)

func (s GraphStatus) IsValid() bool {
	switch s {
	case GraphDraft, GraphRunning, GraphDone, GraphArchived:
		return true
	}
	return false
}

var graphTransitions = map[GraphStatus][]GraphStatus{
	GraphDraft:    {GraphRunning, GraphArchived},
	GraphRunning:  {GraphDraft, GraphDone},
	GraphDone:     {GraphArchived},
	GraphArchived: {}, // terminal
}

func (s GraphStatus) CanTransitionTo(to GraphStatus) bool {
	for _, n := range graphTransitions[s] {
		if n == to {
			return true
		}
	}
	return false
}

// Sentinel errors.
var (
	ErrGraphNotFound        = errors.New("orchestration: graph not found")
	ErrGraphExists          = errors.New("orchestration: graph already exists")
	ErrNodeNotFound         = errors.New("orchestration: node not found")
	ErrNodeExists           = errors.New("orchestration: node already exists")
	ErrEdgeExists           = errors.New("orchestration: edge already exists")
	ErrIllegalTransition    = errors.New("orchestration: illegal status transition")
	ErrSelfEdge             = errors.New("orchestration: self-referencing edge not allowed")
	ErrCycleDetected        = errors.New("orchestration: adding edge would create a cycle")
	ErrNodeNotRemovable     = errors.New("orchestration: node in running/completed status cannot be removed")
	ErrMissingRequiredField = errors.New("orchestration: missing required field")
	ErrInvalidCategory      = errors.New("orchestration: invalid node category")
	ErrMissingControlKind   = errors.New("orchestration: control node requires a controlKind")
	ErrInvalidControlKind   = errors.New("orchestration: invalid controlKind")
)
```

- [ ] **Step 4: Implement node.go**

Create `internal/projectmanager/orchestration/node.go`:

```go
package orchestration

import (
	"encoding/json"
	"strings"
	"time"
)

// ActionLog records a node lifecycle event.
type ActionLog struct {
	OccurredAt time.Time
	Action     string
	Detail     string
}

// Node is a business or control node within a Graph.
type Node struct {
	id          NodeID
	graphID     GraphID
	category    NodeCategory
	controlKind ControlKind
	title       string
	status      NodeStatus
	outcome     string
	metadata    map[string]any
	actionLogs  []ActionLog
	createdAt   time.Time
	updatedAt   time.Time
	version     int
}

// NewNodeInput captures constructor args.
type NewNodeInput struct {
	ID          NodeID
	GraphID     GraphID
	Category    NodeCategory
	ControlKind ControlKind // required for control nodes, empty for business
	Title       string
	Metadata    map[string]any
	CreatedAt   time.Time // zero → time.Now()
}

func NewNode(in NewNodeInput) (*Node, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, ErrMissingRequiredField
	}
	if strings.TrimSpace(string(in.GraphID)) == "" {
		return nil, ErrMissingRequiredField
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, ErrMissingRequiredField
	}
	if !in.Category.IsValid() {
		return nil, ErrInvalidCategory
	}
	if in.Category == NodeCategoryControl {
		if in.ControlKind == "" {
			return nil, ErrMissingControlKind
		}
		if !in.ControlKind.IsValid() {
			return nil, ErrInvalidControlKind
		}
	}
	at := in.CreatedAt
	if at.IsZero() {
		at = time.Now()
	}
	meta := in.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	return &Node{
		id:          in.ID,
		graphID:     in.GraphID,
		category:    in.Category,
		controlKind: in.ControlKind,
		title:       in.Title,
		status:      NodeOpen,
		metadata:    meta,
		actionLogs:  nil,
		createdAt:   at.UTC(),
		updatedAt:   at.UTC(),
		version:     1,
	}, nil
}

// RehydrateNodeInput for persistence round-trip.
type RehydrateNodeInput struct {
	ID          NodeID
	GraphID     GraphID
	Category    NodeCategory
	ControlKind ControlKind
	Title       string
	Status      NodeStatus
	Outcome     string
	Metadata    map[string]any
	ActionLogs  []ActionLog
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int
}

func RehydrateNode(in RehydrateNodeInput) (*Node, error) {
	if !in.Status.IsValid() {
		return nil, ErrIllegalTransition
	}
	if in.Version < 1 {
		return nil, ErrMissingRequiredField
	}
	meta := in.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	return &Node{
		id:          in.ID,
		graphID:     in.GraphID,
		category:    in.Category,
		controlKind: in.ControlKind,
		title:       in.Title,
		status:      in.Status,
		outcome:     in.Outcome,
		metadata:    meta,
		actionLogs:  in.ActionLogs,
		createdAt:   in.CreatedAt.UTC(),
		updatedAt:   in.UpdatedAt.UTC(),
		version:     in.Version,
	}, nil
}

// Getters.
func (n *Node) ID() NodeID              { return n.id }
func (n *Node) GraphID() GraphID        { return n.graphID }
func (n *Node) Category() NodeCategory  { return n.category }
func (n *Node) ControlKind() ControlKind { return n.controlKind }
func (n *Node) Title() string           { return n.title }
func (n *Node) Status() NodeStatus      { return n.status }
func (n *Node) Outcome() string         { return n.outcome }
func (n *Node) CreatedAt() time.Time    { return n.createdAt }
func (n *Node) UpdatedAt() time.Time    { return n.updatedAt }
func (n *Node) Version() int            { return n.version }

func (n *Node) Metadata() map[string]any {
	cp := make(map[string]any, len(n.metadata))
	for k, v := range n.metadata {
		cp[k] = v
	}
	return cp
}

func (n *Node) ActionLogs() []ActionLog {
	cp := make([]ActionLog, len(n.actionLogs))
	copy(cp, n.actionLogs)
	return cp
}

// MetadataJSON returns metadata as a JSON string for persistence.
func (n *Node) MetadataJSON() string {
	if len(n.metadata) == 0 {
		return "{}"
	}
	b, err := json.Marshal(n.metadata)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// ActionLogsJSON returns action logs as a JSON string for persistence.
func (n *Node) ActionLogsJSON() string {
	if len(n.actionLogs) == 0 {
		return "[]"
	}
	b, err := json.Marshal(n.actionLogs)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// Mutations.

func (n *Node) Start(at time.Time) error {
	return n.transition(NodeRunning, "started", "", at)
}

func (n *Node) Complete(outcome string, at time.Time) error {
	if err := n.transition(NodeCompleted, "completed", outcome, at); err != nil {
		return err
	}
	n.outcome = outcome
	return nil
}

func (n *Node) Reopen(reason string, at time.Time) error {
	if err := n.transition(NodeReopen, "reopened", reason, at); err != nil {
		return err
	}
	n.outcome = "" // clear outcome on reopen
	return nil
}

func (n *Node) Discard(at time.Time) error {
	return n.transition(NodeDiscarded, "discarded", "", at)
}

// SetTitle updates the node title.
func (n *Node) SetTitle(title string, at time.Time) error {
	if strings.TrimSpace(title) == "" {
		return ErrMissingRequiredField
	}
	n.title = title
	n.touch(at)
	return nil
}

// SetMetadata replaces the metadata map.
func (n *Node) SetMetadata(meta map[string]any, at time.Time) {
	if meta == nil {
		meta = map[string]any{}
	}
	n.metadata = meta
	n.touch(at)
}

func (n *Node) transition(to NodeStatus, action, detail string, at time.Time) error {
	if !n.status.CanTransitionTo(to) {
		return ErrIllegalTransition
	}
	n.status = to
	n.appendLog(action, detail, at)
	n.touch(at)
	return nil
}

func (n *Node) appendLog(action, detail string, at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	n.actionLogs = append(n.actionLogs, ActionLog{
		OccurredAt: at.UTC(),
		Action:     action,
		Detail:     detail,
	})
}

func (n *Node) touch(at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	n.updatedAt = at.UTC()
	n.version++
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/projectmanager/orchestration/... -v -run TestNode`
Expected: all tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/projectmanager/orchestration/types.go internal/projectmanager/orchestration/node.go internal/projectmanager/orchestration/node_test.go
git commit -m "feat(orchestration): add node entity with status machine and domain types"
```

---

### Task 2: Edge Value Object and Cycle Detection

**Files:**
- Create: `internal/projectmanager/orchestration/edge.go`
- Test: `internal/projectmanager/orchestration/edge_test.go`

**Interfaces:**
- Consumes: `GraphID`, `NodeID`, `ErrSelfEdge`, `ErrCycleDetected`, `ErrEdgeExists` from Task 1
- Produces:
  - `type Edge struct { FromNodeID, ToNodeID NodeID }`
  - `func ValidateNoCycle(edges []Edge, add Edge) error` — rejects self-edge + cycle
  - `func ReopenChain(edges []Edge, from, to NodeID) []NodeID` — computes nodes on path from `to` to `from` (reverse traversal for condition failure reopen)

- [ ] **Step 1: Write the failing tests**

Create `internal/projectmanager/orchestration/edge_test.go`:

```go
package orchestration

import "testing"

func TestValidateNoCycle_SelfEdge(t *testing.T) {
	err := ValidateNoCycle(nil, Edge{FromNodeID: "a", ToNodeID: "a"})
	if err != ErrSelfEdge {
		t.Fatalf("want ErrSelfEdge, got %v", err)
	}
}

func TestValidateNoCycle_SimpleChain(t *testing.T) {
	// a -> b -> c — adding c -> a creates a cycle
	existing := []Edge{
		{FromNodeID: "a", ToNodeID: "b"},
		{FromNodeID: "b", ToNodeID: "c"},
	}
	if err := ValidateNoCycle(existing, Edge{FromNodeID: "c", ToNodeID: "a"}); err != ErrCycleDetected {
		t.Fatalf("want ErrCycleDetected, got %v", err)
	}
}

func TestValidateNoCycle_NoCycle(t *testing.T) {
	existing := []Edge{
		{FromNodeID: "a", ToNodeID: "b"},
		{FromNodeID: "b", ToNodeID: "c"},
	}
	// a -> c is fine (shortcut, no cycle)
	if err := ValidateNoCycle(existing, Edge{FromNodeID: "a", ToNodeID: "c"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateNoCycle_Diamond(t *testing.T) {
	// a -> b, a -> c, b -> d, c -> d — adding d -> a creates cycle
	existing := []Edge{
		{FromNodeID: "a", ToNodeID: "b"},
		{FromNodeID: "a", ToNodeID: "c"},
		{FromNodeID: "b", ToNodeID: "d"},
		{FromNodeID: "c", ToNodeID: "d"},
	}
	if err := ValidateNoCycle(existing, Edge{FromNodeID: "d", ToNodeID: "a"}); err != ErrCycleDetected {
		t.Fatalf("want ErrCycleDetected, got %v", err)
	}
	// d -> e is fine
	if err := ValidateNoCycle(existing, Edge{FromNodeID: "d", ToNodeID: "e"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateNoCycle_EmptyGraph(t *testing.T) {
	if err := ValidateNoCycle(nil, Edge{FromNodeID: "a", ToNodeID: "b"}); err != nil {
		t.Fatalf("unexpected error on empty graph: %v", err)
	}
}

func TestReopenChain_LinearPath(t *testing.T) {
	// a -> b -> c -> d. Reopen from d back to target a: should return [c, b, a]
	edges := []Edge{
		{FromNodeID: "a", ToNodeID: "b"},
		{FromNodeID: "b", ToNodeID: "c"},
		{FromNodeID: "c", ToNodeID: "d"},
	}
	chain := ReopenChain(edges, "d", "a")
	expected := []NodeID{"c", "b", "a"}
	if len(chain) != len(expected) {
		t.Fatalf("chain = %v, want %v", chain, expected)
	}
	for i, id := range expected {
		if chain[i] != id {
			t.Fatalf("chain[%d] = %s, want %s", i, chain[i], id)
		}
	}
}

func TestReopenChain_BranchingPath(t *testing.T) {
	// a -> b -> d, a -> c -> d. Reopen from d to a: should include b, c, a (order may vary but all present)
	edges := []Edge{
		{FromNodeID: "a", ToNodeID: "b"},
		{FromNodeID: "a", ToNodeID: "c"},
		{FromNodeID: "b", ToNodeID: "d"},
		{FromNodeID: "c", ToNodeID: "d"},
	}
	chain := ReopenChain(edges, "d", "a")
	found := map[NodeID]bool{}
	for _, id := range chain {
		found[id] = true
	}
	for _, want := range []NodeID{"a", "b", "c"} {
		if !found[want] {
			t.Fatalf("chain %v missing %s", chain, want)
		}
	}
	if found["d"] {
		t.Fatal("chain should not include the source node d")
	}
}

func TestReopenChain_NoPath(t *testing.T) {
	// a -> b, c -> d. Reopen from d to a: no path, empty chain
	edges := []Edge{
		{FromNodeID: "a", ToNodeID: "b"},
		{FromNodeID: "c", ToNodeID: "d"},
	}
	chain := ReopenChain(edges, "d", "a")
	if len(chain) != 0 {
		t.Fatalf("chain = %v, want empty", chain)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/projectmanager/orchestration/... -v -run "TestValidateNoCycle|TestReopenChain"`
Expected: compilation error — `Edge`, `ValidateNoCycle`, `ReopenChain` not defined

- [ ] **Step 3: Implement edge.go**

Create `internal/projectmanager/orchestration/edge.go`:

```go
package orchestration

// Edge is a directed dependency: ToNodeID depends on FromNodeID completing first.
type Edge struct {
	FromNodeID NodeID
	ToNodeID   NodeID
}

// ValidateNoCycle checks that adding `add` to `existing` edges does not create
// a cycle. Also rejects self-edges. Uses iterative DFS with three-color marking.
func ValidateNoCycle(existing []Edge, add Edge) error {
	if add.FromNodeID == add.ToNodeID {
		return ErrSelfEdge
	}

	// Build adjacency list including the new edge.
	adj := map[NodeID][]NodeID{}
	for _, e := range existing {
		adj[e.FromNodeID] = append(adj[e.FromNodeID], e.ToNodeID)
	}
	adj[add.FromNodeID] = append(adj[add.FromNodeID], add.ToNodeID)

	// Collect all nodes.
	nodes := map[NodeID]bool{}
	for _, e := range existing {
		nodes[e.FromNodeID] = true
		nodes[e.ToNodeID] = true
	}
	nodes[add.FromNodeID] = true
	nodes[add.ToNodeID] = true

	// Three-color DFS: 0=white, 1=gray(in-stack), 2=black(done).
	color := map[NodeID]int{}
	for n := range nodes {
		color[n] = 0
	}

	for n := range nodes {
		if color[n] != 0 {
			continue
		}
		// Iterative DFS using an explicit stack.
		type frame struct {
			node NodeID
			idx  int // next child index to visit
		}
		stack := []frame{{node: n, idx: 0}}
		color[n] = 1

		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			children := adj[top.node]
			if top.idx < len(children) {
				child := children[top.idx]
				top.idx++
				if color[child] == 1 {
					return ErrCycleDetected
				}
				if color[child] == 0 {
					color[child] = 1
					stack = append(stack, frame{node: child, idx: 0})
				}
			} else {
				color[top.node] = 2
				stack = stack[:len(stack)-1]
			}
		}
	}
	return nil
}

// ReopenChain computes all nodes on any path from `target` to `from` (exclusive
// of `from` itself) by reverse-traversing the edge graph. Used by condition
// failure to determine which nodes to reopen. Returns nodes in reverse
// topological order (closest to `from` first, `target` last).
func ReopenChain(edges []Edge, from, target NodeID) []NodeID {
	// Build reverse adjacency: for each edge a->b, reverse[b] = append(reverse[b], a)
	reverse := map[NodeID][]NodeID{}
	for _, e := range edges {
		reverse[e.ToNodeID] = append(reverse[e.ToNodeID], e.FromNodeID)
	}

	// BFS backwards from `from` to find all nodes that can reach `target`.
	visited := map[NodeID]bool{}
	queue := []NodeID{from}
	visited[from] = true
	reachable := map[NodeID]bool{}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, pred := range reverse[cur] {
			if visited[pred] {
				continue
			}
			visited[pred] = true
			reachable[pred] = true
			queue = append(queue, pred)
		}
	}

	if !reachable[target] {
		return nil // no path from target to from
	}

	// Now forward BFS from target, collecting only nodes that are also reachable.
	// This gives us exactly the nodes on paths between target and from.
	forward := map[NodeID][]NodeID{}
	for _, e := range edges {
		forward[e.FromNodeID] = append(forward[e.FromNodeID], e.ToNodeID)
	}

	onPath := map[NodeID]bool{}
	fwdQueue := []NodeID{target}
	fwdVisited := map[NodeID]bool{target: true}

	for len(fwdQueue) > 0 {
		cur := fwdQueue[0]
		fwdQueue = fwdQueue[1:]
		if cur == from {
			continue // don't go past `from`
		}
		onPath[cur] = true
		for _, succ := range forward[cur] {
			if fwdVisited[succ] {
				continue
			}
			if reachable[succ] || succ == from {
				fwdVisited[succ] = true
				fwdQueue = append(fwdQueue, succ)
			}
		}
	}

	// Build result: reverse topological order (closest to from first).
	// Simple approach: BFS backward from `from`, only include onPath nodes.
	var result []NodeID
	backQueue := []NodeID{from}
	backVisited := map[NodeID]bool{from: true}
	for len(backQueue) > 0 {
		cur := backQueue[0]
		backQueue = backQueue[1:]
		for _, pred := range reverse[cur] {
			if backVisited[pred] {
				continue
			}
			if onPath[pred] {
				backVisited[pred] = true
				result = append(result, pred)
				backQueue = append(backQueue, pred)
			}
		}
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/projectmanager/orchestration/... -v -run "TestValidateNoCycle|TestReopenChain"`
Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/projectmanager/orchestration/edge.go internal/projectmanager/orchestration/edge_test.go
git commit -m "feat(orchestration): add edge value object with cycle detection and reopen chain"
```

---

### Task 3: Graph Aggregate Root

**Files:**
- Create: `internal/projectmanager/orchestration/graph.go`
- Test: `internal/projectmanager/orchestration/graph_test.go`

**Interfaces:**
- Consumes: `GraphID`, `NodeID`, `GraphStatus`, `GraphDraft`, `GraphRunning`, `GraphDone`, `GraphArchived`, `Edge`, `Node`, `NewNodeInput`, `ValidateNoCycle`, `ErrIllegalTransition`, `ErrNodeNotRemovable` from Tasks 1-2
- Produces:
  - `func NewGraph(in NewGraphInput) (*Graph, error)` — creates graph with auto start+end nodes
  - `func RehydrateGraph(in RehydrateGraphInput) (*Graph, error)`
  - `func (g *Graph) Start(at time.Time) error` — draft -> running
  - `func (g *Graph) Finish(at time.Time) error` — running -> done
  - `func (g *Graph) Archive(at time.Time) error` — done/draft -> archived
  - `func (g *Graph) Revert(at time.Time) error` — running -> draft
  - `func (g *Graph) AddNode(in NewNodeInput) (*Node, error)`
  - `func (g *Graph) RemoveNode(nodeID NodeID) error`
  - `func (g *Graph) AddEdge(from, to NodeID) error`
  - `func (g *Graph) RemoveEdge(from, to NodeID) error`
  - `func (g *Graph) FindNode(id NodeID) *Node`
  - `func (g *Graph) Nodes() []*Node`
  - `func (g *Graph) Edges() []Edge`
  - `func (g *Graph) ReadyNodes() []*Node` — nodes whose upstream deps are all completed/discarded
  - `func (g *Graph) IsAutoDone() bool` — true when all end nodes' upstream chains are terminal
  - Getters: `ID()`, `PlanID()`, `Status()`, `CreatedAt()`, `UpdatedAt()`, `Version()`

- [ ] **Step 1: Write the failing tests**

Create `internal/projectmanager/orchestration/graph_test.go`:

```go
package orchestration

import (
	"testing"
	"time"
)

func TestNewGraph_AutoStartEnd(t *testing.T) {
	g, err := NewGraph(NewGraphInput{
		ID:        "g1",
		PlanID:    "plan-1",
		CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if g.Status() != GraphDraft {
		t.Fatalf("status = %s, want draft", g.Status())
	}
	nodes := g.Nodes()
	if len(nodes) != 2 {
		t.Fatalf("nodes = %d, want 2 (start + end)", len(nodes))
	}
	var hasStart, hasEnd bool
	for _, n := range nodes {
		if n.ControlKind() == ControlKindStart {
			hasStart = true
		}
		if n.ControlKind() == ControlKindEnd {
			hasEnd = true
		}
	}
	if !hasStart || !hasEnd {
		t.Fatal("missing start or end node")
	}
}

func TestGraph_AddNode_AddEdge(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	startID := g.StartNodeID()
	endID := g.EndNodeID()

	dev, err := g.AddNode(NewNodeInput{
		ID: "dev-1", GraphID: "g1", Category: NodeCategoryBusiness, Title: "Dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	if dev.Status() != NodeOpen {
		t.Fatalf("new node status = %s, want open", dev.Status())
	}

	// start -> dev -> end
	if err := g.AddEdge(startID, dev.ID()); err != nil {
		t.Fatal(err)
	}
	if err := g.AddEdge(dev.ID(), endID); err != nil {
		t.Fatal(err)
	}
	if len(g.Edges()) != 2 {
		t.Fatalf("edges = %d, want 2", len(g.Edges()))
	}
}

func TestGraph_AddEdge_CycleRejected(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	g.AddNode(NewNodeInput{ID: "a", GraphID: "g1", Category: NodeCategoryBusiness, Title: "A"})
	g.AddNode(NewNodeInput{ID: "b", GraphID: "g1", Category: NodeCategoryBusiness, Title: "B"})
	g.AddEdge("a", "b")
	if err := g.AddEdge("b", "a"); err != ErrCycleDetected {
		t.Fatalf("want ErrCycleDetected, got %v", err)
	}
}

func TestGraph_RemoveNode(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	g.AddNode(NewNodeInput{ID: "a", GraphID: "g1", Category: NodeCategoryBusiness, Title: "A"})

	if err := g.RemoveNode("a"); err != nil {
		t.Fatal(err)
	}
	if g.FindNode("a") != nil {
		t.Fatal("node should be removed")
	}
}

func TestGraph_RemoveNode_RunningBlocked(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	g.AddNode(NewNodeInput{ID: "a", GraphID: "g1", Category: NodeCategoryBusiness, Title: "A"})
	n := g.FindNode("a")
	n.Start(t0)

	if err := g.RemoveNode("a"); err != ErrNodeNotRemovable {
		t.Fatalf("want ErrNodeNotRemovable, got %v", err)
	}
}

func TestGraph_RemoveEdge(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	g.AddNode(NewNodeInput{ID: "a", GraphID: "g1", Category: NodeCategoryBusiness, Title: "A"})
	g.AddNode(NewNodeInput{ID: "b", GraphID: "g1", Category: NodeCategoryBusiness, Title: "B"})
	g.AddEdge("a", "b")

	if err := g.RemoveEdge("a", "b"); err != nil {
		t.Fatal(err)
	}
	if len(g.Edges()) != 0 {
		t.Fatalf("edges = %d, want 0", len(g.Edges()))
	}
}

func TestGraph_ReadyNodes(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	startID := g.StartNodeID()

	g.AddNode(NewNodeInput{ID: "a", GraphID: "g1", Category: NodeCategoryBusiness, Title: "A"})
	g.AddNode(NewNodeInput{ID: "b", GraphID: "g1", Category: NodeCategoryBusiness, Title: "B"})
	g.AddEdge(startID, "a")
	g.AddEdge("a", "b")

	// Mark start as completed (control node)
	g.FindNode(startID).Complete("", t0)

	ready := g.ReadyNodes()
	if len(ready) != 1 || ready[0].ID() != "a" {
		t.Fatalf("ready = %v, want [a]", nodeIDs(ready))
	}

	// Complete a -> b becomes ready
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("success", t0)
	ready = g.ReadyNodes()
	if len(ready) != 1 || ready[0].ID() != "b" {
		t.Fatalf("ready = %v, want [b]", nodeIDs(ready))
	}
}

func TestGraph_StatusTransitions(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})

	if err := g.Start(t0); err != nil {
		t.Fatal(err)
	}
	if g.Status() != GraphRunning {
		t.Fatalf("status = %s, want running", g.Status())
	}

	// running -> draft (revert)
	if err := g.Revert(t0); err != nil {
		t.Fatal(err)
	}
	if g.Status() != GraphDraft {
		t.Fatalf("status = %s, want draft", g.Status())
	}

	// draft -> archived
	if err := g.Archive(t0); err != nil {
		t.Fatal(err)
	}
	if g.Status() != GraphArchived {
		t.Fatalf("status = %s, want archived", g.Status())
	}
}

func TestGraph_Finish(t *testing.T) {
	g, _ := NewGraph(NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	g.Start(t0)
	if err := g.Finish(t0); err != nil {
		t.Fatal(err)
	}
	if g.Status() != GraphDone {
		t.Fatalf("status = %s, want done", g.Status())
	}
}

func nodeIDs(nodes []*Node) []NodeID {
	ids := make([]NodeID, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID()
	}
	return ids
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/projectmanager/orchestration/... -v -run TestGraph`
Expected: compilation error — `Graph`, `NewGraph`, etc. not defined

- [ ] **Step 3: Implement graph.go**

Create `internal/projectmanager/orchestration/graph.go`:

```go
package orchestration

import (
	"strings"
	"time"
)

// Graph is the aggregate root: a DAG of Nodes connected by Edges.
type Graph struct {
	id        GraphID
	planID    string
	status    GraphStatus
	nodes     map[NodeID]*Node
	edges     []Edge
	startNode NodeID
	endNode   NodeID
	createdAt time.Time
	updatedAt time.Time
	version   int
}

// NewGraphInput captures constructor args.
type NewGraphInput struct {
	ID          GraphID
	PlanID      string
	StartNodeID NodeID // optional; auto-generated if empty
	EndNodeID   NodeID // optional; auto-generated if empty
	CreatedAt   time.Time
}

func NewGraph(in NewGraphInput) (*Graph, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, ErrMissingRequiredField
	}
	at := in.CreatedAt
	if at.IsZero() {
		at = time.Now()
	}

	startID := in.StartNodeID
	if startID == "" {
		startID = NodeID(string(in.ID) + ":start")
	}
	endID := in.EndNodeID
	if endID == "" {
		endID = NodeID(string(in.ID) + ":end")
	}

	g := &Graph{
		id:        in.ID,
		planID:    in.PlanID,
		status:    GraphDraft,
		nodes:     map[NodeID]*Node{},
		edges:     nil,
		startNode: startID,
		endNode:   endID,
		createdAt: at.UTC(),
		updatedAt: at.UTC(),
		version:   1,
	}

	// Auto-create start + end control nodes.
	start, _ := NewNode(NewNodeInput{
		ID: startID, GraphID: in.ID, Category: NodeCategoryControl,
		ControlKind: ControlKindStart, Title: "Start", CreatedAt: at,
	})
	end, _ := NewNode(NewNodeInput{
		ID: endID, GraphID: in.ID, Category: NodeCategoryControl,
		ControlKind: ControlKindEnd, Title: "End", CreatedAt: at,
	})
	g.nodes[startID] = start
	g.nodes[endID] = end
	return g, nil
}

// RehydrateGraphInput for persistence round-trip.
type RehydrateGraphInput struct {
	ID        GraphID
	PlanID    string
	Status    GraphStatus
	StartNode NodeID
	EndNode   NodeID
	Nodes     []*Node
	Edges     []Edge
	CreatedAt time.Time
	UpdatedAt time.Time
	Version   int
}

func RehydrateGraph(in RehydrateGraphInput) (*Graph, error) {
	if !in.Status.IsValid() {
		return nil, ErrIllegalTransition
	}
	if in.Version < 1 {
		return nil, ErrMissingRequiredField
	}
	nodes := map[NodeID]*Node{}
	for _, n := range in.Nodes {
		nodes[n.ID()] = n
	}
	return &Graph{
		id:        in.ID,
		planID:    in.PlanID,
		status:    in.Status,
		nodes:     nodes,
		edges:     in.Edges,
		startNode: in.StartNode,
		endNode:   in.EndNode,
		createdAt: in.CreatedAt.UTC(),
		updatedAt: in.UpdatedAt.UTC(),
		version:   in.Version,
	}, nil
}

// Getters.
func (g *Graph) ID() GraphID        { return g.id }
func (g *Graph) PlanID() string     { return g.planID }
func (g *Graph) Status() GraphStatus { return g.status }
func (g *Graph) StartNodeID() NodeID { return g.startNode }
func (g *Graph) EndNodeID() NodeID   { return g.endNode }
func (g *Graph) CreatedAt() time.Time { return g.createdAt }
func (g *Graph) UpdatedAt() time.Time { return g.updatedAt }
func (g *Graph) Version() int        { return g.version }

func (g *Graph) FindNode(id NodeID) *Node { return g.nodes[id] }

func (g *Graph) Nodes() []*Node {
	out := make([]*Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, n)
	}
	return out
}

func (g *Graph) Edges() []Edge {
	cp := make([]Edge, len(g.edges))
	copy(cp, g.edges)
	return cp
}

// Status transitions.

func (g *Graph) Start(at time.Time) error  { return g.transition(GraphRunning, at) }
func (g *Graph) Finish(at time.Time) error { return g.transition(GraphDone, at) }
func (g *Graph) Archive(at time.Time) error { return g.transition(GraphArchived, at) }
func (g *Graph) Revert(at time.Time) error  { return g.transition(GraphDraft, at) }

func (g *Graph) transition(to GraphStatus, at time.Time) error {
	if !g.status.CanTransitionTo(to) {
		return ErrIllegalTransition
	}
	g.status = to
	g.touch(at)
	return nil
}

// Node operations.

func (g *Graph) AddNode(in NewNodeInput) (*Node, error) {
	in.GraphID = g.id
	if in.CreatedAt.IsZero() {
		in.CreatedAt = time.Now()
	}
	n, err := NewNode(in)
	if err != nil {
		return nil, err
	}
	if g.nodes[n.ID()] != nil {
		return nil, ErrNodeExists
	}
	g.nodes[n.ID()] = n
	g.touch(in.CreatedAt)
	return n, nil
}

func (g *Graph) RemoveNode(nodeID NodeID) error {
	n := g.nodes[nodeID]
	if n == nil {
		return ErrNodeNotFound
	}
	if n.Status() == NodeRunning || n.Status() == NodeCompleted {
		return ErrNodeNotRemovable
	}
	delete(g.nodes, nodeID)
	// Remove all edges referencing this node.
	filtered := g.edges[:0]
	for _, e := range g.edges {
		if e.FromNodeID != nodeID && e.ToNodeID != nodeID {
			filtered = append(filtered, e)
		}
	}
	g.edges = filtered
	g.touch(time.Now())
	return nil
}

// Edge operations.

func (g *Graph) AddEdge(from, to NodeID) error {
	if g.nodes[from] == nil || g.nodes[to] == nil {
		return ErrNodeNotFound
	}
	// Check for duplicate.
	for _, e := range g.edges {
		if e.FromNodeID == from && e.ToNodeID == to {
			return ErrEdgeExists
		}
	}
	newEdge := Edge{FromNodeID: from, ToNodeID: to}
	if err := ValidateNoCycle(g.edges, newEdge); err != nil {
		return err
	}
	g.edges = append(g.edges, newEdge)
	g.touch(time.Now())
	return nil
}

func (g *Graph) RemoveEdge(from, to NodeID) error {
	for i, e := range g.edges {
		if e.FromNodeID == from && e.ToNodeID == to {
			g.edges = append(g.edges[:i], g.edges[i+1:]...)
			g.touch(time.Now())
			return nil
		}
	}
	return nil // idempotent: removing non-existent edge is a no-op
}

// ReadyNodes returns business nodes in open/reopen status whose upstream
// dependencies are all completed or discarded.
func (g *Graph) ReadyNodes() []*Node {
	// Build set of upstream deps per node.
	upstream := map[NodeID][]NodeID{}
	for _, e := range g.edges {
		upstream[e.ToNodeID] = append(upstream[e.ToNodeID], e.FromNodeID)
	}

	var ready []*Node
	for _, n := range g.nodes {
		if n.Status() != NodeOpen && n.Status() != NodeReopen {
			continue
		}
		allDone := true
		for _, depID := range upstream[n.ID()] {
			dep := g.nodes[depID]
			if dep == nil {
				allDone = false
				break
			}
			if dep.Status() != NodeCompleted && dep.Status() != NodeDiscarded {
				allDone = false
				break
			}
		}
		if allDone {
			ready = append(ready, n)
		}
	}
	return ready
}

// IsAutoDone returns true when all end nodes' upstream chains are terminal
// (every business node is completed or discarded).
func (g *Graph) IsAutoDone() bool {
	for _, n := range g.nodes {
		if n.Category() == NodeCategoryControl {
			continue
		}
		if n.Status() != NodeCompleted && n.Status() != NodeDiscarded {
			return false
		}
	}
	return true
}

func (g *Graph) touch(at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	g.updatedAt = at.UTC()
	g.version++
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/projectmanager/orchestration/... -v -run TestGraph`
Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/projectmanager/orchestration/graph.go internal/projectmanager/orchestration/graph_test.go
git commit -m "feat(orchestration): add graph aggregate root with node/edge management"
```

---

### Task 4: Condition Node Evaluation and Failure Reopen

**Files:**
- Create: `internal/projectmanager/orchestration/condition.go`
- Test: `internal/projectmanager/orchestration/condition_test.go`

**Interfaces:**
- Consumes: `Node`, `Edge`, `NodeID`, `NodeCompleted`, `NodeDiscarded`, `NodeReopen`, `NodeCategoryControl`, `ControlKindCondition`, `ActionLog`, `ReopenChain` from Tasks 1-2; `Graph`, `Graph.FindNode()`, `Graph.Edges()` from Task 3
- Produces:
  - `type EvaluatorKind string` — `EvaluatorUpstream`, `EvaluatorHook`, `EvaluatorManual`
  - `type LogicKind string` — `LogicAnd`, `LogicOr`
  - `type MaxExceededAction string` — `MaxExceededForceSuccess`, `MaxExceededDiscard`
  - `type ConditionConfig struct { ... }` — parsed from node metadata
  - `func ParseConditionConfig(metadata map[string]any) (ConditionConfig, error)`
  - `func EvaluateUpstream(g *Graph, conditionNodeID NodeID, cfg ConditionConfig) (success bool, err error)`
  - `func ApplyConditionResult(g *Graph, conditionNodeID NodeID, cfg ConditionConfig, success bool, at time.Time) error` — routes success/failure, reopens chain on failure

- [ ] **Step 1: Write the failing tests**

Create `internal/projectmanager/orchestration/condition_test.go`:

```go
package orchestration

import (
	"testing"
	"time"
)

func TestParseConditionConfig(t *testing.T) {
	meta := map[string]any{
		"evaluator":       "upstream_outcome",
		"logic":           "and",
		"on_success":      []any{"node-3", "node-4"},
		"on_failure":      []any{"node-1"},
		"max_rounds":      float64(3),
		"on_max_exceeded": "discard",
	}
	cfg, err := ParseConditionConfig(meta)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Evaluator != EvaluatorUpstream {
		t.Fatalf("evaluator = %s, want upstream_outcome", cfg.Evaluator)
	}
	if cfg.Logic != LogicAnd {
		t.Fatalf("logic = %s, want and", cfg.Logic)
	}
	if len(cfg.OnSuccess) != 2 || cfg.OnSuccess[0] != "node-3" {
		t.Fatalf("on_success = %v", cfg.OnSuccess)
	}
	if len(cfg.OnFailure) != 1 || cfg.OnFailure[0] != "node-1" {
		t.Fatalf("on_failure = %v", cfg.OnFailure)
	}
	if cfg.MaxRounds != 3 {
		t.Fatalf("max_rounds = %d, want 3", cfg.MaxRounds)
	}
	if cfg.OnMaxExceeded != MaxExceededDiscard {
		t.Fatalf("on_max_exceeded = %s, want discard", cfg.OnMaxExceeded)
	}
}

func TestEvaluateUpstream_And_AllSuccess(t *testing.T) {
	g := buildConditionTestGraph(t)
	// Complete both upstream nodes with success
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("success", t0)
	g.FindNode("b").Start(t0)
	g.FindNode("b").Complete("success", t0)

	cfg := ConditionConfig{Evaluator: EvaluatorUpstream, Logic: LogicAnd}
	ok, err := EvaluateUpstream(g, "cond", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected success (all upstream success with AND)")
	}
}

func TestEvaluateUpstream_And_OneFailed(t *testing.T) {
	g := buildConditionTestGraph(t)
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("success", t0)
	g.FindNode("b").Start(t0)
	g.FindNode("b").Complete("failure", t0)

	cfg := ConditionConfig{Evaluator: EvaluatorUpstream, Logic: LogicAnd}
	ok, err := EvaluateUpstream(g, "cond", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected failure (one upstream failure with AND)")
	}
}

func TestEvaluateUpstream_Or_OneSuccess(t *testing.T) {
	g := buildConditionTestGraph(t)
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("failure", t0)
	g.FindNode("b").Start(t0)
	g.FindNode("b").Complete("success", t0)

	cfg := ConditionConfig{Evaluator: EvaluatorUpstream, Logic: LogicOr}
	ok, err := EvaluateUpstream(g, "cond", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected success (one upstream success with OR)")
	}
}

func TestApplyConditionResult_Success(t *testing.T) {
	g := buildConditionTestGraph(t)
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("success", t0)
	g.FindNode("b").Start(t0)
	g.FindNode("b").Complete("success", t0)

	cfg := ConditionConfig{
		OnSuccess: []NodeID{"downstream"},
	}
	// Add downstream node
	g.AddNode(NewNodeInput{ID: "downstream", Category: NodeCategoryBusiness, Title: "DS"})
	g.AddEdge("cond", "downstream")

	err := ApplyConditionResult(g, "cond", cfg, true, t0)
	if err != nil {
		t.Fatal(err)
	}
	// condition node itself should be completed
	if g.FindNode("cond").Status() != NodeCompleted {
		t.Fatalf("condition status = %s, want completed", g.FindNode("cond").Status())
	}
}

func TestApplyConditionResult_Failure_ReopensChain(t *testing.T) {
	g := buildConditionTestGraph(t)
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("success", t0)
	g.FindNode("b").Start(t0)
	g.FindNode("b").Complete("failure", t0)

	cfg := ConditionConfig{
		OnFailure: []NodeID{"a"},
	}
	err := ApplyConditionResult(g, "cond", cfg, false, t0)
	if err != nil {
		t.Fatal(err)
	}

	// a and b should be reopened (they are on the chain from cond back to a)
	if g.FindNode("a").Status() != NodeReopen {
		t.Fatalf("a status = %s, want reopen", g.FindNode("a").Status())
	}
	if g.FindNode("b").Status() != NodeReopen {
		t.Fatalf("b status = %s, want reopen", g.FindNode("b").Status())
	}
	// condition should be reopened too (so it can re-evaluate)
	if g.FindNode("cond").Status() != NodeReopen {
		t.Fatalf("cond status = %s, want reopen", g.FindNode("cond").Status())
	}
}

func TestApplyConditionResult_MaxRoundsExceeded_Discard(t *testing.T) {
	g := buildConditionTestGraph(t)
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("failure", t0)
	g.FindNode("b").Start(t0)
	g.FindNode("b").Complete("failure", t0)

	cfg := ConditionConfig{
		OnFailure:      []NodeID{"a"},
		MaxRounds:      1,
		OnMaxExceeded:  MaxExceededDiscard,
	}

	// First failure: round 1 -> reopen
	err := ApplyConditionResult(g, "cond", cfg, false, t0)
	if err != nil {
		t.Fatal(err)
	}
	// Now simulate round 2: re-complete and re-fail
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("failure", t0)
	g.FindNode("b").Start(t0)
	g.FindNode("b").Complete("failure", t0)
	g.FindNode("cond").Start(t0)
	g.FindNode("cond").Complete("", t0)
	g.FindNode("cond").Reopen("re-eval", t0)

	// Second failure: round exceeds max -> discard
	err = ApplyConditionResult(g, "cond", cfg, false, t0)
	if err != nil {
		t.Fatal(err)
	}
	if g.FindNode("cond").Status() != NodeDiscarded {
		t.Fatalf("cond status = %s, want discarded (max rounds exceeded)", g.FindNode("cond").Status())
	}
}

// buildConditionTestGraph creates: a -> cond, b -> cond (two upstream business nodes)
func buildConditionTestGraph(t *testing.T) *Graph {
	t.Helper()
	g, err := NewGraph(NewGraphInput{ID: "g1", PlanID: "p1", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	g.AddNode(NewNodeInput{ID: "a", Category: NodeCategoryBusiness, Title: "A"})
	g.AddNode(NewNodeInput{ID: "b", Category: NodeCategoryBusiness, Title: "B"})
	g.AddNode(NewNodeInput{
		ID: "cond", Category: NodeCategoryControl,
		ControlKind: ControlKindCondition, Title: "Check",
	})
	g.AddEdge("a", "cond")
	g.AddEdge("b", "cond")
	return g
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/projectmanager/orchestration/... -v -run "TestParseCondition|TestEvaluateUpstream|TestApplyCondition"`
Expected: compilation error — `ConditionConfig`, `ParseConditionConfig`, etc. not defined

- [ ] **Step 3: Implement condition.go**

Create `internal/projectmanager/orchestration/condition.go`:

```go
package orchestration

import (
	"errors"
	"fmt"
	"time"
)

// EvaluatorKind determines how a condition node computes its result.
type EvaluatorKind string

const (
	EvaluatorUpstream EvaluatorKind = "upstream_outcome"
	EvaluatorHook     EvaluatorKind = "external_hook"
	EvaluatorManual   EvaluatorKind = "manual"
)

// LogicKind for upstream_outcome aggregation.
type LogicKind string

const (
	LogicAnd LogicKind = "and"
	LogicOr  LogicKind = "or"
)

// MaxExceededAction determines what happens when max_rounds is exceeded.
type MaxExceededAction string

const (
	MaxExceededForceSuccess MaxExceededAction = "force_success"
	MaxExceededDiscard      MaxExceededAction = "discard"
)

// ConditionConfig holds the evaluation rules for a condition node.
type ConditionConfig struct {
	Evaluator      EvaluatorKind
	Logic          LogicKind         // for upstream_outcome
	HookURL        string            // for external_hook
	HookMethod     string            // for external_hook
	SuccessCondition string          // for external_hook
	OnSuccess      []NodeID
	OnFailure      []NodeID
	MaxRounds      int               // 0 = unlimited
	OnMaxExceeded  MaxExceededAction
}

// ParseConditionConfig extracts a ConditionConfig from a node's metadata map.
func ParseConditionConfig(metadata map[string]any) (ConditionConfig, error) {
	var cfg ConditionConfig

	if v, ok := metadata["evaluator"].(string); ok {
		cfg.Evaluator = EvaluatorKind(v)
	}
	if v, ok := metadata["logic"].(string); ok {
		cfg.Logic = LogicKind(v)
	}
	if v, ok := metadata["hook_url"].(string); ok {
		cfg.HookURL = v
	}
	if v, ok := metadata["hook_method"].(string); ok {
		cfg.HookMethod = v
	}
	if v, ok := metadata["success_condition"].(string); ok {
		cfg.SuccessCondition = v
	}
	if v, ok := metadata["on_success"].([]any); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				cfg.OnSuccess = append(cfg.OnSuccess, NodeID(s))
			}
		}
	}
	if v, ok := metadata["on_failure"].([]any); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				cfg.OnFailure = append(cfg.OnFailure, NodeID(s))
			}
		}
	}
	if v, ok := metadata["max_rounds"].(float64); ok {
		cfg.MaxRounds = int(v)
	}
	if v, ok := metadata["on_max_exceeded"].(string); ok {
		cfg.OnMaxExceeded = MaxExceededAction(v)
	}
	return cfg, nil
}

// EvaluateUpstream checks upstream business nodes' outcomes using AND/OR logic.
// Returns true if the condition passes.
func EvaluateUpstream(g *Graph, conditionNodeID NodeID, cfg ConditionConfig) (bool, error) {
	edges := g.Edges()
	// Find all direct upstream nodes of the condition.
	var upstreamIDs []NodeID
	for _, e := range edges {
		if e.ToNodeID == conditionNodeID {
			upstreamIDs = append(upstreamIDs, e.FromNodeID)
		}
	}
	if len(upstreamIDs) == 0 {
		return false, errors.New("orchestration: condition node has no upstream nodes")
	}

	logic := cfg.Logic
	if logic == "" {
		logic = LogicAnd
	}

	for _, id := range upstreamIDs {
		n := g.FindNode(id)
		if n == nil {
			return false, fmt.Errorf("orchestration: upstream node %s not found", id)
		}
		if n.Category() == NodeCategoryControl {
			continue // skip control nodes
		}
		isSuccess := n.Status() == NodeCompleted && n.Outcome() == "success"

		if logic == LogicAnd && !isSuccess {
			return false, nil
		}
		if logic == LogicOr && isSuccess {
			return true, nil
		}
	}

	if logic == LogicAnd {
		return true, nil // all passed
	}
	return false, nil // none passed (OR)
}

// ApplyConditionResult routes the condition's success/failure. On failure, it
// reopens the entire chain from the condition back to each on_failure target.
func ApplyConditionResult(g *Graph, conditionNodeID NodeID, cfg ConditionConfig, success bool, at time.Time) error {
	condNode := g.FindNode(conditionNodeID)
	if condNode == nil {
		return ErrNodeNotFound
	}

	if success {
		// Mark condition as completed with success outcome.
		if condNode.Status() == NodeOpen || condNode.Status() == NodeReopen {
			if err := condNode.Start(at); err != nil {
				return err
			}
		}
		if condNode.Status() == NodeRunning {
			return condNode.Complete("success", at)
		}
		return nil
	}

	// Failure path: check max rounds.
	round := countReopens(condNode) + 1
	if cfg.MaxRounds > 0 && round > cfg.MaxRounds {
		switch cfg.OnMaxExceeded {
		case MaxExceededForceSuccess:
			if condNode.Status() == NodeOpen || condNode.Status() == NodeReopen {
				if err := condNode.Start(at); err != nil {
					return err
				}
			}
			return condNode.Complete("force_success", at)
		case MaxExceededDiscard:
			if condNode.Status() == NodeCompleted {
				if err := condNode.Reopen("max exceeded", at); err != nil {
					return err
				}
			}
			if condNode.Status() == NodeOpen || condNode.Status() == NodeReopen {
				if err := condNode.Start(at); err != nil {
					return err
				}
			}
			return condNode.Discard(at)
		default:
			return condNode.Discard(at)
		}
	}

	// Reopen the chain for each on_failure target.
	edges := g.Edges()
	for _, targetID := range cfg.OnFailure {
		chain := ReopenChain(edges, conditionNodeID, targetID)
		for _, nid := range chain {
			n := g.FindNode(nid)
			if n == nil {
				continue
			}
			if n.Category() == NodeCategoryControl && n.ControlKind() != ControlKindCondition {
				continue // skip start/end nodes
			}
			if n.Status() == NodeCompleted {
				if err := n.Reopen(fmt.Sprintf("reactivated_by:%s,round:%d", conditionNodeID, round), at); err != nil {
					return err
				}
			}
		}
	}

	// Reopen the condition node itself so it can re-evaluate.
	if condNode.Status() == NodeCompleted {
		return condNode.Reopen(fmt.Sprintf("failure,round:%d", round), at)
	}
	if condNode.Status() == NodeOpen || condNode.Status() == NodeReopen {
		// Already in a re-evaluable state.
		return nil
	}
	return nil
}

// countReopens counts how many times a node has been reopened (from action logs).
func countReopens(n *Node) int {
	count := 0
	for _, log := range n.ActionLogs() {
		if log.Action == "reopened" {
			count++
		}
	}
	return count
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/projectmanager/orchestration/... -v -run "TestParseCondition|TestEvaluateUpstream|TestApplyCondition"`
Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/projectmanager/orchestration/condition.go internal/projectmanager/orchestration/condition_test.go
git commit -m "feat(orchestration): add condition node evaluation and failure reopen chain"
```

---

### Task 5: Repository Interfaces and SQLite Migration

**Files:**
- Create: `internal/projectmanager/orchestration/repository.go`
- Create: `internal/persistence/migrations/0091_v228_orchestration_graphs.up.sql`
- Create: `internal/persistence/migrations/0091_v228_orchestration_graphs.down.sql`

**Interfaces:**
- Consumes: `Graph`, `GraphID`, `Node`, `NodeID`, `Edge` from Tasks 1-3
- Produces:
  - `type GraphRepository interface { Save, Update, FindByID, FindByPlanID, Delete }`
  - `type NodeRepository interface { Save, Update, FindByID, ListByGraph, Delete }`
  - `type EdgeRepository interface { Save, Delete, ListByGraph }`
  - Migration SQL creating `pm_graphs`, `pm_graph_nodes`, `pm_graph_edges`, `pm_graph_node_action_logs`

- [ ] **Step 1: Create repository.go**

Create `internal/projectmanager/orchestration/repository.go`:

```go
package orchestration

import "context"

// GraphRepository persists Graph aggregates.
type GraphRepository interface {
	Save(ctx context.Context, g *Graph) error
	Update(ctx context.Context, g *Graph) error
	FindByID(ctx context.Context, id GraphID) (*Graph, error)
	FindByPlanID(ctx context.Context, planID string) (*Graph, error)
	Delete(ctx context.Context, id GraphID) error
}

// NodeRepository persists Node entities within a Graph.
type NodeRepository interface {
	Save(ctx context.Context, n *Node) error
	Update(ctx context.Context, n *Node) error
	FindByID(ctx context.Context, id NodeID) (*Node, error)
	ListByGraph(ctx context.Context, graphID GraphID) ([]*Node, error)
	Delete(ctx context.Context, id NodeID) error
}

// EdgeRepository persists edges within a Graph.
type EdgeRepository interface {
	Save(ctx context.Context, graphID GraphID, e Edge) error
	Delete(ctx context.Context, graphID GraphID, from, to NodeID) error
	ListByGraph(ctx context.Context, graphID GraphID) ([]Edge, error)
}
```

- [ ] **Step 2: Create up migration**

Create `internal/persistence/migrations/0091_v228_orchestration_graphs.up.sql`:

```sql
-- 0091_v228_orchestration_graphs.up.sql — orchestration engine (design spec 2026-07-02)
-- Generic DAG engine: Graph aggregate + Node entity + Edge + ActionLog.

CREATE TABLE IF NOT EXISTS pm_graphs (
    id         TEXT PRIMARY KEY,
    plan_id    TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'draft',
    start_node TEXT NOT NULL,
    end_node   TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_pm_graphs_plan_id ON pm_graphs(plan_id);

CREATE TABLE IF NOT EXISTS pm_graph_nodes (
    id           TEXT PRIMARY KEY,
    graph_id     TEXT NOT NULL REFERENCES pm_graphs(id) ON DELETE CASCADE,
    category     TEXT NOT NULL,
    control_kind TEXT NOT NULL DEFAULT '',
    title        TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'open',
    outcome      TEXT NOT NULL DEFAULT '',
    metadata     TEXT NOT NULL DEFAULT '{}',
    action_logs  TEXT NOT NULL DEFAULT '[]',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    version      INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_pm_graph_nodes_graph_id ON pm_graph_nodes(graph_id);
CREATE INDEX IF NOT EXISTS idx_pm_graph_nodes_status ON pm_graph_nodes(graph_id, status);

CREATE TABLE IF NOT EXISTS pm_graph_edges (
    graph_id      TEXT NOT NULL REFERENCES pm_graphs(id) ON DELETE CASCADE,
    from_node_id  TEXT NOT NULL REFERENCES pm_graph_nodes(id) ON DELETE CASCADE,
    to_node_id    TEXT NOT NULL REFERENCES pm_graph_nodes(id) ON DELETE CASCADE,
    PRIMARY KEY (graph_id, from_node_id, to_node_id)
);
```

- [ ] **Step 3: Create down migration**

Create `internal/persistence/migrations/0091_v228_orchestration_graphs.down.sql`:

```sql
-- 0091_v228_orchestration_graphs.down.sql
DROP TABLE IF EXISTS pm_graph_edges;
DROP TABLE IF EXISTS pm_graph_nodes;
DROP TABLE IF EXISTS pm_graphs;
```

- [ ] **Step 4: Verify migration applies**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/persistence/... -v -run TestMigrat -count=1`
Expected: migrations run without error (the existing migration test should pick up the new file)

- [ ] **Step 5: Commit**

```bash
git add internal/projectmanager/orchestration/repository.go internal/persistence/migrations/0091_v228_orchestration_graphs.up.sql internal/persistence/migrations/0091_v228_orchestration_graphs.down.sql
git commit -m "feat(orchestration): add repository interfaces and database migration"
```

---

### Task 6: SQLite Repository Implementations

**Files:**
- Create: `internal/projectmanager/orchestration/sqlite/helpers.go`
- Create: `internal/projectmanager/orchestration/sqlite/graph_repo.go`
- Create: `internal/projectmanager/orchestration/sqlite/node_repo.go`
- Create: `internal/projectmanager/orchestration/sqlite/edge_repo.go`
- Test: `internal/projectmanager/orchestration/sqlite/repos_test.go`

**Interfaces:**
- Consumes: `orchestration.GraphRepository`, `orchestration.NodeRepository`, `orchestration.EdgeRepository` from Task 5; `persistence.ExecutorFromCtx`, `persistence.MemoryDSN`, `persistence.NewMigrator` from existing infra
- Produces:
  - `func NewGraphRepo(db *sql.DB) *GraphRepo` implementing `orchestration.GraphRepository`
  - `func NewNodeRepo(db *sql.DB) *NodeRepo` implementing `orchestration.NodeRepository`
  - `func NewEdgeRepo(db *sql.DB) *EdgeRepo` implementing `orchestration.EdgeRepository`

- [ ] **Step 1: Write the failing round-trip tests**

Create `internal/projectmanager/orchestration/sqlite/repos_test.go`:

```go
package sqlite

import (
	"context"
	"testing"
	"time"

	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
	"github.com/oopslink/agent-center/internal/persistence"
)

var t0 = time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)

func setup(t *testing.T) (context.Context, *GraphRepo, *NodeRepo, *EdgeRepo) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return context.Background(), NewGraphRepo(db), NewNodeRepo(db), NewEdgeRepo(db)
}

func TestGraphRepo_RoundTrip(t *testing.T) {
	ctx, gr, _, _ := setup(t)

	g, _ := orch.NewGraph(orch.NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	if err := gr.Save(ctx, g); err != nil {
		t.Fatal(err)
	}

	// Duplicate
	if err := gr.Save(ctx, g); err != orch.ErrGraphExists {
		t.Fatalf("dup save: want ErrGraphExists, got %v", err)
	}

	// FindByID
	got, err := gr.FindByID(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanID() != "plan-1" || got.Status() != orch.GraphDraft {
		t.Fatalf("FindByID: plan=%s status=%s", got.PlanID(), got.Status())
	}

	// FindByPlanID
	got2, err := gr.FindByPlanID(ctx, "plan-1")
	if err != nil {
		t.Fatal(err)
	}
	if got2.ID() != "g1" {
		t.Fatalf("FindByPlanID: id=%s", got2.ID())
	}

	// Update
	g.Start(t0)
	if err := gr.Update(ctx, g); err != nil {
		t.Fatal(err)
	}
	got3, _ := gr.FindByID(ctx, "g1")
	if got3.Status() != orch.GraphRunning {
		t.Fatalf("after update: status=%s, want running", got3.Status())
	}

	// Not found
	if _, err := gr.FindByID(ctx, "nope"); err != orch.ErrGraphNotFound {
		t.Fatalf("want ErrGraphNotFound, got %v", err)
	}

	// Delete
	if err := gr.Delete(ctx, "g1"); err != nil {
		t.Fatal(err)
	}
	if _, err := gr.FindByID(ctx, "g1"); err != orch.ErrGraphNotFound {
		t.Fatalf("after delete: want ErrGraphNotFound, got %v", err)
	}
}

func TestNodeRepo_RoundTrip(t *testing.T) {
	ctx, gr, nr, _ := setup(t)

	g, _ := orch.NewGraph(orch.NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	gr.Save(ctx, g)

	n, _ := orch.NewNode(orch.NewNodeInput{
		ID: "n1", GraphID: "g1", Category: orch.NodeCategoryBusiness,
		Title: "Dev task", Metadata: map[string]any{"branch": "feat-1"}, CreatedAt: t0,
	})
	if err := nr.Save(ctx, n); err != nil {
		t.Fatal(err)
	}

	got, err := nr.FindByID(ctx, "n1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title() != "Dev task" || got.Category() != orch.NodeCategoryBusiness {
		t.Fatalf("FindByID: title=%s cat=%s", got.Title(), got.Category())
	}
	if got.Metadata()["branch"] != "feat-1" {
		t.Fatal("metadata not preserved")
	}

	// Update with status change
	n.Start(t0)
	n.Complete("success", t0)
	if err := nr.Update(ctx, n); err != nil {
		t.Fatal(err)
	}
	got2, _ := nr.FindByID(ctx, "n1")
	if got2.Status() != orch.NodeCompleted || got2.Outcome() != "success" {
		t.Fatalf("after update: status=%s outcome=%s", got2.Status(), got2.Outcome())
	}
	if len(got2.ActionLogs()) != 2 {
		t.Fatalf("action logs = %d, want 2", len(got2.ActionLogs()))
	}

	// ListByGraph
	list, err := nr.ListByGraph(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	// g1 has auto start+end + n1 = 3 nodes total, but we only saved n1 to node repo
	if len(list) != 1 {
		t.Fatalf("ListByGraph = %d, want 1", len(list))
	}
}

func TestEdgeRepo_RoundTrip(t *testing.T) {
	ctx, gr, nr, er := setup(t)

	g, _ := orch.NewGraph(orch.NewGraphInput{ID: "g1", PlanID: "plan-1", CreatedAt: t0})
	gr.Save(ctx, g)

	// Save start and end nodes so FK constraints are satisfied
	for _, n := range g.Nodes() {
		nr.Save(ctx, n)
	}

	n1, _ := orch.NewNode(orch.NewNodeInput{ID: "n1", GraphID: "g1", Category: orch.NodeCategoryBusiness, Title: "A", CreatedAt: t0})
	n2, _ := orch.NewNode(orch.NewNodeInput{ID: "n2", GraphID: "g1", Category: orch.NodeCategoryBusiness, Title: "B", CreatedAt: t0})
	nr.Save(ctx, n1)
	nr.Save(ctx, n2)

	edge := orch.Edge{FromNodeID: "n1", ToNodeID: "n2"}
	if err := er.Save(ctx, "g1", edge); err != nil {
		t.Fatal(err)
	}

	edges, err := er.ListByGraph(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 || edges[0].FromNodeID != "n1" || edges[0].ToNodeID != "n2" {
		t.Fatalf("ListByGraph = %v", edges)
	}

	// Delete
	if err := er.Delete(ctx, "g1", "n1", "n2"); err != nil {
		t.Fatal(err)
	}
	edges2, _ := er.ListByGraph(ctx, "g1")
	if len(edges2) != 0 {
		t.Fatalf("after delete: edges = %d", len(edges2))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/projectmanager/orchestration/sqlite/... -v`
Expected: compilation error — repos don't exist

- [ ] **Step 3: Implement helpers.go**

Create `internal/projectmanager/orchestration/sqlite/helpers.go`:

```go
package sqlite

import (
	"encoding/json"
	"strings"
	"time"

	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func nullString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique")
}

func marshalMetadata(m map[string]any) string {
	if len(m) == 0 {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func unmarshalMetadata(s string) map[string]any {
	if strings.TrimSpace(s) == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return map[string]any{}
	}
	return m
}

func marshalActionLogs(logs []orch.ActionLog) string {
	if len(logs) == 0 {
		return "[]"
	}
	b, err := json.Marshal(logs)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func unmarshalActionLogs(s string) []orch.ActionLog {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var logs []orch.ActionLog
	if err := json.Unmarshal([]byte(s), &logs); err != nil {
		return nil
	}
	return logs
}
```

- [ ] **Step 4: Implement graph_repo.go**

Create `internal/projectmanager/orchestration/sqlite/graph_repo.go`:

```go
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
	"github.com/oopslink/agent-center/internal/persistence"
)

type GraphRepo struct{ db *sql.DB }

func NewGraphRepo(db *sql.DB) *GraphRepo { return &GraphRepo{db: db} }

const graphSelect = `SELECT id, plan_id, status, start_node, end_node, created_at, updated_at, version FROM pm_graphs`

func (r *GraphRepo) Save(ctx context.Context, g *orch.Graph) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_graphs (id, plan_id, status, start_node, end_node, created_at, updated_at, version)
		 VALUES (?,?,?,?,?,?,?,?)`,
		string(g.ID()), g.PlanID(), string(g.Status()),
		string(g.StartNodeID()), string(g.EndNodeID()),
		ts(g.CreatedAt()), ts(g.UpdatedAt()), g.Version())
	if isUnique(err) {
		return orch.ErrGraphExists
	}
	return err
}

func (r *GraphRepo) Update(ctx context.Context, g *orch.Graph) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE pm_graphs SET plan_id=?, status=?, updated_at=?, version=? WHERE id=?`,
		g.PlanID(), string(g.Status()), ts(g.UpdatedAt()), g.Version(), string(g.ID()))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return orch.ErrGraphNotFound
	}
	return nil
}

func (r *GraphRepo) FindByID(ctx context.Context, id orch.GraphID) (*orch.Graph, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, graphSelect+` WHERE id = ?`, string(id))
	g, err := scanGraph(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, orch.ErrGraphNotFound
	}
	return g, err
}

func (r *GraphRepo) FindByPlanID(ctx context.Context, planID string) (*orch.Graph, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, graphSelect+` WHERE plan_id = ?`, planID)
	g, err := scanGraph(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, orch.ErrGraphNotFound
	}
	return g, err
}

func (r *GraphRepo) Delete(ctx context.Context, id orch.GraphID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `DELETE FROM pm_graphs WHERE id = ?`, string(id))
	return err
}

func scanGraph(scan func(...any) error) (*orch.Graph, error) {
	var id, planID, status, startNode, endNode, createdAt, updatedAt string
	var version int
	if err := scan(&id, &planID, &status, &startNode, &endNode, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	return orch.RehydrateGraph(orch.RehydrateGraphInput{
		ID:        orch.GraphID(id),
		PlanID:    planID,
		Status:    orch.GraphStatus(status),
		StartNode: orch.NodeID(startNode),
		EndNode:   orch.NodeID(endNode),
		CreatedAt: parseTime(createdAt),
		UpdatedAt: parseTime(updatedAt),
		Version:   version,
	})
}
```

- [ ] **Step 5: Implement node_repo.go**

Create `internal/projectmanager/orchestration/sqlite/node_repo.go`:

```go
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
	"github.com/oopslink/agent-center/internal/persistence"
)

type NodeRepo struct{ db *sql.DB }

func NewNodeRepo(db *sql.DB) *NodeRepo { return &NodeRepo{db: db} }

const nodeSelect = `SELECT id, graph_id, category, control_kind, title, status, outcome, metadata, action_logs, created_at, updated_at, version FROM pm_graph_nodes`

func (r *NodeRepo) Save(ctx context.Context, n *orch.Node) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_graph_nodes (id, graph_id, category, control_kind, title, status, outcome, metadata, action_logs, created_at, updated_at, version)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(n.ID()), string(n.GraphID()), string(n.Category()), string(n.ControlKind()),
		n.Title(), string(n.Status()), n.Outcome(),
		n.MetadataJSON(), n.ActionLogsJSON(),
		ts(n.CreatedAt()), ts(n.UpdatedAt()), n.Version())
	if isUnique(err) {
		return orch.ErrNodeExists
	}
	return err
}

func (r *NodeRepo) Update(ctx context.Context, n *orch.Node) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE pm_graph_nodes SET title=?, status=?, outcome=?, metadata=?, action_logs=?, updated_at=?, version=? WHERE id=?`,
		n.Title(), string(n.Status()), n.Outcome(),
		n.MetadataJSON(), n.ActionLogsJSON(),
		ts(n.UpdatedAt()), n.Version(), string(n.ID()))
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return orch.ErrNodeNotFound
	}
	return nil
}

func (r *NodeRepo) FindByID(ctx context.Context, id orch.NodeID) (*orch.Node, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, nodeSelect+` WHERE id = ?`, string(id))
	n, err := scanNode(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, orch.ErrNodeNotFound
	}
	return n, err
}

func (r *NodeRepo) ListByGraph(ctx context.Context, graphID orch.GraphID) ([]*orch.Node, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, nodeSelect+` WHERE graph_id = ? ORDER BY created_at, id`, string(graphID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*orch.Node
	for rows.Next() {
		n, err := scanNode(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *NodeRepo) Delete(ctx context.Context, id orch.NodeID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `DELETE FROM pm_graph_nodes WHERE id = ?`, string(id))
	return err
}

func scanNode(scan func(...any) error) (*orch.Node, error) {
	var id, graphID, category, controlKind, title, status, outcome string
	var metadataJSON, actionLogsJSON, createdAt, updatedAt string
	var version int
	if err := scan(&id, &graphID, &category, &controlKind, &title, &status, &outcome,
		&metadataJSON, &actionLogsJSON, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	return orch.RehydrateNode(orch.RehydrateNodeInput{
		ID:          orch.NodeID(id),
		GraphID:     orch.GraphID(graphID),
		Category:    orch.NodeCategory(category),
		ControlKind: orch.ControlKind(controlKind),
		Title:       title,
		Status:      orch.NodeStatus(status),
		Outcome:     outcome,
		Metadata:    unmarshalMetadata(metadataJSON),
		ActionLogs:  unmarshalActionLogs(actionLogsJSON),
		CreatedAt:   parseTime(createdAt),
		UpdatedAt:   parseTime(updatedAt),
		Version:     version,
	})
}
```

- [ ] **Step 6: Implement edge_repo.go**

Create `internal/projectmanager/orchestration/sqlite/edge_repo.go`:

```go
package sqlite

import (
	"context"
	"database/sql"

	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
	"github.com/oopslink/agent-center/internal/persistence"
)

type EdgeRepo struct{ db *sql.DB }

func NewEdgeRepo(db *sql.DB) *EdgeRepo { return &EdgeRepo{db: db} }

func (r *EdgeRepo) Save(ctx context.Context, graphID orch.GraphID, e orch.Edge) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_graph_edges (graph_id, from_node_id, to_node_id) VALUES (?,?,?)`,
		string(graphID), string(e.FromNodeID), string(e.ToNodeID))
	if isUnique(err) {
		return orch.ErrEdgeExists
	}
	return err
}

func (r *EdgeRepo) Delete(ctx context.Context, graphID orch.GraphID, from, to orch.NodeID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`DELETE FROM pm_graph_edges WHERE graph_id = ? AND from_node_id = ? AND to_node_id = ?`,
		string(graphID), string(from), string(to))
	return err
}

func (r *EdgeRepo) ListByGraph(ctx context.Context, graphID orch.GraphID) ([]orch.Edge, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT from_node_id, to_node_id FROM pm_graph_edges WHERE graph_id = ? ORDER BY from_node_id, to_node_id`,
		string(graphID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []orch.Edge
	for rows.Next() {
		var from, to string
		if err := rows.Scan(&from, &to); err != nil {
			return nil, err
		}
		out = append(out, orch.Edge{FromNodeID: orch.NodeID(from), ToNodeID: orch.NodeID(to)})
	}
	return out, rows.Err()
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/projectmanager/orchestration/sqlite/... -v`
Expected: all tests PASS

- [ ] **Step 8: Commit**

```bash
git add internal/projectmanager/orchestration/repository.go internal/projectmanager/orchestration/sqlite/ internal/persistence/migrations/0091_v228_orchestration_graphs.up.sql internal/persistence/migrations/0091_v228_orchestration_graphs.down.sql
git commit -m "feat(orchestration): add SQLite repository implementations and migration"
```

---

### Task 7: Run All Tests and Final Verification

**Files:**
- No new files; runs existing tests to verify nothing is broken

**Interfaces:**
- Consumes: all code from Tasks 1-6

- [ ] **Step 1: Run orchestration package tests**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/projectmanager/orchestration/... -v -count=1`
Expected: all tests PASS

- [ ] **Step 2: Run existing projectmanager tests to verify no regression**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/projectmanager/... -count=1`
Expected: all tests PASS (new package does not affect existing code)

- [ ] **Step 3: Run full persistence migration tests**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/persistence/... -v -count=1`
Expected: all tests PASS (new migration applies cleanly)

- [ ] **Step 4: Verify build**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go build ./...`
Expected: clean build, no errors

- [ ] **Step 5: Commit (if any fixes were needed)**

```bash
git add -A && git commit -m "fix(orchestration): address test/build issues from integration"
```
