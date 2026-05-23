package service

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

func setupConcludeStack(t *testing.T) (*testHarness, *IssueLifecycleService, *trsqlite.TaskRepo) {
	t.Helper()
	h := newHarness(t)
	taskRepo := trsqlite.NewTaskRepo(h.db)
	spawner := dispatch.NewIssueConcludeSpawn(h.db, taskRepo, h.sink, h.gen, h.clk)
	writer := convservice.NewMessageWriter(h.db, h.convRepo, h.msgRepo, h.sink, h.gen, h.clk)
	opener := NewIssueConversationOpener(h.convRepo, h.sink, h.gen, h.clk)
	lifecycle := NewIssueLifecycleService(h.db, h.issueRepo, opener, realMessageWriterAdapter{writer}, h.sink, h.gen, h.clk).
		WithSpawnerAndCommenter(spawner, realMessageWriterAdapter{writer})
	return h, lifecycle, taskRepo
}

func TestConclude_ClosedNoAction(t *testing.T) {
	h, lifecycle, taskRepo := setupConcludeStack(t)
	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	out, err := lifecycle.Conclude(context.Background(), ConcludeIssueCommand{
		IssueID:     res.IssueID,
		Resolution:  discussion.Resolution{Kind: discussion.ResolutionClosedNoAction, Summary: "skip"},
		ConcludedBy: "user:h",
		Actor:       observability.Actor("user:h"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.TaskIDs) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(out.TaskIDs))
	}
	got, _ := h.issueRepo.FindByID(context.Background(), res.IssueID)
	if got.Status() != discussion.StatusClosedNoAction {
		t.Fatalf("status: %s", got.Status())
	}
	if got.ConclusionSummary() != "skip" || got.ConcludedByIdentityID() != "user:h" {
		t.Fatalf("conclusion fields: %+v", got)
	}
	if h.countEvents(t, "issue.concluded") != 1 {
		t.Fatal("issue.concluded not emitted")
	}
	if h.countEvents(t, "task.created") != 0 {
		t.Fatal("no task.created expected")
	}
	tasks, _ := taskRepo.FindByProject(context.Background(), "P-1", task.Filter{})
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks: %d", len(tasks))
	}
}

// taskruntime.TaskFilterRef is unused — keep a no-arg helper in a test
// file is unidiomatic. Use task.Filter{} directly above.
func TestConclude_ClosedWithTasksOneTask(t *testing.T) {
	h, lifecycle, taskRepo := setupConcludeStack(t)
	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginWebConsole, Actor: observability.Actor("user:h"),
	})
	out, err := lifecycle.Conclude(context.Background(), ConcludeIssueCommand{
		IssueID: res.IssueID,
		Resolution: discussion.Resolution{
			Kind:    discussion.ResolutionClosedWithTasks,
			Summary: "go",
			Tasks: []dispatch.IssueConcludeTaskSpec{
				{LocalID: "a", Title: "T-A"},
			},
		},
		ConcludedBy: "user:h",
		Actor:       observability.Actor("user:h"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.TaskIDs) != 1 {
		t.Fatalf("expected 1 task, got %d", len(out.TaskIDs))
	}
	got, _ := h.issueRepo.FindByID(context.Background(), res.IssueID)
	if got.Status() != discussion.StatusClosedWithTasks {
		t.Fatalf("status: %s", got.Status())
	}
	if h.countEvents(t, "issue.concluded") != 1 ||
		h.countEvents(t, "issue.tasks_spawned") != 1 ||
		h.countEvents(t, "task.created") != 1 {
		t.Fatalf("events: concluded=%d tasks_spawned=%d task.created=%d",
			h.countEvents(t, "issue.concluded"),
			h.countEvents(t, "issue.tasks_spawned"),
			h.countEvents(t, "task.created"))
	}
	// system message should be in the conversation
	msgs, _ := h.msgRepo.FindRecent(context.Background(), got.ConversationID(), 10)
	hasSystem := false
	for _, m := range msgs {
		if m.ContentKind() == conversation.MessageContentSystem {
			hasSystem = true
		}
	}
	if !hasSystem {
		t.Fatal("expected system message about spawn")
	}
	// task should be in DB
	tasks, _ := taskRepo.FindByProject(context.Background(), "P-1", task.Filter{})
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].FromIssueID() != string(res.IssueID) {
		t.Fatalf("from_issue_id: %s want %s", tasks[0].FromIssueID(), res.IssueID)
	}
}

