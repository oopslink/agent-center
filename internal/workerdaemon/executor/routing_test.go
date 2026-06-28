package executor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/clock"
)

func newStore(t *testing.T) (*RoutingStore, string) {
	t.Helper()
	root := t.TempDir()
	s, err := NewRoutingStore(root, clock.NewFakeClock(testNow))
	if err != nil {
		t.Fatalf("NewRoutingStore: %v", err)
	}
	return s, root
}

func TestNewRoutingStore_Errors(t *testing.T) {
	if _, err := NewRoutingStore("  ", nil); err == nil {
		t.Fatal("expected error for empty agent root")
	}
	// nil clock must default rather than panic.
	if _, err := NewRoutingStore(t.TempDir(), nil); err != nil {
		t.Fatalf("nil clock default: %v", err)
	}
}

func TestLayout_RoutingPath(t *testing.T) {
	l, _ := NewLayout("/agent")
	if got, want := l.RoutingPath(), "/agent/routing.json"; got != want {
		t.Errorf("RoutingPath=%q want %q", got, want)
	}
}

func TestProblemValidate(t *testing.T) {
	if err := (Problem{ProblemID: "", CreatedAt: testNow}).Validate(); err == nil {
		t.Error("empty problem_id should fail")
	}
	if err := (Problem{ProblemID: "p\x00", CreatedAt: testNow}).Validate(); err == nil {
		t.Error("null byte problem_id should fail")
	}
	if err := (Problem{ProblemID: "p1"}).Validate(); err == nil {
		t.Error("zero created_at should fail")
	}
	if err := (Problem{ProblemID: "p1", CreatedAt: testNow}).Validate(); err != nil {
		t.Errorf("valid problem: %v", err)
	}
}

func TestRoutingTableValidate_DuplicateID(t *testing.T) {
	tbl := &RoutingTable{Problems: []Problem{
		{ProblemID: "p1", CreatedAt: testNow},
		{ProblemID: "p1", CreatedAt: testNow},
	}}
	if err := tbl.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate error, got %v", err)
	}
}

func TestRoute_Precedence(t *testing.T) {
	tbl := &RoutingTable{Problems: []Problem{
		{ProblemID: "p-issue", IssueRef: "issue-1", CreatedAt: testNow},
		{ProblemID: "p-task", TaskRefs: []string{"task-9"}, CreatedAt: testNow},
		{ProblemID: "p-chat", ChatIDs: []string{"channel-7"}, CreatedAt: testNow},
		{ProblemID: "p-sem", CreatedAt: testNow},
	}}

	cases := []struct {
		name    string
		sig     Signal
		wantID  string
		wantWhy MatchReason
		wantNew bool
	}{
		{"issue wins over everything", Signal{IssueRef: "issue-1", TaskRef: "task-9", ChatID: "channel-7", SemanticHint: "p-sem"}, "p-issue", MatchIssueRef, false},
		{"task over chat+semantic", Signal{TaskRef: "task-9", ChatID: "channel-7", SemanticHint: "p-sem"}, "p-task", MatchTaskRef, false},
		{"chat over semantic", Signal{ChatID: "channel-7", SemanticHint: "p-sem"}, "p-chat", MatchChatID, false},
		{"semantic last", Signal{SemanticHint: "p-sem"}, "p-sem", MatchSemantic, false},
		{"semantic ignored when unknown", Signal{SemanticHint: "p-nope"}, "", MatchNone, true},
		{"no signal → new", Signal{}, "", MatchNone, true},
		{"unknown issue → new", Signal{IssueRef: "issue-404"}, "", MatchNone, true},
		{"blank-ish refs ignored", Signal{IssueRef: "   ", TaskRef: "  "}, "", MatchNone, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tbl.Route(tc.sig)
			if got.ProblemID != tc.wantID || got.Reason != tc.wantWhy || got.IsNew != tc.wantNew {
				t.Errorf("Route(%+v)=%+v want id=%q reason=%q new=%v", tc.sig, got, tc.wantID, tc.wantWhy, tc.wantNew)
			}
		})
	}
}

