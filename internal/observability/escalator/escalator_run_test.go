package escalator_test

import (
	"context"
	"testing"
	"time"
)

func TestEscalator_Run_StopsOnCtxCancel(t *testing.T) {
	svc, _, _, _ := setupEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Run(ctx, func(_ error) {})
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit on ctx cancel")
	}
}
