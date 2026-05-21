package escalator_test

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability/escalator"
)

func TestEscalator_MissingDeps_Error(t *testing.T) {
	svc := escalator.NewService(nil, nil, nil, escalator.Config{})
	_, err := svc.Scan(context.Background())
	if err == nil {
		t.Fatal("expected missing deps error")
	}
}

func TestEscalator_Run_ErrorReported(t *testing.T) {
	svc := escalator.NewService(nil, nil, nil, escalator.Config{Interval: 10 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	called := false
	svc.Run(ctx, func(_ error) { called = true })
	if !called {
		t.Fatal("onError should be invoked when Scan fails")
	}
}
