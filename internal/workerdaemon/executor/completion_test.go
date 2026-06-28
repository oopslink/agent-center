package executor

import (
	"errors"
	"testing"
	"time"
)

func okOutput(id string) *Output {
	return &Output{ExecutorID: id, Success: true, Result: "done", FinishedAt: time.Unix(1700000000, 0)}
}

func failOutput(id string) *Output {
	return &Output{ExecutorID: id, Success: false, Error: &ErrorDetail{Kind: "boom", Message: "it broke"}, FinishedAt: time.Unix(1700000000, 0)}
}

func runningStatus(id string) *Status {
	return &Status{ExecutorID: id, State: StateRunning, Model: "m", StartedAt: time.Unix(1700000000, 0), LastProgressAt: time.Unix(1700000000, 0)}
}

func failedStatus(id string) *Status {
	return &Status{ExecutorID: id, State: StateFailed, Model: "m", StartedAt: time.Unix(1700000000, 0), Error: &ErrorDetail{Kind: "stk", Message: "status said fail"}}
}

func doneStatus(id string) *Status {
	return &Status{ExecutorID: id, State: StateDone, Model: "m", StartedAt: time.Unix(1700000000, 0), Summary: "all good"}
}

// TestClassify_DualSignal exhaustively covers the §9 determinations across the
// live-exit path and the recovery/orphan path.
func TestClassify_DualSignal(t *testing.T) {
	id := "e1"
	exitErr := errors.New("exit status 1")
	cases := []struct {
		name      string
		facts     CompletionFacts
		wantKind  OutcomeKind
		retryable bool
		// wantErrKind, if non-empty, asserts the resolved error.Kind.
		wantErrKind string
	}{
		// ---- live path: exit observed ----
		{
			name:     "exit0 + success output → succeeded",
			facts:    CompletionFacts{ExecutorID: id, Exited: true, ExitErr: nil, Output: okOutput(id), HasOutput: true, Status: doneStatus(id)},
			wantKind: OutcomeSucceeded,
		},
		{
			name:        "exit0 + failure output → failed (trust explicit failure)",
			facts:       CompletionFacts{ExecutorID: id, Exited: true, ExitErr: nil, Output: failOutput(id), HasOutput: true},
			wantKind:    OutcomeFailed,
			wantErrKind: "boom", // resolved from output.error
		},
		{
			name:        "exit0 + no output → crashed (retryable anomaly)",
			facts:       CompletionFacts{ExecutorID: id, Exited: true, ExitErr: nil, HasOutput: false, Status: runningStatus(id)},
			wantKind:    OutcomeCrashed,
			retryable:   true,
			wantErrKind: "clean_exit_no_output",
		},
		{
			name:        "nonzero exit, detail from status.error",
			facts:       CompletionFacts{ExecutorID: id, Exited: true, ExitErr: exitErr, Status: failedStatus(id)},
			wantKind:    OutcomeFailed,
			wantErrKind: "stk",
		},
		{
			name:        "nonzero exit, no status/output → synthesized detail",
			facts:       CompletionFacts{ExecutorID: id, Exited: true, ExitErr: exitErr},
			wantKind:    OutcomeFailed,
			wantErrKind: "nonzero_exit",
		},
		// ---- recovery path: no exit observed ----
		{
			name:     "orphan alive → running (re-adopt)",
			facts:    CompletionFacts{ExecutorID: id, Exited: false, Alive: true, Status: runningStatus(id)},
			wantKind: OutcomeRunning,
		},
		{
			name:     "orphan gone + success output → succeeded",
			facts:    CompletionFacts{ExecutorID: id, Exited: false, Alive: false, Output: okOutput(id), HasOutput: true},
			wantKind: OutcomeSucceeded,
		},
		{
			name:        "orphan gone + failure output → failed",
			facts:       CompletionFacts{ExecutorID: id, Exited: false, Alive: false, Output: failOutput(id), HasOutput: true},
			wantKind:    OutcomeFailed,
			wantErrKind: "boom",
		},
		{
			name:        "orphan gone + status failed → failed",
			facts:       CompletionFacts{ExecutorID: id, Exited: false, Alive: false, Status: failedStatus(id)},
			wantKind:    OutcomeFailed,
			wantErrKind: "stk",
		},
		{
			name:        "orphan gone + status done but no output → crashed",
			facts:       CompletionFacts{ExecutorID: id, Exited: false, Alive: false, Status: doneStatus(id)},
			wantKind:    OutcomeCrashed,
			retryable:   true,
			wantErrKind: "done_no_output",
		},
		{
			name:        "orphan gone + status running → crashed (the §9 core case)",
			facts:       CompletionFacts{ExecutorID: id, Exited: false, Alive: false, Status: runningStatus(id)},
			wantKind:    OutcomeCrashed,
			retryable:   true,
			wantErrKind: "process_gone",
		},
		{
			name:        "orphan gone + nothing on disk → crashed",
			facts:       CompletionFacts{ExecutorID: id, Exited: false, Alive: false},
			wantKind:    OutcomeCrashed,
			retryable:   true,
			wantErrKind: "process_gone",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.facts)
			if got.Kind != tc.wantKind {
				t.Fatalf("Kind = %q, want %q", got.Kind, tc.wantKind)
			}
			if got.Retryable != tc.retryable {
				t.Errorf("Retryable = %v, want %v", got.Retryable, tc.retryable)
			}
			if got.ExecutorID != id {
				t.Errorf("ExecutorID = %q, want %q", got.ExecutorID, id)
			}
			switch tc.wantKind {
			case OutcomeSucceeded, OutcomeRunning:
				if got.Error != nil {
					t.Errorf("Error = %+v, want nil for %s", got.Error, tc.wantKind)
				}
			default:
				if got.Error == nil {
					t.Fatalf("Error = nil, want detail for %s", tc.wantKind)
				}
				if tc.wantErrKind != "" && got.Error.Kind != tc.wantErrKind {
					t.Errorf("Error.Kind = %q, want %q", got.Error.Kind, tc.wantErrKind)
				}
				if got.Error.Message == "" {
					t.Error("resolved error must carry a human-readable message")
				}
			}
		})
	}
}

// TestResolveError_Precedence verifies status.error beats output.error beats the
// synthesized fallback, and that invalid (half-populated) details are skipped.
func TestResolveError_Precedence(t *testing.T) {
	id := "e1"
	good := &ErrorDetail{Kind: "k", Message: "m"}
	half := &ErrorDetail{Kind: "k"} // invalid: no message

	// status.error wins.
	got := resolveError(CompletionFacts{Status: &Status{Error: good}, Output: &Output{Error: &ErrorDetail{Kind: "other", Message: "x"}}}, "fb", "fbmsg")
	if got.Kind != "k" {
		t.Errorf("status.error should win, got %q", got.Kind)
	}
	// invalid status.error → fall through to output.error.
	got = resolveError(CompletionFacts{Status: &Status{Error: half}, Output: &Output{Error: good}}, "fb", "fbmsg")
	if got.Kind != "k" || got.Message != "m" {
		t.Errorf("should fall to output.error, got %+v", got)
	}
	// neither valid → synthesized fallback.
	got = resolveError(CompletionFacts{ExecutorID: id}, "fb", "fbmsg")
	if got.Kind != "fb" || got.Message != "fbmsg" {
		t.Errorf("should use fallback, got %+v", got)
	}
}
