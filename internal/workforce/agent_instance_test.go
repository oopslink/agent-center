package workforce

import (
	"errors"
	"testing"
	"time"
)

func wid(s string) *WorkerID { v := WorkerID(s); return &v }

func intPtr(v int) *int { return &v }

func TestNewAgentInstance_Valid(t *testing.T) {
	a, err := NewAgentInstance(NewAgentInstanceInput{
		ID:        "01HAGI",
		Name:      "coder-mbp",
		AgentCLI:  "claude-code",
		WorkerID:  wid("W-1"),
		Config:    `{"a":1}`,
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("NewAgentInstance: %v", err)
	}
	if a.State() != AgentInstanceIdle {
		t.Fatalf("state: %s", a.State())
	}
	if a.Version() != 1 {
		t.Fatalf("version: %d", a.Version())
	}
	if a.IsBuiltin() {
		t.Fatal("should not be builtin")
	}
}

func TestNewAgentInstance_BuiltinNoWorkerID(t *testing.T) {
	a, err := NewAgentInstance(NewAgentInstanceInput{
		ID: "01HSUP", Name: "supervisor", AgentCLI: "claude-code",
		WorkerID: nil, IsBuiltin: true, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !a.IsBuiltin() {
		t.Fatal()
	}
	if a.WorkerID() != nil {
		t.Fatal("builtin should have nil worker_id")
	}
}

func TestNewAgentInstance_XORViolations(t *testing.T) {
	// Builtin + worker_id supplied → reject.
	if _, err := NewAgentInstance(NewAgentInstanceInput{
		ID: "01H", Name: "x", AgentCLI: "c",
		WorkerID: wid("W-1"), IsBuiltin: true,
		CreatedAt: time.Now(),
	}); err == nil {
		t.Fatal("builtin+worker_id should reject")
	}
	// Non-builtin + nil worker_id → reject.
	if _, err := NewAgentInstance(NewAgentInstanceInput{
		ID: "01H", Name: "x", AgentCLI: "c",
		WorkerID: nil, IsBuiltin: false,
		CreatedAt: time.Now(),
	}); err == nil {
		t.Fatal("non-builtin+nil worker_id should reject")
	}
}

func TestNewAgentInstance_NameValidation(t *testing.T) {
	for _, badName := range []string{"", "  ", "with spaces", "@bad"} {
		if _, err := NewAgentInstance(NewAgentInstanceInput{
			ID: "01H", Name: badName, AgentCLI: "c",
			WorkerID: wid("W-1"), CreatedAt: time.Now(),
		}); err == nil {
			t.Fatalf("name %q should reject", badName)
		}
	}
}

func TestNewAgentInstance_MaxConcurrentValidation(t *testing.T) {
	zero := 0
	if _, err := NewAgentInstance(NewAgentInstanceInput{
		ID: "01H", Name: "x", AgentCLI: "c",
		WorkerID: wid("W-1"), MaxConcurrent: &zero,
		CreatedAt: time.Now(),
	}); err == nil {
		t.Fatal("max_concurrent=0 should reject")
	}
}

func TestAgentInstance_MarkActive(t *testing.T) {
	a := freshAI(t)
	if err := a.MarkActive(); err != nil {
		t.Fatal(err)
	}
	if a.State() != AgentInstanceActive {
		t.Fatalf("state: %s", a.State())
	}
	// Idempotent.
	if err := a.MarkActive(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentInstance_MarkIdle(t *testing.T) {
	a := freshAI(t)
	_ = a.MarkActive()
	if err := a.MarkIdle(); err != nil {
		t.Fatal(err)
	}
	if a.State() != AgentInstanceIdle {
		t.Fatalf("state: %s", a.State())
	}
}

func TestAgentInstance_MarkSleeping_Awakened(t *testing.T) {
	a := freshAI(t)
	if err := a.MarkSleeping(); err != nil {
		t.Fatal(err)
	}
	if a.State() != AgentInstanceSleeping {
		t.Fatalf("state: %s", a.State())
	}
	if err := a.MarkAwakened(); err != nil {
		t.Fatal(err)
	}
	if a.State() != AgentInstanceIdle {
		t.Fatalf("state: %s", a.State())
	}
}

func TestAgentInstance_Archive_Happy(t *testing.T) {
	a := freshAI(t)
	if err := a.Archive(time.Now(), AgentInstanceArchivedReasonManual, "user wanted"); err != nil {
		t.Fatal(err)
	}
	if a.State() != AgentInstanceArchived {
		t.Fatalf("state: %s", a.State())
	}
}

func TestAgentInstance_Archive_RejectsBuiltin(t *testing.T) {
	a, _ := NewAgentInstance(NewAgentInstanceInput{
		ID: "01HSUP", Name: "supervisor", AgentCLI: "claude-code",
		IsBuiltin: true, CreatedAt: time.Now(),
	})
	err := a.Archive(time.Now(), AgentInstanceArchivedReasonManual, "test")
	if !errors.Is(err, ErrAgentInstanceIsBuiltin) {
		t.Fatalf("expected is-builtin, got %v", err)
	}
}

func TestAgentInstance_Archive_RejectsActive(t *testing.T) {
	a := freshAI(t)
	_ = a.MarkActive()
	err := a.Archive(time.Now(), AgentInstanceArchivedReasonManual, "test")
	if err == nil {
		t.Fatal("expected error from non-idle archive")
	}
}

func TestAgentInstance_Archive_RejectsSleeping(t *testing.T) {
	a := freshAI(t)
	_ = a.MarkSleeping()
	err := a.Archive(time.Now(), AgentInstanceArchivedReasonManual, "test")
	if err == nil {
		t.Fatal("expected error from sleeping archive")
	}
}

func TestAgentInstance_HomeDirPath(t *testing.T) {
	wA := freshAI(t)
	if wA.HomeDirPath() != "~/.agent-center-worker/agents/"+string(wA.ID())+"/" {
		t.Fatalf("worker home: %s", wA.HomeDirPath())
	}
	builtin, _ := NewAgentInstance(NewAgentInstanceInput{
		ID: "01H", Name: "supervisor", AgentCLI: "claude-code",
		IsBuiltin: true, CreatedAt: time.Now(),
	})
	if builtin.HomeDirPath() != "~/.agent-center/agents/supervisor/" {
		t.Fatalf("builtin home: %s", builtin.HomeDirPath())
	}
}

func TestAgentInstance_SetConfig(t *testing.T) {
	a := freshAI(t)
	prevVer := a.Version()
	if err := a.SetConfig(time.Now(), `{"x":2}`); err != nil {
		t.Fatal(err)
	}
	if a.Config() != `{"x":2}` {
		t.Fatalf("config: %s", a.Config())
	}
	if a.Version() != prevVer+1 {
		t.Fatalf("version: %d", a.Version())
	}
}

func TestAgentInstance_SetConfig_AfterArchive(t *testing.T) {
	a := freshAI(t)
	_ = a.Archive(time.Now(), AgentInstanceArchivedReasonManual, "test")
	if err := a.SetConfig(time.Now(), `{"y":3}`); !errors.Is(err, ErrAgentInstanceArchived) {
		t.Fatalf("expected archived, got %v", err)
	}
}

func TestAgentInstanceState_Validity(t *testing.T) {
	for _, s := range []AgentInstanceState{AgentInstanceIdle, AgentInstanceActive, AgentInstanceSleeping, AgentInstanceArchived} {
		if !s.IsValid() {
			t.Fatalf("%s should be valid", s)
		}
	}
	if AgentInstanceState("bogus").IsValid() {
		t.Fatal()
	}
	if !AgentInstanceArchived.IsTerminal() {
		t.Fatal("archived is terminal")
	}
	if AgentInstanceIdle.IsTerminal() {
		t.Fatal("idle not terminal")
	}
}

func freshAI(t *testing.T) *AgentInstance {
	t.Helper()
	a, err := NewAgentInstance(NewAgentInstanceInput{
		ID:        "01HTEST",
		Name:      "test-agent",
		AgentCLI:  "claude-code",
		WorkerID:  wid("W-1"),
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
