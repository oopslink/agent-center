package projectmanager

import (
	"errors"
	"testing"
	"time"
)

func mkStage(t *testing.T, id, plan string, deps []StageID, maxRounds int) *Stage {
	t.Helper()
	st, err := NewStage(NewStageInput{
		ID: StageID(id), PlanID: PlanID(plan), Name: "stage " + id,
		DependsOnStages: deps, MaxRounds: maxRounds, CreatedAt: time.Unix(1000, 0),
	})
	if err != nil {
		t.Fatalf("NewStage(%s): %v", id, err)
	}
	return st
}

func TestNewStage_Validation(t *testing.T) {
	if _, err := NewStage(NewStageInput{ID: "", PlanID: "p", Name: "n"}); err == nil {
		t.Fatal("empty id should error")
	}
	if _, err := NewStage(NewStageInput{ID: "s", PlanID: "", Name: "n"}); err == nil {
		t.Fatal("empty plan should error")
	}
	if _, err := NewStage(NewStageInput{ID: "s", PlanID: "p", Name: "  "}); !errors.Is(err, ErrEmptyStageName) {
		t.Fatalf("blank name = %v, want ErrEmptyStageName", err)
	}
	// self-dependency rejected.
	if _, err := NewStage(NewStageInput{ID: "s", PlanID: "p", Name: "n", DependsOnStages: []StageID{"s"}}); !errors.Is(err, ErrStageSelfDependency) {
		t.Fatalf("self dep = %v, want ErrStageSelfDependency", err)
	}
}

func TestNewStage_MaxRoundsDefault(t *testing.T) {
	st := mkStage(t, "s", "p", nil, 0)
	if st.MaxRounds() != DefaultStageMaxRounds {
		t.Fatalf("max_rounds default = %d, want %d", st.MaxRounds(), DefaultStageMaxRounds)
	}
	st2 := mkStage(t, "s", "p", nil, 7)
	if st2.MaxRounds() != 7 {
		t.Fatalf("max_rounds = %d, want 7", st2.MaxRounds())
	}
}

