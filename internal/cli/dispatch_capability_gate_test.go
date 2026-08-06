package cli

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/workforce"
)

type fakeDispatchWorkerRepo struct {
	workers map[string]*workforce.Worker
}

func (f fakeDispatchWorkerRepo) FindByID(_ context.Context, id workforce.WorkerID) (*workforce.Worker, error) {
	if w, ok := f.workers[string(id)]; ok {
		return w, nil
	}
	return nil, workforce.ErrWorkerNotFound
}

type recordingCapabilityWaits struct {
	waiting []environment.CapabilityWait
}

func (r *recordingCapabilityWaits) UpsertWaiting(_ context.Context, wait environment.CapabilityWait) error {
	r.waiting = append(r.waiting, wait)
	return nil
}
func (r *recordingCapabilityWaits) Resolve(context.Context, string, string, time.Time) error {
	return nil
}
func (r *recordingCapabilityWaits) CancelByTask(context.Context, string, string, time.Time) error {
	return nil
}
func (r *recordingCapabilityWaits) MarkTimedOut(context.Context, string, string, string, time.Time) error {
	return nil
}
func (r *recordingCapabilityWaits) RecordRedrive(context.Context, string, string, time.Time) error {
	return nil
}
func (r *recordingCapabilityWaits) ListWaitingByWorker(context.Context, string) ([]environment.CapabilityWait, error) {
	return nil, nil
}
func (r *recordingCapabilityWaits) ListWaiting(context.Context) ([]environment.CapabilityWait, error) {
	return nil, nil
}
func (r *recordingCapabilityWaits) ListExpiredWaiting(context.Context, time.Time, int) ([]environment.CapabilityWait, error) {
	return nil, nil
}

func dispatchWorker(t *testing.T, caps []workforce.Capability) *workforce.Worker {
	t.Helper()
	now := time.Unix(1_700_000_000, 0).UTC()
	w, err := workforce.RehydrateWorker(workforce.RehydrateWorkerInput{
		ID: "W1", Name: "W1", Status: workforce.WorkerOnline,
		CapabilityList: caps,
		EnrolledAt:     now, CreatedAt: now, UpdatedAt: now, Version: 1,
	})
	if err != nil {
		t.Fatalf("worker: %v", err)
	}
	return w
}

func TestAssignTarget_MissingCapability_EntersWaitAndSkipsWake(t *testing.T) {
	ag := mustAgent(t, "entity-1", "member-1", "W1", agent.LifecycleRunning)
	ars := newAgentReads()
	ars.put(ag)
	waits := &recordingCapabilityWaits{}
	wr := fakeDispatchWorkerRepo{workers: map[string]*workforce.Worker{"W1": dispatchWorker(t, nil)}}

	tgt, ok, err := buildAssignTargetWithCapabilityWaits(fakeDispatchReads{}, ars, wr, waits)(
		context.Background(), "agent:member-1", "T1")
	if err != nil || ok {
		t.Fatalf("missing capability must skip, got ok=%v err=%v tgt=%+v", ok, err, tgt)
	}
	if len(waits.waiting) != 1 {
		t.Fatalf("waiting rows=%d, want 1", len(waits.waiting))
	}
	if waits.waiting[0].Status != environment.CapabilityWaitWaiting || waits.waiting[0].Reason != workforce.CapabilityMatchMissingCLI {
		t.Fatalf("wait=%+v, want waiting/missing_cli", waits.waiting[0])
	}
}

func TestAssignTarget_FreshCapability_AllowsWake(t *testing.T) {
	ag := mustAgent(t, "entity-1", "member-1", "W1", agent.LifecycleRunning)
	ars := newAgentReads()
	ars.put(ag)
	wr := fakeDispatchWorkerRepo{workers: map[string]*workforce.Worker{"W1": dispatchWorker(t, []workforce.Capability{
		{AgentCLI: "claude-code", Detected: true, Enabled: true},
	})}}

	tgt, ok, err := buildAssignTargetWithCapabilities(fakeDispatchReads{}, ars, wr)(
		context.Background(), "agent:member-1", "T1")
	if err != nil || !ok {
		t.Fatalf("fresh capability should resolve, got ok=%v err=%v", ok, err)
	}
	if tgt.AgentID != "entity-1" || tgt.WorkerID != "W1" || tgt.TaskID != "T1" {
		t.Fatalf("target=%+v", tgt)
	}
}
