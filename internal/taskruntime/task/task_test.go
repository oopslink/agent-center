package task

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime"
)

var ref = time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

func mkOpen(t *testing.T) *Task {
	t.Helper()
	task, err := New(NewInput{
		ID:        "T-1",
		ProjectID: "P-1",
		Title:     "test",
		CreatedBy: "user:hayang",
		Priority:  PriorityMedium,
		Now:       ref,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return task
}

func TestNew_Happy(t *testing.T) {
	task := mkOpen(t)
	if task.Status() != StatusOpen {
		t.Fatalf("expected open, got %s", task.Status())
	}
	if task.Version() != 1 {
		t.Fatalf("version: %d", task.Version())
	}
}

func TestNew_RejectsMissing(t *testing.T) {
	cases := []struct {
		name string
		in   NewInput
		want string
	}{
		{"no id", NewInput{ProjectID: "P", Title: "t", CreatedBy: "u", Now: ref}, "id required"},
		{"no project", NewInput{ID: "T", Title: "t", CreatedBy: "u", Now: ref}, "project_id"},
		{"no title", NewInput{ID: "T", ProjectID: "P", CreatedBy: "u", Now: ref}, "title"},
		{"no created_by", NewInput{ID: "T", ProjectID: "P", Title: "t", Now: ref}, "created_by"},
		{"no now", NewInput{ID: "T", ProjectID: "P", Title: "t", CreatedBy: "u"}, "now"},
		{"bad priority", NewInput{ID: "T", ProjectID: "P", Title: "t", CreatedBy: "u", Priority: "lol", Now: ref}, "invalid priority"},
		{"self-dep", NewInput{ID: "T", ProjectID: "P", Title: "t", CreatedBy: "u", Now: ref, DependsOnTaskIDs: []taskruntime.TaskID{"T"}}, "self-dependency"},
		{"empty dep", NewInput{ID: "T", ProjectID: "P", Title: "t", CreatedBy: "u", Now: ref, DependsOnTaskIDs: []taskruntime.TaskID{""}}, "empty task id"},
		{"dup dep", NewInput{ID: "T", ProjectID: "P", Title: "t", CreatedBy: "u", Now: ref, DependsOnTaskIDs: []taskruntime.TaskID{"X", "X"}}, "duplicate dep"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := New(c.in)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(c.want)) {
				t.Fatalf("err %v should contain %q", err, c.want)
			}
		})
	}
}

func TestSuspend_Happy(t *testing.T) {
	task := mkOpen(t)
	if err := task.Suspend(ref.Add(time.Second)); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if task.Status() != StatusSuspended {
		t.Fatalf("status: %s", task.Status())
	}
	if task.Version() != 2 {
		t.Fatalf("version: %d", task.Version())
	}
}

func TestSuspend_RejectsTerminal(t *testing.T) {
	task := mkOpen(t)
	if err := task.MarkDone(ref); err != nil {
		t.Fatal(err)
	}
	if err := task.Suspend(ref); !errors.Is(err, ErrTaskInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}
}

