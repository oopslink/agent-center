# Agent Runtime & Memory Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the Agent Home directory structure, task execution state, local event streams, session instance lease, abort flow, GC, and reset scope alignment as specified in `docs/design/features/agent-runtime-memory-management.md`.

**Architecture:** The worker daemon's AgentController manages per-agent home directories under `AgentHomeBase/agents/{agent_id}/`. Each agent home has `memory/` (git-managed persistent knowledge), `tasks/` (per-task execution directories with JSONL event streams), and `plans/` (plan-level metadata). A `session.instance` file enforces single-instance CLI and tracks generation/PID. Task lifecycle flows through local directories: create → execute → complete/abort, with aborted tasks renamed atomically and cleaned by a periodic GC.

**Tech Stack:** Go 1.22+, file I/O (os/filepath), JSONL encoding, atomic file writes (temp+rename), existing `supervisormanager` epoch/lock primitives, existing `cognition/memory` GitOps.

## Global Constraints

- **DDD boundaries**: All new types live in their owning BC. Task execution local state is Agent-side (worker daemon); Memory stays in Cognition BC.
- **No FK constraints** in SQL (§9.w). New types are file-based, no new DB tables.
- **Path containment**: Every file operation under agent home MUST be validated against path escape (existing `wipeContained` pattern).
- **Atomic writes**: All state files use temp-file + rename pattern (existing `writeEpochAtomic` pattern).
- **Error handling**: No swallowed errors (§17). Every `if err != nil` either returns the error or emits an observable event.
- **Test coverage ≥ 90%** line coverage for new code.
- **Naming**: Follow existing conventions — `snake_case` for files, `CamelCase` for Go types, ULID for IDs.

---

### Task 1: Agent Home Directory Structure

Update the agent home layout from `{config, logs, tmp, memory, workspace}` to `{memory, plans, tasks}` per design §3.

**Files:**
- Modify: `internal/agent/agent.go:360` — update `HomeSubdirs`
- Modify: `internal/agent/agent.go:370-373` — remove `DefaultWorkspaceRel`, add `TasksDirRel`, `PlansDirRel`, `MemoryDirRel`
- Modify: `internal/workerdaemon/agent_controller.go:1950-1962` — update `agentPaths` to return `(home, tasksDir, plansDir, error)` instead of `(home, workspace, error)`
- Modify: `internal/workerdaemon/agent_controller.go:1975-2002` — update `cleanReset` to use new directory names
- Test: `internal/agent/agent_test.go` — update home subdir tests
- Test: `internal/workerdaemon/agent_controller_test.go` — update agentPaths + cleanReset tests

**Interfaces:**
- Produces: `Agent.TasksDirRel() string`, `Agent.PlansDirRel() string`, `Agent.MemoryDirRel() string`
- Produces: `agentPaths(agentID) (home, tasksDir, plansDir, error)`

- [ ] **Step 1: Write failing tests for new path helpers**

```go
// internal/agent/agent_test.go — add to existing test file
func TestAgent_HomeSubdirs_NewLayout(t *testing.T) {
	// Design §3: memory, plans, tasks (no config/logs/tmp/workspace)
	want := []string{"memory", "plans", "tasks"}
	if !reflect.DeepEqual(agent.HomeSubdirs, want) {
		t.Errorf("HomeSubdirs = %v, want %v", agent.HomeSubdirs, want)
	}
}

func TestAgent_TasksDirRel(t *testing.T) {
	a := mustNewAgent(t, "ag-1", "org-1", "w-1")
	got := a.TasksDirRel()
	want := "workers/w-1/agents/ag-1/tasks"
	if got != want {
		t.Errorf("TasksDirRel() = %q, want %q", got, want)
	}
}

func TestAgent_PlansDirRel(t *testing.T) {
	a := mustNewAgent(t, "ag-1", "org-1", "w-1")
	got := a.PlansDirRel()
	want := "workers/w-1/agents/ag-1/plans"
	if got != want {
		t.Errorf("PlansDirRel() = %q, want %q", got, want)
	}
}

func TestAgent_MemoryDirRel(t *testing.T) {
	a := mustNewAgent(t, "ag-1", "org-1", "w-1")
	got := a.MemoryDirRel()
	want := "workers/w-1/agents/ag-1/memory"
	if got != want {
		t.Errorf("MemoryDirRel() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/agent/ -run 'TestAgent_(HomeSubdirs_NewLayout|TasksDirRel|PlansDirRel|MemoryDirRel)' -v`
Expected: FAIL — `HomeSubdirs` still has old values, methods don't exist.

- [ ] **Step 3: Update HomeSubdirs and add path helpers**

```go
// internal/agent/agent.go — replace line 360
var HomeSubdirs = []string{"memory", "plans", "tasks"}

// Replace DefaultWorkspaceRel (lines 370-373) with three new helpers:

// TasksDirRel returns the worker-relative tasks directory:
// workers/{worker_id}/agents/{agent_id}/tasks.
func (a *Agent) TasksDirRel() string {
	return path.Join(a.HomeRel(), "tasks")
}

// PlansDirRel returns the worker-relative plans directory:
// workers/{worker_id}/agents/{agent_id}/plans.
func (a *Agent) PlansDirRel() string {
	return path.Join(a.HomeRel(), "plans")
}

// MemoryDirRel returns the worker-relative memory directory:
// workers/{worker_id}/agents/{agent_id}/memory.
func (a *Agent) MemoryDirRel() string {
	return path.Join(a.HomeRel(), "memory")
}
```

- [ ] **Step 4: Run agent tests to verify they pass**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/agent/ -run 'TestAgent_(HomeSubdirs_NewLayout|TasksDirRel|PlansDirRel|MemoryDirRel)' -v`
Expected: PASS

- [ ] **Step 5: Fix all callers of `DefaultWorkspaceRel` and old `HomeSubdirs`**

Search for all callers of `DefaultWorkspaceRel` and `HomeSubdirs` in the codebase. Update each caller to use the new helpers. Also update `agentPaths` in the worker daemon:

```go
// internal/workerdaemon/agent_controller.go — replace agentPaths (lines 1950-1962)
func (c *AgentController) agentPaths(agentID string) (home, tasksDir, plansDir string, err error) {
	if strings.TrimSpace(c.cfg.AgentHomeBase) == "" {
		return "", "", "", errors.New("agent_controller: agent_home_base required")
	}
	if strings.TrimSpace(c.cfg.WorkerID) == "" {
		return "", "", "", errors.New("agent_controller: worker_id required")
	}
	if strings.TrimSpace(agentID) == "" {
		return "", "", "", errors.New("agent_controller: agent_id required")
	}
	home = filepath.Join(c.cfg.AgentHomeBase, "agents", agentID)
	tasksDir = filepath.Join(home, "tasks")
	plansDir = filepath.Join(home, "plans")
	return home, tasksDir, plansDir, nil
}
```

Update `cleanReset` (lines 1975-2002) to use `tasks` + `plans` instead of `workspace`:

```go
func (c *AgentController) cleanReset(agentID, resetScope string) error {
	home, tasksDir, plansDir, err := c.agentPaths(agentID)
	if err != nil {
		return err
	}
	memory := filepath.Join(home, "memory")

	var targets []string
	switch strings.ToLower(strings.TrimSpace(resetScope)) {
	case "memory":
		targets = []string{memory}
	case "workspace":
		// Design §3.1: workspace resets tasks/ + plans/
		targets = []string{tasksDir, plansDir}
	case "", "all":
		targets = []string{memory, tasksDir, plansDir}
	default:
		c.log("reset agent=%s unknown scope=%q — defaulting to all", agentID, resetScope)
		targets = []string{memory, tasksDir, plansDir}
	}

	for _, t := range targets {
		if err := c.wipeContained(home, t); err != nil {
			return err
		}
	}
	c.log("reset agent=%s scope=%q wiped %d dir(s) under %s", agentID, resetScope, len(targets), home)
	return nil
}
```

- [ ] **Step 6: Fix all callers of `agentPaths` across workerdaemon**

Every call to `agentPaths` returns 4 values now. Update each call site:
- `boot_reconcile.go` lines 349, 371 — update destructuring
- `self_heal.go` line 223 — update destructuring
- `agent_controller.go` line 681, 1176 — update destructuring
- All tests referencing `agentPaths`

For callers that only need `home`, use `home, _, _, err := c.agentPaths(...)`.

- [ ] **Step 7: Run all workerdaemon tests**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/... -v -count=1`
Expected: PASS (all existing tests updated to new signatures)

