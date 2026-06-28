package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
)

func TestULIDMinter_PathSafeAndUnique(t *testing.T) {
	m := NewULIDMinter(clock.NewFakeClock(time.Unix(1700000000, 0)))

	e1, e2 := m.NewExecutorID(), m.NewExecutorID()
	p1, p2 := m.NewProblemID(), m.NewProblemID()

	if !strings.HasPrefix(e1, "exec-") {
		t.Errorf("executor id %q should have exec- prefix", e1)
	}
	if !strings.HasPrefix(p1, "problem-") {
		t.Errorf("problem id %q should have problem- prefix", p1)
	}
	if e1 == e2 || p1 == p2 {
		t.Errorf("ids must be unique: exec %q/%q problem %q/%q", e1, e2, p1, p2)
	}
	// Executor ids MUST be path-safe (they become directory names — the F2 guard
	// rejects separators/traversal).
	for _, id := range []string{e1, e2} {
		if err := executorIDPathSafe(id); err != nil {
			t.Errorf("executor id %q not path-safe: %v", id, err)
		}
	}
}

func TestNewULIDMinter_NilClockDefaults(t *testing.T) {
	m := NewULIDMinter(nil) // must not panic; uses system clock
	if id := m.NewExecutorID(); !strings.HasPrefix(id, "exec-") {
		t.Errorf("got %q", id)
	}
}

// executorIDPathSafe routes the id through the same validation the F2 layer uses,
// by attempting to build an Input with it (Input.Validate calls validateExecutorID).
func executorIDPathSafe(id string) error {
	in := executor.Input{ExecutorID: id, Goal: executor.Goal{Title: "t"}, Model: "m", CreatedAt: time.Unix(1, 0)}
	return in.Validate()
}
