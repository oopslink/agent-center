package executor

import (
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)

func TestState_Valid(t *testing.T) {
	for _, s := range []State{StateRunning, StateDone, StateFailed} {
		if !s.Valid() {
			t.Errorf("State %q should be valid", s)
		}
	}
	for _, s := range []State{"", "pending", "RUNNING", "killed"} {
		if State(s).Valid() {
			t.Errorf("State %q should be invalid", s)
		}
	}
}

func TestErrorDetail_Validate(t *testing.T) {
	tests := []struct {
		name    string
		e       ErrorDetail
		wantErr bool
	}{
		{"ok", ErrorDetail{Kind: "timeout", Message: "stalled"}, false},
		{"missing kind", ErrorDetail{Message: "x"}, true},
		{"missing message", ErrorDetail{Kind: "x"}, true},
		{"blank kind", ErrorDetail{Kind: "   ", Message: "x"}, true},
		{"both missing", ErrorDetail{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.e.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func validInput() Input {
	return Input{
		ExecutorID: "exec-abc123",
		Goal:       Goal{Title: "do the thing"},
		Model:      "claude-haiku-4-5",
		CreatedAt:  testNow,
	}
}

func TestInput_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mut     func(*Input)
		wantErr string
	}{
		{"ok", func(*Input) {}, ""},
		{"ok full", func(in *Input) {
			in.ProblemID = "prob-1"
			in.Context = "ctx"
			in.Source = SourceRefs{ChatIDs: []string{"c1"}, IssueRef: "issue-1", TaskRef: "task-1"}
			in.Goal.Description = "d"
			in.Goal.IssueSpec = "spec"
		}, ""},
		{"missing id", func(in *Input) { in.ExecutorID = "" }, "executor_id required"},
		{"bad id separator", func(in *Input) { in.ExecutorID = "../x" }, "path separator"},
		{"bad id traversal", func(in *Input) { in.ExecutorID = ".." }, "path traversal"},
		{"missing title", func(in *Input) { in.Goal.Title = "  " }, "goal.title required"},
		{"missing model", func(in *Input) { in.Model = "" }, "model required"},
		{"missing created_at", func(in *Input) { in.CreatedAt = time.Time{} }, "created_at required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := validInput()
			tt.mut(&in)
			err := in.Validate()
			assertErrContains(t, err, tt.wantErr)
		})
	}
}

func TestOutput_Validate(t *testing.T) {
	tests := []struct {
		name    string
		o       Output
		wantErr string
	}{
		{"ok success", Output{ExecutorID: "e1", Success: true, Result: "r", FinishedAt: testNow}, ""},
		{"ok failure", Output{ExecutorID: "e1", Success: false, Error: &ErrorDetail{Kind: "k", Message: "m"}, FinishedAt: testNow}, ""},
		{"missing id", Output{Success: true, FinishedAt: testNow}, "executor_id required"},
		{"missing finished_at", Output{ExecutorID: "e1", Success: true}, "finished_at required"},
		{"success with error", Output{ExecutorID: "e1", Success: true, Error: &ErrorDetail{Kind: "k", Message: "m"}, FinishedAt: testNow}, "must be nil when success"},
		{"failure no error", Output{ExecutorID: "e1", Success: false, FinishedAt: testNow}, "error required when not success"},
		{"failure bad error", Output{ExecutorID: "e1", Success: false, Error: &ErrorDetail{Kind: "k"}, FinishedAt: testNow}, "error.message required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertErrContains(t, tt.o.Validate(), tt.wantErr)
		})
	}
}

func TestStatus_Validate(t *testing.T) {
	tests := []struct {
		name    string
		s       Status
		wantErr string
	}{
		{"ok running", Status{ExecutorID: "e1", State: StateRunning, StartedAt: testNow, LastProgressAt: testNow}, ""},
		{"ok done", Status{ExecutorID: "e1", State: StateDone, StartedAt: testNow, Summary: "ok"}, ""},
		{"ok failed", Status{ExecutorID: "e1", State: StateFailed, StartedAt: testNow, Error: &ErrorDetail{Kind: "k", Message: "m"}}, ""},
		{"missing id", Status{State: StateRunning, StartedAt: testNow}, "executor_id required"},
		{"invalid state", Status{ExecutorID: "e1", State: "weird", StartedAt: testNow}, `state "weird" invalid`},
		{"missing started_at", Status{ExecutorID: "e1", State: StateRunning}, "started_at required"},
		{"failed no error", Status{ExecutorID: "e1", State: StateFailed, StartedAt: testNow}, "error required when failed"},
		{"failed bad error", Status{ExecutorID: "e1", State: StateFailed, StartedAt: testNow, Error: &ErrorDetail{Message: "m"}}, "error.kind required"},
		{"running with error", Status{ExecutorID: "e1", State: StateRunning, StartedAt: testNow, Error: &ErrorDetail{Kind: "k", Message: "m"}}, "must be nil when state=running"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertErrContains(t, tt.s.Validate(), tt.wantErr)
		})
	}
}

func TestProgressEntry_Validate(t *testing.T) {
	tests := []struct {
		name    string
		e       ProgressEntry
		wantErr string
	}{
		{"ok", ProgressEntry{At: testNow, Message: "step 1"}, ""},
		{"ok with phase", ProgressEntry{At: testNow, Phase: "build", Message: "compiling"}, ""},
		{"missing at", ProgressEntry{Message: "x"}, "progress.at required"},
		{"missing message", ProgressEntry{At: testNow, Message: " "}, "progress.message required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertErrContains(t, tt.e.Validate(), tt.wantErr)
		})
	}
}

// assertErrContains fails unless err matches want: want=="" requires nil; a
// non-empty want requires a non-nil error whose message contains want.
func assertErrContains(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err.Error(), want)
	}
}