func TestConclude_ClosedWithTasks_LazyPathNoSystemMessage(t *testing.T) {
	h, lifecycle, _ := setupConcludeStack(t)
	// CLI path (no conv bound)
	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	out, err := lifecycle.Conclude(context.Background(), ConcludeIssueCommand{
		IssueID: res.IssueID,
		Resolution: discussion.Resolution{
			Kind:    discussion.ResolutionClosedWithTasks,
			Summary: "go",
			Tasks:   []dispatch.IssueConcludeTaskSpec{{LocalID: "a", Title: "T-A"}},
		},
		ConcludedBy: "user:h",
		Actor:       observability.Actor("user:h"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.TaskIDs) != 1 {
		t.Fatal("spawn fail")
	}
	got, _ := h.issueRepo.FindByID(context.Background(), res.IssueID)
	if got.HasConversation() {
		t.Fatal("issue should still be unbound")
	}
	// No system message anywhere
	if h.countEvents(t, "conversation.message_added") != 0 {
		t.Fatal("no system message expected when no conv bound")
	}
}

func TestConclude_ClosedWithTasksThreeTasksAndLocalIDDep(t *testing.T) {
	h, lifecycle, taskRepo := setupConcludeStack(t)
	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginWebConsole, Actor: observability.Actor("user:h"),
	})
	out, err := lifecycle.Conclude(context.Background(), ConcludeIssueCommand{
		IssueID: res.IssueID,
		Resolution: discussion.Resolution{
			Kind:    discussion.ResolutionClosedWithTasks,
			Summary: "go",
			Tasks: []dispatch.IssueConcludeTaskSpec{
				{LocalID: "a", Title: "T-A"},
				{LocalID: "b", Title: "T-B", DependsOnLocalIDs: []string{"a"}},
				{LocalID: "c", Title: "T-C", DependsOnLocalIDs: []string{"a", "b"}},
			},
		},
		ConcludedBy: "user:h",
		Actor:       observability.Actor("user:h"),
	})
	if err != nil {
		t.Fatalf("expected ok: %v", err)
	}
	if len(out.TaskIDs) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(out.TaskIDs))
	}
	if h.countEvents(t, "task.created") != 3 {
		t.Fatal("task.created x3 expected")
	}
	// Verify dep wiring
	tasks, _ := taskRepo.FindByProject(context.Background(), "P-1", task.Filter{})
	deps := map[string][]string{}
	for _, tk := range tasks {
		stringDeps := make([]string, 0, len(tk.DependsOnTaskIDs()))
		for _, d := range tk.DependsOnTaskIDs() {
			stringDeps = append(stringDeps, string(d))
		}
		deps[tk.Title()] = stringDeps
	}
	if len(deps["T-A"]) != 0 {
		t.Fatalf("A should have no deps: %v", deps["T-A"])
	}
	if len(deps["T-B"]) != 1 {
		t.Fatalf("B should have 1 dep: %v", deps["T-B"])
	}
	if len(deps["T-C"]) != 2 {
		t.Fatalf("C should have 2 deps: %v", deps["T-C"])
	}
}

func TestConclude_CycleRollsBackEverything(t *testing.T) {
	h, lifecycle, taskRepo := setupConcludeStack(t)
	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginWebConsole, Actor: observability.Actor("user:h"),
	})
	_, err := lifecycle.Conclude(context.Background(), ConcludeIssueCommand{
		IssueID: res.IssueID,
		Resolution: discussion.Resolution{
			Kind:    discussion.ResolutionClosedWithTasks,
			Summary: "go",
			Tasks: []dispatch.IssueConcludeTaskSpec{
				{LocalID: "a", Title: "A", DependsOnLocalIDs: []string{"b"}},
				{LocalID: "b", Title: "B", DependsOnLocalIDs: []string{"a"}},
			},
		},
		ConcludedBy: "user:h",
		Actor:       observability.Actor("user:h"),
	})
	if err == nil {
		t.Fatal("expected cycle err")
	}
	got, _ := h.issueRepo.FindByID(context.Background(), res.IssueID)
	if got.Status() != discussion.StatusOpen {
		t.Fatalf("status should remain open: %s", got.Status())
	}
	if h.countEvents(t, "issue.concluded") != 0 ||
		h.countEvents(t, "issue.tasks_spawned") != 0 ||
		h.countEvents(t, "task.created") != 0 {
		t.Fatal("no events should remain after rollback")
	}
	tasks, _ := taskRepo.FindByProject(context.Background(), "P-1", task.Filter{})
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks after rollback, got %d", len(tasks))
	}
}

func TestConclude_AlreadyConcluded_Rejected(t *testing.T) {
	_, lifecycle, _ := setupConcludeStack(t)
	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	if _, err := lifecycle.Conclude(context.Background(), ConcludeIssueCommand{
		IssueID:     res.IssueID,
		Resolution:  discussion.Resolution{Kind: discussion.ResolutionClosedNoAction, Summary: "skip"},
		ConcludedBy: "user:h",
		Actor:       observability.Actor("user:h"),
	}); err != nil {
		t.Fatal(err)
	}
	_, err := lifecycle.Conclude(context.Background(), ConcludeIssueCommand{
		IssueID:     res.IssueID,
		Resolution:  discussion.Resolution{Kind: discussion.ResolutionClosedNoAction, Summary: "again"},
		ConcludedBy: "user:h",
		Actor:       observability.Actor("user:h"),
	})
	if !errors.Is(err, discussion.ErrIssueAlreadyConcluded) {
		t.Fatalf("expected ErrIssueAlreadyConcluded, got %v", err)
	}
}

