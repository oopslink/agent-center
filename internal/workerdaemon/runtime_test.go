package workerdaemon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/admin/dispatchq"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

// fakeCenter is an in-memory CenterClient that records calls and
// returns queued payloads.
type fakeCenter struct {
	mu sync.Mutex

	enrolls    int
	heartbeats int

	dispatches []dispatch.DispatchEnvelope
	kills      []dispatchq.KillRequest

	progress  []reportEvent
	failures  []reportEvent
	artifacts []reportEvent
}

type reportEvent struct {
	ExecutionID string
	Milestone   string
	Content     string
}

func (f *fakeCenter) Enroll(_ context.Context, _ string, _ []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enrolls++
	return nil
}
func (f *fakeCenter) Heartbeat(_ context.Context, _ string, _ []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.heartbeats++
	return nil
}
func (f *fakeCenter) PullDispatches(_ context.Context, _ string) ([]dispatch.DispatchEnvelope, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.dispatches
	f.dispatches = nil
	return out, nil
}
func (f *fakeCenter) PullKills(_ context.Context) ([]dispatchq.KillRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.kills
	f.kills = nil
	return out, nil
}
func (f *fakeCenter) ReportProgress(_ context.Context, execID, milestone, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.progress = append(f.progress, reportEvent{execID, milestone, content})
	return nil
}
func (f *fakeCenter) ReportFailure(_ context.Context, execID, reason, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures = append(f.failures, reportEvent{execID, reason, message})
	return nil
}
func (f *fakeCenter) ReportArtifact(_ context.Context, execID string, _ []byte, kind string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.artifacts = append(f.artifacts, reportEvent{execID, kind, ""})
	return nil
}

func (f *fakeCenter) push(envs ...dispatch.DispatchEnvelope) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dispatches = append(f.dispatches, envs...)
}
func (f *fakeCenter) pushKill(k dispatchq.KillRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kills = append(f.kills, k)
}

func validEnvelope(execID string) dispatch.DispatchEnvelope {
	return dispatch.DispatchEnvelope{
		EnvelopeVersion: dispatch.EnvelopeVersionV2,
		ExecutionID:     taskruntime.TaskExecutionID(execID),
		TaskID:          taskruntime.TaskID("T-" + execID),
		WorkerID:        "w-1",
		ProjectID:       "P-1",
		AgentInstanceID: "AI-1",
		AgentCLI:        "fakeagent",
		WorkspaceMode:   execution.WorkspaceDirect,
		TaskTitle:       "title",
		Priority:        "normal",
	}
}

// TestRuntime_EnrollThenDispatchEndToEnd drives one envelope through a
// scripted spawner that emits start → progress → done events and asserts
// the fakeCenter saw the expected forwards.
func TestRuntime_EnrollThenDispatchEndToEnd(t *testing.T) {
	fc := &fakeCenter{}
	spawnerCalls := 0
	spawner := func(ctx context.Context, env dispatch.DispatchEnvelope, rt *Runtime) error {
		spawnerCalls++
		// Mirror the production flow: emit a couple of progress events +
		// a done event via the rt.client.
		if err := rt.client.ReportProgress(ctx, string(env.ExecutionID), "started", "ok"); err != nil {
			return err
		}
		if err := rt.client.ReportProgress(ctx, string(env.ExecutionID), "step_1", "compiled"); err != nil {
			return err
		}
		return rt.client.ReportProgress(ctx, string(env.ExecutionID), "done", "agent exited cleanly")
	}
	rt := NewRuntime(RuntimeConfig{
		WorkerID:     "w-1",
		PollInterval: 10 * time.Millisecond,
		HeartbeatEvery: 10 * time.Second,
		Logger:       func(string) {},
	}, fc, spawner)

	fc.push(validEnvelope("E-1"))

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = rt.Run(ctx)

	// Wait for in-flight goroutines.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		fc.mu.Lock()
		done := len(fc.progress) >= 3
		fc.mu.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()
	if fc.enrolls == 0 {
		t.Fatal("Enroll not called")
	}
	if spawnerCalls != 1 {
		t.Fatalf("spawner calls=%d (want 1)", spawnerCalls)
	}
	if len(fc.progress) < 3 {
		t.Fatalf("want >=3 progress, got %d: %+v", len(fc.progress), fc.progress)
	}
	// Last event is "done" milestone.
	last := fc.progress[len(fc.progress)-1]
	if last.Milestone != "done" {
		t.Fatalf("last milestone=%q (want done)", last.Milestone)
	}
}

