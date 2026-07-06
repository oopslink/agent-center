package agentruntime

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/agentruntime/orchestrator"
)

// TestExecutorActivityObserver_Emits covers the observer→activity bridge: stop and
// progress events (and emitExecutorStart) each post ONE lifecycle activity, and a nil
// Launched is a no-op.
func TestExecutorActivityObserver_Emits(t *testing.T) {
	rep := &recReporter{}
	rt := NewLocalRuntime(LocalRuntimeConfig{
		AgentID: "a", Reporter: rep,
		Log: func(string, ...any) {}, Now: func() time.Time { return time.Unix(1, 0) },
	}, &SessionState{})
	obs := executorActivityObserver{r: rt, agentID: "a"}

	obs.ExecutorStopped(executor.StopEvent{ExecutorID: "e1", TaskRef: "T", Outcome: executor.OutcomeSucceeded})
	obs.ExecutorProgress(executor.ProgressEvent{ExecutorID: "e1", TaskRef: "T", State: "running"})
	rt.emitExecutorStart("a", "T", "title", &orchestrator.Launched{ExecutorID: "e1", CLI: "claude-code", Model: "m"})
	rt.emitExecutorStart("a", "T", "title", nil) // nil → no-op

	rep.mu.Lock()
	got := append([]string(nil), rep.activity...)
	rep.mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("expected 3 lifecycle activity emits, got %d: %v", len(got), got)
	}
	for _, ev := range got {
		if ev != "lifecycle" {
			t.Errorf("event type = %q, want lifecycle", ev)
		}
	}
}

// T758: the executor lifecycle activity payloads follow a fixed per-event schema
// the Web Console Activity stream reads. Every payload MUST carry the executor_id +
// task_ref prefix (design point 3) plus its event marker; the tests assert that
// invariant and the event-specific keys, in the activity_payload_v271_test.go style.

func TestExecutorStartPayload_Schema(t *testing.T) {
	p := executorStartPayload(executorStartFields{
		ExecutorID:  "exec-abc123",
		TaskRef:     "T758",
		PID:         4242,
		CLI:         "claude-code",
		Model:       "claude-opus-4-8",
		ModelSource: "task_model",
		ProblemID:   "prob-1",
		Title:       "do the thing",
	})
	if p["event"] != "executor.start" {
		t.Fatalf("event = %v, want executor.start", p["event"])
	}
	if p["executor_id"] != "exec-abc123" || p["task_ref"] != "T758" {
		t.Fatalf("missing executor_id/task_ref prefix: %+v", p)
	}
	if p["pid"] != 4242 || p["cli"] != "claude-code" || p["model"] != "claude-opus-4-8" {
		t.Fatalf("start payload core = %+v", p)
	}
	if p["model_source"] != "task_model" || p["problem_id"] != "prob-1" || p["title"] != "do the thing" {
		t.Fatalf("start payload optionals = %+v", p)
	}
	// scope drives the row preview parenthetical ("executor.start (model)").
	if p["scope"] != "claude-opus-4-8" {
		t.Fatalf("scope = %v, want model", p["scope"])
	}
}

func TestExecutorStartPayload_OmitsEmptyOptionals(t *testing.T) {
	p := executorStartPayload(executorStartFields{ExecutorID: "e1", TaskRef: "", CLI: "codex", Model: "gpt"})
	for _, k := range []string{"model_source", "problem_id", "title"} {
		if _, ok := p[k]; ok {
			t.Errorf("empty optional %q must be omitted: %+v", k, p)
		}
	}
	// executor_id + task_ref are ALWAYS present (task_ref may be an empty string).
	if _, ok := p["executor_id"]; !ok {
		t.Errorf("executor_id must always be present: %+v", p)
	}
	if _, ok := p["task_ref"]; !ok {
		t.Errorf("task_ref must always be present (even empty): %+v", p)
	}
}

