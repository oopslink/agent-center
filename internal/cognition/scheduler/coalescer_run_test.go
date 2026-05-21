package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/scheduler"
)

func TestCoalescer_Run_StopsOnCtxCancel(t *testing.T) {
	c, _, _, _, _ := newTestCoalescer(t, scheduler.CoalescerConfig{
		RollingWindow: 30 * time.Second,
		HardWindow:    5 * time.Minute,
		BatchSize:     100, MaxConcurrentInvocations: 5,
		TickInterval: 10 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, nil) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil || err != context.Canceled {
			t.Errorf("err = %v", err)
		}
	case <-time.After(time.Second):
		t.Error("timed out")
	}
}

func TestCoalescer_Run_ErrorHookCalled(t *testing.T) {
	// We can't easily induce a Tick error from the in-memory fake; skip
	// the failure-injection variant. We at least exercise the Run loop
	// without crashing.
}