func TestNewStage_DepsDedupOrder(t *testing.T) {
	st := mkStage(t, "s", "p", []StageID{"a", "b", "a", " ", "c"}, 1)
	got := st.DependsOnStages()
	want := []StageID{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("deps = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("deps[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestStage_SetGateNodeID_BumpsVersion(t *testing.T) {
	st := mkStage(t, "s", "p", nil, 1)
	v := st.Version()
	st.SetGateNodeID("node-1", time.Unix(2000, 0))
	if st.GateNodeID() != "node-1" {
		t.Fatalf("gate = %q", st.GateNodeID())
	}
	if st.Version() != v+1 {
		t.Fatalf("version = %d, want %d", st.Version(), v+1)
	}
}

func TestProjectStageStatus(t *testing.T) {
	D, R, O := StageMemberDone, StageMemberRunning, StageMemberOpen
	cases := []struct {
		name    string
		members []StageMemberState
		gate    StageGateState
		want    StageStatus
	}{
		{"empty", nil, StageGateNone, StageOpen},
		{"all-open-no-gate", []StageMemberState{O, O}, StageGateNone, StageOpen},
		{"one-running", []StageMemberState{R, O}, StageGateNone, StageRunning},
		{"one-done-rest-open", []StageMemberState{D, O}, StageGateNone, StageRunning},
		{"all-done-no-gate", []StageMemberState{D, D}, StageGateNone, StageDone},
		{"all-done-gate-pending", []StageMemberState{D, D}, StageGatePending, StageRunning},
		{"all-done-gate-passed", []StageMemberState{D, D}, StageGatePassed, StageDone},
		{"gate-reopened-dominates", []StageMemberState{D, D}, StageGateReopened, StageReopen},
		{"gate-reopened-while-running", []StageMemberState{R, O}, StageGateReopened, StageReopen},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ProjectStageStatus(tc.members, tc.gate); got != tc.want {
				t.Fatalf("ProjectStageStatus(%v,%v) = %q, want %q", tc.members, tc.gate, got, tc.want)
			}
		})
	}
}

// stageTask is a tiny helper to build a task with a stage for the DAG helpers.
func stageTask(t *testing.T, id, plan, stage string) *Task {
	t.Helper()
	tk, err := NewTask(NewTaskInput{
		ID: TaskID(id), ProjectID: "proj", Title: id, CreatedBy: "user:u",
		CreatedAt: time.Unix(1000, 0), StageID: StageID(stage),
	})
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	return tk
}

func TestStageEntries(t *testing.T) {
	// Stage A: dev -> review (dev is the entry). Stage B: build (its own entry).
	tasks := []*Task{
		stageTask(t, "dev", "p", "A"),
		stageTask(t, "review", "p", "A"),
		stageTask(t, "build", "p", "B"),
	}
	// review depends_on dev (review runs after dev) -> review has an in-stage pred.
	edges := []Dependency{{PlanID: "p", FromTaskID: "review", ToTaskID: "dev"}}
	stageOf := StageOf(tasks)
	entries := StageEntries(StageMembers(tasks, "A"), stageOf, "A", edges)
	if len(entries) != 1 || entries[0] != "dev" {
		t.Fatalf("stage A entries = %v, want [dev]", entries)
	}
	bEntries := StageEntries(StageMembers(tasks, "B"), stageOf, "B", edges)
	if len(bEntries) != 1 || bEntries[0] != "build" {
		t.Fatalf("stage B entries = %v, want [build]", bEntries)
	}
}

func TestValidateStageEdges(t *testing.T) {
	tasks := []*Task{
		stageTask(t, "a1", "p", "A"),
		stageTask(t, "a2", "p", "A"),
		stageTask(t, "b1", "p", "B"),
		stageTask(t, "free", "p", ""), // stageless
	}
	stageOf := StageOf(tasks)
	// Same-stage edge OK.
	if err := ValidateStageEdges(stageOf, []Dependency{{FromTaskID: "a2", ToTaskID: "a1"}}); err != nil {
		t.Fatalf("same-stage edge rejected: %v", err)
	}
	// Edge touching a stageless node OK.
	if err := ValidateStageEdges(stageOf, []Dependency{{FromTaskID: "a1", ToTaskID: "free"}}); err != nil {
		t.Fatalf("stageless edge rejected: %v", err)
	}
	// Cross-stage business edge rejected.
	if err := ValidateStageEdges(stageOf, []Dependency{{FromTaskID: "b1", ToTaskID: "a1"}}); !errors.Is(err, ErrStageCrossEdge) {
		t.Fatalf("cross-stage edge = %v, want ErrStageCrossEdge", err)
	}
}

func TestValidateStageDAG(t *testing.T) {
	a := mkStage(t, "A", "p", nil, 1)
	b := mkStage(t, "B", "p", []StageID{"A"}, 1)
	c := mkStage(t, "C", "p", []StageID{"A", "B"}, 1)
	if err := ValidateStageDAG([]*Stage{a, b, c}); err != nil {
		t.Fatalf("acyclic DAG rejected: %v", err)
	}
	// Dangling dependency.
	bad := mkStage(t, "D", "p", []StageID{"ghost"}, 1)
	if err := ValidateStageDAG([]*Stage{a, bad}); !errors.Is(err, ErrStageCrossPlanDependency) {
		t.Fatalf("dangling dep = %v, want ErrStageCrossPlanDependency", err)
	}
	// Cycle A->B->A.
	a2 := mkStage(t, "A", "p", []StageID{"B"}, 1)
	b2 := mkStage(t, "B", "p", []StageID{"A"}, 1)
	if err := ValidateStageDAG([]*Stage{a2, b2}); !errors.Is(err, ErrStageCycle) {
		t.Fatalf("cyclic DAG = %v, want ErrStageCycle", err)
	}
}

func TestTask_SetStage(t *testing.T) {
	tk := stageTask(t, "t1", "p", "")
	if tk.StageID() != "" {
		t.Fatalf("stage = %q, want empty", tk.StageID())
	}
	if err := tk.SetStage("A", time.Unix(2000, 0)); err != nil {
		t.Fatalf("SetStage: %v", err)
	}
	if tk.StageID() != "A" {
		t.Fatalf("stage = %q, want A", tk.StageID())
	}
}