func TestResume_Happy(t *testing.T) {
	task := mkOpen(t)
	_ = task.Suspend(ref)
	if err := task.Resume(ref.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if task.Status() != StatusOpen {
		t.Fatalf("status: %s", task.Status())
	}
}

func TestResume_NonSuspended(t *testing.T) {
	task := mkOpen(t)
	if err := task.Resume(ref); !errors.Is(err, ErrTaskInvalidTransition) {
		t.Fatalf("err: %v", err)
	}
}

func TestAbandon_HappyAndInvariants(t *testing.T) {
	task := mkOpen(t)
	if err := task.Abandon("user_request", "no longer needed", ref); err != nil {
		t.Fatal(err)
	}
	if task.Status() != StatusAbandoned {
		t.Fatalf("status: %s", task.Status())
	}
	if task.AbandonedReason() != "user_request" {
		t.Fatalf("reason: %s", task.AbandonedReason())
	}
	if !task.IsTerminal() {
		t.Fatalf("expected terminal")
	}
}

func TestAbandon_RequiresReasonMessage(t *testing.T) {
	task := mkOpen(t)
	if err := task.Abandon("", "m", ref); err == nil {
		t.Fatal("expected reason required")
	}
	if err := task.Abandon("r", "", ref); err == nil {
		t.Fatal("expected message required")
	}
}

func TestAbandon_RejectsTerminal(t *testing.T) {
	task := mkOpen(t)
	_ = task.MarkDone(ref)
	if err := task.Abandon("r", "m", ref); !errors.Is(err, ErrTaskInvalidTransition) {
		t.Fatalf("err: %v", err)
	}
}

func TestMarkDone_Happy(t *testing.T) {
	task := mkOpen(t)
	if err := task.MarkDone(ref); err != nil {
		t.Fatal(err)
	}
	if !task.IsTerminal() {
		t.Fatal("expected terminal")
	}
}

func TestMarkDone_RejectsTerminal(t *testing.T) {
	task := mkOpen(t)
	_ = task.MarkDone(ref)
	if err := task.MarkDone(ref); !errors.Is(err, ErrTaskInvalidTransition) {
		t.Fatalf("err: %v", err)
	}
}

func TestBindConversation_HappyAndUnbindRejected(t *testing.T) {
	task := mkOpen(t)
	if err := task.BindConversation("C-1", ref); err != nil {
		t.Fatal(err)
	}
	if task.ConversationID() != "C-1" {
		t.Fatalf("conv id: %s", task.ConversationID())
	}
	if err := task.BindConversation("C-2", ref); !errors.Is(err, ErrCannotUnbindConversation) {
		t.Fatalf("expected ErrCannotUnbindConversation, got %v", err)
	}
	if err := task.BindConversation("", ref); err == nil {
		t.Fatal("expected error on empty conv id")
	}
}

func TestSetCurrentExecutionID_HappyAndTerminal(t *testing.T) {
	task := mkOpen(t)
	if err := task.SetCurrentExecutionID("E-1", ref); err != nil {
		t.Fatal(err)
	}
	if task.CurrentExecutionID() != "E-1" {
		t.Fatalf("exec id: %s", task.CurrentExecutionID())
	}
	if !task.HasActiveExecution() {
		t.Fatal("expected active")
	}
	_ = task.MarkDone(ref)
	if err := task.SetCurrentExecutionID("E-2", ref); !errors.Is(err, ErrTaskInvalidTransition) {
		t.Fatalf("err: %v", err)
	}
}

func TestClearCurrentExecutionID(t *testing.T) {
	task := mkOpen(t)
	_ = task.SetCurrentExecutionID("E-1", ref)
	task.ClearCurrentExecutionID(ref.Add(time.Second))
	if task.HasActiveExecution() {
		t.Fatal("expected no active")
	}
}

func TestUpdatePriority(t *testing.T) {
	task := mkOpen(t)
	if err := task.UpdatePriority(PriorityHigh, ref); err != nil {
		t.Fatal(err)
	}
	if task.Priority() != PriorityHigh {
		t.Fatalf("priority: %s", task.Priority())
	}
	if err := task.UpdatePriority("lol", ref); err == nil {
		t.Fatal("expected invalid")
	}
	_ = task.MarkDone(ref)
	if err := task.UpdatePriority(PriorityLow, ref); !errors.Is(err, ErrTaskInvalidTransition) {
		t.Fatalf("err: %v", err)
	}
}

func TestUpdateMetadata_Happy(t *testing.T) {
	tk := mkOpen(t)
	if err := tk.UpdateMetadata("new title", "new desc", PriorityHigh, ref); err != nil {
		t.Fatal(err)
	}
	if tk.Title() != "new title" {
		t.Fatalf("title=%q", tk.Title())
	}
	if tk.Description() != "new desc" {
		t.Fatalf("description=%q", tk.Description())
	}
	if tk.Priority() != PriorityHigh {
		t.Fatalf("priority=%s", tk.Priority())
	}
	if tk.Version() != 2 {
		t.Fatalf("version=%d", tk.Version())
	}
}

func TestUpdateMetadata_RejectsEmptyTitle(t *testing.T) {
	tk := mkOpen(t)
	if err := tk.UpdateMetadata("   ", "x", PriorityHigh, ref); err == nil ||
		!strings.Contains(err.Error(), "title required") {
		t.Fatalf("err=%v", err)
	}
}

func TestUpdateMetadata_RejectsInvalidPriority(t *testing.T) {
	tk := mkOpen(t)
	if err := tk.UpdateMetadata("ok", "x", "bogus", ref); !errors.Is(err, ErrInvalidPriority) {
		t.Fatalf("err=%v", err)
	}
}

func TestUpdateMetadata_RejectsTerminal(t *testing.T) {
	tk := mkOpen(t)
	if err := tk.MarkDone(ref); err != nil {
		t.Fatal(err)
	}
	if err := tk.UpdateMetadata("new", "x", PriorityHigh, ref); !errors.Is(err, ErrTaskInvalidTransition) {
		t.Fatalf("err=%v", err)
	}
}

func TestUpdateDependencies_ActiveExecutionBlocks(t *testing.T) {
	task := mkOpen(t)
	_ = task.SetCurrentExecutionID("E-1", ref)
	if err := task.UpdateDependencies([]taskruntime.TaskID{"T-2"}, ref); !errors.Is(err, ErrTaskInvariantViolation) {
		t.Fatalf("err: %v", err)
	}
	task.ClearCurrentExecutionID(ref)
	if err := task.UpdateDependencies([]taskruntime.TaskID{"T-2"}, ref); err != nil {
		t.Fatalf("update deps: %v", err)
	}
	if len(task.DependsOnTaskIDs()) != 1 {
		t.Fatalf("len: %d", len(task.DependsOnTaskIDs()))
	}
}

func TestSetRequiresWorktree(t *testing.T) {
	task := mkOpen(t)
	if err := task.SetRequiresWorktree(false, ref); err != nil {
		t.Fatal(err)
	}
	if task.RequiresWorktree() {
		t.Fatal("expected false")
	}
	_ = task.SetCurrentExecutionID("E-1", ref)
	if err := task.SetRequiresWorktree(true, ref); !errors.Is(err, ErrTaskInvariantViolation) {
		t.Fatalf("err: %v", err)
	}
}

func TestRehydrate_RoundTrip(t *testing.T) {
	in := RehydrateInput{
		ID:               "T-1",
		ProjectID:        "P-1",
		Title:            "t",
		Status:           StatusOpen,
		Priority:         PriorityHigh,
		RequiresWorktree: true,
		CreatedBy:        "user:hayang",
		CreatedAt:        ref,
		UpdatedAt:        ref,
		Version:          1,
	}
	t1, err := Rehydrate(in)
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	if t1.ID() != "T-1" || t1.Priority() != PriorityHigh {
		t.Fatalf("rehydrate state: %+v", t1)
	}
	in.Status = "BOGUS"
	if _, err := Rehydrate(in); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus")
	}
	in.Status = StatusOpen
	in.Priority = "WAT"
	if _, err := Rehydrate(in); !errors.Is(err, ErrInvalidPriority) {
		t.Fatalf("expected ErrInvalidPriority")
	}
	in.Priority = PriorityHigh
	in.Version = 0
	if _, err := Rehydrate(in); err == nil {
		t.Fatal("expected version error")
	}
}

