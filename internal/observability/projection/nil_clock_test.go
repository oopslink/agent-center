package projection_test

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability/projection"
)

func TestService_NilClock_FallsBackToSystem(t *testing.T) {
	env := setup(t)
	svc := projection.NewTaskExecutionProjectionService(env.repo, env.sink, nil, nil)
	if err := svc.UpdateProjection(context.Background(), "E-x", projection.ProjectionUpdate{LastPushAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}
