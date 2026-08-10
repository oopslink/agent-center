package agentruntime

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/agentruntime/orchestrator"
)

type activityCaptureReporter struct {
	nopReporter
	mu              sync.Mutex
	payloads        []string
	interactionRefs []string
}

func (r *activityCaptureReporter) ReportAgentActivity(_ context.Context, _, _, payloadJSON, _, interactionRef string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.payloads = append(r.payloads, payloadJSON)
	r.interactionRefs = append(r.interactionRefs, interactionRef)
	return nil
}

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
	obs.ExecutorRecovery(executor.RecoveryEvent{ExecutorID: "e1", TaskRef: "T", Event: "executor.recovery_scan_completed", Decision: "adopt"})
	rt.emitExecutorStart("a", "T", "title", &orchestrator.Launched{ExecutorID: "e1", CLI: "claude-code", Model: "m"})
	rt.emitExecutorStart("a", "T", "title", nil) // nil → no-op

	rep.mu.Lock()
	got := append([]string(nil), rep.activity...)
	rep.mu.Unlock()
	if len(got) != 4 {
		t.Fatalf("expected 4 lifecycle activity emits, got %d: %v", len(got), got)
	}
	for _, ev := range got {
		if ev != "lifecycle" {
			t.Errorf("event type = %q, want lifecycle", ev)
		}
	}
}

func TestExecutorActivityBridge_KeepsExecutionIDGroupingWithSlotPayload(t *testing.T) {
	rep := &activityCaptureReporter{}
	rt := NewLocalRuntime(LocalRuntimeConfig{
		AgentID: "a", Reporter: rep,
		Log: func(string, ...any) {}, Now: func() time.Time { return time.Unix(1, 0) },
	}, &SessionState{})
	slot := 3
	execID := "exec-activity"
	rt.emitExecutorStart("a", "task-1", "title", &orchestrator.Launched{
		ExecutorID: execID,
		SlotIndex:  &slot,
		CLI:        "codex",
		Model:      "gpt-5",
	})

	rep.mu.Lock()
	defer rep.mu.Unlock()
	if len(rep.payloads) != 1 || len(rep.interactionRefs) != 1 {
		t.Fatalf("activity emits = payloads:%d refs:%d, want 1/1", len(rep.payloads), len(rep.interactionRefs))
	}
	if rep.interactionRefs[0] != "executor:"+execID {
		t.Fatalf("interaction_ref = %q, want executor:%s", rep.interactionRefs[0], execID)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(rep.payloads[0]), &payload); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	if payload["slot_index"] != float64(3) {
		t.Fatalf("slot_index payload = %v, want 3", payload["slot_index"])
	}
}

// T758: the executor lifecycle activity payloads follow a fixed per-event schema
// the Web Console Activity stream reads. Every payload MUST carry the executor_id +
// task_ref prefix (design point 3) plus its event marker; the tests assert that
// invariant and the event-specific keys, in the activity_payload_v271_test.go style.

func TestExecutorStartPayload_Schema(t *testing.T) {
	slot := 0
	p := executorStartPayload(executorStartFields{
		ExecutorID:  "exec-abc123",
		SlotIndex:   &slot,
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
	if p["slot_index"] != 0 {
		t.Fatalf("slot_index = %v, want 0", p["slot_index"])
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
	slot := 2
	base := func(o executor.OutcomeKind, reason, detail string, retryable, recovered bool) executor.StopEvent {
		return executor.StopEvent{
			ExecutorID: "exec-xyz", SlotIndex: &slot, TaskRef: "T758", Outcome: o,
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
			if p["slot_index"] != 2 {
				t.Fatalf("slot_index = %v, want 2", p["slot_index"])
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

func TestExecutorStopPayload_IncludesGitDeliverySnapshot(t *testing.T) {
	p := executorStopPayload(executor.StopEvent{
		ExecutorID: "exec-xyz", TaskRef: "T758", Outcome: executor.OutcomeSucceeded,
		Git: &executor.FinalizedGitStatus{
			Branch: "feat/x", HeadSHA: "abc", Probed: true, Pushed: true,
			BaseRef: "origin/main", BaseKnown: true, AheadOfBase: 1,
		},
	})
	g, ok := p["git"].(*executor.FinalizedGitStatus)
	if !ok {
		t.Fatalf("git payload missing/wrong type: %+v", p["git"])
	}
	if g.Branch != "feat/x" || g.HeadSHA != "abc" || !g.Pushed {
		t.Fatalf("git payload = %+v", g)
	}
}

func TestExecutorProgressPayload_Schema(t *testing.T) {
	at := time.Unix(1700000123, 0)
	slot := 1
	p := executorProgressPayload(executor.ProgressEvent{
		ExecutorID: "exec-run", SlotIndex: &slot, TaskRef: "T758", State: "running",
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
	if p["slot_index"] != 1 {
		t.Fatalf("slot_index = %v, want 1", p["slot_index"])
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

func TestExecutorRecoveryPayload_Schema(t *testing.T) {
	slot := 0
	p := executorRecoveryPayload(executor.RecoveryEvent{
		ExecutorID: "exec-run",
		SlotIndex:  &slot,
		TaskRef:    "T155",
		Event:      "executor.recovery_slot_conflict",
		Reason:     "duplicate_running_slot",
		Detail:     "slot 0 already occupied",
		Outcome:    executor.OutcomeRunning,
		PID:        4242,
		Decision:   "not_adopted",
	})
	if p["event"] != "executor.recovery_slot_conflict" {
		t.Fatalf("event = %v", p["event"])
	}
	if p["executor_id"] != "exec-run" || p["task_ref"] != "T155" {
		t.Fatalf("missing executor_id/task_ref prefix: %+v", p)
	}
	if p["slot_index"] != 0 || p["pid"] != 4242 {
		t.Fatalf("slot/pid = %+v", p)
	}
	if p["reason"] != "duplicate_running_slot" || p["decision"] != "not_adopted" || p["outcome"] != "running" {
		t.Fatalf("recovery payload = %+v", p)
	}
	if p["scope"] != "not_adopted" {
		t.Fatalf("scope = %v, want decision", p["scope"])
	}
}

func TestExecutorInteractionRef(t *testing.T) {
	if got := executorInteractionRef("exec-abc"); got != "executor:exec-abc" {
		t.Fatalf("interaction ref = %q, want executor:exec-abc", got)
	}
}