func TestGetters_DefensiveCopy(t *testing.T) {
	task := mkOpen(t)
	// Force EtaAt + Deps
	eta := ref.Add(time.Hour)
	tt, err := New(NewInput{
		ID:                "T-1",
		ProjectID:         "P-1",
		Title:             "t",
		CreatedBy:         "u:1",
		Priority:          PriorityMedium,
		EtaAt:             &eta,
		DependsOnTaskIDs:  []taskruntime.TaskID{"T-2"},
		Now:               ref,
	})
	if err != nil {
		t.Fatal(err)
	}
	deps := tt.DependsOnTaskIDs()
	deps[0] = "MUTATED"
	if tt.DependsOnTaskIDs()[0] == "MUTATED" {
		t.Fatal("expected defensive copy")
	}
	got := tt.EtaAt()
	if got == nil || !got.Equal(eta) {
		t.Fatalf("eta: %v", got)
	}
	*got = ref
	if tt.EtaAt().Equal(ref) {
		t.Fatal("expected defensive copy")
	}
	if task.IsTerminal() {
		t.Fatal("open should not be terminal")
	}
}

func TestStatusAndPriority_Enums(t *testing.T) {
	if !StatusOpen.IsValid() || !StatusAbandoned.IsTerminal() {
		t.Fatal("enum mismatch")
	}
	if PriorityLow.String() != "low" {
		t.Fatal("low")
	}
	if _, err := ParsePriority("medium"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePriority("foo"); !errors.Is(err, ErrInvalidPriority) {
		t.Fatalf("expected ErrInvalidPriority")
	}
}