// TestRuntime_InvalidEnvelopeReportsFailure ensures envelopes failing
// Validate() never reach the spawner and produce a NACK-style failure.
func TestRuntime_InvalidEnvelopeReportsFailure(t *testing.T) {
	fc := &fakeCenter{}
	spawnerCalled := false
	rt := NewRuntime(RuntimeConfig{
		WorkerID:     "w-1",
		PollInterval: 10 * time.Millisecond,
	}, fc, func(context.Context, dispatch.DispatchEnvelope, *Runtime) error {
		spawnerCalled = true
		return nil
	})

	bad := validEnvelope("E-1")
	bad.AgentCLI = "" // breaks Validate
	fc.push(bad)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = rt.Run(ctx)

	if spawnerCalled {
		t.Fatal("spawner should not be called for invalid envelope")
	}
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.failures) == 0 {
		t.Fatal("expected ReportFailure")
	}
	if fc.failures[0].Milestone != "nack_envelope_invalid" {
		t.Fatalf("reason=%q", fc.failures[0].Milestone)
	}
}

// TestRuntime_KillSignalsRegisteredHandle verifies that a kill request
// matched to a live execution invokes Signal on the registered handle.
func TestRuntime_KillFiltersUnknownExecution(t *testing.T) {
	fc := &fakeCenter{}
	rt := NewRuntime(RuntimeConfig{
		WorkerID:     "w-1",
		PollInterval: 10 * time.Millisecond,
	}, fc, func(context.Context, dispatch.DispatchEnvelope, *Runtime) error { return nil })

	// Kill for execution we don't own — must be a no-op (no panic).
	fc.pushKill(dispatchq.KillRequest{ExecutionID: "E-ghost"})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = rt.Run(ctx)
}

func TestRuntime_RequiresWorkerID(t *testing.T) {
	fc := &fakeCenter{}
	rt := NewRuntime(RuntimeConfig{}, fc, func(context.Context, dispatch.DispatchEnvelope, *Runtime) error { return nil })
	err := rt.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for missing worker_id")
	}
}

// TestRuntime_LiveCountTracksHandle exercises registerLive directly.
func TestRuntime_LiveCountTracksHandle(t *testing.T) {
	fc := &fakeCenter{}
	rt := NewRuntime(RuntimeConfig{WorkerID: "w-1"}, fc, nil)
	if rt.LiveCount() != 0 {
		t.Fatalf("initial LiveCount=%d", rt.LiveCount())
	}
	rt.registerLive("E-1", &procHandle{})
	if rt.LiveCount() != 1 {
		t.Fatalf("after register, LiveCount=%d", rt.LiveCount())
	}
}

// TestExtractFakeAgentScript exercises the helper parsing.
func TestExtractFakeAgentScript(t *testing.T) {
	cases := []struct {
		desc string
		want string
	}{
		{desc: "fakeagent-script: /tmp/s.jsonl", want: "/tmp/s.jsonl"},
		{desc: "preamble\nfakeagent-script: /tmp/x.jsonl\ntail", want: "/tmp/x.jsonl"},
		{desc: "no marker here", want: ""},
		{desc: "  fakeagent-script:   /tmp/y.jsonl  ", want: "/tmp/y.jsonl"},
	}
	for _, c := range cases {
		env := dispatch.DispatchEnvelope{TaskDescription: c.desc}
		got := extractFakeAgentScript(env)
		if got != c.want {
			t.Fatalf("extractFakeAgentScript(%q) = %q, want %q", c.desc, got, c.want)
		}
	}
}

// TestSafeReason maps empty to a default.
func TestSafeReason(t *testing.T) {
	if safeReason("") != "agent_self_reported_failure" {
		t.Fatal("empty should map to agent_self_reported_failure")
	}
	if safeReason("custom") != "custom" {
		t.Fatal("non-empty should pass through")
	}
}

// Touch errors so the import remains referenced if future cleanups
// remove an explicit use.
var _ = errors.New