func TestExecutorStopPayload_FourClasses(t *testing.T) {
	base := func(o executor.OutcomeKind, reason, detail string, retryable, recovered bool) executor.StopEvent {
		return executor.StopEvent{
			ExecutorID: "exec-xyz", TaskRef: "T758", Outcome: o,
			Reason: reason, Detail: detail, Retryable: retryable, Recovered: recovered,
			At: time.Unix(1700000000, 0),
		}
	}
	cases := []struct {
		name        string
		ev          executor.StopEvent
		wantOutcome string
		wantReason  any // string when present, nil when must be absent
		wantScope   string
		wantRecov   bool
	}{
		{"正常退出", base(executor.OutcomeSucceeded, "", "", false, false), "succeeded", nil, "succeeded", false},
		{"异常退出(exit code)", base(executor.OutcomeFailed, "nonzero_exit", "executor exited with error", false, false), "failed", "nonzero_exit", "failed:nonzero_exit", false},
		{"看门狗 stall-kill", base(executor.OutcomeFailed, "stalled", "killed by watchdog", false, false), "failed", "stalled", "failed:stalled", false},
		{"orphan 清理", base(executor.OutcomeCrashed, "process_gone", "process gone", true, true), "crashed", "process_gone", "crashed:process_gone", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := executorStopPayload(tc.ev)
			if p["event"] != "executor.stop" {
				t.Fatalf("event = %v", p["event"])
			}
			if p["executor_id"] != "exec-xyz" || p["task_ref"] != "T758" {
				t.Fatalf("missing executor_id/task_ref prefix: %+v", p)
			}
			if p["outcome"] != tc.wantOutcome {
				t.Errorf("outcome = %v, want %s", p["outcome"], tc.wantOutcome)
			}
			if tc.wantReason == nil {
				if _, ok := p["reason"]; ok {
					t.Errorf("success must omit reason: %+v", p)
				}
			} else if p["reason"] != tc.wantReason {
				t.Errorf("reason = %v, want %v", p["reason"], tc.wantReason)
			}
			if p["scope"] != tc.wantScope {
				t.Errorf("scope = %v, want %s", p["scope"], tc.wantScope)
			}
			if p["recovered"] != tc.wantRecov {
				t.Errorf("recovered = %v, want %v", p["recovered"], tc.wantRecov)
			}
		})
	}
}

func TestExecutorProgressPayload_Schema(t *testing.T) {
	at := time.Unix(1700000123, 0)
	p := executorProgressPayload(executor.ProgressEvent{
		ExecutorID: "exec-run", TaskRef: "T758", State: "running",
		Summary: "wrote tests", Detail: "读 task.go", LastProgressAt: at,
	})
	if p["event"] != "executor.progress" {
		t.Fatalf("event = %v", p["event"])
	}
	if p["executor_id"] != "exec-run" || p["task_ref"] != "T758" {
		t.Fatalf("missing executor_id/task_ref prefix: %+v", p)
	}
	if p["state"] != "running" || p["scope"] != "running" {
		t.Fatalf("progress state/scope = %+v", p)
	}
	if p["summary"] != "wrote tests" {
		t.Fatalf("summary = %v", p["summary"])
	}
	if p["detail"] != "读 task.go" {
		t.Fatalf("detail = %v", p["detail"])
	}
	if p["last_progress_at"] != at.UTC().Format(time.RFC3339) {
		t.Fatalf("last_progress_at = %v", p["last_progress_at"])
	}
}

func TestExecutorProgressPayload_OmitsEmptySummary(t *testing.T) {
	p := executorProgressPayload(executor.ProgressEvent{ExecutorID: "e1", State: "running"})
	if _, ok := p["summary"]; ok {
		t.Errorf("empty summary must be omitted: %+v", p)
	}
	if _, ok := p["detail"]; ok {
		t.Errorf("empty detail must be omitted: %+v", p)
	}
	if _, ok := p["last_progress_at"]; ok {
		t.Errorf("zero last_progress_at must be omitted: %+v", p)
	}
}

func TestExecutorInteractionRef(t *testing.T) {
	if got := executorInteractionRef("exec-abc"); got != "executor:exec-abc" {
		t.Fatalf("interaction ref = %q, want executor:exec-abc", got)
	}
}