- [ ] **Step 8: Run full project build + test**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go build ./... && go test ./... -count=1`
Expected: BUILD + PASS (no broken callers remain)

- [ ] **Step 9: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go internal/workerdaemon/
git commit -m "refactor(agent): update home layout to memory/plans/tasks (design §3)

Replace HomeSubdirs {config,logs,tmp,memory,workspace} with {memory,plans,tasks}.
Add TasksDirRel/PlansDirRel/MemoryDirRel helpers, remove DefaultWorkspaceRel.
Update agentPaths to return (home, tasksDir, plansDir, err).
Update cleanReset: workspace scope now clears tasks/ + plans/.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Session Instance Lease File

Implement the `session.instance` lease file (design §3, §4.1) that tracks CLI single-instance enforcement with `session_id`, `generation`, `pid`, `prev_pid`, `prev_crash_at`. Complements the existing `session.epoch` (which handles reset epochs).

**Files:**
- Create: `internal/workerdaemon/sessioninstance/instance.go` — `InstanceState` type + Read/Write/Acquire
- Create: `internal/workerdaemon/sessioninstance/instance_test.go` — exhaustive tests
- Modify: `internal/workerdaemon/agent_controller.go` — call `Acquire` on session start

**Interfaces:**
- Consumes: `agentPaths(agentID)` from Task 1
- Produces: `InstanceState{SessionID, Generation, PID, PrevPID, PrevCrashAt}`, `ReadInstance(home) (InstanceState, error)`, `AcquireInstance(home, sessionID, pid) (InstanceState, error)`, `ReleaseInstance(home) error`

- [ ] **Step 1: Write failing tests for InstanceState read/write**

```go
// internal/workerdaemon/sessioninstance/instance_test.go
package sessioninstance

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadInstance_Missing_ReturnsZero(t *testing.T) {
	home := t.TempDir()
	st, err := ReadInstance(home)
	if err != nil {
		t.Fatalf("ReadInstance on empty home: %v", err)
	}
	if st.Generation != 0 || st.SessionID != "" || st.PID != 0 {
		t.Errorf("expected zero state, got %+v", st)
	}
}

func TestAcquireInstance_Fresh(t *testing.T) {
	home := t.TempDir()
	st, err := AcquireInstance(home, "sess-1", 1234)
	if err != nil {
		t.Fatalf("AcquireInstance: %v", err)
	}
	if st.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", st.SessionID, "sess-1")
	}
	if st.Generation != 1 {
		t.Errorf("Generation = %d, want 1", st.Generation)
	}
	if st.PID != 1234 {
		t.Errorf("PID = %d, want 1234", st.PID)
	}
	if st.PrevPID != 0 {
		t.Errorf("PrevPID = %d, want 0", st.PrevPID)
	}

	// Verify persisted
	persisted, err := ReadInstance(home)
	if err != nil {
		t.Fatalf("ReadInstance after acquire: %v", err)
	}
	if persisted.SessionID != "sess-1" || persisted.Generation != 1 {
		t.Errorf("persisted = %+v", persisted)
	}
}

func TestAcquireInstance_Successive_BumpsGeneration(t *testing.T) {
	home := t.TempDir()
	st1, _ := AcquireInstance(home, "sess-1", 100)
	st2, err := AcquireInstance(home, "sess-2", 200)
	if err != nil {
		t.Fatalf("second AcquireInstance: %v", err)
	}
	if st2.Generation != st1.Generation+1 {
		t.Errorf("Generation = %d, want %d", st2.Generation, st1.Generation+1)
	}
	if st2.PrevPID != 100 {
		t.Errorf("PrevPID = %d, want 100", st2.PrevPID)
	}
}

func TestAcquireInstance_AfterCrash_RecordsPrevCrashAt(t *testing.T) {
	home := t.TempDir()
	// Simulate a crash: write state with a PID, then re-acquire without release
	AcquireInstance(home, "sess-1", 100)
	// No ReleaseInstance call = simulated crash
	st, err := AcquireInstance(home, "sess-2", 200)
	if err != nil {
		t.Fatalf("AcquireInstance after crash: %v", err)
	}
	if st.PrevCrashAt.IsZero() {
		t.Error("PrevCrashAt should be non-zero after crash (no clean release)")
	}
}

func TestReleaseInstance(t *testing.T) {
	home := t.TempDir()
	AcquireInstance(home, "sess-1", 100)
	if err := ReleaseInstance(home); err != nil {
		t.Fatalf("ReleaseInstance: %v", err)
	}
	// After release, the file still exists but PID is cleared
	st, err := ReadInstance(home)
	if err != nil {
		t.Fatalf("ReadInstance after release: %v", err)
	}
	if st.PID != 0 {
		t.Errorf("PID = %d after release, want 0", st.PID)
	}
}

