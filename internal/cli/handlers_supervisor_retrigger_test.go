package cli

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/cognition/scheduler"
	"github.com/oopslink/agent-center/internal/observability"
)

// stubProcessRunner implements scheduler.ProcessRunner without actually
// spawning anything — used to test the retrigger happy path inside the
// CLI handler.
type stubProcessRunner struct {
	wg sync.WaitGroup
}

func (r *stubProcessRunner) Start(_ context.Context, _ scheduler.ProcessSpec, cb func(int, error, string)) (scheduler.ProcessHandle, error) {
	h := &stubHandle{done: make(chan struct{})}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		if cb != nil {
			cb(0, nil, "")
		}
		close(h.done)
	}()
	return h, nil
}

type stubHandle struct{ done chan struct{} }

func (h *stubHandle) PID() int                   { return 1 }
func (h *stubHandle) Signal(_ os.Signal) error   { return nil }
func (h *stubHandle) Kill() error                { return nil }
func (h *stubHandle) Done() <-chan struct{}      { return h.done }

func TestSupervisorRetriggerCommand_HappyWithSpawner(t *testing.T) {
	a := newTestApp(t)
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-RG")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E1"})
	prev, _ := cognition.Spawn(cognition.SpawnInput{ID: "INVRG", Scope: scope, TriggerEvents: tes, StartedAt: time.Now().UTC()})
	if err := a.InvocationRepo.Save(context.Background(), prev); err != nil {
		t.Fatal(err)
	}
	if err := prev.MarkFailed(cognition.FailedReasonClaudeNonZero, "exit 1", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := a.InvocationRepo.UpdateStatusToTerminal(context.Background(), prev); err != nil {
		t.Fatal(err)
	}
	// wire a stub spawner
	runner := &stubProcessRunner{}
	sp, err := scheduler.NewSpawner(scheduler.SpawnerConfig{
		Binary: "agent-center", MemoryDir: t.TempDir(), UsageDir: t.TempDir(),
	}, scheduler.SpawnerDeps{
		DB: a.DB, Repo: a.InvocationRepo, Sink: a.Sink, Clock: a.Clock, IDGen: a.IDGen,
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	a.SetSupervisorSpawner(sp)
	out, errs, code := runHandler(t, a.SupervisorRetriggerCommand(), []string{"INVRG", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code=%d errs=%s", code, errs)
	}
	if out == "" {
		t.Error("empty output")
	}
	runner.wg.Wait()
}

func TestSupervisorRetriggerCommand_SuccessfulCannotRetrigger(t *testing.T) {
	a := newTestApp(t)
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-OK")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E1"})
	inv, _ := cognition.Spawn(cognition.SpawnInput{ID: "INVOK", Scope: scope, TriggerEvents: tes, StartedAt: time.Now().UTC()})
	if err := a.InvocationRepo.Save(context.Background(), inv); err != nil {
		t.Fatal(err)
	}
	if err := inv.MarkSucceeded(time.Now().UTC(), cognition.TokenUsage{}, 0); err != nil {
		t.Fatal(err)
	}
	if err := a.InvocationRepo.UpdateStatusToTerminal(context.Background(), inv); err != nil {
		t.Fatal(err)
	}
	_, _, code := runHandler(t, a.SupervisorRetriggerCommand(), []string{"INVOK"})
	if code != ExitInvalidTransition {
		t.Errorf("code=%d", code)
	}
}