func TestConclude_WithdrawnRejected(t *testing.T) {
	_, lifecycle, _ := setupConcludeStack(t)
	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	if _, err := lifecycle.Withdraw(context.Background(), WithdrawIssueCommand{
		IssueID: res.IssueID, Reason: "dup", Message: "x", WithdrawnBy: "user:h",
		Actor: observability.Actor("user:h"),
	}); err != nil {
		t.Fatal(err)
	}
	_, err := lifecycle.Conclude(context.Background(), ConcludeIssueCommand{
		IssueID:     res.IssueID,
		Resolution:  discussion.Resolution{Kind: discussion.ResolutionClosedNoAction, Summary: "x"},
		ConcludedBy: "user:h",
		Actor:       observability.Actor("user:h"),
	})
	if !errors.Is(err, discussion.ErrIssueWithdrawn) {
		t.Fatalf("expected ErrIssueWithdrawn, got %v", err)
	}
}

func TestConclude_WithdrawnViaConclude(t *testing.T) {
	h, lifecycle, _ := setupConcludeStack(t)
	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	if _, err := lifecycle.Conclude(context.Background(), ConcludeIssueCommand{
		IssueID: res.IssueID,
		Resolution: discussion.Resolution{
			Kind:    discussion.ResolutionWithdrawn,
			Summary: "pull back",
		},
		ConcludedBy: "user:h",
		Actor:       observability.Actor("user:h"),
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := h.issueRepo.FindByID(context.Background(), res.IssueID)
	if got.Status() != discussion.StatusWithdrawn {
		t.Fatalf("status: %s", got.Status())
	}
	if h.countEvents(t, "issue.withdrawn") != 1 {
		t.Fatal("issue.withdrawn expected")
	}
}

func TestConclude_RejectsInvalidInputs(t *testing.T) {
	_, lifecycle, _ := setupConcludeStack(t)
	cases := []struct {
		name string
		cmd  ConcludeIssueCommand
	}{
		{"bad_actor", ConcludeIssueCommand{IssueID: "X",
			Resolution: discussion.Resolution{Kind: discussion.ResolutionClosedNoAction, Summary: "s"},
			ConcludedBy: "u", Actor: "BAD"}},
		{"empty_id", ConcludeIssueCommand{IssueID: "",
			Resolution: discussion.Resolution{Kind: discussion.ResolutionClosedNoAction, Summary: "s"},
			ConcludedBy: "u", Actor: observability.Actor("user:h")}},
		{"bad_resolution", ConcludeIssueCommand{IssueID: "X",
			Resolution: discussion.Resolution{Kind: "bogus", Summary: "s"},
			ConcludedBy: "u", Actor: observability.Actor("user:h")}},
		{"empty_concluded_by", ConcludeIssueCommand{IssueID: "X",
			Resolution: discussion.Resolution{Kind: discussion.ResolutionClosedNoAction, Summary: "s"},
			ConcludedBy: "", Actor: observability.Actor("user:h")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := lifecycle.Conclude(context.Background(), c.cmd); err == nil {
				t.Fatal("expected err")
			}
		})
	}
}

func TestConclude_NilSpawnerWhenWithTasks(t *testing.T) {
	h := newHarness(t)
	writer := convservice.NewMessageWriter(h.db, h.convRepo, h.msgRepo, h.sink, h.gen, h.clk)
	opener := NewIssueConversationOpener(h.convRepo, h.sink, h.gen, h.clk)
	// Note: NO WithSpawnerAndCommenter call → spawner nil
	lifecycle := NewIssueLifecycleService(h.db, h.issueRepo, opener, realMessageWriterAdapter{writer}, h.sink, h.gen, h.clk)
	res, _ := lifecycle.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	_, err := lifecycle.Conclude(context.Background(), ConcludeIssueCommand{
		IssueID: res.IssueID,
		Resolution: discussion.Resolution{Kind: discussion.ResolutionClosedWithTasks, Summary: "go",
			Tasks: []dispatch.IssueConcludeTaskSpec{{LocalID: "a", Title: "x"}}},
		ConcludedBy: "user:h",
		Actor:       observability.Actor("user:h"),
	})
	if err == nil {
		t.Fatal("expected spawner err")
	}
}

func TestConclude_NotFound(t *testing.T) {
	_, lifecycle, _ := setupConcludeStack(t)
	_, err := lifecycle.Conclude(context.Background(), ConcludeIssueCommand{
		IssueID:     "ghost",
		Resolution:  discussion.Resolution{Kind: discussion.ResolutionClosedNoAction, Summary: "s"},
		ConcludedBy: "user:h",
		Actor:       observability.Actor("user:h"),
	})
	if !errors.Is(err, discussion.ErrIssueNotFound) {
		t.Fatalf("expected not_found, got %v", err)
	}
}

func TestBuildSpawnSystemMessage(t *testing.T) {
	if buildSpawnSystemMessage(nil) != "已结论（无新任务）" {
		t.Fatal("empty case wrong")
	}
	got := buildSpawnSystemMessage([]taskruntime.TaskID{"T1", "T2"})
	if got == "" {
		t.Fatal("expected non-empty")
	}
}