func TestRoutingTable_AddAndFind(t *testing.T) {
	tbl := &RoutingTable{}
	p := Problem{ProblemID: "p1", CreatedAt: testNow, TaskRefs: []string{"t1", "t1", "t2"}, ChatIDs: []string{"c1", "c1"}}
	if err := tbl.Add(p); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// duplicate id rejected
	if err := tbl.Add(Problem{ProblemID: "p1", CreatedAt: testNow}); err == nil {
		t.Error("duplicate Add should fail")
	}
	// invalid rejected
	if err := tbl.Add(Problem{ProblemID: "", CreatedAt: testNow}); err == nil {
		t.Error("invalid Add should fail")
	}
	got, ok := tbl.Find("p1")
	if !ok {
		t.Fatal("Find p1 missing")
	}
	if len(got.TaskRefs) != 2 || len(got.ChatIDs) != 1 {
		t.Errorf("Add did not dedupe: %+v", got)
	}
	if _, ok := tbl.Find("nope"); ok {
		t.Error("Find nope should be absent")
	}
}

func TestRoutingTable_Attach(t *testing.T) {
	tbl := &RoutingTable{}
	if err := tbl.Add(Problem{ProblemID: "p1", CreatedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	// attach to missing problem
	if err := tbl.AttachChat("ghost", "c1"); err == nil {
		t.Error("attach to missing problem should fail")
	}
	mustOK(t, tbl.AttachChat("p1", "c1"))
	mustOK(t, tbl.AttachChat("p1", "c1")) // idempotent
	mustOK(t, tbl.AttachChat("p1", "c2"))
	mustOK(t, tbl.AttachExecutor("p1", "e1"))
	mustOK(t, tbl.AttachExecutor("p1", "e1")) // idempotent
	mustOK(t, tbl.AttachTaskRef("p1", "task-1"))
	mustOK(t, tbl.AttachTaskRef("p1", "task-1")) // idempotent

	p, _ := tbl.Find("p1")
	if len(p.ChatIDs) != 2 || len(p.ExecutorIDs) != 1 || len(p.TaskRefs) != 1 {
		t.Errorf("attach set-union wrong: %+v", p)
	}

	// validation paths
	if err := tbl.AttachChat("p1", " "); err == nil {
		t.Error("blank chat should fail")
	}
	if err := tbl.AttachExecutor("p1", "../escape"); err == nil {
		t.Error("bad executor id should fail")
	}
	if err := tbl.AttachTaskRef("p1", ""); err == nil {
		t.Error("blank task ref should fail")
	}
}

func TestRoutingTable_SetIssueRef(t *testing.T) {
	tbl := &RoutingTable{}
	mustOK(t, tbl.Add(Problem{ProblemID: "p1", CreatedAt: testNow}))
	mustOK(t, tbl.SetIssueRef("p1", "issue-1"))
	mustOK(t, tbl.SetIssueRef("p1", "issue-1")) // re-set same is fine
	if err := tbl.SetIssueRef("p1", "issue-2"); err == nil {
		t.Error("rebind to different issue should fail")
	}
	if err := tbl.SetIssueRef("p1", "  "); err == nil {
		t.Error("blank issue ref should fail")
	}
	if err := tbl.SetIssueRef("ghost", "issue-1"); err == nil {
		t.Error("set on missing problem should fail")
	}
	p, _ := tbl.Find("p1")
	if p.IssueRef != "issue-1" {
		t.Errorf("issue ref=%q", p.IssueRef)
	}
}

func TestStore_LoadMissingIsEmpty(t *testing.T) {
	s, _ := newStore(t)
	tbl, err := s.Load()
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if len(tbl.Problems) != 0 {
		t.Errorf("missing file should be empty table, got %+v", tbl)
	}
}

func TestStore_SaveLoadRoundTrip(t *testing.T) {
	s, root := newStore(t)
	mustOK(t, s.Register(Problem{ProblemID: "p1", IssueRef: "issue-1", ChatIDs: []string{"c1"}}))

	// file actually lives at <root>/routing.json
	if _, err := os.Stat(filepath.Join(root, "routing.json")); err != nil {
		t.Fatalf("routing.json not written: %v", err)
	}
	tbl, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p, ok := tbl.Find("p1")
	if !ok {
		t.Fatal("p1 missing after reload")
	}
	if p.CreatedAt != testNow {
		t.Errorf("Register should stamp created_at from clock, got %v", p.CreatedAt)
	}
}

func TestStore_RegisterDuplicate(t *testing.T) {
	s, _ := newStore(t)
	mustOK(t, s.Register(Problem{ProblemID: "p1"}))
	if err := s.Register(Problem{ProblemID: "p1"}); err == nil {
		t.Error("duplicate Register should fail")
	}
}

func TestStore_RegisterPreservesExplicitCreatedAt(t *testing.T) {
	s, _ := newStore(t)
	custom := testNow.Add(-1000)
	mustOK(t, s.Register(Problem{ProblemID: "p1", CreatedAt: custom}))
	tbl, _ := s.Load()
	p, _ := tbl.Find("p1")
	if !p.CreatedAt.Equal(custom) {
		t.Errorf("explicit created_at overwritten: %v", p.CreatedAt)
	}
}

func TestStore_Route(t *testing.T) {
	s, _ := newStore(t)
	mustOK(t, s.Register(Problem{ProblemID: "p1", IssueRef: "issue-1"}))
	d, err := s.Route(Signal{IssueRef: "issue-1"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if d.ProblemID != "p1" || d.Reason != MatchIssueRef {
		t.Errorf("Route=%+v", d)
	}
	d2, _ := s.Route(Signal{ChatID: "unknown"})
	if !d2.IsNew {
		t.Errorf("unknown should be new: %+v", d2)
	}
}

func TestStore_Merge(t *testing.T) {
	s, _ := newStore(t)
	mustOK(t, s.Register(Problem{ProblemID: "p1"}))

	// merge a new chat + explicit refs + executors
	mustOK(t, s.Merge("p1", Signal{ChatID: "c1", IssueRef: "issue-1", TaskRef: "task-1"}, "e1", "e2", "  "))
	// idempotent re-merge
	mustOK(t, s.Merge("p1", Signal{ChatID: "c1"}, "e1"))

	tbl, _ := s.Load()
	p, _ := tbl.Find("p1")
	if len(p.ChatIDs) != 1 || p.IssueRef != "issue-1" || len(p.TaskRefs) != 1 || len(p.ExecutorIDs) != 2 {
		t.Errorf("merge result wrong: %+v", p)
	}

	// merge into missing problem
	if err := s.Merge("ghost", Signal{ChatID: "c1"}); err == nil {
		t.Error("merge into missing problem should fail")
	}
	// conflicting issue rebind surfaces
	if err := s.Merge("p1", Signal{IssueRef: "issue-2"}); err == nil {
		t.Error("conflicting issue rebind should fail")
	}
	// a malformed executor id surfaces rather than being recorded
	if err := s.Merge("p1", Signal{}, "../escape"); err == nil {
		t.Error("bad executor id in merge should fail")
	}
}

func TestStore_LoadCorrupt(t *testing.T) {
	s, root := newStore(t)
	if err := os.WriteFile(filepath.Join(root, "routing.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); err == nil {
		t.Error("corrupt routing.json should error, not silently zero")
	}
}

func TestStore_LoadInvalidContent(t *testing.T) {
	s, root := newStore(t)
	// well-formed JSON but a problem missing created_at → Validate rejects.
	if err := os.WriteFile(filepath.Join(root, "routing.json"), []byte(`{"problems":[{"problem_id":"p1"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); err == nil {
		t.Error("invalid table should error")
	}
}

func TestStore_SaveNilAndInvalid(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Save(nil); err == nil {
		t.Error("Save(nil) should fail")
	}
	if err := s.Save(&RoutingTable{Problems: []Problem{{ProblemID: ""}}}); err == nil {
		t.Error("Save invalid table should fail")
	}
}

// E2E: same problem discussed across two chats merges into one execution thread,
// a new issue spawns its own problem (acceptance criteria, design §8).
func TestRouting_CrossChatMerge_AcceptanceFlow(t *testing.T) {
	s, _ := newStore(t)

	// First message about problem A on chat-1, bound to issue-1.
	d := mustRoute(t, s, Signal{ChatID: "chat-1", IssueRef: "issue-1"})
	if !d.IsNew {
		t.Fatal("first message should be new problem")
	}
	mustOK(t, s.Register(Problem{ProblemID: "A", IssueRef: "issue-1", ChatIDs: []string{"chat-1"}, ExecutorIDs: []string{"exec-A"}}))

	// Same problem surfaces on chat-2 carrying the SAME issue ref → must merge to A,
	// not spawn a new problem/executor.
	d = mustRoute(t, s, Signal{ChatID: "chat-2", IssueRef: "issue-1"})
	if d.IsNew || d.ProblemID != "A" || d.Reason != MatchIssueRef {
		t.Fatalf("cross-chat same-issue should merge to A: %+v", d)
	}
	mustOK(t, s.Merge("A", Signal{ChatID: "chat-2", IssueRef: "issue-1"}))

	// A follow-up on chat-2 with no refs now rides chat continuity → still A.
	d = mustRoute(t, s, Signal{ChatID: "chat-2"})
	if d.IsNew || d.ProblemID != "A" || d.Reason != MatchChatID {
		t.Fatalf("chat-2 follow-up should stay on A via chat: %+v", d)
	}

	// A genuinely different issue on chat-3 → new problem.
	d = mustRoute(t, s, Signal{ChatID: "chat-3", IssueRef: "issue-2"})
	if !d.IsNew {
		t.Fatalf("new issue should be a new problem: %+v", d)
	}
	mustOK(t, s.Register(Problem{ProblemID: "B", IssueRef: "issue-2", ChatIDs: []string{"chat-3"}}))

	tbl, _ := s.Load()
	a, _ := tbl.Find("A")
	if len(a.ChatIDs) != 2 || len(a.ExecutorIDs) != 1 {
		t.Errorf("problem A should have 2 chats and a single executor (no dup spawn): %+v", a)
	}
	if len(tbl.Problems) != 2 {
		t.Errorf("expected exactly 2 problems, got %d", len(tbl.Problems))
	}
}

// When routing.json is corrupt, every store wrapper that loads first must
// surface the load error rather than proceed on an empty table.
func TestStore_WrappersPropagateLoadError(t *testing.T) {
	s, root := newStore(t)
	if err := os.WriteFile(filepath.Join(root, "routing.json"), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Route(Signal{ChatID: "c1"}); err == nil {
		t.Error("Route should propagate load error")
	}
	if err := s.Register(Problem{ProblemID: "p1"}); err == nil {
		t.Error("Register should propagate load error")
	}
	if err := s.Merge("p1", Signal{ChatID: "c1"}); err == nil {
		t.Error("Merge should propagate load error")
	}
}

func mustOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func mustRoute(t *testing.T, s *RoutingStore, sig Signal) Decision {
	t.Helper()
	d, err := s.Route(sig)
	if err != nil {
		t.Fatalf("Route(%+v): %v", sig, err)
	}
	return d
}

// guard: Load surfaces a non-ErrNotExist read error path is covered by corrupt
// test; ensure errors.Is wiring stays intact for missing.
func TestStore_LoadMissingUsesErrNotExist(t *testing.T) {
	s, _ := newStore(t)
	_, err := s.Load()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing should be nil error (empty table): %v", err)
	}
}