func TestReadInstance_CorruptFile_ReturnsError(t *testing.T) {
	home := t.TempDir()
	// Write garbage
	os.WriteFile(filepath.Join(home, InstanceFileName), []byte("not json"), 0o600)
	_, err := ReadInstance(home)
	if err == nil {
		t.Fatal("expected error on corrupt file, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/sessioninstance/ -v`
Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Implement session instance**

```go
// internal/workerdaemon/sessioninstance/instance.go
package sessioninstance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const InstanceFileName = "session.instance"

// InstanceState tracks the CLI single-instance lease per design §3.
type InstanceState struct {
	SessionID   string    `json:"session_id"`
	Generation  int       `json:"generation"`
	PID         int       `json:"pid"`
	PrevPID     int       `json:"prev_pid"`
	PrevCrashAt time.Time `json:"prev_crash_at,omitempty"`
	// CleanRelease is true when the prior session exited via ReleaseInstance
	// (not a crash). Internal bookkeeping — AcquireInstance checks this to
	// decide whether to populate PrevCrashAt.
	CleanRelease bool `json:"clean_release,omitempty"`
}

func instanceFilePath(home string) string {
	return filepath.Join(home, InstanceFileName)
}

// ReadInstance reads <home>/session.instance. A MISSING file is the initial
// state (zero). A corrupt file is an ERROR (not silently zeroed), following
// the same principle as supervisormanager.ReadEpoch.
func ReadInstance(home string) (InstanceState, error) {
	if home == "" {
		return InstanceState{}, errors.New("sessioninstance: home required")
	}
	b, err := os.ReadFile(instanceFilePath(home))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return InstanceState{}, nil
		}
		return InstanceState{}, fmt.Errorf("sessioninstance: read: %w", err)
	}
	var st InstanceState
	if err := json.Unmarshal(b, &st); err != nil {
		return InstanceState{}, fmt.Errorf("sessioninstance: corrupt %s: %w",
			instanceFilePath(home), err)
	}
	return st, nil
}

// AcquireInstance claims the single-instance lease: bumps generation, records
// PID, preserves crash history. The caller must hold the agent home lock.
func AcquireInstance(home, sessionID string, pid int) (InstanceState, error) {
	if home == "" {
		return InstanceState{}, errors.New("sessioninstance: home required")
	}
	prev, err := ReadInstance(home)
	if err != nil {
		return InstanceState{}, err
	}

	next := InstanceState{
		SessionID:  sessionID,
		Generation: prev.Generation + 1,
		PID:        pid,
		PrevPID:    prev.PID,
	}
	// If the previous instance had a PID and was NOT cleanly released, it
	// crashed — record the crash timestamp.
	if prev.PID != 0 && !prev.CleanRelease {
		next.PrevCrashAt = time.Now().UTC()
	}
	if err := writeInstanceAtomic(home, next); err != nil {
		return InstanceState{}, err
	}
	return next, nil
}

// ReleaseInstance marks a clean shutdown: clears PID, sets CleanRelease.
func ReleaseInstance(home string) error {
	if home == "" {
		return errors.New("sessioninstance: home required")
	}
	st, err := ReadInstance(home)
	if err != nil {
		return err
	}
	st.PID = 0
	st.CleanRelease = true
	return writeInstanceAtomic(home, st)
}

func writeInstanceAtomic(home string, st InstanceState) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("sessioninstance: mkdir: %w", err)
	}
	b, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("sessioninstance: marshal: %w", err)
	}
	final := instanceFilePath(home)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("sessioninstance: write tmp: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("sessioninstance: rename: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/sessioninstance/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/workerdaemon/sessioninstance/
git commit -m "feat(workerdaemon): add session instance lease file (design §3, §4.1)

Implements session.instance tracking: session_id, generation (monotonic),
pid, prev_pid, prev_crash_at. Complements session.epoch (reset epochs).
AcquireInstance bumps generation + records crash history on unclean exit.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Task Execution State Types

Define the value types for per-task execution directories (design §6.3): `TaskExecutionStatus`, `TaskExecutionMeta` (task.json), `ExecutionContext` (execution.json).

**Files:**
- Create: `internal/workerdaemon/taskexec/types.go` — domain types
- Create: `internal/workerdaemon/taskexec/types_test.go` — validation tests

**Interfaces:**
- Produces: `TaskExecutionStatus` enum (`pending`, `running`, `paused`, `failed`, `done`)
- Produces: `TaskExecutionMeta{TaskID, Status, PlanID, CreatedAt, UpdatedAt}`
- Produces: `ExecutionContext{ForkParams, RetryCount, LLMConfig}`

- [ ] **Step 1: Write tests for task execution status**

```go
// internal/workerdaemon/taskexec/types_test.go
package taskexec

import "testing"

func TestTaskExecutionStatus_IsValid(t *testing.T) {
	valid := []TaskExecutionStatus{StatusPending, StatusRunning, StatusPaused, StatusFailed, StatusDone}
	for _, s := range valid {
		if !s.IsValid() {
			t.Errorf("%q should be valid", s)
		}
	}
	if TaskExecutionStatus("bogus").IsValid() {
		t.Error("bogus should be invalid")
	}
}

func TestTaskExecutionStatus_IsTerminal(t *testing.T) {
	if !StatusFailed.IsTerminal() {
		t.Error("failed should be terminal")
	}
	if !StatusDone.IsTerminal() {
		t.Error("done should be terminal")
	}
	if StatusRunning.IsTerminal() {
		t.Error("running should not be terminal")
	}
}

func TestTaskExecutionMeta_Validate(t *testing.T) {
	m := TaskExecutionMeta{TaskID: "t-1", Status: StatusPending}
	if err := m.Validate(); err != nil {
		t.Errorf("valid meta: %v", err)
	}
	m.TaskID = ""
	if err := m.Validate(); err == nil {
		t.Error("empty TaskID should fail validation")
	}
	m.TaskID = "t-1"
	m.Status = "bogus"
	if err := m.Validate(); err == nil {
		t.Error("invalid status should fail validation")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/taskexec/ -v`
Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Implement types**

```go
// internal/workerdaemon/taskexec/types.go
package taskexec

import (
	"errors"
	"time"
)

// TaskExecutionStatus is the agent-local execution status (design §6.3).
type TaskExecutionStatus string

const (
	StatusPending TaskExecutionStatus = "pending"
	StatusRunning TaskExecutionStatus = "running"
	StatusPaused  TaskExecutionStatus = "paused"
	StatusFailed  TaskExecutionStatus = "failed"
	StatusDone    TaskExecutionStatus = "done"
)

func (s TaskExecutionStatus) IsValid() bool {
	switch s {
	case StatusPending, StatusRunning, StatusPaused, StatusFailed, StatusDone:
		return true
	}
	return false
}

func (s TaskExecutionStatus) IsTerminal() bool {
	return s == StatusFailed || s == StatusDone
}

// TaskExecutionMeta is persisted as tasks/{task_id}/task.json (design §6.3).
type TaskExecutionMeta struct {
	TaskID    string              `json:"task_id"`
	Status    TaskExecutionStatus `json:"status"`
	PlanID    string              `json:"plan_id,omitempty"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

func (m *TaskExecutionMeta) Validate() error {
	if m.TaskID == "" {
		return errors.New("taskexec: task_id required")
	}
	if !m.Status.IsValid() {
		return errors.New("taskexec: invalid status")
	}
	return nil
}

// ExecutionContext is persisted as tasks/{task_id}/execution.json (design §6.3).
type ExecutionContext struct {
	SessionID  string            `json:"session_id,omitempty"`
	RetryCount int               `json:"retry_count"`
	LLMModel   string            `json:"llm_model,omitempty"`
	LLMConfig  map[string]string `json:"llm_config,omitempty"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/taskexec/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/workerdaemon/taskexec/
git commit -m "feat(taskexec): add task execution state types (design §6.3)

TaskExecutionStatus (pending/running/paused/failed/done),
TaskExecutionMeta (task.json), ExecutionContext (execution.json).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Task Execution Directory Manager

Implement the directory manager for `tasks/{task_id}/` lifecycle: create, read, update status, list, scan (design §3, §3.2, §6.3).

**Files:**
- Create: `internal/workerdaemon/taskexec/dirmanager.go` — directory CRUD
- Create: `internal/workerdaemon/taskexec/dirmanager_test.go` — tests

**Interfaces:**
- Consumes: `TaskExecutionMeta`, `ExecutionContext` from Task 3
- Produces: `DirManager` with `Create(tasksDir, meta, execCtx)`, `Read(tasksDir, taskID)`, `UpdateStatus(tasksDir, taskID, status)`, `ScanActive(tasksDir) []TaskExecutionMeta`, `IsStandardDir(name) bool`

- [ ] **Step 1: Write failing tests**

```go
// internal/workerdaemon/taskexec/dirmanager_test.go
package taskexec

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDirManager_Create_And_Read(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Now().UTC()
	meta := TaskExecutionMeta{
		TaskID: "task-abc", Status: StatusPending, PlanID: "plan-1",
		CreatedAt: now, UpdatedAt: now,
	}
	execCtx := ExecutionContext{SessionID: "sess-1", LLMModel: "claude-4"}
	if err := dm.Create(tasksDir, meta, execCtx); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := dm.Read(tasksDir, "task-abc")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Meta.TaskID != "task-abc" || got.Meta.Status != StatusPending {
		t.Errorf("Read meta = %+v", got.Meta)
	}
	if got.ExecCtx.SessionID != "sess-1" {
		t.Errorf("Read execCtx = %+v", got.ExecCtx)
	}
}

func TestDirManager_UpdateStatus(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Now().UTC()
	meta := TaskExecutionMeta{TaskID: "task-1", Status: StatusPending, CreatedAt: now, UpdatedAt: now}
	dm.Create(tasksDir, meta, ExecutionContext{})
	if err := dm.UpdateStatus(tasksDir, "task-1", StatusRunning); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, _ := dm.Read(tasksDir, "task-1")
	if got.Meta.Status != StatusRunning {
		t.Errorf("Status = %q, want running", got.Meta.Status)
	}
}

func TestDirManager_ScanActive(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Now().UTC()
	// Create standard dirs
	dm.Create(tasksDir, TaskExecutionMeta{TaskID: "t-1", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}, ExecutionContext{})
	dm.Create(tasksDir, TaskExecutionMeta{TaskID: "t-2", Status: StatusPaused, CreatedAt: now, UpdatedAt: now}, ExecutionContext{})

	active := dm.ScanActive(tasksDir)
	if len(active) != 2 {
		t.Fatalf("ScanActive = %d entries, want 2", len(active))
	}
}

func TestDirManager_ScanActive_IgnoresAborted(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Now().UTC()
	dm.Create(tasksDir, TaskExecutionMeta{TaskID: "t-1", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}, ExecutionContext{})
	// Create an aborted-looking directory manually
	abortedDir := filepath.Join(tasksDir, "t-2__aborted_20260626T100000Z")
	if err := mkdirAll(abortedDir); err != nil {
		t.Fatal(err)
	}

	active := dm.ScanActive(tasksDir)
	if len(active) != 1 {
		t.Fatalf("ScanActive = %d entries (should skip aborted), want 1", len(active))
	}
}

func TestIsStandardTaskDir(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"task-abc", true},
		{"01JXYZ", true},
		{"task-1__aborted_20260626T100000Z", false},
		{"task-1__aborted_20260626T100000Z__gc_deleting", false},
		{".hidden", false},
	}
	for _, tt := range tests {
		if got := IsStandardTaskDir(tt.name); got != tt.want {
			t.Errorf("IsStandardTaskDir(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/taskexec/ -run 'TestDirManager|TestIsStandard' -v`
Expected: FAIL

- [ ] **Step 3: Implement DirManager**

```go
// internal/workerdaemon/taskexec/dirmanager.go
package taskexec

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	taskMetaFile    = "task.json"
	execContextFile = "execution.json"
)

// TaskDirEntry holds a task directory's parsed contents.
type TaskDirEntry struct {
	Meta    TaskExecutionMeta
	ExecCtx ExecutionContext
}

// DirManager handles per-task execution directory lifecycle.
type DirManager struct{}

func NewDirManager() *DirManager { return &DirManager{} }

// Create initializes tasks/{task_id}/ with task.json + execution.json.
func (d *DirManager) Create(tasksDir string, meta TaskExecutionMeta, execCtx ExecutionContext) error {
	if err := meta.Validate(); err != nil {
		return err
	}
	dir := filepath.Join(tasksDir, meta.TaskID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("taskexec: mkdir %q: %w", dir, err)
	}
	if err := writeJSONAtomic(filepath.Join(dir, taskMetaFile), meta); err != nil {
		return fmt.Errorf("taskexec: write task.json: %w", err)
	}
	if err := writeJSONAtomic(filepath.Join(dir, execContextFile), execCtx); err != nil {
		return fmt.Errorf("taskexec: write execution.json: %w", err)
	}
	return nil
}

// Read loads the task.json + execution.json from tasks/{task_id}/.
func (d *DirManager) Read(tasksDir, taskID string) (TaskDirEntry, error) {
	dir := filepath.Join(tasksDir, taskID)
	var entry TaskDirEntry
	if err := readJSON(filepath.Join(dir, taskMetaFile), &entry.Meta); err != nil {
		return TaskDirEntry{}, fmt.Errorf("taskexec: read task.json for %s: %w", taskID, err)
	}
	// execution.json is optional (may not exist for very old dirs)
	_ = readJSON(filepath.Join(dir, execContextFile), &entry.ExecCtx)
	return entry, nil
}

// UpdateStatus updates the status field in task.json.
func (d *DirManager) UpdateStatus(tasksDir, taskID string, status TaskExecutionStatus) error {
	dir := filepath.Join(tasksDir, taskID)
	var meta TaskExecutionMeta
	if err := readJSON(filepath.Join(dir, taskMetaFile), &meta); err != nil {
		return fmt.Errorf("taskexec: read for update %s: %w", taskID, err)
	}
	meta.Status = status
	meta.UpdatedAt = time.Now().UTC()
	return writeJSONAtomic(filepath.Join(dir, taskMetaFile), meta)
}

// ScanActive scans tasksDir for standard task directories (design §3.2).
// Returns metadata for each, skipping aborted/gc_deleting dirs.
func (d *DirManager) ScanActive(tasksDir string) []TaskExecutionMeta {
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return nil
	}
	var result []TaskExecutionMeta
	for _, e := range entries {
		if !e.IsDir() || !IsStandardTaskDir(e.Name()) {
			continue
		}
		var meta TaskExecutionMeta
		if err := readJSON(filepath.Join(tasksDir, e.Name(), taskMetaFile), &meta); err != nil {
			continue // skip unreadable dirs
		}
		result = append(result, meta)
	}
	return result
}

// IsStandardTaskDir identifies a standard execution directory name (design §3.2).
// Standard: pure task_id (no suffix). Non-standard: __aborted_ or __gc_deleting.
func IsStandardTaskDir(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") {
		return false
	}
	if strings.Contains(name, "__aborted_") {
		return false
	}
	if strings.Contains(name, "__gc_deleting") {
		return false
	}
	return true
}

func writeJSONAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func mkdirAll(path string) error {
	return os.MkdirAll(path, 0o700)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/taskexec/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/workerdaemon/taskexec/
git commit -m "feat(taskexec): add task execution directory manager (design §3, §6.3)

Create/Read/UpdateStatus/ScanActive for tasks/{task_id}/ directories.
IsStandardTaskDir filters aborted/gc_deleting names per §3.2.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Local Event Stream (events.current.jsonl)

Implement the per-task local event stream writer and offset tracker (design §8, §10).

**Files:**
- Create: `internal/workerdaemon/taskexec/eventstream.go` — JSONL writer + offset
- Create: `internal/workerdaemon/taskexec/eventstream_test.go` — tests

**Interfaces:**
- Consumes: Task directory from Task 4
- Produces: `EventStreamWriter` with `Append(taskDir, event)`, `ReadAll(taskDir) []RawEvent`, `ReadOffset(taskDir) EventOffset`, `UpdateOffset(taskDir, offset)`

- [ ] **Step 1: Write failing tests**

```go
// internal/workerdaemon/taskexec/eventstream_test.go
package taskexec

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEventStreamWriter_Append_And_ReadAll(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	os.MkdirAll(taskDir, 0o700)
	w := NewEventStreamWriter()

	ev1 := RawEvent{
		ID:        "ev-1",
		EventType: "assistant_text",
		Payload:   `{"text":"hello"}`,
		OccurredAt: time.Now().UTC(),
	}
	ev2 := RawEvent{
		ID:        "ev-2",
		EventType: "tool_use",
		Payload:   `{"tool_name":"grep"}`,
		OccurredAt: time.Now().UTC(),
	}
	if err := w.Append(taskDir, ev1); err != nil {
		t.Fatalf("Append ev1: %v", err)
	}
	if err := w.Append(taskDir, ev2); err != nil {
		t.Fatalf("Append ev2: %v", err)
	}

	events, err := w.ReadAll(taskDir)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("ReadAll = %d events, want 2", len(events))
	}
	if events[0].ID != "ev-1" || events[1].ID != "ev-2" {
		t.Errorf("events = %+v", events)
	}
}

func TestEventStreamWriter_ReadAll_MissingFile(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	os.MkdirAll(taskDir, 0o700)
	w := NewEventStreamWriter()
	events, err := w.ReadAll(taskDir)
	if err != nil {
		t.Fatalf("ReadAll on missing: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestEventOffset_ReadWrite(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	os.MkdirAll(taskDir, 0o700)
	w := NewEventStreamWriter()

	off := EventOffset{Segment: "current", ByteOffset: 1024, LastEventID: "ev-5"}
	if err := w.UpdateOffset(taskDir, off); err != nil {
		t.Fatalf("UpdateOffset: %v", err)
	}
	got, err := w.ReadOffset(taskDir)
	if err != nil {
		t.Fatalf("ReadOffset: %v", err)
	}
	if got.Segment != "current" || got.ByteOffset != 1024 || got.LastEventID != "ev-5" {
		t.Errorf("offset = %+v", got)
	}
}

func TestEventOffset_ReadMissing(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task-1")
	os.MkdirAll(taskDir, 0o700)
	w := NewEventStreamWriter()
	off, err := w.ReadOffset(taskDir)
	if err != nil {
		t.Fatalf("ReadOffset on missing: %v", err)
	}
	if off.ByteOffset != 0 || off.LastEventID != "" {
		t.Errorf("expected zero offset, got %+v", off)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/taskexec/ -run 'TestEventStream|TestEventOffset' -v`
Expected: FAIL

- [ ] **Step 3: Implement event stream**

```go
// internal/workerdaemon/taskexec/eventstream.go
package taskexec

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	eventsCurrentFile = "events.current.jsonl"
	eventsOffsetFile  = "events.offset"
)

// RawEvent is a single event line in events.current.jsonl (design §8.2).
type RawEvent struct {
	ID             string    `json:"id"`
	EventType      string    `json:"event_type"`
	TaskRef        string    `json:"task_ref,omitempty"`
	InteractionRef string    `json:"interaction_ref,omitempty"`
	Payload        string    `json:"payload"`
	OccurredAt     time.Time `json:"occurred_at"`
}

// EventOffset tracks the Center consumption position (design §8.1).
type EventOffset struct {
	Segment     string `json:"segment"`
	ByteOffset  int64  `json:"byte_offset"`
	LastEventID string `json:"last_event_id"`
}

// EventStreamWriter manages the per-task JSONL event stream.
type EventStreamWriter struct{}

func NewEventStreamWriter() *EventStreamWriter { return &EventStreamWriter{} }

// Append writes one event line to events.current.jsonl. Appends atomically
// by opening in O_APPEND mode.
func (w *EventStreamWriter) Append(taskDir string, ev RawEvent) error {
	path := filepath.Join(taskDir, eventsCurrentFile)
	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("taskexec: marshal event: %w", err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("taskexec: open events file: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("taskexec: write event: %w", err)
	}
	return nil
}

// ReadAll reads all events from events.current.jsonl (oldest-first).
// Returns nil slice if the file doesn't exist.
func (w *EventStreamWriter) ReadAll(taskDir string) ([]RawEvent, error) {
	path := filepath.Join(taskDir, eventsCurrentFile)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return fmt.Errorf("taskexec: open events: %w", err), nil
	}
	defer f.Close()

	var events []RawEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20) // 1MB max line
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev RawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // skip malformed lines (design §10.2: truncate dirty data)
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return events, fmt.Errorf("taskexec: scan events: %w", err)
	}
	return events, nil
}

// ReadOffset reads events.offset. Missing file returns zero offset.
func (w *EventStreamWriter) ReadOffset(taskDir string) (EventOffset, error) {
	path := filepath.Join(taskDir, eventsOffsetFile)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return EventOffset{}, nil
		}
		return EventOffset{}, fmt.Errorf("taskexec: read offset: %w", err)
	}
	var off EventOffset
	if err := json.Unmarshal(b, &off); err != nil {
		return EventOffset{}, fmt.Errorf("taskexec: unmarshal offset: %w", err)
	}
	return off, nil
}

// UpdateOffset writes the consumption position.
func (w *EventStreamWriter) UpdateOffset(taskDir string, off EventOffset) error {
	return writeJSONAtomic(filepath.Join(taskDir, eventsOffsetFile), off)
}
```

- [ ] **Step 4: Fix the ReadAll return signature (returns were swapped)**

The ReadAll function has a bug — the error return was in the wrong position for the `os.ErrNotExist` branch. Fix:

```go
// Fix the ReadAll method — correct the returns
func (w *EventStreamWriter) ReadAll(taskDir string) ([]RawEvent, error) {
	path := filepath.Join(taskDir, eventsCurrentFile)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("taskexec: open events: %w", err)
	}
	defer f.Close()

	var events []RawEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev RawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return events, fmt.Errorf("taskexec: scan events: %w", err)
	}
	return events, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/taskexec/ -run 'TestEventStream|TestEventOffset' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/workerdaemon/taskexec/
git commit -m "feat(taskexec): add local event stream writer (design §8)

JSONL append to events.current.jsonl, ReadAll for replay,
EventOffset tracking for Center consumption position.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Task Abort Flow

Implement the abort flow (design §11.2): stop execution → write abort event → atomic rename to `tasks/{task_id}__aborted_{ts}/`.

**Files:**
- Create: `internal/workerdaemon/taskexec/abort.go` — abort logic
- Create: `internal/workerdaemon/taskexec/abort_test.go` — tests

**Interfaces:**
- Consumes: `DirManager`, `EventStreamWriter` from Tasks 4-5
- Produces: `AbortTask(tasksDir, taskID) error`, `AbortedDirName(taskID, ts) string`, `ParseAbortedDir(name) (taskID string, ts time.Time, gcDeleting bool, ok bool)`

- [ ] **Step 1: Write failing tests**

```go
// internal/workerdaemon/taskexec/abort_test.go
package taskexec

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAbortedDirName(t *testing.T) {
	ts := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	got := AbortedDirName("task-abc", ts)
	want := "task-abc__aborted_20260626T100000Z"
	if got != want {
		t.Errorf("AbortedDirName = %q, want %q", got, want)
	}
}

func TestParseAbortedDir(t *testing.T) {
	tests := []struct {
		name       string
		wantID     string
		wantGC     bool
		wantOK     bool
	}{
		{"task-1__aborted_20260626T100000Z", "task-1", false, true},
		{"task-1__aborted_20260626T100000Z__gc_deleting", "task-1", true, true},
		{"task-1", "", false, false},
		{"", "", false, false},
	}
	for _, tt := range tests {
		id, _, gc, ok := ParseAbortedDir(tt.name)
		if ok != tt.wantOK {
			t.Errorf("ParseAbortedDir(%q) ok = %v, want %v", tt.name, ok, tt.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if id != tt.wantID {
			t.Errorf("ParseAbortedDir(%q) id = %q, want %q", tt.name, id, tt.wantID)
		}
		if gc != tt.wantGC {
			t.Errorf("ParseAbortedDir(%q) gc = %v, want %v", tt.name, gc, tt.wantGC)
		}
	}
}

func TestAbortTask(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Now().UTC()
	meta := TaskExecutionMeta{TaskID: "task-1", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}
	dm.Create(tasksDir, meta, ExecutionContext{})

	// Write an event first
	w := NewEventStreamWriter()
	w.Append(filepath.Join(tasksDir, "task-1"), RawEvent{ID: "ev-1", EventType: "assistant_text", Payload: "{}", OccurredAt: now})

	abortedName, err := AbortTask(tasksDir, "task-1", now)
	if err != nil {
		t.Fatalf("AbortTask: %v", err)
	}
	// Original dir should be gone
	if _, err := os.Stat(filepath.Join(tasksDir, "task-1")); !os.IsNotExist(err) {
		t.Error("original task dir should not exist after abort")
	}
	// Aborted dir should exist
	if _, err := os.Stat(filepath.Join(tasksDir, abortedName)); err != nil {
		t.Errorf("aborted dir %q should exist: %v", abortedName, err)
	}
	// Should have an abort event in the events file
	events, _ := w.ReadAll(filepath.Join(tasksDir, abortedName))
	if len(events) < 2 {
		t.Fatalf("expected ≥2 events (original + abort), got %d", len(events))
	}
	last := events[len(events)-1]
	if last.EventType != "lifecycle" {
		t.Errorf("last event type = %q, want lifecycle", last.EventType)
	}
}

func TestAbortTask_MissingDir(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	os.MkdirAll(tasksDir, 0o700)
	_, err := AbortTask(tasksDir, "nonexistent", time.Now())
	if err == nil {
		t.Error("expected error for missing task dir")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/taskexec/ -run 'TestAbort' -v`
Expected: FAIL

- [ ] **Step 3: Implement abort**

```go
// internal/workerdaemon/taskexec/abort.go
package taskexec

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const abortedSuffix = "__aborted_"
const gcDeletingSuffix = "__gc_deleting"

// AbortedDirName builds the abort-archived directory name (design §11.2).
func AbortedDirName(taskID string, ts time.Time) string {
	return taskID + abortedSuffix + ts.UTC().Format("20060102T150405Z")
}

// GCDeletingDirName builds the GC temporary directory name (design §11.3).
func GCDeletingDirName(abortedName string) string {
	return abortedName + gcDeletingSuffix
}

// ParseAbortedDir extracts the task ID, timestamp, and gc_deleting flag
// from an aborted directory name. Returns ok=false if not an aborted dir.
func ParseAbortedDir(name string) (taskID string, ts time.Time, gcDeleting bool, ok bool) {
	if name == "" {
		return "", time.Time{}, false, false
	}
	gcDeleting = strings.HasSuffix(name, gcDeletingSuffix)
	clean := strings.TrimSuffix(name, gcDeletingSuffix)

	idx := strings.Index(clean, abortedSuffix)
	if idx < 0 {
		return "", time.Time{}, false, false
	}
	taskID = clean[:idx]
	tsStr := clean[idx+len(abortedSuffix):]
	ts, err := time.Parse("20060102T150405Z", tsStr)
	if err != nil {
		return taskID, time.Time{}, gcDeleting, true // valid structure, unparsable timestamp
	}
	return taskID, ts, gcDeleting, true
}

// AbortTask stops a task execution and atomically renames its directory
// to the aborted archive format (design §11.2).
//
// 1. Write abort lifecycle event to events.current.jsonl
// 2. Update task.json status to "done" (aborted)
// 3. Atomic rename: tasks/{task_id}/ → tasks/{task_id}__aborted_{ts}/
//
// Returns the aborted directory name.
func AbortTask(tasksDir, taskID string, ts time.Time) (string, error) {
	srcDir := filepath.Join(tasksDir, taskID)
	if _, err := os.Stat(srcDir); err != nil {
		return "", fmt.Errorf("taskexec: abort %s: source dir: %w", taskID, err)
	}

	// 1. Write abort event
	abortEvent := RawEvent{
		ID:         fmt.Sprintf("abort-%s-%d", taskID, ts.UnixNano()),
		EventType:  "lifecycle",
		Payload:    mustMarshal(map[string]string{"event": "aborted"}),
		OccurredAt: ts.UTC(),
	}
	w := NewEventStreamWriter()
	if err := w.Append(srcDir, abortEvent); err != nil {
		return "", fmt.Errorf("taskexec: abort %s: write event: %w", taskID, err)
	}

	// 2. Atomic rename
	abortedName := AbortedDirName(taskID, ts)
	dstDir := filepath.Join(tasksDir, abortedName)
	if err := os.Rename(srcDir, dstDir); err != nil {
		return "", fmt.Errorf("taskexec: abort %s: rename: %w", taskID, err)
	}
	return abortedName, nil
}

func mustMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/taskexec/ -run 'TestAbort' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/workerdaemon/taskexec/
git commit -m "feat(taskexec): add task abort flow with atomic rename (design §11.2)

AbortTask writes lifecycle event then renames tasks/{id}/ to
tasks/{id}__aborted_{ts}/. ParseAbortedDir extracts components.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Aborted Task Garbage Collection

Implement the Worker GC for aborted (7d) and done (3d) task directories (design §11.3).

**Files:**
- Create: `internal/workerdaemon/taskexec/gc.go` — GC logic
- Create: `internal/workerdaemon/taskexec/gc_test.go` — tests

**Interfaces:**
- Consumes: `ParseAbortedDir`, `GCDeletingDirName`, `IsStandardTaskDir` from Tasks 4, 6
- Produces: `GCConfig{AbortedRetention, DoneRetention}`, `RunGC(tasksDir, cfg, now) GCResult`

- [ ] **Step 1: Write failing tests**

```go
// internal/workerdaemon/taskexec/gc_test.go
package taskexec

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunGC_CleansExpiredAborted(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	os.MkdirAll(tasksDir, 0o700)

	// Create an aborted dir older than retention (8 days ago)
	oldTs := time.Now().Add(-8 * 24 * time.Hour)
	oldName := AbortedDirName("task-old", oldTs)
	os.MkdirAll(filepath.Join(tasksDir, oldName), 0o700)

	// Create a recent aborted dir (1 day ago) — should NOT be cleaned
	recentTs := time.Now().Add(-1 * 24 * time.Hour)
	recentName := AbortedDirName("task-recent", recentTs)
	os.MkdirAll(filepath.Join(tasksDir, recentName), 0o700)

	cfg := GCConfig{AbortedRetention: 7 * 24 * time.Hour, DoneRetention: 3 * 24 * time.Hour}
	result := RunGC(tasksDir, cfg, time.Now())

	if result.AbortedCleaned != 1 {
		t.Errorf("AbortedCleaned = %d, want 1", result.AbortedCleaned)
	}
	// Old dir gone
	if _, err := os.Stat(filepath.Join(tasksDir, oldName)); !os.IsNotExist(err) {
		t.Error("old aborted dir should be gone")
	}
	// Recent dir still there
	if _, err := os.Stat(filepath.Join(tasksDir, recentName)); err != nil {
		t.Error("recent aborted dir should still exist")
	}
}

func TestRunGC_CleansExpiredDone(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	old := time.Now().Add(-4 * 24 * time.Hour)
	// Create a done task dir
	meta := TaskExecutionMeta{TaskID: "task-done", Status: StatusDone, CreatedAt: old, UpdatedAt: old}
	dm.Create(tasksDir, meta, ExecutionContext{})

	cfg := GCConfig{AbortedRetention: 7 * 24 * time.Hour, DoneRetention: 3 * 24 * time.Hour}
	result := RunGC(tasksDir, cfg, time.Now())

	if result.DoneCleaned != 1 {
		t.Errorf("DoneCleaned = %d, want 1", result.DoneCleaned)
	}
}

func TestRunGC_SkipsRunningTasks(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	old := time.Now().Add(-10 * 24 * time.Hour)
	meta := TaskExecutionMeta{TaskID: "task-active", Status: StatusRunning, CreatedAt: old, UpdatedAt: old}
	dm.Create(tasksDir, meta, ExecutionContext{})

	cfg := GCConfig{AbortedRetention: 7 * 24 * time.Hour, DoneRetention: 3 * 24 * time.Hour}
	result := RunGC(tasksDir, cfg, time.Now())

	if result.DoneCleaned != 0 {
		t.Errorf("DoneCleaned = %d, want 0 (running task not cleaned)", result.DoneCleaned)
	}
}

func TestRunGC_HandlesGCDeletingLeftover(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	os.MkdirAll(tasksDir, 0o700)
	// Create a gc_deleting leftover (previous GC crashed)
	leftover := "task-x__aborted_20260601T000000Z__gc_deleting"
	os.MkdirAll(filepath.Join(tasksDir, leftover), 0o700)

	cfg := GCConfig{AbortedRetention: 7 * 24 * time.Hour}
	result := RunGC(tasksDir, cfg, time.Now())

	if result.LeftoverCleaned != 1 {
		t.Errorf("LeftoverCleaned = %d, want 1", result.LeftoverCleaned)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/taskexec/ -run 'TestRunGC' -v`
Expected: FAIL

- [ ] **Step 3: Implement GC**

```go
// internal/workerdaemon/taskexec/gc.go
package taskexec

import (
	"os"
	"path/filepath"
	"time"
)

// GCConfig controls retention periods (design §11.3).
type GCConfig struct {
	AbortedRetention time.Duration // default 7d
	DoneRetention    time.Duration // default 3d
}

// DefaultGCConfig returns the design-specified defaults.
func DefaultGCConfig() GCConfig {
	return GCConfig{
		AbortedRetention: 7 * 24 * time.Hour,
		DoneRetention:    3 * 24 * time.Hour,
	}
}

// GCResult reports what was cleaned.
type GCResult struct {
	AbortedCleaned  int
	DoneCleaned     int
	LeftoverCleaned int
	Errors          []error
}

// RunGC scans tasksDir and removes expired directories (design §11.3).
//
// Three categories:
// 1. __gc_deleting leftovers → always remove (previous GC crash recovery)
// 2. __aborted_ directories → remove if older than AbortedRetention
// 3. Standard task dirs with status=done → remove if UpdatedAt older than DoneRetention
func RunGC(tasksDir string, cfg GCConfig, now time.Time) GCResult {
	var result GCResult
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if !os.IsNotExist(err) {
			result.Errors = append(result.Errors, err)
		}
		return result
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()

		// 1. gc_deleting leftovers: always clean up
		_, _, gcDeleting, isAborted := ParseAbortedDir(name)
		if gcDeleting {
			if err := os.RemoveAll(filepath.Join(tasksDir, name)); err != nil {
				result.Errors = append(result.Errors, err)
			} else {
				result.LeftoverCleaned++
			}
			continue
		}

		// 2. Aborted directories: check retention
		if isAborted {
			_, abortTs, _, _ := ParseAbortedDir(name)
			if !abortTs.IsZero() && now.Sub(abortTs) > cfg.AbortedRetention {
				// Two-phase delete: rename to gc_deleting, then remove
				gcName := GCDeletingDirName(name)
				src := filepath.Join(tasksDir, name)
				dst := filepath.Join(tasksDir, gcName)
				if err := os.Rename(src, dst); err != nil {
					result.Errors = append(result.Errors, err)
					continue
				}
				if err := os.RemoveAll(dst); err != nil {
					result.Errors = append(result.Errors, err)
				} else {
					result.AbortedCleaned++
				}
			}
			continue
		}

		// 3. Standard task dirs: check if done + expired
		if IsStandardTaskDir(name) {
			metaPath := filepath.Join(tasksDir, name, taskMetaFile)
			var meta TaskExecutionMeta
			if err := readJSON(metaPath, &meta); err != nil {
				continue // skip unreadable
			}
			if meta.Status == StatusDone && !meta.UpdatedAt.IsZero() && now.Sub(meta.UpdatedAt) > cfg.DoneRetention {
				if err := os.RemoveAll(filepath.Join(tasksDir, name)); err != nil {
					result.Errors = append(result.Errors, err)
				} else {
					result.DoneCleaned++
				}
			}
		}
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/taskexec/ -run 'TestRunGC' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/workerdaemon/taskexec/
git commit -m "feat(taskexec): add aborted/done task garbage collection (design §11.3)

RunGC cleans expired aborted dirs (7d default), done dirs (3d default),
and gc_deleting leftovers. Two-phase delete via rename + RemoveAll.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Plans Local Directory

Implement the `plans/{plan_id}/plan.json` local metadata store (design §3).

**Files:**
- Create: `internal/workerdaemon/taskexec/planmeta.go` — plan metadata CRUD
- Create: `internal/workerdaemon/taskexec/planmeta_test.go` — tests

**Interfaces:**
- Produces: `PlanMeta{PlanID, Notes, FailureLog}`, `WritePlanMeta(plansDir, meta)`, `ReadPlanMeta(plansDir, planID)`, `ListPlanMetas(plansDir)`

- [ ] **Step 1: Write failing tests**

```go
// internal/workerdaemon/taskexec/planmeta_test.go
package taskexec

import (
	"path/filepath"
	"testing"
)

func TestPlanMeta_WriteAndRead(t *testing.T) {
	plansDir := filepath.Join(t.TempDir(), "plans")
	meta := PlanMeta{
		PlanID: "plan-abc",
		Notes:  "some lessons learned",
	}
	if err := WritePlanMeta(plansDir, meta); err != nil {
		t.Fatalf("WritePlanMeta: %v", err)
	}
	got, err := ReadPlanMeta(plansDir, "plan-abc")
	if err != nil {
		t.Fatalf("ReadPlanMeta: %v", err)
	}
	if got.PlanID != "plan-abc" || got.Notes != "some lessons learned" {
		t.Errorf("got = %+v", got)
	}
}

func TestPlanMeta_ReadMissing(t *testing.T) {
	plansDir := filepath.Join(t.TempDir(), "plans")
	_, err := ReadPlanMeta(plansDir, "nonexistent")
	if err == nil {
		t.Error("expected error for missing plan")
	}
}

func TestListPlanMetas(t *testing.T) {
	plansDir := filepath.Join(t.TempDir(), "plans")
	WritePlanMeta(plansDir, PlanMeta{PlanID: "p-1", Notes: "a"})
	WritePlanMeta(plansDir, PlanMeta{PlanID: "p-2", Notes: "b"})
	metas := ListPlanMetas(plansDir)
	if len(metas) != 2 {
		t.Fatalf("ListPlanMetas = %d, want 2", len(metas))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/taskexec/ -run 'TestPlanMeta|TestListPlanMetas' -v`
Expected: FAIL

- [ ] **Step 3: Implement plan metadata**

```go
// internal/workerdaemon/taskexec/planmeta.go
package taskexec

import (
	"fmt"
	"os"
	"path/filepath"
)

const planMetaFile = "plan.json"

// PlanMeta is the plan-level local metadata (design §3: "Plan 级公共信息").
type PlanMeta struct {
	PlanID     string   `json:"plan_id"`
	Notes      string   `json:"notes,omitempty"`
	FailureLog []string `json:"failure_log,omitempty"`
}

// WritePlanMeta persists plan metadata to plans/{plan_id}/plan.json.
func WritePlanMeta(plansDir string, meta PlanMeta) error {
	if meta.PlanID == "" {
		return fmt.Errorf("taskexec: plan_id required")
	}
	dir := filepath.Join(plansDir, meta.PlanID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("taskexec: mkdir plan %s: %w", meta.PlanID, err)
	}
	return writeJSONAtomic(filepath.Join(dir, planMetaFile), meta)
}

// ReadPlanMeta reads plan metadata from plans/{plan_id}/plan.json.
func ReadPlanMeta(plansDir, planID string) (PlanMeta, error) {
	var meta PlanMeta
	path := filepath.Join(plansDir, planID, planMetaFile)
	if err := readJSON(path, &meta); err != nil {
		return PlanMeta{}, fmt.Errorf("taskexec: read plan %s: %w", planID, err)
	}
	return meta, nil
}

// ListPlanMetas lists all plan metadata in plansDir.
func ListPlanMetas(plansDir string) []PlanMeta {
	entries, err := os.ReadDir(plansDir)
	if err != nil {
		return nil
	}
	var result []PlanMeta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var meta PlanMeta
		if err := readJSON(filepath.Join(plansDir, e.Name(), planMetaFile), &meta); err != nil {
			continue
		}
		result = append(result, meta)
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/taskexec/ -run 'TestPlanMeta|TestListPlanMetas' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/workerdaemon/taskexec/
git commit -m "feat(taskexec): add plan-level local metadata (design §3)

WritePlanMeta/ReadPlanMeta/ListPlanMetas for plans/{plan_id}/plan.json.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Boot Task Reconciliation

Implement the tasks/ scan and Center verification at Agent CLI start (design §3.2): scan standard task directories, verify each against Center, abort orphans.

**Files:**
- Create: `internal/workerdaemon/taskexec/bootreconcile.go` — boot-time task reconciliation
- Create: `internal/workerdaemon/taskexec/bootreconcile_test.go` — tests

**Interfaces:**
- Consumes: `DirManager.ScanActive`, `AbortTask` from Tasks 4, 6
- Produces: `TaskVerifier` interface (port for Center API), `ReconcileTasksOnBoot(tasksDir, verifier, agentID) ReconcileResult`

- [ ] **Step 1: Write failing tests**

```go
// internal/workerdaemon/taskexec/bootreconcile_test.go
package taskexec

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// fakeVerifier simulates Center API responses.
type fakeVerifier struct {
	assigned map[string]bool // taskID → still assigned to this agent
}

func (f *fakeVerifier) IsTaskAssigned(ctx context.Context, agentID, taskID string) (bool, error) {
	return f.assigned[taskID], nil
}

func TestReconcileTasksOnBoot_KeepsAssigned(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Now().UTC()
	dm.Create(tasksDir, TaskExecutionMeta{TaskID: "t-1", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}, ExecutionContext{})

	v := &fakeVerifier{assigned: map[string]bool{"t-1": true}}
	result := ReconcileTasksOnBoot(context.Background(), tasksDir, v, "agent-1", now)

	if result.Kept != 1 {
		t.Errorf("Kept = %d, want 1", result.Kept)
	}
	if result.Aborted != 0 {
		t.Errorf("Aborted = %d, want 0", result.Aborted)
	}
}

func TestReconcileTasksOnBoot_AbortsReassigned(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Now().UTC()
	dm.Create(tasksDir, TaskExecutionMeta{TaskID: "t-1", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}, ExecutionContext{})

	v := &fakeVerifier{assigned: map[string]bool{"t-1": false}}
	result := ReconcileTasksOnBoot(context.Background(), tasksDir, v, "agent-1", now)

	if result.Aborted != 1 {
		t.Errorf("Aborted = %d, want 1", result.Aborted)
	}
}

func TestReconcileTasksOnBoot_EmptyTasksDir(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	v := &fakeVerifier{}
	result := ReconcileTasksOnBoot(context.Background(), tasksDir, v, "agent-1", time.Now())
	if result.Kept != 0 && result.Aborted != 0 {
		t.Errorf("expected zero result, got %+v", result)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/taskexec/ -run 'TestReconcileTasksOnBoot' -v`
Expected: FAIL

- [ ] **Step 3: Implement boot reconciliation**

```go
// internal/workerdaemon/taskexec/bootreconcile.go
package taskexec

import (
	"context"
	"time"
)

// TaskVerifier is the port for checking task assignment with Center (design §3.2).
type TaskVerifier interface {
	IsTaskAssigned(ctx context.Context, agentID, taskID string) (bool, error)
}

// ReconcileResult reports the boot reconciliation outcome.
type ReconcileResult struct {
	Kept    int
	Aborted int
	Errors  []error
}

// ReconcileTasksOnBoot scans tasks/ for standard execution directories,
// verifies each against the Center, and aborts orphaned tasks (design §3.2).
func ReconcileTasksOnBoot(ctx context.Context, tasksDir string, verifier TaskVerifier, agentID string, now time.Time) ReconcileResult {
	var result ReconcileResult
	dm := NewDirManager()
	active := dm.ScanActive(tasksDir)

	for _, meta := range active {
		assigned, err := verifier.IsTaskAssigned(ctx, agentID, meta.TaskID)
		if err != nil {
			result.Errors = append(result.Errors, err)
			continue
		}
		if assigned {
			result.Kept++
			continue
		}
		// Task no longer assigned to this agent → abort
		if _, err := AbortTask(tasksDir, meta.TaskID, now); err != nil {
			result.Errors = append(result.Errors, err)
			continue
		}
		result.Aborted++
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/taskexec/ -run 'TestReconcileTasksOnBoot' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/workerdaemon/taskexec/
git commit -m "feat(taskexec): add boot-time task reconciliation (design §3.2)

ReconcileTasksOnBoot scans tasks/ for standard dirs, verifies each
against Center via TaskVerifier port, aborts orphaned tasks.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Integration — Wire Task Infrastructure Into AgentController

Connect the new task execution infrastructure (Tasks 2-9) into the existing `AgentController` workflow: create task dirs on `agent.work`, manage lifecycle, use session instance lease.

**Files:**
- Modify: `internal/workerdaemon/agent_controller.go` — integrate taskexec + sessioninstance
- Modify: `internal/workerdaemon/boot_reconcile.go` — add task-level boot reconcile
- Test: `internal/workerdaemon/agent_controller_test.go` — integration tests

**Interfaces:**
- Consumes: All interfaces from Tasks 1-9
- Produces: Updated `AgentController` that manages task execution directories

- [ ] **Step 1: Add taskexec imports and DirManager to AgentControllerConfig**

```go
// internal/workerdaemon/agent_controller.go — add to AgentControllerConfig struct
	// TaskDirManager manages per-task execution directories. Nil → task
	// directory management disabled (backwards-compatible).
	TaskDirManager *taskexec.DirManager
	// TaskVerifier checks task assignment with Center for boot reconcile.
	// Nil → boot task reconcile skipped.
	TaskVerifier taskexec.TaskVerifier
```

- [ ] **Step 2: Create task directory on agent.work command**

In the `handleWork` method (the handler for `cmdTypeAgentWork`), after validating the work payload, create the task execution directory:

```go
// In handleWork, after parsing workPayload and before injecting the brief:
if c.cfg.TaskDirManager != nil {
	home, tasksDir, _, err := c.agentPaths(pl.AgentID)
	if err == nil {
		now := c.now()
		meta := taskexec.TaskExecutionMeta{
			TaskID:    pl.TaskID,
			Status:    taskexec.StatusPending,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if createErr := c.cfg.TaskDirManager.Create(tasksDir, meta, taskexec.ExecutionContext{}); createErr != nil {
			c.log("agent=%s task=%s create task dir: %v", pl.AgentID, pl.TaskID, createErr)
		} else {
			c.log("agent=%s task=%s task dir created at %s", pl.AgentID, pl.TaskID, filepath.Join(tasksDir, pl.TaskID))
		}
		_ = home // used by tasksDir derivation
	}
}
```

- [ ] **Step 3: Add task-level boot reconcile to ReconcileOnBoot**

In `ReconcileOnBoot`, after the existing agent-level reconciliation, add task directory reconciliation:

```go
// At the end of ReconcileOnBoot, after the per-agent loop:
if c.cfg.TaskDirManager != nil && c.cfg.TaskVerifier != nil {
	for id := range union {
		home, tasksDir, _, perr := c.agentPaths(id)
		if perr != nil {
			continue
		}
		_ = home
		result := taskexec.ReconcileTasksOnBoot(ctx, tasksDir, c.cfg.TaskVerifier, id, c.now())
		if result.Aborted > 0 || len(result.Errors) > 0 {
			c.log("boot-reconcile agent=%s tasks: kept=%d aborted=%d errors=%d",
				id, result.Kept, result.Aborted, len(result.Errors))
		}
	}
}
```

- [ ] **Step 4: Integrate session instance into session start**

In `startSession`, acquire the session instance lease before spawning the supervisor:

```go
// In startSession, after acquiring home lock and before spawning:
inst, instErr := sessioninstance.AcquireInstance(home, sessionID, os.Getpid())
if instErr != nil {
	c.log("agent=%s session instance acquire: %v", agentID, instErr)
	// Non-fatal: proceed without lease tracking
}
if inst.Generation > 0 {
	c.log("agent=%s session instance generation=%d (prev_pid=%d)", agentID, inst.Generation, inst.PrevPID)
}
```

- [ ] **Step 5: Run all workerdaemon tests**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/... -v -count=1 -timeout=120s`
Expected: PASS

- [ ] **Step 6: Run full build**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 7: Commit**

```bash
git add internal/workerdaemon/
git commit -m "feat(workerdaemon): wire task execution + session instance into AgentController

Create task dirs on agent.work, run task-level boot reconcile,
acquire session instance lease on session start.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: Run Full Test Suite + Coverage Report

Verify all changes work together, measure coverage, and ensure no regressions.

**Files:**
- All modified and new files from Tasks 1-10

- [ ] **Step 1: Run full test suite**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./... -count=1 -timeout=300s`
Expected: PASS (no regressions)

- [ ] **Step 2: Measure coverage for new packages**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go test ./internal/workerdaemon/taskexec/ ./internal/workerdaemon/sessioninstance/ -cover -coverprofile=coverage.out -count=1`
Expected: ≥ 90% line coverage for both packages

- [ ] **Step 3: Check for uncovered paths**

Run: `cd /Users/aaronlin/works/codes/oss/agent-center && go tool cover -func=coverage.out | grep -v '100.0%' | head -20`
Expected: All critical paths covered. If any function is below 90%, add targeted tests.

- [ ] **Step 4: Clean up coverage file**

Run: `rm coverage.out`

- [ ] **Step 5: Final commit (if any coverage gaps were filled)**

```bash
git add .
git commit -m "test: fill coverage gaps for taskexec + sessioninstance

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```
